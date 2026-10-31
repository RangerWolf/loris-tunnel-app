import { frpStatus, syncStatus } from './data'

// Only the standalone screenshot entry imports this read-only fixture bridge.
// Unlisted actions reject; they never pretend to connect, save, or start tunnels.
const reads = {
  GetFRPStatus: () => frpStatus,
  GetCloudSyncStatus: () => syncStatus,
  GetAutoRunEnabled: () => true,
  GetTrafficMonitorEnabled: () => true,
  GetConfigLocationInfo: () => ({ effectiveConfigDir: '/Users/demo/Library/Application Support/loris-tunnel', isCustomConfigDir: false }),
}
export const fixtureCalls = []
export function installPreviewBridge() {
  if (window.go || window.runtime) throw new Error('Screenshot preview must not run inside Wails')
  window.go = { main: { App: new Proxy({}, {
    get: (_, method) => async () => {
      fixtureCalls.push(method)
      if (!Object.hasOwn(reads, method)) throw new Error(`Screenshot preview is read-only: ${String(method)}`)
      return structuredClone(reads[method]())
    },
  }) } }
  window.runtime = { EventsOnMultiple: () => () => {} }
  window.__screenshotFixtureCalls = fixtureCalls
}
