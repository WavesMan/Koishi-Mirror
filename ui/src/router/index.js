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
    meta: { title: '首页 - NPM镜像源' }
  },
  {
    path: '/packages',
    name: 'PackageList',
    component: PackageList,
    meta: { title: '包列表 - NPM镜像源' }
  },
  {
    path: '/package/:name(.*)',
    name: 'PackageDetail',
    component: PackageDetail,
    props: route => ({ name: route.params.name }),
    meta: { title: '包详情 - NPM镜像源' }
  },
  {
    path: '/stats',
    name: 'StorageStats',
    component: StorageStats,
    meta: { title: '存储统计 - NPM镜像源' }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 设置页面标题
router.beforeEach((to, from, next) => {
  document.title = to.meta.title || 'NPM镜像源'
  next()
})

export default router
