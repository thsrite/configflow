<template>
  <div class="min-w-0">
    <Toolbar v-model:search="search" placeholder="搜索日志">
      <template #filters>
        <Segmented :model-value="mode" :options="MODES" label="日志来源方式" @update:model-value="changeMode" />
        <Select :model-value="level" @update:model-value="changeLevel(String($event))">
          <SelectTrigger class="min-h-11 w-44 max-w-full" aria-label="最低日志等级"><SelectValue placeholder="日志等级" /></SelectTrigger>
          <SelectContent>
            <SelectItem v-for="item in MIHOMO_LOG_LEVELS" :key="item.value" :value="item.value">
              {{ item.label }}{{ item.value === configuredLevel ? ' · 配置' : '' }}
            </SelectItem>
          </SelectContent>
        </Select>
      </template>
      <template #actions>
        <Button variant="outline" class="min-h-11" :aria-pressed="paused" @click="paused = !paused">
          <Play v-if="paused" class="size-4" /><Pause v-else class="size-4" />{{ paused ? '恢复刷新' : '暂停刷新' }}
        </Button>
        <Button variant="outline" class="min-h-11" :aria-busy="loading" :disabled="loading" @click="refresh">
          <RefreshCw class="size-4" :class="{ 'animate-spin': loading }" />刷新日志
        </Button>
      </template>
    </Toolbar>

    <div class="mb-3 flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
      <span role="status">{{ refreshStatus }}</span>
      <span v-if="mode === 'file'">最近 {{ MAX_LINES }} 行<span v-if="search.trim()"> · 匹配 {{ matchingLines.length }} 行</span></span>
      <span v-else>保留最近 {{ LIVE_MAX }} 条<span v-if="search.trim()"> · 匹配 {{ liveMatches.length }} 条</span><span v-if="skipped" class="text-warning-accent"> · 日志过多，已跳过 {{ skipped }} 条</span></span>
      <span v-if="updatedAt" class="num">更新于 {{ updatedAt }}</span>
      <Button variant="ghost" size="sm" class="ml-auto min-h-11" :class="follow && 'bg-primary-soft text-primary-accent'" :aria-pressed="follow" @click="toggleFollow">
        <ArrowDownToLine class="size-3.5" />跟随日志
      </Button>
    </div>
    <p v-if="mode === 'file' && location" class="mb-3 break-all font-mono text-xs text-muted-foreground">{{ location }}</p>
    <p v-if="fileLevelHint" data-file-level-hint class="mb-3 text-xs text-muted-foreground">
      日志文件只记录 {{ configuredLevel }} 及以上（配置的 log-level）。要看 {{ level }} 日志，请切换到
      <button type="button" class="cursor-pointer text-primary-accent underline-offset-2 hover:underline" @click="changeMode('live')">实时</button>。
    </p>
    <p v-if="mode === 'live' && configuredLevel === 'silent'" class="mb-3 text-xs text-muted-foreground">
      配置的 log-level 是 silent，日志文件不记录内容；实时日志不受影响，默认按 info 显示。
    </p>
    <p v-if="error" role="alert" class="mb-3 break-words text-sm text-destructive-accent">
      {{ error }}<span v-if="updatedAt"> 当前保留上次读取的日志。</span>
    </p>

    <SectionCard v-if="mode === 'live' && liveUnsupported">
      <EmptyState :icon="FileText" title="需要升级 Agent" :description="liveUnsupported">
        <Button variant="outline" class="min-h-11" as-child><RouterLink to="/agents">前往升级 Agent</RouterLink></Button>
      </EmptyState>
    </SectionCard>
    <AgentLogLines v-else-if="mode === 'live'" ref="logPane" :items="liveMatches" :notice="liveNotice" height-class="h-[55dvh] min-h-60 max-h-[640px]" />
    <SectionCard v-else-if="legacy || unavailable || (sourcesReady && !sources.length)">
      <EmptyState :icon="FileText" :title="legacy ? '需要升级 Agent' : '暂无可读取的 Mihomo 日志'" :description="unavailableMessage">
        <Button v-if="legacy" variant="outline" class="min-h-11" as-child><RouterLink to="/agents">前往升级 Agent</RouterLink></Button>
        <Button v-else variant="outline" class="min-h-11" :disabled="loading" @click="refresh">重新读取来源</Button>
      </EmptyState>
    </SectionCard>
    <AgentLogLines v-else ref="logPane" :text="filteredText" :min-level="agentLevelOf(level)" :notice="notice"
      empty-text="没有该等级及以上的日志" height-class="h-[55dvh] min-h-60 max-h-[640px]" />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { ArrowDownToLine, FileText, Pause, Play, RefreshCw } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import Toolbar from '@/components/common/Toolbar.vue'
