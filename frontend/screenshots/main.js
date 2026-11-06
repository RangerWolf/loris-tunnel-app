// Separate entry: never loads App.vue, real configuration, analytics or a Wails backend.
import { installPreviewBridge } from './bridge'
import { createApp, nextTick } from 'vue'
import 'bootstrap/dist/css/bootstrap.min.css'
import 'bootstrap/dist/js/bootstrap.bundle.min.js'
import 'bootstrap-icons/font/bootstrap-icons.css'
import '../src/style.css'
import '../src/styles/app-shell.css'
import '../src/styles/ssh-ui-m1.css'
import '../src/styles/providers-page.css'
import '../src/styles/theme-overrides.css'
import '../src/styles/theme.css'
import i18n from '../src/i18n'
import Preview from './Preview.vue'

const locale = new URLSearchParams(location.search).get('locale')
i18n.global.locale.value = locale === 'zh-CN' ? 'zh-CN' : 'en'
document.documentElement.lang = i18n.global.locale.value
installPreviewBridge()
createApp(Preview).use(i18n).mount('#app')
await nextTick()
document.documentElement.dataset.screenshotReady = 'true'
