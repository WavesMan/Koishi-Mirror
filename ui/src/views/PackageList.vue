<template>
  <div class="package-list-page">
    <div class="container">
      <div class="page-header">
        <h1 class="page-title">包列表</h1>
        <div class="search-filters">
          <div class="search-box">
            <input
              v-model.trim="searchKey"
              placeholder="搜索包名称..."
              @keyup.enter="fetchPackages"
              class="search-input"
            />
            <button @click="fetchPackages" class="btn btn-primary">搜索</button>
          </div>
          <div class="filter-group">
            <select v-model="statusFilter" class="filter-select">
              <option value="">全部状态</option>
              <option value="pending">待同步</option>
              <option value="syncing">同步中</option>
              <option value="success">同步成功</option>
              <option value="failed">同步失败</option>
            </select>
            
            <select v-model="sortBy" class="filter-select">
              <option value="name">按名称排序</option>
              <option value="version">按版本排序</option>
              <option value="syncTime">按同步时间排序</option>
            </select>
          </div>
        </div>
      </div>

      <div class="loading-state" v-if="loading">
        <div class="spinner"></div>
        <p>加载中...</p>
      </div>

      <div class="error-state" v-else-if="error">
        <p>{{ error }}</p>
        <button @click="fetchPackages" class="btn btn-secondary">重试</button>
      </div>

      <div class="empty-state" v-else-if="packages.length === 0">
        <p>没有找到匹配的包</p>
      </div>

      <div class="packages-grid" v-else>
        <div class="package-card" v-for="pkg in sortedPackages" :key="pkg.name + '@' + pkg.version">
          <div class="pkg-header">
            <router-link :to="`/package/${pkg.name}`" class="pkg-name">{{ pkg.name }}</router-link>
            <span class="status-badge" :class="`status-${pkg.syncStatus || 'pending'}`">
              {{ getStatusText(pkg.syncStatus || 'pending') }}
            </span>
          </div>
          
          <div class="pkg-body">
            <div class="pkg-row">
              <span class="label">版本:</span>
              <span class="value">v{{ pkg.version }}</span>
            </div>
            <div class="pkg-row" v-if="pkg.description">
              <span class="label">描述:</span>
              <span class="value description" :title="pkg.description">{{ pkg.description }}</span>
            </div>
            <div class="pkg-row" v-if="pkg.dist && pkg.dist.size">
              <span class="label">大小:</span>
              <span class="value">{{ formatSize(pkg.dist.size) }}</span>
            </div>
            <div class="pkg-row" v-if="pkg.syncTime">
              <span class="label">同步时间:</span>
              <span class="value">{{ formatTime(pkg.syncTime) }}</span>
            </div>
          </div>
          
          <div class="pkg-footer">
            <a
              :href="`/download/${pkg.name}/${pkg.version}`"
              class="btn btn-sm btn-outline"
              target="_blank"
              :class="{ disabled: pkg.syncStatus !== 'success' }"
            >
              下载 Tarball
            </a>
          </div>
        </div>
      </div>

      <div class="pagination" v-if="totalPages > 1">
        <button 
          class="btn btn-secondary"
          @click="prevPage" 
          :disabled="page <= 1"
        >
          上一页
        </button>
        <span class="page-info">
          {{ page }} / {{ totalPages }}
        </span>
        <button 
          class="btn btn-secondary"
          @click="nextPage" 
          :disabled="page >= totalPages"
        >
          下一页
        </button>
      </div>
    </div>
  </div>
</template>

<script>
import { ref, computed, onMounted } from 'vue'
import axios from 'axios'

