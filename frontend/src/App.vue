<template>
  <!-- 登录页不套用应用壳 -->
  <template v-if="isLoginPage">
    <router-view />
  </template>

  <div v-else class="relative flex min-h-screen min-h-dvh flex-col bg-background">
    <!-- 全局氛围层：网格 + 极光，固定在视口，不参与布局与交互 -->
    <div class="tech-backdrop" aria-hidden="true" />

    <header
      class="glass-strong sticky top-0 z-800 flex h-(--cf-topbar-h) shrink-0 items-center gap-3 border-b border-border/50 px-4 pt-[env(safe-area-inset-top)] max-[900px]:gap-2 max-[900px]:px-3"
      style="box-sizing: content-box"
    >
      <router-link
        to="/dashboard"
        class="group flex min-w-0 shrink items-center gap-2.5 text-[15px] font-semibold tracking-[-0.015em] text-foreground no-underline max-[900px]:min-h-9 max-[900px]:min-w-9"
      >
        <span class="relative flex size-7 items-center justify-center">
          <span
            class="absolute inset-0 rounded-[9px] bg-linear-to-br from-primary/50 to-accent-2-fill/40 blur-[7px] transition-opacity duration-300 group-hover:opacity-100 opacity-70"
            aria-hidden="true"
          />
          <img src="/icon.png" alt="" class="relative size-6.5 rounded-[8px]" />
        </span>
        <span class="truncate max-[360px]:hidden">ConfigFlow</span>
      </router-link>

      <!-- 命令面板入口：桌面显示快捷键，移动端退化为图标按钮 -->
      <button
        type="button"
        class="ml-2 hidden h-8 min-w-[190px] cursor-pointer items-center gap-2 rounded-lg border border-border/60 bg-background/40 px-2.5 text-[12.5px] text-muted-foreground transition-colors hover:border-border-strong hover:text-foreground md:flex"
        @click="palette?.show()"
      >
        <Search class="size-3.5" aria-hidden="true" />
        <span>快速跳转…</span>
        <kbd class="ml-auto rounded border border-border/70 px-1.5 py-0.5 font-mono text-[10px]">
          {{ metaKeyLabel }}K
        </kbd>
      </button>

      <div class="ml-auto flex min-w-0 items-center gap-1.5">
        <ProfileSwitcher />

        <Badge
          variant="outline"
          class="rounded-md border-border/60 font-mono text-[11px] text-muted-foreground max-[420px]:hidden"
        >
          {{ versionInfo }}
        </Badge>

        <Button
          variant="ghost"
          size="icon-sm"
          class="md:hidden"
          title="快速跳转"
          aria-label="快速跳转"
          @click="palette?.show()"
        >
          <Search class="size-[17px]" />
        </Button>

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
          <DropdownMenuContent align="end" class="glass-strong">
            <DropdownMenuLabel class="text-[12px] font-normal text-muted-foreground">
              已登录 · {{ username }}
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem @select="handleCommand('logout')">
              <LogOut class="size-4" />
              <span>退出登录</span>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>

    <div class="relative flex min-h-0 flex-1">
      <AppRail
        class="max-[900px]:hidden"
        :active-path="route.path"
        :profile-name="currentProfileName"
        :subscription-aggregation-enabled="subscriptionAggregationEnabled"
      />

      <main class="relative z-10 min-w-0 flex-1 overflow-x-hidden">
        <div
          class="mx-auto max-w-(--cf-content-max) px-8 pt-6 pb-10 max-[900px]:px-4 max-[900px]:pt-4 max-[900px]:pb-[calc(env(safe-area-inset-bottom)+var(--cf-tabbar-h)+var(--cf-sp-5))]"
        >
          <MobileGroupNav
            :active-path="route.path"
            :subscription-aggregation-enabled="subscriptionAggregationEnabled"
          />
          <router-view v-slot="{ Component }">
            <!-- 路由切换过渡：out-in 避免两页同时占位导致的跳动 -->
            <transition name="page" mode="out-in">
              <component :is="Component" :key="`${activeProfileId}:${route.path}`" />
            </transition>
          </router-view>
        </div>
      </main>
    </div>

    <MobileTabBar :active-scope="activeScope" @select="openGroup" />

    <CommandPalette
      ref="palette"
      :subscription-aggregation-enabled="subscriptionAggregationEnabled"
    />
  </div>

  <Toaster position="top-center" rich-colors close-button :duration="3000" />
  <ConfirmHost />
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { FileText, LogOut, Moon, Search, Sun, User } from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { Toaster } from '@/components/ui/sonner'
import { systemApi } from './api'
import api from './api'
import ConfirmHost from './components/feedback/ConfirmHost.vue'
import ProfileSwitcher from './components/ProfileSwitcher.vue'
import AppRail from './components/shell/AppRail.vue'
import CommandPalette from './components/shell/CommandPalette.vue'
import MobileTabBar from './components/shell/MobileTabBar.vue'
import MobileGroupNav from './components/shell/MobileGroupNav.vue'
import { confirm, notify } from './lib/feedback'
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
const palette = ref<InstanceType<typeof CommandPalette> | null>(null)
const isLoginPage = computed(() => route.path === '/login')

// 快捷键提示按平台显示，Windows/Linux 上写 ⌘ 会误导
const metaKeyLabel = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl+'

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
  if (command !== 'logout') return
  const ok = await confirm('确定要退出登录吗？', { title: '退出登录', confirmText: '退出' })
  if (!ok) return

  localStorage.removeItem('token')
  localStorage.removeItem('username')
  notify.success('已退出登录')
  router.push('/login')
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

<style>
/* 路由过渡：进出场都很短，避免翻页时的等待感 */
.page-enter-active {
  transition:
    opacity 0.22s var(--cf-ease),
    transform 0.22s var(--cf-ease);
}

.page-leave-active {
  transition:
    opacity 0.12s var(--cf-ease),
    transform 0.12s var(--cf-ease);
}

.page-enter-from {
  opacity: 0;
  transform: translateY(8px);
}

.page-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

@media (prefers-reduced-motion: reduce) {
  .page-enter-active,
  .page-leave-active {
    transition: none;
  }
}
</style>
