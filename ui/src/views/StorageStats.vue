<template>
  <div class="storage-stats-page">
    <div class="container">
      <div class="page-header">
        <h1 class="page-title">存储统计</h1>
      </div>
      
      <div class="loading-state" v-if="loading">
        <div class="spinner"></div>
        <p>加载中...</p>
      </div>

      <div class="error-state" v-else-if="error">
        <p>{{ error }}</p>
        <button @click="fetchStorageStats" class="btn btn-secondary">重试</button>
      </div>

      <div class="stats-content" v-else>
        <div class="stats-overview">
          <div class="stat-card">
            <div class="stat-icon blue">💾</div>
            <div class="stat-info">
              <div class="stat-value">{{ formatSize(stats.totalSize) }}</div>
              <div class="stat-title">总存储大小</div>
            </div>
          </div>
          
          <div class="stat-card">
            <div class="stat-icon green">📦</div>
            <div class="stat-info">
              <div class="stat-value">{{ stats.packageCount }}</div>
              <div class="stat-title">包数量</div>
            </div>
          </div>
          
          <div class="stat-card">
            <div class="stat-icon purple">🏷️</div>
            <div class="stat-info">
              <div class="stat-value">{{ stats.versionCount }}</div>
              <div class="stat-title">版本数量</div>
            </div>
          </div>
          
          <div class="stat-card">
            <div class="stat-icon orange">📊</div>
            <div class="stat-info">
              <div class="stat-value">{{ formatSize(avgPackageSize) }}</div>
              <div class="stat-title">平均包大小</div>
            </div>
          </div>
        </div>

        <div class="charts-grid">
          <div class="chart-card">
            <h2 class="card-title">存储占用 Top 10</h2>
            <div id="sizeDistributionChart" class="chart"></div>
          </div>
          
          <div class="chart-card">
            <h2 class="card-title">版本数量 Top 10</h2>
            <div id="versionCountChart" class="chart"></div>
          </div>
        </div>

        <div class="table-card">
          <h2 class="card-title">详细统计 (Top 10)</h2>
          
          <div class="table-responsive">
            <table class="data-table">
              <thead>
                <tr>
                  <th>包名</th>
                  <th>版本数量</th>
                  <th>总大小</th>
                  <th>最大版本</th>
                  <th>最小版本</th>
                  <th>平均大小</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="pkg in topPackages" :key="pkg.name">
                  <td>
                    <router-link :to="`/package/${pkg.name}`" class="pkg-link">{{ pkg.name }}</router-link>
                  </td>
                  <td>{{ pkg.versionCount }}</td>
                  <td>{{ formatSize(pkg.totalSize) }}</td>
                  <td>{{ formatSize(pkg.maxSize) }}</td>
                  <td>{{ formatSize(pkg.minSize) }}</td>
                  <td>{{ formatSize(pkg.avgSize) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import axios from 'axios'
import { use as echartsUse, init as echartsInit } from 'echarts/core'
import { BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
echartsUse([BarChart, GridComponent, TooltipComponent, CanvasRenderer])

export default {
  name: 'StorageStats',
  setup() {
    const stats = ref({
      totalSize: 0,
      packageCount: 0,
      versionCount: 0,
      packageStats: {}
    })
    const loading = ref(false)
    const error = ref('')
    let sizeDistributionChart = null
    let versionCountChart = null

    const avgPackageSize = computed(() => {
      if (stats.value.packageCount === 0) return 0
      return stats.value.totalSize / stats.value.packageCount
    })

    const topPackages = computed(() => {
      const packages = []
      for (const name in stats.value.packageStats) {
        if (!name) continue
        packages.push({
          name,
          ...stats.value.packageStats[name]
        })
      }
      
      // 按总大小排序
      packages.sort((a, b) => b.totalSize - a.totalSize)
      
      // 返回前10个
      return packages.slice(0, 10)
    })

    // 获取存储统计
    const fetchStorageStats = async () => {
      loading.value = true
      error.value = ''
      
      try {
        const res = await axios.get('/stats')
        stats.value = res.data
        loading.value = false
        await nextTick()
        renderCharts()
      } catch (err) {
        error.value = '获取存储统计失败'
        console.error('获取存储统计失败:', err)
        loading.value = false
      }
    }

    // 渲染图表
    const renderCharts = () => {
      renderSizeDistributionChart()
      renderVersionCountChart()
    }

    // 渲染包大小分布图表
    const renderSizeDistributionChart = async () => {
      const chartDom = document.getElementById('sizeDistributionChart')
      if (!chartDom) return
      
      if (sizeDistributionChart) {
        sizeDistributionChart.dispose()
      }
      sizeDistributionChart = echartsInit(chartDom)

      // 准备数据
      const packages = []
      const sizes = []
      
      for (const name in stats.value.packageStats) {
        if (!name) continue
        const pkg = stats.value.packageStats[name]
        packages.push(name)
        sizes.push(pkg.totalSize)
      }
      
      // 按大小排序
      const sortedData = packages.map((name, index) => ({
        name,
        value: sizes[index]
      })).sort((a, b) => b.value - a.value).slice(0, 10)
      
      const option = {
        tooltip: {
          trigger: 'axis',
          axisPointer: { type: 'shadow' },
          formatter: (params) => {
            const data = params[0]
            return `${data.name}<br/>大小: ${formatSize(data.value)}`
          }
        },
        grid: {
          left: '3%',
          right: '4%',
          bottom: '3%',
          containLabel: true
        },
        xAxis: {
          type: 'value',
          axisLabel: { formatter: (value) => formatSize(value) }
        },
        yAxis: {
          type: 'category',
          data: sortedData.map(item => item.name).reverse(),
          axisLabel: {
            interval: 0,
            width: 100,
            overflow: 'truncate'
          }
        },
        series: [
          {
            name: '存储大小',
            type: 'bar',
            data: sortedData.map(item => item.value).reverse(),
            itemStyle: { color: '#3b82f6' }
          }
        ]
      }
      
      sizeDistributionChart.setOption(option)
    }

    // 渲染版本数量分布图表
    const renderVersionCountChart = async () => {
      const chartDom = document.getElementById('versionCountChart')
      if (!chartDom) return
      
      if (versionCountChart) {
        versionCountChart.dispose()
      }
      versionCountChart = echartsInit(chartDom)

      // 准备数据
      const packages = []
      const counts = []
      
      for (const name in stats.value.packageStats) {
        if (!name) continue
        const pkg = stats.value.packageStats[name]
        packages.push(name)
        counts.push(pkg.versionCount)
      }
      
      // 按版本数量排序
      const sortedData = packages.map((name, index) => ({
        name,
        value: counts[index]
      })).sort((a, b) => b.value - a.value).slice(0, 10)
      
      const option = {
        tooltip: {
          trigger: 'axis',
          axisPointer: { type: 'shadow' }
        },
        grid: {
          left: '3%',
          right: '4%',
          bottom: '3%',
          containLabel: true
        },
        xAxis: { type: 'value' },
        yAxis: {
          type: 'category',
          data: sortedData.map(item => item.name).reverse(),
          axisLabel: {
            interval: 0,
            width: 100,
            overflow: 'truncate'
          }
        },
        series: [
          {
            name: '版本数量',
            type: 'bar',
            data: sortedData.map(item => item.value).reverse(),
            itemStyle: { color: '#10b981' }
          }
        ]
      }
      
      versionCountChart.setOption(option)
    }

    // 格式化文件大小
    const formatSize = (bytes) => {
      if (bytes === 0) return '0 B'
      const k = 1024
      const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
      const i = Math.floor(Math.log(bytes) / Math.log(k))
      return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
    }

    const handleResize = () => {
      sizeDistributionChart?.resize()
      versionCountChart?.resize()
    }

    onMounted(() => {
      fetchStorageStats()
      window.addEventListener('resize', handleResize)
    })

    onUnmounted(() => {
      window.removeEventListener('resize', handleResize)
      sizeDistributionChart?.dispose()
      versionCountChart?.dispose()
    })

    return {
      stats,
      loading,
      error,
      avgPackageSize,
      topPackages,
      fetchStorageStats,
      formatSize
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
}

/* Overview Cards */
.stats-overview {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 1.5rem;
  margin-bottom: 2rem;
}

.stat-card {
  background: white;
  padding: 1.5rem;
  border-radius: var(--radius-lg);
  border: 1px solid var(--border-color);
  box-shadow: var(--shadow-sm);
  display: flex;
  align-items: center;
  gap: 1rem;
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
.stat-icon.purple { background-color: #f3e8ff; color: #a855f7; }
.stat-icon.orange { background-color: #fff7ed; color: #f97316; }

.stat-info { flex: 1; }

.stat-value {
  font-size: 1.5rem;
  font-weight: 700;
  line-height: 1.2;
  color: var(--text-primary);
}

.stat-title {
  color: var(--text-secondary);
  font-size: 0.875rem;
}

/* Charts Grid */
.charts-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(400px, 1fr));
  gap: 1.5rem;
  margin-bottom: 2rem;
}

.chart-card {
  background: white;
  padding: 1.5rem;
  border-radius: var(--radius-lg);
  border: 1px solid var(--border-color);
  box-shadow: var(--shadow-sm);
}

.card-title {
  font-size: 1.1rem;
  font-weight: 600;
  margin-bottom: 1rem;
  color: var(--text-primary);
}

.chart {
  height: 300px;
  width: 100%;
}

/* Table Card */
.table-card {
  background: white;
  padding: 1.5rem;
  border-radius: var(--radius-lg);
  border: 1px solid var(--border-color);
  box-shadow: var(--shadow-sm);
  margin-bottom: 2rem;
}

.table-responsive {
  overflow-x: auto;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.95rem;
}

.data-table th,
.data-table td {
  padding: 0.75rem 1rem;
  text-align: left;
  border-bottom: 1px solid var(--border-color);
}

.data-table th {
  font-weight: 600;
  color: var(--text-secondary);
  background-color: #f8fafc;
}

.data-table tr:last-child td {
  border-bottom: none;
}

.pkg-link {
  font-weight: 500;
  color: var(--primary-color);
}

.pkg-link:hover {
  text-decoration: underline;
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

@media (max-width: 768px) {
  .charts-grid { grid-template-columns: 1fr; }
  .data-table th, .data-table td { padding: 0.5rem; font-size: 0.85rem; }
}
</style>
