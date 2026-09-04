<template>
  <Card class="gap-0 py-0" aria-label="运行状态">
    <header class="flex flex-wrap items-center gap-3 px-5 pt-4 pb-3">
      <h2 class="m-0 text-sm font-semibold text-foreground">运行状态</h2>
      <Tabs
        :model-value="active"
        class="ml-auto min-w-0"
        @update:model-value="$emit('update:active', String($event))"
      >
        <TabsList class="h-8">
          <TabsTrigger v-for="tab in tabs" :key="tab" :value="tab" class="text-xs">
            {{ tab }}
          </TabsTrigger>
        </TabsList>
      </Tabs>
    </header>

    <Separator />

    <p v-if="!rows.length" class="m-0 px-5 py-10 text-center text-[13px] text-muted-foreground">
      暂无运行记录
    </p>

    <!-- 宽表在自身容器内滚动，页面不产生横向滚动 -->
    <div v-else class="overflow-x-auto">
      <table class="w-full border-collapse text-[13px]">
        <thead>
          <tr>
            <th
              v-for="col in ['任务', '详情', '状态', '时间']"
              :key="col"
              scope="col"
              class="border-b border-border px-5 py-2 text-left text-[11px] font-semibold tracking-wide whitespace-nowrap text-muted-foreground uppercase first:pl-5 last:pr-5"
            >
              {{ col }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in rows" :key="i" class="transition-colors hover:bg-accent/40">
            <td class="border-b border-border px-5 py-2.5 align-top font-medium whitespace-nowrap text-foreground">
              <span class="mr-2 inline-block size-1.5 rounded-full align-middle" :class="dotTone(row.level)" aria-hidden="true" />
              {{ row.task }}
            </td>
            <td class="min-w-[220px] border-b border-border px-5 py-2.5 align-top text-muted-foreground">
              {{ row.detail }}
            </td>
            <td class="border-b border-border px-5 py-2.5 align-top">
              <Badge :variant="badgeTone(row.level)">{{ row.status }}</Badge>
            </td>
            <td class="border-b border-border px-5 py-2.5 align-top font-mono text-xs tabular-nums whitespace-nowrap text-muted-foreground">
              {{ row.time }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <footer v-if="rows.length" class="border-t border-border px-5 py-2.5 text-xs text-muted-foreground">
      共 {{ rows.length }} 条
    </footer>
  </Card>
</template>

<script setup lang="ts">
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

export interface ActivityRow {
  task: string
  detail: string
  status: string
  level: 'ok' | 'warn' | 'err'
  time: string
}

defineProps<{ rows: ActivityRow[]; tabs: string[]; active: string }>()
defineEmits<{ (e: 'update:active', tab: string): void }>()

const dotTone = (level: ActivityRow['level']): string =>
  level === 'ok' ? 'bg-success-accent' : level === 'warn' ? 'bg-warning-accent' : 'bg-destructive-accent'

const badgeTone = (level: ActivityRow['level']) =>
  level === 'ok' ? ('success' as const) : level === 'warn' ? ('warning' as const) : ('danger' as const)
</script>
