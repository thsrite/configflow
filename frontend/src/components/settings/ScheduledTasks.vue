<template>
  <SectionCard
    :padded="false"
    title="定时任务"
    :icon="CalendarClock"
    description="订阅与规则集由服务端按间隔更新，设备拉取时使用已校验的缓存；更新失败会保留上一次有效缓存。其他任务按各自的方式触发，这里汇总最近一次结果。"
    data-scheduled-tasks
  >
    <template #actions>
      <span v-if="tasks.length" class="chip num" data-task-summary>已启用 {{ summary.enabled }} / {{ summary.total }}</span>
      <span v-if="summary.failed" class="chip chip-bad num" data-task-failed>{{ summary.failed }} 个失败</span>
      <span v-if="summary.running" class="chip chip-acc num">{{ summary.running }} 个运行中</span>
      <Button variant="outline" size="sm" class="max-md:min-h-11" :disabled="loading" aria-label="刷新任务列表" @click="load()">
        <Loader2 v-if="loading" class="size-3.5 animate-spin" />
        <RefreshCw v-else class="size-3.5" />
        <span class="max-sm:hidden">刷新</span>
      </Button>
    </template>

    <div v-if="categories.length > 1 || summary.failed" class="flex min-w-0 gap-2 overflow-x-auto px-5 pb-3 max-md:px-4 max-sm:px-3" role="group" aria-label="筛选任务">
      <button type="button" class="chip shrink-0" :aria-pressed="filter === 'all'" @click="filter = 'all'">全部 <span class="num">{{ tasks.length }}</span></button>
      <button v-if="summary.failed" type="button" class="chip shrink-0" :aria-pressed="filter === 'failed'" @click="filter = 'failed'">失败 <span class="num">{{ summary.failed }}</span></button>
      <button v-for="item in categories" :key="item.category" type="button" class="chip shrink-0" :aria-pressed="filter === item.category" @click="filter = item.category">
        {{ CATEGORY_LABELS[item.category] }} <span class="num">{{ item.count }}</span>
      </button>
    </div>

    <p v-if="error" role="alert" class="m-0 break-words px-5 pb-3 text-[12.5px] text-destructive-accent max-md:px-4 max-sm:px-3">
      {{ error }}<template v-if="tasks.length">，保留上次读取的结果。</template>
    </p>

    <LoadingRows v-if="loading && !loaded" :rows="4" />
    <EmptyState v-else-if="loaded && !tasks.length" :icon="CalendarClock" title="还没有定时任务" description="添加订阅或远端规则集后，会在这里按间隔自动更新。" />
    <div v-else-if="tasks.length" class="cf-table-wrap">
      <table class="cf-table cf-mobile-records scheduled-task-records min-w-[980px]">
        <thead>
          <tr>
            <th>任务</th>
            <th>运行方式</th>
            <th>下次运行</th>
            <th>上次运行</th>
            <th class="text-right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="task in visible" :key="task.key" :data-task="task.key">
            <td class="cf-record-wide min-w-[320px] max-w-[440px] whitespace-normal">
              <div class="flex min-w-0 items-baseline gap-2">
                <span class="min-w-0 truncate font-medium text-foreground" :title="task.name">{{ task.name }}</span>
                <span v-if="secondary(task)" class="min-w-0 shrink truncate text-[12px] text-muted-foreground" :title="secondary(task)">{{ secondary(task) }}</span>
              </div>
              <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
                <span class="chip">{{ CATEGORY_LABELS[task.category] }}</span>
                <span v-if="statusView(task)" :class="['chip', statusView(task)!.chip]" data-task-status>
                  <Loader2 v-if="task.running" class="size-3 animate-spin" aria-hidden="true" />{{ statusView(task)!.label }}
                </span>
                <span v-if="!task.enabled" class="chip">{{ task.interval_seconds === 0 ? '仅手动' : '未启用' }}</span>
                <span v-if="task.run_count" class="num text-[11.5px] text-muted-foreground">运行 {{ task.run_count }} 次<template v-if="task.failure_count"> · 失败 {{ task.failure_count }} 次</template></span>
              </div>
              <p v-if="problem(task)" :class="['mt-1.5 mb-0 line-clamp-3 whitespace-pre-wrap break-words text-[12px] leading-relaxed', problemTone(task)]" :title="problem(task)" data-task-error>{{ problem(task) }}</p>
              <p v-if="cacheNote(task)" :class="['mt-1 mb-0 text-[12px] leading-relaxed', task.cache_retained ? 'text-foreground' : 'text-destructive-accent']" data-task-cache>{{ cacheNote(task) }}</p>
              <p v-if="task.state_message" class="mt-1 mb-0 text-[12px] leading-relaxed text-muted-foreground">{{ task.state_message }}</p>
            </td>
            <td data-label="运行方式" class="whitespace-nowrap text-[13px]">{{ scheduleText(task) }}</td>
            <td data-label="下次运行" class="whitespace-nowrap text-[13px]">
              <span v-if="task.running" class="inline-flex items-center gap-1.5 text-primary-accent"><Loader2 class="size-3.5 animate-spin" aria-hidden="true" />运行中…</span>
              <span v-else-if="!task.enabled || !task.next_run_at" class="text-muted-foreground">{{ task.enabled && task.interval_seconds === null ? '按需' : '—' }}</span>
              <span v-else :class="['num', isOverdue(task, now) ? 'text-destructive-accent' : 'text-foreground']" :title="absoluteTime(task.next_run_at)">
                {{ relativeTo(task.next_run_at, now) }}
              </span>
            </td>
            <td data-label="上次运行" class="whitespace-nowrap text-[13px]">
              <template v-if="task.last_run_at">
                <span class="num" :title="absoluteTime(task.last_run_at)">{{ relativeTo(task.last_run_at, now) }}</span>
                <span v-if="durationText(task.last_duration_ms)" class="num text-muted-foreground"> · {{ durationText(task.last_duration_ms) }}</span>
                <span v-if="triggerText(task)" class="block text-[11.5px] text-muted-foreground">{{ triggerText(task) }}</span>
              </template>
              <span v-else class="text-muted-foreground">尚未运行</span>
            </td>
            <td class="cf-record-wide">
              <div class="flex items-center justify-end gap-1 max-md:justify-start">
                <Button
                  variant="ghost" size="icon" class="size-8 max-md:size-11"
                  :aria-label="`立即运行「${task.name}」`" :title="task.can_run ? '立即运行' : task.run_hint"
                  :disabled="!task.can_run || task.running || starting.has(task.key)"
                  @click="run(task)"
                >
                  <Loader2 v-if="starting.has(task.key)" class="size-4 animate-spin" />
                  <Play v-else class="size-4" />
                </Button>
                <Button
                  v-if="task.editable" variant="ghost" size="icon" class="size-8 max-md:size-11"
                  :aria-label="`编辑「${task.name}」的间隔`" title="编辑间隔" @click="openEditor(task)"
                >
                  <Pencil class="size-4" />
                </Button>
                <Button
                  v-if="task.link" as-child variant="ghost" size="icon" class="size-8 max-md:size-11"
                >
                  <RouterLink :to="task.link" :aria-label="`前往「${task.name}」所在页面`" title="前往所在页面"><ArrowUpRight class="size-4" /></RouterLink>
                </Button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="!visible.length" class="m-0 px-5 py-6 text-center text-[13px] text-muted-foreground">没有符合筛选的任务。</p>
    </div>

    <Dialog :open="Boolean(editing)" @update:open="value => { if (!value && !saving) editing = null }">
      <DialogContent v-if="editing" class="max-w-[440px] max-md:[&_[data-slot=button]]:min-h-11" :show-close-button="!saving">
        <DialogHeader>
          <DialogTitle class="break-words">编辑「{{ editing.name }}」</DialogTitle>
          <DialogDescription>{{ editorDescription }}</DialogDescription>
        </DialogHeader>
        <form @submit.prevent="saveEditor">
          <fieldset :disabled="saving" class="m-0 flex min-w-0 flex-col gap-4 border-0 p-0">
            <div v-if="editing.category === 'router_rule_sync'" class="flex min-h-11 items-center justify-between gap-3">
              <Label for="task-router-auto" class="flex min-h-11 flex-1 items-center">自动同步</Label>
              <Switch id="task-router-auto" :model-value="routerEnabled" @update:model-value="value => (routerEnabled = Boolean(value))" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="task-interval">{{ intervalLabel }}</Label>
              <Select v-model="intervalChoice">
                <SelectTrigger id="task-interval" class="w-full max-md:min-h-11"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="option in intervalOptions" :key="option.value" :value="option.value">{{ option.label }}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <p v-if="editorError" role="alert" class="m-0 break-words text-[12px] text-destructive-accent">{{ editorError }}</p>
          </fieldset>
          <DialogFooter class="mt-5">
            <Button type="button" variant="outline" :disabled="saving" @click="editing = null">取消</Button>
            <Button type="submit" :disabled="saving"><Loader2 v-if="saving" class="size-4 animate-spin" />保存</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  </SectionCard>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ArrowUpRight, CalendarClock, Loader2, Pencil, Play, RefreshCw } from '@lucide/vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { routerApi, tasksApi } from '@/api'
