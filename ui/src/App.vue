<template>
  <div class="app-container">
    <header class="app-header">
      <div class="header-content container">
        <div class="logo-section">
          <img src="./assets/koishi.png" alt="Logo" class="logo-img" />
          <h1 class="logo-text">
            <router-link to="/">WaveYo Koishi Mirror</router-link>
          </h1>
        </div>
        <nav class="nav-menu">
          <router-link to="/" class="nav-link">首页</router-link>
          <router-link to="/packages" class="nav-link">包列表</router-link>
          <router-link to="/stats" class="nav-link">统计</router-link>
          <a href="https://github.com/WavesMan/Koishi-Mirror" target="_blank" class="nav-link github-link">
            GitHub
          </a>
        </nav>
      </div>
    </header>

    <main class="app-main">
      <router-view v-slot="{ Component }">
        <transition name="fade" mode="out-in">
          <component :is="Component" />
        </transition>
      </router-view>
    </main>

    <footer class="app-footer">
      <div class="container footer-content">
        <div class="footer-left">
          <p class="copyright">© 2025-{{ new Date().getFullYear() }} WaveYo Koishi Mirror</p>
          <p class="description">高性能、安全可靠的 Koishi Plugins 镜像服务</p>
        </div>
        <div class="icp-links">
          <a v-if="status.icpEnabled && status.icpRecord" :href="status.icpUrl || 'https://beian.miit.gov.cn'" target="_blank">{{ status.icpRecord }}</a>
          <a v-if="status.icpEnabled && status.securityRecord" :href="status.securityUrl" target="_blank">{{ status.securityRecord }}</a>
        </div>
        <div class="footer-right">
          <a href="#" @click.prevent="openPolicy('terms')">使用条款</a>
          <a href="#" @click.prevent="openPolicy('privacy')">隐私政策</a>
        </div>
      </div>
    </footer>
    <!-- 政策条款弹窗 -->
    <div v-if="showPolicy" class="modal-overlay" @click.self="closePolicy">
      <div class="modal-container">
        <div class="modal-header">
          <h3>服务条款与隐私政策</h3>
          <button class="modal-close" @click="closePolicy">×</button>
        </div>
        <div class="modal-body markdown" v-html="policyHtml"></div>
      </div>
    </div>
  </div>
</template>

<script>
import { ref, computed, onMounted } from 'vue'
import axios from 'axios'
import TermsMd from './assets/Terms_of_Service_nd_Privacy_Policy.md?raw'

