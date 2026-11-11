/**
 * Mihomo 日志的结构化展示：把连接日志拆成设备、目标、规则、策略，
 * 按「设备 + 目标 + 策略」合并重复，警告和错误单独汇总。认不出的行原样保留。
 *
 * 连接日志的几种句式（mihomo tunnel）：
 *   [TCP] 192.168.1.5:5000 --> example.com:443 match RuleSet(Proxy) using 节点选择[香港 01]
 *   [TCP] 192.168.1.5:5000(curl, uid=0) --> 1.2.3.4:443 doesn't match any rule using DIRECT
 *   [UDP] 192.168.1.5:5000 --> example.com:443 using GLOBAL
 *   [TCP] mihomo --> 10.0.0.1:5001 match GeoIP(lan) using DIRECT          （内核自身发起）
 *   [TCP] dial 节点选择 (match RuleSet(Proxy)) 192.168.1.5:5000 --> example.com:443 error: …
 */
import type { AgentLogEntry, AgentLogLevel } from '@/lib/agentLogs'
import { deviceNote } from '@/lib/device-notes'

export type PolicyKind = 'direct' | 'reject' | 'proxy'

export interface MihomoConnectionLog {
  network: 'TCP' | 'UDP'
  /** 来源 IP；内核自身发起时为空 */
  sourceIP: string
  /** 来源的进程信息（如 curl, uid=0），没有时为空 */
  process: string
  /** 内核自身发起的连接 */
  inner: boolean
  target: string
  host: string
  port: string
  /** 规则原文，如 RuleSet(Proxy)；没匹配规则时为空 */
  rule: string
  /** 列表中显示的规则，如 Proxy、GeoIP lan、未匹配 */
  ruleLabel: string
  /** 入口策略组；单个出站时与 node 相同 */
  group: string
  node: string
  kind: PolicyKind
}

export interface MihomoDialFailure {
  network: 'TCP' | 'UDP'
  sourceIP: string
  inner: boolean
  target: string
  policy: string
  ruleLabel: string
  error: string
}

export interface MihomoLogRow {
  entry: AgentLogEntry
  connection: MihomoConnectionLog | null
  failure: MihomoDialFailure | null
}

const CONNECTION = /^\[(TCP|UDP)\] (.+?) --> (\S+) (?:match (.+?) using (.+)|doesn't match any rule using (.+)|using (.+))$/
const DIAL_FAILURE = /^\[(TCP|UDP)\] dial (.+?) \(match (.+?)\) (.+?) --> (\S+) error: ([\s\S]+)$/
const SOURCE = /^(\[[^\]]+\]|[^\s:()[\]]+):\d+(?:\((.*)\))?$/
const INNER_SOURCE = /^(mihomo|clash|Inner|\[Inner\])$/i

function splitSource(source: string): { sourceIP: string; process: string; inner: boolean } {
  if (INNER_SOURCE.test(source)) return { sourceIP: '', process: '', inner: true }
  const match = SOURCE.exec(source)
  if (!match) return { sourceIP: source, process: '', inner: false }
  return { sourceIP: match[1].replace(/^\[|\]$/g, ''), process: match[2] || '', inner: false }
}

function splitTarget(target: string): { host: string; port: string } {
  const match = /^(\[[^\]]+\]|.+):(\d+)$/.exec(target)
  return match ? { host: match[1], port: match[2] } : { host: target, port: '' }
}

/** RuleSet(Proxy) → Proxy；GeoIP(lan) → GeoIP lan；Match() → Match */
export function ruleLabel(rule: string): string {
  const match = /^([\w-]+)\((.*)\)$/.exec(rule)
  if (!match) return rule
  const [, type, payload] = match
  if (!payload) return type
  return type === 'RuleSet' ? payload : `${type} ${payload}`
}

/** 策略组[节点] 拆开；组名在前，节点名可能自带括号，所以按第一个 [ 拆 */
function splitChain(chain: string): { group: string; node: string } {
  const open = chain.indexOf('[')
  if (open > 0 && chain.endsWith(']')) return { group: chain.slice(0, open), node: chain.slice(open + 1, -1) }
  return { group: chain, node: chain }
}

export function policyKind(node: string): PolicyKind {
  const name = node.trim().toUpperCase()
  if (name === 'DIRECT') return 'direct'
  if (name.startsWith('REJECT')) return 'reject'
  return 'proxy'
}

