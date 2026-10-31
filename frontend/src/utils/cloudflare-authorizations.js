// These methods are bound by Wails; keep credential material entirely in Go.
function api() {
  const app = window.go?.main?.App
  if (!app) throw new Error('Cloudflare requires the desktop application runtime')
  return app
}
export const startCloudflareZoneAuthorization = zoneId => api().StartCloudflareZoneAuthorization(zoneId || '')
export const removeCloudflareAuthorization = zoneId => api().RemoveCloudflareAuthorization(zoneId)
