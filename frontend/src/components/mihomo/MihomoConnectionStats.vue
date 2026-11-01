<template>
  <SectionCard class="stats-card" title="连接统计" :padded="false" :description="description" :help="HELP">
    <template #actions>
      <Segmented v-if="history?.enabled" v-model="range" :options="TRAFFIC_RANGES" label="统计范围" class="max-w-full overflow-x-auto" />
    </template>

    <div v-if="agentId" class="stats-record">
      <label class="flex min-h-11 cursor-pointer items-center gap-2 font-medium" :class="!canToggle && 'cursor-not-allowed opacity-60'">
        <Switch :model-value="!!history?.enabled" :disabled="!canToggle" aria-label="后台记录流量" @update:model-value="toggle(!!$event)" />后台记录
      </label>
      <template v-if="history?.enabled">
        <span class="text-muted-foreground">记录中<template v-if="history.since"> · 从 {{ sinceText }} 开始</template></span>
        <span class="text-muted-foreground">保留 {{ history.retention_days }} 天</span>
        <div class="stats-record-actions">
          <Popover>
            <PopoverTrigger as-child>
              <Button variant="ghost" size="sm" :disabled="saving">记录设置</Button>
            </PopoverTrigger>
            <PopoverContent align="end" class="w-64" role="dialog" aria-label="记录设置">
              <div class="flex items-center justify-between gap-3">
                <span class="text-[12.5px]">保留时长</span>
                <Select :model-value="String(history.retention_days)" :disabled="saving" @update:model-value="changeRetention(Number($event))">
                  <SelectTrigger class="w-24 min-w-0" aria-label="保留时长"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="days in history.retention_choices" :key="days" :value="String(days)">{{ days }} 天</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <p class="mt-3 mb-0 text-[12px] text-muted-foreground">所有设备共用；缩短时长需要确认。</p>
            </PopoverContent>
          </Popover>
          <Button variant="ghost" size="sm" :disabled="saving" @click="clearHistory"><Trash2 class="size-3.5" />清空记录</Button>
        </div>
        <span v-if="history.full_chain === false" class="w-full text-[12px] text-muted-foreground">
          Agent 更新到 {{ history.full_chain_version }} 后记录完整链路；在此之前的记录只有策略组和出口节点两层。
        </span>
      </template>
      <span v-else-if="history && !history.supported" class="text-warning-accent">需要将 Agent 更新到 {{ history.min_version }} 或更新版本。</span>
      <span v-else-if="historyError" role="alert" class="text-warning-accent">{{ historyError }}</span>
      <span v-else class="text-muted-foreground">开启后 Agent 在后台持续记录，不打开页面也能统计；可查看最近 24 小时到 30 天。</span>
    </div>

    <div class="stats-summary">
      <div v-for="tile in tiles" :key="tile.label" data-tile class="stats-metric" :class="{ 'stats-metric-primary': tile.primary }">
        <div class="stats-metric-label" :class="tile.tone && `stats-metric-${tile.tone}`">{{ tile.label }}</div>
        <div class="stats-metric-value num">{{ tile.value }}</div>
        <div v-if="tile.sub" class="stats-metric-sub num" :title="tile.sub">{{ tile.sub }}</div>
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

    <div class="stats-toolbar">
      <div class="stats-dimension">
        <span class="stats-dimension-label">统计维度</span>
        <Segmented v-model="by" :options="OVERVIEW_AGGREGATES" label="聚合方式" class="stats-aggregate" />
      </div>
      <button v-if="rows.some(row => row.parts.length)" type="button" class="stats-toggle-all" @click="toggleAll">
        <ChevronsUpDown v-if="allCollapsed" class="size-3" aria-hidden="true" /><ChevronsDownUp v-else class="size-3" aria-hidden="true" />
        {{ allCollapsed ? '展开全部明细' : '收起全部明细' }}
      </button>
    </div>

    <LoadingRows v-if="live ? loading : !data" :rows="4" />
    <EmptyState v-else-if="!rows.length" :icon="Cable" :title="live ? '没有活动连接' : '这段时间还没有记录'" :description="live ? '有流量经过 Mihomo 时自动显示。' : 'Agent 每 5 分钟上报一次，刚开启时稍等几分钟。'" />
    <div v-else class="stats-frame cf-table-wrap overflow-y-auto overscroll-contain" :class="tall ? 'max-h-[75dvh]' : 'max-h-[560px]'" tabindex="0" role="region" :aria-label="`连接统计，${column}`">
      <table class="cf-table stats-table">
        <thead>
          <tr>
            <th scope="col">{{ column }} / {{ partsLabel }}</th>
            <th scope="col" class="text-right">{{ partsBySource ? '类型' : '直连占比' }}</th>
            <th v-for="item in sorts" :key="item.key" scope="col" class="text-right" :class="item.key === 'total' ? 'stats-total' : 'stats-detail'" :aria-sort="sort === item.key ? 'descending' : 'none'">
              <button type="button" class="stats-sort" :data-active="sort === item.key" :aria-label="`按${item.label}排序`" @click="sort = item.key">
                {{ item.label }}<ArrowDown v-if="sort === item.key" class="size-3" />
              </button>
            </th>
          </tr>
        </thead>
        <tbody v-for="row in sorted" :key="row.key">
          <tr class="stats-group" data-group>
            <td class="stats-name">
              <div class="stats-group-heading">
                <button v-if="row.parts.length" type="button" class="stats-expand" :aria-expanded="!collapsed.has(row.key)"
                  :aria-label="`${collapsed.has(row.key) ? '展开' : '收起'} ${groupTitle(row.key)} 的${partsLabel}`" @click="toggleGroup(row.key)">
                  <ChevronRight class="size-3.5 transition-transform" :class="!collapsed.has(row.key) && 'rotate-90'" />
                </button>
                <span v-else class="stats-expand-spacer" aria-hidden="true" />
                <ChainLabel v-if="by === 'chain' && row.key !== UNATTRIBUTED_KEY" class="min-w-0" :layers="chainLayers(row.key)" strong />
                <span v-else class="min-w-0">
                  <span class="block truncate font-semibold" :class="nameOf(row.key).mono && 'font-mono text-[12.5px]'" :title="nameOf(row.key).title">{{ nameOf(row.key).label }}</span>
                  <span v-if="nameOf(row.key).detail" class="block truncate text-[11px] font-normal text-muted-foreground" :class="nameOf(row.key).detailMono && 'font-mono'">{{ nameOf(row.key).detail }}</span>
                </span>
                <span class="stats-group-tag">汇总</span>
                <span v-if="row.partCount" class="stats-part-count">{{ row.partCount }} {{ partsBySource ? '台' : '条' }}{{ partsLabel }}</span>
              </div>
            </td>
            <td class="text-right">
              <span v-if="row.key === UNATTRIBUTED_KEY" class="text-muted-foreground">—</span>
              <span v-else-if="partsBySource" class="chip" :class="kindChip(outletOfGroup(row.key))?.class">{{ kindChip(outletOfGroup(row.key))?.label ?? '—' }}</span>
              <span v-else class="inline-flex items-center gap-2" :title="`直连 ${formatBytes(row.direct)} · 代理 ${formatBytes(row.proxy)}`">
                <span class="stats-share" aria-hidden="true"><span :style="{ width: `${share(row)}%` }" /></span>
                <span class="num w-9 text-right">{{ share(row) }}%</span>
              </span>
            </td>
            <td class="stats-detail num text-right">{{ formatBytes(row.download) }}</td>
            <td class="stats-detail num text-right">{{ formatBytes(row.upload) }}</td>
            <td class="stats-total num text-right font-medium">{{ formatBytes(row.download + row.upload) }}</td>
            <td v-if="live" class="stats-detail num text-right" :class="row.speed ? '' : 'text-muted-foreground'">{{ speedText(row.speed) }}</td>
            <td class="stats-detail num text-right">{{ row.count }}</td>
          </tr>
          <template v-if="!collapsed.has(row.key)">
            <tr v-for="part in shownParts(row)" :key="row.key + '\u0000' + part.key" class="stats-part">
              <td class="stats-name">
                <ChainLabel v-if="!partsBySource" :layers="chainLayers(part.key)" :omit-leading="by === 'group' ? row.key : undefined" />
                <span v-else class="block truncate" :class="partName(part.key).mono && 'font-mono text-[12px]'" :title="partName(part.key).title">{{ partName(part.key).label }}</span>
              </td>
              <td class="text-right">
                <span v-if="!partsBySource && kindChip(lastLayer(part.key))" class="chip" :class="kindChip(lastLayer(part.key))!.class">{{ kindChip(lastLayer(part.key))!.label }}</span>
              </td>
              <td class="stats-detail num text-right text-muted-foreground">{{ formatBytes(part.download) }}</td>
              <td class="stats-detail num text-right text-muted-foreground">{{ formatBytes(part.upload) }}</td>
              <td class="stats-total num text-right">{{ formatBytes(part.download + part.upload) }}</td>
              <td v-if="live" class="stats-detail num text-right text-muted-foreground">{{ speedText(part.speed) }}</td>
              <td class="stats-detail num text-right text-muted-foreground">{{ part.count }}</td>
            </tr>
            <tr v-if="hiddenParts(row).count" class="stats-part stats-more">
              <td class="stats-name" :colspan="live ? 7 : 6">
                <button type="button" class="stats-more-button" @click="showAll(row.key)">
                  <ChevronDown class="size-3.5" aria-hidden="true" />
                  显示其余 {{ hiddenParts(row).count }} 条{{ partsLabel }}<span class="num text-muted-foreground"> · {{ formatBytes(hiddenParts(row).bytes) }}</span>
                </button>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
      <p v-if="!live && data?.omitted" class="m-0 border-t border-border px-5 py-2 text-[12px] text-muted-foreground">另有 {{ data.omitted }} 项流量较小，未列出。</p>
    </div>
  </SectionCard>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, onUnmounted, ref, watch } from 'vue'
