#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Disposable CLI checks; generated identities and firmware are synthetic only."""
import hashlib, http.server, pathlib, subprocess, tempfile, threading, sys, json, shutil
workspace=pathlib.Path(__file__).resolve().parents[1]
binary=workspace/'bin/goteddycloud'
checks=0
def run(*args,ok=True):
 global checks
 result=subprocess.run([str(binary),*map(str,args)],cwd=workspace,capture_output=True,timeout=180)
 if (result.returncode==0)!=ok:raise AssertionError((args,result.returncode,result.stdout.decode(),result.stderr.decode()))
 checks+=1
 return result.stdout.decode()
with tempfile.TemporaryDirectory(prefix='goteddycloud-cli-') as folder:
 base=pathlib.Path(folder)
 deployment=base/'deployment'
 assert 'goteddycloud' in run('--version')
 help_text=subprocess.run([str(binary),'--help'],capture_output=True,check=True).stderr.decode()
 for flag in ['base_path','httpsecure','source','destination','generate-server-certs','generate-client-cert','generate-client-cert-tb2','add-to-settings','encode','encode_test','skip-seconds','esp32-hostpatch','esp32-portpatch','port','oldrtnlhost','oldapihost','esp32-fixup','esp32-inject','esp32-extract','docker-test','url-test','cloud-test','hash','hostname','config-set','help']:assert '-'+flag in help_text,flag
 checks+=1
 run('-b',deployment,'-Ccloud.enabled=false,mqtt.enabled=false')
 run('-b',deployment,'-g')
 destination=base/'identity';destination.mkdir()
 run('-b',deployment,'-c001122334455','-d',destination)
 before={p.name:p.read_bytes() for p in destination.iterdir()}
 run('-b',deployment,'--generate-client-cert=001122334455','--destination',destination)
 assert before=={p.name:p.read_bytes() for p in destination.iterdir()}
 run('-b',deployment,'--generate-client-cert-tb2=aabbccddeeff','--add-to-settings')
 assert 'AABBCCDDEEFF' in (deployment/'config/config.overlay.ini').read_text()
 run('-b',deployment,'-D','-http','127.0.0.1:0','-httpsecure','127.0.0.1:0','-box','127.0.0.1:0')
 run('-b',deployment,'-D','-http','127.0.0.1:0','-httpsecure','off','-box','off')
 input_file=base/'hash.txt';input_file.write_text('synthetic hash test')
 assert run('-H',input_file).strip()==hashlib.sha1(input_file.read_bytes()).hexdigest().upper()
 run('-E',base/'unused.taf','-Ccloud.enabled=false',ok=False)
 run('-b',deployment,'-D','-H',input_file,ok=False)
 class Handler(http.server.BaseHTTPRequestHandler):
  def do_GET(self):
   assert self.headers.get('Authorization')=='BD '+('61'*32).upper()
   self.send_response(200);self.end_headers();self.wfile.write(b'synthetic response')
  def log_message(self,*args):pass
 server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
 thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
 run('-b',deployment,'-Ccore.allowPrivateFetch=true')
 try:assert 'HTTP 200; 18 bytes' in run('-b',deployment,'-U',f'http://127.0.0.1:{server.server_port}/test','-H','a'*32)
 finally:server.shutdown();server.server_close();thread.join()
 fixture=base/'fixture'
 subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(fixture)],cwd=workspace,check=True,capture_output=True)
 subprocess.run(['python3','scripts/firmware-fixture.py',str(fixture)],cwd=workspace,check=True,capture_output=True)
 image=fixture/'data/firmware/firmware_001122334455.bin'
 original=image.read_bytes();extracted=base/'extracted';extracted.mkdir()
 run('-b',deployment,'-X',image,'-d',extracted)
 assert image.read_bytes()==original and len(list(extracted.iterdir()))==3
 run('-b',deployment,'-I',image,'-s',extracted)
 run('-b',deployment,'-P',image,'-h','local.box')
 assert b'local.box\0' in image.read_bytes() and b'prod.de.tbs.toys\0' not in image.read_bytes()
 preserved=image.read_bytes()
 run('-b',deployment,'-P',image,'-h','too-long-hostname-for-slot.example',ok=False)
 assert image.read_bytes()==preserved
 damaged=bytearray(preserved);damaged[32]^=1;image.write_bytes(damaged)
 run('-b',deployment,'-F',image)
 subprocess.run(['go','run','-buildvcs=false','./cmd/firmwareprobe',str(image)],cwd=workspace,check=True,capture_output=True)
 # Exercise the C-sized synthetic waveform (100 blocks, 99 chapters).
 encoded=base/'encode-test.taf'
 run('-E',encoded)
 assert encoded.stat().st_size>4096
 # Reuse the TAF upload/validation probe rather than trusting process success.
 probe=subprocess.run(['go','run','-buildvcs=false','./cmd/tafprobe',str(encoded)],cwd=workspace,capture_output=True)
 if probe.returncode:raise AssertionError(probe.stderr.decode())
 metadata=json.loads(probe.stdout);assert metadata['audio_id']==0xdeafbeef and len(metadata['tracks'])==99
 print(probe.stdout.decode().strip())
print(f'CLI smoke: {checks} checks passed')
