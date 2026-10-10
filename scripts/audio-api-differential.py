#!/usr/bin/env python3
"""Replay normal audio-management contracts against owned C and Go fixtures."""
import argparse
import http.client
import json
import ssl
from pathlib import Path
import subprocess
import urllib.parse

p=argparse.ArgumentParser()
p.add_argument('--reference-http',type=int,default=18080)
p.add_argument('--candidate-http',type=int,default=18081)
p.add_argument('--reference-web',type=int,default=18443)
p.add_argument('--candidate-web',type=int,default=19443)
p.add_argument('--reference-root',type=Path,default=Path('.cache/oracle-runtime'))
p.add_argument('--candidate-root',type=Path,default=Path('.cache/ci-candidate-runtime'))
a=p.parse_args()
roots=[a.reference_root.resolve(),a.candidate_root.resolve()]
ports=[a.reference_http,a.candidate_http]
web_ports=[a.reference_web,a.candidate_web]
for root in roots:assert (root/'.synthetic-fixture').is_file()
def request(port,path,data,kind):
    # Large binary multipart bodies hit the pinned C raw HTTP buffering bug;
    # use the verified web TLS listener, as in the firmware content comparison.
    index=ports.index(port)
    c=http.client.HTTPSConnection('127.0.0.1',web_ports[index],context=ssl.create_default_context(cafile=str(roots[index]/'certs/server/teddy-cert.pem')),timeout=30) if kind.startswith('multipart/') else http.client.HTTPConnection('127.0.0.1',port,timeout=30)
    c.request('POST',path,body=data,headers={'Content-Type':kind})
    r=c.getresponse();result=(r.status,r.getheader('Content-Type'),r.read());c.close();return result

def multipart(name,data):
    boundary='synthetic-teddycloud-audio-test'
    return (('--'+boundary+'\r\nContent-Disposition: form-data; name="file"; filename="'+name+'"\r\nContent-Type: application/octet-stream\r\n\r\n').encode()+data+('\r\n--'+boundary+'--\r\n').encode(),'multipart/form-data; boundary='+boundary)

def compare(name,path,data,kind,status,message):
    observations=[request(port,path,data,kind) for port in ports]
    assert observations[0]==observations[1],(name,observations)
    assert observations[0]==(status,'text/plain; charset=utf-8',message),(name,observations)
    print('PASS audio API '+name,flush=True)

data=(roots[0]/'data/library/synthetic.taf').read_bytes()
body,kind=multipart('api-upload.taf',data)
compare('TAF upload','/api/tafUpload?special=library&name=api-upload.taf',body,kind,200,b'OK')
for root in roots:assert (root/'data/library/api-upload.taf').read_bytes()==data
body,kind=multipart('api-bad.taf',b'not a TAF')
compare('invalid TAF','/api/tafUpload?special=library&name=api-bad.taf',body,kind,500,b'TAF validation failed')
for root in roots:assert not (root/'data/library/api-bad.taf').exists()
raw=(roots[0]/'tone1.wav').read_bytes()[44:]
body,kind=multipart('samples.pcm',raw)
compare('PCM upload','/api/pcmUpload?special=library&name=api-pcm.taf&audioId=1234',body,kind,200,b'OK')
form=urllib.parse.urlencode([('source','tone1.wav'),('source','tone2.wav'),('target','api-encoded.taf')]).encode()
compare('multiple file encode','/api/fileEncode?special=library',form,'application/x-www-form-urlencoded',200,b'OK\r\n')
# Codec bytes/vendor strings may differ; validate each output with the Go parser.
for filename,tracks,audio_id in [('api-pcm.taf',1,1234),('api-encoded.taf',2,None)]:
    for root in roots:
        output=subprocess.check_output(['go','run','-buildvcs=false','./cmd/tafprobe',str(root/'data/library'/filename)],text=True)
        header=json.loads(output)
        assert len(header['tracks'])==tracks,(filename,header)
        if audio_id is not None:assert header['audio_id']==audio_id,(filename,header)
    print('PASS audio API validated '+filename,flush=True)

# File management and migration use the same confined publication services.
for endpoint,body,status in [('dirCreate','api-dir',200),('dirCreate','api-dir',500),('dirDelete','api-dir',200),('fileDelete','api-missing',500),('fileMove','source=api-missing&target=api-other',500),('fileMove','source=api-upload.taf&target=api-moved.taf',200),('fileDelete','api-moved.taf',200)]:
    observations=[request(port,'/api/'+endpoint+'?special=library',body.encode(),'application/x-www-form-urlencoded') for port in ports]
    assert observations[0]==observations[1],(endpoint,body,observations)
    assert observations[0][0]==status,(endpoint,observations)
    print('PASS file API '+endpoint+' '+body,flush=True)
for root in roots:
    folder=root/'data/content/default/04000000'
    folder.mkdir()
    (folder/'04000000').write_bytes(data)
    (folder/'04000000.json').write_text(json.dumps({'source':'','nocloud':True,'fixture_extension':'keep'}))
observations=[request(port,'/api/migrateContent2Lib',b'ruid=0400000004000000&libroot=true','application/x-www-form-urlencoded') for port in ports]
assert observations[0]==observations[1],('migration',observations)
assert observations[0][0]==200,observations
for root in roots:
    assert not (root/'data/content/default/04000000/04000000').exists()
    mapping=json.loads((root/'data/content/default/04000000/04000000.json').read_text())
    target=root/'data/library'/mapping['source'].removeprefix('lib://')
    assert target.read_bytes()==data
assert json.loads((roots[1]/'data/content/default/04000000/04000000.json').read_text())['fixture_extension']=='keep'
print('PASS file API root-library migration and unknown metadata preservation',flush=True)
