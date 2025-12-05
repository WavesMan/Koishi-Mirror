<template>
  <div class="package-detail-page">
    <div class="container">
      <div class="back-nav">
        <router-link to="/packages" class="back-link">
          <span class="icon">&larr;</span> 返回包列表
        </router-link>
      </div>

      <div class="loading-state" v-if="loading">
        <div class="spinner"></div>
        <p>加载中...</p>
      </div>

      <div class="error-state" v-else-if="error">
        <p>{{ error }}</p>
        <button @click="fetchPackageDetail" class="btn btn-secondary">重试</button>
      </div>

      <div class="detail-content" v-else>
        <div class="pkg-header-card card">
          <div class="pkg-title-row">
            <h1 class="pkg-title">{{ packageDetail.name }}</h1>
            <div class="pkg-badges" v-if="packageDetail.versions && packageDetail.versions.length > 0">
              <span class="badge badge-blue">latest: v{{ latestVersion }}</span>
            </div>
          </div>
          
          <p class="pkg-desc" v-if="packageDetail.description">{{ packageDetail.description }}</p>
          
          <div class="pkg-meta-row">
            <div class="meta-item" v-if="packageDetail.author">
              <span class="label">作者:</span>
              <span class="value">{{ packageDetail.author }}</span>
            </div>
            <div class="meta-item" v-if="packageDetail.license">
              <span class="label">许可证:</span>
              <span class="value">{{ packageDetail.license }}</span>
            </div>
            <div class="meta-item">
              <span class="label">版本数:</span>
              <span class="value">{{ packageDetail.versions?.length || 0 }}</span>
            </div>
          </div>
        </div>

        <div class="layout-grid">
          <div class="main-col">
            <div class="versions-card card">
              <h2 class="card-title">版本历史</h2>
              <div class="versions-list">
                <div class="version-item" v-for="version in packageDetail.versions" :key="version.version">
                  <div class="version-main">
                    <div class="version-info">
                      <span class="version-num">v{{ version.version }}</span>
                      <span class="status-badge" :class="`status-${version.syncStatus}`">
                        {{ getStatusText(version.syncStatus) }}
                      </span>
                    </div>
                    <div class="version-meta">
                      <span v-if="version.dist.size">{{ formatSize(version.dist.size) }}</span>
                      <span class="separator" v-if="version.dist.size && version.syncTime">•</span>
                      <span v-if="version.syncTime">{{ formatTime(version.syncTime) }}</span>
                    </div>
                  </div>
                  
                  <div class="version-actions">
                    <a
                      :href="version.dist.tarball"
                      class="btn btn-sm btn-outline"
                      target="_blank"
                      :class="{ disabled: version.syncStatus !== 'success' }"
                    >
                      下载
                    </a>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div class="side-col">
            <div class="usage-card card">
              <h2 class="card-title">安装指南</h2>
              
              <div class="usage-item">
                <div class="usage-header">
                  <span class="usage-label">最新版本</span>
                  <span class="usage-ver">v{{ latestVersion }}</span>
                </div>
                <div class="code-block">
                  <code>{{ installLatestCmd }}</code>
                  <button class="copy-btn" @click="copyToClipboard(installLatestCmd, $event)">复制</button>
                </div>
              </div>
              
              <div class="usage-item">
                <div class="usage-header">
                  <span class="usage-label">指定版本</span>
                </div>
                <div class="code-block">
                  <code>{{ installSpecificCmd }}</code>
                  <button class="copy-btn" @click="copyToClipboard(installSpecificCmd, $event)">复制</button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import axios from 'axios'

export default {
  name: 'PackageDetail',
  setup() {
    const route = useRoute()
    const packageDetail = ref({
      name: '',
      description: '',
      author: '',
      versions: []
    })
    const loading = ref(false)
    const error = ref('')

    const latestVersion = computed(() => {
      if (packageDetail.value.versions && packageDetail.value.versions.length > 0) {
        // Sort versions if needed, assuming backend returns sorted or we take first
        // Usually backend should return sorted or we sort here. 
        // For now assume first is latest or we find max semver.
        return packageDetail.value.versions[0].version
      }
      return '0.0.0'
    })

    const normalizeRegistry = (u) => {
      if (!u) return ''
      return u.replace(/\/$/, '')
    }

    const installLatestCmd = computed(() => {
      const base = window.location.origin
      const reg = normalizeRegistry(base)
      return `npm install ${packageDetail.value.name} --registry=${reg}`
    })

    const installSpecificCmd = computed(() => {
      const base = window.location.origin
      const reg = normalizeRegistry(base)
      return `npm install ${packageDetail.value.name}@${latestVersion.value} --registry=${reg}`
    })

    // 获取包详情
    const fetchPackageDetail = async () => {
      const name = route.params.name
      if (!name) {
        error.value = '包名不能为空'
        return
      }

      loading.value = true
      error.value = ''
      
      try {
        const encoded = encodeURIComponent(name)
        const res = await axios.get(`/package/${encoded}`)
        packageDetail.value = res.data
        // Ensure versions are sorted (descending)
        if (packageDetail.value.versions) {
           packageDetail.value.versions.sort((a, b) => {
             // Simple sort, ideally use semver
             return b.version.localeCompare(a.version, undefined, { numeric: true, sensitivity: 'base' })
           })
        }
      } catch (err) {
        error.value = '获取包详情失败'
        console.error('获取包详情失败:', err)
      } finally {
        loading.value = false
      }
    }

    // 格式化时间
    const formatTime = (timeStr) => {
      if (!timeStr) return '-'
      const date = new Date(timeStr)
      return date.toLocaleString('zh-CN')
    }

    // 格式化文件大小
    const formatSize = (bytes) => {
      if (!bytes && bytes !== 0) return '-'
      if (bytes === 0) return '0 B'
      const k = 1024
      const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
      const i = Math.floor(Math.log(bytes) / Math.log(k))
      return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
    }

    // 获取状态文本
    const getStatusText = (status) => {
      const statusMap = {
        'pending': '待同步',
        'syncing': '同步中',
        'success': '已同步',
        'failed': '同步失败'
      }
      return statusMap[status] || status
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
      fetchPackageDetail()
    })

    watch(() => route.params.name, (newVal) => {
      if (newVal) fetchPackageDetail()
    })

    return {
      packageDetail,
      loading,
      error,
      latestVersion,
      installLatestCmd,
      installSpecificCmd,
      fetchPackageDetail,
      formatTime,
      formatSize,
      getStatusText,
      copyToClipboard
    }
  }
}
</script>

