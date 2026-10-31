# Loris Tunnel 开发约束

## UI 设计：禁止渐变色（强制）

- **所有页面、组件、弹窗、按钮和图表背景必须使用纯色**。禁止使用任何装饰性渐变，包括 `linear-gradient()`、`radial-gradient()`、`conic-gradient()` 及其 repeating 变体；不要用图片或 SVG 渐变变相实现。
- 不要因为“美化”“现代化”或参考其他产品而擅自加入渐变。优先用纯色、细边框、间距、字体层级和必要的状态色区分信息。
- 设计参考采用紧凑、克制、扁平的桌面工具风格；兼顾浅色和深色主题，圆角保持小幅度，不为追求视觉效果增大圆角。
- 修改已有 UI 时，如涉及渐变背景，应改成合适的纯色，并保持原有的交互及功能完整。
- UI 修改完成后搜索前端代码中的 `gradient(`、`linearGradient`、`radialGradient`、`<linearGradient` 等写法，确保没有引入新的渐变效果；已有遗留渐变也应在本次相关修改范围内清理。

## 输入框：不要自动大写首字符

- macOS WebView 对未声明的文本框会把首字母自动大写。新增或修改 `<input type="text">`、`<input type="search">`、`<textarea>` 时，加上 `autocapitalize="none"`、`autocorrect="off"`、`spellcheck="false"`。
- 名称、地址、域名、路径、搜索、Token、代理名都按用户原样输入，不要依赖系统大写。
- 例外：升级弹窗的注册码、云同步 Token 保持现在的输入行为，不要补这三项属性。
- 数字、勾选、密码、文件选择不受此影响。

## 改造原则

- 优先复用现有 Vue 组件、Go/Wails API、SSH Tunnel 数据模型与操作逻辑，不在 UI 改造中重写业务逻辑。
- 未实现的 Provider / Tunnel 功能必须明确隐藏或禁用；禁止使用模拟成功结果伪装实际连接成功。
- 在不明确要求的情况下，不修改生产配置、不自动发布、不提交或推送 Git 提交。
