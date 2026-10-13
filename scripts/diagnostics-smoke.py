#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Executable opt-in packet/TLS diagnostics with disposable identities only."""
from fixture_tls import client_context
import contextlib,http.client,os,pathlib,signal,socket,ssl,struct,subprocess,tempfile,time
workspace=pathlib.Path(__file__).resolve().parents[1]
def freeport():
 with socket.socket() as sock:sock.bind(('127.0.0.1',0));return sock.getsockname()[1]
with tempfile.TemporaryDirectory(prefix='goteddycloud-diagnostics-') as folder:
 root=pathlib.Path(folder)/'fixture';subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(root)],cwd=workspace,check=True,capture_output=True)
 ports=[freeport() for _ in range(3)]
 options={'core.server.http_port':ports[0],'core.server.https_web_port':ports[1],'core.server.https_api_port':ports[2],'pcap.enabled':'true','pcap.filename':'config/synthetic.pcap','core.sslkeylogfile':'config/synthetic.keys'}
 config=root/'config/config.ini';config.write_text(config.read_text()+''.join(f'{k}={v}\n' for k,v in options.items()))
 previous=0
 for run in range(2):
  log=open(workspace/'.cache'/f'diagnostics-{run}.log','w');process=subprocess.Popen([str(workspace/os.environ.get('GOTEDDYCLOUD_BINARY','bin/goteddycloud')),'--base_path',str(root)],stdout=log,stderr=log,start_new_session=True)
  def request(port,tls=False):
   c=http.client.HTTPSConnection('127.0.0.1',port,context=client_context(root),timeout=5) if tls else http.client.HTTPConnection('127.0.0.1',port,timeout=5)
   c.request('GET','/v1/time' if port==ports[2] else '/api/time');r=c.getresponse();data=r.read();assert r.status==200 and data.isdigit();c.close()
  try:
   for _ in range(100):
    try:request(ports[0]);break
    except OSError:time.sleep(.1)
   else:raise AssertionError('readiness')
   request(ports[1],True);request(ports[2],True)
  finally:
   os.killpg(process.pid,signal.SIGTERM)
   try:process.wait(timeout=8)
   except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
   log.close()
  capture=root/'config/synthetic.pcap';raw=capture.read_bytes();assert len(raw)>previous;previous=len(raw);assert raw[:8]==bytes.fromhex('d4c3b2a102000400') and struct.unpack_from('<I',raw,20)[0]==101
  pos=24;payload=b'';packets=0
  while pos<len(raw):
   size=struct.unpack_from('<I',raw,pos+8)[0];pos+=16;packet=raw[pos:pos+size];assert len(packet)==size and size>=40;payload+=packet[40:];pos+=size;packets+=1
  assert pos==len(raw) and b'GET /v1/time HTTP/1.1' in payload and b'HTTP/1.1 200 OK' in payload and b'PRIVATE KEY' not in payload
  keyfile=root/'config/synthetic.keys';lines=keyfile.read_text().splitlines();randoms={line.split()[1] for line in lines if len(line.split())==3};assert len(randoms)>=2*(run+1)
  assert capture.stat().st_mode&0o777==0o600 and keyfile.stat().st_mode&0o777==0o600
 print(f'Diagnostics smoke: HTTP/web TLS/box TLS, {packets} valid PCAP records, TLS secrets, restart append and private file modes pass')
