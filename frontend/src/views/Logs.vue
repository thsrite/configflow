<template>
  <div>
    <PageHeader title="日志">
      <template #actions>
        <div class="flex flex-wrap gap-1.5" role="group" aria-label="按级别筛选">
          <button
            v-for="level in LEVELS"
            :key="level"
            type="button"
            class="chip font-mono"
            :aria-pressed="levelsOn.has(level)"
            @click="toggleLevel(level)"
          >
            {{ level }}
          </button>
        </div>
        <Button variant="outline" class="border-border/60 bg-background/40" @click="togglePause">
          <component :is="paused ? Play : Pause" class="size-4" />
          {{ paused ? '继续' : '暂停' }}
        </Button>
      </template>
    </PageHeader>

    <Toolbar v-model:search="searchKeyword" placeholder="搜索关键词…">
      <template #filters>
        <Select v-model="logLines" @update:model-value="reload()">
          <SelectTrigger class="h-9 w-[120px] border-input bg-card text-[13px] dark:border-transparent" aria-label="显示行数">
            <SelectValue placeholder="显示行数" />
          </SelectTrigger>
          <SelectContent class="glass-strong">
            <SelectItem v-for="option in LINE_OPTIONS" :key="option.value" :value="option.value">
              {{ option.label }}
            </SelectItem>
          </SelectContent>
        </Select>
      </template>

      <template #actions>
        <Button variant="ghost" size="sm" :disabled="loading" @click="reload()">
          <RefreshCw class="size-3.5" :class="loading && 'animate-spin'" />
          刷新
        </Button>
        <Button variant="ghost" size="sm" title="滚动到底部" @click="scrollToBottom">
          <ArrowDownToLine class="size-3.5" />
          到底部
        </Button>
        <Button variant="ghost" size="sm" class="text-destructive-accent hover:bg-destructive-soft" @click="clearLogs">
          <Trash2 class="size-3.5" />
          清空
        </Button>
      </template>
    </Toolbar>

    <div v-if="logInfo || totalLines" class="mb-3 flex flex-wrap items-center gap-1.5">
      <span v-if="logInfo" class="chip font-mono"><FileText class="size-3" aria-hidden="true" />{{ logInfo.path }}</span>
      <span v-if="logInfo" class="chip font-mono">{{ logInfo.size_mb }} MB</span>
      <span class="chip font-mono">总行数 {{ totalLines }}</span>
      <span v-if="visibleLines.length !== lines.length" class="chip chip-acc font-mono">显示 {{ visibleLines.length }}</span>
      <span class="ml-auto flex items-center gap-2 font-mono text-[11px] text-muted-foreground">
        <span :class="cn('size-1.5 rounded-full', paused ? 'bg-muted-foreground/60' : 'live-dot')" aria-hidden="true" />
        {{ paused ? '已暂停' : `每 ${TAIL_MS / 1000}s 尾随` }}
      </span>
    </div>

    <!-- 终端：常暗，与主题无关。桌面独立滚动；窄屏随页面滚动、完整换行，不嵌套滚动区 -->
    <div class="terminal overflow-hidden rounded-[18px] border shadow-surface">
      <div
        ref="logContainer"
        class="logs-container py-3.5 font-mono text-[12.5px] leading-[1.75]"
      >
        <p v-if="!visibleLines.length" class="px-[18px] py-10 text-center text-[13px] opacity-60">
          {{ lines.length ? '当前级别筛选下没有日志' : loading ? '正在读取日志…' : '暂无日志' }}
        </p>
        <div v-else class="logs-list">
          <div
            v-for="line in visibleLines"
            :key="line.id"
            :class="cn('log-row px-[18px]', line.fresh && 'is-fresh')"
          >
            <template v-if="line.level">
              <span class="log-time ts" :title="line.stamp">{{ line.time }}</span>
              <span :class="cn('log-level font-semibold', `lv-${line.level}`)">{{ line.levelLabel }}</span>
              <span class="log-logger md" :title="line.logger">[{{ line.logger }}]</span>
              <span class="log-message">{{ line.message }}</span>
            </template>
            <span v-else class="log-message log-raw opacity-75">{{ line.raw }}</span>
          </div>
        </div>
        <div ref="logEnd" data-testid="logs-end" class="logs-end" aria-hidden="true" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { ArrowDownToLine, FileText, Pause, Play, RefreshCw, Trash2 } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import PageHeader from '@/components/common/PageHeader.vue'
import Toolbar from '@/components/common/Toolbar.vue'
import api from '@/api'
import { confirmDanger, notify } from '@/lib/feedback'
import { cn } from '@/lib/utils'