import { notify } from '@/lib/feedback'
import { isAxiosError } from 'axios'
import {
  CATEGORY_LABELS, absoluteTime, cacheNote, durationText, isOverdue,
  relativeTo, scheduleText, taskIntervalOptions, taskStatusView, taskSummary,
  type ScheduledTask, type TaskCategory
} from '@/lib/scheduledTasks'

const errorMessage = (failure: unknown, fallback: string) =>
  isAxiosError<{ message?: string }>(failure) ? failure.response?.data?.message || fallback : failure instanceof Error ? failure.message : fallback

const FAST_POLL = 3000
const SLOW_POLL = 60000

const tasks = ref<ScheduledTask[]>([])
const loading = ref(false)
const loaded = ref(false)
const error = ref('')
const filter = ref<'all' | 'failed' | TaskCategory>('all')
const starting = ref(new Set<string>())
const now = ref(Date.now())
let fastUntil = 0
let timer: number | undefined
let clock: number | undefined
let readToken = 0
let disposed = false

const summary = computed(() => taskSummary(tasks.value))
const categories = computed(() => {
  const counts = new Map<TaskCategory, number>()
  for (const task of tasks.value) counts.set(task.category, (counts.get(task.category) || 0) + 1)
  return [...counts].map(([category, count]) => ({ category, count }))
})
const visible = computed(() => tasks.value.filter(task =>
  filter.value === 'all' ? true
    : filter.value === 'failed' ? task.enabled && !task.running && task.last_status === 'failed'
      : task.category === filter.value))

