# Privacy Policy

**Effective date:** May 27, 2026  
**Last updated:** May 27, 2026

This Privacy Policy describes how **Loris Tunnel** (the "App") and its official website (the "Site") collect, use, and protect information when you use our desktop application on macOS or Windows, or visit our website.

If you have questions about this policy, contact us at [yang.rangerwolf@gmail.com](mailto:yang.rangerwolf@gmail.com).

---

## 1. Summary

- Loris Tunnel is a **local SSH tunnel manager**. Your SSH connections go **directly between your device and your servers** — we do not proxy or inspect your tunneled traffic.
- Most sensitive data (SSH passwords, private keys, tunnel configurations) is stored **only on your device**.
- When you use certain optional or built-in features, limited technical data may be sent to our servers or third-party services, as described below.

---

## 2. Information Stored Locally on Your Device

The App stores configuration data on your computer, including:

- SSH jumper and tunnel settings (hostnames, ports, usernames, authentication method)
- SSH passwords or key passphrases (if you choose password or key-based authentication)
- Paths to SSH private key files
- UI preferences (such as language)
- Application logs related to tunnel status and errors

On packaged builds, configuration is typically stored at:

- **Windows:** `%USERPROFILE%\.loris-tunnel\config.toml`
- **macOS:** `~/.loris-tunnel/config.toml`

This local data is **not uploaded** to us unless you explicitly use a feature that sends diagnostic information (see **AI Debug** below). You can delete this data at any time by removing the configuration file or uninstalling the App.

---

## 3. Information Collected by the App

### 3.1 Device identifier

To support license activation and usage reporting, the App generates or reads a **machine identifier** derived from your operating system (for example, Windows MachineGuid). This identifier is not your name or email address. It is used to:

- Check and redeem license codes
- Associate optional AI Debug usage with your device
- Record anonymous usage events (startup and periodic heartbeat)

### 3.2 Usage events (our backend)

When the App runs, it may send the following to our backend API (`https://loris-tunnel-prod.flyml.net`):

| Data | Purpose |
|------|---------|
| Machine identifier | Link events to your device for license and analytics |
| Event type (`startup` or `heartbeat`) | Understand active usage |
| App version | Compatibility and support |
| Platform (e.g. `windows`, `darwin`) | Platform-specific improvements |
| Client timestamp | Event ordering |

We do **not** include your SSH credentials, tunnel contents, or personal files in these events.

### 3.3 License redemption

If you enter a license code, the App sends the **license code** and **machine identifier** to our backend to validate and activate your license. We store activation records associated with your machine identifier.

### 3.4 AI Debug (optional, user-initiated)

When you choose **AI Debug** after a connection failure, the App may send diagnostic information to our backend for automated analysis, including:

- Machine identifier
- Error messages and connection test results
- Tunnel metadata (name, mode, ports, hosts — **not passwords**)
- Jumper metadata (host, port, username, auth type, key file path — **not passwords or key contents**)
- Truncated SSH debug output and network check results
- UI language preference

AI Debug is **not** run automatically. Passwords and private key contents are **never** included in these requests.
If you believe AI Debug generated inappropriate content, use the in-app **Report inappropriate content** action in the AI Debug result card. This opens your default mail client with a prefilled draft to [admin@lorisdev.cc](mailto:admin@lorisdev.cc).

### 3.5 Analytics (Google Analytics)

The App uses **Google Analytics 4** (measurement ID: `G-D5TZJ5BHHX`) to collect anonymous usage statistics, such as:

- App start events
- Page or screen views within the App
- Button clicks and feature interactions (e.g. creating a tunnel, opening settings)
- App version and platform

These events help us understand which features are used and improve the product. They do **not** include SSH credentials or the contents of your tunnels.

