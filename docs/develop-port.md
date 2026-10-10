# Official development branch port

Target C commit: `8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1`. Frontend: `636a8d65043f6c6e322f76bcb11e958725159e96`. The original master pin `78c3b38e36f665d3a5fffe907f73a02725176377` remains available as a historical oracle. The sibling checkout is unchanged.

`develop-inventory.json` classifies all 114 commits and 109 changed files, including 29 runtime translation units. `reference-inventory.json` now indexes 76 routes, 269 settings/tree entries and 25 CLI options. C ownership, buffer and compiler fixes map to checked Go APIs and tests; the C networking stack, WASM build and developer-specific configuration are not copied into the Go backend. Frontend assets are built from the pinned official frontend.

## New behavior

- Optional management login: users in `config/web_users.json`, reference-compatible salted SHA256 hashes, 64 bounded seven-day sessions, cookie/bearer authentication, five-failure/ten-minute address limits, user/password management and `TEDDYCLOUD_WEB_AUTH_DISABLE=1|true|yes` override. User files are published atomically with mode 0600. The management gate does not replace box identity. CORS preflight and the public SPA remain available before login.
- `cloud.autoMarkListenedOnSync=true`: assigned TAF source sidecars gain a listened flag on local content download. This is a download heuristic. The tag metadata remains separate. `/api/fileSetListened` changes a file's flag, fileIndexV2 reports it and hides sidecars while their sibling exists. Unknown sidecar fields survive updates.
- TAF `lengthSeconds` uses the reference's final-block granule definition. `/content/<file>?tracks=1,2&special=library` exports exact Ogg blocks, adding the first 512 header bytes for later chapters. Multiple selections produce ZIP entries named from sanitized catalog titles.
- `--esp32-portpatch FILE --port PORT` and firmware API `port`: pattern-match supported Xtensa connect functions, patch every matching function and repair image checksums; 443 restores original instructions. Ports 1–32767 are supported. Original-preserving/atomic firmware publication remains enforced.
- New box overlays pin their own certificate directory and paths. Uploads create that directory privately and select the uploaded certificate paths. Box and embedded MQTT TLS session tickets are disabled so each connection establishes identity anew; web TLS retains its independent policy.
- `core.allowPrivateFetch=false`: image, metadata and generic downloads validate DNS results and dial validated literals, preventing rebinding. Private downloads require explicit opt-in. Configured cloud forwarding remains separate. IPv6 receives equivalent protections.
- Root always redirects to `/web`; the legacy frontend bundle and `core.new_webgui_as_default` are removed. CC3235 is generation TB1. Domain/crawler detection is independent. Secret settings are omitted from every settings index. OTA hashes accept plain hex only. RTNL CSV doubles quotes and escapes line endings. Growing files retry EOF, and forced range restarts use the retained stream prefix without starting an encoder.

## Login setup and migration

Start from a consistent copied deployment, as described in `migration.md`. Copy `web_users.json` with mode 0600 when migrating from develop. Sessions and login limits are process-local and reset at restart, as in C. While protection is disabled, POST JSON `{ "username": "admin", "password": "choose-a-password" }` to `/api/auth/users/create`, then POST `{ "enabled": true }` to `/api/auth/enabled`. Use HTTPS for login. The official frontend exposes these controls. Role-based access and private areas extend this login system; see [user areas](user-areas.md). The recovery environment override bypasses enforcement without deleting users or changing the persisted setting.

The development frontend should be installed as a complete asset build; remove the previous legacy.html/library React bundle from copied web assets. Removed settings remain preserved as unknown INI fields but are no longer active options. Private NAS image URLs require `core.allowPrivateFetch=true` intentionally. Existing box overlays retain their configured credential paths; new boxes receive isolated paths.

## Deliberate stronger validation

Go retains bounded uploads/exports, request cancellation, confined roots, complete identity validation and atomic persistence. Invalid certificate specimens accepted by C are rejected. Corrupt or oversized user databases fail startup instead of silently disabling protection. Failed user persistence retains the prior in-memory users. Export selections are limited to 99 unique tracks and 512 MiB of total output; duplicate selections are rejected rather than depending on an external zip error. ZIP metadata/compressed bytes may differ, while entry names and Ogg bytes match. Unsatisfiable ranges receive 416 instead of C's fallback to the full response. Same-origin redirect and verified upstream TLS constraints remain stronger than C. See `verification.md` for older deliberate differences still applicable.

## Verification and limits

Run `scripts/reference.sh build`, `scripts/frontend.sh build`, `scripts/oracle-suite.sh`, and the isolated `scripts/develop-differential.py` harness with pytest available. Rehearsal directories must be unused; the harness refuses existing deployments and always stops its servers. `--stage PATH --hardening-only` exercises the upstream settings, secrets, private-fetch and connection-pool regressions independently. The upstream simulator helper uses the C CLI solely to generate synthetic client certificates for both servers. Its TB2/firmware-capture stubs stay skipped.

Software verification evidence is recorded in `verification.md`. Neither simulator profiles nor an official frontend build establish physical playback or interactive frontend acceptance. No box is available; the user identified CC3200 as the intended later target. Those five original acceptance tasks remain open.

Development cipher limitation: the pinned C oracle accepts RSA AES256-CBC-SHA256 (`0x003d`), which Go crypto/tls does not implement. AES128-CBC-SHA256 is enabled on the isolated box listener. The unsupported AES256 case is retained as a strict expected failure by `scripts/develop_pytest_policy.py`; all other simulator assertions remain mandatory. Normal simulated CC3200 negotiation succeeds. This is a compatibility deviation, not physical-device acceptance.
