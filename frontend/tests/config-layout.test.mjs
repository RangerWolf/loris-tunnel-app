import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { parse } from 'vue/compiler-sfc'

const source = readFileSync(new URL('../src/components/pages/ConfigPage.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const template = descriptor.template.content
const styles = descriptor.styles[0].content

test('Config uses the same label/value grid as Sync without row dividers', () => {
  assert.equal((template.match(/class="lt-kv config-kv"/g) || []).length, 4)
  assert.doesNotMatch(template, /class="config-row|class="config-name|class="config-desc/)
  assert.match(styles, /\.config-grid \{[^}]*grid-template-columns: minmax\(0, 1fr\)/)
  assert.doesNotMatch(styles, /border-bottom|config-row/)
  const pairs = [...template.matchAll(/<dt class="text-body-secondary">([\s\S]*?)<\/dt>\s*<dd>([\s\S]*?)<\/dd>/g)]
  assert.equal(pairs.length, 9)
  for (const [label, description] of [
    ['language', 'languageDesc'],
    ['brandColor', 'brandColorDesc'],
    ['theme', 'themeDesc'],
    ['autoRun', 'autoRunDesc'],
    ['trafficMonitor', 'trafficMonitorDesc'],
    ['manageConfig', 'manageConfigDesc'],
  ]) {
    const pair = pairs.find(([, key]) => key.includes(`t('config.${label}')`))
    assert.ok(pair, `missing label ${label}`)
    assert.match(pair[2], new RegExp(`t\\('config\\.${description}'\\)`), `description for ${label} must be in the value column`)
  }
})

test('Config shows two accessible accent swatches with selection feedback', () => {
  const pair = [...template.matchAll(/<dt class="text-body-secondary">([\s\S]*?)<\/dt>\s*<dd>([\s\S]*?)<\/dd>/g)]
    .find(([, label]) => label.includes("t('config.brandColor')"))
  assert.ok(pair, 'missing accent color setting')
  const swatches = pair[2]
  assert.match(swatches, /role="group"/)
  assert.match(swatches, /aria-describedby="config-brand-color-desc"/)
  assert.equal((swatches.match(/class="config-accent-swatch config-accent-swatch--/g) || []).length, 2)
  assert.doesNotMatch(swatches, /<select/)
  for (const color of ['blue', 'green']) {
    assert.ok(swatches.includes(`config-accent-swatch--${color}`))
    assert.ok(swatches.includes(`:aria-pressed="accentColor === '${color}'"`))
    assert.ok(swatches.includes(`:aria-label="t('config.brand${color[0].toUpperCase() + color.slice(1)}')"`))
    assert.ok(swatches.includes(`$emit('accent-color-change', '${color}')`))
    assert.ok(swatches.includes(`v-if="accentColor === '${color}'"`))
  }
  assert.match(swatches, /bi-check-lg/)
  assert.match(styles, /\.config-accent-swatch\.is-selected \{[^}]*box-shadow:/)
  assert.match(styles, /\.config-accent-swatch:focus-visible/)
})

test('Config retains its controls and keeps data and license details in value column', () => {
  for (const fragment of [
    "onAutoRunChange($event.target.checked, $event.target)",
    "onTrafficMonitorChange($event.target.checked)",
    "$emit('theme-change', $event.target.checked)",
    "$emit('accent-color-change', 'blue')",
    "$emit('accent-color-change', 'green')",
    '@click="onImportConfig"',
    '@click="onExportConfig"',
    '@click="onOpenConfigDir"',
    '@click="onChooseConfigDataDir"',
    '@click="onResetConfigDataDir"',
    '<CopyIconButton :value="configLocationInfo.effectiveConfigDir"',
    "@click=\"$emit('check-updates')\"",
    "@click=\"$emit('refresh-license-status')\"",
    "@click=\"$emit('upgrade', 'activate')\"",
    "class=\"config-path-row\"",
  ]) {
    assert.ok(template.includes(fragment), `missing expected control: ${fragment}`)
  }
  const pairs = [...template.matchAll(/<dt class="text-body-secondary">([\s\S]*?)<\/dt>\s*<dd>([\s\S]*?)<\/dd>/g)]
  const directoryValue = pairs.find(([, label]) => label.includes("t('config.configDataDir')"))?.[2]
  const licenseValue = pairs.find(([, label]) => label.includes("t('config.licenseStatus')"))?.[2]
  assert.match(directoryValue, /config-path-row[\s\S]*config-dir-actions/)
  assert.match(licenseValue, /license-secret[\s\S]*purchase.haveCode/)
})
