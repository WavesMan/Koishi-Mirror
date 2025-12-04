<template>
  <div class="home">
    <!-- Hero Section -->
    <section class="hero-section">
      <div class="container">
        <h1 class="hero-title">加速你的Koishi Bot开发流程</h1>
        <p class="hero-subtitle">
          Koishi Plugins 的镜像仓库，提供高效、稳定的包同步和下载服务。
          <br />
          让 Koishi Plugins 都飞起来。
        </p>
        <div class="hero-actions">
          <router-link to="/packages" class="btn btn-primary btn-lg">
            <span class="icon">📦</span> 浏览包列表
          </router-link>
          <router-link to="/stats" class="btn btn-secondary btn-lg">
            <span class="icon">📊</span> 查看存储统计
          </router-link>
        </div>
      </div>
    </section>

    <!-- Stats Grid -->
    <section class="stats-section container">
      <div class="stats-grid">
        <div class="stat-card">
          <div class="stat-icon blue">📦</div>
          <div class="stat-info">
            <div class="stat-value">{{ status.totalPackages || 0 }}</div>
            <div class="stat-label">包总数</div>
          </div>
        </div>
        <div class="stat-card">
          <div class="stat-icon green">✅</div>
          <div class="stat-info">
            <div class="stat-value success">{{ status.syncedPackages || 0 }}</div>
            <div class="stat-label">已同步</div>
          </div>
        </div>
        <div class="stat-card">
          <div class="stat-icon red">❌</div>
          <div class="stat-info">
            <div class="stat-value danger">{{ status.failedPackages || 0 }}</div>
            <div class="stat-label">同步失败</div>
          </div>
        </div>
        <div class="stat-card">
          <div class="stat-icon purple">💾</div>
          <div class="stat-info">
            <div class="stat-value">{{ formatSize(status.storageSize || 0) }}</div>
            <div class="stat-label">存储占用</div>
          </div>
        </div>
      </div>
      
      <div class="meta-info text-center mt-4">
        <span class="meta-item">
          <span class="meta-label">最后同步:</span>
          <span class="meta-value">{{ formatTime(status.lastSyncTime) }}</span>
        </span>
        <span class="meta-item">
          <span class="meta-label">数据源:</span>
          <a :href="status.dataSourceURL" target="_blank" class="meta-link">{{ status.dataSourceURL || 'Unknown' }}</a>
        </span>
      </div>
    </section>

    <!-- Usage Guide -->
    <section class="guide-section container">
      <h2 class="section-title text-center">快速上手</h2>
      <div class="guide-grid">
        <div class="guide-card">
          <h3 class="guide-title">单次使用</h3>
          <p class="guide-desc">在安装命令中临时指定 Registry 地址。</p>
          <div class="code-block">
            <code>{{ installCmd }}</code>
            <button class="copy-btn" @click="copyToClipboard(installCmd, $event)">复制</button>
          </div>
        </div>
        <div class="guide-card">
          <h3 class="guide-title">设为默认</h3>
          <p class="guide-desc">将本镜像源设置为 npm 默认源，永久生效。</p>
          <div class="code-block">
            <code>{{ setCmd }}</code>
            <button class="copy-btn" @click="copyToClipboard(setCmd, $event)">复制</button>
          </div>
        </div>
        <div class="guide-card">
          <h3 class="guide-title">恢复官方</h3>
          <p class="guide-desc">恢复为 npm 官方源地址。</p>
          <div class="code-block">
            <code>npm config set registry https://registry.npmjs.org/</code>
            <button class="copy-btn" @click="copyToClipboard('npm config set registry https://registry.npmjs.org/', $event)">复制</button>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<script>
import { ref, onMounted } from 'vue'
import axios from 'axios'

export default {
  name: 'Home',
  setup() {
    const status = ref({})
    const installCmd = ref('')
    const setCmd = ref('')

    // 获取镜像状态
    const fetchStatus = async () => {
      try {
        const res = await axios.get('/status')
        status.value = res.data
      } catch (err) {
        status.value = {
          lastSyncTime: '',
          totalPackages: 0,
          syncedPackages: 0,
          failedPackages: 0,
          storageSize: 0,
          s3Bucket: '',
          dataSourceURL: ''
        }
      }
    }

    const normalizeRegistry = (u) => {
      if (!u) return ''
      return u.replace(/\/$/, '')
    }

    const updateGuide = () => {
      const base = window.location.origin
      const reg = normalizeRegistry(base)
      installCmd.value = `npm install <package-name> --registry=${reg}`
      setCmd.value = `npm config set registry ${reg}`
    }

    // 格式化时间
    const formatTime = (timeStr) => {
      if (!timeStr) return '从未'
      const date = new Date(timeStr)
      return date.toLocaleString('zh-CN')
    }

    // 格式化文件大小
    const formatSize = (bytes) => {
      if (bytes === 0) return '0 B'
      const k = 1024
      const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
      const i = Math.floor(Math.log(bytes) / Math.log(k))
      return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
    }

    const copyToClipboard = (text, event) => {
      navigator.clipboard.writeText(text).then(() => {
        if (event && event.target) {
          const originalText = event.target.innerText
          event.target.innerText = '已复制'
          event.target.disabled = true
          setTimeout(() => {
            event.target.innerText = originalText
            event.target.disabled = false
          }, 2000)
        }
      })
    }

    onMounted(() => {
      fetchStatus()
      updateGuide()
    })

    return {
      status,
      installCmd,
      setCmd,
      fetchStatus,
      formatTime,
      formatSize,
      copyToClipboard
    }
  }
}
</script>

