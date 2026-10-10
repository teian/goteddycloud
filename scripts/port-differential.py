#!/usr/bin/env python3
"""Compare the exact new firmware instruction patch and repaired image via both CLIs."""
import hashlib,pathlib,shutil,struct,subprocess,tempfile
workspace=pathlib.Path(__file__).resolve().parents[1]
reference=workspace/'.cache/teddycloud-develop-reference/bin/teddycloud';candidate=workspace/'bin/goteddycloud'
start=[0x36,0x21,0x01,0x72,0x61,0x19,0x62,0x61,0x18,-1,-1,-1,0x20,0x72,0x20,0x61,-1,-1,-1,-1,-1,0xa0,0x2a,0x20]
tail=[-1,-1,-1,0x0c,0x15,0x8a,0x84,0x99,0x21,0x52,0x41,0x34,0x92,0x21,0x16,0x0c,0x15,-1,-1,-1,0xa9,0x11,0xad,0x03]
code=bytearray(0xd2+len(tail))
for offset,pattern in [(0,start),(0xd2,tail)]:
 for i,v in enumerate(pattern):
  if v>=0:code[offset+i]=v
for offset,v in [(0x12,b'\x40\x40\xf4'),(0xd2,b'\x82\xae\x45'),(0xe3,b'\x80\x57\x83')]:code[offset:offset+3]=v
def app(payload):
 footer=(32+len(payload))|15;data=bytearray(footer+33);data[0]=0xe9;data[1]=1;data[23]=1;struct.pack_into('<II',data,24,0x3f400000,len(payload));data[32:32+len(payload)]=payload;checksum=0xef
 for v in payload:checksum^=v
 data[footer]=checksum;data[footer+1:]=hashlib.sha256(data[:footer+1]).digest();return data
with tempfile.TemporaryDirectory(prefix='goteddycloud-port-') as folder:
 base=pathlib.Path(folder);root=base/'fixture';subprocess.run(['go','run','-buildvcs=false','./cmd/fixturegen','-output',str(root)],cwd=workspace,check=True,capture_output=True);subprocess.run(['python3','scripts/firmware-fixture.py',str(root)],cwd=workspace,check=True,capture_output=True)
 image=bytearray((root/'data/firmware/firmware_001122334455.bin').read_bytes());payload=app(code);image[0xa000:0xa000+len(payload)]=payload
 files=[base/'C.bin',base/'Go.bin']
 for file in files:file.write_bytes(image)
 for port in [1,8443,32767,443]:
  for binary,file in zip([reference,candidate],files):
   result=subprocess.run([str(binary),'--base_path',str(root)+'/', '--esp32-portpatch',str(file),'--port',str(port)],cwd=workspace,capture_output=True,timeout=30);assert result.returncode==0,(binary,port,result.stderr.decode(),result.stdout.decode())
  left,right=[file.read_bytes() for file in files]
  assert left[0xa000:0xa000+len(payload)]==right[0xa000:0xa000+len(payload)],('application bytes',port)
  subprocess.run(['go','run','-buildvcs=false','./cmd/firmwareprobe',str(files[1])],cwd=workspace,check=True,capture_output=True)
  print('PASS firmware port differential',port,'application instructions, XOR and SHA256 match C; complete Go image validates',flush=True)
 assert files[1].read_bytes()[0xa020:0xa020+len(code)]==code
 preserved=files[1].read_bytes()
 for args in [[],['--port','0'],['--port','32768']]:
  result=subprocess.run([str(candidate),'--base_path',str(root)+'/', '--esp32-portpatch',str(files[1]),*args],capture_output=True);assert result.returncode!=0 and files[1].read_bytes()==preserved
 print('PASS firmware port CLI rejects missing/invalid ports without changing the image',flush=True)
