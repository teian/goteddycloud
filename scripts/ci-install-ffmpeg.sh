#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-only
# Configure only disposable Ubuntu CI runners, never a developer/deployment host.
set -euo pipefail
if [[ "${GITHUB_ACTIONS:-}" != true ]]; then
  echo 'This package setup is restricted to disposable GitHub Actions runners.' >&2
  exit 2
fi
source /etc/os-release
if [[ "$ID" != ubuntu || "$VERSION_ID" != 24.04 ]]; then
  echo 'Expected the pinned Ubuntu 24.04 runner.' >&2
  exit 2
fi
# Keep the runner's signed Ubuntu repositories/keyrings. Prefer their official
# HTTPS endpoints over the slow HTTP Azure mirror seen in the previous run.
for apt_source in /etc/apt/apt-mirrors*.txt /etc/apt/sources.list /etc/apt/sources.list.d/*.sources; do
  [[ -f "$apt_source" ]] || continue
  sudo sed -i \
    -e 's|http://azure.archive.ubuntu.com/ubuntu|https://archive.ubuntu.com/ubuntu|g' \
    -e 's|http://archive.ubuntu.com/ubuntu|https://archive.ubuntu.com/ubuntu|g' \
    -e 's|http://security.ubuntu.com/ubuntu|https://security.ubuntu.com/ubuntu|g' \
    "$apt_source"
done
sudo tee /etc/apt/apt.conf.d/99goteddycloud-ci > /dev/null <<'APT'
Acquire::Retries "3";
Acquire::http::Timeout "30";
Acquire::https::Timeout "30";
DPkg::Lock::Timeout "60";
APT::Install-Recommends "false";
APT::Install-Suggests "false";
APT
if ! command -v ffmpeg > /dev/null || ! command -v ffprobe > /dev/null; then
  sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get update --error-on=any
  sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get install -y --no-install-recommends ffmpeg
fi
ffmpeg -version
ffprobe -version
