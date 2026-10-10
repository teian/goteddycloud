# Implementation verification

Initial implementation run, 2026-10-09. This is partial milestone 1 work;
no replacement parity, physical-device support or production readiness is claimed.

- Inventory regeneration check: 69 routes, 267 settings/tree entries, 23 CLI flags,
  nine pinned submodules; `python3 scripts/reference-inventory.py --check` passes.
- C oracle: isolated checkout and eight native/frontend submodules populated at
  pinned revisions. emsdk intentionally excluded from native build. `scripts/reference.sh build`
  succeeds with upstream `NO_SANITIZERS=1 NO_WARN_FAIL=1`; warnings are retained in
  `.cache/reference-build.log`. No tracked C sources were modified.
- C runtime starts against generated fixture data on loopback 18080/18443/18444.
  Missing default upstream-client DER files are logged; cloud is disabled, and
  generated per-box test identities are separate. This is not an upstream-cloud test.
- C encoder: two generated one-second stereo tones produce a 24576-byte TAF;
  reference `/api/fileIndexV2?path=/&special=library` reports `valid=true`,
  payload `size=20480`, and two track positions. IDs/timestamps are intentionally
  generated rather than treated as stable fixture expectations.
- `CGO_ENABLED=0 go build -buildvcs=false ./...` passes. Optional VCS stamping is
  disabled because this workspace has no Git checkout yet; no repository was
  initialized on the user's behalf.
- `go test -buildvcs=false -race ./...` and `go vet -buildvcs=false ./...` pass.
  Checks cover comparison mismatches, file confinement, fixtures, optional client
  certificates/SNI/resumption and binary framing. Race checks use the host Go
  race runtime; the application build remains cgo-free.
- Running the C oracle and Go HTTP prototype simultaneously, `go run -buildvcs=false
  ./cmd/compatcheck` passes offline time and ordered prefix-dispatch cases.
  Only these live API cases are verified; the remaining inventory stays pending.
- The comparator's protobuf normalization operates on decoded wire fields;
  nested/packed byte values remain exact. Schema-aware comparisons and a broader
  fixture suite belong with domain binding implementation.
- TLS default-policy mismatch: SHA1-only synthetic TLS 1.2 negotiation succeeds
  through the C first server flight and fails in default Go. The isolated diagnostic
  `GODEBUG=tlssha1=1` variant succeeds through the same flight. See
  [TLS evidence and policy options](tls-compatibility.md).
- No hardware, cross-platform native runners, live upstream cloud, frontend browser
  flows or full Go conversion pipeline have been validated.

Generated logs, certificates and audio are deliberately ignored under `.cache/`.
The checked-in tools and commands reproduce the synthetic checks without including
private generated keys or recordings. Hardware acceptance tasks stay unchecked.

## Runtime and offline core update

The executable now loads flat configuration and overlays, starts HTTP/web TLS/
box TLS listeners, and supervises the isolated legacy TLS child. Tests verify
bind cleanup, shutdown with incomplete handshakes, private-header spoof rejection,
strict parent TLS settings, certificate preservation, filesystem confinement,
concurrent settings/metadata edits, identity discovery and restart isolation.

`testdata/device-offline.json` has 18 passing live C/Go comparisons: time/prefix,
reset, decoded freshness with a locally updated tag, global and per-box settings,
default settings index, box listing, stored bytes, positive and zero-start ranges,
header skipping, claim and setting mutation/readback/restoration. Reference
settings/set prepares Content-Length 0; Go preserves its empty observable body.
Empty-body freshness hangs in the reference receive loop; Go handles the empty
protobuf request, and rejects malformed/oversized requests with bounded 400/413
responses. Invalid range underflow is rejected with 416; this is an explicit
safety deviation, not parity evidence for malformed ranges.

The production TAF writer groups Opus packets and pads each page to the next
4096-byte boundary. The two-tone fixture now has 32768 payload bytes, chapter
blocks [0,4], and C-reported valid=true with trackSeconds [0,1]. Tests cover Ogg
lacing transitions, hashes, CRCs, bounded FFmpeg admission (two encoders, eight
waiting jobs), cancellation, missing codecs, concurrent files and preservation of
valid output after errors. These results establish structure, not hardware playback.

Reference login/logout/refresh are placeholder endpoints using a fixed public test
token; the pinned implementation does not enforce web management sessions. The
spec now explicitly records that source behavior. RTNL state and bounded SSE
queues are implemented; complete event/logging parity remains under task 3.7/6.11.

User-reported hardware: CC3200. No box is currently available, and firmware is
unknown. Physical acceptance remains pending.

## Acceptance automation and packaging update

The clean-workspace `scripts/oracle-suite.sh` rehearsal passed 18 offline API
cases, eight auth/SSE cases, and the fragmented binary RTNL volume/SSE comparison.
This includes failed-login headers and empty settings/set bodies. The frontend
at the pinned submodule revision builds locally from npm ci and serves through
confined static/SPA routes; full frontend workflows remain pending.

Parser fuzz smoke passed roughly 65,000 confined-path cases, 110,000 TAF header
cases and 109,000 Opus framing cases in two-second bounded runs. Go race tests
and vet passed. The GitHub workflow is authored; no remote run is claimed.

All five versioned backend archives built from a frozen source tree and include
GPL/project notices, the protobuf BSD license and the retained Opus notice.
Linux amd64 passed native startup/TLS/settings/restart/shutdown verification and
migration/rollback rehearsal. Other binary targets are build-only. Linux amd64
and arm64 container images built; amd64 passed codec-free stored delivery and
persistent settings/identity smoke tests. See [packaging](packaging.md).

Cloud client tests now verify configured server roots and client certificates,
request bounds, deadlines/cancellation, header filtering and redacted errors.
Policy tests verify excluded freshness/claim tags and explicit overrides. Full
cloud/content-cache/OTA differential acceptance is still pending.

## Static, packaging and audio follow-up

Eight additional C/Go differential cases pass for the root redirect, SPA root
and nested routes, explicit HTML, legacy HTML, JavaScript/CSS MIME types and
plugin discovery. This found and fixed the root redirect Content-Type header.
Fixtures are authored synthetic assets; the pinned official frontend build is
separate and is not redistributed in backend archives.

The amd64 FFmpeg container passed WAV-to-TAF conversion under UID 65532.
Audio integration tests now cover skipping one second from each source, TAF
input, and atomic append: payload prefix/audio ID preservation, continued Ogg
sequence/CRC, reference chapter-table reset, and retaining the original after
failed appends. CLI --encode/-e and --skip-seconds/-S are available. Playlist
integration and live delivery remain pending under tasks 5.4 and 5.5.

RTNL combined log2/log3 events now follow reference order (raw2, raw3, log3
state, log2 state). Firmware detail has its own field, and content title/picture
events resolve catalog entries and tag model fallbacks. RTNL raw/CSV logging and transient firmware/domain settings are implemented.
A fresh isolated suite passed all 34 HTTP/SSE/static cases and both RTNL
replays, including mixed-event ordering, byte-exact raw frames and CSV fields
with only the wall-clock timestamp normalized. Logging is disabled/restored
after replay. Full function mapping differential and hardware remain pending;
task 3.7 stays open.

## Content cache publication

Cloud content forwarding and its cache service now have race-tested fixtures:
eight concurrent misses produce one upstream request; cached TAFs validate both
hash/length and Ogg structure; truncated refreshes and cancellation retain valid
content; library audio-ID collisions preserve the existing library file and the
new usable UID cache. Cache-to-library uses lib://by/audioID/<id>.taf and retains
unknown content metadata. Waiting for a shared publication lock is cancellable.
Full cloud policy/status/header differential remains pending under 4.2 and 4.8.

