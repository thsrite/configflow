<template>
  <div>
    <PageHeader
      eyebrow="System"
      title="日志"
      description="运行记录与错误排查，支持关键词搜索、级别过滤与自动刷新。"
    >
      <template #actions>
        <Button variant="outline" class="border-border/60 bg-background/40" :disabled="loading" @click="loadLogs()">
          <RefreshCw class="size-4" :class="loading && 'animate-spin'" />
          刷新
        </Button>
        <Button
          variant="outline"
          class="border-destructive-accent/30 bg-destructive-soft/40 text-destructive-accent"
          @click="clearLogs"
        >
          <Trash2 class="size-4" />
          清空
        </Button>
      </template>
    </PageHeader>

    <Toolbar v-model:search="searchKeyword" placeholder="搜索关键词…">
      <template #filters>
        <Select v-model="logLevel" @update:model-value="loadLogs()">
          <SelectTrigger class="h-9 w-[132px] border-transparent bg-background/50 text-[13px]">
            <SelectValue placeholder="日志级别" />
          </SelectTrigger>
          <SelectContent class="glass-strong">
            <SelectItem v-for="level in LEVELS" :key="level.value" :value="level.value">
              {{ level.label }}
            </SelectItem>
          </SelectContent>
        </Select>

        <Select v-model="logLines" @update:model-value="loadLogs()">
          <SelectTrigger class="h-9 w-[120px] border-transparent bg-background/50 text-[13px]">
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
        <label
          class="flex h-9 cursor-pointer items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-3 text-[12.5px] text-muted-foreground"
        >
          <Switch :model-value="autoRefresh" @update:model-value="toggleAutoRefresh" />
          <span>自动刷新</span>
          <StatusDot v-if="autoRefresh" tone="success" pulse label="5s" />
        </label>
        <Button variant="ghost" size="sm" title="滚动到底部" @click="scrollToBottom">
          <ArrowDownToLine class="size-4" />
          到底部
        </Button>
      </template>
    </Toolbar>

    <div v-if="logInfo || totalLines" class="mb-3 flex flex-wrap items-center gap-2">
      <Badge v-if="logInfo" variant="outline" class="gap-1.5 font-mono text-[11px]">
        <FileText class="size-3" aria-hidden="true" />
        {{ logInfo.path }}
      </Badge>
      <Badge v-if="logInfo" variant="outline" class="num font-mono text-[11px]">
        {{ logInfo.size_mb }} MB
      </Badge>
      <Badge variant="outline" class="num font-mono text-[11px]">总行数 {{ totalLines }}</Badge>
      <Badge v-if="filteredLines !== totalLines" variant="brand" class="num font-mono text-[11px]">
        过滤后 {{ filteredLines }}
      </Badge>
    </div>

    <SectionCard :padded="false" :class="loading && 'scanline'">
      <!-- 终端风格：等宽、行号列固定、内容区自身滚动 -->
      <div
        ref="logContainer"
        class="max-h-[calc(100dvh-320px)] min-h-[320px] overflow-auto rounded-xl bg-background/45 font-mono text-[12px] leading-[1.7]"
      >
        <EmptyState
          v-if="!logs.length"
          :icon="ScrollText"
          title="暂无日志"
          description="调整关键词或级别筛选，或等待系统产生新的运行记录。"
        />

        <div v-else class="min-w-max py-2">
          <div
            v-for="(line, index) in parsedLogs"
            :key="index"
            class="group grid grid-cols-[52px_minmax(0,1fr)] gap-3 px-3 transition-colors hover:bg-accent/40"
          >
            <span class="num shrink-0 py-0.5 text-right text-muted-foreground/50 select-none">
              {{ index + 1 }}
            </span>
            <!-- Vue 模板会压缩标签间的空白，各段之间用 gap 而不是空格分隔 -->
            <span
              v-if="line.level"
              class="flex flex-wrap items-baseline gap-x-2 py-0.5"
            >
              <span class="text-muted-foreground/80">{{ line.time }}</span>
              <span class="text-info-accent">{{ line.logger }}</span>
              <span :class="levelClass(line.level)">{{ line.level }}</span>
              <span class="min-w-0 break-words whitespace-pre-wrap text-foreground/90">
                {{ line.message }}
              </span>
            </span>
            <span v-else class="py-0.5 break-words whitespace-pre-wrap text-foreground/75">
              {{ line.raw }}
            </span>
          </div>
        </div>
      </div>
    </SectionCard>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { ArrowDownToLine, FileText, RefreshCw, ScrollText, Trash2 } from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import EmptyState from '@/components/common/EmptyState.vue'
import PageHeader from '@/components/common/PageHeader.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import StatusDot from '@/components/common/StatusDot.vue'
import Toolbar from '@/components/common/Toolbar.vue'
import api from '@/api'
import { confirmDanger, notify } from '@/lib/feedback'

