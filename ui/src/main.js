import { createApp } from 'vue'
import './style.css'
import App from './App.vue'
import router from './router'
import axios from 'axios'

// 配置axios
axios.defaults.baseURL = '/api'
axios.defaults.timeout = 10000

// 创建Vue应用
const app = createApp(App)

// 注册全局属性
app.config.globalProperties.$axios = axios

// 使用路由
app.use(router)

// 挂载应用
app.mount('#app')