const statusView = taskStatusView

function secondary(task: ScheduledTask) {
  return [task.profile_name, task.detail].filter(Boolean).join(' · ')
}

/** 失败与跳过的原因始终可见，不只放在悬停提示里。 */
function problem(task: ScheduledTask) {
  if (task.running || !task.last_error) return ''
  return task.last_status === 'failed' || task.last_status === 'skipped' ? task.last_error : ''
}

function problemTone(task: ScheduledTask) {
  if (task.last_status === 'skipped' || task.cache_retained) return 'text-warning-accent'
  return 'text-destructive-accent'
}

function triggerText(task: ScheduledTask) {
  return { manual: '手动运行', pull: '设备首次拉取', shutdown: '服务停止时', schedule: '', event: '', }[task.last_trigger || 'schedule'] || ''
}

function schedule() {
  window.clearTimeout(timer)
  if (disposed) return
  const fast = Date.now() < fastUntil || tasks.value.some(task => task.running)
  timer = window.setTimeout(() => load(true), fast ? FAST_POLL : SLOW_POLL)
}

async function load(quiet = false) {
  const token = ++readToken
  if (!quiet) loading.value = true
  try {
    const { data } = await tasksApi.list()
    if (token !== readToken) return
    tasks.value = Array.isArray(data.tasks) ? data.tasks : []
    error.value = ''
    loaded.value = true
  } catch (failure) {
    if (token !== readToken) return
    error.value = errorMessage(failure, '读取定时任务失败')
    loaded.value = true
  } finally {
    if (token === readToken) {
      loading.value = false
      now.value = Date.now()
      schedule()
    }
  }
}

