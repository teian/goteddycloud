#!/usr/bin/env python3
"""Pinned develop oracle and Go checks on fresh synthetic deployments only."""
import argparse, sys, shutil, contextlib, http.client, json, os, pathlib, signal, socket, subprocess, time, zipfile, io
workspace=pathlib.Path(__file__).resolve().parents[1]
reference=workspace/'.cache/teddycloud-develop-reference'
parser=argparse.ArgumentParser();parser.add_argument('--stage',default='.cache/develop-rehearsal');parser.add_argument('--go-binary',default=str(workspace/'bin/goteddycloud'));mode=parser.add_mutually_exclusive_group();mode.add_argument('--hardening-only',action='store_true');mode.add_argument('--api-only',action='store_true',help='compare auth, JSON and track exports without rerunning the hardware simulator');args=parser.parse_args()
stage=workspace/args.stage
assert not stage.exists(), 'use a fresh rehearsal directory'
stage.mkdir()
def freeport():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def req(port,method,path,body=None,token=None):
 headers={}
 if isinstance(body,dict):body=json.dumps(body);headers['Content-Type']='application/json'
 if token:headers['Authorization']='Bearer '+token
 c=http.client.HTTPConnection('127.0.0.1',port,timeout=30);c.request(method,path,body,headers);r=c.getresponse();result=(r.status,dict(r.getheaders()),r.read());c.close();return result
