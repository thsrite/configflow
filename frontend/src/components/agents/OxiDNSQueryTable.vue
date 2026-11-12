<template>
  <div class="space-y-4">
    <!-- 数据来源、时间范围、结果三个选择与下方四个输入框各定义一次：桌面平铺，手机收进底部「筛选」面板，绑定同一份草稿 -->
    <DefineSelects v-slot="{ inSheet }">
      <div :class="inSheet ? 'min-w-0 col-span-2 text-xs text-muted-foreground' : 'contents'">
        <span v-if="inSheet" class="mb-1.5 block">数据来源</span>
        <Select :model-value="recorder" @update:model-value="value => emit('recorder', String(value))">
          <SelectTrigger :class="inSheet ? 'min-h-11 w-full bg-card' : 'min-h-11 w-full max-sm:col-span-2 sm:w-[190px]'" aria-label="查询记录器"><SelectValue /></SelectTrigger>
          <SelectContent><SelectItem v-for="tag in recorders" :key="tag" :value="tag">{{ tag }}</SelectItem></SelectContent>
        </Select>
      </div>
      <div :class="inSheet ? 'min-w-0 text-xs text-muted-foreground' : 'contents'">
        <span v-if="inSheet" class="mb-1.5 block">时间范围</span>
        <Select :model-value="draft.range" @update:model-value="selectFilter('range', $event as OxiDNSQueryFilters['range'])">
          <SelectTrigger :class="inSheet ? 'min-h-11 w-full bg-card' : 'min-h-11 w-[140px]'" aria-label="查询时间范围"><SelectValue /></SelectTrigger>
          <SelectContent><SelectItem value="1h">最近 1 小时</SelectItem><SelectItem value="24h">最近 24 小时</SelectItem><SelectItem value="all">全部时间</SelectItem></SelectContent>
        </Select>
      </div>
      <div :class="inSheet ? 'min-w-0 text-xs text-muted-foreground' : 'contents'">
        <span v-if="inSheet" class="mb-1.5 block">查询结果</span>
        <Select :model-value="draft.status" @update:model-value="selectFilter('status', $event as OxiDNSQueryFilters['status'])">
          <SelectTrigger :class="inSheet ? 'min-h-11 w-full bg-card' : 'min-h-11 w-[140px]'" aria-label="查询结果筛选"><SelectValue /></SelectTrigger>
          <SelectContent><SelectItem value="all">全部结果</SelectItem><SelectItem value="error">处理错误</SelectItem><SelectItem value="has_response">有应答</SelectItem><SelectItem value="no_response">无应答</SelectItem></SelectContent>
        </Select>
      </div>
    </DefineSelects>
    <DefineFields>
      <div class="min-w-0 text-xs text-muted-foreground"><span>客户端 IP</span><OxiDNSClientSelect class="mt-1.5" :model-value="draft.client_ip" :agent-id="agentId" :recorder="recorder" :range="draft.range" :collected-at="collectedAt" :notes="notes" @update:model-value="selectClient" /></div>
      <label class="text-xs text-muted-foreground">查询类型<Input v-model="draft.qtype" class="mt-1.5 min-h-11 bg-card font-mono" placeholder="例如 A、AAAA" /></label>
      <label class="text-xs text-muted-foreground">响应码<Input v-model="draft.rcode" class="mt-1.5 min-h-11 bg-card font-mono" placeholder="例如 NOERROR" /></label>
      <label class="text-xs text-muted-foreground">匹配器标签<Input v-model="draft.matcher_tag" class="mt-1.5 min-h-11 bg-card font-mono" placeholder="按实际执行标签查询" /></label>
    </DefineFields>
    <form @submit.prevent="emit('filter', { ...draft })">
      <Toolbar v-model:search="draft.qname" placeholder="查询域名" class="[&_input]:min-h-11">
        <template v-if="!isPhone" #filters><ReuseSelects /></template>
        <template #actions>
          <Button v-if="isPhone" type="button" variant="outline" class="relative size-11 p-0" :aria-label="activeFilterCount ? `筛选，已启用 ${activeFilterCount} 项` : '筛选'" title="筛选" aria-haspopup="dialog" :aria-expanded="filtersOpen" @click="filtersOpen = true">
            <ListFilter class="size-4" /><span v-if="activeFilterCount" class="query-filter-count num" aria-hidden="true">{{ activeFilterCount }}</span>
          </Button>
          <Button type="submit" class="min-h-11" :disabled="loading"><Search class="size-4" />查询</Button>
        </template>
      </Toolbar>
      <div v-if="!isPhone" class="grid grid-cols-2 gap-3 md:grid-cols-4"><ReuseFields /></div>
    </form>
    <Sheet v-if="isPhone" v-model:open="filtersOpen">
      <SheetContent side="bottom" :show-close-button="false"
        class="max-h-[85dvh] gap-0 rounded-t-2xl border-border bg-card pb-[env(safe-area-inset-bottom)] shadow-overlay data-[state=open]:duration-200 data-[state=closed]:duration-150">
        <SheetHeader class="shrink-0 flex-row items-center justify-between gap-3 p-4 pb-2">
          <div class="min-w-0">
            <SheetTitle class="text-base">筛选查询记录</SheetTitle>
            <SheetDescription class="mt-1 text-xs">选择项立即生效；输入框填好后点「应用」。</SheetDescription>
          </div>
          <SheetClose as-child>
            <Button variant="ghost" size="icon" class="size-11 shrink-0" aria-label="关闭筛选"><X class="size-5" aria-hidden="true" /></Button>
          </SheetClose>
        </SheetHeader>
        <form class="min-h-0 overflow-y-auto overscroll-contain px-4 pt-1 pb-4" aria-label="查询记录筛选" @submit.prevent="applySheet">
          <div class="grid grid-cols-2 gap-3"><ReuseSelects :in-sheet="true" /><ReuseFields /></div>
          <div class="mt-4 flex gap-2">
            <Button type="button" variant="outline" class="min-h-11 flex-1" :disabled="loading || !sheetFilterCount" @click="clearSheet">清除筛选</Button>
            <Button type="submit" class="min-h-11 flex-1" :disabled="loading">应用</Button>
          </div>
        </form>
      </SheetContent>
    </Sheet>
    <p v-if="clockSkewMs != null && Math.abs(clockSkewMs) >= 300000" role="status" class="m-0 text-xs text-warning-accent">浏览器与 DNS 设备的时钟相差较大，查询范围已按 DNS 设备时间计算。请核对两端的系统时间。</p>
    <p v-if="error" class="m-0 rounded-lg border border-warning-accent/30 bg-warning-soft px-3 py-2.5 text-sm text-warning-accent" role="alert">
      {{ error }}<span v-if="data"> 保留上次成功读取的结果。</span>
      <Button variant="ghost" class="ml-2 min-h-11" :disabled="loading" @click="emit('retry')">重试</Button>
    </p>
    <LoadingRows v-if="loading && !data" :rows="5" />
    <EmptyState v-else-if="!data && error" title="查询记录读取失败" description="请检查核心连接后重试。" />
    <EmptyState v-else-if="data && !data.records.length" :icon="Search" :title="hasNext ? '这一页暂无匹配的查询记录' : '没有匹配的查询记录'" :description="hasNext ? '还有更早的记录，可点击下一页继续查找。' : '可以调整时间范围或筛选条件。查询记录由核心记录器提供。'" />
    <DataTableShell v-else-if="data" :class="isPhone && 'oxidns-query-phone'" :footer="'本页 ' + data.records.length + ' 条 · 第 ' + page + ' 页'">
      <TableHeader><TableRow><TableHead><span class="inline-flex items-center gap-1">时间<FieldHelp label="查询时间" :text="`按浏览器时区（${queryTimeZone()}）显示。最近 1 小时和最近 24 小时按 DNS 设备的采样时间计算，翻页时保留原时间范围。全部时间不设起止限制。`" /></span></TableHead><TableHead>查询</TableHead><TableHead>客户端</TableHead><TableHead>结果</TableHead><TableHead>解析来源</TableHead><TableHead class="text-right">耗时</TableHead><TableHead class="text-right">详情</TableHead></TableRow></TableHeader>
      <TableBody>
        <TableRow v-for="record in data.records" :key="record.id" class="[&>td]:py-1.5">
          <TableCell class="num text-xs" :title="isPhone ? formatQueryTime(record.created_at_ms) : undefined">{{ isPhone ? formatQueryTimeShort(record.created_at_ms) : formatQueryTime(record.created_at_ms) }}</TableCell>
          <TableCell class="max-w-[320px]">
            <div v-for="(question, index) in record.questions_json" :key="index" class="flex items-center gap-2">
              <span class="min-w-0 truncate font-mono text-xs" :title="question.name">{{ question.name }}</span><span class="chip shrink-0 font-mono">{{ formatDNSType(question.qtype) }}</span>
            </div>
            <span v-if="!record.questions_json?.length" class="text-muted-foreground">—</span>
          </TableCell>
          <TableCell class="max-w-72 whitespace-normal text-xs">
            <span class="flex flex-wrap items-center gap-x-2 gap-y-0.5"><span v-if="note(record.client_ip)" class="break-all">{{ note(record.client_ip) }}</span><span :class="['break-all font-mono', { 'text-muted-foreground': note(record.client_ip) }]">{{ record.client_ip || '—' }}</span><span v-if="isAgentProbe(record)" class="chip shrink-0 font-sans" :title="AGENT_PROBE_NOTE">Agent 检查<span class="sr-only">：{{ AGENT_PROBE_NOTE }}</span></span></span>
          </TableCell>
          <TableCell>
            <span :class="['chip', record.error ? 'chip-bad' : record.has_response ? 'chip-ok' : 'chip-warn']">
              {{ record.error ? '处理错误' : record.has_response ? formatDNSCode(record.rcode) : '无应答' }}
            </span>
          </TableCell>
          <TableCell class="text-xs" title="根据执行轨迹显示转发组、缓存或本地策略">
            <Button v-if="sourceOf(record).failed" variant="ghost" :class="[ROW_ACTION, 'query-source-retry text-warning-accent']" :aria-label="'重试读取查询 ' + record.id + ' 的解析来源'" @click="retrySource(record)">读取失败 · 重试</Button>
            <span v-else class="chip">{{ sourceOf(record).label }}</span>
          </TableCell>
          <TableCell class="num text-right">{{ formatMonitorNumber(record.elapsed_ms, 2) }}<span v-if="monitorNumber(record.elapsed_ms)"> ms</span></TableCell>
          <TableCell class="text-right"><Button variant="ghost" :class="[ROW_ACTION, 'query-detail']" :aria-label="'查看查询 ' + record.id + ' 详情'" @click="emit('detail', record)"><ChevronRight v-if="isPhone" class="size-4" aria-hidden="true" /><template v-else>查看</template></Button></TableCell>
        </TableRow>
      </TableBody>
    </DataTableShell>
    <p v-if="data?.records.length" class="m-0 text-xs leading-relaxed text-muted-foreground">解析来源根据执行轨迹归纳，显示转发组、缓存或本地策略；详情保留完整路径。</p>
    <div v-if="data || page > 1" class="flex flex-wrap items-center justify-between gap-3">
      <p class="m-0 text-xs text-muted-foreground">{{ page > 1 ? '浏览历史页时暂停记录自动刷新，保留当前页。' : '记录按时间倒序排列，每页最多 50 条。' }}</p>
      <div class="flex items-center gap-2 max-sm:w-full max-sm:[&>button]:flex-1">
        <Button variant="outline" class="min-h-11" :disabled="loading || page <= 1" @click="emit('previous')">上一页</Button>
        <Button variant="outline" class="min-h-11" :disabled="loading || !hasNext" @click="emit('next')">下一页</Button>
      </div>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ChevronRight, ListFilter, Search, X } from '@lucide/vue'
