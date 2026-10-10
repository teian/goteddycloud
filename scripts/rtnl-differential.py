#!/usr/bin/env python3
"""Synthetic loopback RTNL replay; requires running C and Go fixture servers."""
import argparse
import http.client
import json
from pathlib import Path
import socket
import ssl
import struct
import time


def varint(value):
    out = bytearray()
    while value > 127:
        out.append((value & 127) | 128)
        value >>= 7
    out.append(value)
    return bytes(out)


def replay(web_port, box_port, root, frame, fragmented=False, event_count=3):
    connection = http.client.HTTPConnection('127.0.0.1', web_port, timeout=20)
    connection.request('GET', '/api/sse')
    stream = connection.getresponse()
    assert stream.status == 200
    context = ssl.create_default_context(cafile=str(root / 'certs/server/teddy-cert.pem'))
    context.maximum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(str(root / 'certs/client/001122334455/teddy-cert.pem'),
                            str(root / 'certs/client/001122334455/teddy-key.pem'))
    events = []
    def event():
        name = None
        data = []
        while True:
            line = stream.readline().decode().rstrip('\r\n')
            if line.startswith('event: '):
                name = line[7:]
            elif line.startswith('data: '):
                data.append(line[6:])
            elif not line:
                assert name is not None, 'incomplete SSE stream'
                return [name, json.loads('\n'.join(data))]
    events.append(event())
    with socket.create_connection(('127.0.0.1', box_port), timeout=5) as raw:
        with context.wrap_socket(raw, server_hostname='127.0.0.1') as tls:
            wire = struct.pack('>I', len(frame)) + frame
            # C uses its HTTP line reader for the first binary message. A trailing
            # LF in optional field9 lets it dispatch the complete frame. Go is
            # independently exercised with one-byte TLS application records.
            if fragmented:
                for byte in wire:
                    tls.sendall(bytes([byte]))
            else:
                tls.sendall(wire)
            for _ in range(event_count):
                events.append(event())
    stream.close()
    connection.close()
    return events


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--reference-http', type=int, default=18080)
    p.add_argument('--reference-box', type=int, default=18444)
    p.add_argument('--candidate-http', type=int, default=18082)
    p.add_argument('--candidate-box', type=int, default=19447)
    p.add_argument('--candidate-root', type=Path, default=Path('.cache/ci-candidate-runtime'))
    p.add_argument('--root', type=Path, default=Path('.cache/oracle-runtime'))
    args = p.parse_args()
    assert (args.root / '.synthetic-fixture').is_file(), 'only generated fixtures are allowed'
    payload = struct.pack('<Iii', 0, -40, 2)
    log = b'\x09' + struct.pack('<Q', 1234) + b'\x10\x01\x18\x00\x20\x1b\x28' + varint(8672) + b'\x32' + varint(len(payload)) + payload + b'\x4a\x01\x0a'
    rpc = b'\x12' + varint(len(log)) + log
    reference = replay(args.reference_http, args.reference_box, args.root, rpc, fragmented=True)
    candidate = replay(args.candidate_http, args.candidate_box, args.root, rpc, fragmented=True)
    assert reference == candidate, 'RTNL SSE names, payloads or sequence differ'
    print('PASS fragmented RTNL volume replay and four decoded SSE events')
    log3 = b'\x0d' + struct.pack('<I', 1700000000) + b'\x10\x03'
    combined = b'\x1a' + varint(len(log3)) + log3 + rpc
    roots = [args.root.resolve(), args.candidate_root.resolve()]
    ports = [args.reference_http, args.candidate_http]
    saved = []
    def setting(port, key, value=None, overlay=""):
        c = http.client.HTTPConnection('127.0.0.1', port, timeout=5)
        method = 'GET' if value is None else 'POST'
        operation = 'get' if value is None else 'set'
        c.request(method, '/api/settings/'+operation+'/'+key+('?overlay='+overlay if overlay else ''), body=value)
        r = c.getresponse()
        assert r.status == 200
        result = r.read().decode()
        c.close()
        return result
    try:
        for port, root in zip(ports, roots):
            assert (root / '.synthetic-fixture').is_file()
            for name in ['rtnl-replay.bin','rtnl-replay.csv']:
                assert not (root / 'config' / name).exists(), 'replay log must be new'
            for key, value in {
                'rtnl.logRawFile': str(root / 'config/rtnl-replay.bin'),
                'rtnl.logHumanFile': str(root / 'config/rtnl-replay.csv'),
                'rtnl.logRaw': 'true', 'rtnl.logHuman': 'true',
            }.items():
                for overlay in ["", "001122334455"]:
                    saved.append((port,key,overlay,setting(port,key,overlay=overlay)))
                    setting(port,key,value,overlay)
        reference = replay(args.reference_http,args.reference_box,args.root,combined,True,5)
        candidate = replay(args.candidate_http,args.candidate_box,args.root,combined,True,5)
        assert reference == candidate, 'combined RTNL event ordering differs'
        for root in roots:
            for attempt in range(100):
                if (root / 'config/rtnl-replay.csv').exists(): break
                time.sleep(.02)
        assert (roots[0] / 'config/rtnl-replay.bin').read_bytes() == (roots[1] / 'config/rtnl-replay.bin').read_bytes(), 'raw log differs'
        csv = [(root / 'config/rtnl-replay.csv').read_bytes() for root in roots]
        # Only the wall-clock prefix of the single CSV record is nondeterministic.
        def normalize(data):
            header, row = data.split(b'\r\n',1)
            timestamp, fields = row.split(b';',1)
            assert timestamp.isdigit()
            return header + b'\r\nTIME;' + fields
        assert normalize(csv[0]) == normalize(csv[1]), 'CSV log differs'
        print('PASS combined RTNL ordering, byte-exact raw frames and normalized CSV logs')
        # State/event-bearing function families from pinned handler_rtnl.c.
        def rpc2(group, function, payload, uptime=2000):
            log = b'\x09' + struct.pack('<Q', uptime) + b'\x10\x01\x18\x00\x20' + varint(group) + b'\x28' + varint(function) + b'\x32' + varint(len(payload)) + payload + b'\x4a\x01\x0a'
            return b'\x12' + varint(len(log)) + log
        cases = []
        for kind in [1,2,3,4,5,6,7,8,11,12,13,99]:
            log = b'\x0d' + struct.pack('<I',1700000000) + b'\x10' + varint(kind)
            frame = b'\x1a' + varint(len(log)) + log + b'\xfa\x01\x01\x0a'
            cases.append(('log3-'+str(kind),frame,3 if kind==11 else 1 if kind==99 else 2))
        for function in [8627,16065,8646,15452,8630,16067]:
            cases.append(('tag-'+str(function),rpc2(15,function,bytes.fromhex('123456789abcdef0')),1 if function in [8630,16067] else 2))
        for group,function in [(20,6803),(20,16096),(22,297)]:
            cases.append(('audio-'+str(function),rpc2(group,function,struct.pack('<I',4000000000)),3))
        for function in [15426,15427]:
            cases.append(('tilt-'+str(function),rpc2(12,function,struct.pack('<i',-42)),2))
        cases.append(('volume-esp32',rpc2(27,15524,struct.pack('<Iii',0,-30,3)),3))
        for group,function,payload in [(8,704,b'synthetic\0'),(8,703,b'full synthetic\0'),(8,7146,b'12345678detail\0'),(6,791,b'eu\0'),(6,1009,b'api.synthetic.invalid\0'),(37,1009,b'rtnl.synthetic.invalid\0'),(20,6480,b'content/00000001/00000001\0'),(22,6480,b'content/00000001/00000001\0'),(255,123,b'unknown')]:
            cases.append(('runtime-'+str(group)+'-'+str(function),rpc2(group,function,payload),2 if group==22 and function==6480 else 1))
        for name,frame,count in cases:
            reference = replay(args.reference_http,args.reference_box,args.root,frame,False,count)
            candidate = replay(args.candidate_http,args.candidate_box,args.root,frame,False,count)
            assert reference == candidate, name+': RTNL event mapping differs: '+repr((reference,candidate))
            print('PASS RTNL '+name,flush=True)
    finally:
        for port,key,overlay,value in reversed(saved): setting(port,key,value,overlay)



if __name__ == '__main__':
    main()
