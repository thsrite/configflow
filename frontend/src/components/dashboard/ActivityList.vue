<template>
  <SectionCard :padded="false" role="region" aria-label="运行状态">
    <header class="flex flex-wrap items-center gap-3 px-5 pt-5 pb-3 max-md:px-4">
      <h2 class="m-0 flex items-center gap-2 text-[14px] font-semibold text-foreground">
        <Activity class="size-4 text-primary-accent" :stroke-width="2.2" aria-hidden="true" />
        运行状态
      </h2>
      <Tabs
        :model-value="active"
        class="ml-auto min-w-0"
        @update:model-value="$emit('update:active', String($event))"
      >
        <TabsList class="h-8 bg-background/50">
          <TabsTrigger v-for="tab in tabs" :key="tab" :value="tab" class="text-xs">
            {{ tab }}
          </TabsTrigger>
        </TabsList>
      </Tabs>
    </header>

    <Separator />

    <EmptyState
      v-if="!rows.length"
      :icon="Activity"
      title="暂无运行记录"
      description="系统产生日志后，最近的任务会在这里按时间顺序出现。"
    />

    <!-- 时间线：左侧一条主干 + 状态节点，比表格更贴合「按时间发生的事」 -->
    <ol v-else class="m-0 list-none px-5 py-3 max-md:px-4">
      <Motion
        v-for="(row, i) in rows"
        :key="`${row.time}-${i}`"
        as="li"
        v-bind="listItem(i)"
        class="group relative grid grid-cols-[14px_minmax(0,1fr)_auto] items-start gap-x-3 py-2.5"
      >
        <!-- 主干线：最后一项不再向下延伸 -->
        <span
          v-if="i < rows.length - 1"
          class="absolute top-6 bottom-0 left-[6px] w-px bg-border/70"
          aria-hidden="true"
        />
        <span
          class="relative mt-1.5 size-3 rounded-full border-2 border-background"
          :class="dotTone(row.level)"
          aria-hidden="true"
        />

        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-[13px] font-medium text-foreground">{{ row.task }}</span>
            <Badge :variant="badgeTone(row.level)" class="h-5 px-1.5 text-[10.5px]">
              {{ row.status }}
            </Badge>
          </div>
          <p class="mt-1 mb-0 text-[12.5px] leading-relaxed break-words text-muted-foreground">
            {{ row.detail }}
          </p>
        </div>

        <span class="num mt-0.5 shrink-0 font-mono text-[11.5px] whitespace-nowrap text-muted-foreground">
          {{ row.time }}
        </span>
      </Motion>
    </ol>

    <template v-if="rows.length">
      <Separator />
      <footer class="px-5 py-2.5 text-xs text-muted-foreground max-md:px-4">共 {{ rows.length }} 条</footer>
    </template>
  </SectionCard>
</template>

<script setup lang="ts">
import { Motion } from 'motion-v'
import { Activity } from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import EmptyState from '@/components/common/EmptyState.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import { listItem } from '@/lib/motion'

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
  level === 'ok'
    ? 'bg-success-accent shadow-[0_0_8px_var(--success-accent)]'
    : level === 'warn'
      ? 'bg-warning-accent shadow-[0_0_8px_var(--warning-accent)]'
      : 'bg-destructive-accent shadow-[0_0_8px_var(--destructive-accent)]'

const badgeTone = (level: ActivityRow['level']) =>
  level === 'ok' ? ('success' as const) : level === 'warn' ? ('warning' as const) : ('danger' as const)
</script>
