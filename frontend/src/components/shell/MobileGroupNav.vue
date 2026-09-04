<template>
  <!-- 当前分组内的页面切换：横向分段控件，就地切换不弹层。
       仅移动端出现；桌面由左侧 rail 承担分组导航。 -->
  <nav
    v-if="items.length > 1"
    class="-mx-4 mb-4 hidden gap-2 overflow-x-auto px-4 [scrollbar-width:none] [scroll-snap-type:x_proximity] [&::-webkit-scrollbar]:hidden max-[900px]:flex"
    aria-label="分组内页面"
  >
    <router-link
      v-for="item in items"
      :key="item.path"
      :to="item.path"
      :class="cn(
        'inline-flex h-9 shrink-0 items-center gap-1.5 rounded-md border border-border bg-card px-3.5 text-[13px] font-medium whitespace-nowrap text-muted-foreground no-underline [scroll-snap-align:start] transition-colors',
        activePath === item.path && 'border-primary bg-primary text-primary-foreground'
      )"
      :aria-current="activePath === item.path ? 'page' : undefined"
    >
      <component :is="iconOf(item.icon)" class="size-3.5" :stroke-width="2" aria-hidden="true" />
      <span>{{ item.label }}</span>
    </router-link>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NAV_GROUPS, scopeOfPath, type NavItem } from '@/navigation'
import { iconOf } from '@/lib/icons'
import { cn } from '@/lib/utils'

const props = defineProps<{
  activePath: string
  subscriptionAggregationEnabled: boolean
}>()

const items = computed<NavItem[]>(() => {
  const scope = scopeOfPath(props.activePath)
  const group = NAV_GROUPS.find(g => g.scope === scope)
  if (!group) return []
  return group.items.filter(
    item =>
      item.flag !== 'subscriptionAggregation' || props.subscriptionAggregationEnabled
  )
})
</script>
