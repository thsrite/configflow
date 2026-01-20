<template>
  <div id="app">
    <!-- 登录页面 -->
    <router-view v-if="isLoginPage" />

    <!-- 主应用 -->
    <el-container v-else class="app-container">
      <el-header class="modern-header">
        <div class="header-content">
          <div class="logo-section">
            <el-button class="mobile-menu-btn" @click="drawerVisible = true" text>
              <el-icon :size="24"><Menu /></el-icon>
            </el-button>
            <img src="/icon.png" alt="Logo" class="logo-icon" />
            <h2 class="logo-text">代理配置生成器</h2>
          </div>
          <div class="header-actions">
            <div class="version-github-group">
              <el-tag class="version-tag" effect="plain">{{ versionInfo }}</el-tag>
              <el-button
                text
                class="github-link"
                @click="openGithub"
                title="查看文档"
              >
                <el-icon :size="18">
                  <svg viewBox="0 0 1024 1024" xmlns="http://www.w3.org/2000/svg">
                    <path d="M512 12.672c-282.88 0-512 229.248-512 512 0 226.261333 146.688 418.133333 350.08 485.76 25.6 4.821333 34.986667-11.008 34.986667-24.618667 0-12.16-0.426667-44.373333-0.64-87.04-142.421333 30.890667-172.458667-68.693333-172.458667-68.693333C188.672 770.986667 155.008 755.2 155.008 755.2c-46.378667-31.744 3.584-31.104 3.584-31.104 51.413333 3.584 78.421333 52.736 78.421333 52.736 45.653333 78.293333 119.850667 55.68 149.12 42.581333 4.608-33.109333 17.792-55.68 32.426667-68.48-113.706667-12.8-233.216-56.832-233.216-253.013333 0-55.893333 19.84-101.546667 52.693333-137.386667-5.76-12.928-23.04-64.981333 4.48-135.509333 0 0 42.88-13.738667 140.8 52.48 40.96-11.392 84.48-17.024 128-17.28 43.52 0.256 87.04 5.888 128 17.28 97.28-66.218667 140.16-52.48 140.16-52.48 27.52 70.528 10.24 122.581333 5.12 135.509333 32.64 35.84 52.48 81.493333 52.48 137.386667 0 196.693333-119.68 240-233.6 252.586667 17.92 15.36 34.56 46.762667 34.56 94.72 0 68.522667-0.64 123.562667-0.64 140.202666 0 13.44 8.96 29.44 35.2 24.32C877.44 942.592 1024 750.592 1024 524.672c0-282.752-229.248-512-512-512" fill="currentColor"/>
                  </svg>
                </el-icon>
              </el-button>
            </div>
            <el-dropdown v-if="showUserInfo" @command="handleCommand" trigger="click">
              <el-button text>
                <el-icon style="margin-right: 4px"><User /></el-icon>
                <span>{{ username }}</span>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="logout">
                    <el-icon><SwitchButton /></el-icon>
                    <span>退出登录</span>
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </div>
      </el-header>
      <el-container class="main-container">
        <!-- 桌面端侧边栏 -->
        <el-aside width="220px" class="modern-aside desktop-aside">
          <el-menu
            :default-active="activeMenu"
            router
            class="modern-menu"
            :unique-opened="true"
          >
            <el-menu-item index="/dashboard" class="menu-item">
              <el-icon><DataAnalysis /></el-icon>
              <span>数据统计</span>
            </el-menu-item>
            <el-menu-item index="/subscriptions" class="menu-item">
              <el-icon><Link /></el-icon>
              <span>订阅管理</span>
            </el-menu-item>
            <el-menu-item index="/nodes" class="menu-item">
              <el-icon><Connection /></el-icon>
              <span>节点管理</span>
            </el-menu-item>
            <el-menu-item v-if="subscriptionAggregationEnabled" index="/subscription-aggregation" class="menu-item">
              <el-icon><Connection /></el-icon>
              <span>订阅聚合</span>
            </el-menu-item>
            <el-menu-item index="/proxy-groups" class="menu-item">
              <el-icon><Menu /></el-icon>
              <span>策略管理</span>
            </el-menu-item>
            <el-menu-item index="/rule-library" class="menu-item">
              <el-icon><FolderOpened /></el-icon>
              <span>规则仓库</span>
            </el-menu-item>
            <el-menu-item index="/rules" class="menu-item">
              <el-icon><Document /></el-icon>
              <span>规则配置</span>
            </el-menu-item>
            <el-menu-item index="/generate" class="menu-item">
              <el-icon><Download /></el-icon>
              <span>生成配置</span>
            </el-menu-item>
            <el-menu-item index="/agents" class="menu-item">
              <el-icon><Monitor /></el-icon>
              <span>Agent 管理</span>
            </el-menu-item>
            <el-menu-item index="/logs" class="menu-item">
              <el-icon><Document /></el-icon>
              <span>日志管理</span>
            </el-menu-item>
          </el-menu>
        </el-aside>

        <!-- 移动端抽屉菜单 -->
        <el-drawer v-model="drawerVisible" direction="ltr" size="70%" class="mobile-drawer">
          <template #header>
            <div class="drawer-header">
              <img src="/icon.png" alt="Logo" class="logo-icon" />
              <h3 class="logo-text">菜单</h3>
            </div>
          </template>
          <el-menu
            :default-active="activeMenu"
            router
            class="modern-menu"
            @select="drawerVisible = false"
          >
            <el-menu-item index="/dashboard">
              <el-icon><DataAnalysis /></el-icon>
              <span>数据统计</span>
            </el-menu-item>
            <el-menu-item index="/subscriptions">
              <el-icon><Link /></el-icon>
              <span>订阅管理</span>
            </el-menu-item>
            <el-menu-item index="/nodes">
              <el-icon><Connection /></el-icon>
              <span>节点管理</span>
            </el-menu-item>
            <el-menu-item v-if="subscriptionAggregationEnabled" index="/subscription-aggregation">
              <el-icon><Connection /></el-icon>
              <span>订阅聚合</span>
            </el-menu-item>
            <el-menu-item index="/proxy-groups">
              <el-icon><Menu /></el-icon>
              <span>策略管理</span>
            </el-menu-item>
            <el-menu-item index="/rule-library">
              <el-icon><FolderOpened /></el-icon>
              <span>规则仓库</span>
            </el-menu-item>
            <el-menu-item index="/rules">
              <el-icon><Document /></el-icon>
              <span>规则配置</span>
            </el-menu-item>
            <el-menu-item index="/generate">
              <el-icon><Download /></el-icon>
              <span>生成配置</span>
            </el-menu-item>
            <el-menu-item index="/agents">
              <el-icon><Monitor /></el-icon>
              <span>Agent 管理</span>
            </el-menu-item>
            <el-menu-item index="/logs">
              <el-icon><Document /></el-icon>
              <span>日志管理</span>
            </el-menu-item>
          </el-menu>
        </el-drawer>

        <el-main class="modern-main">
          <router-view />
        </el-main>
      </el-container>
    </el-container>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { systemApi, statsApi } from './api'
