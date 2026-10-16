#!/usr/bin/env bash
# Build an isolated copy of the pinned frontend fork.
set -euo pipefail
cd "$(dirname "$0")/.."
frontend=.cache/teddycloud-reference/teddycloud_web
pin=e2f40c2c69a3b2b46093ddc5542773dd0fa0067e
if [[ "${REFERENCE_BRANCH:-develop}" == develop ]]; then
 frontend="${FRONTEND_SOURCE:-.cache/teddycloud-web-fork}"
 pin=$(cat frontend/upstream-revision)
 if [[ ! -d "$frontend/.git" && ! -f "$frontend/.git" ]]; then
  git clone --no-checkout https://github.com/teian/teddycloud_web.git "$frontend"
 fi
 if [[ "$frontend" == .cache/teddycloud-web-fork ]]; then
  if ! git -C "$frontend" cat-file -e "$pin^{commit}" 2>/dev/null; then
   git -C "$frontend" fetch origin "$pin"
  fi
  git -C "$frontend" checkout --detach "$pin"
 fi
fi
[[ "$(git -C "$frontend" rev-parse HEAD)" == "$pin" ]] || { echo 'Unexpected frontend revision' >&2; exit 1; }
[[ -z "$(git -C "$frontend" status --porcelain --untracked-files=no)" ]] || { echo 'Frontend contains tracked edits' >&2; exit 1; }
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