import { createReusableTemplate, useMediaQuery } from '@vueuse/core'
import Toolbar from '@/components/common/Toolbar.vue'
import DataTableShell from '@/components/common/DataTableShell.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import OxiDNSClientSelect from '@/components/agents/OxiDNSClientSelect.vue'
import { deviceNote } from '@/lib/device-notes'
import { useOxiDNSQuerySources } from '@/composables/useOxiDNSQuerySources'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { AGENT_PROBE_NOTE, isAgentProbe, formatDNSType, formatDNSCode, formatMonitorNumber, formatQueryTime, queryTimeZone, monitorNumber, type OxiDNSQueries, type OxiDNSQuery, type OxiDNSQueryFilters, emptyQueryFilters } from '@/types/oxidnsMonitor'
const props = defineProps<{
  agentId: string; recorders: string[]; recorder: string; instanceId?: string | null; filters: OxiDNSQueryFilters; data: OxiDNSQueries | null
  loading: boolean; error: string; page: number; hasNext: boolean
  collectedAt?: string; clockSkewMs?: number | null; notes?: Record<string, string>
}>()
const emit = defineEmits<{
  filter: [value: OxiDNSQueryFilters]; recorder: [value: string]; detail: [value: OxiDNSQuery]
  next: []; previous: []; retry: []
}>()
// 行内操作看起来 28px 高，上下各 8px 透明区仍可点，点击区保持 44px，不撑高整行
const ROW_ACTION = "relative h-7 min-h-0 px-2.5 max-md:min-h-0 after:absolute after:inset-x-0 after:-inset-y-2 after:content-['']"
const note = (ip: unknown) => typeof ip === 'string' ? deviceNote(props.notes || {}, ip) : ''
const draft = reactive({ ...props.filters })
const { sourceOf, retry: retrySource } = useOxiDNSQuerySources(props)
function selectFilter<K extends 'range' | 'status'>(key: K, value: OxiDNSQueryFilters[K]) {
  if (draft[key] === value) return
  draft[key] = value
  emit('filter', { ...draft })
}
function selectClient(value: string) {
  draft.client_ip = value
  emit('filter', { ...draft })
}
watch(() => props.filters, value => Object.assign(draft, value))