processes=[];targets=[]
try:
 for name,binary in [('C',reference/'bin/teddycloud'),('Go',pathlib.Path(args.go_binary).resolve())]:
  root=stage/name;subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(root)],cwd=workspace,check=True,capture_output=True)
  if name=='C':
   subprocess.run([str(reference/'bin/teddycloud'),'--base_path',str(root)+'/', '--encode',str(root/'data/library/synthetic.taf'),str(root/'tone1.wav'),str(root/'tone2.wav')],check=True,stdout=(stage/(name+'-encode.log')).open('w'),stderr=subprocess.STDOUT)
  else:shutil.copyfile(stage/'C/data/library/synthetic.taf',root/'data/library/synthetic.taf')
  ca=root/'certs/server/ca-root.pem'
  subprocess.run(['openssl','req','-new','-x509','-key',str(root/'certs/server/ca-key.pem'),'-out',str(ca),'-days','1','-subj','/CN=TeddyCloud Synthetic Test CA','-addext','basicConstraints=critical,CA:TRUE'],check=True,capture_output=True)
  subprocess.run(['bash','scripts/frontend.sh','install-fixture',str(root)],cwd=workspace,env=dict(os.environ,REFERENCE_BRANCH='develop'),check=True)
  ports=[freeport() for _ in range(3)];config=root/'config/config.ini';config.write_text(config.read_text()+'core.boxCertAuth=true\n'+''.join(f'{key}={value}\n' for key,value in zip(['core.server.http_port','core.server.https_web_port','core.server.https_api_port'],ports)))
  log=(stage/(name+'-server.log')).open('w');p=subprocess.Popen([str(binary),'--base_path',str(root)+'/'],stdout=log,stderr=log,start_new_session=True);processes.append((p,log))
  for _ in range(100):
   try:
    if req(ports[0],'GET','/api/auth/status')[0]==200:break
   except OSError:pass
   if p.poll() is not None:raise AssertionError((name,'server exited'))
   time.sleep(.1)
  else:raise AssertionError((name,'readiness'))
  targets.append((name,root,ports))
 for name,root,ports in targets:
  port=ports[0]
  def call(method,path,body=None,token=None,status=200):
   result=req(port,method,path,body,token);assert result[0]==status,(name,path,result);return result
  call('POST','/api/auth/users/create',{'username':'port-test','password':'secret'})
  call('POST','/api/auth/enabled',{'enabled':True})
  call('GET','/api/settings/getIndex',status=401)
  call('OPTIONS','/api/unknown',status=204)
  _,headers,data=call('POST','/api/auth/login',{'username':'PORT-TEST','password':'secret'});token=json.loads(data)['token'];assert len(token)==64 and 'HttpOnly' in headers['Set-Cookie']
  call('POST','/api/auth/refresh-token',token=token)
  call('GET','/api/settings/getIndex',token=token)
  call('POST','/api/auth/enabled',{'enabled':False},token)
  call('POST','/api/auth/users/delete',{'username':'port-test'},token)
  print('PASS',name,'auth, gate, CORS, cookie, refresh and user lifecycle',flush=True)
 for path in ['/api/auth/status','/api/settings/getIndex?internal=true&nolevel=true','/api/fileIndexV2?special=library','/api/getTagInfo?ruid=0000000100000001']:
  results=[req(t[2][0],'GET',path) for t in targets]
  left,right=[json.loads(r[2]) for r in results]
  if path=='/api/auth/status':
   # Native roles/areas extend the legacy status response. This replay has
   # removed its only user, so each added identity field must be empty.
   for key in ['role','area','areaRole','selectedArea']:
    assert right.pop(key)=='',(path,key,right)
  if 'getIndex' in path:
   assert all(d['level']!=99 for d in right['options']);continue # documented C-only state defaults differ
  if 'fileIndex' in path:
   for obj in (left,right):
    obj['files']=[row for row in obj['files'] if row['name']!='..']
    for row in obj['files']:row.pop('date',None)
  assert left==right,(path,left,right)
  print('PASS develop JSON differential',path,flush=True)
 for tracks in ['1','2','1,2']:
  results=[req(t[2][0],'GET','/content/synthetic.taf?special=library&tracks='+tracks) for t in targets]
  assert all(r[0]==200 for r in results),results
  if ',' not in tracks:assert results[0][2]==results[1][2]
  else:
   entries=[]
   for r in results:
    with zipfile.ZipFile(io.BytesIO(r[2])) as z:entries.append({n:z.read(n) for n in z.namelist()})
   assert entries[0]==entries[1],[(k,len(v)) for k,v in entries[0].items()]
  print('PASS develop track byte differential',tracks,flush=True)
 for name,root,ports in ([] if args.api_only else targets):
  env=dict(os.environ,TEDDYCLOUD_BASE_URL=f'http://127.0.0.1:{ports[0]}',TEDDYCLOUD_HTTPS_API_PORT=str(ports[2]),TC_SANDBOX=str(root),TC_TEMPLATE=str(root),TEDDYCLOUD_CLIENT_CERT=str(root/'certs/client/001122334455/teddy-cert.pem'),TEDDYCLOUD_CLIENT_KEY=str(root/'certs/client/001122334455/teddy-key.pem'))
  tests=['test_cors_preflight.py','test_taf_length.py','test_web_legacy_gone.py','test_box_sim.py','test_box_profiles.py']
  if args.hardening_only:
   identity=root/'pool-cert';identity.mkdir()
   subprocess.run([str(reference/'bin/teddycloud'),'--base_path',str(root)+'/', '--generate-client-cert','deadbeef00ff','--destination',str(identity)],check=True,capture_output=True)
   subprocess.run(['openssl','x509','-inform','DER','-in',str(identity/'client.der'),'-out',str(identity/'client.pem')],check=True,capture_output=True)
   subprocess.run(['openssl','rsa','-inform','DER','-in',str(identity/'private.der'),'-out',str(identity/'private.pem')],check=True,capture_output=True)
   env['TEDDYCLOUD_CLIENT_CERT']=str(identity/'client.pem');env['TEDDYCLOUD_CLIENT_KEY']=str(identity/'private.pem')
  if args.hardening_only:tests=['test_auth_pool_reuse.py','test_hardening.py::Settings528','test_hardening.py::Secrets505','test_hardening.py::PrivateFetch']
  env['PYTHONPATH']=str(workspace/'scripts')
  env['PYTEST_PLUGINS']='develop_pytest_policy' if name=='Go' else 'develop_reference_policy'
  subprocess.run([sys.executable,'-m','pytest','-xq']+[str(reference/'tests/py'/t) for t in tests],cwd=workspace,env=env,check=True)
  print('PASS',name,'upstream develop tests',flush=True)
finally:
 for p,log in processes:
  with contextlib.suppress(ProcessLookupError):os.killpg(p.pid,signal.SIGTERM)
  try:p.wait(timeout=8)
  except subprocess.TimeoutExpired:
   with contextlib.suppress(ProcessLookupError):os.killpg(p.pid,signal.SIGKILL)
   p.wait()
  log.close()
