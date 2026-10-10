## Why

Rewrite teddyCloud's C backend in Go to simplify its build, concurrency, maintenance, and deployment while retaining the local-cloud behavior that existing Tonieboxes and the official web frontend depend on. The official C implementation is the behavioral reference; the rewrite must demonstrate compatibility rather than merely expose similarly named endpoints.

## What Changes

- Introduce a standalone Go backend in this repository, using the local `../teddycloud` checkout at commit `78c3b38e36f665d3a5fffe907f73a02725176377` as the initial reference baseline.
- Port the reference device protocols, HTTP/TLS listeners, box identity and settings overlays, local content delivery, cloud forwarding/cache policy, OTA handling, and RTNL state reporting.
- Preserve existing configuration, certificates, content/library layouts, metadata, management APIs, authentication, SSE, and compatibility with the separately maintained official web frontend.
- Implement TAF parsing/writing, conversion and live streaming, MQTT/Home Assistant integration, reverse proxies, and firmware/certificate utilities in staged milestones.
- Use a Go backend without linking the C implementation; allow an explicit external FFmpeg dependency for audio processing. Record all dependencies and redistributed asset licenses and retain upstream attribution.
- Establish a source-indexed compatibility inventory, sanitized reference fixtures, differential checks, hardware validation, migration instructions, and reproducible builds before declaring replacement parity.

## Capabilities

### New Capabilities

- `reference-compatibility`: Pinned reference inventory, behavioral comparison, compatibility exceptions, and release acceptance gates.
- `server-runtime`: Go process lifecycle, configuration/overlay persistence, certificates, listener separation, authentication, and portable deployment.
- `toniebox-cloud-protocol`: Device identity, time/claim/freshness/content/OTA/reset/setup protocols, cloud policy, and RTNL ingestion.
- `content-audio-pipeline`: Content/library metadata, TAF integrity, range delivery, caching, conversion, playlists, and live streaming.
- `web-management`: Official frontend assets and API contracts, settings/content administration, custom tonies, SSE, proxies, and firmware utilities.
- `mqtt-home-assistant`: MQTT telemetry, discovery, persisted box commands, embedded broker behavior where present, and reconnect recovery.

### Modified Capabilities

None. This repository has no existing capability specifications.

## Impact

This is a new implementation in an otherwise unimplemented repository. Expected code areas are `cmd/goteddycloud`, internal service packages, generated protobuf bindings, fixture/integration tests, packaging, and documentation. Compatibility affects existing teddyCloud deployments and the official frontend; development and migration tests use isolated copies of data and non-production ports. The C checkout remains a read-only reference. Rewriting the frontend, adding protocol behavior absent from the baseline, and porting its WASM build or vendored C networking stack are outside this change. TB2/v3 routes present in the baseline are included, but route availability alone is not evidence of complete TB2 hardware support.

## Development branch extension

The user additionally requested porting all official `develop` changes. Pin `8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1` as the development target, preserve the original master oracle, and implement its added behavior and security fixes. Audit C-only memory/build changes against the corresponding Go implementation. Physical-device and frontend acceptance gates remain evidence-based.

## Selectable storage extension

The user additionally requested that all deployment storage be abstracted behind a selectable filesystem or generic S3-compatible backend. Filesystem remains the default. This covers durable settings, metadata, identities, audio, firmware, web assets, caches and diagnostic files. Temporary FFmpeg inputs, live spools and private upload staging remain local scratch and publish completed artifacts to the selected backend. Provider-specific behavior and unavailable live-service evidence must be explicit.
