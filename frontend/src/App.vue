<template>
  <!-- 登录页不套用应用壳 -->
  <router-view v-if="isLoginPage" />

  <div v-else class="cf-shell">
    <header class="cf-topbar">
      <router-link to="/dashboard" class="cf-brand">
        <img src="/icon.png" alt="" class="cf-brand__logo" />
        <span class="cf-brand__name">ConfigFlow</span>
      </router-link>

      <div class="cf-topbar__actions">
        <ProfileSwitcher />
        <el-tag class="cf-version cf-mono" effect="plain" size="small">{{ versionInfo }}</el-tag>
        <el-button
          class="cf-icon-btn"
          text
          :title="theme === 'dark' ? '切换到浅色' : '切换到深色'"
          :aria-label="theme === 'dark' ? '切换到浅色' : '切换到深色'"
          @click="toggleTheme"
        >
          <el-icon :size="17"><component :is="theme === 'dark' ? 'Sunny' : 'Moon'" /></el-icon>
        </el-button>
        <el-button class="cf-icon-btn cf-hide-mobile" text title="查看文档" aria-label="查看文档" @click="openGithub">
          <el-icon :size="17"><Document /></el-icon>
        </el-button>
        <el-dropdown v-if="showUserInfo" trigger="click" @command="handleCommand">
          <el-button class="cf-icon-btn" text :title="username" :aria-label="`用户 ${username}`">
            <el-icon :size="17"><User /></el-icon>
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
    </header>

    <div class="cf-body">
      <AppRail
        class="cf-hide-mobile"
        :active-path="route.path"
        :profile-name="currentProfileName"
        :subscription-aggregation-enabled="subscriptionAggregationEnabled"
      />

      <main class="cf-main">
        <div class="cf-main__inner">
          <router-view :key="activeProfileId" />
        </div>
      </main>
    </div>

    <MobileTabBar class="cf-show-mobile" :active-scope="activeScope" @select="openGroup" />

    <!-- 移动端二级入口：分组内页面选择 -->
    <el-drawer
      v-model="groupSheetVisible"
      direction="btt"
      size="auto"
      :with-header="false"
      class="cf-group-sheet"
    >
      <div v-if="openedGroup" class="cf-sheet">
        <div class="cf-sheet__grip" aria-hidden="true"></div>
        <div class="cf-sheet__title">
          {{ openedGroup.scope === 'profile' ? `当前配置 · ${currentProfileName}` : openedGroup.tabLabel }}
        </div>
        <div v-if="openedGroup.hint" class="cf-sheet__hint">{{ openedGroup.hint }}</div>
        <button
          v-for="item in openedGroupItems"
          :key="item.path"
          type="button"
          class="cf-sheet__row"
          :class="{ 'is-active': route.path === item.path }"
          @click="goto(item.path)"
        >
          <el-icon class="cf-sheet__icon"><component :is="item.icon" /></el-icon>
          <span class="cf-sheet__label">{{ item.label }}</span>
          <el-icon class="cf-sheet__chev"><ArrowRight /></el-icon>
        </button>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { systemApi } from './api'
import api from './api'
import ProfileSwitcher from './components/ProfileSwitcher.vue'
import AppRail from './components/shell/AppRail.vue'
import MobileTabBar from './components/shell/MobileTabBar.vue'
import { useProfileStore } from './stores/profile'
import { useThemeStore } from './stores/theme'
import { scopeOfPath, type NavGroup, type NavItem } from './navigation'

const route = useRoute()
const router = useRouter()
const profileStore = useProfileStore()
const { activeProfileId } = profileStore
const { theme, toggleTheme } = useThemeStore()

const versionInfo = ref('v1.0')
const subscriptionAggregationEnabled = ref(false)
const showUserInfo = ref(false)
const username = ref('')
const isLoginPage = computed(() => route.path === '/login')

const currentProfileName = computed(
  () => profileStore.activeProfile.value?.name || activeProfileId.value || '默认'
)

