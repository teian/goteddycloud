# Release packaging

The backend remains incomplete; publishing a release does not establish physical
device compatibility or internet security acceptance.

Publishing a GitHub release triggers `.github/workflows/release.yml`. It first
runs the reusable compatibility workflow, then publishes native release assets
and the multi-platform container image to `ghcr.io/<owner>/goteddycloud`. Version
tags follow the GitHub release tag; stable releases also update `latest`.
Prereleases do not change `latest`. Pushing a commit or a tag alone does not
publish images. The release tag must match the root `VERSION` file, with an
optional `v` prefix. The current application version is 0.1.0.
GitHub's workflow token supplies `contents: write` for release
assets and `packages: write` for GHCR; no separate registry password is required.
An existing GHCR package must allow this repository's Actions token to write it.
The package's visibility is controlled in GitHub Packages, not by this workflow.

To retry only image publishing after a workflow/tooling fix, run **Publish
release → Run workflow** on the default branch with the existing release tag,
or `gh workflow run release.yml -f tag=v0.1.0`. This path keeps the published Git tag
and binary/package assets unchanged. By default it builds Docker from the immutable tagged
sources, uses current workflow validation tools, and requires successful push CI
for that tag's commit. Only the release marked latest by GitHub updates the image's
`latest` tag; older releases and prereleases keep their version tag only.

To explicitly rebuild an existing image tag from a corrected source commit,
provide `-f source_ref=FULL_COMMIT_SHA` as well. That commit must have successful
push CI and its `VERSION` must match the selected release. This replaces only
the GHCR image tags, preserving the Git release tag and native release assets.
OCI revision labels record the actual source commit. The default retry path
continues to use the original tagged sources.

Both `goteddycloud` and standalone `cc3200tool` are built with `CGO_ENABLED=0`:

| OS | Architectures | Archive |
| --- | --- | --- |
| Linux | amd64, ARMv7, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |
| macOS (`darwin`) | amd64, arm64 | `.tar.gz` |

Go does not support 32-bit ARM for Windows or macOS. `armv7` assets use
`GOARCH=arm GOARM=7`. Every Linux architecture additionally receives separate
`.deb`, `.rpm` and Arch Linux `.pkg.tar.zst` packages for each program, generated
with pinned nFPM v2.48.0. Arch's native names are x86_64, armv7h and aarch64.
These are installable files, not hosted APT/RPM/Arch repositories. No package
starts or enables a network service automatically.

Before publication, the workflow checks archive headers, executable permissions,
notices, package architecture fields, packaged executable bytes and the complete
checksum list. It builds/runs both Nix packages from their local release archives,
checks all three Docker image manifests against their actual ELF executables,
and starts the native FFmpeg image with the three configured listeners.
Windows/macOS and ARM binaries remain cross-build evidence; a package inspection
does not establish runtime acceptance on those systems.

`goteddycloud-VERSION-nix.tar.gz` contains `default.nix` plus per-platform binary
archive hashes for both programs. Extract it and run `nix-build default.nix`, or
add `--argstr program cc3200tool`. This binary package expression uses the user's
configured nixpkgs. It is not an upstream nixpkgs submission or a Nix binary cache.

`python3 scripts/build-release.py VERSION` takes one frozen source snapshot and
writes native archives, a Nix package, dependency notices, build provenance and
`SHA256SUMS` to `dist/`. Add `--packages --nfpm /path/to/nfpm` to build Linux
packages; this requires a semantic version such as `v1.2.3`. Native packages and
archives require separately supplied frontend assets. Docker images build and
include the frontend as described below. Its outstanding upstream licensing
questions are recorded in [the license inventory](licenses.md).
Manufacturer DLLs and locally embedded loaders are excluded from
release snapshots and images; pass the external loader with `--loader` as needed.
The backend links no C runtime or upstream C libraries. FFmpeg remains external.
Build manifests conservatively say runtime_verified=false; native evidence is
recorded separately and is never inferred from cross-compilation.