Two explicit safety boundaries differ from the C cache path: cache files are
limited to 1 GiB, and source metadata is changed only after validated publication,
so a failed transfer does not remove the last usable source. Partial/ranged or
header-stripped responses cannot replace a full cache. Temporary files are removed
on failed or cancelled publication. These checks do not establish physical playback.

## OTA and v3 follow-up

Seven v1 OTA differential cases pass, including latest timestamp selection,
ignoring .tmp files, positive ranges, current-version responses and disabled local
delivery. Twelve v3 differential cases pass for setup/check-OTA, freshness JSON,
local content metadata, first/later chapter bytes, invalid/missing content, and
hash-based OTA selection. Explicit 404 responses now omit Content-Type like C.
Unit/race checks cover v3 excluded-tag filtering, a range crossing the repeated
chapter header, device-specific folders, transient User-Agent firmware state,
OTA cache downloads without Content-Disposition and interrupted downloads.

OTA caching accepts completed, non-empty bodies with matching Content-Length and
a 64 MiB bound; it does not claim firmware signature validation. Source malformed
path behavior is replaced with bounded 400/404 responses and confined paths.
Cloud failure/status/header differential and live chapter handling remain open.
Certificate identity extraction now uses subject/issuer common names, matching
src/tls_adapter.c, rather than accepting organization fields in a full DN.
See [physical acceptance checklist](hardware-acceptance.md) for pending CC3200 work.

## Custom metadata persistence and rename

The pinned upstream custom JSON API suite passes all five tests against an
isolated Go process: backup rotation, bulk/search, normalization/reload,
validation, and delete/rename. The expanded oracle suite passes 74 HTTP cases
plus fragmented and combined RTNL replay, including 21 custom model cases.
Rename recovery is also checked by killing an owned helper process after its
first linked metadata write, restarting, and verifying the catalog and all
links roll forward together. Collisions, case-only no-ops, opt-out, changed
content roots, and repeated recovery are covered. Unknown metadata survives.
These checks use synthetic data and do not establish physical playback.

## Public metadata update

Metadata search uses the reference custom-first quotas and matching fields.
The update endpoint sends and flushes the reference trigger response before
fetching the fixed HTTPS api.revvox.de source, using system trust, a 30-second
deadline, one admitted request, a 64 MiB bound, and no tag credentials. The
version query identifies goteddycloud rather than the C build revision. Tests
cover failed status, short transfer, malformed JSON, cancellation, busy updates,
raw-byte GET preservation, and active lookup replacement. Failed updates retain
the previous disk and memory state. Invalid manual reload returns 500 and retains
the prior snapshot; this deliberately replaces the C implementation's unconditional
OK and partial/empty reload. This is mock-verified, not a production-source test.

## Reverse proxies

Reverse cloud forwarding uses the configured upstream and normalized BD token,
always issues GET as the reference does, preserves path/query, relays remote
status/headers/body and counts requests. Generic forwarding implements the
three fixed C targets, suffix joining, literal macvendor query suffix, ignored
GitHub query parameters, and target TTL/restart flush behavior. Mock tests cover
cache reuse/expiry, restart flush, failed/short responses, unknown targets, and
confined cache files including an outside-root symlink. Generic requests have
eight admission slots, a 30-second deadline and 64 MiB body bound. Completed
200 bodies publish atomically; incomplete/error responses cannot poison cache.
Intentional deviations: failures return bounded 502, overlong paths return 400
instead of truncating, hop-by-hop headers are stripped, and generic responses
are buffered within the bound. Public targets use system TLS verification.
The outgoing user agent identifies the Go build as teddyCloud/goteddycloud.

## RTNL function-family differential

The expanded isolated replay passes all 33 additional state/event cases across
log3 button/knock/tilt/charger/playback/unknown events, CC3200 and ESP32 tag and
volume IDs, audio-ID groups, tilt angles, firmware fields, domains, and system
sound playback. It found and corrected an audio-play group mismatch: only group
22 triggers system-sound stop, and path components must be exactly eight bytes.
The C strtok slash handling is preserved. Fragmented transport, combined-event
ordering, raw binary logs and normalized CSV comparisons also pass. Malformed
payload bounds/isolation use Go tests rather than feeding unsafe inputs to C.
MQTT publication remains a separate pending milestone. No physical RTNL claim
is made.

## Local TAP playlists and append coordination

Local TAP JSON sources support 1-99 ordered files, symbolic configured-root
paths, explicit audio IDs, and reusable validated TAF output. Inputs are opened
through confined roots and copied to a private directory before FFmpeg. Four
playlist jobs are admitted, aggregate input/output are bounded to 1 GiB, and
publication uses streaming atomic writes. Failed/canceled conversions preserve
previous cache files; invalid IDs trigger regeneration. Go tests cover chapters,
cache reuse, concurrent same-output requests, failure, leading JSON whitespace,
path traversal and outside-root symlinks. Audio conversion now serializes by
destination before claiming an encoder, so concurrent append cannot lose writes.
The append chapter table still resets as in C; a full old chapter table is no
longer incorrectly rejected before that reset.

Cached TAP comparisons require explicit successful status codes as well as
matching bytes/JSON, ranges, and later chapter delivery.
Uncached playlists currently complete conversion before delivery: growing/live
TAP behavior remains task 5.5. The Go output uses the TAP audio_id; pinned C's
tap_generate_taf passes zero to ffmpeg_stream despite comparing the requested ID
when deciding whether to regenerate. The playlist fixture lives inside the configured default content directory;
success expectations prevent equal 404 responses from passing delivery checks.

## Audio and file management APIs

POST fileEncode stages 1-99 confined sources, preserves their order, converts
privately, and refuses an existing destination. PCM uploads accept stereo 48 kHz
signed 16-bit samples and an explicit uint32 audioId (zero by default, as in C).
TAF uploads validate full payload integrity before atomic publication. Four
management audio requests are admitted, total source/upload size is limited to
1 GiB, and request cancellation owns staging/conversion cleanup. Multipart input
is streamed and limited to one file per request. Success statuses, MIME and bodies
for fileEncode/pcmUpload/tafUpload, plus invalid TAF failure, pass against C.
Both C and Go PCM/multi-file outputs pass the Go TAF validator and chapter/audio-ID
checks; codec output bytes and vendor strings are not required to match.

File/directory create/delete and file move use the same confined roots and sorted
resource locks. Moves refuse existing targets; same-filesystem rename is atomic,
and cross-device regular-file fallback publishes a completed private copy before
removing its source. Migration supports the root-library option and by/audioID
layout, validates audio and collisions, publishes the source pointer before
deleting the UID file, and retains unknown metadata. API success/error contracts
pass C comparisons for directory creation/repetition/removal, missing delete/move,
file move/delete and root-library migration. Unit tests cover collisions, two
concurrent moves, escapes, outside-root symlinks, canceled copy publication,
root migration and preserved source files. The fallback primitive is tested
directly; no mounted cross-device directory-move support is claimed.

Intentional safety differences: invalid/canceled uploads preserve previous valid
files; malformed inputs receive bounded 400/413/500 errors; upload filenames reject
separators, and multi-part uploads with more than one file are rejected. Go uses
the requested upload name. File deletion rejects directories; directory rename
reports success rather than C's post-rename file-only existence-check failure.
Cross-device directory moves fail without deleting source. Full inventory audit
and frontend interaction remain pending.

The final combined rehearsal passes 127 checks: 78 HTTP fixture cases, four
audio response comparisons, two output-structure checks, eight file/migration
checks, two RTNL framing/log checks and 33 RTNL function-family comparisons.
The complete Go race suite and go vet pass. Thirty repeated eight-client cache
miss/migration runs pass after synchronizing source-mapping selection with file
opening; this prevents a stale mapping from yielding a transient 404 after UID
removal. The comparison runner now supports required expected status codes so
matching error responses cannot pass successful-playback fixture cases.

