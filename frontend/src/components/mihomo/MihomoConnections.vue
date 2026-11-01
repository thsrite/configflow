<template>
  <div ref="panelElement" class="min-w-0 mihomo-connections">
    <div class="mb-4 min-w-0 rounded-xl border border-border bg-card">
      <div class="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b border-border px-3 py-2">
        <Segmented v-model="view" :options="viewOptions" label="连接状态" class="max-w-full overflow-x-auto" />
        <div class="ml-auto flex flex-wrap items-center gap-2">
          <label class="flex min-h-11 items-center gap-2 text-xs text-muted-foreground">
            <Switch v-model="live" aria-label="每 2 秒刷新连接" />实时
          </label>
          <Button v-if="view === 'closed'" variant="ghost" class="min-h-11" :disabled="!closed.length" @click="closed = []">
            <Eraser class="size-3.5" />清空记录
          </Button>
          <Button v-else variant="ghost" class="min-h-11" :disabled="!activeMatches.length || closingAll" @click="closeAll">
            <Loader2 v-if="closingAll" class="size-3.5 animate-spin" /><X v-else class="size-3.5" />{{ filtering ? '断开筛选结果' : '全部断开' }}
          </Button>
        </div>
      </div>
      <Toolbar v-model:search="search" class="mb-0 rounded-none border-0 bg-transparent" placeholder="搜索主机、IP、备注、规则，支持正则">
        <template #filters>
          <div class="col-span-full flex min-w-0 flex-wrap items-center gap-1.5 max-md:w-full">
            <Select :model-value="settings.sort" @update:model-value="value => changeSort(String(value) as ConnectionSort)">
              <SelectTrigger class="min-h-11 w-36 min-w-0 max-md:flex-[1_1_8rem]" aria-label="连接排序字段"><SelectValue /></SelectTrigger>
              <SelectContent class="max-h-80">
                <SelectItem v-for="option in CONNECTION_SORT_OPTIONS" :key="option.key" :value="option.key">{{ option.label }}</SelectItem>
              </SelectContent>
            </Select>
            <Button variant="ghost" size="icon" class="size-11 shrink-0" :aria-label="settings.direction === 'asc' ? '当前升序，点击改为降序' : '当前降序，点击改为升序'"
              @click="settings.direction = settings.direction === 'asc' ? 'desc' : 'asc'">
              <ArrowUp v-if="settings.direction === 'asc'" class="size-4" /><ArrowDown v-else class="size-4" />
            </Button>
            <Select :model-value="settings.groupBy || 'none'" @update:model-value="value => setGroup(String(value))">
              <SelectTrigger class="min-h-11 w-40 min-w-0 max-md:flex-[1_1_8rem]" :class="settings.groupBy ? 'border-primary-accent text-primary-accent' : ''" aria-label="连接分组">
                <span class="flex min-w-0 items-center gap-2"><Layers class="size-4" :class="settings.groupBy ? 'text-primary-accent' : 'text-muted-foreground'" /><SelectValue class="truncate" /></span>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">不分组</SelectItem>
                <SelectItem v-for="field in CONNECTION_GROUP_FIELDS" :key="field" :value="field">按{{ columnLabel(field) }}分组</SelectItem>
              </SelectContent>
            </Select>
            <Popover>
              <PopoverTrigger as-child>
                <Button variant="outline" class="min-h-11 shrink-0 gap-1.5 px-3" :class="filterCount ? 'border-primary-accent text-primary-accent' : ''" :aria-label="`筛选连接${filterCount ? `，${filterCount} 项条件` : ''}`">
                  <ListFilter class="size-4" /><span>筛选</span><span v-if="filterCount" class="num text-xs">{{ filterCount }}</span>
                </Button>
              </PopoverTrigger>
              <PopoverContent align="end" :collision-padding="12" class="w-[440px] max-w-[calc(100vw-24px)] max-h-(--reka-popover-content-available-height) overflow-y-auto p-4" role="dialog" aria-label="连接筛选" @open-auto-focus="focusFilters">
                <div class="mb-3 flex min-h-8 items-center justify-between gap-2">
                  <div class="flex items-center gap-1 text-sm font-semibold">筛选连接<FieldHelp label="连接筛选" text="同一项可多选，满足其中任意一个即可；不同项需要同时满足。链路可选择策略组或节点。搜索与隐藏条件均支持正则，也会匹配未显示的列。" /></div>
                  <div class="flex shrink-0 items-center gap-1">
                    <Button variant="ghost" class="min-h-11 px-2" :disabled="!filtering" @click="clearFilters">清除</Button>
                    <PopoverClose as-child><Button variant="secondary" class="min-h-11 px-3">完成</Button></PopoverClose>
                  </div>
                </div>
                <div class="connection-filter-fields grid min-w-0 grid-cols-1 gap-4 min-[400px]:grid-cols-2">
                  <FormField v-for="group in filterGroups" :key="group.key" :label="group.label" :html-for="`connection-filter-${group.key}`">
                    <MultiSelect v-model="filters[group.key]" :trigger-id="`connection-filter-${group.key}`" :options="group.options" :placeholder="`全部${group.label}`" :search-placeholder="`搜索${group.label}…`" :max-visible="1" />
                  </FormField>
                  <FormField class="col-span-full" label="隐藏匹配的连接" html-for="connection-exclude" help="符合这里内容的连接会从结果中隐藏。支持正则表达式，留空时不隐藏。">
                    <Input id="connection-exclude" v-model="filters.exclude" placeholder="例如：测速域名或进程名" class="min-h-11" />
                  </FormField>
                </div>
              </PopoverContent>
            </Popover>
            <Popover>
              <PopoverTrigger as-child>
                <Button variant="outline" size="icon" class="size-11 shrink-0" aria-label="设置显示列" title="显示列"><Columns3 class="size-4" /></Button>
              </PopoverTrigger>
              <PopoverContent align="end" :collision-padding="12" class="w-72 max-h-(--reka-popover-content-available-height) overflow-y-auto" role="dialog" aria-label="显示列">
                <div class="mb-2 flex items-center justify-between gap-2 text-sm font-semibold">显示列<Button variant="ghost" class="min-h-11 px-2 font-normal" @click="resetColumns">恢复默认</Button></div>
                <p class="mb-2 mt-0 text-xs leading-relaxed text-muted-foreground">勾选后优先显示；空间不足会收起其他列，详情中仍可查看。</p>
                <div class="grid grid-cols-2 gap-x-3">
                  <label v-for="column in CONNECTION_COLUMNS" :key="column.key" class="flex min-h-11 cursor-pointer items-center gap-2 text-[13px]">
                    <Checkbox :model-value="visibleColumnKeys.has(column.key)" :aria-label="column.label" :disabled="column.key === 'target'" @update:model-value="value => toggleColumn(column.key, !!value)" />{{ column.label }}
                    <span v-if="settings.columns.includes(column.key) && !visibleColumnKeys.has(column.key)" class="ml-auto shrink-0 text-[11px] text-muted-foreground">收起</span>
                  </label>
                </div>
              </PopoverContent>
            </Popover>
          </div>
        </template>
      </Toolbar>
      <div v-if="filtering || settings.groupBy" class="flex min-w-0 flex-wrap items-center justify-between gap-x-2 border-t border-border px-3 py-1 text-xs text-muted-foreground">
        <span class="min-w-0">
          <template v-if="settings.groupBy">按{{ columnLabel(settings.groupBy) }}分组 · <span class="num text-foreground">{{ groups.length }}</span> 组 · </template>
          {{ filtering ? '匹配' : '共' }} <span class="num text-foreground">{{ rows.length }}</span> 条连接<span v-if="filters.exclude.trim()"> · 已应用隐藏条件</span>
        </span>
        <div class="flex flex-wrap items-center gap-1">
          <template v-if="settings.groupBy">
            <Button variant="ghost" class="min-h-11 px-2 text-xs" :disabled="!expanded.size" @click="expanded = new Set()">全部收起</Button>
            <Button variant="ghost" class="min-h-11 px-2 text-xs" @click="setGroup('none')"><X class="size-3.5" />取消分组</Button>
          </template>
          <Button v-if="filtering" variant="ghost" class="min-h-11 px-2 text-xs" @click="clearFilters"><X class="size-3.5" />清除筛选</Button>
        </div>
      </div>
    </div>

    <div v-if="notesError" role="alert" class="mb-3 flex flex-wrap items-center gap-2 text-xs text-warning-accent">
      <span>设备备注暂未读取：{{ notesError }}</span><Button variant="ghost" class="min-h-11 px-2" @click="loadNotes">重试</Button>
    </div>
    <p v-if="error && snapshot" role="alert" class="mb-4 rounded-lg border border-warning-accent/30 bg-warning-soft p-3 text-sm text-warning-accent">
      {{ error }} 以下为上次成功读取的连接。
    </p>
    <LoadingRows v-if="!snapshot && !error" :rows="6" />
    <SectionCard v-else-if="!snapshot">
      <EmptyState :icon="TriangleAlert" title="读取连接失败" :description="error">
        <Button variant="outline" class="min-h-11" @click="load">重试</Button>
      </EmptyState>
    </SectionCard>
    <SectionCard v-else-if="!rows.length">
      <EmptyState :icon="Cable" :title="filtering ? '没有匹配的连接' : view === 'closed' ? '暂无已关闭的连接' : '当前没有活动连接'"
        :description="view === 'closed' && !filtering ? '打开本页后关闭的连接会记录在这里（最近 500 条），切换设备或刷新页面后清空。' : undefined" />
    </SectionCard>
    <div v-else class="connection-table-frame min-w-0 overflow-hidden rounded-xl border border-border bg-card">
      <div class="connection-table-scroll max-h-[min(65dvh,640px)] overflow-y-auto overscroll-contain" tabindex="0" role="region" aria-label="连接列表，点击连接查看完整详情">
        <table class="cf-table connection-table table-fixed">
          <caption class="cf-sr">点击表头排序，点击分组按钮归类；分组中的速度和流量为合计，时长为最长连接时长。</caption>
          <thead>
            <tr>
              <th v-for="column in visibleColumns" :key="column.key" scope="col" :style="columnStyle(column)" :class="{ 'cf-table__right': column.numeric }"
                :aria-sort="settings.sort === column.key ? settings.direction === 'asc' ? 'ascending' : 'descending' : 'none'">
                <div class="connection-heading" :class="{ 'justify-end': column.numeric }">
                  <button type="button" class="connection-sort" :data-active="settings.sort === column.key" :aria-label="'按' + column.label + '排序'" :title="column.label + ' · 再次点击切换升降序'" @click="changeSort(column.key, true)">
                    <span>{{ column.label }}</span><ArrowUp v-if="settings.sort === column.key && settings.direction === 'asc'" class="size-3" /><ArrowDown v-else-if="settings.sort === column.key" class="size-3" /><ArrowUpDown v-else class="size-3 text-muted-foreground" />
                  </button>
                  <button v-if="isGroupField(column.key)" type="button" class="connection-group-toggle connection-icon" :data-active="settings.groupBy === column.key" :aria-pressed="settings.groupBy === column.key"
                    :aria-label="(settings.groupBy === column.key ? '取消按' : '按') + column.label + '分组'" :title="(settings.groupBy === column.key ? '取消按' : '按') + column.label + '分组'" @click="toggleGroup(column.key)">
                    <ZoomOut v-if="settings.groupBy === column.key" class="size-3.5" /><ZoomIn v-else class="size-3.5" />
                  </button>
                </div>
              </th>
              <th class="connection-actions cf-table__right" scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="entry in visibleEntries" :key="entry.id">
              <tr v-if="entry.kind === 'group'" class="connection-group-row" :data-group-id="entry.group.id">
                <td v-for="column in visibleColumns" :key="column.key" :data-column="column.key" :data-label="column.label" :class="{ 'cf-table__right num': column.numeric }">
                  <button v-if="column.key === 'target'" type="button" class="connection-group-label" :aria-expanded="expanded.has(entry.group.id)" :aria-label="(expanded.has(entry.group.id) ? '收起 ' : '展开 ') + entry.group.label + '（' + entry.group.members.length + ' 条连接）'" :title="groupTitle(entry.group)" @click="toggleExpanded(entry.group.id)">
                    <ChevronDown v-if="expanded.has(entry.group.id)" class="size-3.5 shrink-0" /><ChevronRight v-else class="size-3.5 shrink-0" /><span class="truncate">{{ entry.group.label }}</span><span class="num shrink-0 text-muted-foreground">({{ entry.group.members.length }})</span><span class="shrink-0 text-xs text-muted-foreground">合计</span>
                  </button>
                  <span v-else class="connection-cell" :title="groupText(entry.group, column.key)">{{ groupText(entry.group, column.key) }}</span>
                </td>
                <td class="connection-actions cf-table__right"></td>
              </tr>
              <tr v-else-if="entry.kind === 'more'" class="connection-more-row">
                <td :colspan="visibleColumns.length + 1"><Button variant="ghost" size="sm" @click="showMore(entry.group.id)">继续显示本组 · 剩余 {{ entry.group.members.length - memberLimit(entry.group.id) }} 条</Button></td>
              </tr>
              <tr v-else :data-connection-id="entry.row.id" :class="{ 'connection-child': entry.parent, 'is-disabled': entry.row.closedAt }" @click="selectedId = entry.row.id">
                <td v-for="column in visibleColumns" :key="column.key" :data-column="column.key" :data-label="column.label" :class="{ 'cf-table__right num': column.numeric }">
                  <button v-if="column.key === 'target'" type="button" class="connection-host font-mono" :title="entry.row.target" :aria-label="'查看 ' + entry.row.target + ' 的连接详情'" @click.stop="selectedId = entry.row.id"><span class="connection-cell">{{ entry.row.target }}</span></button>
                  <span v-else class="connection-cell" :class="{ 'font-mono': column.key === 'source' && !sourceName(entry.row) }" :title="cellTitle(entry.row, column.key)">{{ cellText(entry.row, column.key) }}</span>
                </td>
              <td class="connection-actions cf-table__right" @click.stop>
                <div class="flex items-center justify-end">
                <DropdownMenu>
                  <DropdownMenuTrigger as-child><Button variant="ghost" size="icon" class="connection-icon" :aria-label="`操作 ${entry.row.target}`"><Ellipsis class="size-4" /></Button></DropdownMenuTrigger>
                  <DropdownMenuContent align="end" class="max-w-[calc(100vw-24px)]">
                    <DropdownMenuItem class="min-h-11" :disabled="!deviceAddress(entry.row.metadata.sourceIP)" @select="noteIP = deviceAddress(entry.row.metadata.sourceIP)"><Pencil class="size-4" />编辑来源设备备注</DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem class="min-h-11" :disabled="!connectionRuleValues(entry.row.metadata).length" @select="openRule(entry.row, 'proxy')">加入代理规则集</DropdownMenuItem>
                    <DropdownMenuItem class="min-h-11" :disabled="!connectionRuleValues(entry.row.metadata).length" @select="openRule(entry.row, 'direct')">加入直连规则集</DropdownMenuItem>
                    <DropdownMenuLabel v-if="!connectionRuleValues(entry.row.metadata).length" class="max-w-64 whitespace-normal text-xs font-normal text-muted-foreground">此连接没有可用的域名或真实目标 IP。</DropdownMenuLabel>
                  </DropdownMenuContent>
                </DropdownMenu>
                <Button v-if="!entry.row.closedAt" variant="ghost" size="icon" class="connection-icon" :aria-label="`断开 ${entry.row.target}`" title="断开" @click.stop="closeOne(entry.row.id)"><X class="size-4" /></Button>
                </div>
              </td>

              </tr>
            </template>
          </tbody>
        </table>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-2 border-t border-border px-3 py-2 text-xs text-muted-foreground">
        <span class="num">共 {{ rows.length }} 条连接<span v-if="settings.groupBy"> · {{ groups.length }} 组</span><span v-if="pageCount > 1"> · {{ (page - 1) * PAGE_SIZE + 1 }}–{{ Math.min(page * PAGE_SIZE, pageTotal) }}{{ settings.groupBy ? ' 组' : '' }}</span><span v-if="hiddenColumnCount" class="font-sans"> · 已收起 {{ hiddenColumnCount }} 列，点击连接查看详情</span></span>
        <div v-if="pageCount > 1" class="flex items-center gap-1">
          <Button variant="ghost" size="icon" class="size-11" aria-label="上一页连接" :disabled="page === 1" @click="page--"><ChevronLeft class="size-4" /></Button>
          <span class="num px-1">{{ page }} / {{ pageCount }}</span>
          <Button variant="ghost" size="icon" class="size-11" aria-label="下一页连接" :disabled="page === pageCount" @click="page++"><ChevronRight class="size-4" /></Button>
        </div>
      </div>
    </div>
    <DeviceNoteDialog v-if="noteIP" :key="agentId + noteIP" :agent-id="agentId" :ip="noteIP" @close="noteIP = ''" @saved="applyNotes" />
    <ConnectionRuleDialog v-if="ruleAction" :key="agentId" :agent-id="agentId" :values="ruleAction.values" :target="ruleAction.target" @close="ruleAction = null" />
    <MihomoConnectionDetail :agent-id="agentId" :row="selected" :source-name="selected ? sourceName(selected) : ''" @close="selectedId = ''" @proxy-changed="load" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useElementSize, useMediaQuery } from '@vueuse/core'
