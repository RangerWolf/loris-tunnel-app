#!/usr/bin/env bash
set -euo pipefail

APP_NAME="loris-tunnel.app"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_APP="$SCRIPT_DIR/build/bin/$APP_NAME"
INSTALL_DIR="${1:-/Applications}"
TARGET_APP="$INSTALL_DIR/$APP_NAME"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "此脚本只能在 macOS 上运行。" >&2
  exit 1
fi

if ! command -v wails >/dev/null 2>&1; then
  echo "未找到 wails 命令，请先安装 Wails CLI 并确保它在 PATH 中。" >&2
  exit 1
fi

echo "在 $SCRIPT_DIR 执行 wails build..."
(cd "$SCRIPT_DIR" && wails build)

if [[ ! -d "$SOURCE_APP" ]]; then
  echo "构建完成，但没有找到应用包：$SOURCE_APP" >&2
  exit 1
fi

SUDO=""
if [[ ! -w "$INSTALL_DIR" ]]; then
  if ! command -v sudo >/dev/null 2>&1; then
    echo "没有写入 $INSTALL_DIR 的权限，且找不到 sudo。" >&2
    exit 1
  fi
  sudo -v
  SUDO="sudo"
fi

run_privileged() {
  if [[ -n "$SUDO" ]]; then
    sudo "$@"
  else
    "$@"
  fi
}

run_privileged mkdir -p "$INSTALL_DIR"
STAGED_APP="$INSTALL_DIR/.${APP_NAME}.new"

cleanup() {
  run_privileged rm -rf "$STAGED_APP"
}
trap cleanup EXIT

echo "复制构建产物到 $TARGET_APP..."
run_privileged rm -rf "$STAGED_APP"
run_privileged ditto "$SOURCE_APP" "$STAGED_APP"

# 先完整复制到同一目录，再切换目标，避免复制过程中留下半个应用包。
if [[ -e "$TARGET_APP" ]]; then
  run_privileged rm -rf "$TARGET_APP"
fi
run_privileged mv "$STAGED_APP" "$TARGET_APP"
trap - EXIT

echo "已替换应用：$TARGET_APP"
