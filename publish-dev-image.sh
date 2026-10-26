#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-only
set -euo pipefail

if [[ $# -gt 0 ]]; then
    if [[ $# -eq 1 && ( "$1" == --help || "$1" == -h ) ]]; then
        cat <<'HELP'
Usage: ./publish-dev-image.sh

Build local sources with FFmpeg and the pinned frontend for the same platforms
as release CI: linux/amd64, linux/arm/v7 and linux/arm64. Check architecture,
startup, frontend delivery and layer reuse, then publish the verified OCI image
to ghcr.io/teian/goteddycloud:dev with its original compressed layer digests.

Requires Docker with Buildx, Git, Python 3, Skopeo and a GHCR login with
write:packages (used for the development build cache as well as the image):
  docker login ghcr.io --username teian

A persistent docker-container builder named goteddycloud-publish is created on
first use. Set PUBLISH_BUILDER to use an existing Buildx builder instead.
The builder must support all three platforms (ARM emulation or native workers).
The pinned frontend commit must already be available on GitHub.
Release tags, latest and the release buildcache are never overwritten.
HELP
        exit 0
    fi
    echo 'No arguments accepted. Use --help for usage.' >&2
    exit 2
fi

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
readonly repository='ghcr.io/teian/goteddycloud'
readonly image="$repository:dev"
readonly platforms='linux/amd64,linux/arm/v7,linux/arm64'
readonly builder="${PUBLISH_BUILDER:-goteddycloud-publish}"
for dependency in docker git python3 skopeo; do
    command -v "$dependency" >/dev/null || {
        echo "Missing required command: $dependency" >&2
        exit 1
    }
done
docker buildx version >/dev/null
revision=$(git rev-parse HEAD)
readonly revision
native_platform=$(docker version --format '{{.Server.Os}}/{{.Server.Arch}}')
case "$native_platform" in
    linux/amd64|linux/arm64) native_arch=${native_platform#linux/}; native_variant='' ;;
    linux/arm) native_arch=arm; native_variant=v7; native_platform=linux/arm/v7 ;;
    *) echo "Unsupported Docker server platform: $native_platform" >&2; exit 1 ;;
esac
if ! docker buildx inspect "$builder" >/dev/null 2>&1; then
    if [[ -n "${PUBLISH_BUILDER:-}" ]]; then
        echo "Requested Buildx builder does not exist: $builder" >&2
        exit 1
    fi
    docker buildx create --name "$builder" --driver docker-container >/dev/null
fi
docker buildx inspect "$builder" --bootstrap

workdir=$(mktemp -d "${TMPDIR:-/tmp}/goteddycloud-publish.XXXXXXXX")
readonly workdir
readonly test_image="goteddycloud:publish-test-${revision:0:12}-$$"
cleanup() {
    docker image rm "$test_image" >/dev/null 2>&1 || true
    rm -rf -- "$workdir"
}
trap cleanup EXIT

# Match release CI's target/exporter/attestations and import its complete stage
# cache. Keep local development cache writes separate from the release cache.
build_args=(
    --builder "$builder" --target ffmpeg
    --cache-from "type=registry,ref=$repository:buildcache"
    --cache-from "type=registry,ref=$repository:dev-buildcache"
    --label "org.opencontainers.image.revision=$revision"
    --label org.opencontainers.image.source=https://github.com/teian/goteddycloud
    --label org.opencontainers.image.licenses=GPL-2.0-only
)
build() {
    local version=$1
    shift
    docker buildx build "${build_args[@]}" --build-arg "VERSION=$version" "$@" .
}

echo "Building $image for $platforms (frontend $(cat frontend/upstream-revision))"
build dev --platform "$platforms" --tag "$image" \
    --output "type=oci,dest=$workdir/images.tar" \
    --cache-to "type=registry,ref=$repository:dev-buildcache,mode=max" \
    --provenance=mode=max --sbom=true
python3 scripts/check-container-architectures.py "$workdir/images.tar" --require-ffmpeg --require-catalog

# Load the native member of the inspected archive, rather than rebuilding it.
# A Docker archive works with Docker contexts and classic image stores too.
skopeo --override-os linux --override-arch "$native_arch" --override-variant "$native_variant" \
    copy "oci-archive:$workdir/images.tar:dev" "docker-archive:$workdir/native.tar:$test_image"
docker load --input "$workdir/native.tar"
echo 'Checking container startup, FFmpeg and bundled frontend'
docker run --rm "$test_image" \
    -base_path /data -http :18080 -httpsecure :18443 -box :18444 -docker-test
docker run --rm --entrypoint ffmpeg "$test_image" -version
python3 scripts/container-webui-smoke.py --image "$test_image"

echo 'Checking that a server version change replaces only the server layer'
build dev-layercheck --platform "$native_platform" \
    --output "type=oci,dest=$workdir/layer-variant.tar" \
    --provenance=false --sbom=false
python3 scripts/check-container-layers.py "$workdir/images.tar" "$workdir/layer-variant.tar" \
    --platform "linux/$native_arch"

echo "Publishing verified $image without rebuilding or recompressing its layers"
if ! skopeo copy --all --preserve-digests --digestfile "$workdir/published-digest" \
    "oci-archive:$workdir/images.tar:dev" "docker://$image"; then
    echo 'Push failed. Check registry/network access and Docker/Skopeo credentials.' >&2
    echo 'GHCR requires a token with write:packages; log in with:' >&2
    echo '  docker login ghcr.io --username teian' >&2
    exit 1
fi
echo "Published $image ($(cat "$workdir/published-digest"))"