import { ChevronDown, Layers, ZoomIn, ZoomOut, ArrowDown, ArrowUp, ArrowUpDown, Cable, ChevronLeft, ChevronRight, Columns3, Ellipsis, Eraser, ListFilter, Loader2, Pencil, TriangleAlert, X } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { PopoverClose } from 'reka-ui'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import Toolbar from '@/components/common/Toolbar.vue'
import Segmented from '@/components/common/Segmented.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import FormField from '@/components/common/FormField.vue'
import MultiSelect from '@/components/common/MultiSelect.vue'
import MihomoConnectionDetail from './MihomoConnectionDetail.vue'
import DeviceNoteDialog from './DeviceNoteDialog.vue'
import ConnectionRuleDialog from './ConnectionRuleDialog.vue'
import { connectionRuleValues, deviceAddress, deviceNote, type DeviceNotes } from '@/lib/device-notes'
import { useTabActive } from '@/components/ui/tabs'
import { clientToolsApi, mihomoPanelApi } from '@/api'
import { confirmDanger, notify } from '@/lib/feedback'
import { formatBytes } from '@/lib/format'
import {
  compareSources, panelError, sourceLabel, type ConnectionRow, type MihomoConnection, type MihomoConnectionsSnapshot
} from '@/types/mihomo'
import {
  CONNECTION_COLUMNS, CONNECTION_SORT_OPTIONS, DEFAULT_CONNECTION_COLUMNS, CONNECTION_GROUP_FIELDS,
  connectionDisplayColumns, connectionNetworkLabel, connectionRuleLabel, defaultConnectionSortDirection, emptyConnectionFilters,
  filterConnections, loadConnectionTableSettings, saveConnectionTableSettings, sortConnections,
  type ConnectionColumn, type ConnectionSort, type ConnectionGroupField
} from '@/lib/mihomo-connections'