## Refreshed development builds (2026-10-09)

The current source builds with CGO disabled for Linux amd64/arm64, Windows amd64,
and macOS amd64/arm64. Artifacts and frozen-source provenance are in
`dist/20261009-current` under version `0.0.0-dev.20261009`. Linux amd64 passes
native listener/TLS/persistence/restart/shutdown and copied-data migration/restore
smokes. The refreshed `goteddycloud:dev-20261009` Linux container passes startup,
codec-free stored TAF delivery, persisted settings/certificate identity, restart
and graceful shutdown as an unprivileged user. Other refreshed targets are
build-only. No remote CI, public release or physical CC3200 result is implied.


### Management content, tags and image cache (2026-10-09)

Tasks 6.4, 6.8 and 6.11 are implemented. The isolated pinned C/Go replay in
`.cache/management-rehearsal.log` passes **162 checks**, including 11 content,
12 tag/box/assignment and 12 image-cache/statistics comparisons. The new scripts
are part of `scripts/oracle-suite.sh` and its existing CI job. Each response is
required to have its intended status; JSON comparisons ignore object key order.
File indices compare all regular-file fields after fixing timestamps and sorting
rows; the deployment-specific parent directory row is excluded. Cache statistics
normalize only the deployment root path length in the C memory estimate. Stats
compare schema and all non-connection values, and independently require that one
request increments the connection counter by one.

Content metadata GET returns stored bytes and SET accepts the C editable field
set, preserving unknown JSON fields. Raw TAF/Ogg, mapped tag downloads, regular
multipart uploads, both file index formats, TAF headers/track times and model
lookups are implemented. File inspection resolves mapped TAF and cached TAP
sources without starting cloud downloads or conversions. Indexes are bounded to
10,000 entries/child inspections; multipart uploads accept at most 99 files and
1 GiB aggregate. Each file publishes atomically. Earlier completed multipart
files remain committed if a later part fails, as with C; failed or cancelled
replacement of a file retains its previous bytes. Root escapes and unsafe raw
multipart filenames are rejected. Empty files return successful empty downloads.
The raw growing/live branch remains task 5.5.

Box/tag APIs preserve UID formatting, system/tag classification, source metadata,
download URLs and the reference one-tag-per-directory index behavior. Pending
assignment is consumed by one eligible unknown/flex tag, with validated private
publication and an unchanged source file. Concurrent eligible requests cannot
reuse it. Unlike C, assigning over a stale source mapping writes a UID copy and
clears the pointer instead of overwriting a library source. Tag lookup keeps the
reference default header validation; configured full TAF validation checks hashes.
Unknown fields survive model inference and assignment.

Trigger reload validates both configuration files before publishing active maps;
write saves persisted maps, excluding runtime fields. Exit/restart invokes the
owned server shutdown path. Restart reloads the deployment and listeners in the
same supervisor. The native test in `.cache/native-triggers.log` exercises a real
API-triggered restart with all three listeners and the box worker, persisted
settings and unchanged certificate hashes. Listener changes require restart;
reload alone refreshes configuration maps. Malformed reload retains prior state.

The image index uses uppercase SHA256 URL filenames and the C extension rule,
lazy/preload downloads, mapped box/catalog pictures, flush/refill, 404 and redirect
fallbacks. Only registered entries are downloadable. Downloads use system TLS
trust without box credentials, bounded concurrency, 16 MiB atomic publication and
HTTP timeouts. The index is limited to 4096 entries. Full filename lookup replaces
C's short-hash collision matching. Unsafe extensions and redirect HTML are
sanitized; cross-origin redirects are refused. `memory_used` deliberately exposes
the 64-bit C index estimate (including its sentinel node and path spelling), not
Go heap usage. Unregistered disk files and the separate reverse proxy cache are
excluded from image cache statistics/flush, matching reference index ownership.
The stats page template comes from the pinned GPL C backend.

Focused content/tag/cache race tests, the complete Go race suite and vet pass
(`.cache/content-tags-race.log`, `.cache/full-race-latest.log`,
`.cache/full-vet-latest.log`). Physical CC3200 acceptance remains pending because
no box is available. Release archives/container builds from the earlier snapshot
must be refreshed after the remaining implementation is finished.


### Certificate and ESP32 tools (2026-10-09)

Tasks 6.13 and 6.14 are complete on synthetic fixtures. The combined replay in
`.cache/firmware-fixed-rehearsal.log` passes **171 checks**, adding nine firmware
checks: both CA DER aliases (C selects TB1 for both), three certificate uploads,
unique firmware upload suffixes, certificate extraction, and patched firmware
layout/identities/WiFi. `scripts/firmware-fixture.py` generates a 1 MiB synthetic
flash layout using only marked fixture identities, a bootloader/application,
wear-levelled 4096-byte-sector FAT12 CERT directory, and writable TB_WIFI NVS.
The copied C and Go images are independently checked with `cmd/firmwareprobe`.
Embedded certificate hashes and partition layouts agree, both contain replacement
hostname/WiFi values, and both originals retain their SHA256. FAT timestamps and
allocation are not required to be byte-identical. No real firmware is distributed.

The pure Go firmware service bounds images to 64 MiB and concurrent management
operations to two. It checks segment bounds/counts, XOR checksums, application
SHA256, optional partition MD5, nonoverlapping partition ranges, FAT geometry,
cluster chains and NVS item spans/data/header CRCs. FAT12/16/32 short-name handling
uses the reference 4096-byte sector and wear-level translation; long names are
excluded as in the pinned `FF_USE_LFN=0` build. Certificate extraction validates
all three DER identities and the client/key match before inspecting conflicts or
publishing files. Repair and patch operate on owned copies; final validation
precedes atomic output publication. Existing originals and a previous good output
survive failed hostname patches. WiFi replaces SSID0/PW0 blobs and INDEX through
existing TB_WIFI namespaces with C-compatible entry/CRC encoding. Full/missing
NVS namespaces or FAT directory capacity fail clearly.

Deliberate safety/functional deviations: C accepts arbitrary certificate and
firmware upload bytes; Go validates them first. C upload responses can lose the
first filename character because canonicalization removes a doubled separator
before a stale-length slice; Go returns the actual published filename. The
replay explicitly asserts this known discrepancy and checks both real files.
C can overwrite following firmware bytes when a new hostname exceeds the old
string slot; Go rejects that input. Invalid images/credentials never replace
installed files. Different box credential conflicts are preflighted together;
Go retains per-box credentials when also populating missing global defaults,
where C may move them out of the box directory. Each credential file publishes
atomically; a filesystem I/O failure during a validated multi-file installation
can still leave an earlier completed file installed. Unknown uploaded certificate
filenames must contain parsable certificate material; private/key names must
contain a valid private key. Upload does not silently change selected settings,
matching the C absolute-path/bare-filename comparison behavior. Firmware repair
returns validation failures that C sometimes suppresses. HTTP 400 identifies
invalid images/inputs, 409 conflicts and exhausted suffixes, 503 admission limits.

`internal/app/firmware_test.go` verifies failed-operation preservation, conflicts,
validated extraction, safe upload names and patched image checksums. The firmware
parser is included in bounded CI fuzzing, seeded with both invalid bytes and a
complete synthetic image. Full Go race tests and vet pass. This evidence covers
synthetic file workflows; private production images and device flashing remain
unverified, and hardware acceptance stays pending.

