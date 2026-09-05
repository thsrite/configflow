<template>
  <SectionCard class="mb-3" :padded="false" role="region" aria-label="配置健康">
    <div class="flex items-center gap-4 px-5 pt-5 pb-4 max-md:px-4">
      <!-- 健康度环：由下方各项的 ok 占比推导，不引入无法计算的指标 -->
      <div class="relative grid size-16 shrink-0 place-items-center">
        <svg viewBox="0 0 44 44" class="size-16 -rotate-90" aria-hidden="true">
          <circle cx="22" cy="22" r="19" fill="none" stroke="var(--border)" stroke-width="4" />
          <circle
            cx="22"
            cy="22"
            r="19"
            fill="none"
            :stroke="ringColor"
            stroke-width="4"
            stroke-linecap="round"
            :stroke-dasharray="CIRCUMFERENCE"
            :stroke-dashoffset="CIRCUMFERENCE * (1 - score / 100)"
            class="transition-[stroke-dashoffset] duration-700 ease-[cubic-bezier(0.32,0.72,0,1)]"
            :style="{ filter: `drop-shadow(0 0 6px ${ringColor})` }"
          />
        </svg>
        <span class="absolute text-[15px] leading-none font-semibold tracking-[-0.02em] num text-foreground">
          {{ score }}
        </span>
      </div>

      <div class="min-w-0 flex-1">
        <h2 class="m-0 text-[14px] font-semibold text-foreground">配置健康</h2>
        <p class="mt-1 mb-0 text-[12px] text-muted-foreground">
          {{ okCount }}/{{ rows.length }} 项正常
        </p>
        <Badge :variant="overall.variant" class="mt-2">{{ overall.text }}</Badge>
      </div>
    </div>

    <Separator />

    <ul class="m-0 list-none px-5 py-1 max-md:px-4">
      <li
        v-for="row in rows"
        :key="row.label"
        class="flex items-center gap-2.5 border-b border-border/50 py-2.5 last:border-b-0"
      >
        <StatusDot :tone="toneOf(row.level)" :pulse="row.level === 'err'" />
        <span class="min-w-0 flex-1 truncate text-[13px] text-muted-foreground">{{ row.label }}</span>
        <span class="num shrink-0 text-[13px] font-medium" :class="textTone(row.level)">
          {{ row.value }}
        </span>
      </li>
    </ul>

    <Separator />

    <footer class="flex items-center gap-2 px-5 py-2.5 max-md:px-4">
      <span class="text-xs text-muted-foreground">上次检测 {{ checkedAt || '—' }}</span>
      <Button variant="ghost" size="sm" class="ml-auto" :disabled="loading" @click="$emit('refresh')">
        <RefreshCw class="size-3.5" :class="loading && 'animate-spin'" />
        立即检测
      </Button>
    </footer>
  </SectionCard>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RefreshCw } from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import SectionCard from '@/components/common/SectionCard.vue'
import StatusDot from '@/components/common/StatusDot.vue'

export type HealthLevel = 'ok' | 'warn' | 'err'

export interface HealthRow {
  label: string
  value: string
  level: HealthLevel
}

const props = defineProps<{
  rows: HealthRow[]
  checkedAt?: string
  loading?: boolean
}>()

defineEmits<{ (e: 'refresh'): void }>()

const CIRCUMFERENCE = 2 * Math.PI * 19

const okCount = computed(() => props.rows.filter(r => r.level === 'ok').length)

/* 健康度 = 正常项占比。warn 记半分，err 不计分，
 * 这样「有告警」与「有异常」不会得到同一个分数。 */
const score = computed(() => {
  if (!props.rows.length) return 0
  const points = props.rows.reduce(
    (sum, r) => sum + (r.level === 'ok' ? 1 : r.level === 'warn' ? 0.5 : 0),
    0
  )
  return Math.round((points / props.rows.length) * 100)
})

// 总体状态取最差的一项，避免出现「有告警但总体良好」的自相矛盾
const overall = computed(() => {
  if (props.rows.some(r => r.level === 'err')) return { variant: 'danger' as const, text: '异常' }
  if (props.rows.some(r => r.level === 'warn')) return { variant: 'warning' as const, text: '需关注' }
  return { variant: 'success' as const, text: '良好' }
})

const ringColor = computed(() =>
  overall.value.text === '异常'
    ? 'var(--destructive-accent)'
    : overall.value.text === '需关注'
      ? 'var(--warning-accent)'
      : 'var(--success-accent)'
)

const toneOf = (level: HealthLevel) =>
  level === 'ok' ? ('success' as const) : level === 'warn' ? ('warning' as const) : ('danger' as const)

const textTone = (level: HealthLevel): string =>
  level === 'ok'
    ? 'text-foreground'
    : level === 'warn'
      ? 'text-warning-accent'
      : 'text-destructive-accent'
</script>
