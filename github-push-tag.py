#!/usr/bin/env python3
"""
github-push-tag.py

创建并推送 Git tag 到 GitHub。
- 无参数：从 wails.json 的 info.productVersion 读取版本作为 tag，交互确认后 push。
- 一个参数：使用该参数作为 tag，交互确认后 push。
"""

import json
import subprocess
import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
WAILS_CONFIG = SCRIPT_DIR / "wails.json"


def extract_version_from_wails_config():
    """从 wails.json 中读取 info.productVersion。"""
    try:
        config = json.loads(WAILS_CONFIG.read_text(encoding="utf-8"))
        version = config["info"]["productVersion"]
    except (OSError, ValueError, KeyError, TypeError) as exc:
        sys.exit(f"错误：无法读取 wails.json 中的 info.productVersion：{exc}")
    if not isinstance(version, str) or not version.strip():
        sys.exit("错误：wails.json 中的 info.productVersion 必须是非空字符串。")
    return version.strip()


def normalize_tag(version: str) -> str:
    """若版本号未带 v 前缀，则加上（常见 tag 写法）。"""
    if not version:
        return version
    return version if version.startswith("v") else f"v{version}"


def run_cmd(cmd, check=True):
    result = subprocess.run(cmd, shell=False)
    if check and result.returncode != 0:
        sys.exit(result.returncode)
    return result.returncode


def main():
    if len(sys.argv) > 2:
        print("用法: python github-push-tag.py [tag]")
        print("  无参数: 从 wails.json 读取 info.productVersion 作为 tag")
        print("  一个参数: 使用该参数作为 tag")
        sys.exit(1)

    if len(sys.argv) == 2:
        tag = sys.argv[1].strip()
        if not tag:
            sys.exit("错误: tag 不能为空。")
        print(f"使用自定义 tag: {tag}")
    else:
        version = extract_version_from_wails_config()
        tag = normalize_tag(version)
        print(f"从 wails.json 读取 info.productVersion: {version}")
        print(f"生成 tag: {tag}")

    print()
    confirm = input(f"确认将 tag '{tag}' 推送到 GitHub? [y/N]: ").strip().lower()
    if confirm != "y" and confirm != "yes":
        print("已取消。")
        sys.exit(0)

    print(f"\n执行: git tag {tag}")
    run_cmd(["git", "tag", tag])
    print(f"执行: git push origin {tag}")
    run_cmd(["git", "push", "origin", tag])
    print(f"\n✅ tag '{tag}' 已推送。")


if __name__ == "__main__":
    main()
