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

/**
 * 日志正文按含义着色时的片段类别；空字符串为普通文字。
 * tag 开头的 [TCP] 等标签，src / dst 箭头两侧的来源与目标，kw 为 match / using，
 * rule 命中的规则，direct / reject / proxy 为所走策略，str 为引号字符串，err 为 error: 之后的原因。
 */
export type LogTokenKind = '' | 'tag' | 'src' | 'arrow' | 'dst' | 'kw' | 'rule' | 'direct' | 'reject' | 'proxy' | 'str' | 'err'

export interface LogToken {
  text: string
  kind: LogTokenKind
  /** 命中搜索词 */
  hit?: boolean
}

const LEADING_TAG = /^\[[A-Za-z][\w-]*\]/
// Mihomo 的错误原因都写在 error: 之后，例如 dial … error: i/o timeout
const ERROR_AT = /\berror:/
// 箭头、match 规则、using 策略（只在 match 之后）、引号字符串（单引号两侧不能紧贴字母，
// 避免把 don't 这类缩写当成字符串）。
// 规则写法有 RuleSet(google)（连接日志）和 RuleSet/google（拨号失败）两种
const MESSAGE_TOKEN = /-->|\bmatch ([A-Z][\w-]*(?:\([^)]*\)|\/[^\s)]+)?)|\busing (\S(?:.*\S)?)|"[^"\n]*"|(?<![\w'])'[^'\n]+'(?![\w'])/g
// 来源可能带进程名：192.168.1.5:51234(chrome)
const ADDRESS_BEFORE = /([^\s()/[\]]+(?:\([^)]*\))?)(\s*)$/
const ADDRESS_AFTER = /^(\s*)([^\s()[\]]+)/

const policyKind = (policy: string): LogTokenKind =>
  policy === 'DIRECT' ? 'direct' : /^REJECT\b/.test(policy) ? 'reject' : 'proxy'

/**
 * 把一条日志正文切成着色片段，原文一个字不改，拼起来与输入相同。
 * 主要识别 Mihomo 的连接日志（[TCP] 来源 --> 目标 match 规则 using 策略）；
 * 其他格式（OxiDNS、Agent、系统日志）只给开头标签、引号字符串和 error: 之后的原因着色。
 */
export function tokenizeLogMessage(message: string): LogToken[] {
  const tokens: LogToken[] = []
  const push = (text: string, kind: LogTokenKind = '') => {
    if (!text) return
    const last = tokens[tokens.length - 1]
    if (last && last.kind === kind) last.text += text
    else tokens.push({ text, kind })
  }
  let rest = message
  const tag = LEADING_TAG.exec(rest)
  if (tag) {
    push(tag[0], 'tag')
    rest = rest.slice(tag[0].length)
  }
  const errorAt = rest.search(ERROR_AT)
  const head = errorAt < 0 ? rest : rest.slice(0, errorAt)
  let index = 0
  let wantTarget = false
  let matched = false
  // 箭头后的第一个地址是目标
  const plain = (text: string) => {
    if (wantTarget) {
      wantTarget = false
      const target = ADDRESS_AFTER.exec(text)
      if (target) {
        push(target[1])
        push(target[2], 'dst')
        text = text.slice(target[0].length)
      }
    }
    return text
  }
  for (const match of head.matchAll(MESSAGE_TOKEN)) {
    const at = match.index ?? 0
    const before = plain(head.slice(index, at))
    index = at + match[0].length
    if (match[0] === '-->') {
      const source = ADDRESS_BEFORE.exec(before)
      if (source) {
        push(before.slice(0, source.index))
        push(source[1], 'src')
        push(source[2])
      } else {
        push(before)
      }
      push('-->', 'arrow')
      wantTarget = true
    } else if (match[1] !== undefined) {
      push(before)
      push('match', 'kw')
      push(' ')
      push(match[1], 'rule')
      matched = true
    } else if (match[2] !== undefined && matched) {
      push(before)
      push('using', 'kw')
      push(' ')
      push(match[2], policyKind(match[2]))
    } else if (match[2] !== undefined) {
      push(before + match[0])
    } else {
      push(before)
      push(match[0], 'str')
    }
  }
  push(plain(head.slice(index)))
  if (errorAt >= 0) push(rest.slice(errorAt), 'err')
  return tokens
}

/** 按搜索词（忽略大小写的普通文本）把片段再切开并标出命中部分，可跨片段命中 */
export function markLogTokens(tokens: LogToken[], keyword: string): LogToken[] {
  const needle = keyword.trim().toLowerCase()
  if (!needle) return tokens
  const text = tokens.map(token => token.text).join('').toLowerCase()
  const ranges: Array<[number, number]> = []
  for (let at = text.indexOf(needle); at >= 0; at = text.indexOf(needle, at + needle.length)) {
    ranges.push([at, at + needle.length])
  }
  if (!ranges.length) return tokens
  const result: LogToken[] = []
  let offset = 0
  let range = 0
  for (const token of tokens) {
    const end = offset + token.text.length
    let cursor = offset
    while (cursor < end) {
      while (range < ranges.length && ranges[range][1] <= cursor) range++
      const current = ranges[range]
      const hit = !!current && current[0] <= cursor
      const stop = Math.min(end, current ? (hit ? current[1] : current[0]) : end)
      result.push({ text: token.text.slice(cursor - offset, stop - offset), kind: token.kind, hit })
      cursor = stop
    }
    offset = end
  }
  return result
}