import { ArrowDown, Cable, ChevronDown, ChevronRight, ChevronsDownUp, ChevronsUpDown, Trash2 } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import SectionCard from '@/components/common/SectionCard.vue'
import Segmented from '@/components/common/Segmented.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import Sparkline from '@/components/common/Sparkline.vue'
import { mihomoPanelApi, trafficHistoryApi } from '@/api'
import { choose, confirmDanger, notify } from '@/lib/feedback'
import { formatBytes } from '@/lib/format'
import { deviceNote } from '@/lib/device-notes'
import {
  aggregateConnections, chainLayers, isInnerSource, OTHER_TARGET_KEY, outletKind, OVERVIEW_AGGREGATES, PARTS_BY_SOURCE,
  TRAFFIC_RANGES, UNATTRIBUTED_KEY, type OverviewAggregate, type TrafficHistoryData, type TrafficRange
} from '@/lib/mihomo-overview'
import { panelError, type MihomoConnection } from '@/types/mihomo'

const HELP = '「实时」汇总当前活动连接：流量是每条连接建立以来的累计字节，速度来自最近两次采样之差，连接关闭后不再计入。开启后台记录后，Agent 持续采样并每 5 分钟上报，服务端按「定时任务 → 监控历史保存」的间隔写入数据库；24 小时到 30 天的范围包括已关闭的连接。链路从入口策略组排到出口节点，出口是直连还是代理按 Mihomo 当前的节点类型判断。目标按主域名归并；「未归属」是两次采样之间开始又结束的短连接，只知道总量。超过 7 天的数据按天保存（目标超过 2 天按天、每台设备每天保留流量最大的 100 个），范围边界按整天计。'
const STORAGE_KEY = 'configflow:mihomo-overview:aggregate'
const RANGE_KEY = 'configflow:mihomo-overview:range'
const HISTORY_POLL_MS = 60_000
// 每组默认列出的明细条数，其余折成「其余 N 条」
const PARTS_SHOWN = 3
type SortKey = 'download' | 'upload' | 'total' | 'speed' | 'count'

