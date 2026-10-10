<template>
  <!-- 系统要求减少动效，或在 Tweaks 里关掉动效时，motion-v 的进场动画一并跳过 -->
  <MotionConfig reduced-motion="user" :skip-animations="skipAnimations">
  <!-- 登录页不套用应用壳 -->
  <template v-if="isLoginPage">
    <router-view />
  </template>

  <div v-else class="relative flex min-h-screen min-h-dvh bg-background">
    <!-- 全局氛围层：点阵 + 暖光 + 纸感颗粒，固定在视口，不参与布局与交互 -->
    <div class="tech-backdrop" aria-hidden="true" />
    <div class="paper-grain" aria-hidden="true" />
    <a
      href="#main-content"
      class="sr-only fixed top-2 left-3 z-[1000] rounded-lg bg-primary px-4 py-3 font-medium text-primary-foreground focus:not-sr-only focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2"
    >
      跳转到主要内容
    </a>

    <AppRail
      class="max-[900px]:hidden"
      :active-path="route.path"
      :profile-name="currentProfileName"
      :subscription-aggregation-enabled="subscriptionAggregationEnabled"
      :version="versionInfo"
    />

    <div class="relative flex min-w-0 flex-1 flex-col">
      <header
        class="glass-strong sticky top-0 z-20 flex h-(--cf-topbar-h) shrink-0 items-center gap-3 border-b border-border/60 px-7 pt-[env(safe-area-inset-top)] max-[900px]:gap-2 max-[900px]:px-3"
        style="box-sizing: content-box"
      >
        <!-- 移动端没有侧栏，品牌放在顶栏 -->
        <router-link
          to="/dashboard"
          class="group hidden min-w-0 shrink items-center gap-2 text-foreground no-underline max-[900px]:flex max-[900px]:min-h-9 max-[900px]:min-w-9"
          aria-label="ConfigFlow 首页"
        >
          <BrandMark class="size-6 text-primary" />
          <span class="font-display truncate text-[17px] font-semibold max-[440px]:hidden">ConfigFlow</span>
        </router-link>

        <!-- 桌面端面包屑：作用域 + 当前页 -->
        <div class="flex min-w-0 items-center gap-2.5 max-[900px]:hidden">
          <span
            v-if="crumb.scope"
            :class="cn(
              'shrink-0 rounded-full border px-2 py-0.5 font-mono text-[10.5px] tracking-[0.06em]',
              crumb.scope === 'profile'
                ? 'border-primary-accent/40 bg-primary-soft text-primary-accent'
                : crumb.scope === 'resource'
                  ? 'border-info-accent/40 text-info-accent'
                  : 'border-border-strong text-muted-foreground'
            )"
          >
            {{ crumb.scopeLabel }}
          </span>
          <span class="truncate text-[13.5px] font-semibold text-foreground">{{ crumb.label }}</span>
        </div>

        <div class="ml-auto flex min-w-0 items-center gap-1.5">
          <!-- 实时吞吐：已绑定在线 Agent 心跳上报的网卡速率，没有上报时整块隐藏 -->
          <div
            v-if="liveThroughput"
            class="mr-1.5 flex items-center gap-3.5 rounded-full border border-border bg-card/70 px-3 py-1.5 font-mono text-[12px] text-muted-foreground max-[1180px]:hidden"
            title="已绑定 Agent 上报的网卡吞吐"
          >
            <span class="text-primary-accent">↓ <b class="font-medium">{{ liveThroughput.down[0] }}</b> {{ liveThroughput.down[1] }}</span>
            <span class="text-success-accent">↑ <b class="font-medium">{{ liveThroughput.up[0] }}</b> {{ liveThroughput.up[1] }}</span>
            <span>{{ onlineCount }}/{{ boundAgents.length }} 在线</span>
          </div>

          <!-- 命令面板入口：桌面显示快捷键，移动端退化为图标按钮 -->
          <button
            type="button"
            class="hidden h-8 min-w-[210px] cursor-pointer items-center gap-2 rounded-lg border border-border/70 bg-card/60 px-2.5 text-[12.5px] text-muted-foreground transition-colors hover:border-border-strong hover:text-foreground md:flex"
            @click="palette?.show()"
          >
            <Search class="size-3.5" aria-hidden="true" />
            <span>搜索页面与操作…</span>
            <kbd class="ml-auto rounded border border-border-strong bg-background/60 px-1.5 py-0.5 font-mono text-[10px]">
              {{ metaKeyLabel }}K
            </kbd>
          </button>

          <ProfileSwitcher class="min-[901px]:hidden" />

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
            title="Tweaks"
            aria-label="Tweaks"
            :aria-expanded="tweaksOpen"
            @click="tweaksOpen = !tweaksOpen"
          >
            <SlidersHorizontal class="size-[17px]" />
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

      <main
        id="main-content"
        tabindex="-1"
        class="relative z-10 min-w-0 flex-1 scroll-mt-32 overflow-x-hidden focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
      >
        <div
          class="mx-auto max-w-(--cf-content-max) px-8 pt-7 pb-12 max-[900px]:px-4 max-[900px]:pt-4 max-[900px]:pb-[calc(env(safe-area-inset-bottom)+var(--cf-tabbar-h)+40px)]"
        >
          <!-- 不做整页过渡：out-in 会在两页之间留一帧空白，观感是闪一下。
               进场动效交给页面内的卡片与列表逐项播放。 -->
          <MobileGroupNav
            :active-path="route.path"
            :subscription-aggregation-enabled="subscriptionAggregationEnabled"
          />
          <router-view :key="pageKey" />
        </div>
      </main>
    </div>

    <MobileTabBar :active-path="route.path" @more="palette?.show()" />

    <TweaksPanel :open="tweaksOpen" @close="tweaksOpen = false" />

    <CommandPalette
      ref="palette"
      :subscription-aggregation-enabled="subscriptionAggregationEnabled"
    />
  </div>

  <Toaster position="bottom-center" :duration="2600" :mobile-offset="{ bottom: '92px' }" />
  <ConfirmHost />
  <PromptHost />
  </MotionConfig>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { FileText, LogOut, Moon, Search, SlidersHorizontal, Sun, User } from '@lucide/vue'
