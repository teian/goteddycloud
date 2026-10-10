#!/usr/bin/env python3
"""C/Go replay on marked disposable content fixtures."""
import http.client,json,os
from pathlib import Path
from urllib.parse import urlencode
roots=[Path('.cache/oracle-runtime'),Path('.cache/ci-candidate-runtime')]
ports=[18080,18081]
for root in roots: assert (root/'.synthetic-fixture').is_file()
def request(port,method,path,data=None,kind=None):
 c=http.client.HTTPConnection('127.0.0.1',port,timeout=30)
 c.request(method,path,data,{'Content-Type':kind} if kind else {})
 r=c.getresponse(); result=(r.status,r.getheader('Content-Type'),r.read());c.close();return result
def compare(name,method,path,data=None,kind=None,status=200,json_body=False):
 results=[request(port,method,path,data,kind) for port in ports]
 if json_body: results=[(s,k,json.loads(b)) for s,k,b in results]
 assert results[0]==results[1],(name,results)
 assert results[0][0]==status,(name,results)
 print('PASS content API '+name,flush=True)
 return results[0]
uid='0500000005000000'
for root in roots:
 folder=root/'data/content/default/05000000';folder.mkdir()
 (folder/'05000000.json').write_text(json.dumps({'live':False,'nocloud':True,'source':'','fixture_extension':'keep'}))
 (folder/'05000000').write_bytes((root/'data/library/synthetic.taf').read_bytes())
compare('get raw metadata','GET','/content/json/get/'+uid)
compare('raw JSON path','GET','/content/json/05000000/05000000.json')
compare('missing metadata','GET','/content/json/get/0700000007000000',status=404)
compare('metadata edit','POST','/content/json/set/'+uid,urlencode({'hide':'true','live':'false','claimed':'true','tonie_model':'synthetic-model','source':'','ignored':'foo'}).encode(),'application/x-www-form-urlencoded')
for root in roots:
 metadata=json.loads((root/'data/content/default/05000000/05000000.json').read_text())
 assert metadata['hide'] and metadata['claimed'] and metadata['tonie_model']=='synthetic-model'
assert json.loads((roots[1]/'data/content/default/05000000/05000000.json').read_text())['fixture_extension']=='keep'
compare('raw TAF','GET','/content/05000000/05000000')
compare('Ogg payload','GET','/content/05000000/05000000?ogg=true')
compare('download tag','GET','/content/download/05000000/05000000?skip_header=true')
boundary='content-fixture-boundary';data=b''
for name,payload in [('one.txt',b'first'),('two.txt',b'second')]:
 data+=('--'+boundary+'\r\nContent-Disposition: form-data; name="file"; filename="'+name+'"\r\n\r\n').encode()+payload+b'\r\n'
data+=('--'+boundary+'--\r\n').encode()
for root in roots: (root/'data/library/content-api-index').mkdir()
compare('multipart files','POST','/api/fileUpload?special=library&path=content-api-index',data,'multipart/form-data; boundary='+boundary)
for root in roots:
 folder=root/'data/library/content-api-index'
 (folder/'audio.taf').write_bytes((root/'data/library/synthetic.taf').read_bytes())
 for name in ['one.txt','two.txt','audio.taf']: os.utime(folder/name,(1700000000,1700000000))
for version in ['','V2']:
 results=[request(port,'GET','/api/fileIndex'+version+'?special=library&path=content-api-index') for port in ports]
 assert all(s==200 and k=='text/json' for s,k,b in results),results
 rows=[json.loads(b)['files'] for s,k,b in results]
 # Parent directory dates/sizes are deployment-specific filesystem values.
 rows=[[r for r in rs if r['name']!='..'] for rs in rows]
 rows=[sorted(rs,key=lambda r:r['name']) for rs in rows]
 assert rows[0]==rows[1],(version,rows)
 assert len(rows[0])==3 and rows[0][0]['name']=='audio.taf',(version,rows)
 print('PASS content API file index '+(version or 'legacy'),flush=True)
compare('missing legacy directory','GET','/api/fileIndex?special=library&path=absent',json_body=True)
