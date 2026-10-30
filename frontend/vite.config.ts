import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'

export default defineConfig({
  plugins: [
    vue(),
    ui({
      colorMode: false,
      ui: { colors: { primary: 'emerald', neutral: 'stone' } },
    }),
  ],
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
  },
})