CLI compatibility (2026-10-09): `scripts/cli-smoke.py` passes 18 executable
checks covering all 23 pinned long-option registrations, short aliases and
attached/grouped getopt arguments, comma config updates, server/client identity
generation and preservation, TB2 overlay installation, three-listener Docker
startup/shutdown, SHA1, bounded URL diagnostics with tag auth, ESP32 extraction,
injection, hostname patching, repair and failed-patch preservation. The full
synthetic encode test creates a validated 9,740,288-byte payload with audio ID
0xDEAFBEEF and 99 chapters (100 waveform blocks, as the C chapter cap requires).
The unit suite verifies generated client issuer signatures and RSA/P256 key types.
Random certificate and encoded audio bytes are not required to be identical.
`--help` exits successfully; conflicting operations fail explicitly. Docker-test
stops after listener readiness and does not perform C's public catalog update.
URL/cloud diagnostics report only status and byte count, enforce 30 seconds and
64 MiB, and redact transport errors. Firmware operations validate private copies
before atomic replacement; slot-overflow hostname patches fail with input intact.

MQTT/HA (2026-10-09): `scripts/mqtt-differential.py` passes against the pinned C
binary using a disposable loopback MQTT broker and marked synthetic identities.
All 162 discovery documents (132 server settings, 30 box entities) match after
normalizing only device build/software/platform fields. Availability, volume/dB
and command state topics match. Valid and invalid speaker-limit commands are
checked on both servers, including the persisted overlay. Embedded TLS CONNECT,
SUBSCRIBE, settings/desired JSON and PING also match. The full Go race suite passes
with the integration wired into runtime startup/shutdown. TLS tests require broker
trust by default and exercise explicit configured CA and insecure mode separately.
A controllable-clock test verifies 5/10/20/40/60/60-second failure delays, reset to
5 seconds after connection, and restoration after success. QoS 2 duplicate PUBREL
produces only one command application. Fresh-tonies, cancellation and cleanup are
covered by the embedded endpoint test. No physical MQTT acceptance is claimed.

MQTT limits: 64 KiB external-client packets, 512 queued publications, 64 in-flight
QoS exchanges, 30-second acknowledgment timeout, 32 registered boxes, 4096 restore
messages; the device endpoint allows 32 connections/subscriptions and bounded
4 KiB packets. Client TLS uses a separate TLS 1.2+ policy without the box listener's
legacy SHA1 switch. Embedded TLS uses its separately configured certificate and
optional client certificates; missing material fails startup instead of C's
implicit plaintext fallback. The endpoint adds proper QoS acknowledgments and
per-filter SUBACK codes. Fresh-tonies changes reach each subscribed connection;
C's single shared changed flag can cause only the first connection to receive them.
No retained general-purpose broker or unsupported pause/mute/instant-volume
control is advertised. Latest telemetry is replayed after outages; stale queued
telemetry is discarded in favor of the snapshot. Malformed numeric and boolean
commands are rejected instead of C's atoi/TRUE coercion. Reference REST settings
writes use the same validated box command service; C has no separate REST stop
route. Live server-side stop integration remains tracked by task 5.5/7.4.

Live audio (2026-10-09): `scripts/live-differential.py` verifies C/Go HTTP status,
MIME, advertised stream length, provisional TAF payload length and initial Ogg
structure against a slow synthetic HTTP WAV radio. Codec bytes, clock-derived
IDs and C's uninitialized provisional SHA1 are deliberately not compared. Go
uses a deterministic zero provisional SHA1 and rewrites it on finalization.
`internal/audio/stream_test.go` validates 20-second silence, final hashes, resumed
payload preservation and cancellation of a blocked source. Long silence exposed
an Ogg segment-table overflow; block construction now reserves lacing entries
for final packet padding. `internal/app/live_test.go` verifies two simultaneous
radios, stopping only one box, retaining a validated continuation prefix, TAP
conversion while serving and subsequent validated-cache delivery, and delivery
of blocks appended to an incomplete UID file. The full Go race suite and vet pass.

Live limits and differences: two active encoders share the existing bounded
10-job admission queue. Each stream owns a private disk spool bounded by
encode.stream_max_size (maximum 2 GiB minus one), uses 32 KiB delivery buffers,
30-second network/growing-file idle limits and 10-second client write deadlines.
HTTP(S) radio bodies are capped at 1 GiB; redirects remain on the same origin and
scheme. FFmpeg receives bytes through stdin with only the pipe protocol enabled;
external-protocol playlists are not implicitly fetched by FFmpeg. Confined local
media and 1–99-source TAP playlists are supported. Startup sweeping applies to
HTTP radio, rather than discarding fast finite local files. Configured stream
buffer, bitrate, source skip and force-new-stream resume selection are honored.
Resumed streams retain the previous valid audio prefix and identity. Known boxes
retain `<UID>.stream.<box-id>` rather than C's shared `<UID>.stream`, preventing
one box's continuation from reading another box's stream. Successful TAP output
is atomically published only after full validation; failed/cancelled playlists
retain the previous cache. Growing-source waits terminate after inactivity.
As in C, finite/explicitly stopped live responses may end before their advertised
maximum Content-Length; callers must handle the closed stream. Physical CC3200
playback, seek, stop and reconnect acceptance remain pending.

Cloud replay (2026-10-09): `scripts/cloud-differential.py` passes 24 response comparisons against the pinned C executable using an isolated mutual-TLS mock. Forwarded methods, paths, authorization, request bodies and content/OTA cache bytes match. Coverage includes protected and overridden claims, V1/V3 freshness, setup, check-OTA, chapter/meta mapping, cache hits, HTTP 503 responses and cache retention. Both V1 and V2 content follow C's V2 enable flag. Local claim omits Content-Type; successfully forwarded V1 freshness uses application/octet-stream without a charset. Missing V3 metadata marked nocloud stays local in Go (404); C forwards it (200). This intentional exclusion-policy deviation is asserted separately. These are synthetic checks; V3 hardware behavior is unverified.

Inventory audit additions (2026-10-09): `scripts/security-differential.py` compares ten isolated responses for domain-before-crawler priority, crawler locking, robots access, HTTPS-only and startup hard lock. The HTTP HTTPS-only difference is separately asserted, not normalized away. Unit checks cover synthetic domain hashes, warning-only incident state and subsequent TLS locking. Domain detection never resolves or contacts a listed host. Settings-write/hardLock timing differs because Go persists immediately.

`internal/diagnostics/pcap_test.go` validates raw PCAP framing, IP/TCP checksums, multi-packet response delivery/flush, private file modes, caps and symlink confinement. `internal/runtime/server_test.go` verifies separate web and supervised-box TLS secrets from actual handshakes. Both pass under the race detector. `scripts/diagnostics-smoke.py` verifies the built executable across all three listeners and two starts: 30 valid packet records, at least four TLS handshake identities, append behavior and 0600 modes. Capture is opt-in; detailed capture scope and limits are in settings-audit.md.

`TestStartupCatalogUpdatesAndCORS` verifies startup tonies/tonieboxes requests through an injected transport with no authentication, validated publication, worker cancellation/reaping and configured CORS on both listener routes. It and security tests pass under the race detector. Synthetic fixture generation now explicitly disables startup public metadata updates and selects info-level logging for readiness checks. See settings-audit.md for the complete consumer classification and deliberate runtime-state/logging/validation differences.

The final inventory audit now covers 69 route rows, 267 settings/tree entries, 23 CLI options, five non-table routing contracts and 16 integration/format areas. Generated rows merge separately maintained evidence instead of hardcoding every non-route row as pending. Internal C-only state and deliberate setting differences are explicitly labeled documented deviations. The audit discovered and closed scoped tasks 9.1–9.3. No row classification establishes physical or frontend acceptance.

