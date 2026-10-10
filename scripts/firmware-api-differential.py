#!/usr/bin/env python3
"""Compare synthetic certificate and ESP32 workflows, preserving source images."""
import base64,hashlib,http.client,json,subprocess
from pathlib import Path
ports=[18080,18081];roots=[Path('.cache/oracle-runtime').resolve(),Path('.cache/ci-candidate-runtime').resolve()]
for root in roots:assert (root/'.synthetic-fixture').is_file()
def req(port,method,path,data=None,kind=None):
 c=http.client.HTTPConnection('127.0.0.1',port,timeout=30);c.request(method,path,data,{'Content-Type':kind} if kind else {})
 r=c.getresponse();result=(r.status,r.getheader('Content-Type'),r.read());c.close();return result
def check(name,method,path,data=None,kind=None):
 results=[req(port,method,path,data,kind) for port in ports];assert results[0]==results[1],(name,results);assert results[0][0]==200,(name,results);print('PASS firmware API '+name,flush=True);return results[0]
def multipart(name,data):
 boundary='synthetic-firmware-fixture';body=('--'+boundary+'\r\nContent-Disposition: form-data; name="file"; filename="'+name+'"\r\n\r\n').encode()+data+('\r\n--'+boundary+'--\r\n').encode();return body,'multipart/form-data; boundary='+boundary
check('CA DER','GET','/api/getFile/ca.der');check('CA DER alias','GET','/api/getFile/c2.der')
certificate=(roots[0]/'certs/client/001122334455/ca.der').read_bytes();body,kind=multipart('ca.der',certificate)
check('certificate upload','POST','/api/uploadCert',body,kind)
for root in roots:assert (root/'certs/client/ca.der').read_bytes()==certificate
for name,source in [('client.der','teddy-cert.pem'),('private.der','teddy-key.pem')]:
 raw=(roots[0]/'certs/client/001122334455'/source).read_text()
 material=base64.b64decode(''.join(line for line in raw.splitlines() if not line.startswith('-----')))
 body,kind=multipart(name,material);check('upload '+name,'POST','/api/uploadCert',body,kind)
for root in roots:subprocess.run(['python3','scripts/firmware-fixture.py',str(root)],check=True)
original=(roots[0]/'data/firmware/firmware_001122334455.bin').read_bytes();before=hashlib.sha256(original).hexdigest()
body,kind=multipart('upload_001122334455',original)
for suffix in ['', '_1']:
 results=[req(port,'POST','/api/esp32/uploadFirmware',body,kind) for port in ports]
 expected=('upload_001122334455'+suffix+'.bin').encode()
 # C canonicalizes a double separator after selecting the output, then slices
 # using the pre-canonical root length. Its response loses the first character.
 assert results[0]==(200,'text/plain; charset=utf-8',expected[1:]),results
 assert results[1]==(200,'text/plain; charset=utf-8',expected),results
 for root in roots:assert (root/'data/firmware'/expected.decode()).read_bytes()==original
 print('PASS firmware API upload suffix '+(suffix or 'initial')+' (documented C filename truncation)',flush=True)
check('certificate extraction','POST','/api/esp32/extractCerts?filename=firmware_001122334455.bin')
for root in roots:
 expected=json.loads((root/'data/firmware/synthetic-identity.json').read_text())
 for name,digest in expected.items():assert hashlib.sha256((root/'certs/client/001122334455'/name.lower()).read_bytes()).hexdigest()==digest
results=[req(port,'GET','/api/esp32/patchFirmware?filename=firmware_001122334455.bin&hostname=tc.local&wifi_ssid=synthetic-wifi&wifi_pass=synthetic-password') for port in ports]
assert all(s==200 and k=='binary/octet-stream' for s,k,b in results),[(s,k,b[:100]) for s,k,b in results]
# FAT timestamps/allocation may vary; independently validate both images and
# compare extracted identities. Original dump hashes must remain unchanged.
reports=[]
for root,(status,kind,data) in zip(roots,results):
 target=root/'data/firmware/patched_001122334455.bin';assert target.read_bytes()==data
 report=json.loads(subprocess.check_output(['go','run','-buildvcs=false','./cmd/firmwareprobe',str(target)],text=True));reports.append(report)
 assert b'tc.local\0' in data and b'synthetic-wifi' in data and b'synthetic-password' in data
 assert hashlib.sha256((root/'data/firmware/firmware_001122334455.bin').read_bytes()).hexdigest()==before
assert reports[0]==reports[1],reports
print('PASS firmware API patched layout, identities, WiFi and unchanged originals',flush=True)
