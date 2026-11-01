/**
 * 系统设置「定时任务」：每个后台任务一行（/api/tasks）。
 * 订阅、规则集与监控历史保存由服务端按间隔运行；其余任务沿用各自已有的触发方式，这里只展示结果。
 */

export type TaskCategory =
  | 'subscription' | 'rule_source' | 'router_rule_sync' | 'agent_rule_sync'
  | 'dns_follow' | 'domain_discovery' | 'webdav_backup' | 'metrics_history'

export type TaskRunStatus = 'success' | 'failed' | 'skipped'

/** 与后端 routes/tasks.py 的行结构一一对应。 */
export interface ScheduledTask {
  key: string
  category: TaskCategory
  name: string
  /** 规则集的来源主机名；订阅地址含凭据，不返回 */
  detail: string
  resource_id: string
  profile_id: string | null
  profile_name: string | null
  enabled: boolean
  /** 按间隔运行的任务；事件触发的任务为 null，见 schedule_text */
  interval_seconds: number | null
  schedule_text: string
  editable: boolean
  can_run: boolean
  run_hint: string
  running: boolean
  next_run_at: string | null
  /** 当前状态的补充说明（等待首次发布、已暂停等），与上次运行结果分开 */
  state_message: string
  link: string | null
  last_run_at: string | null
  last_status: TaskRunStatus | null
  last_error: string | null
  last_duration_ms: number | null
  last_trigger: 'schedule' | 'manual' | 'pull' | 'event' | 'shutdown' | null
  last_success_at: string | null
  run_count: number
  failure_count: number
  /** 失败时是否仍在使用上一次有效缓存（订阅、规则集） */
  cache_retained: boolean | null
  cache_updated_at: string | null
}

export const CATEGORY_LABELS: Record<TaskCategory, string> = {
  subscription: '订阅更新',
  rule_source: '规则集更新',
  router_rule_sync: 'ROS 规则同步',
  agent_rule_sync: 'Agent 规则同步',
  dns_follow: 'DNS 联动',
  domain_discovery: '域名发现',
  webdav_backup: '配置备份',
  metrics_history: '监控历史',
}

/** 「每 24 小时」「每 30 分钟」；0 表示关闭自动更新。 */
export function intervalText(seconds: number): string {
  if (!seconds) return '仅手动'
  if (seconds % 86400 === 0) return `每 ${seconds / 86400} 天`
  if (seconds % 3600 === 0) return `每 ${seconds / 3600} 小时`
  if (seconds % 60 === 0) return `每 ${seconds / 60} 分钟`
  return `每 ${seconds} 秒`
}

export function scheduleText(task: ScheduledTask): string {
  if (!task.enabled) return '已关闭'
  return task.interval_seconds === null ? task.schedule_text : intervalText(task.interval_seconds)
}

/** 「刚刚 / 5 分钟前 / 3 小时后 / 2 天后」；超过 30 天显示日期。 */
export function relativeTo(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return '—'
  const time = new Date(iso).getTime()
  if (Number.isNaN(time)) return '—'
  const diff = (time - now) / 1000
  const span = Math.abs(diff)
  if (span < 45) return diff > 0 ? '即将运行' : '刚刚'
  const suffix = diff > 0 ? '后' : '前'
  if (span < 3600) return `${Math.round(span / 60)} 分钟${suffix}`
  if (span < 86400) return `${Math.round(span / 3600)} 小时${suffix}`
  if (span < 86400 * 30) return `${Math.round(span / 86400)} 天${suffix}`
  return new Date(time).toLocaleDateString('zh-CN')
}

export function absoluteTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const time = new Date(iso)
  return Number.isNaN(time.getTime()) ? '' : time.toLocaleString('zh-CN', { hour12: false })
}

export function durationText(ms: number | null | undefined): string {
  if (!ms || ms <= 0) return ''
  if (ms < 1000) return `${Math.round(ms)} ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(ms < 10000 ? 1 : 0)} 秒`
  return `${Math.round(ms / 60000)} 分钟`
}

export interface TaskStatusView {
  label: string
  /** 对应 .chip 的配色 */
  chip: 'chip-ok' | 'chip-warn' | 'chip-bad' | 'chip-acc' | ''
}

/** 上次运行结果：失败但仍在使用旧缓存为警告色，没有可用内容为危险色。 */
export function taskStatusView(task: ScheduledTask): TaskStatusView | null {
  if (task.running) return { label: '运行中', chip: 'chip-acc' }
  switch (task.last_status) {
    case 'success':
      return { label: '成功', chip: 'chip-ok' }
    case 'skipped':
      return { label: '已跳过', chip: 'chip-warn' }
    case 'failed':
      if (task.cache_retained) return { label: '失败 · 使用旧缓存', chip: 'chip-warn' }
      return { label: '失败', chip: 'chip-bad' }
    default:
      return null
  }
}

/** 失败时说明正在使用哪个时间的缓存，或已没有可用内容。 */
export function cacheNote(task: ScheduledTask): string {
  if (task.last_status !== 'failed' || task.running || task.cache_retained === null) return ''
  if (!task.cache_retained) return '没有可用的有效缓存，请修正来源后重新运行。'
  if (!task.cache_updated_at) return '继续使用上一次有效缓存。'
  return `继续使用 ${absoluteTime(task.cache_updated_at)}（${relativeTo(task.cache_updated_at)}）更新的缓存。`
}

/** 下次运行已过期（调度可能停止）时标红；正在运行或关闭的不算。 */
export function isOverdue(task: ScheduledTask, now = Date.now()): boolean {
  if (!task.next_run_at || task.running || !task.enabled) return false
  return new Date(task.next_run_at).getTime() < now - 5 * 60 * 1000
}

export const SOURCE_INTERVAL_OPTIONS = [0, 3600, 6 * 3600, 12 * 3600, 86400, 3 * 86400, 7 * 86400]
export const ROUTER_INTERVAL_MINUTES = [15, 30, 60, 180, 360, 720, 1440]
/** 监控历史保存只提供这几档，与后端 metrics_flush.INTERVAL_CHOICES 一致 */
export const METRICS_FLUSH_MINUTES = [5, 10, 30, 60]

export function sourceIntervalOptions(current: number): number[] {
  return [...new Set([...SOURCE_INTERVAL_OPTIONS, current])].sort((a, b) => a - b)
}

/** 编辑弹窗里的间隔选项（值为秒数字符串）。路由器与监控历史只有固定几档。 */
export function taskIntervalOptions(task: ScheduledTask): { value: string; label: string }[] {
  if (task.category === 'router_rule_sync' || task.category === 'metrics_history') {
    const minutes = task.category === 'router_rule_sync' ? ROUTER_INTERVAL_MINUTES : METRICS_FLUSH_MINUTES
    return minutes.map(value => ({ value: String(value * 60), label: intervalText(value * 60) }))
  }
  return sourceIntervalOptions(task.interval_seconds ?? 86400).map(seconds => ({
    value: String(seconds), label: seconds ? intervalText(seconds) : '关闭自动更新（仅手动）'
  }))
}

export function taskSummary(tasks: ScheduledTask[]) {
  return {
    total: tasks.length,
    enabled: tasks.filter(task => task.enabled).length,
    failed: tasks.filter(task => task.enabled && !task.running && task.last_status === 'failed').length,
    running: tasks.filter(task => task.running).length,
  }
}
