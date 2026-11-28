<template>
  <aside
    :data-collapsed="collapsed"
    :class="cn(
      'sticky top-0 z-10 flex h-dvh shrink-0 flex-col border-r border-border/60 bg-background/55 px-3 pt-[calc(env(safe-area-inset-top)+12px)] pb-4 backdrop-blur-xl transition-[width] duration-200 ease-(--cf-ease) [[data-motion=off]_&]:transition-none motion-reduce:transition-none',
      collapsed ? 'w-(--cf-rail-collapsed-w)' : 'w-(--cf-rail-w)'
    )"
  >
    <div :class="cn('mb-3 flex shrink-0 items-center gap-1', collapsed && 'flex-col')">
    <router-link
      to="/dashboard"
      :class="cn('group flex min-h-11 min-w-0 items-center gap-2 text-foreground no-underline', collapsed ? 'w-11 justify-center' : 'flex-1 pl-1')"
      aria-label="ConfigFlow 首页"
    >
      <BrandMark class="size-7 shrink-0 text-primary" :busy="busy" />
      <span v-if="!collapsed" class="min-w-0 leading-none">
        <span class="font-display block text-[19px] font-semibold tracking-[-0.01em]">ConfigFlow</span>
        <span class="mt-1 block font-mono text-[9.5px] tracking-[0.16em] text-muted-foreground uppercase">
          Proxy · Flow
        </span>
      </span>
    </router-link>
      <button
        type="button"
        class="flex size-11 shrink-0 cursor-pointer items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        :aria-label="collapsed ? '展开导航' : '收起导航'"
        :title="collapsed ? '展开导航' : '收起导航'"
        :aria-expanded="!collapsed"
        :aria-controls="navId"
        @click="toggle"
      >
        <component :is="collapsed ? PanelLeftOpen : PanelLeftClose" class="size-4" aria-hidden="true" />
      </button>
    </div>

    <ProfileSwitcher variant="rail" :collapsed="collapsed" class="mb-3 shrink-0" />

    <TooltipProvider :delay-duration="200">
    <nav ref="navigation" :id="navId" class="-mx-1 flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto px-1 [scrollbar-width:none]" aria-label="主导航">
    <template v-for="group in groups" :key="group.scope">
      <div
        v-if="group.title"
        :class="cn(
          'shrink-0 text-muted-foreground',
          collapsed ? 'mx-3 my-2 border-t border-border/60' : 'mt-5 mb-1.5 flex items-center gap-2 px-2.5 font-mono text-[10px] tracking-[0.14em] uppercase first:mt-1'
        )"
      >
        <!-- 作用域用色标 + 分组标题共同表意，不单靠颜色 -->
        <span v-if="!collapsed" class="size-1.5 shrink-0 rounded-full" :class="markClass(group.scope)" aria-hidden="true" />
        <span :class="collapsed ? 'sr-only' : 'truncate'">
          {{ group.title }}<span v-if="group.scope === 'profile'" class="normal-case tracking-normal"> · {{ profileName }}</span>
        </span>
        <span v-if="!collapsed" class="h-px flex-1 bg-border/60" aria-hidden="true" />
      </div>

      <Tooltip v-for="item in visibleItems(group)" :key="item.path" :disabled="!collapsed">
      <TooltipTrigger as-child>
      <router-link
        :to="item.path"
        :class="cn(
          'group relative flex shrink-0 items-center rounded-lg text-[13px] font-medium text-muted-foreground no-underline transition-colors duration-200 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none',
          collapsed ? 'min-h-11 justify-center' : 'min-h-9.5 gap-2.5 px-2.5 [@media(pointer:coarse)]:min-h-11',
          activePath === item.path
            ? 'text-foreground'
            : 'hover:bg-accent/60 hover:text-foreground'
        )"
        :aria-label="item.label"
        :aria-current="activePath === item.path ? 'page' : undefined"
      >
        <!-- 选中态：高亮块在切换时平滑滑动到新项 -->
        <span
          v-if="activePath === item.path"
          data-nav-highlight
          class="absolute inset-0 rounded-lg border border-border-strong bg-secondary"
          aria-hidden="true"
        />
        <!-- 选中态除底色外再加一条左侧指示条，弱视条件下也能分辨 -->
        <span
          v-if="activePath === item.path"
          class="absolute top-1/2 -left-1 h-5 w-[3px] -translate-y-1/2 rounded-r-full bg-primary-accent"
          aria-hidden="true"
        />
        <component
          :is="iconOf(item.icon)"
          :class="cn(
            'relative size-4 shrink-0 transition-colors',
            activePath === item.path ? 'text-primary-accent' : 'group-hover:text-foreground'
          )"
          :stroke-width="2"
          aria-hidden="true"
        />
        <span :class="collapsed ? 'sr-only' : 'relative truncate'">{{ item.label }}</span>
      </router-link>
      </TooltipTrigger>
      <TooltipContent v-if="collapsed" side="right" :side-offset="12">{{ item.label }}</TooltipContent>
      </Tooltip>
    </template>

    </nav>

    <div :class="cn('mt-3 grid shrink-0 gap-2 border-t border-border/60 pt-3 text-[11.5px] text-muted-foreground', !collapsed && 'px-1.5')">
      <!-- 下行吞吐：会话内采样的真实曲线，没有 Agent 上报时不显示 -->
      <div v-if="downRate && !collapsed" class="flex items-center gap-2">
        <span>入</span>
        <Sparkline :data="downHistory" class="h-[22px] min-w-0 flex-1" />
        <b class="min-w-[62px] text-right font-mono font-medium text-foreground/80">{{ downRate }}</b>
      </div>
      <Tooltip :disabled="!collapsed">
      <TooltipTrigger as-child>
      <router-link
        to="/agents"
        :class="cn('flex rounded-md text-muted-foreground no-underline hover:text-foreground', collapsed ? 'min-h-11 flex-col items-center justify-center gap-1 hover:bg-accent/60' : 'items-center gap-2')"
        :aria-label="`${onlineCount} / ${boundAgents.length} Agent 在线`"
      >
        <span v-if="!collapsed" :class="cn('size-1.5 rounded-full', onlineCount ? 'live-dot' : 'bg-muted-foreground/60')" aria-hidden="true" />
        <RadioTower v-else class="size-4" :class="onlineCount ? 'text-primary-accent' : ''" aria-hidden="true" />
        <span :class="collapsed && 'font-mono text-[10px] leading-none'">{{ onlineCount }} / {{ boundAgents.length }}<template v-if="!collapsed"> Agent 在线</template></span>
      </router-link>
      </TooltipTrigger>
      <TooltipContent v-if="collapsed" side="right" :side-offset="12">{{ onlineCount }} / {{ boundAgents.length }} Agent 在线</TooltipContent>
      </Tooltip>
      <div v-if="!collapsed" class="flex items-center font-mono text-[10.5px]">
        <span class="tracking-[0.08em] uppercase">{{ metaKeyLabel }}K 快速跳转</span>
        <span v-if="version" class="ml-auto">{{ version }}</span>
      </div>
    </div>
    </TooltipProvider>
  </aside>