Portable server archives can be extracted and started from the archive's directory,
or selected with `-base_path`. Linux packages install executables under `/usr/bin`.
Before starting a packaged server, create a writable deployment and point it at
your separately supplied web assets:

```sh
goteddycloud -base_path /path/to/deployment \
  -config-set core.wwwdir=/absolute/path/to/www
goteddycloud -base_path /path/to/deployment \
  -http 127.0.0.1:18080 -httpsecure off -box :19444
```

Enable management login while access is still private and provide HTTPS through
the management listener or a trusted reverse proxy before publishing the Web UI.
Login is off by default. See [login setup](develop-port.md),
[HTTPS certificates](management-tls.md) and [reverse proxies](reverse-proxy.md).

Local 2026-10-09 evidence: all five builds and archive checksums passed. Linux
amd64 additionally passed `scripts/native-smoke.py` (three listeners, verified TLS,
overlay persistence, preserved identities, restart and worker shutdown).
Windows/macOS and Linux arm64 archives were cross-compiled only.

```sh
docker buildx build --platform linux/amd64 --target runtime --load -t goteddycloud:local .
docker buildx build --platform linux/arm64 --target runtime --load -t goteddycloud:local-arm64 .
docker buildx build --platform linux/arm/v7 --target runtime --load -t goteddycloud:local-armv7 .
docker buildx build --platform linux/amd64 --target ffmpeg --load -t goteddycloud:local-ffmpeg .
```

All project image stages use digest-pinned Alpine 3.24 variants. The runtime
target contains the static Go server, bundled UI, notices and system CA trust;
the ffmpeg target adds Alpine's FFmpeg package and its required shared libraries.
No Go/Node toolchain, Python, Git, development headers or build cache is shipped.
GHCR release images use the `ffmpeg` target and contain
linux/amd64, linux/arm/v7 and linux/arm64 in one image index. Both targets build
the pinned frontend and owned overlay in a Node.js build stage. The resulting
Web UI is included at `/usr/share/goteddycloud/www/web`, outside the data mount;
Node.js, Git, Python and npm dependencies are absent from the runtime image.
The environment variable `GOTEDDYCLOUD_BUNDLED_WWW` selects this read-only
fallback. A complete custom frontend under the configured `www/web` directory
overrides it; otherwise `/web` works with an empty named volume or host mount.
Upgrades replace bundled assets without copying anything into persistent data.
Both targets also include `tonies.json` and `tonieboxes.json` under
`/usr/share/goteddycloud/catalog`, selected by `GOTEDDYCLOUD_BUNDLED_CATALOG`.
Each build revalidates the official `toniebox-reverse-engineering/tonies-json`
release branch and fetches both files from the same immutable commit.
`snapshot.json` records that commit, entry counts and SHA256 hashes. A failed or
invalid build-time download fails the image build. At runtime, missing or empty
official catalogs are copied atomically to the configured storage backend
(filesystem or S3) before serving requests. Populated catalogs and custom entries
are preserved; normal successful updates replace the baseline. Startup can work
fully offline with this snapshot. Cover images are still fetched separately;
the snapshot contains their URLs, and generic placeholders are embedded locally.
The catalog snapshot uses its own `COPY --link` layer so database changes do not
invalidate FFmpeg, the frontend or the executable layer.
Attribution and dependency notices are under `/usr/share/doc/goteddycloud/frontend`.
Base image manifests and the frontend revision are pinned. Runtime defaults to numeric UID/GID
65532, unprivileged ports 18080/18443/18444, and a persistent /data deployment root.
For an existing host directory, run under its owning UID/GID or prepare appropriate
permissions on the copied deployment. Host port mappings are independently chosen.

