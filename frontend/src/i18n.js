import { createI18n } from 'vue-i18n'
import en from './locales/en.json'
import zhCN from './locales/zh-CN.json'
import zhTW from './locales/zh-TW.json'
import zhHK from './locales/zh-HK.json'
import ru from './locales/ru.json'

function detectSystemLocale() {
    const lang = navigator.language.toLowerCase()
    if (lang.startsWith('zh')) {
        if (lang.includes('hk') || lang.includes('mo')) return 'zh-HK'
        if (lang.includes('tw') || lang === 'zh-hant') return 'zh-TW'
        return 'zh-CN'
    }
    if (lang.startsWith('ru')) return 'ru'
    return 'en'
}

const savedLocale = localStorage.getItem('loris-tunnel.locale')
const locale = savedLocale || detectSystemLocale()

const i18n = createI18n({
    legacy: false, // Use Composition API
    locale: locale,
    fallbackLocale: 'en',
    messages: {
        en,
        'zh-CN': zhCN,
        'zh-TW': zhTW,
        'zh-HK': zhHK,
        ru
    }
})

export default i18n