Audit snapshot final rehearsal (2026-10-09): a new isolated source stage passed
all 171 combined C/Go checks. Full Go race/vet and five bounded parser/path fuzz
runs pass. The 18-check CLI replay, live replay, five pinned upstream custom JSON
tests, current Linux release native smoke and copied-deployment migration/rollback
pass. All five development target archives and amd64/arm64 runtime images build;
the amd64 runtime container smoke passes. The latest successful MQTT replay again
matches 162 discovery documents and four state topics. A preceding run stalled in
the C executable during mqtt_init_box, with C reporting MUTEX_MQTT_BOX held; the
same reference passed the following replay. No Go regression or C fix is inferred,
and oracle instability is retained in compatibility-report.md.

## Official develop port verification (2026-10-09)

Current target is C `8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1` and frontend
`636a8d65043f6c6e322f76bcb11e958725159e96`. Historical master checks above
remain historical. The new inventory classifies 114 commits, 109 changed files
and 29 runtime translation units; generated route/settings/CLI counts are
76/269/25. The isolated C executable and official frontend build successfully.

The combined oracle suite passes 172 checks. Cloud replay passes 24 response
comparisons plus forwarding/cache assertions; security replay passes ten response
comparisons and its explicit HTTPS-only deviation. Live stream replay, 19 CLI
checks, five upstream custom API checks and the HTTP/web-TLS/box-TLS diagnostics
smoke pass. Firmware port differential passes ports 1, 8443, 32767 and restoration
at 443, comparing patched application bytes, XOR and SHA256; invalid/missing
ports retain the original image. Upstream hardening tests pass on both servers
(five tests and five subtests each), covering pool identity reuse, settings,
secrets and private-fetch blocking/opt-in. Full Go race and vet checks pass.

MQTT replay matches 164 Home Assistant discovery documents and four state topics,
valid/invalid commands and embedded TLS/settings/ping on both servers. Registering
the box before waiting for the server discovery burst passed one rehearsal;
a repeat still reproduced the C MQTT mutex stall. Earlier QoS 2 and QoS 0 delayed-registration
runs both stalled in the C oracle; this fixture ordering is not a C bug fix or a
production reliability claim. Successful log: `.cache/develop-mqtt-early.log`.

Auth lifecycle/CORS/cookies, status, file indexes, tag info and individual/ZIP
track bytes match in the new develop harness. Simulator details and the explicit
unsupported cipher are recorded below. Skipped upstream cases are TB2/capture
stubs. No interactive frontend or physical CC3200 acceptance is inferred.

Final simulator rehearsal: C passes 37 tests and skips 33 upstream stubs; Go
passes 36, skips the same 33 and has one strict expected failure for
AES256-CBC-SHA256 (0x003d), absent from Go crypto/tls. Both retain seven upstream
TLS-version deprecation warnings. Logs: `.cache/develop-final-simulator.log`,
`.cache/develop-hardening.log`, `.cache/develop-final-race.log` and
`.cache/develop-final-vet.log`. The AES128-CBC-SHA256 assertion passes.

Development packaging uses version `0.0.0-dev.20261009-develop` under
`dist/20261009-develop`, with five cgo-free target archives, SHA256SUMS and
build.json source digests. Linux amd64 is the available native runtime target;
other native archives are build-only. Local amd64 and arm64 runtime images are
built with matching version tags; amd64 container startup, exact stored TAF
bytes, persistent settings/identity, restart and clean shutdown pass. No release
or image is published. The FFmpeg variant is optional. Native/migration results
for this artifact are recorded in `.cache/develop-native.log` and
`.cache/develop-migration.log`; archive hashes remain in the output directory
rather than this input document to avoid a circular source digest.

## Readability and Go practice fixes (2026-10-09)

Authentication no longer holds its state mutex while reading request bodies,
writing responses or persisting users. A separate mutation mutex serializes
user/settings changes; successful atomic writes publish new user state afterward.
State helpers document their locking contract. User-file loading validates the
entire bounded JSON document before publication, rejecting invalid/duplicate
records, missing/null arrays, excessive users and trailing JSON. Invalid files
fail startup rather than silently removing protection. Explicit empty user arrays
and missing files retain the bootstrap behavior.

Authentication routes, HTTP replies, login handling and user changes are separated.
CLI parsing, read-only deployment validation, hashing and listener startup now have
focused functions/files. Limits and firmware offsets have names, configuration
literals and instruction tables are expanded, worker certificate variables are
explicit, and authored imports consistently separate standard, external and local
packages. Unexpected embedded MQTT termination and automatic listened persistence
failures produce sanitized diagnostics. RC4 remains excluded.

Track export slots belong to each Server. Copying checks cancellation between
bounded reads within a single track; cancellation releases slots and removes the
private temporary output through the existing cleanup path.

Regression tests in `internal/app/readability_test.go` cover slow input/output
without blocking auth state, invalid startup files without partial publication,
concurrent durable user updates, failed persistence retaining live users,
within-track cancellation and independent export slots. Full race/vet checks
pass (`.cache/readability-final-race.log`, `.cache/readability-final-vet.log`).
The C comparison matches auth lifecycle, JSON and exact single/ZIP track bytes
(`.cache/readability-final-api.log`). The full simulator rehearsal retains the
same C 37 passes / Go 36 passes, 33 skips each, and one documented Go cipher
expected failure (`.cache/readability-differential.log`). CLI smoke passes 19
checks; firmware port byte/checksum differential, native startup and migration
rollback pass. The local binary is rebuilt. Versioned archives remain the earlier
development snapshot; these checks do not establish physical/frontend acceptance.

## Measured performance improvements (2026-10-09)

[Performance report](performance.md) contains baseline/final measurements,
retained raw results, workload limits and reproduction commands. Effective
settings are cached with immutable views and mutation invalidation, schema
parsing is cached, request filters are hoisted, and catalog model/audio lookups
use immutable publication-time indexes. Mutable snapshots and returned catalog
entries remain independent; arbitrary unknown overlays do not grow the cache.

Five-sample medians show time-handler throughput in the loopback HTTP harness
rising from 8,840 to 48,978 requests/sec with four clients. Handler microbenchmarks
improve 3.6–26.8×; these are synthetic measurements, excluding TLS and physical
playback. Indexes/cache retain additional memory and move work to publication or
first read; retained heap and startup costs were not measured.

Final full race/vet checks pass (`.cache/performance/race.txt`, `vet.txt`). C API
comparisons match auth lifecycle, JSON and individual/ZIP track bytes
(`api-differential.txt`); security 10-response, cloud 24-check and five upstream
custom-catalog comparisons pass (`security.txt`, `cloud.txt`, `custom-api.txt`).
Native startup/verified TLS/restart and migration rollback pass (`native.txt`,
`migration.txt`), as does reference inventory (`inventory.txt`); all these logs
are under `.cache/performance/`. The local binary includes the optimizations;
versioned archives remain earlier snapshots. Physical/frontend acceptance is
still pending.

## Go practice review fixes (2026-10-09)

All six review follow-up tasks in OpenSpec section 11 are implemented. MQTT
listener failure now cancels and closes active connections before joining its
workers. Client cancellation owns the socket before CONNECT/CONNACK I/O starts;
reader and command workers are joined on every session exit. A single ordered
32-entry command queue isolates callbacks from protocol processing. Overflow
ends the session, and QoS acknowledgements wait for callback completion through
the session's sole network writer. Duplicate PUBREL while a command is executing
does not acknowledge or execute it early. Callbacks receive the session context
and must honor cancellation.

HTTP shutdown cancels request contexts, drains all parent HTTP servers concurrently
under a shared five-second grace period, and waits for serving loops before
application teardown. Deadline expiry closes remaining connections and returns
an error. Go cannot forcibly terminate a handler that ignores cancellation; the
bounded fallback is explicit and tested rather than presented as guaranteed
completion of arbitrary handler code.

Box ingestion serializes per box. The global mutex protects only map membership
and committed state; metadata callbacks and event publication run outside it.
State commits before its ordered events, allowing consumers to read the matching
state immediately. A slow metadata lookup does not block another box's ingestion
or snapshots.

