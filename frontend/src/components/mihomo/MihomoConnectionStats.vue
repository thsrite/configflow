<template>
  <SectionCard title="连接统计" :padded="false" :description="description" :help="HELP">
    <template #actions>
      <Segmented v-if="history?.enabled" v-model="range" :options="TRAFFIC_RANGES" label="统计范围" class="max-w-full overflow-x-auto" />
      <Segmented v-model="by" :options="OVERVIEW_AGGREGATES" label="聚合方式" class="max-w-full overflow-x-auto" />
    </template>

    <div v-if="agentId" class="mx-5 mb-4 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border border-border px-3 py-2 text-[12.5px] max-md:mx-4 max-sm:mx-3">
      <label class="flex min-h-11 cursor-pointer items-center gap-2 font-medium" :class="!canToggle && 'cursor-not-allowed opacity-60'">
        <Switch :model-value="!!history?.enabled" :disabled="!canToggle" aria-label="后台记录流量" @update:model-value="toggle(!!$event)" />后台记录
      </label>
      <template v-if="history?.enabled">
        <span class="text-muted-foreground">记录中<template v-if="history.since"> · 从 {{ sinceText }} 开始</template></span>
        <span class="flex items-center gap-1.5 text-muted-foreground">
          保留
          <Select :model-value="String(history.retention_days)" :disabled="saving" @update:model-value="changeRetention(Number($event))">
            <SelectTrigger class="h-8 w-24 min-w-0 max-sm:min-h-11" aria-label="保留时长"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem v-for="days in history.retention_choices" :key="days" :value="String(days)">{{ days }} 天</SelectItem>
            </SelectContent>
          </Select>
        </span>
        <Button variant="ghost" size="sm" class="ml-auto max-sm:min-h-11" :disabled="saving" @click="clearHistory">
          <Trash2 class="size-3.5" />清空记录
        </Button>
      </template>
      <span v-else-if="history && !history.supported" class="text-warning-accent">需要将 Agent 更新到 {{ history.min_version }} 或更新版本。</span>
      <span v-else-if="historyError" role="alert" class="text-warning-accent">{{ historyError }}</span>
      <span v-else class="text-muted-foreground">开启后 Agent 在后台持续记录，不打开页面也能统计；可查看最近 24 小时到 30 天。</span>
    </div>

    <div class="grid grid-cols-5 gap-3 px-5 pb-4 max-md:grid-cols-3 max-md:gap-2 max-md:px-4 max-sm:px-3">
      <div v-for="tile in tiles" :key="tile.label" class="min-w-0 rounded-xl bg-secondary/60 px-3 py-2.5">
        <div class="truncate text-[12px] text-muted-foreground">{{ tile.label }}</div>
        <div class="num mt-0.5 truncate text-[16px]">{{ tile.value }}</div>
      </div>
    </div>
    <div v-if="!live && trend.length" class="px-5 pb-4 max-md:px-4 max-sm:px-3">
      <div class="mb-1 flex items-center justify-between text-[11.5px] text-muted-foreground">
        <span>流量趋势（每{{ data?.step === 86400 ? '天' : '小时' }}）</span>
        <span class="num">峰值 {{ formatBytes(Math.max(...trend)) }}</span>
      </div>
      <Sparkline class="h-12 w-full" :data="trend" color="--chart-download" />
    </div>
    <p v-if="!live && historyError && history?.enabled" role="alert" class="mx-5 mt-0 mb-3 text-[12.5px] text-warning-accent max-md:mx-4 max-sm:mx-3">{{ historyError }}</p>

    <LoadingRows v-if="live ? loading : !data" :rows="4" />
    <EmptyState v-else-if="!rows.length" :icon="Cable" :title="live ? '没有活动连接' : '这段时间还没有记录'" :description="live ? '有流量经过 Mihomo 时自动显示。' : 'Agent 每 5 分钟上报一次，刚开启时稍等几分钟。'" />
    <div v-else class="cf-table-wrap max-h-[420px] overflow-y-auto overscroll-contain border-t border-border" tabindex="0" role="region" :aria-label="`连接统计，${column}`">
      <table class="cf-table stats-table">
        <thead>
          <tr>
            <th scope="col">{{ column }}</th>
            <th v-for="item in sorts" :key="item.key" scope="col" class="text-right" :aria-sort="sort === item.key ? 'descending' : 'none'">
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
              <span v-if="nameOf(row.key).detail" class="block truncate text-[11px] text-muted-foreground" :class="nameOf(row.key).detailMono && 'font-mono'">{{ nameOf(row.key).detail }}</span>
            </td>
            <td class="num text-right">{{ formatBytes(row.download) }}</td>
            <td class="num text-right">{{ formatBytes(row.upload) }}</td>
            <td class="num text-right">{{ formatBytes(row.download + row.upload) }}</td>
            <td v-if="live" class="num text-right" :class="row.speed ? '' : 'text-muted-foreground'">{{ speedText(row.speed) }}</td>
            <td class="num text-right">{{ row.count }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="!live && data?.omitted" class="m-0 border-t border-border px-5 py-2 text-[12px] text-muted-foreground">另有 {{ data.omitted }} 项流量较小，未列出。</p>
    </div>
  </SectionCard>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { ArrowDown, Cable, Trash2 } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import SectionCard from '@/components/common/SectionCard.vue'
import Segmented from '@/components/common/Segmented.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import Sparkline from '@/components/common/Sparkline.vue'
import { trafficHistoryApi } from '@/api'
import { choose, confirmDanger, notify } from '@/lib/feedback'
import { formatBytes } from '@/lib/format'
import { deviceNote } from '@/lib/device-notes'
import {
  aggregateConnections, isInnerSource, OTHER_TARGET_KEY, OVERVIEW_AGGREGATES, TRAFFIC_RANGES, UNATTRIBUTED_KEY,
  type OverviewAggregate, type TrafficHistoryData, type TrafficRange
} from '@/lib/mihomo-overview'
import { panelError, type MihomoConnection } from '@/types/mihomo'

const HELP = '「实时」汇总当前活动连接：流量是每条连接建立以来的累计字节，速度来自最近两次采样之差，连接关闭后不再计入。开启后台记录后，Agent 持续采样并每 5 分钟上报，服务端按「定时任务 → 监控历史保存」的间隔写入数据库；24 小时到 30 天的范围包括已关闭的连接。目标按主域名归并；「未归属」是两次采样之间开始又结束的短连接，只知道总量。超过 7 天的数据按天保存（目标超过 2 天按天、每台设备每天保留流量最大的 100 个），范围边界按整天计。'
const STORAGE_KEY = 'configflow:mihomo-overview:aggregate'
const RANGE_KEY = 'configflow:mihomo-overview:range'
const HISTORY_POLL_MS = 60_000
type SortKey = 'download' | 'upload' | 'total' | 'speed' | 'count'

const props = defineProps<{
  connections: MihomoConnection[]
  speeds: Map<string, { up: number; down: number }>
  notes: Record<string, string>
  loading?: boolean
  /** 有 Agent 时显示后台记录的开关与历史范围 */
  agentId?: string
  active?: boolean
}>()

function read<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  try {
    const saved = window.localStorage.getItem(key)
    if (allowed.includes(saved as T)) return saved as T
  } catch { /* 存储不可用时用默认值 */ }
  return fallback
}
function remember(key: string, value: string) {
  try { window.localStorage.setItem(key, value) } catch { /* 只在本次会话内生效 */ }
}
const by = ref<OverviewAggregate>(read(STORAGE_KEY, OVERVIEW_AGGREGATES.map(item => item.value), 'source'))
watch(by, value => remember(STORAGE_KEY, value))
const range = ref<TrafficRange>(read(RANGE_KEY, TRAFFIC_RANGES.map(item => item.value), 'live'))
watch(range, value => remember(RANGE_KEY, value))
const sort = ref<SortKey>('download')
const column = computed(() => OVERVIEW_AGGREGATES.find(item => item.value === by.value)!.column)

