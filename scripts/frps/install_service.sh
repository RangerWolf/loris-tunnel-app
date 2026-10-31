#!/usr/bin/env bash
# 将同目录的 frps.service 安装到 systemd。不创建、不修改 frps.toml。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_UNIT="$SCRIPT_DIR/frps.service"
TARGET_UNIT=/etc/systemd/system/frps.service

if [[ "$(id -u)" -eq 0 ]]; then
  run() { "$@"; }
else
  run() { sudo "$@"; }
fi

run install -m 0644 "$SOURCE_UNIT" "$TARGET_UNIT"
run systemctl daemon-reload
run systemctl enable frps
run systemctl restart frps
run systemctl --no-pager --full status frps
