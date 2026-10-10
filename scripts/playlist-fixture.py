#!/usr/bin/env python3
"""Extend an owned synthetic oracle fixture with a cached TAP playlist."""
import json
from pathlib import Path
import shutil
import struct
import sys

root=Path(sys.argv[1]).resolve()
assert (root/'.synthetic-fixture').is_file()
library=root/'data/library'
data=(library/'synthetic.taf').read_bytes()
length=struct.unpack('>I',data[:4])[0]
assert length<=4092
header=data[4:4+length]
def varint(offset):
    value=0
    for shift in range(0,70,7):
        byte=header[offset];offset+=1
        value|=(byte&127)<<shift
        if byte<128:return value,offset
    raise ValueError('oversized fixture varint')
offset=0;audio_id=None
while offset<len(header):
    key,offset=varint(offset)
    field,wire=key>>3,key&7
    if wire==0:
        value,offset=varint(offset)
        if field==3:audio_id=value
    elif wire==1:offset+=8
    elif wire==2:
        count,offset=varint(offset);offset+=count
    elif wire==5:offset+=4
    else:raise ValueError('unsupported fixture wire type')
assert audio_id is not None
for name in ['tone1.wav','tone2.wav']:shutil.copyfile(root/name,library/name)
playlist={'type':'tap','audio_id':audio_id,'filepath':'lib://synthetic.taf','name':'Synthetic playlist','files':[{'filepath':'lib://tone1.wav','name':'One'},{'filepath':'lib://tone2.wav','name':'Two'}]}
path=library/'synthetic-playlist.tap'
assert not path.exists()
path.write_text(json.dumps(playlist))
folder=root/'data/content/default/03000000';folder.mkdir()
(folder/'03000000.json').write_text(json.dumps({'source':'lib://synthetic-playlist.tap','nocloud':True,'live':False,'tonie_model':'SYNTHETIC_001'}))