const history = ref<TrafficHistoryData | null>(null)
const data = ref<TrafficHistoryData | null>(null)
const historyError = ref('')
const saving = ref(false)
// 没开记录时只有实时
const live = computed(() => range.value === 'live' || !history.value?.enabled)
const canToggle = computed(() => !!history.value && !saving.value && (history.value.enabled || history.value.supported))

const description = computed(() => live.value
  ? '当前活动连接的汇总，连接关闭后不再计入。'
  : `最近 ${TRAFFIC_RANGES.find(item => item.value === range.value)!.label}，包括已关闭的连接；数据约每 5 分钟更新。`)
const sinceText = computed(() => history.value?.since ? new Date(history.value.since).toLocaleString('zh-CN', { dateStyle: 'short', timeStyle: 'short' }) : '')

interface StatRow { key: string; download: number; upload: number; speed: number; count: number }
const rows = computed<StatRow[]>(() => live.value
  ? aggregateConnections(props.connections, by.value, props.speeds)
    .map(row => ({ key: row.key, download: row.download, upload: row.upload, speed: row.downSpeed + row.upSpeed, count: row.count }))
  : (data.value?.rows || []).map(row => ({ key: row.key, download: row.down, upload: row.up, speed: 0, count: row.conns })))
const sorts = computed(() => [
  { key: 'download' as const, label: '下载' },
  { key: 'upload' as const, label: '上传' },
  { key: 'total' as const, label: '总流量' },
  ...(live.value ? [{ key: 'speed' as const, label: '速度' }] : []),
  { key: 'count' as const, label: live.value ? '连接数' : '新建连接' }
])
watch(live, isLive => { if (!isLive && sort.value === 'speed') sort.value = 'download' })
const metric: Record<SortKey, (row: StatRow) => number> = {
  download: row => row.download,
  upload: row => row.upload,
  total: row => row.download + row.upload,
  speed: row => row.speed,
  count: row => row.count
}
const sorted = computed(() => [...rows.value].sort((a, b) => metric[sort.value](b) - metric[sort.value](a) || a.key.localeCompare(b.key)))

