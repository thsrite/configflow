<template>
  <!-- 手机用底部面板切换；平板空间足够时平铺，桌面仍使用侧栏。 -->
  <nav
    v-if="group && activeItem && items.length > 1"
    ref="navContainer"
    class="mb-5 hidden min-w-0 max-[900px]:block max-sm:mb-2"
    aria-label="分组内页面"
  >
    <div
      v-show="showTabs"
      ref="tabsContainer"
      class="grid gap-2"
      :style="{ gridTemplateColumns: `repeat(${items.length}, minmax(0, 1fr))` }"
    >
      <router-link
        v-for="item in items"
        :key="item.path"
        :to="item.path"
        :class="cn(
          'inline-flex min-h-11 min-w-0 items-center justify-center gap-2 rounded-lg border px-3 text-[13px] font-medium no-underline transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring',
          activePath === item.path
            ? 'border-primary-accent/30 bg-primary-soft text-primary-accent'
            : 'border-border bg-card text-muted-foreground hover:bg-muted hover:text-foreground'
        )"
        :aria-current="activePath === item.path ? 'page' : undefined"
      >
        <component :is="iconOf(item.icon)" class="size-3.5 shrink-0" :stroke-width="2" aria-hidden="true" />
        <span class="[overflow-wrap:anywhere]">{{ item.label }}</span>
      </router-link>
    </div>

    <Sheet v-model:open="open">
      <SheetTrigger as-child>
        <!-- 卡片样式的按钮：左侧主色图标方块，右侧圆形箭头，一眼能看出可以点 -->
        <button
          v-show="!showTabs"
          type="button"
          data-group-nav-trigger
          class="flex min-h-12 w-full cursor-pointer items-center gap-2.5 rounded-xl border border-border-strong bg-card py-2 pr-2 pl-2.5 text-left text-[13.5px] font-medium text-foreground transition-colors hover:border-primary-accent/40 active:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          :aria-label="`切换${group.tabLabel}页面，当前${activeItem.label}`"
        >
          <span class="flex size-[30px] shrink-0 items-center justify-center rounded-[9px] bg-primary-soft text-primary-accent" aria-hidden="true">
            <component :is="iconOf(activeItem.icon)" class="size-4" :stroke-width="2" />
          </span>
          <span class="min-w-0 flex-1 [overflow-wrap:anywhere]">
            <span class="text-muted-foreground">{{ group.tabLabel }} · </span>{{ activeItem.label }}
          </span>
          <span class="flex size-[30px] shrink-0 items-center justify-center rounded-full bg-accent text-foreground" aria-hidden="true">
            <ChevronDown class="size-4" />
          </span>
        </button>
      </SheetTrigger>

      <SheetContent
        side="bottom"
        :show-close-button="false"
        class="max-h-[85dvh] gap-0 rounded-t-2xl border-border bg-card pb-[env(safe-area-inset-bottom)] shadow-overlay data-[state=open]:duration-200 data-[state=closed]:duration-150"
        @close-auto-focus="restoreFocus"
      >
        <SheetHeader class="shrink-0 flex-row items-center justify-between gap-3 p-4 pb-2">
          <div class="min-w-0">
            <SheetTitle class="text-base [overflow-wrap:anywhere]">切换{{ group.tabLabel }}页面</SheetTitle>
            <SheetDescription class="mt-1 text-xs">选择要查看的页面</SheetDescription>
          </div>
          <SheetClose as-child>
            <Button variant="ghost" size="icon" class="size-11 shrink-0" aria-label="关闭页面切换">
              <X class="size-5" aria-hidden="true" />
            </Button>
          </SheetClose>
        </SheetHeader>

        <nav class="min-h-0 overflow-y-auto overscroll-contain px-4 pt-1 pb-4" :aria-label="`${group.tabLabel}页面`">
          <router-link
            v-for="item in items"
            :key="item.path"
            :to="item.path"
            :class="cn(
              'mb-1 flex min-h-12 items-center gap-3 rounded-lg px-3 py-3 text-sm font-medium no-underline last:mb-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring',
              activePath === item.path
                ? 'bg-primary-soft text-primary-accent'
                : 'text-foreground hover:bg-muted'
            )"
            :aria-current="activePath === item.path ? 'page' : undefined"
            @click="closeCurrentItem(item.path)"
          >
            <component :is="iconOf(item.icon)" class="size-4 shrink-0" :stroke-width="2" aria-hidden="true" />
            <span class="min-w-0 flex-1 [overflow-wrap:anywhere]">{{ item.label }}</span>
            <Check v-if="activePath === item.path" class="size-4 shrink-0" aria-hidden="true" />
          </router-link>
        </nav>
      </SheetContent>
    </Sheet>
  </nav>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useElementSize, useMediaQuery } from '@vueuse/core'
import { Check, ChevronDown, X } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
import { NAV_GROUPS, scopeOfPath, type NavItem } from '@/navigation'
import { iconOf } from '@/lib/icons'
import { cn } from '@/lib/utils'

const props = defineProps<{
  activePath: string
  subscriptionAggregationEnabled: boolean
}>()

const open = ref(false)
const navContainer = ref<HTMLElement>()
const tabsContainer = ref<HTMLElement>()
const { width } = useElementSize(navContainer)
const isMobileLayout = useMediaQuery('(max-width: 900px)')
const isTablet = useMediaQuery('(min-width: 640px) and (max-width: 900px)')

const group = computed(() => NAV_GROUPS.find(g => g.scope === scopeOfPath(props.activePath)))
const items = computed<NavItem[]>(() =>
  (group.value?.items ?? []).filter(
    item => item.flag !== 'subscriptionAggregation' || props.subscriptionAggregationEnabled
  )
)
const activeItem = computed(() => group.value?.items.find(item => item.path === props.activePath))

// 平铺时为每项的图标、完整名称和内边距预留空间；入口增多自动回到菜单。
const showTabs = computed(() => isTablet.value && width.value >= items.value.length * 112 + (items.value.length - 1) * 8)

watch([() => props.activePath, items, showTabs, isMobileLayout], () => {
  open.value = false
})

// 切到其它页面由路由变更关闭；导航被拦截时保持面板可用。
const closeCurrentItem = (path: string) => {
  if (path === props.activePath) open.value = false
}

const restoreFocus = (event: Event) => {
  if (showTabs.value) {
    event.preventDefault()
    tabsContainer.value?.querySelector<HTMLElement>('[aria-current="page"]')?.focus({ preventScroll: true })
  } else if (!isMobileLayout.value) {
    event.preventDefault()
    document.querySelector<HTMLElement>('#main-content')?.focus({ preventScroll: true })
  }
}
</script>