export default {
  name: 'AppRoot',
  setup() {
    const showPolicy = ref(false)
    const policySection = ref('terms')
    const status = ref({
      icpEnabled: false,
      icpRecord: '',
      icpUrl: '',
      securityRecord: '',
      securityUrl: ''
    })

    const renderMarkdown = (md) => {
      // 基础 Markdown 渲染（标题、粗体、斜体、链接、列表、代码块）
      let html = md
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
      // 代码块 ```
      html = html.replace(/```([\s\S]*?)```/g, (m, p1) => `<pre class="code"><code>${p1.replace(/\n/g, '\n')}</code></pre>`) 
      // 标题
      html = html.replace(/^###\s+(.+)$/gm, '<h3>$1</h3>')
                 .replace(/^##\s+(.+)$/gm, '<h2>$1</h2>')
                 .replace(/^#\s+(.+)$/gm, '<h1>$1</h1>')
      // 粗体/斜体
      html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
                 .replace(/\*([^*]+)\*/g, '<em>$1</em>')
      // 链接 [text](url)
      html = html.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
      // 列表（简单处理）
      html = html.replace(/^(?:-\s+.+\n?)+/gm, (block) => {
        const items = block.trim().split(/\n/).map(l => l.replace(/^-/,'').trim()).map(t => `<li>${t}</li>`).join('')
        return `<ul>${items}</ul>`
      })
      // 段落
      html = html.replace(/^(?!<h\d|<ul|<pre|<p|<blockquote)(.+)$/gm, '<p>$1</p>')
      return html
    }

    const policyHtml = computed(() => renderMarkdown(TermsMd))

    const openPolicy = (section) => {
      policySection.value = section
      showPolicy.value = true
      document.body.style.overflow = 'hidden'
    }
    const closePolicy = () => {
      showPolicy.value = false
      document.body.style.overflow = ''
    }

    const fetchStatus = async () => {
      try {
        const res = await axios.get('/status')
        status.value = res.data || status.value
      } catch (e) {}
    }

    onMounted(() => {
      fetchStatus()
    })

    return { showPolicy, policyHtml, openPolicy, closePolicy, status }
  }
}
</script>

<style scoped>
.app-container {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  background-color: var(--background-color);
}

.app-header {
  background-color: rgba(255, 255, 255, 0.9);
  backdrop-filter: blur(8px);
  border-bottom: 1px solid var(--border-color);
  position: sticky;
  top: 0;
  z-index: 100;
  height: 70px;
}

.header-content {
  height: 100%;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.logo-section {
  display: flex;
  align-items: center;
  gap: 12px;
}

.logo-img {
  height: 32px;
  width: auto;
}

.logo-text {
  font-size: 1.25rem;
  font-weight: 700;
  margin: 0;
}

.logo-text a {
  color: var(--text-primary);
}

.nav-menu {
  display: flex;
  gap: 2rem;
  align-items: center;
}

.nav-link {
  color: var(--text-secondary);
  font-weight: 500;
  font-size: 0.95rem;
  transition: color 0.2s;
}

.nav-link:hover,
.nav-link.router-link-active {
  color: var(--primary-color);
}

.github-link {
  display: flex;
  align-items: center;
  gap: 4px;
}

.app-main {
  flex: 1;
  width: 100%;
  padding-top: 2rem;
  padding-bottom: 4rem;
}

.app-footer {
  background-color: white;
  border-top: 1px solid var(--border-color);
  padding: 2rem 0;
  color: var(--text-secondary);
}

.footer-content {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 1rem;
}

.footer-left .copyright {
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 0.25rem;
}

.footer-left .description {
  font-size: 0.875rem;
}

.footer-right {
  display: flex;
  gap: 1.5rem;
}

.footer-right a {
  color: var(--text-secondary);
  font-size: 0.875rem;
}

.footer-right a:hover {
  color: var(--primary-color);
}

.icp-links { display: flex; gap: 1rem; min-height: 22px; }
.icp-links a { color: var(--text-secondary); font-size: 0.875rem; }
.icp-links a:hover { color: var(--primary-color); }

@media (max-width: 768px) {
  .footer-content { flex-direction: column; align-items: flex-start; gap: 0.75rem; }
  .footer-right { gap: 1rem; }
  .icp-links { flex-wrap: wrap; }
}

/* Modal */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}
.modal-container {
  width: min(900px, 92vw);
  height: 80vh;
  background: #fff;
  border-radius: 10px;
  box-shadow: 0 10px 30px rgba(0,0,0,0.2);
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1rem 1.25rem;
  border-bottom: 1px solid var(--border-color);
}
.modal-close {
  border: none;
  background: transparent;
  font-size: 1.5rem;
  line-height: 1;
  cursor: pointer;
  color: var(--text-secondary);
}
.modal-body {
  padding: 1rem 1.25rem;
  overflow: auto;
  flex: 1;
}
.markdown h1, .markdown h2, .markdown h3 { margin: 1rem 0 0.5rem; color: var(--text-primary); }
.markdown p { margin: 0.5rem 0; color: var(--text-secondary); }
.markdown ul { padding-left: 1.2rem; }
.markdown a { color: var(--primary-color); }
.markdown .code { background: #f6f8fa; padding: 0.75rem; border-radius: 6px; overflow: auto; }
.markdown { white-space: normal; word-break: break-word; }

/* Transitions */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