import { connectionText, groupConnections, type ConnectionGroup } from '@/lib/mihomo-connection-groups'

type ViewKey = 'active' | 'closed' | 'all'
const POLL_MS = 2000
const PAGE_SIZE = 100
const CLOSED_LIMIT = 500

const props = withDefaults(defineProps<{ agentId: string; active?: boolean }>(), { active: true })
// 所在标签是否为当前标签：由 Tabs 注入，切换标签不经过 props，隐藏的面板不会整表重新渲染
const active = useTabActive(() => props.active)
const panelElement = ref<HTMLElement | null>(null)
const { width: measuredWidth } = useElementSize(panelElement)
// 切到其他标签时面板隐藏、宽度读成 0：沿用上次的宽度，免得隐藏和切回时各把整张表按另一套列重排一次
const panelWidth = ref(0)
watch(measuredWidth, width => { if (width) panelWidth.value = width }, { immediate: true })
const touchPointer = useMediaQuery('(pointer: coarse)')
const actionsWidth = computed(() => touchPointer.value ? 100 : 76)

const notes = ref<Record<string, string>>({})
const notesError = ref('')
const noteIP = ref('')
const ruleAction = ref<{ values: string[]; target: 'proxy' | 'direct' } | null>(null)
const sourceName = (row: ConnectionRow) => deviceNote(notes.value, row.metadata.sourceIP)
let notesEpoch = 0
function applyNotes(catalog: DeviceNotes) {
  notesEpoch++; notes.value = catalog.notes; notesError.value = ''
}
async function loadNotes() {
  const generation = ++notesEpoch
  const agentId = props.agentId
  try {
    const { data } = await clientToolsApi.notes(agentId)
    if (generation !== notesEpoch || disposed) return
    if (!data?.success) throw new Error('读取备注失败。')
    // 切回标签时会重读备注；内容没变就不替换，免得整表再渲染一次
    if (JSON.stringify(data.notes) !== JSON.stringify(notes.value)) notes.value = data.notes
    notesError.value = ''
  } catch (failure) {
    if (generation === notesEpoch && !disposed) notesError.value = panelError(failure)
  }
}
function openRule(row: ConnectionRow, target: 'proxy' | 'direct') {
  const values = connectionRuleValues(row.metadata)
  if (values.length) ruleAction.value = { values, target }
}
const snapshot = ref<MihomoConnectionsSnapshot | null>(null)
const error = ref('')
const search = ref('')
const settings = ref(loadConnectionTableSettings())
const filters = ref(emptyConnectionFilters())
const view = ref<ViewKey>('active')
const page = ref(1)
const selectedId = ref('')
/** 两次采样之间消失的连接，按关闭时间倒序，只保存在本页 */
const closed = ref<ConnectionRow[]>([])
let lastById = new Map<string, MihomoConnection>()
const live = ref(true)
const closingAll = ref(false)
const now = ref(Date.now())
const speeds = ref(new Map<string, { up: number; down: number }>())
let previous: { at: number; byId: Map<string, { up: number; down: number }> } | null = null

