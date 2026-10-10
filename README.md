# goTeddyCloud

Go rewrite of [teddyCloud](https://github.com/toniebox-reverse-engineering/teddycloud),
based on official `develop` commit `8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1`
(frontend `636a8d65043f6c6e322f76bcb11e958725159e96`).
**Incomplete; not yet a compatible replacement for teddyCloud.**

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
