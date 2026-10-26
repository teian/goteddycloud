#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-only
set -euo pipefail

if [[ $# -gt 0 ]]; then
    if [[ $# -eq 1 && ( "$1" == --help || "$1" == -h ) ]]; then
        cat <<'EOF'
Usage: ./publish-dev-image.sh

Build the local sources with FFmpeg and the frontend pinned in
frontend/upstream-revision, test the image, and push it to
ghcr.io/teian/goteddycloud:dev for this host's architecture.

Requires Docker, Git, Python 3 and a GHCR login with write:packages:
  docker login ghcr.io --username teian

The pinned frontend commit must already be available on GitHub.
Release tags and latest are never published by this script.
EOF
        exit 0
    fi
    echo 'No arguments accepted. Use --help for usage.' >&2
    exit 2
fi

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
readonly image='ghcr.io/teian/goteddycloud:dev'
for dependency in docker git python3; do
    command -v "$dependency" >/dev/null || {
        echo "Missing required command: $dependency" >&2
        exit 1
    }
done
revision=$(git rev-parse HEAD)
readonly revision

echo "Building $image locally (frontend $(cat frontend/upstream-revision))"
docker build \
    --target ffmpeg \
    --build-arg VERSION=dev \
    --label "org.opencontainers.image.revision=$revision" \
    --label org.opencontainers.image.source=https://github.com/teian/goteddycloud \
    --tag "$image" .

echo 'Checking container startup, FFmpeg and bundled frontend'
docker run --rm "$image" \
    -base_path /data -http :18080 -httpsecure :18443 -box :18444 -docker-test
docker run --rm --entrypoint ffmpeg "$image" -version
python3 scripts/container-webui-smoke.py --image "$image"

echo "Publishing $image"
if ! docker push "$image"; then
    echo 'Push failed. Check registry/network access and Docker credentials.' >&2
    echo 'GHCR requires a token with write:packages; log in with:' >&2
    echo '  docker login ghcr.io --username teian' >&2
    exit 1
fi
echo "Published $image"