function toRow(connection: MihomoConnection, closedAt?: number): ConnectionRow {
  const meta = connection.metadata
  const host = meta.host || meta.sniffHost || meta.destinationIP
  const speed = closedAt ? undefined : speeds.value.get(connection.id)
  return {
    ...connection,
    raw: connection,
    target: `${host}:${meta.destinationPort}`,
    chain: [...connection.chains].reverse().join(' → '),
    upSpeed: speed?.up || 0,
    downSpeed: speed?.down || 0,
    closedAt
  }
}

const activeRows = computed(() => (snapshot.value?.connections || []).map(connection => toRow(connection)))
const filterCount = computed(() => [filters.value.sources, filters.value.networks, filters.value.rules, filters.value.chains].filter(values => values.length).length + Number(!!filters.value.exclude.trim()))
const filtering = computed(() => !!search.value.trim() || filterCount.value > 0)
const sources = computed(() => [...new Set([...activeRows.value, ...closed.value].map(sourceLabel))].sort(compareSources))
const filterGroups = computed(() => {
  const all = [...activeRows.value, ...closed.value]
  // 保留已选但当前没有连接的选项，不能在刷新时悄悄放宽筛选范围。
  const options = (values: string[], selected: string[], compare = (a: string, b: string) => a.localeCompare(b)) =>
    [...new Set([...values, ...selected])].filter(Boolean).sort(compare).map(value => ({ value, label: value }))
  return [
    { key: 'sources' as const, label: '来源', options: options(sources.value, filters.value.sources, compareSources).map(option => ({ ...option, label: deviceNote(notes.value, option.value) ? `${deviceNote(notes.value, option.value)} · ${option.value}` : option.label })) },
    { key: 'networks' as const, label: '协议', options: options(all.map(connectionNetworkLabel), filters.value.networks) },
    { key: 'rules' as const, label: '规则', options: options(all.map(connectionRuleLabel), filters.value.rules) },
    { key: 'chains' as const, label: '链路', options: options(all.flatMap(row => row.chains), filters.value.chains) }
  ]
})