Custom-catalog mutations now return typed failures. HTTP dispatch uses
`errors.As` and failure kinds, retaining exact reference messages/statuses,
including the reference's 400 response for encode/save/journal failures. Internal
errors use lowercase sentence starts. Event subscription APIs expose receive-only
channels. Module metadata is tidy, the restart sentinel follows Go naming,
unused assignments and the redundant body conversion are removed, and parser
errors retain proper format names within idiomatic sentences. CI now runs pinned
Staticcheck v0.8.1 and checks `go mod tidy -diff`.

Full race tests, vet, Staticcheck and module-tidiness checks pass. Targeted
lifecycle/concurrency regressions pass ten repeated race-enabled runs. Logs:
`.cache/go-practice-review/final-race.txt`, `final-vet.txt`,
`final-staticcheck.txt`, `final-module-tidy.txt` and `repeated-regressions.txt`.

The final binary passes MQTT comparison (164 discovery documents and four state
topics, valid/invalid commands and embedded TLS/settings/ping), the full RTNL
replay (fragmentation, combined ordering, raw/CSV logs and function families),
five upstream custom-catalog API tests, develop auth/JSON/exact individual/ZIP
track comparisons, native TLS/restart/shutdown, migration rollback and reference
inventory. Logs are `final-mqtt-differential.txt`, `final-rtnl-differential.txt`,
`final-custom-api.txt`, `final-api.txt`, `final-native.txt`, `final-migration.txt`
and `final-inventory.txt` under `.cache/go-practice-review/`.

One repeated MQTT rehearsal stalled in the C oracle before reaching the Go
candidate; its mutex warning and failure are preserved as `mqtt-oracle-stall.txt`
and `mqtt-oracle-stall-server.log`. The subsequent complete comparison passed.
The original C checkout is unchanged. The local cgo-free Linux amd64 binary is
rebuilt; existing versioned archives remain earlier snapshots. The five required
physical/frontend/final acceptance tasks remain pending, independently of this
completed review follow-up.


## Locking review fixes (2026-10-09)

All six follow-up tasks from the locking audit are implemented. Configuration
reads are immutable atomic snapshots; runtime publication progresses separately
from durable I/O and commits preserve the latest transient values. Image
fetch/flush waits and content mutation/inspection/listened waits are cancellable.
RTNL logging and reverse bookkeeping no longer share the freshness lock.
Assignment reservations preserve replacements during earlier work. Shared
live/playlist output keys now coordinate with finite generation and management
writes, and publication releases destination ownership before slow clients finish.
Firmware/certificate transactions release before responses. Spool state locking
is separate from positional I/O/header lifetime, finalized copies use independent
readers, and closure is idempotent. MQTT restore builds discovery outside the
telemetry lock; redundant frame/PCAP mutexes are removed. Canonical path tokens
coordinate separate/nested/aliased roots and disappear after their last waiter.
Necessary catalog, event, authentication and lifecycle coordination remains.

Full race, vet, pinned Staticcheck and module-tidy checks pass. Focused concurrency
regressions pass ten repeated race-enabled runs; image single-fetch coalescing also
passes ten repeats. C/Go content, tags, cache, firmware/certificate, audio and RTNL
comparisons pass (96 checks in the combined API/RTNL log). Live streaming, MQTT
(164 discovery documents/four state topics and command/embedded listener checks)
and develop auth/JSON/exact track comparisons pass. Native TLS, persistence,
restart and child shutdown; copied-data migration/rollback; and reference inventory
checks pass. The original C checkout remains clean. Logs are retained under
`.cache/locking-review/` with the corresponding suite names.

The 16-worker mixed settings benchmark has a median 147.2 ns/op over three runs,
with 162 B/op amortized over the mix. Its mutex/block profiles show writer waits
and no settings-reader waits. Temporary files were on tmpfs; this establishes no
SSD throughput or HTTP latency claim and no new before/after speedup factor.
Permanent raw outputs and profile summaries are in
`docs/benchmarks/2026-10-09-locking/`; detailed rationale and regression coverage
are in `docs/locking-review.md`.

The local CGO_ENABLED=0 Linux amd64 binary is rebuilt: 15,639,853 bytes,
SHA-256 `09b2d32e40306a65ff8ff27845b61993879263402573e613aaeb10569616dda0`.
Linux arm64, Windows amd64, and macOS amd64/arm64 cross-builds pass; these are not
native runtime checks. Existing versioned release archives remain prior snapshots.
This follow-up completes 6/6 tasks, bringing the change to 90/95; the original five
physical/frontend/final acceptance gates remain pending because the required
acceptance evidence is unavailable.

## Selectable deployment storage — 2026-10-10

All durable deployment storage now uses an injected filesystem or generic
S3-compatible backend. Filesystem remains the default. See
[configuration and semantics](storage.md) and [verification results](storage-verification.md)
for automated S3 coverage, local scratch boundaries, conditional reads,
publication/directory-move behavior, build evidence and credential support.
The S3 client uses the standard library; AWS SDK v2 is not linked. Physical and
frontend gates remain open. A reproducible C-oracle firmware-upload timeout and
absence of live-provider testing are recorded explicitly in the storage report.

## CC3200 browser flashing — issue 437 (2026-10-10)

The embedded `/tools/cc3200` tool and built-in `goteddycloud-cc3200` frontend
plugin implement gateway preparation, verified flash/files backup, certificate
import and ordered original/CA/preloader installation. Backups use the selected
filesystem/S3 backend. See [workflow and limits](cc3200-flashing.md).

The full Go race suite passes, including filesystem/S3 backup reload, certificate
conflict preservation, authentication/origin checks, malformed FAT/ZIP rejection
and installation-plan validation. `go vet`, Staticcheck v0.8.1 and dependency
consistency checks pass. The bounded FAT fuzz run completed 62,338 executions
without a failure. Nine Node protocol tests pass with the official pinned NWP
loader supplied locally; without it, eight self-contained tests pass and the
loader test explicitly skips. The actual loader bytes are checked/uploaded only
to simulated SRAM; this does not execute target firmware or establish hardware
support. Native listener, TLS, persistence, restart and shutdown smoke checks pass.

The rebuilt stripped Linux amd64 binary is 11,034,786 bytes, SHA-256
`f8ea6cf040062030c0fddbffcf9d7e48633564400fcc4c53b0cb2c536656e535`.
Existing release archives were not regenerated. Local evidence is under ignored
`.cache/cc3200-*.log`. The C checkout was not changed.

No box or ESP32-C3 was available, and computer-use browser inventory was empty.
Visual browser verification, native USB permissions, actual ROM/NWP handover,
real FAT compatibility, flash read/write verification and post-install boot are
unverified. Existing physical acceptance gates remain open. The new plugin entry
is an intentional extension to the legacy plugin-list response.

### Issue #531: alternate box connection ports (2026-10-10)

Added the CC3200 SD patch generator and download in `/tools/cc3200`, with the
management-only `/api/cc3200/altPort?port=10443` endpoint. The published
HackieboxNG hbng_bl_v0.9.3d port-10443 patch is the semantic reference vector in
`internal/cc3200/testdata/altPort.10443.json`; all Thumb immediate values are
independently decoded in tests. Boundary/method/authentication/box-listener
checks pass. Existing ESP32 API and CLI port support is retained; added tests
verify all matching connection functions, exact 443 restoration and no mutation
of unsupported images.

Validation: full Go race suite, go vet, Staticcheck v0.8.1, UI module syntax check
and eight self-contained simulated serial Node tests passed (one optional loader
test skipped without its environment variable). Logs: `.cache/issue-531-race.log`
and `.cache/issue-531-node.log`. Hardware and visual browser checks remain pending.

