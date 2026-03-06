#!/usr/bin/env python3
"""
github-push-tag.py

创建并推送 Git tag 到 GitHub。
- 无参数：从 frontend/src/App.vue 的 appMeta.version 抽取版本作为 tag，交互确认后 push。
- 一个参数：使用该参数作为 tag，交互确认后 push。
"""

import re
import subprocess
import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
APP_VUE = SCRIPT_DIR / "frontend" / "src" / "App.vue"


def extract_version_from_app_vue():
    """从 App.vue 中解析 appMeta.version 的值。"""
    text = APP_VUE.read_text(encoding="utf-8")
    # 匹配 version: '...' 或 version: "..."
    m = re.search(r"version:\s*['\"]([^'\"]+)['\"]", text)
    if not m:
        sys.exit("错误：在 App.vue 中未找到 appMeta.version。")
    return m.group(1).strip()


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
        print("  无参数: 从 App.vue 读取 version 作为 tag")
        print("  一个参数: 使用该参数作为 tag")
        sys.exit(1)

    if len(sys.argv) == 2:
        tag = sys.argv[1].strip()
        if not tag:
            sys.exit("错误: tag 不能为空。")
        print(f"使用自定义 tag: {tag}")
    else:
        version = extract_version_from_app_vue()
        tag = normalize_tag(version)
        print(f"从 App.vue 读取 version: {version}")
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