function clearFilters() {
  filters.value = emptyConnectionFilters()
  search.value = ''
}
function focusFilters(event: Event) {
  event.preventDefault()
  document.getElementById('connection-filter-sources')?.focus()
}
const activeMatches = computed(() => filterConnections(activeRows.value, search.value, filters.value, sourceName))

const rows = computed(() => {
  const list = view.value === 'active' ? activeMatches.value
    : filterConnections(view.value === 'closed' ? closed.value : [...activeRows.value, ...closed.value], search.value, filters.value, sourceName)
  return sortConnections(list, settings.value.sort, settings.value.direction, now.value)
})
const visibleColumns = computed(() => connectionDisplayColumns(settings.value.columns, panelWidth.value || 1400, actionsWidth.value + 16, settings.value.preferredColumns))
const visibleColumnKeys = computed(() => new Set(visibleColumns.value.map(column => column.key)))
const hiddenColumnCount = computed(() => settings.value.columns.length - visibleColumns.value.length)
const columnLabel = (key: ConnectionColumn) => CONNECTION_COLUMNS.find(column => column.key === key)!.label
function columnStyle(column: typeof visibleColumns.value[number]) {
  // Text columns share spare space; counters keep a predictable compact width.
  if (column.numeric || column.key === 'network') return { width: `${column.width}px` }
  const fixed = visibleColumns.value.filter(item => item.numeric || item.key === 'network').reduce((sum, item) => sum + item.width, actionsWidth.value)
  const flexible = visibleColumns.value.filter(item => !item.numeric && item.key !== 'network')
  const total = flexible.reduce((sum, item) => sum + item.width, 0)
  const available = (panelWidth.value || 1400) - fixed - 16
  return { width: `${Math.max(1, available * column.width / total)}px` }
}
function cellText(row: ConnectionRow, key: ConnectionColumn) {
  if (key === 'downSpeed' || key === 'upSpeed') return row.closedAt ? '—' : speedText(row[key])
  if (key === 'download' || key === 'upload') return formatBytes(row[key])
  if (key === 'duration') return durationText(row.start, row.closedAt)
  if (key === 'start') return startText(row.start)
  return connectionText(row, key, sourceName)
}
function cellTitle(row: ConnectionRow, key: ConnectionColumn) {
  return key === 'source' ? [sourceName(row), sourceLabel(row)].filter(Boolean).join(' · ') : cellText(row, key)
}
const isGroupField = (key: ConnectionColumn): key is ConnectionGroupField => (CONNECTION_GROUP_FIELDS as readonly string[]).includes(key)
const expanded = ref(new Set<string>())
const memberLimits = ref(new Map<string, number>())
const groups = computed(() => settings.value.groupBy ? groupConnections(rows.value, settings.value.groupBy, settings.value.sort, settings.value.direction, now.value, sourceName) : [])
function setGroup(key: string) {
  settings.value.groupBy = isGroupField(key as ConnectionColumn) ? key as ConnectionGroupField : null
  expanded.value = new Set(); memberLimits.value = new Map(); page.value = 1
}
function toggleGroup(key: ConnectionGroupField) { setGroup(settings.value.groupBy === key ? 'none' : key) }
function toggleExpanded(id: string) {
  const next = new Set(expanded.value)
  if (next.has(id)) { next.delete(id); memberLimits.value.delete(id) } else next.add(id)
  expanded.value = next
}
const memberLimit = (id: string) => memberLimits.value.get(id) || 20
function showMore(id: string) { memberLimits.value.set(id, memberLimit(id) + 20) }
function groupTitle(group: ConnectionGroup) {
  return `${group.label} · ${group.members.length} 条连接\n下载 ${formatBytes(group.download)} · 上传 ${formatBytes(group.upload)}\n下载速度 ${speedText(group.downSpeed)} · 上传速度 ${speedText(group.upSpeed)} · 最长 ${elapsedText(group.duration)}`
}
function groupText(group: ConnectionGroup, key: ConnectionColumn) {
  if (key === 'download' || key === 'upload') return formatBytes(group[key])
  if (key === 'downSpeed' || key === 'upSpeed') return speedText(group[key])
  if (key === 'duration') return `最长 ${elapsedText(group.duration)}`
  if (key === 'start') return Number.isFinite(group.start) ? startText(new Date(group.start).toISOString()) : '—'
  return group.values[key] || '—'
}
watch(groups, current => {
  const ids = new Set(current.map(group => group.id))
  for (const id of expanded.value) if (!ids.has(id)) { expanded.value.delete(id); memberLimits.value.delete(id) }
})
function changeSort(key: ConnectionSort, toggle = false) {
  settings.value.direction = toggle && settings.value.sort === key
    ? settings.value.direction === 'asc' ? 'desc' : 'asc'
    : defaultConnectionSortDirection(key)
  settings.value.sort = key
}
function toggleColumn(key: ConnectionColumn, visible: boolean) {
  if (key === 'target') return
  settings.value.columns = CONNECTION_COLUMNS.filter(column => column.key === key ? visible : settings.value.columns.includes(column.key)).map(column => column.key)
  const preferred = (settings.value.preferredColumns || []).filter(column => column !== key)
  if (visible) preferred.push(key)
  if (preferred.length) settings.value.preferredColumns = preferred
  else delete settings.value.preferredColumns
}
function resetColumns() {
  settings.value.columns = [...DEFAULT_CONNECTION_COLUMNS]
  delete settings.value.preferredColumns
}
const viewOptions = computed<Array<{ value: ViewKey; label: string }>>(() => [
  { value: 'active', label: `活跃 (${activeRows.value.length})` },
  { value: 'closed', label: `已关闭 (${closed.value.length})` },
  { value: 'all', label: '全部' }
])
const selected = computed(() => selectedId.value
  ? activeRows.value.find(row => row.id === selectedId.value) || closed.value.find(row => row.id === selectedId.value) || null
  : null)