<style scoped>
.package-detail-page {
  padding-bottom: 4rem;
}

.back-nav {
  margin-bottom: 1.5rem;
}

.back-link {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--text-secondary);
  font-weight: 500;
  transition: color 0.2s;
}

.back-link:hover {
  color: var(--primary-color);
}

/* Header Card */
.pkg-header-card {
  margin-bottom: 2rem;
  border-left: 4px solid var(--primary-color);
}

.pkg-title-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 1rem;
  margin-bottom: 1rem;
}

.pkg-title {
  font-size: 2rem;
  font-weight: 700;
  margin: 0;
  color: var(--text-primary);
}

.badge {
  padding: 0.25rem 0.75rem;
  border-radius: 999px;
  font-size: 0.85rem;
  font-weight: 600;
}

.badge-blue {
  background-color: #eff6ff;
  color: #3b82f6;
}

.pkg-desc {
  font-size: 1.1rem;
  color: var(--text-secondary);
  margin-bottom: 1.5rem;
  line-height: 1.6;
}

.pkg-meta-row {
  display: flex;
  flex-wrap: wrap;
  gap: 2rem;
  padding-top: 1rem;
  border-top: 1px solid var(--border-color);
}

.meta-item {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.95rem;
}

.meta-item .label {
  color: var(--text-secondary);
}

.meta-item .value {
  font-weight: 500;
  color: var(--text-primary);
}

/* Layout Grid */
.layout-grid {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 2rem;
}

.card-title {
  font-size: 1.25rem;
  font-weight: 600;
  margin-bottom: 1.5rem;
  color: var(--text-primary);
}

/* Versions List */
.versions-list {
  display: flex;
  flex-direction: column;
}

.version-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1rem 0;
  border-bottom: 1px solid var(--border-color);
}

.version-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.version-item:first-child {
  padding-top: 0;
}

.version-main {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.version-info {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.version-num {
  font-weight: 600;
  font-size: 1.05rem;
  color: var(--text-primary);
}

.version-meta {
  font-size: 0.85rem;
  color: var(--text-secondary);
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.status-badge {
  font-size: 0.75rem;
  padding: 0.125rem 0.5rem;
  border-radius: 4px;
  font-weight: 500;
}

.status-pending { background-color: #f3f4f6; color: #6b7280; }
.status-syncing { background-color: #eff6ff; color: #3b82f6; }
.status-success { background-color: #f0fdf4; color: #10b981; }
.status-failed { background-color: #fef2f2; color: #ef4444; }

/* Usage Card */
.usage-item {
  margin-bottom: 1.5rem;
}

.usage-item:last-child {
  margin-bottom: 0;
}

.usage-header {
  display: flex;
  justify-content: space-between;
  margin-bottom: 0.5rem;
  font-size: 0.9rem;
}

.usage-label {
  font-weight: 500;
  color: var(--text-secondary);
}

.usage-ver {
  font-weight: 600;
  color: var(--primary-color);
}

.code-block {
  background: #1e293b;
  padding: 0.75rem 1rem;
  padding-right: 3.5rem;
  border-radius: var(--radius-md);
  color: #e2e8f0;
  font-family: 'Fira Code', monospace;
  font-size: 0.85rem;
  position: relative;
  white-space: pre-wrap;
  word-break: break-all;
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

/* Responsive */
@media (max-width: 768px) {
  .layout-grid { grid-template-columns: 1fr; }
  .pkg-title { font-size: 1.5rem; }
  .pkg-meta-row { flex-direction: column; gap: 0.5rem; }
}

/* Loading & Error */
.loading-state, .error-state {
  text-align: center;
  padding: 4rem 0;
  color: var(--text-secondary);
}

.spinner {
  border: 3px solid #f3f3f3;
  border-top: 3px solid var(--primary-color);
  border-radius: 50%;
  width: 24px;
  height: 24px;
  animation: spin 1s linear infinite;
  margin: 0 auto 1rem;
}

@keyframes spin {
  0% { transform: rotate(0deg); }
  100% { transform: rotate(360deg); }
}
</style>
