#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-only
set -euo pipefail
cd "$(dirname "$0")/.."
GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o internal/app/cc3200-tool.wasm ./cmd/cc3200wasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" internal/app/cc3200-wasm-exec.js
chmod 0644 internal/app/cc3200-tool.wasm internal/app/cc3200-wasm-exec.js