export default {
  name: 'PackageList',
  setup() {
    const packages = ref([])
    const loading = ref(false)
    const error = ref('')
    const searchKey = ref('')
    const statusFilter = ref('')
    const sortBy = ref('name')
    const page = ref(1)
    const pageSize = ref(50)
    const total = ref(0)

    const totalPages = computed(() => {
      return Math.ceil(total.value / pageSize.value)
    })

    const compareVersions = (a, b) => {
      const pa = a.split('-')[0]
      const pb = b.split('-')[0]
      const as = pa.split('.')
      const bs = pb.split('.')
      for (let i = 0; i < 3; i++) {
        const ai = i < as.length ? parseInt(as[i], 10) || 0 : 0
        const bi = i < bs.length ? parseInt(bs[i], 10) || 0 : 0
        if (ai !== bi) return ai > bi ? -1 : 1
      }
      if (a === b) return 0
      if (a.includes('-') && !b.includes('-')) return 1
      if (!a.includes('-') && b.includes('-')) return -1
      return a > b ? -1 : 1
    }

    const sortedPackages = computed(() => {
      const list = packages.value.slice()
      const filtered = statusFilter.value ? list.filter(p => (p.syncStatus || 'pending') === statusFilter.value) : list
      if (sortBy.value === 'name') {
        return filtered.sort((a, b) => a.name.localeCompare(b.name))
      }
      if (sortBy.value === 'version') {
        return filtered.sort((a, b) => compareVersions(a.version, b.version))
      }
      if (sortBy.value === 'syncTime') {
        return filtered.sort((a, b) => {
          const ta = a.syncTime ? new Date(a.syncTime).getTime() : 0
          const tb = b.syncTime ? new Date(b.syncTime).getTime() : 0
          return tb - ta
        })
      }
      return filtered
    })

    // 获取包列表
    const fetchPackages = async () => {
      loading.value = true
      error.value = ''
      
      try {
        const params = {
          page: page.value,
          pageSize: pageSize.value,
          search: searchKey.value
        }
        
        const res = await axios.get('/packages', { params })
        packages.value = res.data.packages || []
        total.value = res.data.total || 0
      } catch (err) {
        error.value = '获取包列表失败'
        console.error('获取包列表失败:', err)
        packages.value = [] // Reset on error
        total.value = 0
      } finally {
        loading.value = false
      }
    }

    // 上一页
    const prevPage = () => {
      if (page.value > 1) {
        page.value--
        fetchPackages()
      }
    }

    // 下一页
    const nextPage = () => {
      if (page.value < totalPages.value) {
        page.value++
        fetchPackages()
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

    const getStatusText = (status) => {
      const map = {
        pending: '待同步',
        syncing: '同步中',
        success: '已同步',
        failed: '失败'
      }
      return map[status] || status
    }

    onMounted(() => {
      fetchPackages()
    })

    return {
      packages,
      loading,
      error,
      searchKey,
      statusFilter,
      sortBy,
      page,
      pageSize,
      total,
      totalPages,
      sortedPackages,
      fetchPackages,
      prevPage,
      nextPage,
      formatTime,
      formatSize,
      getStatusText
    }
  }
}
</script>

<style scoped>
.page-header {
  margin-bottom: 2rem;
}

.page-title {
  font-size: 1.75rem;
  font-weight: 700;
  color: var(--text-primary);
  margin-bottom: 1.5rem;
}

.search-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
  justify-content: space-between;
  align-items: center;
}

.search-box {
  display: flex;
  gap: 0.5rem;
  flex: 1;
  min-width: 300px;
}

.search-input {
  flex: 1;
  padding: 0.5rem 1rem;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  font-size: 0.95rem;
  outline: none;
  transition: border-color 0.2s;
}

.search-input:focus {
  border-color: var(--primary-color);
}

.filter-group {
  display: flex;
  gap: 0.75rem;
}

.filter-select {
  padding: 0.5rem 2rem 0.5rem 1rem;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background-color: white;
  font-size: 0.9rem;
  cursor: pointer;
}

/* Grid Layout */
.packages-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 1.5rem;
  margin-bottom: 2rem;
}

.package-card {
  background: white;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1.25rem;
  display: flex;
  flex-direction: column;
  transition: box-shadow 0.2s, transform 0.2s;
}

.package-card:hover {
  box-shadow: var(--shadow-md);
  transform: translateY(-2px);
  border-color: var(--primary-color);
}

.pkg-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 1rem;
}

.pkg-name {
  font-weight: 600;
  font-size: 1.1rem;
  word-break: break-all;
  margin-right: 0.5rem;
}

.status-badge {
  font-size: 0.75rem;
  padding: 0.25rem 0.5rem;
  border-radius: 999px;
  font-weight: 500;
  white-space: nowrap;
}

.status-pending { background-color: #f3f4f6; color: #6b7280; }
.status-syncing { background-color: #eff6ff; color: #3b82f6; }
.status-success { background-color: #f0fdf4; color: #10b981; }
.status-failed { background-color: #fef2f2; color: #ef4444; }

.pkg-body {
  flex: 1;
  font-size: 0.9rem;
  color: var(--text-secondary);
  margin-bottom: 1rem;
}

.pkg-row {
  display: flex;
  margin-bottom: 0.25rem;
}

.pkg-row .label {
  width: 70px;
  flex-shrink: 0;
}

.pkg-row .value {
  color: var(--text-primary);
}

.pkg-row .description {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.pkg-footer {
  margin-top: auto;
  padding-top: 1rem;
  border-top: 1px solid var(--border-color);
}

.btn-outline {
  border: 1px solid var(--border-color);
  background: transparent;
  color: var(--text-primary);
  width: 100%;
}

.btn-outline:hover:not(.disabled) {
  border-color: var(--primary-color);
  color: var(--primary-color);
}

.btn-outline.disabled {
  opacity: 0.5;
  cursor: not-allowed;
  background-color: #f3f4f6;
}

.pagination {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 1rem;
  margin-top: 2rem;
}

.page-info {
  color: var(--text-secondary);
  font-size: 0.9rem;
}

/* Loading & Empty States */
.loading-state, .empty-state, .error-state {
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

@media (max-width: 640px) {
  .search-filters { flex-direction: column; align-items: stretch; }
  .search-box { min-width: auto; }
  .filter-group { justify-content: space-between; }
  .filter-select { flex: 1; }
}
</style>
