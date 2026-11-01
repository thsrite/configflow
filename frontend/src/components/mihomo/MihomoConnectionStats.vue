<template>
  <SectionCard title="连接统计" :padded="false" description="当前活动连接的汇总，连接关闭后不再计入。" :help="HELP">
    <template #actions>
      <Segmented v-model="by" :options="OVERVIEW_AGGREGATES" label="聚合方式" class="max-w-full overflow-x-auto" />
    </template>
    <div class="grid grid-cols-5 gap-3 px-5 pb-4 max-md:grid-cols-3 max-md:gap-2 max-md:px-4 max-sm:px-3">
      <div v-for="tile in tiles" :key="tile.label" class="min-w-0 rounded-xl bg-secondary/60 px-3 py-2.5">
        <div class="truncate text-[12px] text-muted-foreground">{{ tile.label }}</div>
        <div class="num mt-0.5 truncate text-[16px]">{{ tile.value }}</div>
      </div>
    </div>
    <LoadingRows v-if="loading" :rows="4" />
    <EmptyState v-else-if="!rows.length" :icon="Cable" title="没有活动连接" description="有流量经过 Mihomo 时自动显示。" />
    <div v-else class="cf-table-wrap max-h-[420px] overflow-y-auto overscroll-contain border-t border-border" tabindex="0" role="region" :aria-label="`连接统计，${column}`">
      <table class="cf-table stats-table">
        <thead>
          <tr>
            <th scope="col">{{ column }}</th>
            <th v-for="item in SORTS" :key="item.key" scope="col" class="text-right" :aria-sort="sort === item.key ? 'descending' : 'none'">
              <button type="button" class="stats-sort" :data-active="sort === item.key" :aria-label="`按${item.label}排序`" @click="sort = item.key">
                {{ item.label }}<ArrowDown v-if="sort === item.key" class="size-3" />
              </button>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in sorted" :key="row.key">
            <td class="stats-name">
              <span class="block truncate" :class="nameOf(row.key).mono ? 'font-mono text-[12.5px]' : ''" :title="nameOf(row.key).title">{{ nameOf(row.key).label }}</span>
              <span v-if="nameOf(row.key).detail" class="block truncate font-mono text-[11px] text-muted-foreground">{{ nameOf(row.key).detail }}</span>
            </td>
            <td class="num text-right">{{ formatBytes(row.download) }}</td>
            <td class="num text-right">{{ formatBytes(row.upload) }}</td>
            <td class="num text-right">{{ formatBytes(row.download + row.upload) }}</td>
            <td class="num text-right" :class="row.downSpeed + row.upSpeed ? '' : 'text-muted-foreground'">{{ speedText(row.downSpeed + row.upSpeed) }}</td>
            <td class="num text-right">{{ row.count }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </SectionCard>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ArrowDown, Cable } from '@lucide/vue'
import SectionCard from '@/components/common/SectionCard.vue'
import Segmented from '@/components/common/Segmented.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import { formatBytes } from '@/lib/format'
import { deviceNote } from '@/lib/device-notes'
import { aggregateConnections, isInnerSource, OVERVIEW_AGGREGATES, type OverviewAggregate, type OverviewStatRow } from '@/lib/mihomo-overview'
import type { MihomoConnection } from '@/types/mihomo'

const HELP = '流量是每条活动连接建立以来的累计字节，速度来自最近两次采样之差。目标按主域名归并（如 a.b.example.com 计入 example.com）。只统计打开本页期间仍在进行的连接，不是历史账单。'
const STORAGE_KEY = 'configflow:mihomo-overview:aggregate'
type SortKey = 'download' | 'upload' | 'total' | 'speed' | 'count'
const SORTS: Array<{ key: SortKey; label: string }> = [
  { key: 'download', label: '下载' },
  { key: 'upload', label: '上传' },
  { key: 'total', label: '总流量' },
  { key: 'speed', label: '速度' },
  { key: 'count', label: '连接数' }
]

const props = defineProps<{
  connections: MihomoConnection[]
  speeds: Map<string, { up: number; down: number }>
  notes: Record<string, string>
  loading?: boolean
}>()

const readBy = (): OverviewAggregate => {
  try {
    const saved = window.localStorage.getItem(STORAGE_KEY)
    if (OVERVIEW_AGGREGATES.some(item => item.value === saved)) return saved as OverviewAggregate
  } catch { /* 存储不可用时用默认值 */ }
  return 'source'
}
const by = ref<OverviewAggregate>(readBy())
watch(by, value => { try { window.localStorage.setItem(STORAGE_KEY, value) } catch { /* 只在本次会话内生效 */ } })
const sort = ref<SortKey>('download')
const column = computed(() => OVERVIEW_AGGREGATES.find(item => item.value === by.value)!.column)

const rows = computed(() => aggregateConnections(props.connections, by.value, props.speeds))
const metric: Record<SortKey, (row: OverviewStatRow) => number> = {
  download: row => row.download,
  upload: row => row.upload,
  total: row => row.download + row.upload,
  speed: row => row.downSpeed + row.upSpeed,
  count: row => row.count
}
const sorted = computed(() => [...rows.value].sort((a, b) => metric[sort.value](b) - metric[sort.value](a) || a.key.localeCompare(b.key)))

const tiles = computed(() => {
  const sum = rows.value.reduce((acc, row) => ({ down: acc.down + row.download, up: acc.up + row.upload, count: acc.count + row.count }), { down: 0, up: 0, count: 0 })
  return [
    { label: column.value, value: String(rows.value.length) },
    { label: '总流量', value: formatBytes(sum.down + sum.up) },
    { label: '下载', value: formatBytes(sum.down) },
    { label: '上传', value: formatBytes(sum.up) },
    { label: '连接数', value: String(sum.count) }
  ]
})

/** 地址和域名用等宽字体，设备备注、节点和策略组名称用正文字体 */
function nameOf(key: string): { label: string; detail?: string; title: string; mono: boolean } {
  if (by.value === 'destination') return { label: key, title: key, mono: true }
  if (by.value !== 'source') return { label: key, title: key, mono: false }
  if (isInnerSource(key)) return { label: 'Inner', detail: 'Mihomo 自身发起', title: 'Mihomo 自身发起的连接，例如 DNS 上游和测速', mono: false }
  const note = deviceNote(props.notes, key)
  return note ? { label: note, detail: key, title: `${note} · ${key}`, mono: false } : { label: key, title: key, mono: true }
}

const speedText = (bytesPerSecond: number) => bytesPerSecond > 0 ? formatBytes(bytesPerSecond) + '/s' : '—'
</script>

<style scoped>
.stats-table { min-width: 640px; }
.stats-table th { position: sticky; top: 0; z-index: 1; background: var(--card); white-space: nowrap; }
.stats-table td { white-space: nowrap; }
.stats-name { max-width: 280px; }
.stats-sort { display: inline-flex; min-height: 28px; align-items: center; gap: 2px; border-radius: var(--radius-sm); color: inherit; }
.stats-sort[data-active='true'], .stats-sort:hover { color: var(--primary-accent); }
.stats-sort:focus-visible { outline: 2px solid var(--ring); outline-offset: -2px; }
@media (pointer: coarse) {
  .stats-sort { min-height: 44px; }
}
</style>