/* WARNING / CRITICAL 在芯片上缩写，筛选时与原级别对应 */
const LEVELS = ['DEBUG', 'INFO', 'WARN', 'ERROR'] as const
type LevelChip = (typeof LEVELS)[number]
const CHIP_OF: Record<string, LevelChip> = {
  DEBUG: 'DEBUG',
  INFO: 'INFO',
  WARNING: 'WARN',
  ERROR: 'ERROR',
  CRITICAL: 'ERROR'
}

const LINE_OPTIONS = [
  { label: '100 行', value: '100' },
  { label: '200 行', value: '200' },
  { label: '500 行', value: '500' },
  { label: '1000 行', value: '1000' },
  { label: '全部', value: '10000' }
]

const TAIL_MS = 3000

interface LogLine {
  id: number
  raw: string
  /** 完整时间戳，悬停时显示 */
  stamp: string
  time: string
  logger: string
  level: string
  levelLabel: string
  message: string
  fresh: boolean
}

const lines = ref<LogLine[]>([])
const logInfo = ref<any>(null)
const totalLines = ref(0)
const searchKeyword = ref('')
const logLines = ref('200')
const levelsOn = ref<Set<LevelChip>>(new Set(LEVELS))
const paused = ref(false)
const loading = ref(false)
const logContainer = ref<HTMLElement>()
const logEnd = ref<HTMLElement>()

const LOG_LINE = /^(\d{4}-\d{2}-\d{2})\s+(\d{2}:\d{2}:\d{2})[,.](\d+)\s+-\s+([\w.]+)\s+-\s+(\w+)\s+-\s+([\s\S]*)$/
let nextId = 1

const parse = (raw: string, fresh: boolean): LogLine => {
  const m = LOG_LINE.exec(raw)
  if (!m) return { id: nextId++, raw, stamp: '', time: '', logger: '', level: '', levelLabel: '', message: '', fresh }
  const level = m[5]
  return {
    id: nextId++,
    raw,
    stamp: `${m[1]} ${m[2]},${m[3]}`,
    time: `${m[2]}.${m[3].padEnd(3, '0').slice(0, 3)}`,
    logger: m[4].replace(/^backend\./, ''),
    level,
    levelLabel: CHIP_OF[level] || level,
    message: m[6],
    fresh
  }
}

/** 续行（如堆栈）没有级别，跟随上一条有级别的行显示 */
const visibleLines = computed(() => {
  let lastShown = true
  return lines.value.filter(line => {
    if (!line.level) return lastShown
    lastShown = levelsOn.value.has(CHIP_OF[line.level] || 'INFO')
    return lastShown
  })
})

const toggleLevel = (level: LevelChip) => {
  const next = new Set(levelsOn.value)
  if (next.has(level)) next.delete(level)
  else next.add(level)
  levelsOn.value = next
}

/** 窄屏下日志区不单独滚动（overflow: visible），跟随与跳底都作用在页面上 */
const pageScrolls = () => {
  const el = logContainer.value
  return !el || window.getComputedStyle(el).overflowY === 'visible'
}

/** 读者停在底部附近时新日志自动跟随，往上翻看时不打扰 */
const atBottom = (): boolean => {
  if (pageScrolls()) {
    const doc = document.documentElement
    return window.innerHeight + window.scrollY >= doc.scrollHeight - 80
  }
  const el = logContainer.value!
  return el.scrollHeight - el.scrollTop - el.clientHeight < 40
}

const scrollToBottom = () => {
  if (pageScrolls()) logEnd.value?.scrollIntoView({ block: 'end' })
  else if (logContainer.value) logContainer.value.scrollTop = logContainer.value.scrollHeight
}

/**
 * 增量合并：服务端返回的是末尾 N 行，找到上次最后一行在新结果里的位置，
 * 之后的才是新行；找不到（被轮转或筛选条件变了）就整体替换。
 */
const merge = (incoming: string[], reset: boolean) => {
  const limit = Number(logLines.value)
  if (reset || !lines.value.length) {
    lines.value = incoming.map(raw => parse(raw, false))
    return
  }
  const lastRaw = lines.value[lines.value.length - 1].raw
  const at = incoming.lastIndexOf(lastRaw)
  if (at < 0) {
    lines.value = incoming.map(raw => parse(raw, false))
    return
  }
  const added = incoming.slice(at + 1).map(raw => parse(raw, true))
  if (!added.length) return
  const kept = lines.value.map(line => (line.fresh ? { ...line, fresh: false } : line))
  lines.value = [...kept, ...added].slice(-limit)
}

