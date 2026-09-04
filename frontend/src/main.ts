import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'vue-sonner/style.css'
import './styles/theme.css'
import './styles/tokens.css'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import router from './router'
import './stores/theme'
import App from './App.vue'

const app = createApp(App)

// 注册所有图标
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

app.use(ElementPlus)
app.use(router)
app.mount('#app')