async function run(task: ScheduledTask) {
  if (starting.value.has(task.key)) return
  starting.value = new Set(starting.value).add(task.key)
  try {
    const { data } = await tasksApi.run(task.key)
    notify.success(`「${task.name}」${data.message ? '：' + data.message : '已开始运行'}`)
    fastUntil = Date.now() + 30000
    await load(true)
  } catch (failure) {
    notify.error(errorMessage(failure, `「${task.name}」无法运行`))
  } finally {
    const next = new Set(starting.value)
    next.delete(task.key)
    starting.value = next
  }
}

const editing = ref<ScheduledTask | null>(null)
const intervalChoice = ref('')
const routerEnabled = ref(false)
const saving = ref(false)
const editorError = ref('')

const intervalOptions = computed(() => editing.value ? taskIntervalOptions(editing.value) : [])

const intervalLabel = computed(() => {
  switch (editing.value?.category) {
    case 'router_rule_sync':
      return '检查间隔'
    case 'metrics_history':
      return '保存间隔'
    default:
      return '更新间隔'
  }
})

const editorDescription = computed(() => {
  switch (editing.value?.category) {
    case 'metrics_history':
      return 'Agent 监控数据先保存在内存中，按此间隔把新增的数据追加到磁盘；服务正常停止时也会保存一次。间隔越短，意外断电时丢失的数据越少。'
    case 'subscription':
      return '服务端按此间隔下载订阅并更新缓存；关闭后只在订阅页手动更新。与订阅编辑中的「订阅源更新间隔」是同一设置。'
    case 'rule_source':
      return '服务端按此间隔下载并校验规则集；设备、OxiDNS 和发布都使用已校验的缓存。关闭后只在规则库或这里手动更新。'
    default:
      return '与上游路由中这台路由器的「远程规则自动同步」是同一设置。'
  }
})

function openEditor(task: ScheduledTask) {
  editing.value = task
  editorError.value = ''
  intervalChoice.value = String(task.interval_seconds ?? 86400)
  routerEnabled.value = task.enabled
}

async function saveEditor() {
  const task = editing.value
  if (!task || saving.value) return
  saving.value = true
  editorError.value = ''
  try {
    const seconds = Number(intervalChoice.value)
    if (task.category === 'router_rule_sync') {
      await routerApi.saveRuleSync(task.resource_id, { auto_rule_sync: routerEnabled.value, rule_sync_interval: seconds / 60 }, task.profile_id || '')
    } else {
      await tasksApi.setInterval(task.key, seconds)
    }
    notify.success(`已更新「${task.name}」`)
    editing.value = null
    await load(true)
  } catch (failure) {
    editorError.value = errorMessage(failure, '保存失败')
  } finally {
    saving.value = false
  }
}

function onVisibility() {
  if (document.visibilityState === 'visible') load(true)
}

onMounted(() => {
  load()
  clock = window.setInterval(() => { now.value = Date.now() }, 30000)
  document.addEventListener('visibilitychange', onVisibility)
})

onBeforeUnmount(() => {
  disposed = true
  window.clearTimeout(timer)
  window.clearInterval(clock)
  document.removeEventListener('visibilitychange', onVisibility)
})
</script>