/* ---------- 移动端分组入口 ---------- */
const groupSheetVisible = ref(false)
const openedGroup = ref<NavGroup | null>(null)
const activeScope = computed(() => scopeOfPath(route.path))

const itemsOf = (group: NavGroup): NavItem[] =>
  group.items.filter(
    item => item.flag !== 'subscriptionAggregation' || subscriptionAggregationEnabled.value
  )

const openedGroupItems = computed(() => (openedGroup.value ? itemsOf(openedGroup.value) : []))

const openGroup = (group: NavGroup): void => {
  const items = itemsOf(group)
  // 单页分组直接跳转，不弹无意义的选择面板
  if (items.length === 1) {
    router.push(items[0].path)
    return
  }
  openedGroup.value = group
  groupSheetVisible.value = true
}

const goto = (path: string): void => {
  groupSheetVisible.value = false
  if (route.path !== path) router.push(path)
}

/* ---------- 既有业务逻辑 ---------- */
const loadVersion = async () => {
  try {
    const response = await systemApi.getVersion()
    if (response.data && response.data.version) {
      const ver = response.data.version
      versionInfo.value = ver.startsWith('v') ? ver : `v${ver}`
    }
  } catch (error) {
    console.error('Failed to load version:', error)
  }
}

const loadSubscriptionAggregationSetting = async () => {
  try {
    const response = await api.get('/settings/subscription-aggregation')
    subscriptionAggregationEnabled.value = response.data.enabled || false
  } catch (error) {
    console.error('Failed to load subscription aggregation setting:', error)
    subscriptionAggregationEnabled.value =
      localStorage.getItem('subscriptionAggregationEnabled') === 'true'
  }
}

const handleSubscriptionAggregationChange = (event: CustomEvent) => {
  subscriptionAggregationEnabled.value = event.detail.enabled
}

const checkAuthStatus = async () => {
  try {
    const storedUsername = localStorage.getItem('username')
    const token = localStorage.getItem('token')

    if (storedUsername && token) {
      showUserInfo.value = true
      username.value = storedUsername
      return
    }

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

const openGithub = () => {
  window.open('https://github.com/thsrite/configflow', '_blank')
}

const handleCommand = async (command: string) => {
  if (command === 'logout') {
    try {
      await ElMessageBox.confirm('确定要退出登录吗？', '提示', {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      })

      localStorage.removeItem('token')
      localStorage.removeItem('username')
      ElMessage.success('已退出登录')
      router.push('/login')
    } catch (error) {
      // 用户取消操作
    }
  }
}

onMounted(async () => {
  loadVersion()
  checkAuthStatus()
  profileStore.refreshProfiles().catch(() => undefined)
  loadSubscriptionAggregationSetting()
  window.addEventListener(
    'subscription-aggregation-changed',
    handleSubscriptionAggregationChange as EventListener
  )
})

onUnmounted(() => {
  window.removeEventListener(
    'subscription-aggregation-changed',
    handleSubscriptionAggregationChange as EventListener
  )
})

// 路由切换后关闭移动端分组面板，避免返回时残留
watch(
  () => route.path,
  () => {
    groupSheetVisible.value = false
  }
)
</script>

<style scoped>
.cf-shell {
  min-height: 100vh;
  min-height: 100dvh;
  display: flex;
  flex-direction: column;
  background: var(--cf-bg);
}

/* ---------- 顶栏：紧凑 52px ---------- */
.cf-topbar {
  position: sticky;
  top: 0;
  z-index: 800;
  height: var(--cf-topbar-h);
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--cf-sp-3);
  padding: 0 var(--cf-sp-4);
  padding-top: env(safe-area-inset-top);
  box-sizing: content-box;
  background: color-mix(in srgb, var(--cf-bg) 88%, transparent);
  backdrop-filter: saturate(1.4) blur(14px);
  border-bottom: 1px solid var(--cf-bd);
}

.cf-brand {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  text-decoration: none;
  color: var(--cf-fg);
  font-weight: 650;
  font-size: 15px;
  letter-spacing: -0.01em;
  min-width: 0;
  flex: 0 1 auto;
}

.cf-brand__name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cf-brand__logo {
  width: 24px;
  height: 24px;
  border-radius: 7px;
}

.cf-topbar__actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  min-width: 0;
  flex: 1 1 auto;
  justify-content: flex-end;
}