OS certificates and FFmpeg install in independent dependency stages before any
application files are copied. The server executable, frontend, notices and initial
data layout use separate [`COPY --link`](https://docs.docker.com/reference/dockerfile/#copy---link) layers. A server-only or UI-only update
therefore preserves the other payloads and cached dependency blobs; clients with
those blobs download only changed layers and image metadata. The first update
from the older image layout needs the newly arranged layers once. A base-image
update, intentional dependency rebuild or missing build cache can replace package
layers as well. CI preserves intermediate dependency stages in a [registry cache](https://docs.docker.com/build/cache/backends/registry/) using the GHCR
`:buildcache` tag, alongside the GitHub Actions cache, so subsequent releases can
reuse them even after the Actions cache expires. The cache tag is build metadata,
not a deployment image. To deliberately refresh packages when building locally, use
`docker buildx build --no-cache-filter runtime-deps,ffmpeg-deps --target ffmpeg --load -t goteddycloud:local .`.

Release CI builds a second native image with a changed application version and
compares the actual compressed OCI layer digests with the release build using
`scripts/check-container-layers.py`. Publication requires exactly the executable
layer to change; the FFmpeg, OS, UI, notices and data layers must match.

Local Alpine verification (2026-10-13) built both targets for all three platforms,
checked Alpine contents and executable architectures, started the three listeners,
and converted WAV to a validated TAF/Opus file. amd64 ran natively; ARMv7 and arm64
ran under QEMU. Native Web UI/assets, custom overrides and restart checks passed
for both targets. These checks do not establish physical ARM hardware acceptance.
Sizes below are MiB (1,048,576 bytes); downloads sum compressed OCI layers and
exclude small manifests/attestations. Docker image sizes were measured after
unpacking each platform locally.

| Target | Platform | Download | Docker image size |
| --- | --- | ---: | ---: |
| runtime | amd64 | 24.0 | 73.6 |
| runtime | ARMv7 | 23.1 | 69.3 |
| runtime | arm64 | 23.8 | 72.7 |
| ffmpeg | amd64 | 73.3 | 252.7 |
| ffmpeg | ARMv7 | 59.6 | 176.3 |
| ffmpeg | arm64 | 70.3 | 226.0 |

The native server-only variant reused 7/8 compressed layers and changed only its
7.8 MiB executable layer. A UI-only variant reused 7/8 layers and changed only
its 12.1 MiB frontend layer. No compiler, Node, Python or Git runtime was present.

```sh
docker run --name goteddycloud -v goteddycloud-data:/data \
  -p 18080:18080 -p 18443:18443 -p 18444:18444 goteddycloud:local
```

Use `-base_path /data -http :PORT -httpsecure :PORT -box :PORT` as container arguments to
change listener ports; `off` disables a listener. Configuration, certificates and
audio stay under the persistent root by default and may be mounted separately.
Preserve the single-writer rule during migration.

Earlier two-architecture development evidence: the amd64 runtime image passed the container
smoke test: startup as the default user, byte-exact stored TAF delivery without a
codec, settings/identity persistence across restart, and clean exit. Arm64 is
build-only on this host. Native Windows/macOS execution and remote CI runs remain
unverified. The checked-in CI workflow runs tests/vet/race, bounded parser/path
fuzzing, mock TLS/cloud tests, a clean isolated C differential suite, cross-builds,
and Linux smoke/migration checks; adding the workflow does not establish a remote
CI result.

The amd64 FFmpeg image additionally passed a real WAV-to-TAF CLI conversion
as its default UID 65532 (2026-10-09). The runtime-only image needs no FFmpeg
for existing TAF playback. No arm64 runtime result is inferred from its build.

Refreshed audit snapshot (2026-10-09): version `0.0.0-dev.20261009-audit` is
under `dist/20261009-audit`, with per-target source digests and SHA256SUMS.
It includes the CLI, MQTT/live, cloud/V3, security, diagnostics and settings-consumer
work. Linux amd64 native/migration checks and the amd64 runtime container smoke
were repeated successfully; amd64/arm64 image builds also passed. Older artifacts
under `dist/20261009-current` remain earlier development snapshots and should not
be used as evidence for these newer changes. See compatibility-report.md for the
remaining acceptance gates and explicit deviations.
