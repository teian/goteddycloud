## Context

`goteddycloud` currently contains only OpenSpec scaffolding. The read-only reference is `../teddycloud`, official repository `https://github.com/toniebox-reverse-engineering/teddycloud`, commit `78c3b38e36f665d3a5fffe907f73a02725176377`. Its working tree was clean during inspection. C dependencies and the frontend are submodules, currently unpopulated locally; compiling the oracle and exercising the frontend will require obtaining their pinned revisions in a separate test checkout.

Primary source anchors:

| Area | Reference |
| --- | --- |
| Route dispatch, listener access, box identity, TLS | `src/server.c`, `src/tls_adapter.c`, `include/tls_config.h`, `docs/box-https-sni.md` |
| Settings, defaults and overlays | `src/settings.c`, `include/settings.h` |
| Cloud policy, device protocols, proxy/cache | `src/handler_cloud.c`, `src/handler.c`, `src/cloud_request.c`, `src/handler_reverse.c` |
| Binary contracts | `proto/*.proto`, `src/handler_rtnl.c`, `src/toniefile.c` |
| Administration and data | `src/handler_api.c`, `src/contentJson.c`, `src/toniesJson.c`, `docs/api/custom-tonies.md` |
| Events and integration | `src/handler_sse.c`, `src/toniebox_state.c`, `src/mqtt.c`, `src/mqtt_server.c`, `src/home_assistant.c`, `MQTT_CONTROL.md` |
| Firmware utilities and fixtures | `src/esp32.c`, `src/cert.c`, `tests/`, `dev-sandbox/` |

The README lists TB2 as planned although the route table includes v3 handlers. Treat source behavior as the baseline and hardware support as a separately evidenced result. The reference box listener requests optional client certificates and contains incomplete verification logic; a blanket Go mutual-TLS requirement would silently break compatibility.

## Goals / Non-Goals

**Goals:**

- Deliver a maintainable Go implementation of the reference backend with measurable device, frontend and storage compatibility.
- Preserve users' existing files and identities; make migration and rollback explicit.
- Build the backend with `CGO_ENABLED=0`; use explicit external FFmpeg audio processing where required.
- Complete scope in milestones with automated acceptance gates and honest hardware support reporting.

**Non-Goals:**

- Redesigning the frontend, adding device commands unavailable in the protocol, reproducing undocumented manufacturer services beyond baseline behavior, or porting the C WASM/toolchain/vendor libraries.
- Claiming full TB2 support from synthetic route tests or fixing all upstream limitations as part of the translation.

## Decisions

### 1. Pin behavior and inventory before implementation

Create `docs/compatibility.md` with source locations, methods/path matching/listener scope, request/response contracts, settings/defaults, storage side effects, fixtures, and milestone/test links. Include every route in `request_paths`, binary RTNL handling, static/legacy web behavior, settings keys, CLI modes and utilities; inspect handlers, not just names. Track entries as pending, implemented, verified, or documented deviation. Deferred work remains incomplete in this change. Completion requires all in-scope entries verified or a formally amended spec for any exclusion.

Use a separate C checkout at the pinned commit, with pinned submodules, as an oracle. A mock cloud and copied data isolate runs from manufacturer services and production files. Compare status, significant headers, normalized JSON and decoded protobuf, byte-exact local content, file side effects and event sequences. Normalize only documented nondeterminism (time, generated IDs, JSON ordering). This is more reliable than a literal C-to-Go translation, which would preserve incidental implementation structure without proving behavior.

### 2. Separate domain state from transports

Use one Go module, `cmd/goteddycloud`, and internal packages for config, identity/TLS, protocol, cloud, content/TAF, audio jobs, box state, management HTTP, RTNL, MQTT/HA, firmware and persistence. Prefer standard-library HTTP, TLS, logging and process control. Pin reviewed Go dependencies for protobuf, MQTT and any necessary Ogg helpers. Choose a concrete module import path and supported Go toolchain during bootstrap; ownership is not yet specified.

HTTP, MQTT and RTNL invoke common box/config/content services. Publish immutable settings snapshots, serialize mutations per resource, and never hold global locks across network I/O or subprocess waits. Context cancellation owns downloads, encoders, event subscriptions and shutdown. Bound connections, request bodies, job queues and per-client event buffers. A single giant handler package was considered but would couple compatibility fixes to mutable transport state.

### 3. Prove device TLS before broad feature work

Retain configurable HTTP/web HTTPS/box HTTPS listeners and route exposure. Reference ports are 80/8443/443; isolated development uses unprivileged alternatives. On box HTTPS, no SNI selects the TB1 certificate and any SNI selects TB2, independently of box generation. Web HTTPS and MQTT have separate TLS policies. Use `GetConfigForClient` or `GetCertificate` with explicit tests for this selection.

