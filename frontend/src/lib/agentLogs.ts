/**
 * 解析 Agent 日志对话框里的原始文本。覆盖 OxiDNS（tracing）、Mihomo（logrus
 * 文本格式）和 Go Agent（标准库 log）三种格式；认不出的行原样保留，
 * 并作为上一条的续行显示，不推断级别。
 */

export type AgentLogLevel = 'TRACE' | 'DEBUG' | 'INFO' | 'WARN' | 'ERROR'

export interface AgentLogField {
  key: string
  value: string
}

export interface AgentLogEntry {
  id: number
  /** 原始行，用于悬停与复制 */
  raw: string
  /** YYYY-MM-DD；没有时间的行为空 */
  date: string
  /** HH:MM:SS */
  time: string
  level: AgentLogLevel | ''
  /** 完整来源（模块路径与行号） */
  source: string
  /** 来源的最后一段，列表中显示 */
  sourceShort: string
  message: string
  fields: AgentLogField[]
  /** 续行：认不出格式，接在上一条后显示 */
  continuation: boolean
}

const LEVEL_OF: Record<string, AgentLogLevel> = {
  TRACE: 'TRACE',
  DEBUG: 'DEBUG',
  INFO: 'INFO',
  WARN: 'WARN',
  WARNING: 'WARN',
  ERROR: 'ERROR',
  ERR: 'ERROR',
  FATAL: 'ERROR',
  PANIC: 'ERROR',
  CRITICAL: 'ERROR'
}

// 2026-10-20T23:22:20+08:00 T+341494.611 INFO oxidns::plugin::executor::download:184:download completed …
const TRACING_LINE = /^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?\s+(?:T\+[\d.]+\s+)?(TRACE|DEBUG|INFO|WARN|WARNING|ERROR)\s+(?:(\w+(?:::\w+)*:\d+):(?=\S))?\s*([\s\S]*)$/
// time="2026-10-20T23:22:20.123+08:00" level=info msg="[TCP] …"
const LOGRUS_LINE = /^time="(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})[^"]*"\s+level=(\w+)\s+msg="((?:[^"\\]|\\.)*)"\s*([\s\S]*)$/
// 2026/10/20 23:39:46 Starting API server on 0.0.0.0:22919
const GO_LOG_LINE = /^(\d{4})\/(\d{2})\/(\d{2}) (\d{2}:\d{2}:\d{2})(?:\.\d+)? ([\s\S]*)$/

const FIELD_START = /\s([A-Za-z_][\w.]*)=/
const FIELD = /([A-Za-z_][\w.]*)=("(?:[^"\\]|\\.)*"|\S*)/y

const unquote = (value: string) =>
  value.length >= 2 && value.startsWith('"') && value.endsWith('"')
    ? value.slice(1, -1).replace(/\\(["\\])/g, '$1')
    : value

/** 把 "正文 a=1 b=2" 拆成正文与参数；无法完整识别的尾部并回正文 */
export const splitFields = (text: string): { message: string; fields: AgentLogField[] } => {
  const start = FIELD_START.exec(text)
  if (!start) return { message: text, fields: [] }
  const fields: AgentLogField[] = []
  let index = start.index + 1
  while (index < text.length) {
    FIELD.lastIndex = index
    const match = FIELD.exec(text)
    if (!match) break
    fields.push({ key: match[1], value: unquote(match[2]) })
    index = FIELD.lastIndex
    while (text[index] === ' ') index++
  }
  if (index < text.length) {
    // 参数后面又出现普通文字，说明不是纯参数结构，保持原样
    return { message: text, fields: [] }
  }
  return { message: text.slice(0, start.index).trim(), fields }
}

const shortSource = (source: string) => {
  const parts = source.split('::')
  return parts[parts.length - 1]
}

let nextId = 1

export const parseAgentLogLine = (raw: string): AgentLogEntry => {
  const base = { id: nextId++, raw, date: '', time: '', level: '' as const, source: '', sourceShort: '', message: raw, fields: [], continuation: false }

  let m = TRACING_LINE.exec(raw)
  if (m) {
    const source = m[4] || ''
    return { ...base, date: m[1], time: m[2], level: LEVEL_OF[m[3]], source, sourceShort: shortSource(source), ...splitFields(m[5]) }
  }

  m = LOGRUS_LINE.exec(raw)
  if (m) {
    const { message, fields } = splitFields(unquote(`"${m[4]}"`))
    const extra = m[5] ? splitFields(` ${m[5]}`).fields : []
    return { ...base, date: m[1], time: m[2], level: LEVEL_OF[m[3].toUpperCase()] || '', message, fields: [...fields, ...extra] }
  }

  m = GO_LOG_LINE.exec(raw)
  if (m) {
    return { ...base, date: `${m[1]}-${m[2]}-${m[3]}`, time: m[4], ...splitFields(m[5]) }
  }

  return { ...base, continuation: true }
}

export const parseAgentLogs = (text: string): AgentLogEntry[] =>
  text
    .split(/\r?\n/)
    .filter(line => line.trim() !== '')
    .map(parseAgentLogLine)

const LEVEL_RANK: Record<AgentLogLevel, number> = { TRACE: 0, DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 }

/**
 * 只保留不低于 min 的条目。续行跟随上一条的去留；文件开头没有级别的行保留，
 * 避免认不出格式的日志被整段藏起来。
 */
export const filterLogLevel = (entries: AgentLogEntry[], min: AgentLogLevel | ''): AgentLogEntry[] => {
  if (!min) return entries
  let keep = true
  return entries.filter(entry => {
    if (entry.level) keep = LEVEL_RANK[entry.level] >= LEVEL_RANK[min]
    else if (!entry.continuation) keep = true
    return keep
  })
}

/** Mihomo 的日志等级（控制接口 /logs 与配置 log-level 共用） */
export type MihomoLogLevel = 'debug' | 'info' | 'warning' | 'error'

export const MIHOMO_LOG_LEVELS: ReadonlyArray<{ value: MihomoLogLevel; label: string; level: AgentLogLevel }> = [
  { value: 'debug', label: '调试（debug）', level: 'DEBUG' },
  { value: 'info', label: '信息（info）', level: 'INFO' },
  { value: 'warning', label: '警告（warning）', level: 'WARN' },
  { value: 'error', label: '错误（error）', level: 'ERROR' }
]

export const isMihomoLogLevel = (value: unknown): value is MihomoLogLevel =>
  MIHOMO_LOG_LEVELS.some(item => item.value === value)

export const mihomoLevelRank = (level: MihomoLogLevel) => MIHOMO_LOG_LEVELS.findIndex(item => item.value === level)

export const agentLevelOf = (level: MihomoLogLevel): AgentLogLevel =>
  MIHOMO_LOG_LEVELS.find(item => item.value === level)!.level

/** Agent 转来的一条实时日志；time 为 Agent 收到时的 RFC 3339 时间 */
export interface MihomoLiveLog {
  seq: number
  time: string
  level: string
  payload: string
}

const pad = (value: number) => String(value).padStart(2, '0')

/** 实时日志按浏览器时区显示时间 */
export const mihomoLiveEntry = (log: MihomoLiveLog): AgentLogEntry => {
  const at = new Date(log.time)
  const valid = !Number.isNaN(at.getTime())
  const date = valid ? `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}` : ''
  const time = valid ? `${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}` : ''
  const level = isMihomoLogLevel(log.level) ? agentLevelOf(log.level) : ''
  return {
    id: nextId++, raw: `${date} ${time} ${log.level} ${log.payload}`.trim(), date, time, level,
    source: '', sourceShort: '', message: log.payload, fields: [], continuation: false
  }
}
