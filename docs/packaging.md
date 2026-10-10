# Development packaging

The backend is incomplete. These archives/images are development artifacts and
are not published releases or a claim of replacement compatibility.

`python3 scripts/build-release.py VERSION` takes one frozen source snapshot,
builds with CGO_ENABLED=0 for Linux amd64/arm64, Windows amd64 and macOS amd64/arm64,
and writes archives, dependency notices, build provenance and SHA256SUMS to dist/.
The backend links no C runtime or upstream C libraries. FFmpeg remains external.
Frontend assets are not bundled; see [licenses](licenses.md) before redistribution.
Build manifests conservatively say runtime_verified=false; native evidence is
recorded separately and is never inferred from cross-compilation.

Local 2026-10-09 evidence: all five builds and archive checksums passed. Linux
amd64 additionally passed `scripts/native-smoke.py` (three listeners, verified TLS,
overlay persistence, preserved identities, restart and worker shutdown).
Windows/macOS and Linux arm64 archives were cross-compiled only.

```sh
docker buildx build --platform linux/amd64 --target runtime --load -t goteddycloud:local .
docker buildx build --platform linux/arm64 --target runtime --load -t goteddycloud:local-arm64 .
docker buildx build --platform linux/amd64 --target ffmpeg --load -t goteddycloud:local-ffmpeg .
```

The runtime target works without FFmpeg; the ffmpeg target installs the external
codec package. Base image manifests are pinned. Runtime defaults to numeric UID/GID
65532, unprivileged ports 18080/18443/18444, and a persistent /data deployment root.
For an existing host directory, run under its owning UID/GID or prepare appropriate
permissions on the copied deployment. Host port mappings are independently chosen.

```sh
docker run --name goteddycloud -v goteddycloud-data:/data \
  -p 18080:18080 -p 18443:18443 -p 18444:18444 goteddycloud:local
```

Use `-base_path /data -http :PORT -httpsecure :PORT -box :PORT` as container arguments to
change listener ports; `off` disables a listener. Configuration, certificates and
audio stay under the persistent root by default and may be mounted separately.
Preserve the single-writer rule during migration.

Both Linux image architectures build. The amd64 runtime image passed the container
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
