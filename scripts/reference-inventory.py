#!/usr/bin/env python3
"""Extract source contracts from the pinned C tree; never reads deployment data."""
import argparse
import json
import re
import subprocess
from pathlib import Path

PIN = '8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1'
p = argparse.ArgumentParser()
p.add_argument('--source', type=Path, default=Path('.cache/teddycloud-develop-reference'))
p.add_argument('--check', action='store_true')
a = p.parse_args()
def git(*args):
    return subprocess.check_output(['git', '-C', str(a.source), *args], text=True).strip()
if git('rev-parse', 'HEAD') != PIN or git('status', '--porcelain', '--untracked-files=no'):
    raise SystemExit('reference must be the clean pinned revision')

def calls(text):
    # Split macro arguments without interpreting commas inside strings/expressions.
    for m in re.finditer(r'^\s*(OPTION_\w+)\(', text, re.M):
        pos=m.end(); start=pos; depth=0; quoted=False; escape=False; args=[]
        while pos < len(text):
            c=text[pos]
            if escape: escape=False
            elif quoted and c=='\\': escape=True
            elif c=='"': quoted=not quoted
            elif not quoted:
                if c=='(': depth+=1
                elif c==')':
                    if depth==0:
                        args.append(text[start:pos].strip()); break
                    depth-=1
                elif c==',' and depth==0:
                    args.append(text[start:pos].strip()); start=pos+1
            pos+=1
        yield m.group(1), args, text.count('\n',0,m.start())+1

def milestone(path):
    if path.startswith('/v3') or path.startswith('/v1/ota'): return 3
    if path.startswith('/v') or path=='*binary': return 2
    return 4

sources={}
for f in sorted((a.source/'src').glob('*.c')):
    text=f.read_text()
    for m in re.finditer(r'^\w[\w\s*]*\b(\w+)\([^;{}]*\)\s*\{',text,re.M):
        start=m.end(); depth=1; pos=start
        while depth and pos<len(text):
            depth += (text[pos]=='{')-(text[pos]=='}'); pos+=1
        body=text[start:pos]
        sources[m.group(1)]={'file':str(f.relative_to(a.source)), 'line':text.count('\n',0,m.start())+1,
            'status_literals': sorted(set(re.findall(r'(?:statusCode\s*=\s*|httpSendResponse(?:String|Json)?\([^,]*,\s*)([1-5][0-9]{2})',body))),
            'query_keys':sorted(set(re.findall(r'(?:getQueryParameter|getQueryParameterDecoded)\([^,]*,\s*"([^"]+)"',body))),
            'settings_keys':sorted(set(re.findall(r'(?:settings_\w+)\("([^"]+)"',body)))}
server=(a.source/'src/server.c').read_text()
routes=[]
for m in re.finditer(r'\{REQ_(\w+), "([^"]+)", SERTY_(\w+), &(\w+)\}',server):
    method,path,listener,handler=m.groups()
    routes.append(dict(method=method,path=path,listener=listener,handler=handler,
        dispatch_line=server.count('\n',0,m.start())+1,source=sources.get(handler),
        milestone=milestone(path),status='pending',acceptance='differential response + side effects + listener/method checks'))
settings=[]
for macro,args,line in calls((a.source/'src/settings.c').read_text()):
    if not args or not args[0].startswith('"'): continue
    kind=macro.removeprefix('OPTION_').removeprefix('INTERNAL_')
    key=args[0].strip('"')
    settings.append(dict(key=key,macro=macro,type=kind,default_expression=args[2] if len(args)>2 and kind!='TREE_DESC' else None,
        arguments=args[1:],line=line,milestone=5 if key.startswith(('mqtt','hass')) else 2,status='pending',
        acceptance='default/index + validation + INI roundtrip + overlay isolation'))
cli=[]
main=(a.source/'src/main.c').read_text()
for m in re.finditer(r'\{"([^"]+)", (required_argument|no_argument), 0, ([^}]+)\}',main):
    cli.append(dict(option=m.group(1),argument=m.group(2),short_or_code=m.group(3),line=main.count('\n',0,m.start())+1,
        milestone=3 if m.group(1) in ['encode','encode_test','skip-seconds'] else 4 if m.group(1).startswith(('esp32','generate','old')) else 2,
        status='pending',acceptance='isolated CLI invocation: exit/output/file effects'))
submodules=[]
for line in git('ls-tree','-r','HEAD').splitlines():
    if line.startswith('160000'):
        meta,path=line.split('\t'); submodules.append(dict(path=path,commit=meta.split()[2]))
