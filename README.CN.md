# Loris Tunnel

<div align="center">

[English](README.md)

**一款桌面 GUI 应用，用于管理 SSH 隧道——支持自动重连，界面简洁易用。**

![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Windows-blue)
![License](https://img.shields.io/badge/license-Apache%202.0-green)
![Built with Wails](https://img.shields.io/badge/built%20with-Wails-informational)

</div>

---

## 项目简介

**Loris Tunnel** 是一款跨平台桌面应用，让你通过图形界面创建、管理和监控 SSH 隧道。它将 SSH 端口转发的能力封装成直观的 UI，并支持自动重连，即使网络不稳定也能保持隧道畅通。

适合经常需要访问防火墙后面的远程服务器、数据库和内网服务的开发者与运维工程师——不用每次都敲命令行。

![总览](screenshots/screenshot-overview.png)

---

## 功能特性

- 🖥️ **图形化隧道管理** — 通过简洁的桌面 UI 创建、编辑、启动、停止和监控所有 SSH 隧道，日常使用无需命令行
- 🔄 **深度优化的自动重连机制** — 采用智能指数退避算法，确保隧道在网络波动或 SSH 断连后能够自动静默恢复，重连状态实时可见
- ⚡ **实时延迟监测与最优线路选择** — 持续探测并展示各线路 SSH 延迟，支持按响应时间排序并一键切换到最快线路
- 📥 **从 SSH 命令批量导入** — 粘贴任意 `ssh` 命令，自动解析所有 `-L`/`-R`/`-D` 参数，一次性创建多条隧道
- 📋 **一键复制隧道** — 克隆现有隧道快速创建变体，无需重复填写所有字段
- ⛓️ **多跳跳板机链** — 为单条隧道配置多个 SSH 跳板机，支持深层嵌套网络（如 堡垒机 → 内网主机）
- 🔀 **本地、远程与动态（SOCKS5）端口转发** — 支持所有标准 SSH 隧道模式
- ✅ **内置连接测试** — 在创建/编辑对话框中直接测试隧道是否可达，保存前即可验证
- ▶️ **启动时自动开启** — 将隧道标记为自动启动，应用打开后立即连接
- 🌍 **跨平台** — 支持 macOS 和 Windows
- 💬 **多语言 UI** — English 与 简体中文

---

## 截图

**从 SSH 命令批量导入隧道：**

![从 SSH 命令导入](screenshots/screenshot-create-tunnels-from-ssh-command.png)

**实时显示 SSH 连接延迟：**

![SSH 延迟](screenshots/screenshot-show-ssh-latency.png)

---

## 快速开始

### 下载安装

前往 [Releases](../../releases) 页面下载最新版本：

- **macOS**：`.dmg` 安装包
- **Windows**：`.exe` 安装包

### macOS 运行隔离放行方法

首次运行 macOS 版本时，可能会遇到系统安全拦截。以下是两种解除方法：

**方法一：系统设置放行**

1. 打开应用遇到拦截弹窗后，点击"取消"
2. 打开系统的"系统设置" -> "隐私与安全性"
3. 向下滚动，找到安全性板块，点击"仍要打开"

**方法二：终端一键解除**

打开终端（Terminal），输入以下命令并回车（可能需要输入电脑密码）：

```bash
sudo xattr -rd com.apple.quarantine /Applications/loris-tunnel.app
```

### 从源码构建

**前置依赖：**

- [Go](https://golang.org/dl/) ≥ 1.21
- [Node.js](https://nodejs.org/) ≥ 18 + [pnpm](https://pnpm.io/)
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2

```bash
git clone https://github.com/YOUR_USERNAME/loris-tunnel.git
cd loris-tunnel

# 安装前端依赖
cd frontend && pnpm install && cd ..

# 开发模式运行
wails dev

# 构建生产版本
wails build
```

---

## 配置文件

Loris Tunnel 使用 TOML 格式存储配置，路径解析规则如下：

- **开发模式（`wails dev`）**：
  - 若当前工作目录可写：使用 `./config.toml`
  - 否则：使用 `~/.loris-tunnel/config.toml`
- **打包后的生产版本（二进制，可执行文件，包括开机自启动）**：
  - 始终使用 `~/.loris-tunnel/config.toml`

在以上任意模式下，如果目标配置文件不存在或内容为空，Loris Tunnel 会在首次运行时自动创建一个默认配置文件。

配置示例：

```toml
[[jumpers]]
name = "my-server"
host = "example.com"
port = 22
user = "ubuntu"
auth_method = "agent"      # 或 "key" / "password"
# identity = "~/.ssh/id_rsa"

[[tunnels]]
name = "本地数据库"
jumper = "my-server"
mode = "local"             # local | remote | socks5
local_port = 5432
remote_host = "127.0.0.1"
remote_port = 5432
```

---

## 隧道模式说明

| 模式 | 说明 |
|------|------|
| `local` | 将本地端口转发到远程地址（通过 SSH 服务器） |
| `remote` | 将 SSH 服务器上的远程端口转发到本地地址 |
| `socks5` | 以 SSH 服务器为 SOCKS5 代理（动态转发） |

---

## 技术栈

| 层 | 技术 |
|----|------|
| 桌面框架 | [Wails](https://wails.io/) v2 |
| 后端 | Go |
| 前端 | Vue 3 + Vite |
| SSH | Go `golang.org/x/crypto/ssh` |

---

## 社区交流

欢迎加入 QQ 群参与讨论、获取支持和最新动态：

- 💬 **QQ 交流群**: **1009737419**

---

## 开源协议

Apache License 2.0。
