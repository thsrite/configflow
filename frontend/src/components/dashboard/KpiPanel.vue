<template>
  <div
    role="region"
    aria-label="关键指标"
    class="mb-4 grid grid-cols-4 gap-3 max-[1100px]:grid-cols-2 max-[560px]:grid-cols-2"
  >
    <Motion
      v-for="(item, index) in items"
      :key="item.label"
      :as="item.route ? 'button' : 'div'"
      :type="item.route ? 'button' : undefined"
      v-bind="listItem(index)"
      class="m-0 min-w-0 appearance-none border-0 bg-transparent p-0 text-left"
      :class="item.route && 'cursor-pointer rounded-xl focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none'"
      @click="item.route && $router.push(item.route)"
    >
      <StatTile
        :label="item.label"
        :value="Number(item.value) || 0"
        :icon="iconOf(item.icon)"
        :tone="toneOf(item.scope)"
      >
        <span v-if="item.route" class="inline-flex items-center gap-1 transition-colors hover:text-foreground">
          查看详情
          <ChevronRight class="size-3" aria-hidden="true" />
        </span>
      </StatTile>
    </Motion>
  </div>
</template>

<script setup lang="ts">
import { Motion } from 'motion-v'
import { ChevronRight } from '@lucide/vue'
import StatTile from '@/components/common/StatTile.vue'
import { iconOf } from '@/lib/icons'
import { listItem } from '@/lib/motion'

export interface KpiItem {
  label: string
  value: number | string
  icon: string
  scope: 'resource' | 'profile' | 'system'
  route?: string
}

defineProps<{ items: KpiItem[] }>()

/* 作用域用色调区分，与侧栏分组色标一致 */
const toneOf = (scope: KpiItem['scope']) =>
  scope === 'resource' ? ('info' as const) : scope === 'profile' ? ('primary' as const) : ('success' as const)
</script>
