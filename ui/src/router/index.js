import { createRouter, createWebHistory } from 'vue-router'
const Home = () => import('../views/Home.vue')
const PackageList = () => import('../views/PackageList.vue')
const PackageDetail = () => import('../views/PackageDetail.vue')
const StorageStats = () => import('../views/StorageStats.vue')

const routes = [
  {
    path: '/',
    name: 'Home',
    component: Home,
    meta: { title: '首页 - Mirror' }
  },
  {
    path: '/packages',
    name: 'PackageList',
    component: PackageList,
    meta: { title: '包列表 - Mirror' }
  },
  {
    path: '/package/:name(.*)',
    name: 'PackageDetail',
    component: PackageDetail,
    props: route => ({ name: route.params.name }),
    meta: { title: '包详情 - Mirror' }
  },
  {
    path: '/stats',
    name: 'StorageStats',
    component: StorageStats,
    meta: { title: '存储统计 - Mirror' }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 设置页面标题
router.beforeEach((to, from, next) => {
  document.title = to.meta.title || 'Koishi Mirror'
  next()
})

export default router
