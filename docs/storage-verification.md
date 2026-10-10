# Storage verification — 2026-10-10

The selectable storage extension covers every durable deployment consumer.
`docs/storage.md` describes configuration, migration and operational differences.
Existing physical-device/frontend acceptance gates remain pending; this extension
does not declare complete teddyCloud replacement parity.

## Automated results

- Shared filesystem/S3 contract tests pass: atomic stream publication, overflow
  and cancellation preservation, conditional exclusive creation, seek/ranged
  reads, read-ahead request count, object replacement, append, directory markers,
  paginated listings, confinement, directory overwrite rejection and failed
  directory-copy source preservation.
- S3 application regressions pass: settings/users after restart, linked catalog
  rename, directory moves with encoded filenames, static assets and indexes,
  imported/generated certificates and client identity, RTNL and diagnostics,
  OTA cache, content cache/migration, firmware upload/patch/certificate extraction,
  images, playlist conversion and generated live publication. Tests use a logical
  deployment path that is never created locally.
- The standard-library S3 signer matches all four published AWS example
  signatures (GET, PUT, lifecycle and listing). Credential precedence/profile,
  redirect refusal, embedded HTTP-200 copy errors and upload cancellation tests
  also pass. Request payload hashing checks cancellation and preserves reader
  position on success.
- Full race suite, `go vet`, Staticcheck and `go mod tidy -diff` pass. The
  application links no AWS SDK or S3 test-emulator packages. The emulator is an
  independent test-only dependency; its incorrect ranged HTTP-200 status is
  normalized to HTTP 206 in the test harness.
- Native Linux startup, verified web/box TLS, overlay persistence, restart identity
  preservation and child shutdown pass. Copied-data read-only validation,
  mutation and exact snapshot rollback rehearsal pass.
- Linux amd64 native build and Linux arm64, Windows amd64, macOS amd64/arm64
  cross-builds pass with CGO disabled. Cross-builds do not establish native runtime
  support on those targets. Existing versioned release archives remain older
  snapshots.

Reproduce the core checks:

```sh
go test -buildvcs=false -race ./... -timeout 180s
go vet -buildvcs=false ./...
GOFLAGS=-buildvcs=false go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
go mod tidy -diff
CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' \
  -o bin/goteddycloud ./cmd/goteddycloud
python3 scripts/native-smoke.py --binary bin/goteddycloud
python3 scripts/migration-rehearsal.py --binary bin/goteddycloud
```

At storage-extension verification, the stripped Linux amd64 binary was
10,924,194 bytes, SHA-256
`d2fb8e40586339678ca5d89260b66951fb1de0104f50fb455dd5ce23902c4f8d`.
This uses `-s -w`; do not compare it directly with earlier unstripped build sizes.

## Compatibility results and limits

Fresh fixtures against the unchanged pinned C develop oracle passed content,
tag and image-cache comparisons, five certificate comparisons, and a separate
49-check audio/RTNL run. Development auth/CORS/user lifecycle, JSON and track byte
comparisons passed (eight reported checks), as did the live-stream differential.
The reference inventory remains 76 routes, 269 settings/tree entries, 25 CLI
options and nine submodules.

The firmware differential stopped at the C oracle's first
`POST /api/esp32/uploadFirmware`: it timed out after 30 seconds on two fresh
runs. Instrumented request output identifies the C port; the script had not yet
sent that upload to Go. The pinned C working tree is unchanged. No cause is
confirmed, no C fix is claimed and no timeout was increased. Go's filesystem
firmware regressions and S3 upload/patch/extraction tests pass. The remaining C
firmware comparisons from those runs are unverified.

Evidence from this session is under ignored `.cache/storage-*.log` and
`.cache/storage-review/`, including both failed oracle runs. The focused tests
and published-vector inputs are retained in the source tree.

## Real-provider acceptance

On 2026-10-10 the same client, storage and application tests passed against:

| Provider | Exact build | Mode | Result |
| --- | --- | --- | --- |
| PGSTY Silo | `RELEASE.2026-09-16T00-00-00Z` | Single local drive, authenticated S3 | 7 test groups passed |
| NooBaa Core | `master-20261009`, revision `df54b21` | Configured NSFS account, POSIX storage | 7 test groups passed |

Both runs also pass with `-race`. The seven groups cover authenticated protocol
semantics, shared filesystem/S3 operations, conditional creation and 1,005-object
pagination, deployment persistence, OTA/cache migration, live/playlist/images/
certificates, and firmware upload/patch/extraction. Protocol assertions include
rejection of invalid signing secrets, metadata preservation, encoded Unicode/
space/`#`/`+`/`%` object names, exact ranged reads, stale-ETag GET and copy refusal,
and exclusive PUT refusal. No production client changes were required.

Two injected-failure groups skip explicitly against external endpoints; the
default emulator suite passes separately with the race detector, including those
failure tests. Focused `go vet` and `go mod tidy -diff` pass. The only source
changes for provider acceptance are test infrastructure and tests, so the
previously recorded application binary is unchanged.

Silo's downloaded archive matched its published SHA-256. NooBaa's container was
pinned by digest. Setup, identities and repeat commands are in
[provider test setup](s3-providers.md). Per-provider verbose and race logs are in
ignored `.cache/s3-providers/`. Tests clean up their randomly owned prefixes;
both isolated server processes were stopped after verification.

The NooBaa image's simple mode crashed on its first request in `AccountSpaceFS`
with an undefined config path. The configured account mode passed without any
server patches. Configured NSFS disables HTTP by default; HTTP was enabled only
for this loopback test deployment. These runs do not establish live AWS,
versioned-bucket, TLS-provider, or full NooBaa Kubernetes/backend-pool acceptance.
No production data was used, migrated or deleted.

Sources: [AWS Signature V4 examples](https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sig-v4-header-based-auth.html),
[Apache license compatibility guidance](https://apache.org/licenses/GPL-compatibility.html).
