import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import ui from '@nuxt/ui/vue-plugin'
import App from './App.vue'
import DashboardPage from './pages/DashboardPage.vue'
import './assets/main.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [{ path: '/', component: DashboardPage }],
})

createApp(App).use(router).use(ui).mount('#app')