import Segmented from '@/components/common/Segmented.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import AgentLogLines from '@/components/agents/AgentLogLines.vue'
import { useTabActive } from '@/components/ui/tabs'
import { agentApi, mihomoPanelApi, type AgentLogSource } from '@/api'
import { panelError } from '@/types/mihomo'
import { cachedPanelData } from '@/lib/mihomoPanelCache'
import {
  agentLevelOf, isMihomoLogLevel, MIHOMO_LOG_LEVELS, mihomoLevelRank, mihomoLiveEntry,
  type AgentLogEntry, type MihomoLiveLog, type MihomoLogLevel
} from '@/lib/agentLogs'

type LogMode = 'file' | 'live'

const props = withDefaults(defineProps<{ agentId: string; active?: boolean; reloadToken: number }>(), { active: true })
// 所在标签是否为当前标签：由 Tabs 注入，切换标签不经过 props，隐藏的面板不会整组件重新渲染
const active = useTabActive(() => props.active)
const MAX_LINES = 500
const REFRESH_MS = 3000
// 实时日志：每秒取一次增量，浏览器只保留最近 1000 条
const LIVE_REFRESH_MS = 1000
const LIVE_MAX = 1000
const MODES: Array<{ value: LogMode; label: string }> = [{ value: 'file', label: '日志文件' }, { value: 'live', label: '实时' }]
const LIVE_ERRORS: Record<string, string> = {
  CORE_UNREACHABLE: '无法连接 Mihomo 控制接口，正在重试…',
  CORE_AUTH_FAILED: 'Mihomo 控制接口拒绝了 Agent 的 secret，请检查配置。'
}

const mode = ref<LogMode>('file')
const sources = ref<AgentLogSource[]>([])
const sourcesReady = ref(false)
const sourceId = ref('')
const legacy = ref(false)
const text = ref('')
const error = ref('')
const loading = ref(false)
const paused = ref(false)
const follow = ref(true)
const search = ref('')
const updatedAt = ref('')
const visible = ref(document.visibilityState !== 'hidden')
const logPane = ref<InstanceType<typeof AgentLogLines> | null>(null)
// 配置文件里的 log-level（读运行中的内核）；用户没手动选过时，等级默认跟随它
const configuredLevel = ref('')
const level = ref<MihomoLogLevel>('info')
const levelChosen = ref(false)
// 实时日志
const liveEntries = ref<AgentLogEntry[]>([])
const liveAfter = ref(0)
const liveStatus = ref('')
const liveCode = ref('')
const liveUnsupported = ref('')
const skipped = ref(0)

const currentSource = computed(() => sources.value.find(source => source.id === sourceId.value))
const unavailable = computed(() => currentSource.value?.kind === 'unavailable')
const location = computed(() => currentSource.value?.kind === 'journal'
  ? `journalctl -u ${currentSource.value.unit}` : currentSource.value?.path || '')
const unavailableMessage = computed(() => legacy.value
  ? '旧版 Agent 无法区分核心与 Agent 日志，请升级 Agent 后查看 Mihomo 日志。'
  : currentSource.value?.note || 'Agent 未找到 Mihomo 核心日志，请检查设备上的服务日志配置。')
const keyword = computed(() => search.value.trim().toLowerCase())
const matchingLines = computed(() =>
  text.value.split(/\r?\n/).filter(line => line.trim() && (!keyword.value || line.toLowerCase().includes(keyword.value))))
const filteredText = computed(() => keyword.value ? matchingLines.value.join('\n') : text.value)
const liveMatches = computed(() => keyword.value
  ? liveEntries.value.filter(entry => entry.raw.toLowerCase().includes(keyword.value)) : liveEntries.value)
// 选的等级比日志文件记录的更详细时，提示改看实时日志
const fileLevelHint = computed(() => mode.value === 'file' && isMihomoLogLevel(configuredLevel.value)
  && mihomoLevelRank(level.value) < mihomoLevelRank(configuredLevel.value))
