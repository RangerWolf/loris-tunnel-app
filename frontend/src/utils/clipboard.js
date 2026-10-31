import { ClipboardSetText } from '../../wailsjs/runtime/runtime'

export async function writeTextToClipboard(text) {
  const value = String(text ?? '')
  if (!value) return false
  try {
    if (typeof window !== 'undefined' && window.runtime?.ClipboardSetText) {
      if (await ClipboardSetText(value)) return true
    }
  } catch (_) {}
  try {
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value)
      return true
    }
  } catch (_) {}
  if (typeof document === 'undefined') return false
  const previousFocus = document.activeElement
  const input = document.createElement('textarea')
  input.value = value
  input.setAttribute('readonly', '')
  input.style.position = 'fixed'
  input.style.left = '-9999px'
  try {
    document.body.appendChild(input)
    input.select()
    return document.execCommand('copy')
  } catch (_) {
    return false
  } finally {
    input.remove()
    previousFocus?.focus?.()
  }
}
