const DEFAULT_BACKEND_API_BASE_URL = 'http://localhost:8000/api/v1'
const REQUEST_TIMEOUT_MS = 10000

function trimTrailingSlash(url) {
  return String(url || '').replace(/\/+$/, '')
}

export const BACKEND_API_BASE_URL = trimTrailingSlash(
  import.meta.env.VITE_BACKEND_API_BASE_URL || DEFAULT_BACKEND_API_BASE_URL
)

function buildUrl(path, query = {}) {
  const url = new URL(`${BACKEND_API_BASE_URL}${path}`)
  Object.entries(query).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    url.searchParams.set(key, String(value))
  })
  return url.toString()
}

function extractErrorMessage(payload, status) {
  if (payload && typeof payload === 'object') {
    if (typeof payload.detail === 'string' && payload.detail) return payload.detail
    if (typeof payload.message === 'string' && payload.message) return payload.message
  }
  return `Request failed (HTTP ${status})`
}

async function request(path, options = {}) {
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
  const { query = {}, ...fetchOptions } = options
  const headers = {
    Accept: 'application/json',
    ...(fetchOptions.headers || {})
  }
  const hasBody = typeof fetchOptions.body !== 'undefined' && fetchOptions.body !== null
  const hasContentTypeHeader = Object.keys(headers).some((key) => key.toLowerCase() === 'content-type')
  if (hasBody && !hasContentTypeHeader) {
    headers['Content-Type'] = 'application/json'
  }

  try {
    const response = await fetch(buildUrl(path, query), {
      ...fetchOptions,
      headers,
      signal: controller.signal
    })

    let payload = null
    const contentType = response.headers.get('content-type') || ''
    if (contentType.includes('application/json')) {
      payload = await response.json()
    }

    if (!response.ok) {
      throw new Error(extractErrorMessage(payload, response.status))
    }
    return payload
  } catch (error) {
    if (error?.name === 'AbortError') {
      throw new Error('Request timeout while connecting to backend API.')
    }
    if (error instanceof TypeError) {
      throw new Error(`Cannot connect to backend API at ${BACKEND_API_BASE_URL}. Please make sure localhost:8000 is running.`)
    }
    throw error
  } finally {
    window.clearTimeout(timeout)
  }
}

export async function checkUpdate(params) {
  return request('/app/check-update', {
    method: 'GET',
    query: params
  })
}

export async function getLicenseStatus(machineId) {
  return request('/license/status', {
    method: 'GET',
    query: { machine_id: machineId }
  })
}

export async function redeemLicenseCode({ code, machineId }) {
  return request('/license/redeem', {
    method: 'POST',
    body: JSON.stringify({
      code,
      machine_id: machineId
    })
  })
}
