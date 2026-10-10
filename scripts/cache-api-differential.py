#!/usr/bin/env python3
"""Replay image caching against a local synthetic HTTP origin, never public hosts."""
import hashlib,http.client,http.server,json,threading
from pathlib import Path
ports=[18080,18081];roots=[Path('.cache/oracle-runtime').resolve(),Path('.cache/ci-candidate-runtime').resolve()]
for root in roots: assert (root/'.synthetic-fixture').is_file()
class Origin(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  status=200 if self.path=='/ok.png' else 404 if self.path=='/missing.png' else 503
  self.send_response(status);self.end_headers();self.wfile.write(b'synthetic image' if status==200 else b'')
 def log_message(self,*args):pass
origin=http.server.ThreadingHTTPServer(('127.0.0.1',0),Origin)
threading.Thread(target=origin.serve_forever,daemon=True).start()
def req(port,method,path,data=None):
 c=http.client.HTTPConnection('127.0.0.1',port,timeout=30);c.request(method,path,data)
 r=c.getresponse();result=(r.status,r.getheader('Content-Type'),r.getheader('Location'),r.read());c.close();return result
def same(name,method,path,data=None,status=200):
 results=[req(port,method,path,data) for port in ports];results=[(s,k,l,json.loads(b) if k and ("json" in k) else b) for s,k,l,b in results];assert results[0]==results[1],(name,results);assert results[0][0]==status,(name,results);print('PASS cache API '+name,flush=True);return results[0]
try:
 same('enable synthetic private origin','POST','/api/settings/set/core.allowPrivateFetch',b'true')
 same('enable images','POST','/api/settings/set/tonie_json.cache_images',b'true')
 source=f'http://127.0.0.1:{origin.server_port}/ok.png'
 for root in roots:(root/'config/tonieboxes.json').write_text(json.dumps([{'id':'cache-test','name':'Cache test','img_src':source}]))
 same('register image','GET','/api/tonieboxesJson')
 path='/cache/'+hashlib.sha256(source.encode()).hexdigest().upper()+'.png'
 same('download image','GET',path)
 same('cached image','GET',path)
 stats=[json.loads(req(port,'GET','/api/cacheStats')[3]) for port in ports]
 for value,root in zip(stats,roots):value['memory_used']-=len(str(root))
 assert stats[0]==stats[1],stats
 assert stats[0]['total_entries']==2 and stats[0]['total_files']==1 and stats[0]['total_size']==15,stats
 print('PASS cache API statistics with deployment path length normalized',flush=True)
 same('flush','POST','/api/cacheFlush')
 same('refill','GET',path)
 for name,status in [('missing',404),('failure',301)]:
  source=f'http://127.0.0.1:{origin.server_port}/{name}.png'
  for root in roots:(root/'config/tonieboxes.json').write_text(json.dumps([{'id':name,'img_src':source}]))
  same('register '+name,'GET','/api/tonieboxesJson')
  same(name,'GET','/cache/'+hashlib.sha256(source.encode()).hexdigest().upper()+'.png',status=status)
 # The absolute connection counter depends on prior replays. Check schema and
 # the per-request increment, preserving all other values exactly.
 before=[json.loads(req(port,'GET','/api/stats')[3]) for port in ports]
 after=[json.loads(req(port,'GET','/api/stats')[3]) for port in ports]
 for b,a in zip(before,after):assert a['stats'][0]['value']==b['stats'][0]['value']+1
 for value in after:value['stats'][0]['value']=0
 assert after[0]==after[1],after
 print('PASS cache API request counters and stats schema',flush=True)
finally:origin.shutdown();origin.server_close()
