# goTeddyCloud

Go rewrite of [teddyCloud](https://github.com/toniebox-reverse-engineering/teddycloud),
based on official `develop` commit `8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1`
(frontend `636a8d65043f6c6e322f76bcb11e958725159e96`).
**Incomplete; not yet a compatible replacement for teddyCloud.**

Licensed under the [GNU General Public License version 2](LICENSE)
(`GPL-2.0-only`), matching teddyCloud's project license. See [NOTICE](NOTICE)
for upstream attribution and [third-party license notices](docs/licenses.md).

## Docker Compose usage

The [Compose example](docker-compose.yml) uses the versioned GHCR image. Docker
selects amd64, ARMv7 or arm64 automatically. Configuration, certificates and
audio persist in a named volume. The image runs as UID/GID 65532 and includes
FFmpeg and the integrated Web UI.

```sh
cp .env.example .env
docker compose pull
docker compose up -d
docker compose logs -f goteddycloud
```

Management HTTP is at `http://127.0.0.1:18081`; management HTTPS is on 19443.
The box port is **19444**, matching the prepared `tc.gehann.it` port patch.
All three host bindings default to loopback. For a box on your LAN, bind its
port to this computer's actual LAN IP, for example:

```sh
BOX_BIND_IP=192.168.1.10 docker compose up -d
```

Keep that address in a local `.env` file to reuse it. `HTTP_PORT`, `HTTPS_PORT`
and `BOX_PORT` override host ports; `GOTEDDYCLOUD_IMAGE` selects another image.
Stop a native test instance using the same ports, or select unused host ports.
An external TCP proxy may forward the chosen box port to the loopback binding.
The box listener uses legacy name-based identity; restrict it to trusted networks.

Images built from these sources include the pinned frontend with this project's
UI additions. Open `http://127.0.0.1:18081/web/`; no separate frontend installation
is needed. The assets live at `/usr/share/goteddycloud/www/web`, outside `/data`,
so both named volumes and host directory mounts work and image upgrades update
the bundled UI. Existing containers need a fresh image pull and recreation to
use the updated image.

To build and use the updated image locally:

```sh
docker build --target ffmpeg -t goteddycloud:local .
GOTEDDYCLOUD_IMAGE=goteddycloud:local docker compose up -d
```

A complete custom frontend in `/data/data/www/web` (or the configured
`core.wwwdir`'s `web` subdirectory) overrides the bundled UI. Its `index.html` and
assets are served together. Remove that custom build to resume using the image's
UI. Source attribution and dependency notices are retained in the image; see the
[frontend license inventory](docs/licenses.md) for unresolved upstream terms.

In **Settings → Users and areas**, create the first administrator while access is
private, then enable **Require web login**. Login is off by default. Use a trusted
HTTPS reverse proxy or configure [management HTTPS](docs/management-tls.md) before
exposing management access. The current CSRF/origin, password-storage and global
lockout gaps still require hardening; enabling login alone does not clear this
release for direct internet exposure. See the security notes in the
[0.1.0 release](https://github.com/teian/goteddycloud/releases/tag/v0.1.0).

To upgrade, select the new release tag in `.env`, then pull and recreate the
container. Stop the deployment with `down`:

```sh
docker compose pull
docker compose up -d
docker compose down
```

`down` preserves the named data volume; `down -v` deletes it. To build locally,
use `docker build --target ffmpeg -t goteddycloud:local .`, then run Compose with
`GOTEDDYCLOUD_IMAGE=goteddycloud:local`. See [release packaging](docs/packaging.md)
and [reverse proxy setup](docs/reverse-proxy.md) for more options.

Startup downloads the Tonie and box model catalogs from `api.revvox.de`, including
its redirects to the official GitHub mirror. Failed updates retain existing
catalogs. Docker images include both catalogs from the official GitHub release
branch at image build time, so a fresh deployment can identify known Tonies
even when downloading updates is unavailable. Missing or empty catalogs are
seeded from this baseline; populated catalogs and custom entries are preserved.
The warning's
`error` field distinguishes a rejected HTTP response from transfer or catalog errors.

Tonie titles and pictures are matched against this catalog using the audio ID
reported by the box's freshness check, even when its audio is already on the
box's SD card and has never been downloaded to goTeddyCloud. Reported IDs are
retained so a later catalog update can resolve previously unknown tags. After
upgrading, let the box sync again, then reload the Tonies page. A tag UID alone
does not identify its title; special audio ID 1 requires a local audio header/hash
or a manually assigned model. Existing model assignments are preserved.

With cloud content caching enabled, authenticated claims and RTNL tag
presentations also backfill missing original audio when the box plays from its
SD card. RTNL backfills reuse tag credentials saved from an earlier device
request; a UID alone is insufficient to download audio. With library caching
enabled, existing UID caches are moved to the library on the next presentation,
including after an earlier library publication failure, without another download.

The UI reads `/api/cloud/status`, which reports disabled or unavailable cloud
access as HTTP 200 status data. The local `/api/time` clock remains independent.
Raw `/reverse/v1/time` requests still proxy the upstream Tonie cloud and need a
complete upstream CA/client certificate/key identity
in `core.client_cert.*`. A fresh deployment has no manufacturer client identity,
so raw reverse requests return 502 and the cloud indicator stays offline. Import your own
valid identity for cloud access, or disable `cloud.enabled` in Settings for offline
operation. Web HTTPS certificates serve a separate purpose. See
[upstream cloud access and legacy hostname verification](docs/upstream-cloud.md).

The box API and RTNL require [explicitly imported box client certificates](docs/box-certificate-auth.md). Unknown certificates are rejected during TLS, including certificates which copy a known box name or issuer. Existing deployments must import the actual box identities before upgrading to this policy.

## Native build and usage

Requires Go 1.26.8. The backend builds with `CGO_ENABLED=0`. External FFmpeg is
optional for stored audio delivery and required for conversion. Implemented areas
include configuration/overlays, certificates and listener lifecycle, offline device
claim/freshness/content, settings APIs, management users, per-area roles and private/shared user areas, RTNL state,
SSE queues, custom catalog CRUD and recoverable rename, metadata update/search,
local playlists, audio uploads, file mutation and library migration, reverse
proxies, Go TAF construction and live streams, cloud/OTA/V3 forwarding and caches,
MQTT/HA, firmware tools, security mitigation and opt-in diagnostics. Automated
checks cover these implementations; official frontend interaction and physical
device acceptance remain pending. See the inventory for scoped deviations.

```sh
CGO_ENABLED=0 go build -buildvcs=false -o bin/goteddycloud ./cmd/goteddycloud
bin/goteddycloud -base_path /path/to/copied/deployment -validate
bin/goteddycloud -base_path /path/to/copied/deployment \
  -http 127.0.0.1:18081 -httpsecure 127.0.0.1:19443 -box 127.0.0.1:19444
go test -buildvcs=false ./...
```

Deployment storage supports filesystem (default) or generic S3-compatible endpoints.
See [storage configuration and migration](docs/storage.md).

Default ports match C: 80 HTTP, 8443 web HTTPS, 443 box HTTPS; use `off` to disable
a listener. Web UI and management APIs share `-http`/`-httpsecure`; device APIs use
`-box` exclusively. See [reverse proxy setup](docs/reverse-proxy.md) for Nginx, Caddy and Traefik
examples, separate public ports and trusted proxy headers. Missing server material is generated once during normal startup;
invalid imported identities fail without replacement. Management HTTPS supports
[manual certificates or config-enabled ACME/Let's Encrypt](docs/management-tls.md). `-validate` never generates,
writes, listens or contacts the Tonie cloud; it reads the selected storage backend. `-config-set key=value` persists a global
setting and exits. `-listen` retains the HTTP-only probe mode.

Box TLS uses a supervised child of the same Go executable with the legacy
signature setting enabled; parent web/upstream TLS remains strict. The inherited
box identity policy is name-based. Optional web login protects management APIs
with cookie or bearer sessions; it is disabled by default. See the
[development port](docs/develop-port.md) for login setup and changes. See [TLS policy](docs/tls-compatibility.md).

Build the isolated C oracle and generate private synthetic data:

```sh
scripts/reference.sh build
go run -buildvcs=false ./cmd/fixturegen
scripts/reference.sh run
```

The oracle uses loopback 18080/18443/18444 with cloud/MQTT disabled. Generated
keys/audio stay under ignored `.cache/`; fixture generation rejects existing
folders. Scripts leave `../teddycloud` unchanged. Run only one writer per fixture
copy and never point these tests at production deployment data.

```sh
go run -buildvcs=false ./cmd/compatcheck -cases testdata/device-offline.json
go run -buildvcs=false ./cmd/compatcheck -cases testdata/auth-events.json
python3 scripts/migration-rehearsal.py --binary bin/goteddycloud
```

See [compatibility/support report](docs/compatibility-report.md),
[compatibility inventory](docs/compatibility.md), [feasibility](docs/feasibility.md),
[verification](docs/verification.md), [migration](docs/migration.md),
[license notices](docs/licenses.md), and the
[implementation checklist](openspec/changes/rewrite-teddycloud-in-go/tasks.md).
CC3200 hardware is currently unavailable; physical acceptance remains pending.

Publishing a GitHub release builds Windows/Linux/macOS archives for the server
and standalone `cc3200tool`, Linux `.deb`/`.rpm`/Arch packages, a Nix package, and
GHCR images for amd64, ARMv7 and arm64. See [release packaging](docs/packaging.md)
for supported targets, installation and image tags.

The CC3200 ESP32-C3 browser flasher is available at `/tools/cc3200` and as the
built-in **CC3200 flashing** frontend plugin. It creates verified backups, imports
certificates, and installs the replacement CA and HackieboxNG preloader with
read-back verification. See [CC3200 flashing](docs/cc3200-flashing.md) for setup,
supported ROM revisions and hardware-validation limits.

Audio CLI (requires FFmpeg):

```sh
goteddycloud --encode output.taf --skip-seconds 1 chapter1.wav chapter2.wav
```

The skip duration applies to every input. Existing valid TAFs may also be inputs.
Output is validated before atomic publication; encoding failure preserves an
existing destination. This utility exits without opening server listeners.

Roles and private/shared content/box areas with multiple members are documented in [user areas](docs/user-areas.md). The integrated Web UI provides **Settings → Users and areas** (`/web/settings/users`) and **Tonies → Shared catalogs** (`/web/tonies/shared`). Users can share entire stored catalogs or selected audio files read-only. Build the frontend with `scripts/frontend.sh build`; see [frontend integration](frontend/README.md). The standalone tools remain available as a fallback.

The [native CC3200 tool](docs/cc3200-tool.md) is available as
`goteddycloud cc3200 --help` and shares its Go protocol core with browser flashing
through an embedded WebAssembly module. Python/pip is not required.