Google's processing of this data is governed by [Google's Privacy Policy](https://policies.google.com/privacy).

### 3.6 Update checks (GitHub)

When you check for updates in the GitHub distribution, the App queries the public **GitHub Releases API** for the latest release information. This request is made directly to GitHub and is subject to [GitHub's Privacy Statement](https://docs.github.com/en/site-policy/privacy-policies/github-privacy-statement). The Microsoft Store distribution does not query GitHub; updates are managed by Microsoft Store.

---

## 4. Information Collected by the Website

When you visit our official website, we use **Google Analytics 4** (measurement ID: `G-776FG9SDVQ`) to collect standard web analytics, such as pages visited, approximate location (derived from IP), browser type, and referral source.

You can limit analytics collection through your browser settings or ad/analytics opt-out tools provided by Google.

---

## 5. How We Use Information

We use collected information to:

- Operate and maintain the App and website
- Activate and manage licenses
- Provide AI Debug analysis when you request it
- Monitor aggregate usage and improve reliability and features
- Respond to support requests you send us

We do **not** sell your personal information.

---

## 6. Data Sharing

We may share limited data with:

| Recipient | Purpose |
|-----------|---------|
| **Our backend hosting provider** | Store license, usage, and AI Debug request records |
| **Google (Analytics)** | Anonymous usage analytics for the App and website |
| **GitHub** | Public release metadata when checking for updates |
| **Large language model provider** (via our backend) | Process AI Debug diagnostic text you submit |

We may also disclose information if required by law or to protect our rights and users' safety.

---

## 7. Data Retention

- **Local configuration** remains on your device until you delete it or uninstall the App.
- **Usage events, license records, and AI Debug logs** on our servers are retained for as long as needed to operate the service, enforce license terms, and improve the product, after which they may be deleted or anonymized.
- **Google Analytics** data retention follows Google's default settings and our account configuration.

---

## 8. Security

We use HTTPS for communication with our backend. Sensitive credentials are intended to remain on your device. No method of transmission or storage is 100% secure; please protect your device and SSH credentials accordingly.

---

## 9. Your Choices and Rights

Depending on your location, you may have rights to access, correct, or delete personal data we hold about you. Because most sensitive data stays on your device, you can often fulfill these requests locally by deleting your configuration file.

To request deletion of server-side records associated with your machine identifier, or for any privacy-related inquiry, email [yang.rangerwolf@gmail.com](mailto:yang.rangerwolf@gmail.com).

---

## 10. Children's Privacy

Loris Tunnel is not directed at children under 13 (or the applicable age in your jurisdiction). We do not knowingly collect personal information from children.

---

## 11. International Users

Our services may be operated from jurisdictions outside your country. By using the App or Site, you understand that information may be processed in those locations.

---

## 12. Changes to This Policy

We may update this Privacy Policy from time to time. The "Last updated" date at the top will reflect the latest version. Continued use of the App or Site after changes constitutes acceptance of the updated policy.

---

## 13. Contact

**Loris Tunnel**  
Email: [yang.rangerwolf@gmail.com](mailto:yang.rangerwolf@gmail.com)

---

# 隐私政策（简体中文）

**生效日期：** 2026 年 5 月 27 日  
**最后更新：** 2026 年 5 月 27 日

本隐私政策说明 **Loris Tunnel**（「本应用」）及其官方网站（「本网站」）在您使用 macOS 或 Windows 桌面应用、或访问本网站时，如何收集、使用和保护相关信息。

如有疑问，请联系：[yang.rangerwolf@gmail.com](mailto:yang.rangerwolf@gmail.com)。

---

## 概要

- Loris Tunnel 是**本地 SSH 隧道管理工具**。SSH 连接在**您的设备与您的服务器之间直接建立**，我们不会代理或检查隧道内的流量。
- 大多数敏感数据（SSH 密码、私钥、隧道配置）**仅保存在您的设备上**。
- 在使用部分可选或内置功能时，可能会向我们的服务器或第三方服务发送有限的技术数据，详见下文。

---

## 本地存储的数据

本应用会在您的计算机上保存配置，包括：SSH 跳板机与隧道设置、密码或密钥口令（若您使用相应认证方式）、私钥文件路径、界面语言偏好、隧道运行日志等。

- **Windows：** `%USERPROFILE%\.loris-tunnel\config.toml`
- **macOS：** `~/.loris-tunnel/config.toml`

除非您主动使用 **AI Debug** 等诊断功能，否则上述数据**不会上传**至我们的服务器。您可随时删除配置文件或卸载应用以清除本地数据。

---

## 应用可能收集并发送的数据

### 设备标识符

为支持许可证激活与使用情况统计，本应用会读取或生成**设备标识符**（例如 Windows 的 MachineGuid），用于许可证校验、兑换及匿名使用事件记录。该标识符不是您的姓名或邮箱。

### 使用事件（我们的后端）

应用运行期间可能向 `https://loris-tunnel-prod.flyml.net` 发送：设备标识符、事件类型（启动/心跳）、应用版本、平台信息、客户端时间戳。**不包含** SSH 凭据或隧道内容。

### 许可证兑换

若您输入注册码，应用会将**注册码**与**设备标识符**发送至后端以完成激活。

### AI Debug（可选，需用户主动触发）

连接失败时若您选择 **AI Debug**，可能发送：设备标识符、错误信息与检测结果、隧道/跳板机元数据（主机、端口、用户名等，**不含密码**）、截断后的 SSH 调试输出、界面语言。**密码与私钥内容绝不会发送。**
若您认为 AI Debug 生成了不当内容，可在 AI Debug 结果卡片中点击 **举报不当内容**。应用会打开默认邮箱并预填举报草稿，发送至 [admin@lorisdev.cc](mailto:admin@lorisdev.cc)。

### 分析统计（Google Analytics）

本应用使用 Google Analytics 4（`G-D5TZJ5BHHX`）收集匿名使用统计（如启动、页面浏览、功能点击、版本与平台），**不包含** SSH 凭据。详见 [Google 隐私政策](https://policies.google.com/privacy)。

### 更新检查（GitHub）

检查更新时，应用会访问 GitHub 公开 Releases API，受 [GitHub 隐私声明](https://docs.github.com/en/site-policy/privacy-policies/github-privacy-statement) 约束。

---

## 网站数据

访问官方网站时，我们使用 Google Analytics 4（`G-776FG9SDVQ`）收集常规网站访问统计。

---

## 数据用途、共享与安全

我们使用上述数据以运营服务、管理许可证、在您请求时提供 AI Debug、改进产品并响应支持请求。**我们不会出售您的个人信息。**

数据可能共享给：后端托管服务商、Google（分析）、GitHub（更新检查）、以及通过我们后端处理 AI Debug 的大语言模型服务商。通信采用 HTTPS；敏感凭据原则上保留在您的设备上。

---

## 您的权利与联系我们

您可根据适用法律请求访问、更正或删除我们持有的相关数据。多数敏感数据在本地，删除配置文件即可清除。

如需删除与服务端设备标识符相关的记录，或提出其他隐私相关请求，请发送邮件至 [yang.rangerwolf@gmail.com](mailto:yang.rangerwolf@gmail.com)。

本应用不面向 13 岁以下儿童。我们可能不时更新本政策；继续使用即表示接受更新后的版本。

**Loris Tunnel**  
邮箱：[yang.rangerwolf@gmail.com](mailto:yang.rangerwolf@gmail.com)
