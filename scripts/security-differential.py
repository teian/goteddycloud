#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Isolated security mitigation replay; never contacts blacklisted hosts."""
import contextlib,http.client,json,os,pathlib,signal,socket,ssl,subprocess,tempfile,time
workspace=pathlib.Path(__file__).resolve().parents[1]
def freeport():
 with socket.socket() as sock:sock.bind(('127.0.0.1',0));return sock.getsockname()[1]
runs=[]
with tempfile.TemporaryDirectory(prefix='goteddycloud-security-') as folder:
 for name,binary in [('C',workspace/'.cache/teddycloud-develop-reference/bin/teddycloud'),('Go',workspace/'bin/goteddycloud')]:
  results=[]
  for scenario,extra in [('priority',{}),('crawler',{'security_mit.onBlacklistDomain':'false'}),('robots',{}),('https',{'security_mit.httpsOnly':'true'}),('hardlock',{'security_mit.hardLock':'true'})]:
   root=pathlib.Path(folder)/name/scenario
   subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(root)],cwd=workspace,check=True,capture_output=True)
   (root/'data/www/robots.txt').write_text('User-agent: *\nDisallow: /\n')
   http_port,web_port,box_port=freeport(),freeport(),freeport()
   options={'core.server.http_port':str(http_port),'core.server.https_web_port':str(web_port),'core.server.https_api_port':str(box_port),**extra}
   config=root/'config/config.ini';config.write_text(config.read_text()+''.join(f'{key}={value}\n' for key,value in options.items()))
   log=open(workspace/'.cache'/f'security-{name}-{scenario}.log','w');process=subprocess.Popen([str(binary),'--base_path',str(root)+'/'],cwd=workspace,stdout=log,stderr=log,start_new_session=True)
   def request(path,agent='',tls=False):
    connection=http.client.HTTPSConnection('127.0.0.1',web_port,context=ssl._create_unverified_context(),timeout=5) if tls else http.client.HTTPConnection('127.0.0.1',http_port,timeout=5)
    connection.request('GET',path,headers={'User-Agent':agent});response=connection.getresponse();data=response.read();connection.close();return response.status,response.getheader('Content-Type'),data
   try:
    for _ in range(100):
     try:request('/api/settings/get/core.server.http_port');break
     except OSError:time.sleep(.1)
    else:raise AssertionError('readiness')
    if scenario in ['priority','crawler']:
     results.append((scenario,'request',request('/api/settings/get/security_mit.lockAccess','synthetic Googlebot')))
     results.append((scenario,'later',request('/api/settings/get/internal.security_mit.incident')))
    elif scenario in ['robots','https']:
     results.append((scenario,'robots',request('/robots.txt')))
     results.append((scenario,'later',request('/api/settings/get/internal.security_mit.incident')))
     if scenario=='https':results.append((scenario,'TLS',request('/api/settings/get/internal.security_mit.incident',tls=True)))
    else:results.append((scenario,'locked',request('/api/settings/get/internal.security_mit.incident')))
   finally:
    os.killpg(process.pid,signal.SIGTERM)
    try:process.wait(timeout=8)
    except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
    with contextlib.suppress(ProcessLookupError):os.killpg(process.pid,signal.SIGKILL)
    log.close()
  runs.append(results)
for reference,candidate in zip(*runs):
 if reference[:2]==('https','later'):
  assert b'locked' in reference[2][2] and candidate[2]==(200,'text/plain',b'false'),(reference,candidate)
 else:assert reference==candidate,(reference,candidate)
print(f'Security differential: {len(runs[0])} responses match (priority, crawler, robots, HTTPS-only, startup hard lock); HTTP HTTPS-only deviation asserted')