import api from './api'
import { ElMessage, ElMessageBox } from 'element-plus'

const route = useRoute()
const router = useRouter()
const activeMenu = ref(route.path)
const drawerVisible = ref(false)
const versionInfo = ref('v1.0')
const subscriptionAggregationEnabled = ref(false)

// 认证相关
const showUserInfo = ref(false)
const username = ref('')
const isLoginPage = computed(() => route.path === '/login')

// 加载版本信息
const loadVersion = async () => {
  try {
    const response = await systemApi.getVersion()
    if (response.data && response.data.version) {
      versionInfo.value = `v${response.data.version}`
    }
  } catch (error) {
    console.error('Failed to load version:', error)
  }
}

// 加载订阅聚合开关状态
const loadSubscriptionAggregationSetting = async () => {
  try {
    const response = await api.get('/settings/subscription-aggregation')
    subscriptionAggregationEnabled.value = response.data.enabled || false
  } catch (error) {
    console.error('Failed to load subscription aggregation setting:', error)
    // 加载失败，使用 localStorage
    const localEnabled = localStorage.getItem('subscriptionAggregationEnabled') === 'true'
    subscriptionAggregationEnabled.value = localEnabled
  }
}

// 监听订阅聚合开关变化事件
const handleSubscriptionAggregationChange = (event: CustomEvent) => {
  subscriptionAggregationEnabled.value = event.detail.enabled
}