</template>

<script setup lang="ts">
import { PanelLeftClose, PanelLeftOpen, RadioTower } from '@lucide/vue'
import BrandMark from '@/components/common/BrandMark.vue'
import ProfileSwitcher from '@/components/ProfileSwitcher.vue'
import Sparkline from '@/components/common/Sparkline.vue'
import { formatRate, useLive } from '@/stores/live'
import { computed, useId } from 'vue'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useRailCollapse } from '@/composables/useRailCollapse'
import { NAV_GROUPS, type NavGroup, type NavItem, type NavScope } from '@/navigation'
import { iconOf } from '@/lib/icons'
import { useNavHighlight } from '@/composables/useNavHighlight'
import { cn } from '@/lib/utils'

const props = defineProps<{
  activePath: string
  profileName: string
  subscriptionAggregationEnabled: boolean
  version?: string
  /** 全局有任务进行中时品牌标记旋转 */
  busy?: boolean
}>()

const navigation = useNavHighlight(() => props.activePath)
const metaKeyLabel = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl+'
const { collapsed, toggle } = useRailCollapse()
const navId = `rail-navigation-${useId()}`

const { throughput, downHistory, onlineCount, boundAgents } = useLive()
const downRate = computed(() => (throughput.value ? formatRate(throughput.value.down).join(' ') : ''))


const groups = NAV_GROUPS

const visibleItems = (group: NavGroup): NavItem[] =>
  group.items.filter(
    item => item.flag !== 'subscriptionAggregation' || props.subscriptionAggregationEnabled
  )

const markClass = (scope: NavScope): string =>
  scope === 'resource'
    ? 'bg-info-accent shadow-[0_0_8px_var(--info-accent)]'
    : scope === 'profile'
      ? 'bg-primary-accent shadow-[0_0_8px_var(--primary-accent)]'
      : 'bg-muted-foreground'
</script>
