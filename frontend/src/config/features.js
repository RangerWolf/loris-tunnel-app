/** 设为 true 可恢复 jumper/tunnel 连接失败时的 AI 诊断 UI */
export const AI_DEBUG_ENABLED = false

export const ACTIVE_TUNNEL_LIMIT = 3

export const providerCapabilities = {
  ssh: { enabled: true, hasJumpers: true, hasLatency: true, requiresAccount: false },
  devtunnel: { enabled: true, hasJumpers: false, hasLatency: false, requiresAccount: true },
  tailscale: { enabled: false, hasJumpers: false, hasLatency: false, requiresAccount: true },
  frp: { enabled: true, hasJumpers: false, hasLatency: false, requiresAccount: false },
  cloudflare: { enabled: true, hasJumpers: false, hasLatency: false, requiresAccount: true },
}
