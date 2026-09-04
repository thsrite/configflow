<template>
  <!-- 四项 KPI 同处一张卡片、以细分隔线区隔，而非四张互相竞争的独立卡片 -->
  <Card class="mb-4 grid grid-cols-[repeat(auto-fit,minmax(170px,1fr))] gap-0 overflow-hidden py-0 max-[900px]:grid-cols-2">
    <component
      :is="item.route ? 'button' : 'div'"
      v-for="item in items"
      :key="item.label"
      class="flex min-w-0 items-center gap-3 border-l border-border px-5 py-4 text-left transition-colors first:border-l-0 max-[900px]:px-4 max-[900px]:py-3.5 max-[900px]:odd:border-l-0 max-[900px]:[&:nth-child(n+3)]:border-t"
      :class="item.route && 'cursor-pointer hover:bg-accent/60 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none'"
      :type="item.route ? 'button' : undefined"
      @click="item.route && $router.push(item.route)"
    >
      <span
        class="grid size-10 shrink-0 place-items-center rounded-lg"
        :class="iconTone(item.scope)"
        aria-hidden="true"
      >
        <component :is="iconOf(item.icon)" class="size-[18px]" :stroke-width="2" />
      </span>
      <span class="min-w-0 flex-1">
        <span class="block text-[26px] leading-none font-semibold tracking-[-0.03em] tabular-nums text-foreground max-[900px]:text-[22px]">
          {{ item.value }}
        </span>
        <span class="mt-1.5 block truncate text-xs text-muted-foreground">{{ item.label }}</span>
      </span>
    </component>
  </Card>
</template>

<script setup lang="ts">
import { Card } from '@/components/ui/card'
import { iconOf } from '@/lib/icons'

export interface KpiItem {
  label: string
  value: number | string
  icon: string
  scope: 'resource' | 'profile' | 'system'
  route?: string
}

defineProps<{ items: KpiItem[] }>()

/* 作用域用图标底色区分，与侧栏分组色标一致 */
const iconTone = (scope: KpiItem['scope']): string =>
  scope === 'resource'
    ? 'bg-info-soft text-info-accent'
    : scope === 'profile'
      ? 'bg-primary-soft text-primary-accent'
      : 'bg-success-soft text-success-accent'
</script>
