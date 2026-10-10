#!/usr/bin/env python3
"""Reference management listing, assignment and safe trigger contracts."""
import http.client,json,ssl
from pathlib import Path
from urllib.parse import urlencode
ports=[18080,18081]
roots=[Path('.cache/oracle-runtime'),Path('.cache/ci-candidate-runtime')]
for root in roots: assert (root/'.synthetic-fixture').is_file()
def req(port,method,path,data=None):
 c=http.client.HTTPSConnection('127.0.0.1',19444,context=ssl.create_default_context(cafile=str(roots[1]/'certs/server/teddy-cert.pem')),timeout=30) if port==18081 and path.startswith('/v1/') else http.client.HTTPConnection('127.0.0.1',port,timeout=30)
 c.request(method,path,data)
 r=c.getresponse();result=(r.status,r.getheader('Content-Type'),r.read());c.close();return result
def check(name,method,path,data=None,as_json=False,status=200):
 results=[req(port,method,path,data) for port in ports]
 if as_json:results=[(s,k,json.loads(b)) for s,k,b in results]
 assert results[0]==results[1],(name,results)
 assert results[0][0]==status,(name,results)
 print('PASS tags API '+name,flush=True)
check('box list','GET','/api/getBoxes',as_json=True)
check('tag info','GET','/api/getTagInfo?ruid=0500000005000000',as_json=True)
check('missing tag','GET','/api/getTagInfo?ruid=0600000006000000',status=404)
check('custom box metadata','GET','/api/tonieboxesCustomJson')
for root in roots:
 (root/'config/tonieboxes.json').write_text(json.dumps([{'id':'cc3200','name':'Synthetic','img_src':'https://example.invalid/box.png','crop':[2,'bad',4,8],'unknown':'ignored'}]))
check('box metadata crop normalization','GET','/api/tonieboxesJson',as_json=True)
check('unknown trigger','GET','/api/triggerUnknown')
check('write config trigger','GET','/api/triggerWriteConfig')
check('reload config trigger','GET','/api/triggerReloadConfig')
check('assignment queued','POST','/api/assignUnknown?'+urlencode({'special':'library','path':'synthetic.taf'}))
check('assignment downloaded','GET','/v1/content/0600000006000000')
for root in roots:
 assert (root/'data/content/default/06000000/06000000').read_bytes()==(root/'data/library/synthetic.taf').read_bytes()
check('assigned tag info','GET','/api/getTagInfo?ruid=0600000006000000',as_json=True)

results=[req(port,'GET','/api/getTagIndex') for port in ports]
assert all(s==200 and k=='text/json' for s,k,b in results),results
rows=[sorted(json.loads(b)['tags'],key=lambda r:r['ruid']) for s,k,b in results]
assert rows[0]==rows[1],rows
assert any(row['ruid']=='0600000006000000' for row in rows[0]),rows
print('PASS tags API tag index',flush=True)
