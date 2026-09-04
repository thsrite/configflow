<template>
  <Card class="mb-3 gap-0 py-0" role="region" aria-label="配置健康">
    <header class="flex items-center gap-2 px-5 pt-4 pb-3">
      <h2 class="m-0 text-sm font-semibold text-foreground">配置健康</h2>
      <Badge :variant="overall.variant" class="ml-auto">{{ overall.text }}</Badge>
    </header>

    <Separator />

    <ul class="m-0 list-none px-5 py-1">
      <li
        v-for="row in rows"
        :key="row.label"
        class="flex items-center gap-2.5 border-b border-border py-2.5 last:border-b-0"
      >
        <span class="size-1.5 shrink-0 rounded-full" :class="dotTone(row.level)" aria-hidden="true" />
        <span class="min-w-0 flex-1 truncate text-[13px] text-muted-foreground">{{ row.label }}</span>
        <span class="shrink-0 text-[13px] font-medium tabular-nums" :class="textTone(row.level)">
          {{ row.value }}
        </span>
      </li>
    </ul>

    <Separator />

    <footer class="flex items-center gap-2 px-5 py-2.5">
      <span class="text-xs text-muted-foreground">上次检测 {{ checkedAt || '—' }}</span>
      <Button variant="ghost" size="sm" class="ml-auto" :disabled="loading" @click="$emit('refresh')">
        <RefreshCw class="size-3.5" :class="loading && 'animate-spin'" />
        立即检测
      </Button>
    </footer>
  </Card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RefreshCw } from '@lucide/vue'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'

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

// 总体状态取最差的一项，避免出现「有告警但总体良好」的自相矛盾
const overall = computed(() => {
  if (props.rows.some(r => r.level === 'err')) return { variant: 'danger' as const, text: '异常' }
  if (props.rows.some(r => r.level === 'warn')) return { variant: 'warning' as const, text: '需关注' }
  return { variant: 'success' as const, text: '良好' }
})

const dotTone = (level: HealthLevel): string =>
  level === 'ok' ? 'bg-success-accent' : level === 'warn' ? 'bg-warning-accent' : 'bg-destructive-accent'

const textTone = (level: HealthLevel): string =>
  level === 'ok' ? 'text-foreground' : level === 'warn' ? 'text-warning-accent' : 'text-destructive-accent'
</script>
