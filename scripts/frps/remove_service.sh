#!/usr/bin/env bash
# 停止并删除 systemd 中的 frps 单元。不删除 frps.toml 和 frps 程序。
set -euo pipefail

TARGET_UNIT=/etc/systemd/system/frps.service

if [[ "$(id -u)" -eq 0 ]]; then
  run() { "$@"; }
else
  run() { sudo "$@"; }
fi

run systemctl disable --now frps >/dev/null 2>&1 || true
run rm -f "$TARGET_UNIT"
run systemctl daemon-reload
run systemctl reset-failed frps >/dev/null 2>&1 || true

echo "已移除 $TARGET_UNIT"
