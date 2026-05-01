const GA_TRACKING_ID = 'G-D5TZJ5BHHX'

export const AnalyticsEvent = {
  PAGE_VIEW: 'page_view',
  APP_START: 'app_start',
  BUTTON_CLICK: 'button_click',
  MODAL_OPEN: 'modal_open',
  MODAL_CLOSE: 'modal_close',
  FORM_SUBMIT: 'form_submit',
  TOGGLE_SWITCH: 'toggle_switch',
  SEARCH_QUERY: 'search_query',
  CONNECTION_TEST: 'connection_test',
  TUNNEL_ACTION: 'tunnel_action',
  JUMPER_ACTION: 'jumper_action',
  SETTINGS_CHANGE: 'settings_change'
}

export const AnalyticsPage = {
  OVERVIEW: 'overview',
  JUMPERS: 'jumpers',
  TUNNELS: 'tunnels',
  LOGS: 'logs',
  CONFIG: 'config'
}

export const AnalyticsAction = {
  SWITCH_PAGE: 'switch_page',
  CREATE: 'create',
  EDIT: 'edit',
  DELETE: 'delete',
  TOGGLE: 'toggle',
  IMPORT: 'import',
  EXPORT: 'export',
  TEST: 'test',
  SAVE: 'save',
  CANCEL: 'cancel',
  UPGRADE: 'upgrade',
  CHECK_UPDATE: 'check_update',
  OPEN: 'open',
  CLOSE: 'close'
}

function isGAEnabled() {
  return typeof window !== 'undefined' && typeof window.gtag === 'function'
}

export function trackPageView(page, appVersion) {
  if (!isGAEnabled()) return
  window.gtag('event', AnalyticsEvent.PAGE_VIEW, {
    page_location: window.location.href,
    page_title: page,
    app_version: appVersion,
    page_name: page
  })
}

export function trackAppStart(appVersion, platform) {
  if (!isGAEnabled()) return
  window.gtag('event', AnalyticsEvent.APP_START, {
    app_version: appVersion,
    platform: platform || 'unknown'
  })
}

export function trackEvent(category, action, label, params = {}) {
  if (!isGAEnabled()) return
  window.gtag('event', action, {
    event_category: category,
    event_label: label,
    ...params
  })
}

export function trackButtonClick(buttonName, page, additionalParams = {}) {
  trackEvent(page, AnalyticsEvent.BUTTON_CLICK, buttonName, additionalParams)
}

export function trackModalOpen(modalName, page, additionalParams = {}) {
  trackEvent(page, AnalyticsEvent.MODAL_OPEN, modalName, additionalParams)
}

export function trackModalClose(modalName, page, additionalParams = {}) {
  trackEvent(page, AnalyticsEvent.MODAL_CLOSE, modalName, additionalParams)
}

export function trackFormSubmit(formName, page, success = true, additionalParams = {}) {
  trackEvent(page, AnalyticsEvent.FORM_SUBMIT, formName, {
    success,
    ...additionalParams
  })
}

export function trackToggleSwitch(switchName, page, newValue, additionalParams = {}) {
  trackEvent(page, AnalyticsEvent.TOGGLE_SWITCH, switchName, {
    new_value: newValue,
    ...additionalParams
  })
}

export function trackSearch(page, searchQuery, resultCount = 0) {
  trackEvent(page, AnalyticsEvent.SEARCH_QUERY, searchQuery, {
    result_count: resultCount
  })
}

export function trackConnectionTest(testType, page, success, message = '') {
  trackEvent(page, AnalyticsEvent.CONNECTION_TEST, testType, {
    success,
    error_message: message
  })
}

export function trackTunnelAction(action, tunnelName, additionalParams = {}) {
  trackEvent('tunnel', AnalyticsEvent.TUNNEL_ACTION, action, {
    tunnel_name: tunnelName,
    ...additionalParams
  })
}

export function trackJumperAction(action, jumperName, additionalParams = {}) {
  trackEvent('jumper', AnalyticsEvent.JUMPER_ACTION, action, {
    jumper_name: jumperName,
    ...additionalParams
  })
}

export function trackSettingsChange(settingName, newValue, additionalParams = {}) {
  trackEvent('settings', AnalyticsEvent.SETTINGS_CHANGE, settingName, {
    new_value: newValue,
    ...additionalParams
  })
}

export default {
  trackAppStart,
  trackPageView,
  trackEvent,
  trackButtonClick,
  trackModalOpen,
  trackModalClose,
  trackFormSubmit,
  trackToggleSwitch,
  trackSearch,
  trackConnectionTest,
  trackTunnelAction,
  trackJumperAction,
  trackSettingsChange
}
