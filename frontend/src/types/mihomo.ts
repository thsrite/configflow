/** Mihomo 控制接口（external-controller）返回结构中面板用到的字段 */

export type MihomoMode = 'rule' | 'global' | 'direct'

export interface MihomoDelayHistory {
  time: string
  delay: number
}

export interface MihomoProxy {
  name: string
  type: string
  udp?: boolean
  alive?: boolean
  now?: string
  /** 策略组成员 */
  all?: string[]
  /** url-test / fallback 手动固定的成员 */
  fixed?: string
  testUrl?: string
  hidden?: boolean
  icon?: string
  history?: MihomoDelayHistory[]
  extra?: Record<string, { alive?: boolean; history?: MihomoDelayHistory[] }>
}

export interface MihomoConnection {
  id: string
  metadata: {
    network: string
    type: string
    sourceIP: string
    sourcePort: string
    destinationIP: string
    destinationPort: string
    host: string
    process?: string
    processPath?: string
    sniffHost?: string
    remoteDestination?: string
    inboundName?: string
    inboundUser?: string
    inboundIP?: string
    inboundPort?: string
    dnsMode?: string
    specialProxy?: string
    specialRules?: string
  }
  upload: number
  download: number
  start: string
  chains: string[]
  rule: string
  rulePayload: string
}

export interface MihomoConnectionsSnapshot {
  downloadTotal: number
  uploadTotal: number
  connections: MihomoConnection[] | null
  memory?: number
}

/** 面板采样后补充的显示字段，不写入核心配置。 */
export interface ConnectionRow extends MihomoConnection {
  raw: MihomoConnection
  target: string
  chain: string
  upSpeed: number
  downSpeed: number
  /** 前端发现连接消失的时间（毫秒）；活跃连接为空 */
  closedAt?: number
}

export interface MihomoRule {
  type: string
  payload: string
  proxy: string
  size?: number
}

export interface MihomoProvider {
  name: string
  type: string
  vehicleType: string
  behavior?: string
  format?: string
  ruleCount?: number
  proxies?: MihomoProxy[]
  updatedAt?: string
  subscriptionInfo?: { Upload?: number; Download?: number; Total?: number; Expire?: number }
}

export const GROUP_TYPES = new Set(['Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Relay'])
/**
 * 代理集合按 Compatible 在前排序。Compatible 是各策略组自带的集合，后端只给出成员名字；
 * 按名字合并节点时排在前面，同名的订阅节点不会被只有名字的条目覆盖。
 */
export const compatibleFirst = (providers: MihomoProvider[]) =>
  [...providers].sort((a, b) => Number(b.vehicleType === 'Compatible') - Number(a.vehicleType === 'Compatible'))
/** 可在面板里手动切换成员的策略组；url-test / fallback 切换后会固定，直到取消固定 */
export const SWITCHABLE_GROUP_TYPES = new Set(['Selector', 'URLTest', 'Fallback'])

export const GROUP_TYPE_LABELS: Record<string, string> = {
  Selector: '手动选择',
  URLTest: '自动测速',
  Fallback: '故障转移',
  LoadBalance: '负载均衡',
  Relay: '链式代理'
}

export function latestDelay(proxy: MihomoProxy | undefined, testUrl?: string): number | undefined {
  if (!proxy) return undefined
  const history = (testUrl && proxy.extra?.[testUrl]?.history?.length ? proxy.extra[testUrl].history : proxy.history) || []
  return history.length ? history[history.length - 1].delay : undefined
}

export type DelayTone = 'success' | 'warning' | 'danger' | 'muted'

export function delayTone(delay: number | undefined): DelayTone {
  if (delay === undefined) return 'muted'
  if (delay <= 0) return 'danger'
  if (delay < 300) return 'success'
  if (delay < 800) return 'warning'
  return 'danger'
}

export function delayText(delay: number | undefined): string {
  if (delay === undefined) return '—'
  return delay <= 0 ? '超时' : `${delay} ms`
}

/** 按 GLOBAL 组成员顺序排列策略组（与配置文件中的书写顺序一致），GLOBAL 自身放最后 */
export function orderedGroups(proxies: Record<string, MihomoProxy>, includeHidden = false): MihomoProxy[] {
  const groups = Object.values(proxies).filter(proxy => GROUP_TYPES.has(proxy.type) && (includeHidden || !proxy.hidden))
  const order = proxies.GLOBAL?.all || []
  const rank = new Map(order.map((name, index) => [name, index]))
  return groups.sort((a, b) => {
    if (a.name === 'GLOBAL') return 1
    if (b.name === 'GLOBAL') return -1
    return (rank.get(a.name) ?? Number.MAX_SAFE_INTEGER) - (rank.get(b.name) ?? Number.MAX_SAFE_INTEGER)
  })
}