area_contracts=[
('INI','config/config.ini and config.overlay.ini; flat key=value, overlay.<CN>.<key>; src/settings.c settings_load_ovl/settings_save_ovl','2','roundtrip/defaults/unknown fields'),
('Certificates','configured PEM/DER server TB1/TB2 and client identities; src/cert.c + settings paths','2/4','preserve hashes/one-time generation/import/download'),
('Content','UID path resolution, content.json, .nocloud/.live; src/contentJson.c + src/handler_cloud.c','2/3','resolve fixtures/metadata/cloud policy'),
('Library and playlists','configured librarydir, src/tonie_audio_playlist.c + handler_api.c','3/4','playlist/seek/migration effects'),
('TAF','proto/toniebox.pb.taf-header.proto + src/toniefile.c; payload SHA1/Ogg 4096-byte alignment','1/3','generated multi-track integrity/playback'),
('Tonies JSON','tonies.json, tonies.custom.json, box metadata, content links; src/toniesJson.c','4','CRUD/search/reload/normalization'),
('Custom backups','tonie_json.custom_backup_keep default 10; timestamped backups and linked rename','4','retention/recovery/collision'),
('OTA and cache','core.firmwaredir/core.cachedir generation-specific layouts; src/handler.c + handler_cloud.c + cache.c','3','partial/concurrent downloads'),
('RTNL logs','proto/toniebox.pb.rtnl.proto + src/handler_rtnl.c; configured raw/log/dump output','2','decoded state/log effects'),
('SSE/stats','src/handler_sse.c + src/stats.c','4','event names/payloads/slow consumer'),
('MQTT client/HA','src/mqtt.c + src/home_assistant.c + MQTT_CONTROL.md','5','topics/discovery/5-to-60s reconnect/commands'),
('MQTT server','src/mqtt_server.c; TB2 server certificate defaults, enabled=false','5','connect/topic/listener fixture'),
('Firmware','src/esp32.c; host patch/fixup/inject/extract using copied images','4','checksums/layout/original preservation'),
('Reverse proxies','src/handler_reverse.c; configured generic target mappings/cloud auth','4','path/headers/auth/error comparison'),
('Security/auth','handler_web_auth.c users/sessions + handler_security_mit.c; compare configured behavior','4','sessions/lock/threat fixture'),
('PCAP/TLS diagnostics','src/pcaplog.c + pcap_dump.c + tls_adapter.c; configured logs','4','sanitized outputs/no private fixture inclusion')]
areas=[dict(name=name,source=source,milestone=m,acceptance=acceptance,status='pending') for name,source,m,acceptance in area_contracts]
non_table=[
 dict(name='root redirect',contract='Root/index.shtm always redirects to /web',source='src/server.c:607',milestone=4,status='pending'),
 dict(name='SPA/static',contract='Web SPA fallback, assets, MIME and errors',source='src/server.c:617',milestone=4,status='pending'),
 dict(name='listener isolation',contract='Box listener never serves web static files',source='src/server.c:578',milestone=2,status='pending'),
 dict(name='binary framing',contract='00 00 + big endian nonzero length <=65535 across reads',source='src/cyclone/cyclone_tcp/http/http_server.c:475',milestone=2,status='pending'),
 dict(name='TLS selection',contract='No SNI TB1, any SNI TB2, optional certificates, separate policies',source='src/server.c:767-875',milestone=2,status='pending'),
]
audit_path=Path('docs/compatibility-audit.json')
audit=json.loads(audit_path.read_text()) if audit_path.exists() else {}
for group,rows,key in [('settings',settings,'key'),('cli',cli,'option'),('routing',non_table,'name'),('areas',areas,'name')]:
 recorded=audit.get(group,{})
 if recorded.keys()-{row[key] for row in rows}:raise SystemExit('audit references unknown '+group)
 for row in rows:
  if row[key] in recorded:
   entry=recorded[row[key]]
   if entry['status'] not in ['pending','implemented','verified','documented deviation'] or not entry.get('evidence'):raise SystemExit('invalid audit evidence')
   row.update(entry)

evidence_path=Path('docs/compatibility-evidence.json')
evidence=json.loads(evidence_path.read_text()) if evidence_path.exists() else {}
known={r['method']+' '+r['path'] for r in routes}
if evidence.keys()-known:raise SystemExit('evidence references unknown routes')
for route in routes:
    recorded=evidence.get(route['method']+' '+route['path'])
    if recorded:
        if recorded['status'] not in ['pending','implemented','verified','documented deviation']:raise SystemExit('invalid evidence status')
        route.update(recorded)
