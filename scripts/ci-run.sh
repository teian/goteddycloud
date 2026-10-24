#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-only
# Stream a check's output while retaining its real exit status and a log file.
set -euo pipefail
if [[ $# -lt 2 || ! "$1" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]]; then
  echo 'Usage: ci-run.sh log-name command [arguments...]' >&2
  exit 2
fi
check_log_name=$1
shift
mkdir -p .cache/ci-checks
"$@" 2>&1 | tee ".cache/ci-checks/$check_log_name.log"
