#!/usr/bin/env bash
# Build the pinned frontend with the Go UI overlay.
set -euo pipefail
cd "$(dirname "$0")/.."
frontend=.cache/teddycloud-reference/teddycloud_web
pin=e2f40c2c69a3b2b46093ddc5542773dd0fa0067e
if [[ "${REFERENCE_BRANCH:-develop}" == develop ]]; then
 frontend=.cache/teddycloud-develop-reference/teddycloud_web
 pin=$(cat frontend/upstream-revision)
fi
[[ "$(git -C "$frontend" rev-parse HEAD)" == "$pin" ]] || { echo 'Unexpected frontend revision' >&2; exit 1; }
source_frontend="$frontend"
if [[ "${REFERENCE_BRANCH:-develop}" == develop ]]; then
 frontend=.cache/goteddycloud-web
fi
case "${1:-build}" in
 build)
  if [[ "${REFERENCE_BRANCH:-develop}" == develop ]]; then
   python3 scripts/prepare-frontend.py "$source_frontend"
  fi
  npm --prefix "$frontend" ci --ignore-scripts --no-audit --no-fund
  npm --prefix "$frontend" run build
  ;;
 test)
  npm --prefix "$frontend" test
  ;;
 install-fixture)
  fixture="${2:?supply a disposable generated fixture path}"
  [[ -f "$fixture/.synthetic-fixture" ]] || { echo 'Only generated synthetic fixtures are accepted' >&2; exit 1; }
  [[ -f "$frontend/dist/index.html" ]] || { echo 'Build the frontend first' >&2; exit 1; }
  mkdir -p "$fixture/data/www/web"
  cp -a "$frontend/dist/." "$fixture/data/www/web/"
  ;;
 *) echo 'Usage: scripts/frontend.sh [build|test|install-fixture path]' >&2; exit 2 ;;
esac