const loadLogs = async (reset = false, followTail = true) => {
  loading.value = true
  // 只有自动尾随会跟到最新一行，且读者本来就停在底部；手动刷新与筛选保持当前位置
  const follow = followTail && lines.value.length > 0 && atBottom()
  try {
    const params: Record<string, string | number> = { lines: Number(logLines.value) }
    if (searchKeyword.value) params.search = searchKeyword.value
    const { data } = await api.get('/logs/tail', { params })
    if (data.success) {
      merge(data.logs || [], reset)
      totalLines.value = data.total_lines || 0
      if (follow) {
        await nextTick()
        scrollToBottom()
      }
    } else {
      notify.error(data.message || '加载日志失败')
    }
  } catch (error: any) {
    notify.error(error.response?.data?.error || '加载日志失败')
  } finally {
    loading.value = false
  }
}

const reload = () => loadLogs(true, false)

const loadLogInfo = async () => {
  try {
    const { data } = await api.get('/logs/info')
    if (data.success && data.exists) logInfo.value = data
  } catch {
    // 文件信息只是辅助展示
  }
}

let tailTimer: number | undefined
const startTail = () => {
  clearInterval(tailTimer)
  tailTimer = window.setInterval(() => !document.hidden && loadLogs(), TAIL_MS)
}

const togglePause = () => {
  paused.value = !paused.value
  if (paused.value) clearInterval(tailTimer)
  else {
    loadLogs()
    startTail()
  }
}

const clearLogs = async () => {
  const ok = await confirmDanger('确定要清空日志文件吗？此操作不可恢复。', {
    title: '清空日志',
    confirmText: '清空'
  })
  if (!ok) return
  try {
    const { data } = await api.post('/logs/clear')
    if (data.success) {
      notify.success('日志已清空')
      lines.value = []
      totalLines.value = 0
      loadLogInfo()
    }
  } catch {
    notify.error('清空日志失败')
  }
}

/* 关键词改动做防抖，避免每敲一个字符就打一次接口 */
let searchTimer: number | undefined
watch(searchKeyword, () => {
  clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reload, 300)
})

onMounted(async () => {
  await loadLogs(true)
  // 手机首次进入保留筛选区可见；桌面在独立日志窗口中直接显示最新记录
  await nextTick()
  if (!pageScrolls()) scrollToBottom()
  loadLogInfo()
  startTail()
})

onUnmounted(() => {
  clearTimeout(searchTimer)
  clearInterval(tailTimer)
})
</script>

<style scoped>
/* 终端常暗：两套主题下都用同一组暖炭色 */
.terminal {
  background: #141312;
  color: #d9d5ca;
  border-color: rgb(255 255 255 / 8%);
}

:root[data-theme='light'] .terminal {
  background: #1c1b19;
}

/* 窄屏：时间 + 级别一行，来源与正文各占一行，长 URL 与堆栈完整换行 */
.logs-container {
  min-width: 0;
  overflow: visible;
}

.log-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  grid-template-areas: 'time level' 'logger logger' 'message message';
  gap: 0 0.625rem;
  padding-block: 0.375rem;
}

.log-time { grid-area: time; }
.log-level { grid-area: level; }
.log-logger { grid-area: logger; overflow-wrap: anywhere; }
.log-row > .log-message { grid-area: message; }

.log-message {
  min-width: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.log-raw {
  grid-column: 1 / -1;
}

/* 跳到最后一条时，让正文停在移动导航栏及安全区上方 */
.logs-end {
  scroll-margin-bottom: calc(var(--cf-tabbar-h) + env(safe-area-inset-bottom) + var(--cf-sp-5));
}

/* 桌面：终端列布局、独立滚动，来源过长时截断（悬停看全称），正文仍完整换行 */
@media (min-width: 901px) {
  .logs-container {
    height: 560px;
    overflow: auto;
  }

  .log-row {
    grid-template-columns: 96px 64px 150px minmax(0, 1fr);
    grid-template-areas: 'time level logger message';
    gap: 0.625rem;
    padding-block: 0;
  }

  .log-logger {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .logs-end {
    scroll-margin-bottom: 0;
  }
}

.log-row:hover {
  background: rgb(255 255 255 / 3%);
}

.log-row.is-fresh {
  animation: log-in 0.4s ease-out;
}

.ts {
  color: #7c786e;
}

.md {
  color: #b6b2a6;
}

.lv-INFO {
  color: #97ad73;
}

.lv-WARNING {
  color: #d9a984;
}

.lv-ERROR,
.lv-CRITICAL {
  color: #e0705e;
}

.lv-DEBUG {
  color: #7aa7d4;
}

@keyframes log-in {
  from {
    opacity: 0;
  }
}
</style>
