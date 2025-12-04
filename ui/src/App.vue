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
          <a href="https://github.com/waveyo/npm-mirror" target="_blank" class="nav-link github-link">
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
        <div class="footer-right">
          <a href="#">使用条款</a>
          <a href="#">隐私政策</a>
        </div>
      </div>
    </footer>
  </div>
</template>

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