Rebuilt native CGO-disabled stripped binary: 11,047,074 bytes, SHA-256
`fa8234daff27aab052c1038632a87ac816010b69368c9fd237c6f74934104e48`.
Existing release archives were not regenerated.

### Management listener and reverse proxy separation (2026-10-10)

Management HTTP/HTTPS listeners now reject box API routes instead of dispatching
them to Device. CLI help and settings labels explicitly separate management
HTTP/HTTPS from box HTTPS. The extra `core.server.trusted_proxies` CIDR setting
(default empty) enables public scheme/host metadata only for configured direct
management peers. Secure cookies, CC3200 origin checks and management security
mitigation use the accepted scheme. Device certificate identity is unaffected.

The HTTPS-to-HTTP reverse proxy integration test verifies Secure login cookies,
public-host origin checks (including the public port), rejection of cross-origin
requests and management/box route isolation. Trust tests cover ignored spoofed
headers, malformed headers and CIDR validation. The live-stream restart test now
calls the device handler explicitly, matching the intended listener separation.
Go vet, Staticcheck and targeted race tests pass. Full-suite race results are in
`.cache/management-proxy-race.log`. Nginx configuration is documented in
`docs/reverse-proxy.md`; no Nginx deployment or visual browser acceptance is claimed.

Rebuilt native CGO-disabled stripped binary: 11,051,170 bytes; SHA-256
`ab9f50ffedf989be2d42ca008eed62ffe88d8691296a62ec9a4e15fffb90ca05`.
Release packaging includes the proxy guide; existing archives are unchanged.

### Rename management HTTPS flag (2026-10-10)

Renamed `-web` to `-httpsecure` in the CLI, Docker default, smoke/oracle tooling
and deployment examples. `-httpsecure off` disables management HTTPS behind a TLS
terminating reverse proxy; omission retains the configured 8443 default. The
persistent `core.server.https_web_port` key and runtime readiness keys are unchanged.
The native smoke script now checks management routes on management listeners and
verifies their box-route rejection, matching the prior listener separation.

CLI/runtime tests, CLI vet/Staticcheck, all 20 CLI smoke checks (including disabled
management HTTPS) and native startup/TLS/persistence/restart/shutdown smoke pass.
Logs: `.cache/httpsecure-cli-smoke.log`, `.cache/httpsecure-native-smoke.log`.
Rebuilt stripped native binary: 11,051,170 bytes, SHA-256
`f9b95e810a0dc8c3f1ad2013326768ce632064c9271cc0fc16573b5bcbaf5484`.

### Management certificates and automatic ACME (2026-10-10)

Management HTTPS accepts a separate manual PEM certificate/key pair or explicit
config-enabled ACME. The default remains the existing local pair. Added eight
`core.httpsecure.*` settings, independent management TLS runtime wiring and
HTTP-only challenge dispatch. Config validation checks manual material and ACME
inputs without cache writes or provider requests. Box identities remain unchanged.

A disposable HTTPS ACME issuer test exercises account/order authorization,
HTTP-01 responses, CSR signature/domain checks, CA-signed certificates and cache
reuse after manager reconstruction on filesystem and S3. Additional checks cover
manual pairs on both backends, malformed configuration, explicit domain policy,
cache confinement/private permissions, cancellation of ACME network I/O and
independent HTTPS listener certificates. Production Let's Encrypt issuance and
renewal against a public CA were not run; automatic renewal uses the pinned
Go autocert manager.

Full Go race suite, go vet, Staticcheck v0.8.1, tidy-diff and native
startup/TLS/persistence/restart/shutdown smoke pass. Logs:
`.cache/httpsecure-acme-race.log`, `.cache/httpsecure-acme-native.log`.
Schema JSON, release packaging syntax and new documentation links were checked.
The native stripped CGO-disabled binary is 11,452,578 bytes, SHA-256
`689f0d8cef1556780448aa759a92e0e337affce8a7d1352852aa152953144195`.
Release packaging includes the management TLS guide and BSD notices for the
new pinned Go crypto/IDNA dependencies. Existing release archives were not rebuilt.

### Roles and private user areas (2026-10-10)

Added admin/editor/reader roles, private filesystem/S3 areas, exclusive registered
box ownership, per-area catalogs/content/settings/live state, owned-device and
MQTT command/freshness routing, and administrator area selection. Existing
role-less accounts load as administrators. Opaque persisted identities survive
password changes and prevent username reuse from inheriting deleted accounts'
files. Role/ownership checks use current account state; password changes and
account deletion revoke sessions, and access changes disconnect SSE streams.

The built-in `/tools/users` page/plugin exposes roles, box assignments, area
selection and own-password changes. Private APIs use exact allowlists; storage
roots reject sibling and symlink escapes. Shared device credentials are explicit
read-only references. Private remote media uses DNS/IP policy enforcement, and
private audio encoding uses stdin-only FFmpeg input with secondary file/network
protocols disabled. Normal WAV and TAF conversion and malicious playlist rejection
are covered by regression tests. Account deletion retains area data for recovery.

Full `go test -buildvcs=false -race ./...`, vet, Staticcheck v0.8.1, tidy-diff,
JavaScript syntax and Python release/smoke-script syntax checks pass. Tests cover
filesystem/S3 isolation, reader write rejection, current-session demotion and
revocation, access persistence, legacy migration, exclusive box assignment,
concurrent cold initialization and SSE closure. Native executable smoke tests
verify private metadata, reader restrictions, a synthetic client-certificate
box's private volume setting, two-start persistence, verified listener TLS,
existing restart behavior, certificate preservation and worker shutdown.
Logs: `.cache/roles-race.log`, `.cache/roles-vet.log`,
`.cache/roles-staticcheck.log`, `.cache/roles-native.log`,
`.cache/roles-baseline-native.log`.

The stripped native CGO-disabled binary is 11,563,170 bytes, SHA-256
`a1a5349ab35000edb4928303deee639909368cf929661a6f63cbf62435ae8c16`.
Release packaging includes `user-areas.md`; existing release archives were not
rebuilt. Browser visual/interactive acceptance and physical-device acceptance
remain untested. The S3 checks here use the local S3-compatible test server,
without claiming a new live Silo/NooBaa provider acceptance run.

### Shared areas and independent membership roles (2026-10-10)

Areas now have independent identities, member lists and box assignments in the
same atomically published account database. A user can administer one area,
edit another and read a third. Global administrator rights are separate from
area administration. Existing private areas migrate without changing their IDs,
paths, contents or initial permissions. Sharing an existing default area adds
members without copying its files. Deleting a user removes memberships while
preserving areas for remaining members; username reuse restores no old access.

The built-in user-management page supports selecting accessible areas, creating
common areas, assigning independent member roles, editing memberships, assigning
boxes globally and removing independent/orphaned area records while retaining
files. Session selections store canonical IDs, preventing username reuse from
retargeting another member's selected shared area. Status/profile responses
expose the selected area and effective area role. Member/access changes invalidate
SSE connections; current membership is rechecked on subsequent requests.

Full Go race tests, vet, Staticcheck v0.8.1, tidy-diff, JavaScript syntax, Python
smoke/release-script syntax and documentation-link checks pass. New tests cover
filesystem/S3 shared content and box routing, independently different member
roles, global privilege escalation rejection, area-administrator controls,
permission revocation, SSE closure, account deletion/recreation, private-area
migration with preserved paths, invalid registry rejection and failed S3
publication retaining both live and durable permissions. Native process tests
exercise private/shared metadata, role changes, distinct synthetic TLS clients
for private/shared boxes, two-start persistence and worker shutdown. Existing
listener/TLS/restart/identity-preservation smoke also passes.
Logs: `.cache/shared-race.log`, `.cache/shared-tests.log`,
`.cache/shared-vet.log`, `.cache/shared-staticcheck.log`,
`.cache/shared-native.log`, `.cache/shared-baseline-native.log`.

