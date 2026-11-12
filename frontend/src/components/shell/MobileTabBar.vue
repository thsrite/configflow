<template>
  <nav
    class="glass-strong fixed inset-x-0 bottom-0 z-30 hidden h-[calc(var(--cf-tabbar-h)+env(safe-area-inset-bottom))] grid-cols-6 gap-0.5 border-t border-border-strong px-1.5 pt-1 pb-[calc(env(safe-area-inset-bottom)+4px)] max-[900px]:grid"
    aria-label="主导航"
  >
    <component
      :is="tab.path ? 'router-link' : 'button'"
      v-for="tab in TABS"
      :key="tab.label"
      :to="tab.path"
      :type="tab.path ? undefined : 'button'"
      :class="cn(
        'relative flex min-h-11 min-w-0 cursor-pointer flex-col items-center justify-center gap-0.5 rounded-lg border-0 bg-transparent px-0 text-[10.5px] font-semibold no-underline transition-colors duration-200 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none',
        isActive(tab) ? 'text-primary-accent' : 'text-muted-foreground'
      )"
      :aria-current="tab.path && isActive(tab) ? 'page' : undefined"
      :aria-haspopup="tab.path ? undefined : 'dialog'"
      :aria-expanded="tab.path ? undefined : moreOpen"
      @click="!tab.path && (moreOpen = true)"
    >
      <Motion
        v-if="isActive(tab)"
        layout-id="tab-active"
        class="absolute inset-0 rounded-lg bg-primary-soft"
        :transition="SPRING"
        aria-hidden="true"
      />
      <component :is="tab.icon" class="relative size-5" :stroke-width="2" aria-hidden="true" />
      <span class="relative max-w-full truncate">{{ tab.label }}</span>
    </component>
  </nav>

  <!-- 更多：底栏之外的页面按分组排成三列图标方块 -->
  <Sheet v-model:open="moreOpen">
    <SheetContent
      side="bottom"
      :show-close-button="false"
      class="max-h-[85dvh] gap-0 rounded-t-2xl border-border bg-card pb-[env(safe-area-inset-bottom)] shadow-overlay data-[state=open]:duration-200 data-[state=closed]:duration-150"
    >
      <SheetHeader class="shrink-0 flex-row items-center justify-between gap-3 border-b border-border px-4 py-3">
        <SheetTitle class="text-base">更多页面</SheetTitle>
        <SheetDescription class="sr-only">选择要打开的页面</SheetDescription>
        <SheetClose as-child>
          <Button variant="ghost" size="icon" class="size-11 shrink-0" aria-label="关闭更多页面">
            <X class="size-5" aria-hidden="true" />
          </Button>
        </SheetClose>
      </SheetHeader>
      <div class="min-h-0 overflow-y-auto overscroll-contain px-4 pt-3 pb-5">
        <section v-for="group in moreGroups" :key="group.scope" class="mb-4 last:mb-0" :aria-label="group.title">
          <h3 class="m-0 mb-2 px-1 text-[12px] font-medium text-muted-foreground">{{ group.title }}</h3>
          <div class="grid grid-cols-3 gap-2">
            <router-link
              v-for="item in group.items"
              :key="item.path"
              :to="item.path"
              data-more-item
              :class="cn(
                'flex min-h-[76px] min-w-0 flex-col items-center justify-center gap-1.5 rounded-xl border px-1.5 py-2.5 text-center text-[12.5px] font-medium no-underline transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring',
                activePath === item.path
                  ? 'border-primary-accent/30 bg-primary-soft text-primary-accent'
                  : 'border-border bg-secondary/40 text-foreground hover:bg-accent active:bg-accent'
              )"
              :aria-current="activePath === item.path ? 'page' : undefined"
              @click="closeIfCurrent(item.path)"
            >
              <component :is="iconOf(item.icon)" class="size-5 shrink-0" :stroke-width="2" aria-hidden="true" />
              <span class="max-w-full [overflow-wrap:anywhere]">{{ item.label }}</span>
            </router-link>
          </div>
        </section>
      </div>
    </SheetContent>
  </Sheet>
</template>

<script setup lang="ts">
import { computed, ref, watch, type Component } from 'vue'
import { Motion } from 'motion-v'
import { Gauge, Globe, LayoutDashboard, LayoutGrid, ListChecks, Server, X } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { MOBILE_TAB_PATHS, NAV_GROUPS } from '@/navigation'
import { iconOf } from '@/lib/icons'
import { SPRING } from '@/lib/motion'
import { cn } from '@/lib/utils'

/* 底栏放最常用的五个页面直达，其余收进「更多」面板 */
interface Tab {
  label: string
  icon: Component
  path?: string
}

const TABS: Tab[] = [
  { label: '首页', icon: LayoutDashboard, path: '/dashboard' },
  { label: 'OxiDNS', icon: Globe, path: '/oxidns' },
  { label: 'Mihomo', icon: Gauge, path: '/mihomo' },
  { label: '规则', icon: ListChecks, path: '/rules' },
  { label: 'Agent', icon: Server, path: '/agents' },
  { label: '更多', icon: LayoutGrid }
]

const props = withDefaults(defineProps<{ activePath: string; subscriptionAggregationEnabled?: boolean }>(), {
  subscriptionAggregationEnabled: false
})

const moreOpen = ref(false)
const tabPaths = new Set<string>(MOBILE_TAB_PATHS)

const moreGroups = computed(() => NAV_GROUPS
  .map(group => ({
    scope: group.scope,
    title: group.title || group.tabLabel,
    items: group.items.filter(item => !tabPaths.has(item.path)
      && (item.flag !== 'subscriptionAggregation' || props.subscriptionAggregationEnabled))
  }))
  .filter(group => group.items.length))

// 当前页不在底栏直达的五项里时，高亮「更多」，让人知道自己在哪
const isActive = (tab: Tab) => tab.path ? props.activePath === tab.path : !tabPaths.has(props.activePath)

// 换页后关闭；点当前页时路由不变，直接关闭
watch(() => props.activePath, () => { moreOpen.value = false })
const closeIfCurrent = (path: string) => {
  if (path === props.activePath) moreOpen.value = false
}
</script>
