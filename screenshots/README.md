# 自动截图

在 `frontend` 目录执行：

```bash
pnpm install
pnpm exec playwright install chromium # 首次使用时安装浏览器
pnpm screenshots
# 生成与英文逐一对应的简体中文版
pnpm screenshots --locale=zh-CN
```

输出到本目录：

- `screenshot-v2-overview-light.png`：Overview，展示统计、运行中的隧道和最近活动。
- `screenshot-v2-tunnels-light.png`：隧道列表，展示 SSH、Dev Tunnel、Cloudflare、FRP。
- `screenshot-v2-providers-ssh-light.png`：SSH 跳板机，展示 SSH Key、SSH Agent 与关联隧道。
- `screenshot-v2-providers-devtunnel-light.png`：Dev Tunnel 账号、CLI、区域、端口和资源。
- `screenshot-v2-providers-cloudflare-light.png`：Cloudflare 域名授权、凭据和资源。
- `screenshot-v2-providers-frp-light.png`：FRP CLI 和两台示例服务器。
- `screenshot-v2-sync-light.png`：已同步状态、版本、设备和连接信息。
- `screenshot-v2-logs-light.png`：Info、Warn、Error 示例日志。
- `screenshot-v2-config-light.png`：外观、系统设置与数据管理。
- `screenshot-v2-config-license-light.png`：同一 Config 页面滚动到底部，展示数据管理与 Pro 授权。

默认生成英文截图；`--locale=zh-CN` 生成简体中文版，文件统一以 `screenshot-v2-cn-` 开头，其余名称与上面的英文文件一致（例如 `screenshot-v2-cn-overview-light.png`）。两种语言复用相同示例数据、页面顺序和滚动位置，10 张截图逐一对应；中文 README 引用中文版，英文文件保持独立。

中文命令还生成 `screenshot-v2-cn-overview-green-light.png`，对应官网已有的英文绿色主题截图，供中文轮播使用。

截图固定为浅色主题和 Pro 示例状态；版本号使用正式版显示，不带 `-dev`。窗口尺寸自动读取 `main.go`（当前 1024 × 768 逻辑像素），以 2 倍分辨率生成 2048 × 1536 PNG，不包含原生系统标题栏。通过
`frontend/screenshots/data.js` 调整示例名称、地址和状态。
这些数据仅用于展示，不代表真实连接；脚本不会读取或修改 TOML、登录账号、启动隧道或发送统计事件，因此无需备份或恢复真实配置。
仅 Overview 展示脉冲形流量曲线与上传、下载速度，其他页面隐藏流量区域。

脚本启动临时的本机 Vite 服务，复用真实 Vue 页面和布局，检查渲染错误、Pro 与正式版本显示、四种 Provider 和表格内容可见性后截图，结束时关闭浏览器和服务。
独立入口为 `frontend/screenshots/index.html`，不包含在正式应用的默认构建中。
Sync、Config 和 FRP 的初始化读取由 `frontend/screenshots/bridge.js` 提供只读示例结果；所有写入、连接与隧道操作均不提供模拟成功。该入口拒绝在真实 Wails 环境中运行。
需要手动预览时，执行 `pnpm dev` 并打开 `/screenshots/index.html?page=overview`，侧边栏可切换全部页面；也可通过 `?page=providers&provider=cloudflare` 直接打开某一 Provider。除页面切换、筛选与纯前端弹窗外，不执行真实操作。截图命令会使用正式版显示；手动 `pnpm dev` 预览仍保留开发环境标识。

如果希望使用已有浏览器，可通过环境变量指定可执行文件：

```bash
SCREENSHOT_CHROME_PATH='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' pnpm screenshots
```

更新依赖时保留 Playwright 与其 Chromium 的匹配版本；截图应在同一系统与字体环境中生成，以保持外观一致。
