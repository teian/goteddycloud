#!/usr/bin/env bash
# Bootstrap and build an isolated, pinned C oracle. Never writes ../teddycloud.
set -euo pipefail
cd "$(dirname "$0")/.."
pin=78c3b38e36f665d3a5fffe907f73a02725176377
reference=.cache/teddycloud-reference
if [[ "${REFERENCE_BRANCH:-develop}" == develop ]]; then
 pin=8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1
 reference=.cache/teddycloud-develop-reference
fi
case "${1:-build}" in
  prepare|build)
    if [[ ! -d "$reference/.git" ]]; then
      mkdir -p .cache
      source_checkout=../teddycloud
      if [[ ! -d "$source_checkout/.git" ]]; then source_checkout=https://github.com/toniebox-reverse-engineering/teddycloud.git; fi
      git clone --no-hardlinks --no-checkout "$source_checkout" "$reference"
      git -C "$reference" fetch https://github.com/toniebox-reverse-engineering/teddycloud.git "$pin"
      git -C "$reference" checkout --detach "$pin"
    fi
    [[ "$(git -C "$reference" rev-parse HEAD)" == "$pin" ]] || { echo 'Unexpected reference revision' >&2; exit 1; }
    [[ -z "$(git -C "$reference" status --porcelain --untracked-files=no)" ]] || { echo 'Reference contains tracked edits' >&2; exit 1; }
    git -C "$reference" submodule update --init --depth 1 cJSON cyclone/common cyclone/cyclone_tcp cyclone/cyclone_ssl cyclone/cyclone_crypto ogg opus teddycloud_web
    if [[ "${1:-build}" == build ]]; then
      # The pinned tree includes generated protobuf-c files. Older protoc-c is
      # unnecessary unless schemas are deliberately changed in the oracle.
      # Upstream exposes NO_WARN_FAIL for compilers with new warning defaults.
      make -C "$reference" -j"${JOBS:-4}" build NO_SANITIZERS=1 NO_WARN_FAIL=1
    fi
    ;;
  run)
    # Deliberately accept only our generated, disposable fixture deployment.
    runtime="$(pwd)/.cache/oracle-runtime"
    [[ -f "$runtime/.synthetic-fixture" ]] || { echo 'Generate fixtures first: go run ./cmd/fixturegen' >&2; exit 1; }
    [[ -x "$reference/bin/teddycloud" ]] || { echo 'Build oracle first' >&2; exit 1; }
    exec "$reference/bin/teddycloud" --base_path "$runtime/" --config-set 'core.server.bind_ip=127.0.0.1,core.server.http_port=18080,core.server.https_web_port=18443,core.server.https_api_port=18444,cloud.enabled=false,mqtt.enabled=false,mqtt_server.enabled=false,tonie_json.cache_preload=false'
    ;;
  *) echo 'Usage: scripts/reference.sh [prepare|build|run]' >&2; exit 2 ;;
esac
