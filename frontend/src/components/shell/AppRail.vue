<template>
  <nav class="cf-rail flex w-(--cf-rail-w) shrink-0 flex-col gap-0.5 overflow-y-auto border-r border-border bg-background px-3 pt-3 pb-6" aria-label="主导航">
    <template v-for="group in groups" :key="group.scope">
      <div
        v-if="group.title"
        class="mt-5 mb-1.5 flex items-center gap-2 px-2 text-[11px] font-semibold tracking-[0.04em] text-muted-foreground first:mt-1"
      >
        <!-- 作用域用色标 + 分组标题共同表意，不单靠颜色 -->
        <span class="size-1.5 shrink-0 rounded-[2px]" :class="markClass(group.scope)" aria-hidden="true" />
        <span class="truncate">{{ groupTitle(group) }}</span>
      </div>

      <router-link
        v-for="item in visibleItems(group)"
        :key="item.path"
        :to="item.path"
        class="group relative flex min-h-9 items-center gap-2.5 rounded-md px-2.5 text-[13px] font-medium text-muted-foreground no-underline transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
        :class="activePath === item.path && 'bg-accent font-semibold text-foreground'"
        :aria-current="activePath === item.path ? 'page' : undefined"
      >
        <!-- 选中态除底色外再加一条左侧指示条，弱视条件下也能分辨 -->
        <span
          v-if="activePath === item.path"
          class="absolute left-0 h-4.5 w-0.5 rounded-r-full bg-primary-accent"
          aria-hidden="true"
        />
        <component :is="iconOf(item.icon)" class="size-4 shrink-0" :stroke-width="2" aria-hidden="true" />
        <span class="truncate">{{ item.label }}</span>
      </router-link>
    </template>
  </nav>
</template>

<script setup lang="ts">
import { NAV_GROUPS, type NavGroup, type NavItem, type NavScope } from '@/navigation'
import { iconOf } from '@/lib/icons'

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
    ? 'bg-info-accent'
    : scope === 'profile'
      ? 'bg-primary-accent'
      : 'bg-muted-foreground'
</script>