const notice = computed(() => {
  if (text.value) return keyword.value && !matchingLines.value.length ? '没有匹配的日志' : ''
  if (loading.value) return '正在读取日志…'
  if (error.value) return '日志读取失败，请点击「刷新日志」重试。'
  return updatedAt.value ? 'Mihomo 日志暂无内容' : '等待读取日志'
})
const liveNotice = computed(() => {
  if (liveEntries.value.length) return keyword.value && !liveMatches.value.length ? '没有匹配的日志' : ''
  if (error.value) return '实时日志读取失败，请点击「刷新日志」重试。'
  if (liveStatus.value === 'streaming') return `正在接收 ${level.value} 及以上的日志，等待新日志…`
  if (liveStatus.value === 'error') return LIVE_ERRORS[liveCode.value] || '暂时无法读取 Mihomo 日志流。'
  return '正在连接 Mihomo 日志流…'
})
const refreshStatus = computed(() => {
  if (mode.value === 'live') {
    if (liveUnsupported.value) return '实时日志不可用'
    if (paused.value) return '已暂停刷新'
    if (!visible.value) return '页面隐藏，已暂停刷新'
    if (error.value) return '读取失败，稍后重试'
    if (liveStatus.value === 'error') return LIVE_ERRORS[liveCode.value] || '日志流中断，正在重试…'
    if (liveStatus.value === 'streaming') return '实时接收中，每秒更新'
    return '正在连接日志流…'
  }
  if (loading.value) return '正在读取日志…'
  if (legacy.value || unavailable.value || (sourcesReady.value && !sources.value.length)) return '日志不可用'
  if (paused.value) return '已暂停刷新'
  if (!visible.value) return '页面隐藏，已暂停刷新'
  if (error.value) return '读取失败，稍后重试'
  return '每 3 秒自动刷新'
})

let timer: ReturnType<typeof setTimeout> | undefined
let controller: AbortController | undefined
let epoch = 0
let disposed = false

function stop() {
  epoch++
  clearTimeout(timer)
  controller?.abort()
  controller = undefined
  loading.value = false
}

function canRead() {
  return !disposed && active.value && visible.value && !!props.agentId
}

function blocked() {
  return mode.value === 'live'
    ? !!liveUnsupported.value
    : legacy.value || unavailable.value || (sourcesReady.value && !sources.value.length)
}

function schedule() {
  clearTimeout(timer)
  if (canRead() && !paused.value && !blocked()) {
    timer = setTimeout(() => { void load() }, mode.value === 'live' ? LIVE_REFRESH_MS : REFRESH_MS)
  }
}

function resetLive() {
  liveEntries.value = []
  liveAfter.value = 0
  liveStatus.value = ''
  liveCode.value = ''
  skipped.value = 0
}

function applyConfiguredLevel(value: unknown) {
  configuredLevel.value = typeof value === 'string' ? value : ''
  if (levelChosen.value) return
  // silent 时日志文件为空，实时日志也收不到内容，按 info 显示
  level.value = isMihomoLogLevel(configuredLevel.value) ? configuredLevel.value : 'info'
}

async function readConfiguredLevel(id: string) {
  const cached = cachedPanelData<Record<string, unknown>>(id, 'configs')
  if (cached) applyConfiguredLevel(cached['log-level'])
  try {
    const { data } = await mihomoPanelApi.configs(id)
    if (id === props.agentId && !disposed && data?.success) applyConfiguredLevel(data.data?.['log-level'])
  } catch {
    // 读不到配置时保持默认 info，不影响查看日志
  }
}

async function loadFile(request: number, id: string, signal: AbortSignal) {
  if (!sourcesReady.value) {
    const { data } = await agentApi.getLogSources(id, { signal })
    if (request !== epoch) return
    if (!data.success) throw new Error(data.message || '读取日志来源失败。')
    legacy.value = Boolean(data.legacy)
    sources.value = (Array.isArray(data.sources) ? data.sources : []).filter((source: AgentLogSource) =>
      (source.id === 'core' || source.id?.startsWith('core-')) && ['file', 'journal', 'unavailable'].includes(source.kind))
    sourcesReady.value = true
    // The core's log is read from its first readable source; a second one (a
    // separate log file) holds the same lines, so there is nothing to choose.
    if (!sources.value.some(source => source.id === sourceId.value)) {
      sourceId.value = (sources.value.find(source => source.kind !== 'unavailable') || sources.value[0])?.id || ''
      text.value = ''
      updatedAt.value = ''
    }
  }
  if (legacy.value || !currentSource.value || unavailable.value) return
  const { data } = await agentApi.getLogs(id, MAX_LINES, { source: sourceId.value }, { signal })
  if (request !== epoch) return
  if (!data.success) throw new Error(data.message || '读取 Mihomo 日志失败。')
  text.value = [data.logs, data.error_logs].filter(value => typeof value === 'string' && value)
    .map(value => value.replace(/(?:\r?\n)+$/, '')).join('\n').split(/\r?\n/).slice(-MAX_LINES).join('\n')
  updatedAt.value = new Date().toLocaleTimeString()
}