const tiles = computed(() => {
  const count = (key: string) => key !== UNATTRIBUTED_KEY && key !== OTHER_TARGET_KEY
  const sum = live.value
    ? rows.value.reduce((acc, row) => ({ down: acc.down + row.download, up: acc.up + row.upload, count: acc.count + row.count }), { down: 0, up: 0, count: 0 })
    : { down: data.value?.total?.down ?? 0, up: data.value?.total?.up ?? 0, count: data.value?.total?.conns ?? 0 }
  return [
    { label: column.value, value: String(rows.value.filter(row => count(row.key)).length) },
    { label: '总流量', value: formatBytes(sum.down + sum.up) },
    { label: '下载', value: formatBytes(sum.down) },
    { label: '上传', value: formatBytes(sum.up) },
    { label: live.value ? '连接数' : '新建连接', value: String(sum.count) }
  ]
})
const trend = computed(() => (data.value?.trend || []).map(point => point.up + point.down))

/** 地址和域名用等宽字体，设备备注、节点和策略组名称用正文字体 */
function nameOf(key: string): { label: string; detail?: string; detailMono?: boolean; title: string; mono: boolean } {
  if (key === UNATTRIBUTED_KEY) return { label: '未归属', detail: '采样间隔内开始又结束的短连接', title: '两次采样之间开始又结束的短连接，只知道总量', mono: false }
  if (key === OTHER_TARGET_KEY) return { label: '其他目标', detail: '超出每台设备保留数量的目标', title: '超出每台设备保留数量的目标合计', mono: false }
  if (by.value === 'destination') return { label: key, title: key, mono: true }
  if (by.value !== 'source') return { label: key, title: key, mono: false }
  if (isInnerSource(key)) return { label: 'Inner', detail: 'Mihomo 自身发起', title: 'Mihomo 自身发起的连接，例如 DNS 上游和测速', mono: false }
  const note = deviceNote(props.notes, key)
  return note ? { label: note, detail: key, detailMono: true, title: `${note} · ${key}`, mono: false } : { label: key, title: key, mono: true }
}