// 检查认证状态并加载用户信息
const checkAuthStatus = async () => {
  try {
    // 先检查本地是否有登录信息
    const storedUsername = localStorage.getItem('username')
    const token = localStorage.getItem('token')

    // 如果有登录信息，直接显示
    if (storedUsername && token) {
      showUserInfo.value = true
      username.value = storedUsername
      return
    }

    // 检查后端认证状态
    const response = await api.get('/auth/status')
    const authEnabled = response.data.authEnabled

    if (authEnabled && storedUsername) {
      showUserInfo.value = true
      username.value = storedUsername
    } else {
      showUserInfo.value = false
    }
  } catch (error) {
    console.error('Failed to check auth status:', error)
    // 即使接口失败，如果有本地登录信息也显示
    const storedUsername = localStorage.getItem('username')
    const token = localStorage.getItem('token')
    if (storedUsername && token) {
      showUserInfo.value = true
      username.value = storedUsername
    } else {
      showUserInfo.value = false
    }
  }
}

// 打开 GitHub 文档
const openGithub = () => {
  window.open('https://github.com/thsrite/config-flow', '_blank')
}

// 处理用户操作命令
const handleCommand = async (command: string) => {
  if (command === 'logout') {
    try {
      await ElMessageBox.confirm('确定要退出登录吗？', '提示', {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      })

      // 清除本地存储
      localStorage.removeItem('token')
      localStorage.removeItem('username')

      ElMessage.success('已退出登录')

      // 跳转到登录页
      router.push('/login')
    } catch (error) {
      // 用户取消操作
    }
  }
}

onMounted(async () => {
  loadVersion()
  checkAuthStatus()

  // 加载订阅聚合配置
  loadSubscriptionAggregationSetting()

  // 监听订阅聚合开关变化
  window.addEventListener('subscription-aggregation-changed', handleSubscriptionAggregationChange as EventListener)
})

onUnmounted(() => {
  // 移除事件监听
  window.removeEventListener('subscription-aggregation-changed', handleSubscriptionAggregationChange as EventListener)
})

watch(() => route.path, (newPath) => {
  activeMenu.value = newPath
})
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

#app {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
  background: #f5f7fa;
  min-height: 100vh;
}

/* 主容器 */
.app-container {
  height: 100vh;
  background: transparent;
  position: relative;
  z-index: 1;
}

/* 现代化头部 */
.modern-header {
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(10px);
  border-bottom: 1px solid rgba(255, 255, 255, 0.2);
  box-shadow: 0 2px 20px rgba(0, 0, 0, 0.1);
  padding: 0 24px;
  height: 64px !important;
}

.header-content {
  display: flex;
  justify-content: space-between;
  align-items: center;
  height: 100%;
  width: 100%;
}

.logo-section {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
  min-width: 0;
}

.logo-icon {
  width: 32px;
  height: 32px;
  object-fit: contain;
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { transform: scale(1); }
  50% { transform: scale(1.05); }
}

.logo-text {
  background: linear-gradient(135deg, #6B73FF 0%, #000DFF 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-size: 20px;
  font-weight: 600;
  margin: 0;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 16px;
}

.version-github-group {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  background: rgba(107, 115, 255, 0.05);
  border-radius: 8px;
  transition: all 0.3s ease;
}

.version-github-group:hover {
  background: rgba(107, 115, 255, 0.1);
}

.version-tag {
  background: linear-gradient(135deg, #6B73FF 0%, #000DFF 100%);
  color: white;
  border: none;
  font-weight: 500;
  font-size: 13px;
}

.pro-badge {
  background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
  color: white;
  border: none;
  font-weight: 600;
  font-size: 12px;
  padding: 4px 12px;
  box-shadow: 0 2px 8px rgba(72, 187, 120, 0.3);
  animation: proPulse 2s ease-in-out infinite;
}

@keyframes proPulse {
  0%, 100% {
    box-shadow: 0 2px 8px rgba(72, 187, 120, 0.3);
    transform: scale(1);
  }
  50% {
    box-shadow: 0 4px 12px rgba(72, 187, 120, 0.5);
    transform: scale(1.02);
  }
}

.github-link {
  color: #606266;
  padding: 4px;
  transition: all 0.3s ease;
  display: flex;
  align-items: center;
}

.github-link:hover {
  color: #6B73FF;
  transform: scale(1.15);
  background: rgba(107, 115, 255, 0.1);
  border-radius: 4px;
}

/* 主内容容器 */
.main-container {
  height: calc(100vh - 64px);
  background: transparent;
}

/* 现代化侧边栏 */
.modern-aside {
  background: #ffffff;
  border-right: 1px solid rgba(107, 115, 255, 0.08);
  padding: 20px 0;
  overflow-y: auto;
  box-shadow: 2px 0 12px rgba(65, 80, 180, 0.05);
}

/* 现代化菜单 */
.modern-menu {
  border: none;
  background: transparent;
  padding: 0 12px;
}

.modern-menu .el-menu-item {
  margin: 6px 0;
  border-radius: 16px;
  height: 48px;
  line-height: 48px;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  color: #5a6c7d;
  font-weight: 600;
  font-size: 14px;
  position: relative;
  overflow: hidden;
}

.modern-menu .el-menu-item::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 0;
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  border-radius: 0 2px 2px 0;
  transition: height 0.3s ease;
}

.modern-menu .el-menu-item:hover {
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.08) 0%, rgba(0, 13, 255, 0.06) 100%);
  color: #000dff;
  transform: translateX(2px);
}