export function parseConnectionLog(message: string): MihomoConnectionLog | null {
  const match = CONNECTION.exec(message.trim())
  if (!match) return null
  const [, network, source, target, matchedRule, matchedChain, unmatchedChain, modeChain] = match
  const chain = (matchedChain ?? unmatchedChain ?? modeChain ?? '').trim()
  const rule = matchedRule ?? ''
  const { group, node } = splitChain(chain)
  return {
    network: network as 'TCP' | 'UDP',
    ...splitSource(source),
    target,
    ...splitTarget(target),
    rule,
    ruleLabel: matchedRule ? ruleLabel(rule) : unmatchedChain ? '未匹配' : '模式',
    group, node,
    kind: policyKind(node)
  }
}

/** dial 日志里的规则写成 RuleSet/private_ip，也可能是 RuleSet(Proxy) */
function dialRuleLabel(rule: string): string {
  const slash = rule.indexOf('/')
  return slash > 0 && !rule.includes('(') ? ruleLabel(`${rule.slice(0, slash)}(${rule.slice(slash + 1)})`) : ruleLabel(rule)
}

export function parseDialFailure(message: string): MihomoDialFailure | null {
  const match = DIAL_FAILURE.exec(message.trim())
  if (!match) return null
  const [, network, policy, rule, source, target, error] = match
  const { sourceIP, inner } = splitSource(source)
  return { network: network as 'TCP' | 'UDP', sourceIP, inner, target, policy, ruleLabel: dialRuleLabel(rule), error: error.trim() }
}

// 实时日志每秒追加几条，已解析过的条目直接复用
const parsedRows = new WeakMap<AgentLogEntry, MihomoLogRow>()

export function toLogRows(entries: AgentLogEntry[]): MihomoLogRow[] {
  return entries.map(entry => {
    let row = parsedRows.get(entry)
    if (!row) {
      const connection = entry.continuation ? null : parseConnectionLog(entry.message)
      row = { entry, connection, failure: connection || entry.continuation ? null : parseDialFailure(entry.message) }
      parsedRows.set(entry, row)
    }
    return row
  })
}

/** 设备名：「客户端设备」里的备注，没有时显示 IP；内核自身发起的显示 Mihomo */
export function deviceLabel(notes: Record<string, string>, sourceIP: string, inner: boolean): string {
  if (inner) return 'Mihomo 内核'
  return (sourceIP && deviceNote(notes, sourceIP)) || sourceIP
}

/** 搜索覆盖原文和设备备注名 */
export function rowMatches(row: MihomoLogRow, keyword: string, notes: Record<string, string>): boolean {
  if (!keyword) return true
  if (row.entry.raw.toLowerCase().includes(keyword)) return true
  const ip = row.connection?.sourceIP || row.failure?.sourceIP || ''
  const name = ip ? deviceNote(notes, ip) : ''
  return !!name && name.toLowerCase().includes(keyword)
}

export const isAlertLevel = (level: AgentLogLevel | '') => level === 'WARN' || level === 'ERROR'

/** 合并后的一组：连接按「设备 + 目标 + 策略」合并，其他日志按原文合并 */
export interface MihomoLogGroup {
  key: string
  first: MihomoLogRow
  last: MihomoLogRow
  count: number
  /** 组内最近的若干条原始日志，展开时显示 */
  recent: MihomoLogRow[]
}

const GROUP_RECENT = 20

/** 合并重复；按最近一次出现的时间排序，最新的在最后（与跟随日志一致） */
export function groupLogRows(rows: MihomoLogRow[], keyOf: (row: MihomoLogRow) => string = defaultGroupKey): MihomoLogGroup[] {
  const groups = new Map<string, MihomoLogGroup>()
  rows.forEach(row => {
    const key = keyOf(row)
    const group = groups.get(key)
    if (group) {
      // 重新插入，让 Map 的顺序保持为「最近出现」的顺序
      groups.delete(key)
      group.last = row
      group.count++
      group.recent.push(row)
      if (group.recent.length > GROUP_RECENT) group.recent.shift()
      groups.set(key, group)
    } else {
      groups.set(key, { key, first: row, last: row, count: 1, recent: [row] })
    }
  })
  return [...groups.values()]
}

export function defaultGroupKey(row: MihomoLogRow): string {
  const c = row.connection
  if (c) return `c|${c.inner ? '' : c.sourceIP}|${c.network}|${c.target}|${c.group}|${c.node}`
  const f = row.failure
  if (f) return `f|${f.sourceIP}|${f.target}|${f.policy}|${f.error}`
  return `m|${row.entry.level}|${messageShape(row.entry.message)}`
}

/** 只差地址、端口、计数等数字的日志视为同一种（如 [Metadata] not valid 每次指针都不同） */
export function messageShape(message: string): string {
  return message.replace(/0x[0-9a-f]+/gi, '0x#').replace(/\d+/g, '#')
}