Prototype Go TLS 1.2 interoperability, optional client certificate handling, issuer/CN normalization, RSA suites/signatures, resumption, fragment limits and binary RTNL connections using sanitized captures and physical devices. Normal HTTP routing cannot be assumed to accept the reference `*binary` traffic: inspect framing and implement a bounded protocol demultiplexer or dedicated connection handler on the same accepted TLS connection as necessary. Do not replace the cryptographic stack or globally relax certificate validation to work around an unproven mismatch. Unsupported Go TLS behavior is a milestone blocker requiring a documented design/spec amendment.

The legacy TLS policy is to preserve reference RSA/SHA1 negotiation using `GODEBUG=tlssha1=1` only in a child process that owns the box listener. The parent process retains strict Go defaults for web and upstream connections. The same Go executable supplies both modes, with local IPC for box state/requests and supervised shutdown; no C TLS stack is linked. Synthetic negotiation evidence is recorded in `docs/tls-compatibility.md`.

Separate legacy box identity acceptance from web sessions and upstream server verification. Preserve baseline access semantics in an explicit compatibility policy and document any security deviation; do not treat an issuer name as cryptographic proof. Generate missing server material once, preserve supplied certificates, and keep secrets out of fixtures and logs.

### 4. Preserve file contracts and make mutations recoverable

Read/write reference `config/config.ini`, `config/config.overlay.ini`, setting names/types/defaults, box overlays, configured content/library paths, certificates, `content.json`, tonies databases and custom JSON. Preserve unknown persisted keys/fields through edits. Avoid a database migration in this change. Use temp-file publication and serialized updates; preserve the reference custom-tonies backup retention setting (default 10), and make linked-model rename recovery explicit with a journal or equivalent recoverable multi-file transaction.

Centralize root-confined path resolution for read/write/upload/delete/firmware operations, including symlink escapes. Run migration against copies; concurrent C and Go writers against the same tree are unsupported.

### 5. Keep protocol and audio contracts independent of codecs

Generate Go protobuf bindings from upstream proto2 schemas, retaining field numbers, required-field validation and unknown fields. TAF handling must preserve header framing, audio SHA1, length, audio IDs, track/page indexes, Ogg CRC/granule/page layout and baseline 4096-byte block constraints. SHA1 here is a file-format requirement.

Implement format construction and job orchestration in Go. FFmpeg supplies decoding and, if validated, Opus encoding through argument-vector subprocesses; ordinary Ogg output is not presumed to satisfy TAF page constraints. Prototype packet/page handling early and verify generated TAF using the oracle and hardware. Keep stored TAF delivery operational without FFmpeg; report missing codec support on conversion requests. Shell pipelines and embedding the existing C encoder were considered and rejected for portability and build simplicity.

Stream content with bounded buffering and backpressure, support ranges, cancel per-box delivery on stop, and publish cache files only when complete and validated. Apply `live`, `nocloud`, cloud overrides and passthrough/cache policy from metadata and overlays rather than hardcoded global rules.

### 6. Reuse frontend and port integrations through shared services

Distribute or serve a pinned build of the official frontend with attribution and preserve its API/SSE contracts, auth lifecycle, settings schema, content operations, plugin discovery and proxy routes. No frontend development is required. Port ESP32 firmware upload/extraction/patching with validation and fixture tests; patch copied images only.

Preserve MQTT topics/payloads, discovery, embedded server scope found in the inventory, and box event state. Settings commands persist immediately and apply to the box on its next freshness check. Stop cancels server delivery but cannot retract buffered audio. Actual playback volume is telemetry; pause/resume has no protocol support. Reconnect restores subscriptions and discovery using reference backoff behavior.

### 7. Deliver in gated milestones

1. Reference inventory, oracle/fixtures, Go skeleton, TLS/RTNL and TAF feasibility spikes.
2. Runtime, storage and device core with offline local playback and freshness settings.
3. Cloud policy/cache/OTA, complete audio conversion/live jobs and metadata.
4. Frontend management/auth/SSE/proxies/firmware tools.
5. MQTT/HA, remaining inventory entries, migration, packaging and hardware acceptance.

Each milestone has runnable tests and a compatibility report. CI runs Go unit/integration tests, race checks on supported runners, vet, fuzz smoke cases for parsers and path handling, differential suites with a mock cloud, and binary/container smoke checks. Build Linux amd64/arm64, Windows amd64 and macOS amd64/arm64 artifacts; execute native smoke checks where runners exist, and label cross-compilation separately from runtime verification. Container images target Linux amd64/arm64. External FFmpeg remains a documented optional runtime dependency.

## Risks / Trade-offs

- Legacy TLS or binary transport mismatch → resolve with the first milestone before building on an incompatible listener.
- Codec output fails Toniebox block/page requirements → prototype TAF construction early and gate conversion on hardware playback and structural validation.
- Broad rewrite scope hides omissions → exhaustive source-indexed inventory, milestone gates and no parity declaration while pending rows remain.
- Shared-data corruption or partial linked rename → single-writer operation, copied migration data, serialized atomic publication and crash recovery tests.
- Oracle unavailable due to missing submodules → populate only an isolated reference checkout; retain recorded sanitized fixtures with provenance for normal CI.
- Sensitive captures or firmware redistributability → synthetic/sanitized fixtures and locally supplied private test images; retain provenance and asset/dependency notices.
- Reference licensing and frontend dependencies → preserve the upstream GPLv2 license and attribution for derived work, inventory individual component licenses before distribution, and record all bundled asset notices.
- Hardware unavailable → mark device validation pending; automated compatibility does not close hardware acceptance tasks.

