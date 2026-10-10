#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Pinned C/Go MQTT replay with an isolated loopback broker and synthetic keys."""
import contextlib, os, http.client, json, pathlib, queue, select, socket, ssl, struct, subprocess, tempfile, threading, time
workspace=pathlib.Path(__file__).resolve().parents[1]
def field(value):
 value=value.encode() if isinstance(value,str) else value
 return struct.pack('>H',len(value))+value
def wire(header,data=b''):
 size=len(data);length=bytearray()
 while True:
  b=size%128;size//=128;length.append(b|(128 if size else 0))
  if not size:return bytes([header])+length+data
def exact(sock,n):
 data=bytearray()
 while len(data)<n:
  part=sock.recv(n-len(data))
  if not part:raise EOFError()
  data.extend(part)
 return bytes(data)
def packet(sock):
 header=exact(sock,1)[0];size=0;mult=1
 for i in range(4):
  b=exact(sock,1)[0];size+=(b&127)*mult
  if not b&128:break
  mult*=128
 else:raise AssertionError('malformed MQTT length')
 assert size<=65536
 return header,exact(sock,size)
def freeport():
 with socket.socket() as sock:sock.bind(('127.0.0.1',0));return sock.getsockname()[1]
class Broker:
 def __init__(self):
  self.socket=socket.socket();self.socket.bind(('127.0.0.1',0));self.socket.listen();self.port=self.socket.getsockname()[1]
  self.messages={};self.subscriptions=set();self.commands=queue.Queue();self.stopped=threading.Event();self.errors=[];self.connected=threading.Event()
  self.pending=set();self.last_packet=time.monotonic()
  self.thread=threading.Thread(target=self.run,daemon=True);self.thread.start()
 def run(self):
  try:
   self.socket.settimeout(15);conn,_=self.socket.accept()
   with conn:
    conn.settimeout(5);h,data=packet(conn);assert h==0x10;conn.sendall(wire(0x20,b'\0\0'));self.connected.set()
    while not self.stopped.is_set():
     while not self.commands.empty():
      topic,payload=self.commands.get_nowait();conn.sendall(wire(0x30,field(topic)+payload.encode()))
     if not select.select([conn],[],[],.15)[0]:continue
     h,data=packet(conn);self.last_packet=time.monotonic()
     if h>>4==8:
      position=2;codes=[]
      while position<len(data):
       size=int.from_bytes(data[position:position+2]);position+=2;topic=data[position:position+size].decode();position+=size+1;self.subscriptions.add(topic);codes.append(2)
      conn.sendall(wire(0x90,data[:2]+bytes(codes)))
     elif h>>4==3:
      size=int.from_bytes(data[:2]);topic=data[2:2+size].decode();body=data[2+size:];qos=(h>>1)&3
      if qos:
       identifier=body[:2];body=body[2:];conn.sendall(wire(0x50 if qos==2 else 0x40,identifier))
       if qos==2:self.pending.add(identifier)
      self.messages[topic]=body.decode()
     elif h>>4==6:
      conn.sendall(wire(0x70,data));self.pending.discard(data)
     elif h==0xc0:conn.sendall(wire(0xd0))
     elif h==0xe0:return
     else:raise AssertionError((h,data))
  except EOFError:pass
  except Exception as error:
   if not self.stopped.is_set():self.errors.append(repr(error))
 def wait(self,predicate):
  deadline=time.monotonic()+15
  while time.monotonic()<deadline:
   if self.errors:raise AssertionError(self.errors)
   if predicate():return
   time.sleep(.05)
  raise AssertionError(('MQTT timeout',len(self.messages),list(self.messages)[-10:],self.errors))
 def close(self):self.stopped.set();self.socket.close();self.thread.join(timeout=2)
 def idle(self):
  # The pinned C client holds its TX mutex during synchronous QoS publishes.
  # Sending a command inside that burst can deadlock its callback on the same
  # mutex. Finish all handshakes and observe quiescence before command replay.
  self.wait(lambda:not self.pending and time.monotonic()-self.last_packet>1.0)
