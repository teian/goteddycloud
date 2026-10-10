#!/usr/bin/env python3
"""Build a synthetic ESP32 image from marked fixture identities only."""
import base64,hashlib,json,struct,sys
from pathlib import Path
root=Path(sys.argv[1]);assert (root/'.synthetic-fixture').is_file()
def pem(path):return base64.b64decode(''.join(line for line in path.read_text().splitlines() if not line.startswith('-----')))
def crc(data):
 value=0
 for byte in data:
  value^=byte
  for _ in range(8):value=(value>>1)^0xedb88320 if value&1 else value>>1
 return (~value)&0xffffffff
def app(payload):
 footer=(32+len(payload))|15;data=bytearray(footer+33);data[0]=0xe9;data[1]=1;data[23]=1;struct.pack_into('<II',data,24,0x3f400000,len(payload));data[32:32+len(payload)]=payload
 checksum=0xef
 for byte in payload:checksum^=byte
 data[footer]=checksum;data[footer+1:]=hashlib.sha256(data[:footer+1]).digest();return data
image=bytearray(b'\xff'*0x110000)
for offset,payload in [(0,b'synthetic boot\0'),(0xa000,b'rtnl.bxcl.de\0prod.de.tbs.toys\0')]:
 data=app(payload);image[offset:offset+len(data)]=data
for i,(kind,subtype,offset,size,label) in enumerate([(0,0,0xa000,0x2000,b'app'),(1,0x81,0xc000,0x100000,b'assets'),(1,2,0x10c000,0x4000,b'nvs')]):
 struct.pack_into('<HBBII16sI',image,0x9000+i*32,0x50aa,kind,subtype,offset,size,label,0)
logical=bytearray(250*4096);logical[0]=0xeb;struct.pack_into('<H',logical,11,4096);logical[13]=1;struct.pack_into('<H',logical,14,1);logical[16]=1;struct.pack_into('<H',logical,17,128);struct.pack_into('<H',logical,19,250);logical[21]=0xf8;struct.pack_into('<H',logical,22,1);logical[510:512]=b'\x55\xaa';logical[4096:4099]=b'\xf8\xff\xff'
def next_cluster(cluster,value):
 offset=4096+cluster*12//8;old=struct.unpack_from('<H',logical,offset)[0]
 updated=(old&15)|(value<<4) if cluster&1 else (old&0xf000)|value
 struct.pack_into('<H',logical,offset,updated)
def entry(offset,name,cluster,size,attr=0x20):
 stem,_,ext=name.partition('.');logical[offset:offset+11]=(stem.ljust(8)+ext.ljust(3)).encode();logical[offset+11]=attr;struct.pack_into('<H',logical,offset+26,cluster);struct.pack_into('<I',logical,offset+28,size)
entry(8192,'CERT',2,0,16);next_cluster(2,0xfff)
identity=root/'certs/client/001122334455'
materials={'CA.DER':pem(identity/'ca-root.pem'),'CLIENT.DER':pem(identity/'teddy-cert.pem'),'PRIVATE.DER':pem(identity/'teddy-key.pem')}
for i,(name,data) in enumerate(materials.items()):
 cluster=i+3;next_cluster(cluster,0xfff);entry(3*4096+i*32,name,cluster,len(data));offset=(3+cluster-2)*4096;logical[offset:offset+len(data)]=data
partition=bytearray(b'\xff'*0x100000)
for i in range(250):partition[(i+1)*4096:(i+2)*4096]=logical[i*4096:(i+1)*4096]
state=251*4096;partition[state:state+60]=bytes(60);struct.pack_into('<I',partition,state+4,251);struct.pack_into('<I',partition,state+20,4096)
image[0xc000:0x10c000]=partition
# A writable NVS page containing the existing TB_WIFI namespace.
page=0x10c000;struct.pack_into('<II',image,page,0xfffffffe,0)
item=bytearray(b'\xff'*32);item[0:4]=bytes([0,1,1,255]);item[8:24]=b'TB_WIFI'.ljust(16,b'\0');item[24]=1;struct.pack_into('<I',item,4,crc(item[:4]+item[8:]));image[page+64:page+96]=item;image[page+32]=0xfe
(root/'data/firmware/firmware_001122334455.bin').write_bytes(image)
(root/'data/firmware/synthetic-identity.json').write_text(json.dumps({name:hashlib.sha256(data).hexdigest() for name,data in materials.items()}))
print('Synthetic ESP32 image generated',flush=True)
