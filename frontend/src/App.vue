<template>
  <!-- 登录页不套用应用壳 -->
  <router-view v-if="isLoginPage" />

  <div v-else class="flex min-h-screen min-h-dvh flex-col bg-background">
    <header
      class="sticky top-0 z-800 flex h-(--cf-topbar-h) shrink-0 items-center gap-3 border-b border-border bg-background/85 px-4 pt-[env(safe-area-inset-top)] backdrop-blur-xl backdrop-saturate-150 max-[900px]:gap-2 max-[900px]:px-3"
      style="box-sizing: content-box"
    >
      <router-link
        to="/dashboard"
        class="flex min-w-0 shrink items-center gap-2 text-[15px] font-semibold tracking-[-0.01em] text-foreground no-underline max-[900px]:min-h-9 max-[900px]:min-w-9"
      >
        <img src="/icon.png" alt="" class="size-6 rounded-[7px]" />
        <span class="truncate max-[360px]:hidden">ConfigFlow</span>
      </router-link>

      <div class="ml-auto flex min-w-0 flex-1 items-center justify-end gap-2">
        <ProfileSwitcher />

        <Badge variant="outline" class="rounded-md font-mono text-[11px] max-[420px]:hidden">
          {{ versionInfo }}
        </Badge>

        <Button
          variant="ghost"
          size="icon-sm"
          :title="theme === 'dark' ? '切换到浅色' : '切换到深色'"
          :aria-label="theme === 'dark' ? '切换到浅色' : '切换到深色'"
          @click="toggleTheme"
        >
          <component :is="theme === 'dark' ? Sun : Moon" class="size-[17px]" />
        </Button>

        <Button
          variant="ghost"
          size="icon-sm"
          class="max-[900px]:hidden"
          title="查看文档"
          aria-label="查看文档"
          @click="openGithub"
        >
          <FileText class="size-[17px]" />
        </Button>

        <DropdownMenu v-if="showUserInfo">
          <DropdownMenuTrigger as-child>
            <Button variant="ghost" size="icon-sm" :title="username" :aria-label="`用户 ${username}`">
              <User class="size-[17px]" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem @select="handleCommand('logout')">
              <LogOut class="size-4" />
              <span>退出登录</span>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>

    <div class="flex min-h-0 flex-1">
      <AppRail
        class="max-[900px]:hidden"
        :active-path="route.path"
        :profile-name="currentProfileName"
        :subscription-aggregation-enabled="subscriptionAggregationEnabled"
      />

      <main class="min-w-0 flex-1 overflow-x-hidden">
        <div
          class="mx-auto max-w-(--cf-content-max) px-8 pt-5 pb-8 max-[900px]:px-4 max-[900px]:pt-4 max-[900px]:pb-[calc(env(safe-area-inset-bottom)+var(--cf-tabbar-h)+var(--cf-sp-5))]"
        >
          <MobileGroupNav
            :active-path="route.path"
            :subscription-aggregation-enabled="subscriptionAggregationEnabled"
          />
          <router-view :key="activeProfileId" />
        </div>
      </main>
    </div>

    <MobileTabBar :active-scope="activeScope" @select="openGroup" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { FileText, LogOut, Moon, Sun, User } from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { systemApi } from './api'
import api from './api'
import ProfileSwitcher from './components/ProfileSwitcher.vue'
import AppRail from './components/shell/AppRail.vue'
import MobileTabBar from './components/shell/MobileTabBar.vue'
import MobileGroupNav from './components/shell/MobileGroupNav.vue'
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
const activeScope = computed(() => scopeOfPath(route.path))

const itemsOf = (group: NavGroup): NavItem[] =>
  group.items.filter(
    item => item.flag !== 'subscriptionAggregation' || subscriptionAggregationEnabled.value
  )

/**
 * 底栏切换分组：直接进入该分组的第一个页面。
 * 分组内的页面切换交给内容区顶部的分段控件，避免多一次点击和一层模态。
 */
const openGroup = (group: NavGroup): void => {
  const items = itemsOf(group)
  if (!items.length) return
  // 已在该分组内时停留在当前页，不打断用户
  if (scopeOfPath(route.path) === group.scope) return
  router.push(items[0].path)
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
</script>
