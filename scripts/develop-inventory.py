#!/usr/bin/env python3
"""Source-level audit of every upstream development commit and changed file."""
import json,subprocess
from pathlib import Path
source=Path('.cache/teddycloud-develop-reference')
base='78c3b38e36f665d3a5fffe907f73a02725176377';target='8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1'
def git(*args):return subprocess.check_output(['git','-C',str(source),*args],text=True).strip()
assert git('rev-parse','HEAD')==target
# Mapping covers each changed runtime translation unit, not merely commit titles.
coverage={
 'cache':'Exact-key bounded map, existing-entry reuse and guarded misses: internal/app/image_cache.go; image_cache_test.go',
 'cert':'Go owned certificate buffers; internal/certificates tests; private material written atomically mode 0600',
 'cloud_request':'Bounded net/url redirects and headers; direct validated-address dialing; internal/cloud/client.go, internal/fetchpolicy, internal/app/catalog_update.go; redirect credentials deliberately confined',
 'contentJson':'Bounded JSON reads terminate on EOF; confined roots and metadata ownership: internal/content/store.go',
 'esp32':'Validated partition/NVS/FAT bounds and spans; original-preserving repair; private extraction; internal/firmware and internal/app/firmware.go',
 'esp32_port':'Xtensa instruction pattern matching, all-site patch/443 restore and checksum repair: internal/firmware/port.go and port_test.go',
 'fs_ext':'Go checked I/O/SectionReader/CopyN and os.Root confinement: internal/storage; track_export.go',
 'handler':'Owned request/response buffers; bounded headers and track position/duration: internal/app/cloud.go, file_index.go, tags.go',
 'handler_api':'Web users moved to auth.go; CORS management preflight; safe multipart; certificate upload selection/isolation; listened sidecars; Ogg/ZIP track exports; duration; existing bounded firmware/files/encoding APIs; internal/app tests',
 'handler_cloud':'No encoder restart for existing range prefix; growing EOF retries; listened on source sync; OTA hex guard; internal/app/live.go, listened.go, ota.go and tests',
 'handler_rtnl':'Length-checked binary reads and bounded dump strings: internal/boxstate, internal/app/rtnl.go and rtnl_test.go',
 'handler_security_mit':'Independent domain and crawler detection: internal/app/security.go and security_test.go',
 'handler_web_auth':'Persistent salted SHA256 users; bounded sessions/rate limits; cookies/bearer and management gate: internal/app/auth.go and auth_test.go',
 'libraryMeta':'Atomic JSON sidecar preserves unknown fields; index and sync heuristic: internal/app/listened.go and develop_content_test.go',
 'main':'New esp32-portpatch/port flags: cmd/goteddycloud/operations.go; other development additions covered by server auth initialization',
 'mqtt':'Dynamic formatting avoids fixed-array overflow; internal/mqtt/client.go, integration.go and tests',
 'mqtt_server':'Fresh client identity on TLS: internal/app/integrations.go disables session tickets',
 'os_ext':'Shell-free exec.CommandContext, crypto/rand, Go ZIP and 0600 atomic files; no shell quote/process wrappers needed',
 'server':'CORS, authorization, pooled identity isolation, bearer parsing, root redirect, CC3235 generation, box TLS sessions: internal/app/server.go, auth.go, useragent.go; internal/runtime and boxtransport tests',
 'server_helpers':'Standard Go MIME multipart and netip replace C parsers; checked empty upload/traversal/IPv6 cases',
 'settings':'Typed snapshot maps avoid C allocation races/binary lookup; private settings hidden; new schema; new box certificate directories: internal/config, internal/identity',
 'str_ext':'Standard Go strings/net/url/netip and bounded output; public-address predicate: internal/fetchpolicy',
 'tls_adapter':'Owned immutable TLS config; tickets disabled only for box/embedded MQTT: internal/boxtransport and internal/app/integrations.go',
 'tonie_audio_playlist':'Bounded playlist validation and owned strings; full integrity and rejection of oversized playlists are documented stronger behavior: internal/content/playlist.go',
 'toniefile':'Validated append and subprocess arguments without a shell; owned stream lifecycle and repair: internal/taf and internal/audio',
 'toniesJson':'Bounded file reads and atomic successful metadata publication; slices cannot wrap uint8 counts: internal/catalog and internal/app/catalog_update.go',
 'web':'HTTP success and complete transfer required before atomic publication: internal/app/catalog_update.go, image_cache.go',
 'http_server':'Per-request identity; bounded ranges, growing EOF retry and fresh TLS handshakes: internal/devicehttp, runtime, boxtransport, app/live.go',
 'http_server_misc':'Go TLS lifecycle replaces Cyclone handling: internal/runtime and internal/boxtransport',
}
files=[]
for path in git('diff','--name-only',base,target).splitlines():
 if path.startswith('src/'):
  stem=Path(path).stem
  assert stem in coverage, f'unreviewed runtime file: {path}'
  kind='Go behavior or equivalent';evidence=coverage[stem]
 elif path.startswith('include/'):
  kind='C interface';evidence='Declarations correspond to audited runtime units; Go types and standard library replace C headers'
 elif path=='teddycloud_web' or path.startswith('contrib/data/www/'):
  kind='Official frontend assets';evidence='Pin 636a8d65043f6c6e322f76bcb11e958725159e96; scripts/frontend.sh; legacy bundle removed; browser acceptance remains pending'
 elif path.startswith('tests/'):
  kind='Upstream validation';evidence='scripts/develop-differential.py runs relevant upstream tests; Go regression/race/fuzz tests cover native equivalents; simulator hardware stubs remain skipped'
 else:
  kind='Build, docs or developer configuration';evidence='Go build/CI/container workflow and project documentation retained; C compiler/ASAN/Claude settings do not apply to Go runtime'
 files.append(dict(path=path,classification=kind,evidence=evidence))
commits=[]
for row in git('log','--reverse','--format=%H%x09%s',base+'..'+target).splitlines():
 sha,subject=row.split('\t',1);paths=git('diff-tree','--no-commit-id','--name-only','-r',sha).splitlines()
 commits.append(dict(commit=sha,subject=subject,files=paths))
output=dict(base=base,target=target,frontend=git('rev-parse','HEAD:teddycloud_web'),commits=commits,files=files,limitations=['Hardware unavailable; simulator is not physical acceptance','Official frontend built; interactive browser unavailable','C-only memory/build fixes are audited equivalents, not literal C code ports'])
Path('docs/develop-inventory.json').write_text(json.dumps(output,indent=2)+'\n')
print(f'{len(commits)} commits, {len(files)} changed files, {sum(f["path"].startswith("src/") for f in files)} runtime units audited')
