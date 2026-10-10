## 1. Reference baseline and feasibility (milestone 1)

Implementation note (2026-10-09): the selected TLS compatibility design is implemented. Legacy
RSA/SHA1 negotiation uses a supervised box-listener Go child process with
`GODEBUG=tlssha1=1`; web/upstream TLS stays in the strict parent process.
Hardware support remains separately evidenced. See `docs/tls-compatibility.md`.

- [x] 1.1 Record reference commit/submodule revisions and create docs/compatibility.md with an exhaustive method/path/listener/handler inventory, including binary RTNL and static routes.
- [x] 1.2 Extend the inventory with every settings key/default/type, configuration format, CLI mode, data layout, integration and firmware/certificate utility; map each to a milestone and acceptance check.
- [x] 1.3 Inventory upstream/component/frontend licenses and attribution; establish the derived project's license and asset notices before copying source/schema/assets.
- [x] 1.4 Create a separate pinned C oracle checkout with its submodules and a repeatable isolated build/run script; leave ../teddycloud unchanged.
- [x] 1.5 Create synthetic configuration, two-box, tag, TAF and mock-cloud fixtures with provenance and a documented secret-sanitization process.
- [x] 1.6 Bootstrap go.mod, cmd/goteddycloud and internal package boundaries; pin the toolchain/dependencies and verify a CGO_ENABLED=0 build.
- [x] 1.7 Build the differential runner comparing status/headers, normalized JSON/protobuf, bytes, events and file effects; define nondeterminism explicitly and prove a deliberate mismatch fails.
- [x] 1.8 Prototype box TLS 1.2, SNI certificate selection, optional client certificates, legacy suites/signatures and session behavior; record synthetic and available hardware results separately.
- [x] 1.9 Inspect RTNL framing and prototype HTTP/binary dispatch on the reference transport, verifying fragmented and oversized messages.
- [x] 1.10 Prototype FFmpeg-to-Go Opus/Ogg packet handling and TAF block/page construction using a multi-track fixture; compare structure with the C oracle and record available hardware playback.
- [x] 1.11 Publish milestone 1 feasibility results; resolve TLS/RTNL/TAF blockers or amend design/specs explicitly before dependent work.

## 2. Runtime, configuration and persistence (milestone 2)

- [x] 2.1 Implement base-path and reference CLI startup/options/version behavior with startup diagnostics and explicit resource limits.
- [x] 2.2 Implement config.ini/config.overlay.ini parsing, typed validation/defaults, inheritance and unknown-key preservation; round-trip reference fixtures.
- [x] 2.3 Implement atomic settings persistence and shared per-box mutation services; verify concurrent updates and restart isolation.
- [x] 2.4 Implement confined filesystem resolution including symlinks, atomic file publication and per-resource locking; verify outside-root writes cannot occur.
- [x] 2.5 Implement certificate import/loading and one-time generation, separate web/box TLS policies and validated SNI selection from the prototype.
- [x] 2.6 Implement HTTP/web HTTPS/box HTTPS listeners with reference route exposure, bind-failure cleanup, cancellation and graceful shutdown tests.
- [x] 2.7 Implement explicit legacy identity policy, CN normalization, box discovery/registration and overlay access without conflating identity with web sessions.

## 3. Device core and offline content (milestone 2)

- [x] 3.1 Copy attributed proto2 schemas, generate pinned Go bindings and test required fields, unknown fields and malformed payloads.
- [x] 3.2 Implement existing content/library UID resolution, content.json and tonies metadata readers preserving unknown fields.
- [x] 3.3 Implement TAF parsing and structural/hash/length validation with valid, truncated and corrupt fixtures.
- [x] 3.4 Implement bounded stored-content delivery for /v1/content and /v2/content with reference range and error semantics.
- [x] 3.5 Implement /v1/time, /v1/claim and /v1/cloud-reset local behavior; compare statuses and state effects with fixtures.
- [x] 3.6 Implement /v1/freshness-check local marking/filtering and effective per-box speaker/headphone/slap/LED settings.
- [x] 3.7 Implement RTNL transport/parser, /v1/log handling, box state/event updates and malformed-client isolation.
- [ ] 3.8 Run milestone 2 differential checks and offline playback/settings verification; record device firmware and leave unavailable hardware checks pending.

## 4. Cloud forwarding, cache and OTA (milestone 3)

- [x] 4.1 Implement upstream clients with configured identity/credentials, verification, deadlines/cancellation and redacted diagnostics against a mock cloud.
- [x] 4.2 Implement reference cloud enablement, passthrough, nocloud/live/override and claim/freshness/content/log/reset forwarding decisions; assert excluded tags never reach the mock upstream.
- [x] 4.3 Implement content caching and single-key publication coordination; verify concurrent misses, cancellation, partial transfers and valid-cache preservation.
- [x] 4.4 Implement /v1/ota local selection, forwarding and cache behavior with device-specific storage and interrupted-download tests.
- [x] 4.5 Implement /v3/freshness-check and /v3/setup-status reference contracts with decoded fixture comparisons.
- [x] 4.6 Implement /v3/check-ota and /v3/ota contracts, selection and cache side effects.
- [x] 4.7 Implement /v3/chapter and /v3/content-meta contracts with reference content mappings and failure cases.
- [x] 4.8 Run cloud/OTA policy and failure differential suites; document which v3 behavior is hardware-verified.

