<template>
  <SectionCard class="min-w-0" :title="title" help="按请求次数排列 · 前 10 项" :padded="false">
    <div class="cf-table-wrap h-72 max-w-full overflow-y-auto sm:h-96 lg:h-[32rem]" tabindex="0" role="region" :aria-label="title">
      <EmptyState v-if="!entries.length" class="h-full" :title="emptyTitle" :description="emptyDescription" />
      <table v-else class="cf-table table-fixed">
        <caption class="cf-sr">{{ title }}，按请求次数从高到低排列</caption>
        <thead><tr><th class="w-12"><span class="cf-sr">排名</span></th><th>{{ nameLabel }}</th><th class="w-24 text-right">请求次数</th></tr></thead>
        <tbody>
          <tr v-for="(entry, index) in entries.slice(0, 10)" :key="entry.name">
            <td class="num text-muted-foreground">{{ index + 1 }}</td>
            <td class="break-all text-xs">
              <span v-if="entry.note" class="mb-0.5 block font-sans">{{ entry.note }}</span>
              <span :class="['font-mono', { 'text-muted-foreground': entry.note }]">{{ entry.name }}</span>
            </td>
            <td class="num break-all text-right">{{ formatMonitorNumber(entry.count) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </SectionCard>
</template>
<script setup lang="ts">
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import { formatMonitorNumber } from '@/types/oxidnsMonitor'

defineProps<{
  title: string
  nameLabel: string
  entries: Array<{ name: string; count: number; note?: string }>
  emptyTitle: string
  emptyDescription: string
}>()
</script>

<style scoped>
.cf-table th.text-right {
  text-align: right;
}
</style>