// 手机（< 640px）：搜索框与「查询」常驻，其余条件收进底部面板；面板与桌面共用同一份草稿和事件
const isPhone = useMediaQuery('(max-width: 639px)')
const [DefineSelects, ReuseSelects] = createReusableTemplate<{ inSheet?: boolean }>()
const [DefineFields, ReuseFields] = createReusableTemplate()
const filtersOpen = ref(false)
watch(isPhone, phone => { if (!phone) filtersOpen.value = false })
type SheetKey = Exclude<keyof OxiDNSQueryFilters, 'qname'>
const SHEET_KEYS: SheetKey[] = ['range', 'status', 'client_ip', 'qtype', 'rcode', 'matcher_tag']
const DEFAULTS = emptyQueryFilters()
const countActive = (value: OxiDNSQueryFilters) => SHEET_KEYS.filter(key => String(value[key] ?? '').trim() !== DEFAULTS[key]).length
// 角标只数已经生效的条件；面板里还没应用的输入不计入
const activeFilterCount = computed(() => countActive(props.filters))
const sheetFilterCount = computed(() => Math.max(countActive(draft), activeFilterCount.value))
function applySheet() {
  emit('filter', { ...draft })
  filtersOpen.value = false
}
function clearSheet() {
  for (const key of SHEET_KEYS) (draft as Record<SheetKey, string>)[key] = DEFAULTS[key]
  emit('filter', { ...draft })
}
// 手机行内只显示时间；不是今天的记录前面加月日。完整时间放在悬停提示里
function formatQueryTimeShort(value: number) {
  if (!Number.isFinite(value)) return '—'
  const date = new Date(value)
  const now = new Date()
  const sameDay = date.getFullYear() === now.getFullYear() && date.getMonth() === now.getMonth() && date.getDate() === now.getDate()
  const time = date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' })
  return sameDay ? time : date.toLocaleDateString(undefined, { month: '2-digit', day: '2-digit' }) + ' ' + time
}
</script>

