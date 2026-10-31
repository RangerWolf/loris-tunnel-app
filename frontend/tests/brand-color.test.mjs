import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

const css = readFileSync(new URL('../src/styles/theme.css', import.meta.url), 'utf8')
const baseCss = readFileSync(new URL('../src/style.css', import.meta.url), 'utf8')
const app = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')

function brandTokens(selector) {
  const rule = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
    .find(([, name]) => name.trim().split('\n').at(-1).trim() === selector)
  assert.ok(rule, `Missing brand palette: ${selector}`)
  return Object.fromEntries(
    [...rule[2].matchAll(/(--lt-brand-[\w-]+):\s*([^;]+);/g)].map(([, key, value]) => [key, value.trim()])
  )
}

test('blue is the default, and both colors define complete light and dark palettes', () => {
  const blue = brandTokens(':root')
  const blueDark = brandTokens(':root[data-theme="dark"]')
  const green = brandTokens(':root[data-accent="green"]')
  const greenDark = brandTokens(':root[data-theme="dark"][data-accent="green"]')

  assert.equal(blue['--lt-brand-ink'], '#1b6be0')
  assert.equal(blueDark['--lt-brand-ink'], '#5a9fff')
  assert.equal(green['--lt-brand-ink'], '#0f766e')
  assert.equal(greenDark['--lt-brand-ink'], '#4fd1c5')
  for (const palette of [blueDark, green, greenDark]) {
    assert.deepEqual(Object.keys(palette).sort(), Object.keys(blue).sort())
  }
})

test('blue uses cool neutral surfaces while green preserves original light surfaces', () => {
  const properties = [
    '--lt-bg', '--lt-surface-soft',
    '--lt-tunnel-group-bg', '--lt-tunnel-group-border',
    '--lt-jumper-section-bg', '--lt-jumper-section-border',
    '--lt-border', '--lt-border-strong',
  ]
  const tokens = (selector) => {
    const rule = [...baseCss.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
      .find(([, name]) => name.trim().split('\n').at(-1).trim() === selector)
    assert.ok(rule, `Missing surface palette: ${selector}`)
    return Object.fromEntries(
      [...rule[2].matchAll(/(--lt-[\w-]+):\s*([^;]+);/g)].map(([, key, value]) => [key, value.trim()])
    )
  }

  const blue = tokens(':root')
  const green = tokens(':root[data-accent="green"]')
  const dark = tokens(':root[data-theme="dark"]')
  assert.equal(blue['--lt-bg'], '#f5f7fa')
  assert.equal(blue['--lt-surface-soft'], '#f0f3f8')
  assert.equal(green['--lt-bg'], '#f5f6f4')
  assert.equal(green['--lt-surface-soft'], '#eef0ed')
  for (const key of properties) {
    assert.ok(blue[key] && green[key] && dark[key], `Missing neutral surface token: ${key}`)
  }
  assert.ok(
    baseCss.indexOf(':root[data-accent="green"]') < baseCss.indexOf(':root[data-theme="dark"]'),
    'Dark surfaces must override green light colors'
  )
})

test('accent selection is independent of light/dark theme and persisted locally', () => {
  assert.match(app, /savedAccentColor === 'green' \? 'green' : 'blue'/)
  assert.match(app, /setAttribute\('data-accent', accentColor\.value\)/)
  assert.match(app, /localStorage\.setItem\('lt\.accent', color\)/)
  assert.match(app, /function setAccentColor\(color\)/)
  assert.match(app, /:accent-color="accentColor"/)
  assert.match(app, /@accent-color-change="setAccentColor"/)
  assert.match(app, /setAttribute\('data-theme', theme\.value\)/)
  assert.match(app, /setAttribute\('data-bs-theme', theme\.value\)/)
})