const pageTotal = computed(() => settings.value.groupBy ? groups.value.length : rows.value.length)
const pageCount = computed(() => Math.max(1, Math.ceil(pageTotal.value / PAGE_SIZE)))
type TableEntry = { kind: 'group'; id: string; group: ConnectionGroup } | { kind: 'more'; id: string; group: ConnectionGroup } | { kind: 'connection'; id: string; row: ConnectionRow; parent?: string }
const visibleEntries = computed<TableEntry[]>(() => {
  const begin = (page.value - 1) * PAGE_SIZE, end = page.value * PAGE_SIZE
  if (!settings.value.groupBy) return rows.value.slice(begin, end).map(row => ({ kind: 'connection', id: row.id, row }))
  const entries: TableEntry[] = []
  for (const group of groups.value.slice(begin, end)) {
    entries.push({ kind: 'group', id: group.id, group })
    if (!expanded.value.has(group.id)) continue
    entries.push(...group.members.slice(0, memberLimit(group.id)).map(row => ({ kind: 'connection' as const, id: row.id, row, parent: group.id })))
    if (group.members.length > memberLimit(group.id)) entries.push({ kind: 'more', id: group.id + ':more', group })
  }
  return entries
})
watch([search, view, filters, () => settings.value.sort, () => settings.value.direction], () => { page.value = 1 }, { deep: true })
watch(pageCount, count => { page.value = Math.min(page.value, count) })
watch(settings, saveConnectionTableSettings, { deep: true })

