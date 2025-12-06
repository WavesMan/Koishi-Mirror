import { defineConfig, splitVendorChunkPlugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import { execSync } from 'node:child_process'

function getGitMeta() {
  const run = (cmd) => {
    try { return execSync(cmd).toString().trim() } catch { return '' }
  }
  const commit = run('git rev-parse --short HEAD')
  const date = run('git show -s --format=%ci HEAD')
  const branch = run('git rev-parse --abbrev-ref HEAD')
  return { commit, date, branch }
}

export default defineConfig({
  plugins: [vue(), splitVendorChunkPlugin()],
  define: {
    __APP_GIT__: JSON.stringify(getGitMeta()),
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          vue: ['vue'],
          router: ['vue-router'],
          echarts: ['echarts'],
          axios: ['axios']
        }
      }
    }
  }
})