.modern-menu .el-menu-item:hover::before {
  height: 24px;
}

.modern-menu .el-menu-item.is-active {
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.15) 0%, rgba(0, 13, 255, 0.12) 100%);
  color: #000dff;
  box-shadow: 0 4px 16px rgba(107, 115, 255, 0.2);
  border: 1px solid rgba(107, 115, 255, 0.2);
}

.modern-menu .el-menu-item.is-active::before {
  height: 32px;
  width: 4px;
}

.modern-menu .el-menu-item.is-active:hover {
  transform: translateX(0);
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.18) 0%, rgba(0, 13, 255, 0.15) 100%);
}

.modern-menu .el-icon {
  font-size: 20px;
  margin-right: 12px;
  transition: all 0.3s ease;
}

.modern-menu .el-menu-item.is-active .el-icon {
  color: #000dff;
}

.modern-menu .el-menu-item:hover .el-icon {
  transform: scale(1.1);
}

/* 主内容区 */
.modern-main {
  background: transparent;
  padding: 24px;
  overflow-y: auto;
  position: relative;
}

/* 滚动条样式 */
.modern-aside::-webkit-scrollbar,
.modern-main::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}

.modern-aside::-webkit-scrollbar-track,
.modern-main::-webkit-scrollbar-track {
  background: rgba(255, 255, 255, 0.1);
  border-radius: 3px;
}

.modern-aside::-webkit-scrollbar-thumb,
.modern-main::-webkit-scrollbar-thumb {
  background: rgba(107, 115, 255, 0.3);
  border-radius: 3px;
}

.modern-aside::-webkit-scrollbar-thumb:hover,
.modern-main::-webkit-scrollbar-thumb:hover {
  background: rgba(107, 115, 255, 0.5);
}

/* 全局卡片优化 */
:deep(.el-card) {
  background: rgba(255, 255, 255, 0.98);
  backdrop-filter: blur(20px);
  border: 1px solid rgba(255, 255, 255, 0.5);
  border-radius: 16px;
  box-shadow: 0 8px 32px rgba(107, 115, 255, 0.15);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

:deep(.el-card:hover) {
  box-shadow: 0 12px 48px rgba(107, 115, 255, 0.25);
  transform: translateY(-2px);
  border-color: rgba(107, 115, 255, 0.3);
}

:deep(.el-card__header) {
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.08) 0%, rgba(0, 13, 255, 0.08) 100%);
  border-bottom: 1px solid rgba(107, 115, 255, 0.15);
}