function speedText(bytesPerSecond: number) {
  return bytesPerSecond > 0 ? formatBytes(bytesPerSecond) + '/s' : '—'
}

function durationText(start: string, end = now.value) {
  return elapsedText(end - Date.parse(start))
}
function elapsedText(milliseconds: number) {
  const seconds = Math.max(0, Math.floor(milliseconds / 1000))
  if (!Number.isFinite(seconds)) return '—'
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86400)}d`
}

function startText(start: string) {
  return Number.isFinite(Date.parse(start)) ? new Date(start).toLocaleString() : '—'
}

function updateSpeeds(data: MihomoConnectionsSnapshot) {
  const at = Date.now()
  const byId = new Map((data.connections || []).map(c => [c.id, { up: c.upload, down: c.download }]))
  const next = new Map<string, { up: number; down: number }>()
  if (previous) {
    const seconds = Math.max((at - previous.at) / 1000, 0.5)
    for (const [id, current] of byId) {
      const before = previous.byId.get(id)
      if (before) next.set(id, { up: Math.max(0, (current.up - before.up) / seconds), down: Math.max(0, (current.down - before.down) / seconds) })
    }
  }
  speeds.value = next
  previous = { at, byId }
  now.value = at
}

let timer: ReturnType<typeof setTimeout> | null = null
let controller: AbortController | null = null
let disposed = false

async function load() {
  controller?.abort()
  controller = new AbortController()
  try {
    const { data } = await mihomoPanelApi.connections(props.agentId, { signal: controller.signal })
    const result: MihomoConnectionsSnapshot = data.data || { connections: [], uploadTotal: 0, downloadTotal: 0 }
    updateSpeeds(result)
    recordClosed(result.connections || [])
    snapshot.value = result
    error.value = ''
  } catch (failure: any) {
    if (failure?.code === 'ERR_CANCELED') return
    error.value = panelError(failure, '暂时无法读取连接。')
  }
}

function recordClosed(current: MihomoConnection[]) {
  const next = new Map(current.map(connection => [connection.id, connection]))
  const at = Date.now()
  const gone = [...lastById.values()].filter(connection => !next.has(connection.id)).map(connection => toRow(connection, at))
  if (gone.length) closed.value = [...gone, ...closed.value].slice(0, CLOSED_LIMIT)
  lastById = next
}

function schedule() {
  if (timer) clearTimeout(timer)
  timer = null
  if (disposed || !active.value || !live.value) return
  timer = setTimeout(async () => {
    if (document.visibilityState === 'visible') await load()
    schedule()
  }, POLL_MS)
}

async function closeOne(id: string) {
  try {
    await mihomoPanelApi.closeConnection(props.agentId, id)
    if (snapshot.value?.connections) snapshot.value.connections = snapshot.value.connections.filter(c => c.id !== id)
  } catch (failure) {
    notify.error('断开连接失败', panelError(failure))
  }
}

async function closeAll() {
  if (closingAll.value || view.value === 'closed') return
  // 在确认前冻结筛选范围：翻页、轮询或修改筛选均不能扩大将要断开的连接范围。
  const filtered = filtering.value
  const targets = activeMatches.value.map(row => row.id)
  const agentId = props.agentId
  if (!targets.length) return
  const description = filtered ? `将断开当前筛选出的 ${targets.length} 条活动连接（包含其他页）。正在进行的下载和会话会中断。` : '将断开 Mihomo 上的全部活动连接，正在进行的下载和会话会中断。'
  closingAll.value = true
  try {
    if (!await confirmDanger(description, { title: '断开连接', confirmText: '断开' })) return
    if (disposed || props.agentId !== agentId) return
    if (filtered) {
      let failed = 0
      // 大列表限制并发，避免批量操作同时挤满 Agent 的请求队列。
      for (let offset = 0; offset < targets.length; offset += 8) {
        if (disposed || props.agentId !== agentId) return
        const results = await Promise.allSettled(targets.slice(offset, offset + 8).map(id => mihomoPanelApi.closeConnection(agentId, id)))
        failed += results.filter(result => result.status === 'rejected').length
      }
      if (failed) notify.warning(`有 ${failed} 条连接断开失败`)
      else notify.success(`已断开 ${targets.length} 条连接`)
    } else {
      await mihomoPanelApi.closeAllConnections(agentId)
      notify.success('已断开全部连接')
    }
    await load()
  } catch (failure) {
    notify.error('断开连接失败', panelError(failure))
  } finally {
    closingAll.value = false
  }
}

watch(() => [active.value, live.value], ([shown, polling]) => {
  // 暂停后再恢复时旧样本已过时，不能拿来算速率
  if (!shown || !polling) previous = null
  else load()
  // 切走后面板保持挂载，未返回的读取不再更新隐藏的列表；暂停实时刷新时仍让它读完
  if (!shown) controller?.abort()
  schedule()
})
onMounted(async () => {
  if (active.value) await load()
  schedule()
})
onUnmounted(() => {
  disposed = true
  if (timer) clearTimeout(timer)
  controller?.abort()
})
watch(() => props.agentId, () => { expanded.value = new Set(); memberLimits.value = new Map(); notesEpoch++; notes.value = {}; notesError.value = ''; noteIP.value = ''; ruleAction.value = null; void loadNotes() })
watch(active, shown => { if (shown) void loadNotes() })
onMounted(loadNotes)
onUnmounted(() => { notesEpoch++ })
</script>

<style scoped>
.connection-table-frame { container-type: inline-size; }
.connection-table-scroll { scrollbar-gutter: stable; }
.connection-table { width: 100%; }
.connection-table th { position: sticky; top: 0; z-index: 1; padding: 2px 6px; background: var(--card); white-space: nowrap; }
.connection-table td { padding: 2px 6px; height: 36px; white-space: nowrap; }
.connection-table .connection-actions { width: 76px; }
.connection-heading { display: flex; align-items: center; gap: 2px; }
.connection-sort, .connection-group-label { display: flex; min-width: 0; min-height: 28px; align-items: center; gap: 3px; border-radius: var(--radius-sm); color: inherit; }
.connection-sort { white-space: nowrap; }
.connection-sort svg { flex-shrink: 0; }
.connection-icon { display: inline-flex; width: 28px; height: 28px; min-width: 28px; align-items: center; justify-content: center; border-radius: var(--radius-sm); }
.connection-sort[data-active='true'], .connection-group-toggle[data-active='true'], .connection-sort:hover { color: var(--primary-accent); }
.connection-group-toggle[data-active='true'] { background: var(--primary-soft); }
.connection-group-toggle:hover { background: var(--accent); }
.connection-sort:focus-visible, .connection-group-label:focus-visible, .connection-group-toggle:focus-visible, .connection-host:focus-visible { outline: 2px solid var(--ring); outline-offset: -2px; }
.connection-cell { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.connection-host { width: 100%; min-height: 28px; text-align: left; border-radius: var(--radius-sm); }
.connection-host:hover { color: var(--primary-accent); }
.connection-group-row { background: var(--secondary); }
.connection-group-label { width: 100%; text-align: left; }
.connection-child td:first-child { padding-left: 24px; }
.connection-table-scroll:focus-visible { outline: 2px solid var(--ring); outline-offset: -2px; }
.connection-filter-fields :deep([role='combobox']) { min-height: 44px; }
@media (pointer: coarse) {
  .connection-icon { width: 44px; min-width: 44px; height: 44px; }
  .connection-sort, .connection-host, .connection-group-label { min-width: 44px; min-height: 44px; }
  .connection-table .connection-actions { width: 100px; }
}
/* The same records and actions reflow on narrow panels, with touch-friendly targets. */
@container (max-width: 740px) {
  .connection-table, .connection-table tbody { display: block; }
  .connection-table thead { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  .connection-table tbody tr { position: relative; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px 16px; padding: 12px; border-bottom: 1px solid var(--border); }
  .connection-table tbody tr:last-child { border-bottom: 0; }
  .connection-table td { display: block; height: auto; min-width: 0; padding: 0; border-bottom: 0; text-align: left; }
  .connection-table td[data-column]::before { content: attr(data-label); display: block; margin-bottom: 2px; color: var(--muted-foreground); font-family: var(--font-sans); font-size: 11px; }
  .connection-table td[data-column='target'] { grid-column: 1 / -1; padding-right: 88px; }
  .connection-table td[data-column='target']::before { display: none; }
  .connection-table td[data-column='chain'] { grid-column: 1 / -1; }
  .connection-table td.connection-actions { position: absolute; top: 12px; right: 8px; width: 88px; }
  .connection-icon { width: 44px; min-width: 44px; height: 44px; }
  .connection-host, .connection-group-label { min-height: 44px; }
  .connection-cell { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; white-space: normal; overflow-wrap: anywhere; }
  .connection-table td[data-column='rule'] { grid-column: 1 / -1; }
  .connection-table .connection-more-row { display: block; }
  .connection-group-label { flex-wrap: wrap; }
  .connection-group-row td[data-column='target'] { padding-right: 0; }
  .connection-group-label .truncate { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; white-space: normal; overflow-wrap: anywhere; }
}
</style>