## Migration Plan

1. Snapshot the current C configuration, certificates and data; record its version and hashes.
2. Copy the deployment into an isolated Go test directory and run configuration validation without writes, certificate regeneration or upstream contact.
3. Exercise frontend, mock-cloud and device tests on separate ports/network routing; compare the compatibility report.
4. Stop the C writer, snapshot again, deploy Go against the chosen data copy and switch device routing only after successful checks.
5. Roll back by stopping Go, restoring the pre-cutover snapshot and routing to the pinned C deployment. Never mix concurrent writers or assume Go-mutated files can be rolled back without comparison.

## Open Questions

- Which physical TB1 chip/firmware variants and TB2 devices are available? The inventory must identify what the baseline implements and validation must record tested versions.
- Do real ClientHello/RTNL captures expose a Go TLS or framing limitation? Resolve in milestone 1; do not guess from enabled C cipher macros alone.
- Can the chosen FFmpeg packet stream support all TAF alignment, chapter and live semantics without cgo? Resolve with the early conversion spike.
- What canonical Go module/release identity should be used? Choose a local module initially if no repository remote is established; this does not block the proposal.

## Audited implementation decisions (2026-10-09)

The completed software audit records every route, settings/CLI row and integration
with scoped evidence. Deliberate departures are retained explicitly in
`docs/settings-audit.md` and `docs/compatibility-report.md`: V3 nocloud exclusion,
stronger validated/atomic caches and writes, bounded/scoped live jobs, MQTT command
validation/acknowledgments, TLS-only security policy, immediate persisted hard-lock
updates, bounded handler-level diagnostics and structured token-free normal logs.
Native C pointer/build/process-state diagnostics are not recreated as live Go
fields. These decisions do not close the physical/frontend acceptance gates or
establish replacement parity. The original C checkout stays unchanged.

## Development branch extension

Pin official develop `8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1` and its frontend `636a8d65043f6c6e322f76bcb11e958725159e96` in a separate oracle checkout. Management authentication is process-owned, mutex-protected and bounded; users persist atomically in the reference JSON format. Cookie/bearer sessions remain separate from box identity, with preflight/public SPA dispatch before authorization. Listened flags use existing confined metadata roots and preserve unknown fields. Track exports stream checked Ogg blocks to private temporary files and use Go archive/zip. Firmware port patches operate on validated copies and publish only after checksum repair. Generic download dialing resolves once and validates the literal address used for connection. C-only allocation and compiler changes are audited against Go equivalents instead of copying unsafe implementation details. See docs/develop-port.md for deliberate stronger validation and remaining physical/frontend gates.


## Locking review follow-up

Configuration reads use immutable atomic publication; a durable mutation mutex and short state-publication mutex allow runtime updates during disk I/O while preserving failed-write rollback and merging the latest transient state. Confined resource tokens share canonical destination identities across separately opened/nested roots, with sorted acquisition and cancellation. Content mutation barriers preserve linked-rename transactions through a cancellable writer-preferring gate. File selection/open stays serialized with migration; validation and response streaming leave state locks. Image fetch/flush work has cancellable per-entry ownership. Live/playlist publication releases destination coordination after encoding, independently of client streaming, and joins its worker before closing the private spool. Necessary event/transaction/lifecycle serialization remains in place. Evidence and the original audit rationale are in docs/locking-review.md.

## Selectable deployment storage

`storage.Backend` is injected before configuration loading and propagated through immutable settings snapshots and owned service constructors. `storage.Root` retains confinement and shared resource tokens; `storage.File` permits seekable filesystem and remote handles without leaking concrete `os.File` into persistence consumers. Backend selection is startup environment configuration, with filesystem default and generic S3 endpoint/bucket/prefix/path-style options. A small standard-library S3 REST client signs requests with Signature V4 and loads environment or shared profile credentials outside persisted settings; no vendor SDK is linked. S3 maps deployment-relative paths to a bucket prefix and rejects outside-base paths, with no filesystem fallback.

S3 immutable readers use bounded conditional byte ranges against the version/ETag observed at open. Writable files stage privately on local scratch and publish with checked Sync/Close. Atomic stream publication uploads a completed bounded private file directly to the final key. Directory markers retain empty directories and listings paginate. Single-object moves copy then delete; directory moves copy the bounded source snapshot before deleting any source. Multi-object visibility and delete-failure duplicates differ from POSIX atomic rename and require a single writer plus operator recovery after interruptions. Remote externally growing TAFs are rejected explicitly; application-generated live audio still streams from local scratch and publishes complete output remotely. Child TLS key logs are authenticated to the parent's private endpoint so only the parent owns persistence. See docs/storage.md for operational boundaries and verification.
