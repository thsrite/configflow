import { createApp } from 'vue'
import 'vue-sonner/style.css'
import './styles/theme.css'
import './styles/tokens.css'
import router from './router'
import './stores/theme'
import App from './App.vue'

createApp(App).use(router).mount('#app')
