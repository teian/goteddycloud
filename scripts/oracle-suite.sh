#!/usr/bin/env bash
# Isolated C/Go replay for a clean CI workspace. Refuses existing test deployments.
set -euo pipefail
cd "$(dirname "$0")/.."
[[ ! -e .cache/oracle-runtime && ! -e .cache/ci-candidate-runtime ]] || { echo 'Oracle suite requires unused fixture paths' >&2; exit 1; }
mkdir -p .cache bin
scripts/reference.sh build > .cache/ci-reference-build.log 2>&1 || { cat .cache/ci-reference-build.log >&2; exit 1; }
go run -buildvcs=false ./cmd/fixturegen
root="$(pwd)/.cache/oracle-runtime"
.cache/teddycloud-develop-reference/bin/teddycloud --base_path "$root/" --encode "$root/data/library/synthetic.taf" "$root/tone1.wav" "$root/tone2.wav" > .cache/ci-encode.log 2>&1 || { cat .cache/ci-encode.log >&2; exit 1; }
python3 scripts/playlist-fixture.py "$root"
cp -a .cache/oracle-runtime .cache/ci-candidate-runtime
CGO_ENABLED=0 go build -buildvcs=false -o bin/goteddycloud ./cmd/goteddycloud
scripts/reference.sh run > .cache/ci-reference.log 2>&1 &
reference_pid=$!
bin/goteddycloud -base_path .cache/ci-candidate-runtime -http 127.0.0.1:18081 -httpsecure 127.0.0.1:19443 -box 127.0.0.1:19444 > .cache/ci-candidate.log 2>&1 &
candidate_pid=$!
cleanup() {
 kill -TERM "$candidate_pid" "$reference_pid" 2>/dev/null || true
 wait "$candidate_pid" "$reference_pid" 2>/dev/null || true
}
trap cleanup EXIT
for port in 18080 18081; do
 ready=false
 for attempt in $(seq 1 50); do
  path=/v1/time
  if [[ "$port" == 18081 ]]; then path=/api/time; fi
  if curl --fail --silent "http://127.0.0.1:$port$path" > /dev/null; then ready=true; break; fi
  sleep 0.2
 done
 "$ready" || { echo 'Listener readiness timeout' >&2; cat .cache/ci-reference.log .cache/ci-candidate.log >&2; exit 1; }
done
compatcheck() {
 go run -buildvcs=false ./cmd/compatcheck -candidate-device https://127.0.0.1:19444 -candidate-ca "$root/certs/server/teddy-cert.pem" -cases "$1"
}
compatcheck testdata/device-offline.json
compatcheck testdata/auth-events.json
compatcheck testdata/static.json
python3 scripts/plugins-differential.py
compatcheck testdata/ota-offline.json
compatcheck testdata/v3-offline.json
compatcheck testdata/custom-models.json
compatcheck testdata/playlist-cached.json
python3 scripts/content-api-differential.py
python3 scripts/tags-api-differential.py
python3 scripts/cache-api-differential.py
python3 scripts/firmware-api-differential.py
python3 scripts/audio-api-differential.py
python3 scripts/rtnl-differential.py --candidate-http 18081 --candidate-box 19444