/* 全局按钮优化 */
:deep(.el-button--primary) {
  background: linear-gradient(135deg, #6B73FF 0%, #000DFF 100%);
  border: none;
  box-shadow: 0 4px 12px rgba(107, 115, 255, 0.4);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

:deep(.el-button--primary:hover) {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(107, 115, 255, 0.5);
}

:deep(.el-button--success) {
  background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
  border: none;
  box-shadow: 0 4px 12px rgba(72, 187, 120, 0.4);
}

:deep(.el-button--success:hover) {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(72, 187, 120, 0.5);
}

:deep(.el-button--danger) {
  background: linear-gradient(135deg, #9b8fff 0%, #7b73ff 100%);
  border: none;
  box-shadow: 0 4px 12px rgba(155, 143, 255, 0.4);
}

:deep(.el-button--danger:hover) {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(155, 143, 255, 0.5);
}

/* 全局标签优化 */
:deep(.el-tag) {
  border-radius: 6px;
  font-weight: 500;
  border: none;
}

:deep(.el-tag--primary) {
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.2) 0%, rgba(0, 13, 255, 0.2) 100%);
  color: #6B73FF;
}

:deep(.el-tag--success) {
  background: linear-gradient(135deg, rgba(72, 187, 120, 0.2) 0%, rgba(56, 161, 105, 0.2) 100%);
  color: #38a169;
}

:deep(.el-tag--warning) {
  background: linear-gradient(135deg, rgba(237, 137, 54, 0.2) 0%, rgba(221, 107, 32, 0.2) 100%);
  color: #dd6b20;
}

:deep(.el-tag--danger) {
  background: linear-gradient(135deg, rgba(155, 143, 255, 0.2) 0%, rgba(123, 115, 255, 0.2) 100%);
  color: #7b73ff;
}

:deep(.el-tag--info) {
  background: linear-gradient(135deg, rgba(113, 128, 150, 0.2) 0%, rgba(74, 85, 104, 0.2) 100%);
  color: #4a5568;
}

/* 移动端适配 */
.mobile-menu-btn {
  display: none;
  color: #6B73FF;
  padding: 8px;
  margin-right: 8px;
}

.drawer-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.drawer-header .logo-icon {
  width: 24px;
  height: 24px;
}

.drawer-header .logo-text {
  font-size: 18px;
  margin: 0;
}

/* 移动端媒体查询 */
@media (max-width: 768px) {
  /* 显示汉堡菜单按钮 */
  .mobile-menu-btn {
    display: block;
  }

  /* 隐藏桌面端侧边栏 */
  .desktop-aside {
    display: none !important;
  }

  /* Header 调整 */
  .modern-header {
    height: 60px !important;
    padding: 0 16px;
  }

  .logo-section {
    gap: 8px;
    flex: 0 0 auto;
    min-width: 0;
  }

  .logo-text {
    font-size: 15px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .logo-icon {
    width: 28px;
    height: 28px;
    flex-shrink: 0;
  }

  .version-tag {
    font-size: 11px;
    padding: 3px 8px;
  }

  .pro-badge {
    font-size: 10px;
    padding: 3px 8px;
  }

  /* 移动端header-actions调整 */
  .header-actions {
    flex-shrink: 0;
    gap: 8px;
  }

  .version-github-group {
    gap: 6px;
    padding: 4px 6px;
    background: rgba(107, 115, 255, 0.08);
  }

  .github-link {
    padding: 6px;
  }

  .github-link .el-icon {
    font-size: 16px;
  }

  /* 用户信息按钮调整 */
  .header-actions .el-button {
    padding: 6px 10px;
    font-size: 13px;
  }

  .header-actions .el-button .el-icon {
    font-size: 16px;
  }

  /* 主内容容器调整 */
  .main-container {
    height: calc(100vh - 56px);
  }

  .modern-main {
    padding: 12px;
  }

  /* 全局卡片在移动端的调整 */
  :deep(.el-card) {
    border-radius: 12px;
  }

  /* 按钮尺寸调整 */
  :deep(.el-button) {
    padding: 8px 12px;
    font-size: 14px;
  }

  :deep(.el-button.is-small) {
    padding: 6px 10px;
    font-size: 12px;
  }

  /* 对话框在移动端居中显示 */
  :deep(.el-overlay) {
    display: flex !important;
    align-items: center !important;
    justify-content: center !important;
  }

  :deep(.el-overlay-dialog) {
    display: flex !important;
    align-items: center !important;
    justify-content: center !important;
  }

  :deep(.el-dialog) {
    width: 95% !important;
    margin: 0 !important;
    margin-top: 0 !important;
    margin-bottom: 0 !important;
  }

  :deep(.el-dialog__body) {
    padding: 16px;
  }
}

/* 超小屏幕适配 */
@media (max-width: 480px) {
  .modern-header {
    padding: 0 8px;
  }

  .logo-text {
    font-size: 14px;
  }

  .modern-main {
    padding: 8px;
  }

  :deep(.el-dialog) {
    width: 100% !important;
    margin: 0;
    border-radius: 0;
  }
}
</style>