def request(port,path,body=None):
 conn=http.client.HTTPConnection('127.0.0.1',port,timeout=5);conn.request('GET' if body is None else 'POST',path,body);res=conn.getresponse();data=res.read();conn.close();assert res.status==200,(path,res.status,data);return data
with tempfile.TemporaryDirectory(prefix='goteddycloud-mqtt-') as folder:
 base=pathlib.Path(folder);identity=base/'identity';identity.mkdir();identity_base=base/'signer'
 subprocess.run([str(workspace/'bin/goteddycloud'),'-b',str(identity_base),'-c001122334455','-d',str(identity)],check=True,capture_output=True)
 for source,destination,kind in [('client.der','client.pem','x509'),('private.der','private.pem','pkey')]:subprocess.run(['openssl',kind,'-inform','DER','-in',str(identity/source),'-out',str(identity/destination)],check=True,capture_output=True)
 captures=[];desired_captures=[]
 for name,binary in [('C',workspace/'.cache/teddycloud-develop-reference/bin/teddycloud'),('Go',workspace/'bin/goteddycloud')]:
  root=base/name;subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(root)],cwd=workspace,check=True,capture_output=True)
  http_port,web_port,box_port,mqtt_port=freeport(),freeport(),freeport(),freeport();broker=Broker()
  options={'log.level':'5','mqtt.enabled':'false','mqtt.hostname':'127.0.0.1','mqtt.port':str(broker.port),'mqtt.identification':'synthetic-oracle','mqtt.topic':'synthetic','mqtt.qosLevel':os.environ.get('MQTT_TEST_QOS','2'),'mqtt.tls_enabled':'false','mqtt_server.enabled':'true','mqtt_server.port':str(mqtt_port),'mqtt_server.cert.crt':'certs/server_tb2/teddy-cert.pem','mqtt_server.cert.key':'certs/server_tb2/teddy-key.pem','core.server.bind_ip':'127.0.0.1','core.server.http_port':str(http_port),'core.server.https_web_port':str(web_port),'core.server.https_api_port':str(box_port),'core.host_url':'http://synthetic.invalid'}
  config=root/'config/config.ini';config.write_text(config.read_text()+''.join(f'{key}={value}\n' for key,value in options.items()))
  log=open(base/(name+'.log'),'w');process=subprocess.Popen([str(binary),'--base_path',str(root)+'/'],cwd=workspace,stdout=log,stderr=log)
  try:
   deadline=time.monotonic()+15
   while True:
    try:request(http_port,'/v1/time' if name=='C' else '/api/time');break
    except OSError:
     if process.poll() is not None or time.monotonic()>deadline:raise AssertionError((name,'startup', (base/(name+'.log')).read_text()[-1500:]))
     time.sleep(.1)
   embedded_context=ssl.create_default_context(cafile=str(root/'certs/server_tb2/teddy-cert.pem'))
   with socket.create_connection(('127.0.0.1',mqtt_port),timeout=5) as raw:
    with embedded_context.wrap_socket(raw,server_hostname='127.0.0.1') as device:
     device.sendall(wire(0x10,field('MQTT')+bytes([4,2,0,30])+field('synthetic-device')))
     assert packet(device)==(0x20,b'\0\0')
     device.sendall(wire(0x82,b'\0\1'+field('toniebox/001122334455/settings/desired')+b'\0'))
     assert packet(device)==(0x90,b'\0\1\0')
     device.sendall(wire(0x30,field('toniebox/001122334455/settings/request')+b'{}'))
     header,data=packet(device);assert header==0x30
     size=int.from_bytes(data[:2]);assert data[2:2+size].decode()=='toniebox/001122334455/settings/desired'
     desired_captures.append(json.loads(data[2+size:]))
     device.sendall(wire(0xc0));assert packet(device)==(0xd0,b'')
     device.sendall(wire(0xe0))

   # Register while the external MQTT client is disabled. The pinned C
   # client can deadlock when registration subscribes during its connect loop.
   # A recognized name-based identity routes the synthetic RTNL frame to its overlay.
   context=ssl.create_default_context(cafile=str(root/'certs/server/teddy-cert.pem'));context.maximum_version=ssl.TLSVersion.TLSv1_2;context.load_cert_chain(str(identity/'client.pem'),str(identity/'private.pem'))
   payload=struct.pack('<Iii',0,-40,2)
   log2=b'\x09'+struct.pack('<Q',1234)+b'\x10\x01\x18\x00\x20\x1b\x28\xe0\x43\x32'+bytes([len(payload)])+payload+b'\x4a\x01\x0a'
   rpc=b'\x12'+bytes([len(log2)])+log2
   with socket.create_connection(('127.0.0.1',box_port),timeout=5) as raw:
    with context.wrap_socket(raw,server_hostname='127.0.0.1') as tls:tls.sendall(struct.pack('>I',len(rpc))+rpc);time.sleep(2)
   request(http_port,'/api/settings/set/mqtt.enabled',b'true')
   volume='synthetic/box/001122334455/VolumeLevel';broker.wait(lambda:broker.messages.get(volume)=='2')
   broker.wait(lambda:'homeassistant/number/synthetic_Box_001122334455/CmdVolLimitSpk/config' in broker.messages)
   broker.wait(lambda:'synthetic/debug-web-pcm_encode_console_url/status' in broker.messages)
   broker.idle()
   broker.commands.put(('synthetic/box/001122334455/CmdVolLimitSpk/set','2'))
   broker.wait(lambda:broker.messages.get('synthetic/box/001122334455/CmdVolLimitSpk')=='2')
   assert request(http_port,'/api/settings/get/toniebox.max_vol_spk?overlay=001122334455')==b'2'
   broker.idle()
   broker.commands.put(('synthetic/box/001122334455/CmdVolLimitSpk/set','5'));time.sleep(.3)
   assert request(http_port,'/api/settings/get/toniebox.max_vol_spk?overlay=001122334455')==b'2'
   # Wait for the initial server entity burst to complete.
   time.sleep(1)
   captures.append(dict(broker.messages))
  finally:
   (workspace/'.cache'/('mqtt-'+name+'-server.log')).write_text((base/(name+'.log')).read_text())
   (workspace/'.cache'/('mqtt-'+name+'-capture.json')).write_text(json.dumps(broker.messages,indent=2))
   process.terminate()
   try:process.wait(timeout=10)
   except subprocess.TimeoutExpired:process.kill();process.wait()
   broker.close();log.close()
 assert desired_captures[0]==desired_captures[1],'embedded desired settings differ'
 reference,candidate=captures
 def discovery(messages):
  result={}
  for topic,value in messages.items():
   if not topic.startswith('homeassistant/'):continue
   item=json.loads(value);item['dev']['sw']='<build>';item['dev']['hw']='<platform>';result[topic]=item
  return result
 a,b=discovery(reference),discovery(candidate)
 missing=set(a)-set(b);extra=set(b)-set(a);assert not missing and not extra,('discovery entity set',missing,extra)
 # These two owned descriptions document the management/device listener split.
 # Assert both exact spellings before normalizing only the display name; all
 # topic, identity, command, state and numeric-range fields remain exact.
 for key,scheme in [('core.server.http_port','HTTP'),('core.server.https_web_port','HTTPS')]:
  topic='homeassistant/number/teddyCloud_Server/'+key.replace('.','-')+'/config'
  assert a[topic]['name']==f'{key} - {scheme} port for the webinterface'
  assert b[topic]['name']==f'{key} - {scheme} port for Web UI and management API'
  b[topic]['name']=a[topic]['name']
 mismatches={key:[a[key],b[key]] for key in a if a[key]!=b[key]}
 assert not mismatches,json.dumps(mismatches,indent=2)
 for topic in ['synthetic/box/001122334455/VolumeLevel','synthetic/box/001122334455/VolumedB','synthetic/box/001122334455/CmdVolLimitSpk','synthetic/status']:
  assert reference[topic]==candidate[topic],(topic,reference.get(topic),candidate.get(topic))
 print(f'MQTT differential: {len(a)} discovery documents and 4 state topics match with two asserted management-port labels; valid/invalid commands and embedded TLS/settings/ping verified on both servers')