<style scoped>
/* 筛选按钮右上角的条件数 */
.query-filter-count {
  position: absolute;
  top: 3px;
  right: 3px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--primary);
  color: var(--primary-foreground);
  font-size: 11px;
  font-weight: 600;
}

/* 手机记录：第一行域名（等宽、截断）与时间，第二行客户端 · 结果 · 来源 · 耗时；整行点开详情 */
.oxidns-query-phone :deep(thead) {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}

.oxidns-query-phone :deep(table),
.oxidns-query-phone :deep(tbody) {
  display: block;
}

.oxidns-query-phone :deep(tbody tr) {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, auto) auto minmax(0, 1fr) auto auto;
  grid-template-areas:
    'query query query time detail'
    'client result source elapsed detail';
  align-items: center;
  gap: 2px 8px;
  padding: 8px 4px 8px 12px;
}

.oxidns-query-phone :deep(tbody td) {
  display: block;
  min-width: 0;
  max-width: none;
  padding: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: 11.5px;
  line-height: 20px;
  color: var(--muted-foreground);
}

.oxidns-query-phone :deep(td:nth-child(1)) { grid-area: time; text-align: right; }
.oxidns-query-phone :deep(td:nth-child(2)) { grid-area: query; color: var(--foreground); }
.oxidns-query-phone :deep(td:nth-child(3)) { grid-area: client; }
.oxidns-query-phone :deep(td:nth-child(4)) { grid-area: result; }
.oxidns-query-phone :deep(td:nth-child(5)) { grid-area: source; }
.oxidns-query-phone :deep(td:nth-child(6)) { grid-area: elapsed; }
.oxidns-query-phone :deep(td:nth-child(7)) { grid-area: detail; align-self: stretch; display: flex; align-items: center; }