.cf-version {
  font-size: 11px;
}

.cf-icon-btn {
  width: var(--cf-ctrl-h);
  height: var(--cf-ctrl-h);
  min-height: var(--cf-ctrl-h);
  padding: 0;
  border-radius: var(--cf-r-md);
  color: var(--cf-fg-2);
}
.cf-icon-btn:hover {
  background: var(--cf-s2);
  color: var(--cf-fg);
}

/* ---------- 主体 ---------- */
.cf-body {
  flex: 1 1 auto;
  display: flex;
  min-height: 0;
}

.cf-main {
  flex: 1 1 auto;
  min-width: 0;
  overflow-x: hidden;
}

.cf-main__inner {
  max-width: var(--cf-content-max);
  margin: 0 auto;
  padding: var(--cf-sp-5) var(--cf-sp-6) var(--cf-sp-6);
}

/* ---------- 响应式：桌面 rail / 移动底栏 ---------- */
.cf-show-mobile {
  display: none;
}

@media (max-width: 900px) {
  .cf-hide-mobile {
    display: none !important;
  }
  .cf-topbar {
    gap: var(--cf-sp-2);
    padding: 0 var(--cf-sp-3);
  }
  /* 窄屏只剩 24px 图标时，链接自身要撑出可触摸区域 */
  .cf-brand {
    min-width: 34px;
    min-height: 34px;
  }
  .cf-show-mobile {
    display: grid;
  }
  .cf-main__inner {
    padding: var(--cf-sp-4) var(--cf-sp-4)
      calc(env(safe-area-inset-bottom) + var(--cf-tabbar-h) + var(--cf-sp-5));
  }
}

/* 380px 以下：版本号与品牌名依次让位，保证顶栏不撑宽文档 */
@media (max-width: 420px) {
  .cf-version {
    display: none;
  }
}

@media (max-width: 360px) {
  .cf-brand__name {
    display: none;
  }
}

/* ---------- 移动端分组面板 ---------- */
.cf-sheet {
  padding: var(--cf-sp-2) var(--cf-sp-4) calc(env(safe-area-inset-bottom) + var(--cf-sp-4));
}

.cf-sheet__grip {
  width: 36px;
  height: 4px;
  border-radius: var(--cf-r-pill);
  background: var(--cf-bd-strong);
  margin: 0 auto var(--cf-sp-3);
}

.cf-sheet__title {
  font-size: 15px;
  font-weight: 650;
  color: var(--cf-fg);
}

.cf-sheet__hint {
  font-size: 12px;
  color: var(--cf-fg-2);
  margin-top: 2px;
  margin-bottom: var(--cf-sp-2);
}

.cf-sheet__row {
  width: 100%;
  min-height: var(--cf-touch);
  display: flex;
  align-items: center;
  gap: var(--cf-sp-3);
  padding: var(--cf-sp-3) 0;
  background: none;
  border: none;
  border-top: 1px solid var(--cf-bd);
  color: var(--cf-fg);
  font-family: inherit;
  font-size: 15px;
  font-weight: 550;
  text-align: left;
  cursor: pointer;
}

.cf-sheet__row.is-active {
  color: var(--cf-primary);
}

.cf-sheet__icon {
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  border-radius: var(--cf-r-md);
  background: var(--cf-s3);
  display: grid;
  place-items: center;
  font-size: 15px;
}

.cf-sheet__label {
  flex: 1 1 auto;
}

.cf-sheet__chev {
  color: var(--cf-fg-3);
}
</style>

<style>
/* 移动端分组面板走全局样式：Element Plus drawer 渲染在 body 下 */
.cf-group-sheet.el-drawer {
  border-radius: var(--cf-r-xl) var(--cf-r-xl) 0 0;
  background: var(--cf-s1);
}
.cf-group-sheet .el-drawer__body {
  padding: 0;
}
</style>