The stripped native CGO-disabled binary is 11,616,418 bytes, SHA-256
`cf8b540c28ea095d5d1b8acc3638c371ba4cb13d6bc887b608e0dc879fceefdc`.
Browser visual/interactive acceptance, physical-device testing and a new live
Silo/NooBaa acceptance run remain unperformed. Release archives were not rebuilt.

### Read-only catalog grants (2026-10-10)

Users can share their stored content/library catalog with existing accounts as
an entire catalog (including future files) or an explicit file selection. Default
area editors and area/global administrators manage grants through `/tools/shares`
and the exact `/api/auth/catalog/*` routes. Recipients receive no membership,
write access, device access or onward-sharing rights. Safe textual metadata and
stored audio downloads are exposed separately from source/credential sidecars.
Full listings exclude hidden files and symlink aliases and have traversal limits;
partial listings access only selected files. Downloads use standard HTTP ranges.

Grants live in the atomically published area registry for both filesystem and S3.
Password changes preserve recipient identity. Account deletion prunes grants;
username recreation restores no access. Revocation/replacement applies to new
requests, while an already-authorized finite download may finish. Grants belong
to areas and remain independently of the creator's later membership changes.

Full `go test -buildvcs=false -race ./...`, vet, Staticcheck v0.8.1,
`go mod tidy -diff`, JavaScript syntax and Python smoke-script syntax checks pass.
Filesystem and local S3-compatible tests exercise selected/full grants, future
files, metadata projection, denied unshared files and area access, independent
sharing roles, HTTP byte ranges, symlinks, persistence, password changes,
recipient deletion/recreation, invalid grants and failed publication retaining
live/durable access. Native process smoke tests verify selected downloads,
denied private files, two-start grant persistence and revocation, alongside
existing area/member roles, synthetic TLS device routing, listener TLS,
restart identity preservation and shutdown checks.
Logs: `.cache/shares-race.log`, `.cache/shares-vet.log`,
`.cache/shares-staticcheck.log`, `.cache/shares-tidy.log`,
`.cache/shares-native.log`, `.cache/shares-baseline-native.log`.

The stripped CGO-disabled native binary is 11,698,338 bytes, SHA-256
`b832c00c0220e0e5377d37292646a634afa782305b72e91ad205db3c4ffc4317`.
No browser surface was available for interactive/visual acceptance. These S3
checks use the local S3-compatible test server; no new live Silo/NooBaa acceptance
run or physical-device test is claimed. Release archives were not rebuilt.

### Native Web UI users, areas and catalog sharing (2026-10-10)

The owned React/TypeScript overlay in `frontend/src/goteddycloud/` integrates
accounts, area memberships and catalog grants into the pinned frontend's router,
Ant Design layout, theme, authentication and navigation. Settings exposes native
users/areas controls; Tonies exposes native shared catalogs. Neither page embeds
the standalone tools. The header offers an area selector, moved into the mobile
navigation drawer on small screens. Selection clears stale explicit area/device
parameters and the saved content overlay before reloading. Normal accounts enter
the accessible users page from Settings; global settings stay administrator-only.
English and German labels are included. Old standalone routes remain fallbacks.

`scripts/frontend.sh build` prepares an isolated, marked frontend stage with
checked integration anchors and the upstream lockfile, leaving the pinned
reference checkout unchanged. TypeScript checking and the production Vite build
pass. `scripts/frontend.sh test` passes all 86 frontend tests, including the new
reverse-proxy prefix/session handling, expired-session reporting, download path
encoding, independent member roles and stale selector cleanup checks. CI now
runs the frontend tests after building. Shell/Python/Node script syntax checks
also pass. The existing upstream large-chunk advisory remains; the build succeeds.

`scripts/webui-smoke.mjs` passes against a disposable deployment and native Go
process in headless Chromium 156 (Playwright 1.64.0). Browser interaction covers
first-account creation, enabling login, signing in, editor/reader account
creation, a shared area with independently different roles, partial/full grants,
authorized downloads, denied unshared files, revocation, role-aware controls,
Settings redirection and header area selection. Mobile drawer controls, German
labels and dark mode are exercised, with no uncaught page exceptions. Desktop,
mobile, German and dark screenshots were captured under `.cache/webui-*.png`;
desktop and mobile layouts were visually inspected. This supersedes the earlier
lack of browser acceptance for these new users/areas/sharing flows; it does not
establish acceptance of all upstream frontend pages or physical Toniebox playback.
Logs: `.cache/webui-integration-build.log`, `.cache/webui-integration-tests.log`,
`.cache/webui-integration-browser.log`.

The integrated asset build is installed into the running isolated manual-test
deployment `.cache/local-test-20261010`, served at `http://127.0.0.1:18081/web/`.
A separate browser check verifies the live native users page and shared-catalog
login prompt. Its account/content configuration was not changed by these checks.
The Go binary, listener ports, container and release archives were not rebuilt
for this frontend-only change. Frontend assets continue to be installed separately.

### User management as a core UI feature and deep-link assets (2026-10-10)

User and area management now exists only as the native Settings route. The old
user plugin is no longer advertised; its metadata, preview and standalone script
are removed. Legacy user page URLs redirect to the native route. Plugin discovery
still excludes a stale on-disk directory with the old reserved plugin ID.

SPA HTML includes a parser-visible relative base before the upstream scripts.
Tests cover /web with and without the trailing slash, direct settings/community
routes, and URL resolution under an external proxy prefix. Browser smoke checks
also reject failed JavaScript/CSS asset responses, confirm the user management
plugin is absent, and exercise the native account and area workflows.

### Empty box-model catalogs and external guide links (2026-10-10)

The frontend now tracks box-model loading errors independently of catalog size.
Successful empty official/custom lists do not create an error notification;
HTTP/network failures and malformed catalogs still do. Valid results survive a
failure of the other source. The loader cancels pending requests on unmount.

External beginner guides and ESP32 backup tools remain ordinary links without
automatic cross-origin HEAD/GET probes. This avoids CORS errors and does not
claim to have checked the destination's availability.

Validation: TypeScript/Vite build, 89 frontend tests, and browser smoke passed.
Browser checks cover valid empty catalogs with no false notification, an injected
HTTP 500 producing the notification, and visible beginner guide links without
forum fetch/XHR requests. Updated assets were installed in the marked local-test
deployment; no backend restart or account/configuration changes were needed.

### Native CC3200 protocol and browser Go/WASM integration (2026-10-10)

The integrated `goteddycloud cc3200` command and standalone `cc3200tool` use
the same Go protocol core as the browser flashing workflow. Native transport
supports UART and ESP32 UARTGWEX; browser transport continues to use Web Serial.
The previous JavaScript ROM implementation was removed. The manufacturer loader
is supplied locally and checked against its pinned hash, without redistribution.

Validation: full `go test -race ./...`, Go vet (native and WASM), Staticcheck,
eight Node protocol/WASM tests, and the Chromium Web UI smoke test passed.
The smoke test loads the actual embedded Go module under the tool page's CSP.
The optional loader-handover test passed with the pinned reference loader.
Linux native builds and CGO-disabled Windows/amd64 and macOS/arm64 builds passed.
Protocol tests simulate fragmentation, checksum/readback corruption, disconnects,
concurrent requests, flash publication ordering and bounded offline operations.
Device writes require a double-read full backup saved with private permissions
and synchronized before mutation; ordinary outputs also refuse overwrites.

Physical UART/gateway acceptance remains pending because no CC3200 box is
available. Live CC32xx flashing is unsupported. See `docs/cc3200-tool.md` for
commands and offline layout restrictions. The user-managed debug server was not
restarted or reconfigured; rebuild and restart it to serve the new embedded tool.