.oxidns-query-phone :deep(td:nth-child(2) span.truncate) {
  font-size: 13px;
}

.oxidns-query-phone :deep(td:nth-child(3) > span) {
  flex-wrap: nowrap;
}

.oxidns-query-phone :deep(td:nth-child(3) > span > span) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  word-break: normal;
}

.oxidns-query-phone :deep(td:nth-child(3) > span > .chip) {
  flex-shrink: 0;
}

.oxidns-query-phone :deep(.chip) {
  height: 20px;
  padding: 0 7px;
  font-size: 11px;
}

/* 来源在手机上只写文字，不再套胶囊 */
.oxidns-query-phone :deep(td:nth-child(5) .chip) {
  display: inline;
  height: auto;
  padding: 0;
  border: 0;
  background: none;
  font-size: inherit;
}

.oxidns-query-phone :deep(td:nth-child(5))::before,
.oxidns-query-phone :deep(td:nth-child(6))::before {
  content: '· ';
}

/* 「查看」铺满整行作为点击区；行内其他按钮浮在它上面 */
.oxidns-query-phone :deep(.query-detail) {
  position: static;
  width: 24px;
  height: 24px;
  min-width: 0;
  min-height: 0;
  padding: 0;
  color: var(--muted-foreground);
}

.oxidns-query-phone :deep(.query-detail)::after {
  inset: 0;
}

.oxidns-query-phone :deep(td:nth-child(5):has(.query-source-retry)) {
  overflow: visible;
}

.oxidns-query-phone :deep(.query-source-retry) {
  z-index: 1;
  height: 20px;
  padding: 0 4px;
  font-size: 11.5px;
}
</style>
