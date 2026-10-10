#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Mutual-TLS mock cloud replay against pinned C and Go, with no public targets."""
import contextlib,http.client,http.server,json,os,pathlib,signal,socket,ssl,struct,subprocess,tempfile,threading,time,urllib.parse
workspace=pathlib.Path(__file__).resolve().parents[1]
def freeport():
 with socket.socket() as sock:sock.bind(('127.0.0.1',0));return sock.getsockname()[1]
def varint(value):
 out=bytearray()
 while value>127:out.append((value&127)|128);value>>=7
 out.append(value);return bytes(out)
def freshness(ruid,audio_id):
 uid=int.from_bytes(bytes.fromhex(ruid),'little');info=b'\x09'+struct.pack('<Q',uid)+b'\x15'+struct.pack('<I',audio_id);return b'\x0a'+varint(len(info))+info
records=[];mode='success';taf=b''
class Cloud(http.server.BaseHTTPRequestHandler):
 protocol_version='HTTP/1.1'
 def do_POST(self):self.do_GET()
 def do_GET(self):
  global mode
  body=self.rfile.read(int(self.headers.get('Content-Length','0')));assert self.connection.getpeercert(),'client identity missing'
  records.append((self.command,self.path,self.headers.get('Authorization'),body))
  path=urllib.parse.urlsplit(self.path).path;status=200;kind='application/octet-stream';extra={}
  if mode=='unavailable':status=503;data=b'mock unavailable'
  elif path=='/v1/time':data=b'1700000000';kind='text/plain; charset=utf-8'
  elif path=='/v1/freshness-check':data=b'\x10\0\x18\x03\x20\x01\x28\0\x30\0\x38\x03\x40\0'
  elif path=='/v3/freshness-check':data=json.dumps({'items':list(json.loads(body)['content'])},separators=(',',':')).encode();kind='application/json; charset=utf-8'
  elif path=='/v3/setup-status':data=b'{"accepted":true}';kind='application/json; charset=utf-8'
  elif path=='/v3/check-ota':data=b'{"updates":[]}';kind='application/json; charset=utf-8'
  elif path.startswith('/v3/content-meta/'):data=b'{"version":42,"content":[]}';kind='application/json; charset=utf-8'
  elif path.startswith('/v3/chapter/'):data=b'synthetic chapter bytes'
  elif path.startswith('/v1/content/') or path.startswith('/v2/content/'):data=taf
  elif path.startswith('/v1/ota/'):data=b'synthetic OTA';extra['Content-Disposition']='attachment;filename=1900000000.bin'
  elif path.startswith('/v3/ota/'):data=b'synthetic V3 OTA';extra['Content-Disposition']='attachment;filename='+path.rsplit('/',1)[1]+'.bin'
  elif path.startswith('/v1/cloud-reset'):data=b'{}';kind='application/json; charset=utf-8'
  elif path.startswith('/v1/claim'):data=b''
  elif path.startswith('/v1/log'):data=b'accepted log'
  else:status=404;data=b'unknown mock request'
  self.send_response(status);self.send_header('Content-Type',kind);self.send_header('Content-Length',str(len(data)))
  for key,value in extra.items():self.send_header(key,value)
  self.end_headers();self.wfile.write(data)
 def log_message(self,*args):pass
