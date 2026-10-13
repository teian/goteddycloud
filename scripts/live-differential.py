#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Compare live response contracts with the isolated pinned C encoder."""
from fixture_tls import client_context
import http.client,http.server,json,os,pathlib,signal,socket,ssl,struct,subprocess,tempfile,threading,time
workspace=pathlib.Path(__file__).resolve().parents[1]
def freeport():
 with socket.socket() as sock:sock.bind(('127.0.0.1',0));return sock.getsockname()[1]
def header():
 data=bytearray(44);data[:4]=b'RIFF';struct.pack_into('<I',data,4,0x7fffff00);data[8:16]=b'WAVEfmt ';struct.pack_into('<IHHIIHH',data,16,16,1,2,48000,192000,4,16);data[36:40]=b'data';struct.pack_into('<I',data,40,0x7ffffedc);return data
class Radio(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  self.send_response(200);self.end_headers()
  try:
   self.wfile.write(header());self.wfile.flush()
   # Noise avoids unbounded Ogg lacing of tiny silence packets in the oracle.
   sample=b''.join(struct.pack('<hh',(i*113)%12000-6000,(i*97)%12000-6000) for i in range(4000))
   for _ in range(2000):self.wfile.write(sample);self.wfile.flush();time.sleep(.01)
  except (BrokenPipeError,ConnectionResetError):pass
 def log_message(self,*args):pass
radio=http.server.ThreadingHTTPServer(('127.0.0.1',0),Radio);thread=threading.Thread(target=radio.serve_forever,daemon=True);thread.start()
def varint(data,pos):
 value=0;shift=0
 while True:
  b=data[pos];pos+=1;value|=(b&127)<<shift
  if b<128:return value,pos
  shift+=7;assert shift<70
def taf_header(data):
 size=struct.unpack('>I',data[:4])[0];assert 0<size<=4092;pos=4;fields={}
 while pos<4+size:
  key,pos=varint(data,pos);kind=key&7
  if kind==0:value,pos=varint(data,pos)
  elif kind==2:length,pos=varint(data,pos);value=data[pos:pos+length];pos+=length
  else:raise AssertionError(('unexpected header wire type',kind))
  fields[key>>3]=value
 assert len(fields[1])==20
 return fields
results=[]
try:
 with tempfile.TemporaryDirectory(prefix='goteddycloud-live-') as folder:
  base=pathlib.Path(folder)
  for name,binary in [('C',workspace/'.cache/teddycloud-develop-reference/bin/teddycloud'),('Go',workspace/os.environ.get('GOTEDDYCLOUD_BINARY','bin/goteddycloud'))]:
   root=base/name;subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(root)],cwd=workspace,check=True,capture_output=True)
   http_port,web_port,box_port=freeport(),freeport(),freeport()
   config=root/'config/config.ini';config.write_text(config.read_text()+f'core.server.http_port={http_port}\ncore.server.https_web_port={web_port}\ncore.server.https_api_port={box_port}\nencode.ffmpeg_sweep_startup_buffer=false\nencode.ffmpeg_stream_buffer_ms=0\nencode.stream_max_size=1048575\n')
   metadata=root/'data/content/default/11223344/55667788.json';metadata.parent.mkdir(parents=True,exist_ok=True);metadata.write_text(json.dumps({'source':f'http://127.0.0.1:{radio.server_port}/synthetic.wav','nocloud':True}))
   log=open(workspace/'.cache'/f'live-{name}-server.log','w');process=subprocess.Popen([str(binary),'--base_path',str(root)+'/'],cwd=workspace,stdout=log,stderr=log,start_new_session=True)
   connection=None
   try:
    for _ in range(100):
     try:
      probe=http.client.HTTPConnection('127.0.0.1',http_port,timeout=1);probe.request('GET','/v1/time');probe.getresponse().read();probe.close();break
     except OSError:time.sleep(.1)
    else:raise AssertionError((name,'readiness'))
    connection=http.client.HTTPSConnection('127.0.0.1',box_port,context=client_context(root),timeout=20)
    connection.request('GET','/v1/content/1122334455667788');response=connection.getresponse();data=response.read(8192)
    assert response.status==200 and len(data)==8192,(name,response.status,len(data))
    fields=taf_header(data[:4096]);assert b'OggS'==data[4096:4100],(name,'missing live Ogg headers')
    assert fields[2]==1048575-4096,(name,'live payload length',fields[2])
    results.append((response.status,response.getheader('Content-Type'),response.getheader('Content-Length')))
   finally:
    if connection:connection.close()
    os.killpg(process.pid,signal.SIGTERM)
    try:process.wait(timeout=8)
    except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
    with __import__('contextlib').suppress(ProcessLookupError):os.killpg(process.pid,signal.SIGKILL)
    log.close()
  assert results[0]==results[1],results
  print('Live differential: status, MIME, advertised size, provisional TAF payload length and Ogg header structure match C')
finally:radio.shutdown();radio.server_close();thread.join()