## 5. Audio production and live delivery (milestone 3)

- [x] 5.1 Implement Go TAF header/Ogg page construction, CRC/hash/length finalization and track offsets based on the accepted spike.
- [x] 5.2 Implement FFmpeg argument-vector process ownership, dependency detection, bounded jobs and cancellation without shell pipelines.
- [x] 5.3 Implement file and PCM conversion services, validating generated TAF before atomic publication.
- [x] 5.4 Implement multi-file playlists, chapter transitions, seek/skip and append semantics found in the inventory.
- [x] 5.5 Implement stream/webradio conversion and live/growing-content delivery with bounded buffering and per-box cancellation.
- [x] 5.6 Verify codec failure cleanup, missing-FFmpeg behavior, concurrent box isolation and generated-file structural compatibility.
- [ ] 5.7 Record milestone 3 conversion/live playback results on available hardware and complete the audio compatibility rows only with required evidence.

## 6. Official frontend and management APIs (milestone 4)

- [x] 6.1 Obtain and pin the official frontend revision/build assets; implement /web SPA fallback, static/legacy serving, MIME behavior and plugin discovery with attribution.
- [x] 6.2 Implement reference login/logout/token refresh and management authorization behavior; compare baseline placeholder-token and unauthenticated request sequences; document that the pinned reference does not enforce management sessions.
- [x] 6.3 Implement settings index/get/set/reset/removeOverlay APIs using shared persistence services and matching frontend schemas.
- [x] 6.4 Implement box/tag listing/info/assignment, toniebox metadata and trigger APIs against fixtures.
- [x] 6.5 Implement tonies metadata search/update/reload APIs with atomic snapshot replacement and upstream failure handling.
- [x] 6.6 Implement custom-tonies get/upsert/delete normalization, validation, backup retention and active reload; adapt reference API tests.
- [x] 6.7 Implement custom model rename with linked content.json updates and recovery; verify collision, opt-out, restart and interrupted multi-file update cases.
- [x] 6.8 Implement content/json and library index/download/upload APIs with confined paths and reference metadata behavior.
- [x] 6.9 Implement file/directory create/move/delete and content-to-library migration; test conflicts, symlink escapes and partial failure recovery.
- [x] 6.10 Expose fileEncode/pcmUpload/tafUpload through the audio/content services and verify response/error contracts.
- [x] 6.11 Implement stats, cache download/flush/stats and SSE names/payloads with bounded subscriber queues and disconnect cleanup.
- [x] 6.12 Implement /reverse and /reverseGeneric configured target, auth, forwarding and failure contracts using mock targets.
- [x] 6.13 Implement certificate upload/DER downloads and ESP32 image validation/certificate extraction against synthetic or private fixtures.
- [x] 6.14 Implement ESP32 firmware patch/output workflows, verifying checksums/layout and preservation of original images on failure.
- [ ] 6.15 Exercise the official frontend for login, box settings, tag assignment, file conversion, custom model CRUD and event updates; close milestone 4 differential gaps.

## 7. MQTT and Home Assistant (milestone 5)

- [x] 7.1 Implement reference MQTT connection/TLS configuration, telemetry/event topics and payloads from shared box state.
- [x] 7.2 Implement Home Assistant discovery/entities, availability and state publication; compare discovery JSON with reference fixtures.
- [x] 7.3 Implement shared validated speaker/headphone/LED/slap/stop commands and expose reference MQTT and REST command contracts.
- [x] 7.4 Verify immediate overlay persistence, next-freshness application, invalid-command rejection and per-box server-side stop without unsupported playback controls.
- [x] 7.5 Implement reference reconnect backoff/reset, restored subscriptions/state/discovery and broker-outage tests using a controllable clock.
- [x] 7.6 Implement inventory-listed embedded MQTT server/listener and client behavior with separate TLS policy and protocol tests.

## 8. Acceptance, packaging and migration (milestone 5)

