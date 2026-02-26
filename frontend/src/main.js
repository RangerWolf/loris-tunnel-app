import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'
import 'bootstrap/dist/css/bootstrap.min.css'
import 'bootstrap/dist/js/bootstrap.bundle.min.js'
import 'bootstrap-icons/font/bootstrap-icons.css'
import './style.css';

const app = createApp(App)
app.use(i18n)
app.mount('#app')