const speedText = (bytesPerSecond: number) => bytesPerSecond > 0 ? formatBytes(bytesPerSecond) + '/s' : '—'

/* ---------- 后台记录 ---------- */
let controller: AbortController | null = null
let disposed = false
let epoch = 0
async function loadHistory() {
  if (!props.agentId) return
  controller?.abort()
  controller = new AbortController()
  const request = ++epoch
  // 没开记录时只取状态，范围用最短的
  const wanted = !live.value ? range.value : '24h'
  try {
    const { data: body } = await trafficHistoryApi.get(props.agentId, wanted, by.value, { signal: controller.signal })
    if (disposed || request !== epoch) return
    history.value = body.data
    data.value = body.data.enabled && wanted === range.value ? body.data : null
    historyError.value = ''
  } catch (failure: any) {
    if (failure?.code === 'ERR_CANCELED' || disposed || request !== epoch) return
    historyError.value = panelError(failure, '暂时读不到流量记录。')
  }
}

async function toggle(enabled: boolean) {
  if (!props.agentId || saving.value) return
  let clear = false
  if (!enabled) {
    const choice = await choose('停止后 Agent 不再记录流量。已记录的数据可以保留，之后重新开启会接着记录；也可以现在删除。', {
      title: '停止后台记录', confirmText: '停止并保留', altText: '停止并删除'
    })
    if (choice === 'cancel') return
    clear = choice === 'alt'
  }
  saving.value = true
  try {
    await trafficHistoryApi.setEnabled(props.agentId, enabled, clear)
    if (enabled) {
      notify.success('已开启后台记录', 'Agent 每 5 分钟上报一次，几分钟后可在 24 小时范围查看。')
    } else {
      range.value = 'live'
      notify.success(clear ? '已停止并删除记录' : '已停止后台记录')
    }
    await loadHistory()
  } catch (failure) {
    notify.error(enabled ? '开启后台记录失败' : '停止后台记录失败', panelError(failure))
  } finally {
    saving.value = false
  }
}

async function changeRetention(days: number) {
  if (!history.value || days === history.value.retention_days) return
  const previous = history.value.retention_days
  if (days < previous && !await confirmDanger(`超过 ${days} 天的记录会在下次保存时删除，所有 Agent 共用这个设置。`, { title: '缩短保留时长', confirmText: '缩短' })) return
  saving.value = true
  try {
    await trafficHistoryApi.setRetention(days)
    history.value = { ...history.value, retention_days: days }
  } catch (failure) {
    notify.error('修改保留时长失败', panelError(failure))
  } finally {
    saving.value = false
  }
}

async function clearHistory() {
  if (!props.agentId || !await confirmDanger('删除这台设备已记录的全部流量，开关保持开启，之后继续记录。', { title: '清空记录', confirmText: '清空' })) return
  saving.value = true
  try {
    await trafficHistoryApi.clear(props.agentId)
    notify.success('已清空记录')
    await loadHistory()
  } catch (failure) {
    notify.error('清空记录失败', panelError(failure))
  } finally {
    saving.value = false
  }
}

let timer: ReturnType<typeof setInterval> | null = null
function stopPolling() {
  if (timer) clearInterval(timer)
  timer = null
}
function startPolling() {
  stopPolling()
  if (!props.agentId || props.active === false) return
  void loadHistory()
  timer = setInterval(() => {
    // 实时范围只需要开关状态，不必每分钟重读
    if (document.visibilityState === 'visible' && !live.value) void loadHistory()
  }, HISTORY_POLL_MS)
}

watch([range, by], () => { if (props.active !== false && !live.value) void loadHistory() })
watch(() => props.active, shown => { if (shown === false) stopPolling(); else startPolling() })
onMounted(startPolling)
onUnmounted(() => {
  disposed = true
  stopPolling()
  controller?.abort()
})
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