- [x] 8.1 Audit every inventory row against implementation and evidence; add individually scoped tasks for discovered gaps and complete them before parity declaration.
- [x] 8.2 Add CI for Go tests/vet, supported race runs, bounded parser/path fuzz smoke tests and isolated mock-cloud differential suites.
- [x] 8.3 Add versioned CGO_ENABLED=0 build automation for Linux amd64/arm64, Windows amd64 and macOS amd64/arm64; attach license and dependency notices.
- [x] 8.4 Add Linux amd64/arm64 container packaging with persistent mounts, configurable unprivileged ports and optional FFmpeg support.
- [x] 8.5 Run native startup/config/persistence/shutdown smoke checks on available platform runners and container checks; distinguish build-only targets in the report.
- [x] 8.6 Implement read-only deployment validation and document copied-data migration, single-writer cutover, snapshots and rollback; rehearse both migration and restoration.
- [ ] 8.7 Execute physical-device TLS, claim/freshness, cached/local playback, seek/chapters, conversion/live, OTA-policy and MQTT-settings acceptance per available chip/firmware; retain pending tasks for unavailable required targets.
- [ ] 8.8 Publish the final compatibility/support report with all deviations and evidence; verify no in-scope entries remain pending before declaring the rewrite a compatible replacement.

## Audit gaps discovered during 8.1

- [x] 9.1 Implement and compare security mitigation domain/crawler/robots detection, runtime incident state and configured frontend locking.
- [x] 9.2 Implement bounded opt-in PCAP diagnostics with sanitized synthetic-fixture verification and document TLS diagnostic differences.
- [x] 9.3 Audit settings consumers; implement configured CORS, outbound connection timeout and startup metadata updates, and record intentional validation/logging/runtime-state differences per setting.

## 10. Official develop branch port (8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1)

- [x] 10.1 Pin the official develop source and frontend; inventory all commits against the master baseline and classify behavior, Go equivalents and build-only changes.
- [x] 10.2 Port persistent web users, cookie/bearer sessions, login limits, management authorization and auth APIs.
- [x] 10.3 Port CORS preflight, secret-setting exclusion, new settings, root redirect, CC3235 generation and independent security detectors.
- [x] 10.4 Port listened sidecars, file indexes and automatic listened marking on library sync.
- [x] 10.5 Port TAF duration reporting and selected-track downloads.
- [x] 10.6 Port ESP32 connection-port patching through firmware API and CLI, preserving checksum validation.
- [x] 10.7 Port certificate upload isolation/private permissions and disable box TLS session resumption.
- [x] 10.8 Apply private-network download protection and audit remaining development hardening and stream changes.
- [x] 10.9 Build the develop oracle and run differential, simulator and regression checks; document outstanding physical/frontend gates separately.
- [x] 10.10 Update compatibility evidence, migration documentation and reproducible development artifacts for the new pinned baseline.

## 11. Go practice review follow-up

- [x] 11.1 Fix MQTT listener-failure cleanup, CONNECT cancellation and bounded ordered command processing; add lifecycle/protocol regression tests.
- [x] 11.2 Drain HTTP handlers during bounded graceful shutdown before application teardown; test handler cleanup and deadline fallback.
- [x] 11.3 Move content metadata callbacks outside the global box-state lock while preserving per-box state/event ordering.
- [x] 11.4 Replace custom-catalog error-string dispatch with typed errors while preserving reference HTTP messages/statuses.
- [x] 11.5 Restrict event subscriptions to receive-only channels, tidy module metadata and resolve Staticcheck findings without changing reference responses.
- [x] 11.6 Run full race/vet/Staticcheck and relevant C differentials, rebuild the local binary and record evidence.

## 12. Locking review follow-up

- [x] 12.1 Publish immutable atomic settings state, preserve durable rollback/runtime updates and fast-path known identities.
- [x] 12.2 Make image fetch/flush coordination cancellable and release locks before HTTP streaming; separate RTNL/reverse/freshness locks.
- [x] 12.3 Propagate request cancellation through content mutation/inspection/listened waits; shorten assignment reservation and file-validation critical sections.
- [x] 12.4 Coordinate shared live/playlist destinations and firmware/certificate transactions without locking HTTP responses.
- [x] 12.5 Shorten audio spool and MQTT telemetry critical sections, remove redundant locks and fix resource identity/cancellation details.
- [x] 12.6 Add focused concurrency regressions, run race/vet/Staticcheck and differentials, measure mixed-workload contention and rebuild the local binary.

## 13. Selectable deployment storage

- [x] 13.1 Introduce injected backend and seekable file contracts, preserving confined filesystem roots and shared resource coordination.
- [x] 13.2 Implement provider-neutral S3 selection, environment/shared-profile credentials, confined key mapping, version-consistent range reads, staged writes, listings and file/directory moves.
- [x] 13.3 Route durable configuration, users, catalog, content, caches, firmware, certificates, web/plugin assets and diagnostics through the selected backend; relay child TLS diagnostics to the parent.
- [x] 13.4 Add backend contract, failure/cancellation and S3 application persistence/audio regressions, including proof that remote deployments do not create local durable files.
- [x] 13.5 Document configuration, migration, permissions, scratch storage and S3 consistency limitations; retain dependency notices in release packaging.
- [x] 13.6 Run race/vet/Staticcheck, compatibility and native smoke checks, cross-build supported targets and rebuild the local binary.
