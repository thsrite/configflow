<template>
  <nav
    class="relative z-10 flex w-(--cf-rail-w) shrink-0 flex-col gap-0.5 overflow-y-auto border-r border-border/50 bg-background/40 px-3 pt-4 pb-6 backdrop-blur-xl"
    aria-label="主导航"
  >
    <template v-for="group in groups" :key="group.scope">
      <div
        v-if="group.title"
        class="mt-6 mb-2 flex items-center gap-2 px-2.5 text-[10.5px] font-semibold tracking-[0.13em] text-muted-foreground uppercase first:mt-1"
      >
        <!-- 作用域用色标 + 分组标题共同表意，不单靠颜色 -->
        <span class="size-1.5 shrink-0 rounded-full" :class="markClass(group.scope)" aria-hidden="true" />
        <span class="truncate">{{ groupTitle(group) }}</span>
        <span class="h-px flex-1 bg-border/60" aria-hidden="true" />
      </div>

      <router-link
        v-for="item in visibleItems(group)"
        :key="item.path"
        :to="item.path"
        :class="cn(
          'group relative flex min-h-9.5 items-center gap-2.5 rounded-lg px-2.5 text-[13px] font-medium text-muted-foreground no-underline transition-colors duration-200 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none',
          activePath === item.path
            ? 'text-foreground'
            : 'hover:bg-accent/60 hover:text-foreground'
        )"
        :aria-current="activePath === item.path ? 'page' : undefined"
      >
        <!-- 选中态：共享 layoutId 的高亮块在切换时平滑滑动到新项 -->
        <Motion
          v-if="activePath === item.path"
          layout-id="rail-active"
          class="absolute inset-0 rounded-lg border border-primary-accent/25 bg-primary-soft/45"
          :transition="SPRING"
          aria-hidden="true"
        />
        <!-- 选中态除底色外再加一条左侧指示条，弱视条件下也能分辨 -->
        <span
          v-if="activePath === item.path"
          class="absolute top-1/2 left-0 h-5 w-0.5 -translate-y-1/2 rounded-r-full bg-primary-accent shadow-[0_0_10px_var(--primary-accent)]"
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
        <span class="relative truncate">{{ item.label }}</span>
      </router-link>
    </template>

    <div class="mt-auto px-2.5 pt-6">
      <p class="m-0 font-mono text-[10px] tracking-[0.08em] text-muted-foreground/70 uppercase">
        ⌘K 快速跳转
      </p>
    </div>
  </nav>
</template>

<script setup lang="ts">
import { Motion } from 'motion-v'
import { NAV_GROUPS, type NavGroup, type NavItem, type NavScope } from '@/navigation'
import { iconOf } from '@/lib/icons'
import { SPRING } from '@/lib/motion'
import { cn } from '@/lib/utils'

const props = defineProps<{
  activePath: string
  profileName: string
  subscriptionAggregationEnabled: boolean
}>()

const groups = NAV_GROUPS

const groupTitle = (group: NavGroup): string =>
  group.scope === 'profile' ? `当前配置 · ${props.profileName}` : group.title || ''

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
