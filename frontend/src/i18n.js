import { createI18n } from 'vue-i18n'
import en from './locales/en.json'
import zhCN from './locales/zh-CN.json'

// Detect default language
const savedLocale = localStorage.getItem('loris-tunnel.locale')
const systemLocale = navigator.language.startsWith('zh') ? 'zh-CN' : 'en'
const locale = savedLocale || systemLocale

const i18n = createI18n({
    legacy: false, // Use Composition API
    locale: locale,
    fallbackLocale: 'en',
    messages: {
        en,
        'zh-CN': zhCN
    }
})

export default i18n
