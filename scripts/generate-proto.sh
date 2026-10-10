#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .cache/tools
if [[ -n "${PROTOC:-}" ]]; then
  compiler="$PROTOC"
else
  [[ "$(uname -sm)" == 'Linux x86_64' ]] || { echo 'Set PROTOC to an installed protoc on this platform' >&2; exit 1; }
  archive=.cache/tools/protoc-36.2-linux-x86_64.zip
  if [[ ! -f "$archive" ]]; then
    curl --fail --location --silent --show-error 'https://github.com/protocolbuffers/protobuf/releases/download/v36.2/protoc-36.2-linux-x86_64.zip' -o "$archive"
  fi
  echo '121f6c7afe1d4d0e3ea6aab9432038599250134cbf4474cb1167d2c7decd4278  .cache/tools/protoc-36.2-linux-x86_64.zip' | sha256sum --check
  unzip -o -q "$archive" -d .cache/tools/protoc-36.2
  compiler="$PWD/.cache/tools/protoc-36.2/bin/protoc"
fi
GOBIN="$PWD/.cache/tools" go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
args=()
for schema in proto/*.proto; do
  args+=("--go_opt=M${schema#proto/}=goteddycloud/internal/protocol")
done
"$compiler" -I proto --plugin="protoc-gen-go=$PWD/.cache/tools/protoc-gen-go" --go_out=. --go_opt=module=goteddycloud "${args[@]}" proto/*.proto
