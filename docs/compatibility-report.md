# Development target status

Current target: official C develop `8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1`, frontend `636a8d65043f6c6e322f76bcb11e958725159e96`. The development extension adds optional management sessions, listened metadata, track exports, firmware port patching and development hardening. See [development audit](develop-port.md), [commit/file inventory](develop-inventory.json), and the current [route inventory](reference-inventory.json): 76 routes, 269 settings/tree entries and 25 CLI options. Physical-device and interactive frontend acceptance remain pending; replacement parity is not claimed.

Go additionally supports global administration, independent administrator/editor/reader member roles, and private/shared storage and box areas; see [user areas](user-areas.md). This extension is independently enforced by the backend.

The following is the historical master-baseline acceptance report; its test results and old artifact hashes apply to that snapshot. Current development evidence is appended to `verification.md`.

# Compatibility and support report

Status: **development backend; not declared a compatible teddyCloud replacement**.
Report date: 2026-10-09. OpenSpec change: `rewrite-teddycloud-in-go`, 68 of 73 tasks complete. The five remaining gates are physical/offline acceptance (3.8), physical converted/live audio (5.7), official frontend interactions (6.15), physical end-to-end acceptance (8.7), and final replacement acceptance (8.8).

The official C reference is pinned to `78c3b38e36f665d3a5fffe907f73a02725176377`; the official frontend is pinned to `e2f40c2c69a3b2b46093ddc5542773dd0fa0067e`. The original ../teddycloud checkout remains unchanged. Source-indexed evidence covers 69 routes, 267 settings/tree entries, 23 CLI options, five non-table routing contracts and 16 persisted-format/integration areas. Every row has an explicit implementation/deviation classification and evidence; that classification does not substitute for physical/frontend acceptance.

## Implemented and checked

Configuration/overlays, certificates, three listener lifecycles and supervised legacy box TLS, binary RTNL, claim/freshness/content, cloud and V3 APIs, OTA policy/caches, Go TAF construction, conversion and live delivery, management/file/catalog/firmware utilities, placeholder auth, SSE/stats, reverse proxies, MQTT/HA, security mitigation and opt-in diagnostics are implemented. The backend builds without cgo; FFmpeg is external and optional for stored TAF delivery. Ordinary runtime requires no C teddyCloud process.

| Evidence | Result and scope |
| --- | --- |
| Go tests/race/vet | Full race suite and vet pass; new counter/security/startup/diagnostic checks also pass. |
| Parser/path fuzz smoke | MQTT, firmware, TAF header, Opus framing and confined paths pass bounded three-second runs. |
| Clean isolated C/Go suite | 171 checks pass for device/management/auth/static/OTA/V3/playlist/file/audio/firmware/RTNL behavior. |
| Mutual-TLS cloud replay | 24 responses, forwarded method/path/auth/body and content/OTA cache bytes match; excluded V3 metadata deviation separately asserted. Cloud diagnostic CLI also uses the mock successfully. |
| MQTT replay | 162 discovery documents, four state topics, valid/invalid commands and embedded TLS/settings/ping match in the successful replay. One preceding C run stalled while registering a box and logged its MQTT-box mutex held; the same pinned C replay subsequently passed. This oracle instability remains a limitation. |
| Live replay | Status, MIME, advertised size, provisional TAF length and Ogg structure match C; unit/integration checks cover silence, resume, two streams, scoped stop, TAP publication and growing UID files. |
| Security replay | Ten response cases checked; HTTP HTTPS-only difference is separately asserted. |
| CLI | 18 executable checks, including synthetic encode_test output, identities, firmware operations and diagnostics. |
| Official custom JSON tests | Five pinned upstream backup/bulk/normalization/validation/rename tests pass. |
| Diagnostics | PCAP framing/checksums/bounds/confinement and real web/box TLS secrets pass; executable two-start test verifies three listeners, append and private file modes. |
| Linux amd64 artifact | Native verified TLS/listeners, persisted settings/identities, restart, worker reaping and read-only migration/rollback pass. |
| Other artifacts | Linux arm64, Windows amd64, macOS amd64/arm64 cross-builds pass; runtime behavior on these platforms is unverified. |
| Containers | Linux amd64/arm64 images build. amd64 runtime smoke verifies codec-free stored content, persistence/restart and clean exit. Arm64 runtime is unverified. The separate FFmpeg image supports conversion. |

