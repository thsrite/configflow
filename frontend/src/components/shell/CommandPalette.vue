<template>
  <CommandDialog v-model:open="open">
    <CommandInput placeholder="搜索页面或操作…" @update:model-value="onQuery" />
    <CommandList>
      <CommandEmpty>没有找到匹配项，试试其他关键词。</CommandEmpty>
      <!-- 没输入时只列常用页面；开始输入后搜索全部页面 -->
      <CommandGroup v-if="!searching" heading="常用页面" data-frequent-pages>
        <CommandItem
          v-for="item in frequent"
          :key="item.path"
          :value="`${item.label} ${item.path}`"
          @select="go(item.path)"
        >
          <component :is="iconOf(item.icon)" class="size-4 text-muted-foreground" aria-hidden="true" />
          <span>{{ item.label }}</span>
          <CommandShortcut class="font-mono">{{ item.path }}</CommandShortcut>
        </CommandItem>
      </CommandGroup>
      <CommandGroup v-for="group in searching ? groups : []" :key="group.scope" :heading="group.title || '总览'">
        <CommandItem
          v-for="item in visibleItems(group)"
          :key="item.path"
          :value="`${item.label} ${item.path}`"
          @select="go(item.path)"
        >
          <component :is="iconOf(item.icon)" class="size-4 text-muted-foreground" aria-hidden="true" />
          <span>{{ item.label }}</span>
          <CommandShortcut class="font-mono">{{ item.path }}</CommandShortcut>
        </CommandItem>
      </CommandGroup>
      <CommandSeparator />
      <CommandGroup :heading="searching ? '动作' : '常用操作'">
        <CommandItem
          v-for="action in ACTIONS"
          :key="action.id"
          :value="`${ACTION_LABEL[action.id]} ${action.keywords}`"
          @select="onAction(action.id)"
        >
          <component :is="action.icon" class="size-4 text-muted-foreground" aria-hidden="true" />
          <span>{{ ACTION_LABEL[action.id] }}</span>
        </CommandItem>
        <CommandItem value="切换主题 theme dark light" @select="onToggleTheme">
          <SunMoon class="size-4 text-muted-foreground" aria-hidden="true" />
          <span>切换深浅主题</span>
        </CommandItem>
        <CommandItem v-if="preference !== 'system'" value="主题跟随系统 theme system auto" @select="onFollowSystem">
          <Monitor class="size-4 text-muted-foreground" aria-hidden="true" />
          <span>主题跟随系统</span>
        </CommandItem>
      </CommandGroup>
    </CommandList>
  </CommandDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Download, Monitor, RefreshCw, Send, SunMoon, Zap } from '@lucide/vue'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut
} from '@/components/ui/command'
import { MOBILE_TAB_PATHS, NAV_GROUPS, navigationPath, type NavGroup, type NavItem } from '@/navigation'
import { DEFAULT_FREQUENT, DEFAULT_FREQUENT_BEYOND_TABBAR, frequentPages, recordVisit } from '@/lib/frequentPages'
import { useMediaQuery } from '@vueuse/core'
import { iconOf } from '@/lib/icons'
import { useThemeStore } from '@/stores/theme'
import { ACTION_LABEL, runAction, type AppAction } from '@/lib/actions'

const props = defineProps<{ subscriptionAggregationEnabled: boolean }>()

const router = useRouter()
const { preference, setTheme, toggleTheme } = useThemeStore()
const open = ref(false)
const groups = NAV_GROUPS

const visibleItems = (group: NavGroup): NavItem[] =>
  group.items.filter(
    item => item.flag !== 'subscriptionAggregation' || props.subscriptionAggregationEnabled
  )

const query = ref('')
const onQuery = (value: unknown) => { query.value = String(value ?? '') }
const searching = computed(() => !!query.value.trim())
// 每次打开时按最新的访问次数重排
const frequentVersion = ref(0)
watch(open, value => {
  if (!value) return
  query.value = ''
  frequentVersion.value++
})
// 手机和平板显示底栏（与 MobileTabBar 的 max-[900px] 一致）
const hasTabBar = useMediaQuery('(max-width: 900px)')
const tabPaths = new Set<string>(MOBILE_TAB_PATHS)
const allItems = computed(() => groups.flatMap(group => visibleItems(group)))
const frequent = computed<NavItem[]>(() => {
  void frequentVersion.value
  const byPath = new Map(allItems.value.map(item => [item.path, item]))
  // 有底栏时底栏页面点一下就到，不再推荐；改推只能从「更多」进入的页面
  const valid = new Set([...byPath.keys()].filter(path => !hasTabBar.value || !tabPaths.has(path)))
  const defaults = hasTabBar.value ? DEFAULT_FREQUENT_BEYOND_TABBAR : DEFAULT_FREQUENT
  return frequentPages(valid, defaults).map(path => byPath.get(path)!).filter(Boolean)
})
// 记录每次进入的导航页面，用于排出「常用页面」
router.afterEach(to => {
  const path = navigationPath(to.path)
  if (allItems.value.some(item => item.path === path)) recordVisit(path)
})

const go = (path: string): void => {
  open.value = false
  router.push(path)
}

const ACTIONS: Array<{ id: AppAction; icon: typeof Zap; keywords: string }> = [
  { id: 'speedtest', icon: Zap, keywords: 'speed test latency 延迟' },
  { id: 'generate-mihomo', icon: Download, keywords: 'generate build mihomo' },
  { id: 'pull-all', icon: RefreshCw, keywords: 'pull refresh subscription' },
  { id: 'push-all', icon: Send, keywords: 'push deploy agent' }
]

const onAction = (id: AppAction): void => {
  open.value = false
  runAction(router, id)
}

const onToggleTheme = (): void => {
  open.value = false
  toggleTheme()
}

const onFollowSystem = (): void => {
  open.value = false
  setTheme('system')
}

const onKeydown = (event: KeyboardEvent): void => {
  if (event.key.toLowerCase() === 'k' && (event.metaKey || event.ctrlKey)) {
    event.preventDefault()
    open.value = !open.value
  }
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onUnmounted(() => document.removeEventListener('keydown', onKeydown))

defineExpose({ show: () => (open.value = true) })
</script>