/** 链路：各层名称用箭头连接，最后一层是出口节点 */
const ChainLabel = defineComponent({
  props: { layers: { type: Array as () => string[], required: true }, strong: Boolean, omitLeading: String },
  setup(componentProps) {
    return () => {
      const fullLayers = componentProps.layers.length ? componentProps.layers : ['—']
      // 策略组已在父行中显示；仅省略相同的入口，单层出口仍保留。
      const layers = fullLayers.length > 1 && fullLayers[0] === componentProps.omitLeading ? fullLayers.slice(1) : fullLayers
      const children = layers.flatMap((name, index) => [
        ...(index ? [h('span', { class: 'stats-chain-arrow', 'aria-hidden': 'true' }, '→')] : []),
        h('span', { class: index === layers.length - 1 ? 'stats-chain-exit' : '' }, name)
      ])
      return h('span', { class: ['stats-chain', componentProps.strong && 'font-semibold'], title: fullLayers.join(' → ') }, [
        h('span', { class: 'cf-sr' }, fullLayers.join(' → ')),
        h('span', { 'aria-hidden': 'true' }, children)
      ])
    }
  }
})

const props = defineProps<{
  connections: MihomoConnection[]
  speeds: Map<string, { up: number; down: number }>
  notes: Record<string, string>
  loading?: boolean
  /** 有 Agent 时显示后台记录的开关与历史范围 */
  agentId?: string
  active?: boolean
  /** 单独成页时表格可以更高 */
  tall?: boolean
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
// 明细的维度：按链路、出站节点汇总时是设备，其余是链路
const partsBySource = computed(() => PARTS_BY_SOURCE.includes(by.value))
const partsLabel = computed(() => partsBySource.value ? '设备' : '链路')

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

/** 出口节点名称 → Mihomo 节点类型，用来判断直连、代理还是拦截 */
const proxyTypes = ref<Record<string, string>>({})
interface StatPart { key: string; download: number; upload: number; speed: number; count: number }
interface StatRow extends StatPart { parts: StatPart[]; partCount: number; direct: number; proxy: number }
const lastLayer = (chain: string) => {
  const layers = chainLayers(chain)
  return layers.length ? layers[layers.length - 1] : ''
}
/** 分组行自己的出口：按链路时是链路的最后一层，按出站节点时就是节点 */
const outletOfGroup = (key: string) => by.value === 'chain' ? lastLayer(key) : key

function withSplit(row: StatPart, parts: StatPart[], partCount = parts.length): StatRow {
  let direct = 0
  let proxy = 0
  const add = (outlet: string, bytes: number) => {
    const kind = outletKind(outlet, proxyTypes.value)
    if (kind === 'direct') direct += bytes
    else if (kind === 'proxy') proxy += bytes
  }
  if (partsBySource.value) add(outletOfGroup(row.key), row.download + row.upload)
  else parts.forEach(part => add(lastLayer(part.key), part.download + part.upload))
  return { ...row, parts, partCount, direct, proxy }
}
const rows = computed<StatRow[]>(() => live.value
  ? aggregateConnections(props.connections, by.value, props.speeds).map(row => withSplit(
    { key: row.key, download: row.download, upload: row.upload, speed: row.downSpeed + row.upSpeed, count: row.count },
    row.parts.map(part => ({ key: part.key, download: part.download, upload: part.upload, speed: part.downSpeed + part.upSpeed, count: part.count }))))
  : (data.value?.rows || []).map(row => withSplit(
    { key: row.key, download: row.down, upload: row.up, speed: 0, count: row.conns },
    (row.parts || []).map(part => ({ key: part.key, download: part.down, upload: part.up, speed: 0, count: part.conns })),
    row.part_count ?? row.parts?.length ?? 0)))
const sorts = computed(() => [
  { key: 'download' as const, label: '下载' },
  { key: 'upload' as const, label: '上传' },
  { key: 'total' as const, label: '总流量' },
  ...(live.value ? [{ key: 'speed' as const, label: '速度' }] : []),
  { key: 'count' as const, label: live.value ? '连接数' : '新建连接' }
])
watch(live, isLive => { if (!isLive && sort.value === 'speed') sort.value = 'download' })
const metric: Record<SortKey, (row: StatPart) => number> = {
  download: row => row.download,
  upload: row => row.upload,
  total: row => row.download + row.upload,
  speed: row => row.speed,
  count: row => row.count
}
const sorted = computed(() => [...rows.value].sort((a, b) => metric[sort.value](b) - metric[sort.value](a) || a.key.localeCompare(b.key)))
const share = (row: StatRow) => row.direct + row.proxy ? Math.round(row.direct / (row.direct + row.proxy) * 100) : 0

/* ---------- 明细：默认展开每组前几条 ---------- */
const collapsed = ref(new Set<string>())
const expandedAll = ref(new Set<string>())
watch(by, () => { collapsed.value = new Set(); expandedAll.value = new Set() })
const allCollapsed = computed(() => rows.value.some(row => row.parts.length) && rows.value.every(row => !row.parts.length || collapsed.value.has(row.key)))
function toggleGroup(key: string) {
  const next = new Set(collapsed.value)
  if (!next.delete(key)) next.add(key)
  collapsed.value = next
}
function toggleAll() {
  collapsed.value = allCollapsed.value ? new Set() : new Set(rows.value.filter(row => row.parts.length).map(row => row.key))
}
function showAll(key: string) {
  expandedAll.value = new Set(expandedAll.value).add(key)
}
const shownParts = (row: StatRow) => expandedAll.value.has(row.key) ? row.parts : row.parts.slice(0, PARTS_SHOWN)
function hiddenParts(row: StatRow) {
  const shown = shownParts(row)
  const shownBytes = shown.reduce((sum, part) => sum + part.download + part.upload, 0)
  // 服务端最多返回 50 条明细；没列出的部分按行合计减去已显示的计算
  return { count: Math.max(0, row.partCount - shown.length), bytes: Math.max(0, row.download + row.upload - shownBytes) }
}

const tiles = computed(() => {
  const counted = (key: string) => key !== UNATTRIBUTED_KEY && key !== OTHER_TARGET_KEY
  const sum = live.value
    ? rows.value.reduce((acc, row) => ({ down: acc.down + row.download, up: acc.up + row.upload, count: acc.count + row.count }), { down: 0, up: 0, count: 0 })
    : { down: data.value?.total?.down ?? 0, up: data.value?.total?.up ?? 0, count: data.value?.total?.conns ?? 0 }
  // 直连 / 代理按出口节点合计；历史范围用服务端的出口总量，不受表格行数上限影响
  const outlets = live.value
    ? aggregateConnections(props.connections, 'outbound', props.speeds).map(row => ({ key: row.key, bytes: row.download + row.upload }))
    : (data.value?.outbounds || []).map(row => ({ key: row.key, bytes: row.down + row.up }))
  const kinds = { direct: 0, proxy: 0 }
  for (const outlet of outlets) {
    const kind = outletKind(outlet.key, proxyTypes.value)
    if (kind === 'direct') kinds.direct += outlet.bytes
    else if (kind === 'proxy') kinds.proxy += outlet.bytes
  }
  const shareText = (bytes: number) => kinds.direct + kinds.proxy ? `占 ${Math.round(bytes / (kinds.direct + kinds.proxy) * 100)}%` : ''
  return [
    { label: '总流量', value: formatBytes(sum.down + sum.up), sub: `↓ ${formatBytes(sum.down)} · ↑ ${formatBytes(sum.up)}`, primary: true },
    { label: '代理流量', value: formatBytes(kinds.proxy), sub: shareText(kinds.proxy), tone: 'proxy' },
    { label: '直连流量', value: formatBytes(kinds.direct), sub: shareText(kinds.direct), tone: 'direct' },
    { label: live.value ? '活动连接' : '新建连接', value: String(sum.count) },
    { label: column.value, value: String(rows.value.filter(row => counted(row.key)).length) }
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
const groupTitle = (key: string) => by.value === 'chain' ? chainLayers(key).join(' → ') : nameOf(key).label

/** 明细行是设备时的名称 */
function partName(key: string): { label: string; title: string; mono: boolean } {
  if (isInnerSource(key)) return { label: 'Inner', title: 'Mihomo 自身发起的连接', mono: false }
  const note = deviceNote(props.notes, key)
  return note ? { label: `${note} · ${key}`, title: `${note} · ${key}`, mono: false } : { label: key, title: key, mono: true }
}

const KIND_CHIPS = { direct: { label: '直连', class: 'chip-ok' }, proxy: { label: '代理', class: 'chip-acc' }, reject: { label: '拦截', class: 'chip-bad' } } as const
function kindChip(outbound: string) {
  const kind = outletKind(outbound, proxyTypes.value)
  return kind === 'none' ? null : KIND_CHIPS[kind]
}

async function loadProxyTypes() {
  if (!props.agentId) return
  try {
    const { data: body } = await mihomoPanelApi.proxies(props.agentId)
    const proxies: Record<string, { type?: string }> = body.data?.proxies || {}
    if (!disposed) proxyTypes.value = Object.fromEntries(Object.entries(proxies).map(([name, proxy]) => [name, proxy?.type || '']))
  } catch {
    // 读不到节点类型时只按 DIRECT / REJECT 名称判断
  }
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
onMounted(() => {
  startPolling()
  if (props.active !== false) void loadProxyTypes()
})
watch(() => props.active, shown => { if (shown !== false && !Object.keys(proxyTypes.value).length) void loadProxyTypes() })
onUnmounted(() => {
  disposed = true
  stopPolling()
  controller?.abort()
})
</script>

<style scoped>
.stats-card { container-type: inline-size; }
.stats-record { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: 4px 12px; margin: 0 20px 16px; padding-top: 8px; border-top: 1px solid var(--border); font-size: 12px; }
.stats-record-actions { display: flex; align-items: center; gap: 4px; margin-left: auto; color: var(--muted-foreground); }
.stats-summary { display: grid; grid-template-columns: 1.5fr 1fr 1fr .8fr .8fr; margin: 0 20px 20px; padding: 20px; border-radius: var(--radius-xl); background: color-mix(in oklab, var(--secondary) 60%, var(--card)); }
.stats-metric { min-width: 0; padding-inline: 20px; border-left: 1px solid var(--border); }
.stats-metric:first-child { padding-left: 0; border-left: 0; }
.stats-metric:last-child { padding-right: 0; }
.stats-metric-label { color: var(--muted-foreground); font-size: 12px; }
.stats-metric-proxy::before, .stats-metric-direct::before { content: ''; display: inline-block; width: 6px; height: 6px; margin-right: 6px; border-radius: 2px; background: var(--primary-accent); }
.stats-metric-direct::before { background: var(--success-accent); }
.stats-metric-value { margin-top: 4px; font-size: 22px; line-height: 32px; overflow-wrap: anywhere; }
.stats-metric-primary .stats-metric-value { font-size: 28px; font-weight: 500; }
.stats-metric-sub { margin-top: 4px; font-size: 11px; color: var(--muted-foreground); overflow-wrap: anywhere; }
.stats-toolbar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; margin: 0 20px 16px; }
.stats-dimension { display: flex; align-items: center; gap: 16px; min-width: 0; }
.stats-dimension-label { flex-shrink: 0; color: var(--muted-foreground); font-size: 12px; }
.stats-aggregate { min-width: 0; padding: 0; border: 0; border-radius: 0; background: transparent; }
.stats-aggregate :deep(.cf-segmented-options) { width: auto; min-width: 0; flex-wrap: wrap; gap: 0 20px; height: auto; padding: 0; border-radius: 0; background: transparent; box-shadow: none; }
.stats-aggregate :deep(.cf-segmented-thumb) { display: none; }
.stats-aggregate :deep([data-segment]) { height: 40px; margin: 0; padding: 0; border-bottom: 2px solid transparent; border-radius: 0; }
.stats-aggregate :deep([aria-checked='true']) { color: var(--primary-accent); border-bottom-color: var(--primary-accent); font-weight: 500; }
.stats-frame { container-type: inline-size; margin: 0 20px 20px; border-radius: var(--radius-lg); }
.stats-table { width: 100%; table-layout: fixed; }
.stats-table th:first-child { width: 40%; }
.stats-table th { position: sticky; top: 0; z-index: 1; background: var(--card); white-space: nowrap; font-size: 11.5px; font-weight: 500; }
.stats-table td { white-space: nowrap; border-bottom: 0; }
.stats-name { max-width: 420px; }
.stats-group td { height: 48px; padding-block: 10px; border-top: 1px solid var(--border-strong); background: color-mix(in oklab, var(--secondary) 65%, var(--card)); }
.stats-group .stats-total { font-weight: 600; }
.stats-group-heading { display: flex; min-width: 0; align-items: center; gap: 8px; }
.stats-group-tag { flex-shrink: 0; padding: 0 5px; border: 1px solid var(--border-strong); border-radius: var(--radius-xs); color: var(--muted-foreground); font-size: 10px; font-weight: 400; }
.stats-part-count { flex-shrink: 0; color: var(--muted-foreground); font-size: 11px; }
.stats-expand, .stats-expand-spacer { display: inline-flex; width: 22px; height: 22px; flex-shrink: 0; align-items: center; justify-content: center; border-radius: var(--radius-sm); color: var(--muted-foreground); }
.stats-expand:hover { background: var(--accent); color: var(--foreground); }
.stats-part td { height: 38px; padding-block: 8px; border-top: 0; color: var(--muted-foreground); font-size: 12px; }
.stats-part td.stats-name { position: relative; padding-left: 56px; }
/* 连续树线贯穿组内明细，最后一条在中线结束。 */
.stats-part .stats-name::before { content: ''; position: absolute; left: 27px; top: 0; bottom: 0; border-left: 1px solid var(--border-strong); }
.stats-part .stats-name::after { content: ''; position: absolute; left: 27px; top: 50%; width: 17px; border-top: 1px solid var(--border-strong); }
.stats-part:last-child .stats-name::before { bottom: 50%; }
/* ChainLabel 的内部元素不带本组件的 scoped 标记。 */
:deep(.stats-chain) { display: block; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.stats-part :deep(.stats-chain) { color: var(--muted-foreground); }
:deep(.stats-chain-arrow) { margin: 0 5px; color: var(--muted-foreground); }
:deep(.stats-chain-exit) { color: var(--foreground); }
.stats-share { display: inline-block; width: 36px; height: 4px; overflow: hidden; border-radius: 999px; background: color-mix(in oklab, var(--primary-accent) 55%, transparent); }
.stats-share span { display: block; height: 100%; background: var(--success-accent); }
.stats-toggle-all { display: inline-flex; min-height: 30px; flex-shrink: 0; align-items: center; gap: 4px; padding: 0 10px; border: 1px solid var(--border-strong); border-radius: var(--radius-sm); background: var(--card); color: var(--muted-foreground); font-size: 12px; font-weight: 500; }
.stats-toggle-all:hover { background: var(--accent); color: var(--foreground); }
.stats-more-button { display: inline-flex; min-height: 26px; align-items: center; flex-wrap: wrap; gap: 4px; padding: 0 4px; border-radius: var(--radius-sm); color: var(--primary-accent); font-size: 12px; white-space: normal; text-align: left; }
.stats-more-button:hover { background: var(--primary-soft); }
.stats-sort { display: inline-flex; min-height: 28px; align-items: center; gap: 2px; border-radius: var(--radius-sm); color: inherit; }
.stats-sort[data-active='true'], .stats-sort:hover { color: var(--primary-accent); }
.stats-sort:focus-visible, .stats-expand:focus-visible, .stats-toggle-all:focus-visible, .stats-more-button:focus-visible { outline: 2px solid var(--ring); outline-offset: -2px; }
@container (max-width: 900px) {
  .stats-table { table-layout: auto; }
  .stats-table th:first-child { width: auto; }
  .stats-table .stats-detail { display: none; }
  .stats-table td, .stats-table th { padding-inline: 10px; }
  .stats-name { width: 100%; max-width: 0; }
  .stats-group-heading { gap: 4px; }
  .stats-group-tag, .stats-part-count, .stats-share { display: none; }
  .stats-part td.stats-name { padding-left: 36px; }
  .stats-part .stats-name::before, .stats-part .stats-name::after { left: 21px; }
  .stats-part .stats-name::after { width: 9px; }
  :deep(.stats-chain) { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; white-space: normal; overflow-wrap: anywhere; }
}
@container (max-width: 800px) {
  .stats-record, .stats-summary, .stats-toolbar, .stats-frame { margin-inline: 16px; }
  .stats-summary { grid-template-columns: repeat(3, minmax(0, 1fr)); padding: 16px; gap: 16px 0; }
  .stats-metric { padding-inline: 12px; }
  .stats-metric:nth-child(4) { border-left: 0; padding-left: 0; }
  .stats-metric-value { font-size: 18px; line-height: 28px; }
  .stats-metric-primary .stats-metric-value { font-size: 22px; }
  .stats-dimension { width: 100%; }
  .stats-dimension-label { display: none; }
  .stats-aggregate { width: 100%; }
  .stats-aggregate :deep(.cf-segmented-options) { width: 100%; gap: 0 12px; }
  .stats-aggregate :deep([data-segment]) { min-height: var(--cf-touch); font-size: 12px; }
}
@media (max-width: 639px) {
  .stats-record, .stats-summary, .stats-toolbar, .stats-frame { margin-inline: 12px; }
  .stats-record-actions { width: 100%; justify-content: flex-end; }
  .stats-record-actions :deep(button), .stats-toggle-all { min-height: var(--cf-touch); }
  .stats-summary { padding: 12px; }
  .stats-metric { padding-inline: 8px; }
  .stats-metric-value { font-size: 16px; }
  .stats-metric-primary .stats-metric-value { font-size: 20px; }
  .stats-metric-sub { font-size: 10px; }
}
@media (pointer: coarse) {
  .stats-sort, .stats-more-button, .stats-toggle-all, .stats-aggregate :deep([data-segment]) { min-height: var(--cf-touch); }
  .stats-expand, .stats-expand-spacer { width: 22px; height: 44px; position: relative; }
  .stats-expand::after { content: ''; position: absolute; inset: 0 -11px; }
}
</style>