checks=0
with tempfile.TemporaryDirectory(prefix='goteddycloud-cloud-') as folder:
 base=pathlib.Path(folder);common=base/'common';subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(common)],cwd=workspace,check=True,capture_output=True)
 output=common/'data/library/mock.taf';subprocess.run(['go','run','-buildvcs=false','./cmd/tafprobe','-output',str(output),str(common/'tone1.wav'),str(common/'tone2.wav')],cwd=workspace,check=True,capture_output=True);taf=output.read_bytes()
 cloud=http.server.ThreadingHTTPServer(('127.0.0.1',0),Cloud);policy=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);policy.load_cert_chain(str(common/'certs/server/teddy-cert.pem'),str(common/'certs/server/teddy-key.pem'));policy.verify_mode=ssl.CERT_REQUIRED;policy.load_verify_locations(str(common/'certs/client/001122334455/ca-root.pem'));cloud.socket=policy.wrap_socket(cloud.socket,server_side=True);thread=threading.Thread(target=cloud.serve_forever,daemon=True);thread.start()
 runs=[]
 try:
  for name,binary in [('C',workspace/'.cache/teddycloud-develop-reference/bin/teddycloud'),('Go',workspace/'bin/goteddycloud')]:
   root=base/name;subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(root)],cwd=workspace,check=True,capture_output=True);(root/'data/library/synthetic.taf').write_bytes(taf)
   http_port,web_port,box_port=freeport(),freeport(),freeport()
   options={'cloud.enabled':'true','cloud.remote_hostname':'127.0.0.1','cloud.remote_hostname_tb2':'127.0.0.1','cloud.remote_port':str(cloud.server_port),'cloud.remote_port_tb2':str(cloud.server_port),'cloud.markCustomTagByUid':'false','cloud.markCustomTagByPass':'false','cloud.cacheContent':'true','cloud.cacheToLibrary':'false','cloud.cacheOta':'true','cloud.localOta':'true','cloud.enableV1Time':'true','cloud.enableV1Log':'true','cloud.enableV1CloudReset':'true','cloud.enableV1Ota':'true','core.client_cert.file.ca':str(common/'certs/server/teddy-cert.pem'),'core.client_cert.file.crt':str(common/'certs/client/001122334455/teddy-cert.pem'),'core.client_cert.file.key':str(common/'certs/client/001122334455/teddy-key.pem'),'core.server.http_port':str(http_port),'core.server.https_web_port':str(web_port),'core.server.https_api_port':str(box_port)}
   config=root/'config/config.ini';config.write_text(config.read_text()+''.join(f'{key}={value}\n' for key,value in options.items()))
   def metadata(ruid,value):
    target=root/'data/content/default'/ruid[:8].upper()/(ruid[8:].upper()+'.json');target.parent.mkdir(parents=True,exist_ok=True);target.write_text(json.dumps(value))
   metadata('0300000003000000',{'nocloud':True});metadata('0400000004000000',{'nocloud':True})
   metadata('0500000005000000',{'nocloud':True,'cloud_override':True,'cloud_ruid':'0600000006000000','cloud_auth':'02'*32})
   log=open(workspace/'.cache'/f'cloud-{name}-server.log','w');process=subprocess.Popen([str(binary),'--base_path',str(root)+'/'],cwd=workspace,stdout=log,stderr=log,start_new_session=True)
   results={};start_record=len(records)
   def request(label,method,path,data=None,json_body=False,auth=True):
    connection=http.client.HTTPConnection('127.0.0.1',http_port,timeout=15) if name=='C' else http.client.HTTPSConnection('127.0.0.1',box_port,context=ssl.create_default_context(cafile=str(root/'certs/server/teddy-cert.pem')),timeout=15)
    headers={'Content-Type':'application/json' if path.startswith('/v3/') else 'application/octet-stream'}
    if auth:headers['Authorization']='BD '+('01'*32)
    connection.request(method,path,data,headers);response=connection.getresponse();body=response.read();result=(response.status,response.getheader('Content-Type'),json.loads(body) if json_body and body else body);connection.close();results[label]=result;return result
   def set_value(key,value):
    connection=http.client.HTTPConnection('127.0.0.1',http_port,timeout=5);connection.request('POST','/api/settings/set/'+key,value);response=connection.getresponse();response.read();connection.close();assert response.status==200
   try:
    for _ in range(100):
     try:
      connection=http.client.HTTPConnection('127.0.0.1',http_port,timeout=1);connection.request('GET','/api/settings/get/cloud.enabled');response=connection.getresponse();response.read();connection.close();break
     except OSError:time.sleep(.1)
    else:raise AssertionError((name,'readiness'))
    mode='success'
    request('time','GET','/v1/time')
    request('claim allowed','GET','/v1/claim/0200000002000000')
    before=len(records);request('claim protected','GET','/v1/claim/0300000003000000');assert len(records)==before
    request('claim override','GET','/v1/claim/0500000005000000')
    request('log','POST','/v1/log',b'synthetic log')
    request('reset','POST','/v1/cloud-reset',b'{}',json_body=True)
    request('setup','POST','/v3/setup-status',b'{"step":1}',json_body=True)
    request('check OTA','POST','/v3/check-ota',b'{"version":1}',json_body=True)
    request('freshness v1','POST','/v1/freshness-check',freshness('0200000002000000',10)+freshness('0300000003000000',10))
    request('freshness v3','POST','/v3/freshness-check',b'{"content":{"0200000002000000":10,"0300000003000000":10}}',json_body=True)
    request('remote metadata','GET','/v3/content-meta/0700000007000000',json_body=True)
    request('remote chapter','GET','/v3/chapter/mock-chapter?auth=synthetic')
    request('content miss','GET','/v2/content/0800000008000000')
    before=len(records);request('content hit','GET','/v2/content/0800000008000000');assert len(records)==before
    request('local meta','GET','/v3/content-meta/0800000008000000',json_body=True)
    request('local second chapter','GET','/v3/chapter/teddycloud_01_0800000008000000')
    set_value('cloud.enableV2Content','false');request('V1 shares V2 flag','GET','/v1/content/0900000009000000');set_value('cloud.enableV2Content','true')
    request('OTA V1 cache first','GET','/v1/ota/2?cv=0');request('OTA V1 cache next','GET','/v1/ota/2?cv=0')
    request('OTA V3 cache','GET','/v3/ota/2/0123456789abcdef')
    # Explicitly record the deliberate protection of nocloud metadata in Go.
    request('protected missing meta deviation','GET','/v3/content-meta/0400000004000000')
    assert results['protected missing meta deviation'][0]==(200 if name=='C' else 404)
    mode='unavailable'
    request('setup HTTP 503','POST','/v3/setup-status',b'{}')
    request('chapter HTTP 503','GET','/v3/chapter/unavailable')
    request('OTA V3 failure retains cache','GET','/v3/ota/2/0123456789abcdef')
    mode='success'
    if name=='Go':
     before=len(records)
     diagnostic=subprocess.run([str(binary),'--base_path',str(root),'--cloud-test','/v1/time','--hash','01'*32],check=True,capture_output=True,text=True,timeout=15)
     assert diagnostic.stdout.strip()=='HTTP 200; 10 bytes'
     assert records[before][2]=='BD '+('01'*32)
     del records[before:]
    set_value('cloud.enabled','false');before=len(records)
    request('cloud disabled protects cache','GET','/v2/content/0800000008000000');assert len(records)==before
    files={str(p.relative_to(root/'data')):p.read_bytes() for p in (root/'data').rglob('*') if p.is_file() and (p.suffix=='.bin' or str(p.relative_to(root/'data')).startswith('content/default/08000000/') and p.suffix!='.json')}
    runs.append((results,records[start_record:].copy(),files))
   finally:
    os.killpg(process.pid,signal.SIGTERM)
    try:process.wait(timeout=8)
    except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
    with contextlib.suppress(ProcessLookupError):os.killpg(process.pid,signal.SIGKILL)
    log.close()
  (workspace/".cache/cloud-results.txt").write_text(repr(runs))
  reference,candidate=runs
  for label,value in reference[0].items():
   if label=='protected missing meta deviation':continue
   assert value==candidate[0][label],(label,value,candidate[0][label]);checks+=1
  def normalized(entries):
   result=[]
   for method,path,authorization,body in entries:
    if path=='/v3/content-meta/0400000004000000':continue
    if path=='/v3/freshness-check':body=json.loads(body)
    result.append((method,path,authorization,body))
   return result
  assert normalized(reference[1])==normalized(candidate[1]),('forwarded requests',normalized(reference[1]),normalized(candidate[1]))
  assert reference[2]==candidate[2],('cache file effects',reference[2].keys(),candidate[2].keys())
  print(f'Cloud differential: {checks} response checks, forwarded methods/paths/auth/bodies and cache bytes match; protected V3 metadata deviation asserted')
 finally:cloud.shutdown();cloud.server_close();thread.join()