<style scoped>
.hero-section {
  text-align: center;
  padding: 4rem 0;
  background: radial-gradient(circle at center, #eff6ff 0%, var(--background-color) 70%);
}

.hero-title {
  font-size: 3rem;
  font-weight: 800;
  color: var(--text-primary);
  margin-bottom: 1.5rem;
  letter-spacing: -0.02em;
}

.hero-subtitle {
  font-size: 1.25rem;
  color: var(--text-secondary);
  max-width: 600px;
  margin: 0 auto 2.5rem;
  line-height: 1.7;
}

.hero-actions {
  display: flex;
  gap: 1rem;
  justify-content: center;
}

.btn-lg {
  padding: 0.75rem 1.5rem;
  font-size: 1.1rem;
}

.btn-secondary {
  background-color: white;
  border: 1px solid var(--border-color);
  color: var(--text-primary);
}

.btn-secondary:hover {
  border-color: var(--primary-color);
  color: var(--primary-color);
}

.icon {
  margin-right: 0.5rem;
}

/* Stats Section */
.stats-section {
  margin-top: -2rem;
  position: relative;
  z-index: 10;
  margin-bottom: 4rem;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 1.5rem;
}

.stat-card {
  background: white;
  padding: 1.5rem;
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  display: flex;
  align-items: center;
  gap: 1rem;
  border: 1px solid var(--border-color);
  transition: transform 0.2s, box-shadow 0.2s;
}

.stat-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--shadow-lg);
}

.stat-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.5rem;
}

.stat-icon.blue { background-color: #eff6ff; color: #3b82f6; }
.stat-icon.green { background-color: #f0fdf4; color: #10b981; }
.stat-icon.red { background-color: #fef2f2; color: #ef4444; }
.stat-icon.purple { background-color: #f3e8ff; color: #a855f7; }

.stat-info {
  flex: 1;
}

.stat-value {
  font-size: 1.5rem;
  font-weight: 700;
  line-height: 1.2;
}

.stat-value.success { color: var(--success-color); }
.stat-value.danger { color: var(--danger-color); }

.stat-label {
  color: var(--text-secondary);
  font-size: 0.875rem;
}

.meta-info {
  color: var(--text-secondary);
  font-size: 0.9rem;
  display: flex;
  justify-content: center;
  gap: 2rem;
}

.meta-item {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.meta-label {
  font-weight: 500;
}

.meta-link {
  color: var(--primary-color);
  text-decoration: underline;
  text-underline-offset: 2px;
}

/* Guide Section */
.section-title {
  font-size: 2rem;
  font-weight: 700;
  margin-bottom: 2rem;
  color: var(--text-primary);
}

.guide-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 2rem;
}

.guide-card {
  background: white;
  padding: 2rem;
  border-radius: var(--radius-lg);
  border: 1px solid var(--border-color);
}

.guide-title {
  font-size: 1.25rem;
  font-weight: 600;
  margin-bottom: 0.75rem;
  color: var(--text-primary);
}

.guide-desc {
  color: var(--text-secondary);
  margin-bottom: 1.5rem;
  font-size: 0.95rem;
}

.code-block {
  background: #1e293b;
  padding: 1rem;
  padding-right: 3.5rem;
  border-radius: var(--radius-md);
  color: #e2e8f0;
  font-family: 'Fira Code', monospace;
  font-size: 0.9rem;
  position: relative;
  transition: background 0.2s;
  white-space: pre-wrap;
  word-break: break-all;
}

.code-block:hover {
  background: #0f172a;
}

.code-block code {
  display: block;
}

.copy-btn {
  position: absolute;
  top: 0.5rem;
  right: 0.5rem;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: white;
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
  cursor: pointer;
  font-size: 0.75rem;
  transition: background 0.2s;
}

.copy-btn:hover {
  background: rgba(255, 255, 255, 0.2);
}

.copy-btn:disabled {
  cursor: default;
  opacity: 0.7;
}

@media (max-width: 768px) {
  .hero-title { font-size: 2rem; }
  .hero-actions { flex-direction: column; }
  .stats-grid { grid-template-columns: 1fr; }
  .meta-info { flex-direction: column; gap: 0.5rem; }
}
</style>