const LEVELS = [
  { label: '全部级别', value: 'all' },
  { label: 'DEBUG', value: 'DEBUG' },
  { label: 'INFO', value: 'INFO' },
  { label: 'WARNING', value: 'WARNING' },
  { label: 'ERROR', value: 'ERROR' },
  { label: 'CRITICAL', value: 'CRITICAL' }
]

const LINE_OPTIONS = [
  { label: '100 行', value: '100' },
  { label: '200 行', value: '200' },
  { label: '500 行', value: '500' },
  { label: '1000 行', value: '1000' },
  { label: '全部', value: '10000' }
]

const logs = ref<string[]>([])
const logInfo = ref<any>(null)
const totalLines = ref(0)
const filteredLines = ref(0)

const searchKeyword = ref('')
/* Select 的值必须是非空字符串，用 'all' 表示不过滤，请求时再转空 */
const logLevel = ref('all')
const logLines = ref('100')

const autoRefresh = ref(false)
const refreshTimer = ref<number | null>(null)
const REFRESH_INTERVAL = 5000

const loading = ref(false)
const logContainer = ref<HTMLElement>()

/* 日志行解析：拆成时间 / 来源 / 级别 / 正文四段分别着色，
 * 取代原先的 v-html 拼接（scoped 样式对 v-html 内容不生效，颜色实际从未出现）。 */
const LOG_LINE = /^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}[,.]\d+)\s+-\s+([\w.]+)\s+-\s+(\w+)\s+-\s+([\s\S]*)$/

const parsedLogs = computed(() =>
  logs.value.map(raw => {
    const m = LOG_LINE.exec(raw)
    if (!m) return { raw, time: '', logger: '', level: '', message: '' }
    return { raw, time: m[1], logger: m[2], level: m[3], message: m[4] }
  })
)

const levelClass = (level: string): string =>
  ({
    ERROR: 'text-destructive-accent font-semibold',
    CRITICAL: 'text-destructive-accent font-semibold',
    WARNING: 'text-warning-accent font-semibold',
    INFO: 'text-success-accent',
    DEBUG: 'text-muted-foreground'
  })[level] ?? 'text-muted-foreground'

const loadLogs = async (scrollToEnd = false) => {
  loading.value = true
  try {
    const params: Record<string, string | number> = { lines: Number(logLines.value) }
    if (searchKeyword.value) params.search = searchKeyword.value
    if (logLevel.value !== 'all') params.level = logLevel.value

    const response = await api.get('/logs/tail', { params })

    if (response.data.success) {
      logs.value = response.data.logs
      totalLines.value = response.data.total_lines
      filteredLines.value = response.data.filtered_lines

      if (scrollToEnd) {
        await nextTick()
        scrollToBottom()
      }
    } else {
      notify.error(response.data.message || '加载日志失败')
    }
  } catch (error: any) {
    console.error('Failed to load logs:', error)
    notify.error(error.response?.data?.error || '加载日志失败')
  } finally {
    loading.value = false
  }
}

const loadLogInfo = async () => {
  try {
    const response = await api.get('/logs/info')
    if (response.data.success && response.data.exists) {
      logInfo.value = response.data
    }
  } catch (error) {
    console.error('Failed to load log info:', error)
  }
}

const scrollToBottom = () => {
  if (logContainer.value) {
    logContainer.value.scrollTop = logContainer.value.scrollHeight
  }
}

const toggleAutoRefresh = (enabled: boolean) => {
  autoRefresh.value = enabled
  if (enabled) {
    refreshTimer.value = window.setInterval(() => loadLogs(true), REFRESH_INTERVAL)
    notify.success('已启用自动刷新', '每 5 秒拉取一次最新日志')
  } else {
    if (refreshTimer.value) {
      clearInterval(refreshTimer.value)
      refreshTimer.value = null
    }
    notify.info('已停止自动刷新')
  }
}

const clearLogs = async () => {
  const ok = await confirmDanger('确定要清空日志文件吗？此操作不可恢复。', {
    title: '清空日志',
    confirmText: '清空'
  })
  if (!ok) return

  try {
    const response = await api.post('/logs/clear')
    if (response.data.success) {
      notify.success('日志已清空')
      logs.value = []
      totalLines.value = 0
      filteredLines.value = 0
      loadLogInfo()
    }
  } catch (error: any) {
    console.error('Failed to clear logs:', error)
    notify.error('清空日志失败')
  }
}

/* 关键词改动做防抖，避免每敲一个字符就打一次接口 */
let searchTimer: number
watch(searchKeyword, () => {
  clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => loadLogs(), 300)
})

onMounted(async () => {
  await loadLogs(true)
  await loadLogInfo()
})

onUnmounted(() => {
  clearTimeout(searchTimer)
  if (refreshTimer.value) clearInterval(refreshTimer.value)
})
</script>