async function loadLive(request: number, id: string, signal: AbortSignal) {
  try {
    const { data } = await mihomoPanelApi.logs(id, { level: level.value, after: liveAfter.value }, { signal })
    if (request !== epoch) return
    if (!data?.success) throw new Error(data?.message || '读取实时日志失败。')
    const reply = data.data || {}
    const logs: MihomoLiveLog[] = Array.isArray(reply.entries) ? reply.entries : []
    liveStatus.value = typeof reply.status === 'string' ? reply.status : ''
    liveCode.value = typeof reply.code === 'string' ? reply.code : ''
    if (typeof reply.next === 'number') liveAfter.value = reply.next
    if (typeof reply.skipped === 'number' && reply.skipped > 0) skipped.value += reply.skipped
    if (logs.length) liveEntries.value = [...liveEntries.value, ...logs.map(mihomoLiveEntry)].slice(-LIVE_MAX)
    updatedAt.value = new Date().toLocaleTimeString()
  } catch (failure: any) {
    if (request === epoch && failure?.response?.data?.code === 'UPGRADE_REQUIRED') {
      liveUnsupported.value = panelError(failure, '请升级 Agent 后使用实时日志。')
      return
    }
    throw failure
  }
}

async function load() {
  if (!canRead() || loading.value) return
  clearTimeout(timer)
  const request = ++epoch
  const id = props.agentId
  controller = new AbortController()
  const signal = controller.signal
  // 实时日志每秒一次，不显示加载动画，避免按钮闪烁
  loading.value = mode.value === 'file' || !liveStatus.value
  error.value = ''
  try {
    if (mode.value === 'live') await loadLive(request, id, signal)
    else await loadFile(request, id, signal)
    if (request !== epoch) return
    await nextTick()
    if (request === epoch && follow.value) logPane.value?.scrollToBottom()
  } catch (failure) {
    if (request === epoch) error.value = panelError(failure, '暂时无法读取 Mihomo 日志。')
  } finally {
    if (request === epoch) {
      loading.value = false
      controller = undefined
      schedule()
    }
  }
}

function refresh() {
  stop()
  if (mode.value === 'live') liveUnsupported.value = ''
  else sourcesReady.value = false
  void load()
}

function changeMode(value: LogMode) {
  if (value === mode.value) return
  stop()
  mode.value = value
  error.value = ''
  updatedAt.value = ''
  resetLive()
  if (!paused.value) void load()
}

function changeLevel(value: string) {
  if (!isMihomoLogLevel(value) || value === level.value) return
  level.value = value
  levelChosen.value = true
  if (mode.value !== 'live') return
  // 实时日志按新等级重新取，Agent 会改订阅更详细的等级
  stop()
  resetLive()
  error.value = ''
  if (!paused.value) void load()
}

async function toggleFollow() {
  follow.value = !follow.value
  if (follow.value) {
    await nextTick()
    logPane.value?.scrollToBottom()
  }
}

function visibilityChanged() { visible.value = document.visibilityState !== 'hidden' }
watch([() => props.agentId, active, visible, paused], ([id], previous) => {
  stop()
  if (!previous || id !== previous[0]) {
    sources.value = []
    sourcesReady.value = false
    sourceId.value = ''
    legacy.value = false
    text.value = ''
    error.value = ''
    updatedAt.value = ''
    configuredLevel.value = ''
    levelChosen.value = false
    level.value = 'info'
    liveUnsupported.value = ''
    resetLive()
    if (id) void readConfiguredLevel(id)
  }
  if (!paused.value) void load()
}, { immediate: true })
watch(() => props.reloadToken, refresh)
onMounted(() => document.addEventListener('visibilitychange', visibilityChanged))
onUnmounted(() => {
  disposed = true
  stop()
  document.removeEventListener('visibilitychange', visibilityChanged)
})
</script>