import { useMediaQuery } from '@vueuse/core'
import { MotionConfig } from 'motion-v'
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
import BrandMark from '@/components/common/BrandMark.vue'
import { cn } from '@/lib/utils'
import { systemApi } from './api'
import api from './api'
import ConfirmHost from './components/feedback/ConfirmHost.vue'
import PromptHost from './components/feedback/PromptHost.vue'
import ProfileSwitcher from './components/ProfileSwitcher.vue'
import AppRail from './components/shell/AppRail.vue'
import CommandPalette from './components/shell/CommandPalette.vue'
import MobileGroupNav from './components/shell/MobileGroupNav.vue'
import MobileTabBar from './components/shell/MobileTabBar.vue'
import TweaksPanel from './components/shell/TweaksPanel.vue'
import { formatRate, useLive } from './stores/live'
import { confirm, notify } from './lib/feedback'
import { usePreferences } from './stores/preferences'
import { useProfileStore } from './stores/profile'
import { useThemeStore } from './stores/theme'
import { groupOfScope, labelOfPath, scopeOfPath } from './navigation'

const reduceMotion = useMediaQuery('(prefers-reduced-motion: reduce)')
const { prefs } = usePreferences()
const skipAnimations = computed(() => reduceMotion.value || !prefs.value.motion)

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
const tweaksOpen = ref(false)

/* ---------- 实时吞吐（顶栏） ---------- */
const live = useLive()
const { boundAgents, onlineCount } = live
const liveThroughput = computed(() => {
  const t = live.throughput.value
  return t ? { down: formatRate(t.down), up: formatRate(t.up) } : null
})
const isLoginPage = computed(() => route.path === '/login')
/* 只有配置空间内的页面随配置空间切换重建；共享资源与系统页不受影响 */
const pageKey = computed(() =>
  scopeOfPath(route.path) === 'profile' || route.path === '/dashboard'
    ? `${activeProfileId.value}:${route.path}`
    : route.path
)

// 快捷键提示按平台显示，Windows/Linux 上写 ⌘ 会误导
const metaKeyLabel = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl+'

const currentProfileName = computed(
  () => profileStore.activeProfile.value?.name || activeProfileId.value || '默认'
)

/* ---------- 桌面面包屑：作用域 + 当前页 ---------- */
const crumb = computed(() => {
  const scope = scopeOfPath(route.path)
  return {
    scope,
    scopeLabel: scope ? groupOfScope(scope)?.title || '总览' : '',
    label: labelOfPath(route.path) || ''
  }
})

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

/* 登录完成进入应用壳时（以及首次打开时）重新读取登录态与配置空间 */
watch(isLoginPage, login => {
  if (login) return
  checkAuthStatus()
  profileStore.refreshProfiles().catch(() => undefined)
  loadSubscriptionAggregationSetting()
}, { immediate: true })

onMounted(async () => {
  live.start()
  loadVersion()
  window.addEventListener(
    'subscription-aggregation-changed',
    handleSubscriptionAggregationChange as EventListener
  )
})

onUnmounted(() => {
  live.stop()
  window.removeEventListener(
    'subscription-aggregation-changed',
    handleSubscriptionAggregationChange as EventListener
  )
})
</script>