Development archives are under `dist/20261009-audit`, version `0.0.0-dev.20261009-audit`, with per-target `build.json` source digests and SHA256SUMS. Images are local `goteddycloud:dev-20261009-audit`, `:dev-20261009-audit-arm64` and `:dev-20261009-audit-ffmpeg`. These are local artifacts, not published releases. Remote CI execution is unverified.

## Differences and limits

See [verification](verification.md), [settings audit](settings-audit.md), [TLS policy](tls-compatibility.md) and the machine-readable inventories for full detail. Material differences include:

- Legacy RSA/SHA1 policy is isolated in a supervised Go box process; web and upstream TLS remain strict. Name-based box identity and placeholder web login reproduce the baseline without claiming cryptographic device trust or authenticated management sessions.
- Missing V3 metadata marked nocloud stays local. Cache publication validates complete transfers and retains old data on errors. Filesystem mutations and firmware/certificate operations add confinement, size checks and atomic publication.
- MQTT validates commands, restores bounded state and adds proper acknowledgments. Embedded missing TLS material fails startup instead of silently selecting plaintext. Freshness notifications reach each subscribed connection.
- Live inputs support confined local media and HTTP(S). Other FFmpeg network protocols are excluded. Stream spools, queues, network reads and delivery waits are bounded; continuation files are scoped per box. Finite/stopped live HTTP bodies can end before their maximum advertised size, as in C.
- TAP cache integrity is always checked. Some C internal diagnostic/build/process counters remain default/persisted API values rather than live C-state equivalents. Historical C configuration migrations are not replayed. Structured Go logs omit C color/full-token trace behavior. See every documented-deviation settings row before migration.
- PCAP captures handler-level inbound application traffic, not all C transport/outbound traffic. TLS secrets cover web/box servers. Private diagnostic files are bounded and confined. Security HTTPS-only and hard-lock timing differences are explicitly recorded.
- Startup public metadata updates are cancellable and asynchronous, preserving validated previous files on failure. Frontend assets are built separately and are not redistributed in backend archives/images.

## Pending acceptance

The user identified a CC3200 Toniebox but said no box is currently available. Its firmware and isolated deployment are unknown. No physical TLS, claim/freshness, local/cached playback, seek/chapters, converted/live playback, OTA selection or MQTT-settings result is claimed. Follow [hardware acceptance](hardware-acceptance.md) when it becomes available. TB1/TB2 synthetic protocol coverage does not establish support for any real chip/firmware variant.

The official frontend builds, but the available UI tool reports no enabled browsers. Backend API/static tests do not close frontend interaction acceptance. Follow [frontend acceptance](frontend-acceptance.md) in an available browser.

The final replacement gate stays open until these results exist and any resulting gaps are resolved. This report records current support boundaries; it does not close task 8.8 or authorize a production cutover. Use copied deployments and the [migration/rollback procedure](migration.md).

Development TLS deviation: RSA AES256-CBC-SHA256 (0x003d), accepted by the C
oracle, is unavailable in Go crypto/tls. The upstream simulator assertion remains
visible as a strict expected failure. AES128-CBC-SHA256 and normal simulated
CC3200 negotiation are supported. See develop-port.md and verification.md.

Management listener separation: box `/v1/`, `/v2/` and `/v3/` routes now return 404
on management listeners (401 may be returned first when login is required). Use
the box TLS listener for those routes. This intentionally removes the inherited
management-to-device route fallback. A Go-specific setting,
`core.server.trusted_proxies`, enables explicit management proxy trust; see
[reverse proxy setup](reverse-proxy.md). The pinned upstream inventory above is
historical and does not include this extra setting.

Management TLS extension: eight Go-specific `core.httpsecure.*` settings add
manual management certificate/key paths and opt-in ACME provisioning/renewal with
a filesystem/S3 cache. Box TLS keeps the upstream certificate settings. See
[management TLS configuration](management-tls.md). These additions are outside
the pinned upstream settings inventory.