export function panelError(failure: any, fallback = '请求失败，请重试。'): string {
  return failure?.response?.data?.message || failure?.message || fallback
}

/** Mihomo 自身发起的连接（如 DNS 上游、测速）没有来源 IP，与常见面板一致显示为 Inner */
export const INNER_SOURCE = 'Inner'
export const sourceLabel = (connection: MihomoConnection) => connection.metadata.sourceIP || INNER_SOURCE

/** 编译搜索词：是合法正则时按正则（忽略大小写）匹配，否则按普通文本包含匹配 */
export function connectionMatcher(keyword: string): (value: string | undefined) => boolean {
  const text = keyword.trim()
  if (!text) return () => true
  try {
    const pattern = new RegExp(text, 'i')
    return value => !!value && pattern.test(value)
  } catch {
    const lower = text.toLowerCase()
    return value => !!value && value.toLowerCase().includes(lower)
  }
}

/** IPv4 按数值排序，其余（IPv6、Inner）排在后面按字典序 */
export function compareSources(a: string, b: string): number {
  const v4 = (ip: string) => /^\d+\.\d+\.\d+\.\d+$/.test(ip) ? ip.split('.').reduce((sum, part) => sum * 256 + Number(part), 0) : null
  if (a === INNER_SOURCE || b === INNER_SOURCE) return a === b ? 0 : a === INNER_SOURCE ? -1 : 1
  const [x, y] = [v4(a), v4(b)]
  if (x !== null && y !== null) return x - y
  if (x !== null) return -1
  if (y !== null) return 1
  return a.localeCompare(b)
}

export type MemberSort = 'config' | 'delay' | 'name'

/** 策略页的显示偏好，只存在当前浏览器 */
export interface ProxyPanelSettings {
  sort: MemberSort
  groupByProvider: boolean
  hideUnavailable: boolean
  showHiddenGroups: boolean
  closeConnectionsOnSwitch: boolean
  showFinalNode: boolean
  disableTextSelection: boolean
  cardMinWidth: number
}

export const DEFAULT_CARD_MIN_WIDTH = 145
export const CARD_MIN_WIDTH_RANGE = [100, 320] as const
export const DEFAULT_PROXY_SETTINGS: ProxyPanelSettings = {
  sort: 'config',
  groupByProvider: false,
  hideUnavailable: false,
  showHiddenGroups: false,
  closeConnectionsOnSwitch: true,
  showFinalNode: false,
  disableTextSelection: true,
  cardMinWidth: DEFAULT_CARD_MIN_WIDTH
}
const PROXY_SETTINGS_KEY = 'configflow:mihomo-panel:proxy-settings'

export function clampCardWidth(value: unknown): number {
  const number = Math.round(Number(value))
  if (!Number.isFinite(number)) return DEFAULT_CARD_MIN_WIDTH
  return Math.min(CARD_MIN_WIDTH_RANGE[1], Math.max(CARD_MIN_WIDTH_RANGE[0], number))
}

/** 读取时逐项校验类型，旧版本或手改的存储值不会让页面出错 */
export function loadProxySettings(): ProxyPanelSettings {
  let stored: Record<string, unknown> = {}
  try { stored = JSON.parse(window.localStorage.getItem(PROXY_SETTINGS_KEY) || '{}') || {} } catch { /* Fall back to defaults. */ }
  const result = { ...DEFAULT_PROXY_SETTINGS }
  for (const key of Object.keys(result) as Array<keyof ProxyPanelSettings>) {
    if (typeof stored[key] === typeof result[key]) (result as Record<string, unknown>)[key] = stored[key]
  }
  if (!['config', 'delay', 'name'].includes(result.sort)) result.sort = 'config'
  result.cardMinWidth = clampCardWidth(result.cardMinWidth)
  return result
}

export function saveProxySettings(settings: ProxyPanelSettings) {
  try { window.localStorage.setItem(PROXY_SETTINGS_KEY, JSON.stringify(settings)) } catch { /* Settings still apply for this visit. */ }
}

/** 沿 now 逐级找到最终出站节点；遇到环或超过 8 层时停在当前位置 */
export function finalNode(proxies: Record<string, MihomoProxy>, name: string): string {
  let current = name
  const seen = new Set<string>()
  while (proxies[current]?.now && GROUP_TYPES.has(proxies[current].type) && !seen.has(current) && seen.size < 8) {
    seen.add(current)
    current = proxies[current].now as string
  }
  return current
}
