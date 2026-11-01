import { getDomain } from 'tldts-icann'
import { INNER_SOURCE, sourceLabel, type MihomoConnection, type MihomoRule } from '@/types/mihomo'

/** 概览「连接统计」的聚合方式；进程在旁路由上只有本机连接才有，不提供 */
export type OverviewAggregate = 'source' | 'destination' | 'outbound' | 'group'

export const OVERVIEW_AGGREGATES: Array<{ value: OverviewAggregate; label: string; column: string }> = [
  { value: 'source', label: '按设备', column: '设备' },
  { value: 'destination', label: '按目标', column: '目标' },
  { value: 'outbound', label: '按出站节点', column: '出站节点' },
  { value: 'group', label: '按策略组', column: '策略组' }
]

export interface OverviewStatPart {
  key: string
  download: number
  upload: number
  downSpeed: number
  upSpeed: number
  count: number
}

export interface OverviewStatRow extends OverviewStatPart {
  /** 明细：按出站节点汇总时按设备拆开，其余按出口节点拆开 */
  parts: OverviewStatPart[]
}

/** 目标按主域名归并（a.b.example.com.cn → example.com.cn），IP 与无法识别的主机保持原样 */
export function destinationKey(connection: MihomoConnection): string {
  const meta = connection.metadata
  const host = (meta.host || meta.sniffHost || '').toLowerCase().replace(/\.$/, '')
  if (!host) return meta.destinationIP || '—'
  return getDomain(host) || host
}

/** 出口节点：chains 的第一个，即实际出站的节点（直连为 DIRECT） */
export const outboundOf = (connection: MihomoConnection) => connection.chains[0] || '—'

function aggregateKey(connection: MihomoConnection, by: OverviewAggregate): string {
  if (by === 'source') return sourceLabel(connection)
  if (by === 'destination') return destinationKey(connection)
  // chains 从出口节点排到入口策略组
  if (by === 'outbound') return outboundOf(connection)
  return connection.chains[connection.chains.length - 1] || '—'
}

/**
 * 当前活动连接按所选方式汇总：流量是这些连接建立以来的累计字节，速度来自两次采样之差。
 * 已关闭的连接不在快照里，不计入。
 */
export function aggregateConnections(
  connections: MihomoConnection[],
  by: OverviewAggregate,
  speeds: Map<string, { up: number; down: number }>
): OverviewStatRow[] {
  const rows = new Map<string, OverviewStatRow & { partMap: Map<string, OverviewStatPart> }>()
  const add = (target: OverviewStatPart, connection: MihomoConnection) => {
    const speed = speeds.get(connection.id)
    target.download += connection.download
    target.upload += connection.upload
    target.downSpeed += speed?.down || 0
    target.upSpeed += speed?.up || 0
    target.count++
  }
  for (const connection of connections) {
    const key = aggregateKey(connection, by)
    let row = rows.get(key)
    if (!row) rows.set(key, row = { key, download: 0, upload: 0, downSpeed: 0, upSpeed: 0, count: 0, parts: [], partMap: new Map() })
    add(row, connection)
    const partKey = by === 'outbound' ? sourceLabel(connection) : outboundOf(connection)
    let part = row.partMap.get(partKey)
    if (!part) row.partMap.set(partKey, part = { key: partKey, download: 0, upload: 0, downSpeed: 0, upSpeed: 0, count: 0 })
    add(part, connection)
  }
  return [...rows.values()].map(({ partMap, ...row }) => ({
    ...row,
    parts: [...partMap.values()].sort((a, b) => b.download + b.upload - (a.download + a.upload) || a.key.localeCompare(b.key))
  }))
}

export type OutletKind = 'direct' | 'proxy' | 'reject' | 'none'

/**
 * 按 Mihomo 当前的节点类型判断出口是直连、代理还是拦截。配置里自定义的 type: direct 节点也算直连；
 * proxy-providers 的节点不在 /proxies 里，按名称以外一律视为代理。
 */
export function outletKind(name: string, types: Record<string, string> = {}): OutletKind {
  if (!name || name === '—') return 'none'
  const type = types[name]
  if (name === 'DIRECT' || type === 'Direct') return 'direct'
  if (name.startsWith('REJECT') || type === 'Reject' || type === 'RejectDrop') return 'reject'
  return 'proxy'
}

export const isInnerSource = (key: string) => key === INNER_SOURCE

/** 流量记录：空键是「未归属」（采样间隔内开始又结束的短连接），* 是并入的「其他」目标 */
export const UNATTRIBUTED_KEY = ''
export const OTHER_TARGET_KEY = '*'

export type TrafficRange = 'live' | '24h' | '7d' | '30d'
export const TRAFFIC_RANGES: Array<{ value: TrafficRange; label: string }> = [
  { value: 'live', label: '实时' },
  { value: '24h', label: '24 小时' },
  { value: '7d', label: '7 天' },
  { value: '30d', label: '30 天' }
]

export interface TrafficHistoryRow {
  key: string
  up: number
  down: number
  conns: number
  parts?: TrafficHistoryRow[]
}

export interface TrafficHistoryData {
  enabled: boolean
  since: string | null
  supported: boolean
  min_version: string
  retention_days: number
  retention_choices: number[]
  range?: string
  by?: string
  step?: number
  start?: string
  rows?: TrafficHistoryRow[]
  omitted?: number
  total?: { up: number; down: number; conns: number }
  /** 各出口节点的总量（不含未归属），用于直连 / 代理合计 */
  outbounds?: TrafficHistoryRow[]
  trend?: Array<{ at: string; up: number; down: number }>
}

/** Mihomo v1.19.19 起 /rules 带 extra（命中、未命中次数与时间），更早的内核没有 */
export interface MihomoRuleWithExtra extends MihomoRule {
  index?: number
  extra?: { disabled?: boolean; hitCount?: number; hitAt?: string; missCount?: number; missAt?: string }
}

export interface RuleHitItem {
  index: number
  type: string
  payload: string
  proxy: string
  hits: number
  hitAt?: string
}

export function ruleHitCounts(rules: MihomoRuleWithExtra[]): { supported: boolean; total: number; items: RuleHitItem[] } {
  const supported = rules.some(rule => rule.extra)
  const items = rules
    .map((rule, position) => ({
      index: rule.index ?? position,
      type: rule.type,
      payload: rule.payload,
      proxy: rule.proxy,
      hits: rule.extra?.hitCount || 0,
      hitAt: rule.extra?.hitAt
    }))
    .filter(item => item.hits > 0)
    .sort((a, b) => b.hits - a.hits || a.index - b.index)
  return { supported, total: items.reduce((sum, item) => sum + item.hits, 0), items }
}

/** 延迟测试：直连站点走 DIRECT，海外站点走所选策略组当前的出口 */
export interface LatencyTarget {
  key: string
  label: string
  url: string
  via: 'direct' | 'group'
}

export const LATENCY_TARGETS: LatencyTarget[] = [
  { key: 'baidu', label: '百度', url: 'https://www.baidu.com', via: 'direct' },
  { key: 'google', label: 'Google', url: 'https://www.google.com/generate_204', via: 'group' },
  { key: 'cloudflare', label: 'Cloudflare', url: 'https://cp.cloudflare.com/generate_204', via: 'group' },
  { key: 'github', label: 'GitHub', url: 'https://github.com', via: 'group' },
  { key: 'youtube', label: 'YouTube', url: 'https://www.youtube.com/generate_204', via: 'group' }
]
