function api() {
  const app = window.go?.main?.App
  if (!app) throw new Error('CLI maintenance requires the desktop application runtime')
  return app
}
export const getCLIMaintenance = provider => api().GetCLIMaintenance(provider)
export const checkCLIUpdate = (provider, force) => api().CheckCLIUpdate(provider, force)
export const importCLIUpdate = provider => api().ImportCLIUpdate(provider)
export const applyCLIUpdate = (provider, id) => api().ApplyCLIUpdate(provider, id)
export const restorePreviousCLI = provider => api().RestorePreviousCLI(provider)
export const cancelCLIUpdate = provider => api().CancelCLIUpdate(provider)
export const openCLIDirectory = provider => api().OpenCLIDirectory(provider)