data={'reference':PIN,'submodules':submodules,'routes':routes,'settings':settings,'cli':cli,'routing':non_table,'areas':areas}
json_text=json.dumps(data,indent=2)+'\n'
lines=['# Compatibility inventory','',f'Reference: `{PIN}` ([official source](https://github.com/toniebox-reverse-engineering/teddycloud/tree/{PIN})).',
'','Generated with `python3 scripts/reference-inventory.py`. Raw C defaults retain compile-time expressions; these are not evaluated values. Statuses merge the scoped evidence in `docs/compatibility-evidence.json`; implemented does not mean complete route parity. Remaining checks are retained in JSON. `docs/reference-inventory.json` contains complete arguments and source hints.',
'','## Submodules','','| Path | Pinned commit |','| --- | --- |']
for s in submodules: lines.append(f'| `{s["path"]}` | `{s["commit"]}` |')
lines += ['','## Dispatch contracts','','Routes use ordered **prefix** matching, case-insensitive GET/POST method comparison, and listener masks. ANY includes the binary transport dispatch. WEB means web HTTP/HTTPS; BOTH also includes box HTTPS. `src/server.c:328–635` performs dispatch, optional box access enforcement, error mapping and static fallback. Handler source links below define payload/response and mutation details; no route parity is established by this inventory alone.',
'','| Method | Prefix | Listener | Handler source | Milestone / acceptance | Status |','| --- | --- | --- | --- | --- | --- |']
for r in routes:
    s=r['source']; source=f'{s["file"]}:{s["line"]}' if s else 'unresolved'
    lines.append(f'| {r["method"]} | `{r["path"]}` | {r["listener"]} | `{r["handler"]}` ({source}) | M{r["milestone"]}: response/files/listener | {r["status"]} |')
lines += ['','## Non-table routing','','| Contract | Source | Milestone / acceptance | Status |','| --- | --- | --- | --- |']
for r in non_table: lines.append(f'| {r["contract"]} | {r["source"]} | M{r["milestone"]} | {r["status"]} |')
lines += ['','## Settings','','All OPTION entries, including internal state and UI tree nodes, are retained. Complete macro arguments, behavior classifications, evidence and outstanding acceptance are in JSON and [settings audit](settings-audit.md). Implemented means configuration support and the stated consumer; it does not establish hardware or frontend acceptance.','',
'| Key | Type | Default C expression | Source line | Milestone | Status |','| --- | --- | --- | --- | --- | --- |']
for s in settings:
    lines.append(f'| `{s["key"]}` | {s["macro"].removeprefix("OPTION_")} | `{s["default_expression"] or "—"}` | src/settings.c:{s["line"]} | M{s["milestone"]} | {s["status"]} |')
lines += ['','## CLI modes','','The option spelling comes from getopt, including `encode_test` (the usage text differs). Auxiliary flags inherit the invoked operation. Test modes must use a mock cloud/localhost target.',
'','| Option | Argument | Source | Milestone / acceptance | Status |','| --- | --- | --- | --- | --- |']
for c in cli: lines.append(f'| `--{c["option"]}` | {c["argument"]} | src/main.c:{c["line"]} | M{c["milestone"]}: exit/output/files | {c["status"]} |')
lines += ['','## Persisted formats and integrations','','| Area | Contract/source | Milestone / acceptance | Status |','| --- | --- | --- | --- |']

for row in areas: lines.append(f'| {row["name"]} | {row["source"]} | M{row["milestone"]}: {row["acceptance"]} | {row["status"]} |')
lines += ['','## Evidence policy','','See `docs/verification.md` for commands/results. Hardware acceptance stays pending without a recorded physical chip/firmware. No release parity is claimed while pending rows remain. Reference backend C dependencies are oracle-only; the Go backend will not link them. WASM/emsdk are recorded for provenance but excluded from the port.','']
md='\n'.join(lines)
outputs={Path('docs/reference-inventory.json'):json_text,Path('docs/compatibility.md'):md}
for path,text in outputs.items():
    if a.check:
        if not path.exists() or path.read_text()!=text: raise SystemExit(f'{path}: inventory drift; regenerate')
    else: path.parent.mkdir(exist_ok=True,parents=True); path.write_text(text)
print(f'{len(routes)} routes, {len(settings)} settings/tree entries, {len(cli)} CLI options, {len(submodules)} submodules; '+('checked' if a.check else 'written'))
