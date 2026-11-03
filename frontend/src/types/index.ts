export interface Subscription {
  id: string
  name: string
  url: string
  type: string
  enabled: boolean
  interval?: number
  /** 健康检查地址，留空用默认（境外）；回家/内网订阅建议填国内地址 */
  health_check?: Record<string, any>
  health_check_url?: string
  /** 节点名筛选正则，只保留匹配的节点；留空表示不筛选 */
  regex_filter?: string
  /** 筛选后的节点数 */
  cached_node_count?: number | null
  /** 缓存中的原始节点数（筛选前） */
  cached_total_count?: number | null
  cached_updated_at?: string | null
}

export interface ProxyNode {
  id: string
  name: string
  proxy_string: string  // 节点字符串，如 ss://..., vmess://..., trojan://... 等
  enabled: boolean
  remark?: string  // 备注
  subscription_id?: string
  subscription_name?: string
  params?: Record<string, unknown>
}

export interface Rule {
  id: string
  rule_type: string
  value: string
  policy: string
  enabled: boolean
  order?: number  // 排序顺序
  remark?: string  // 备注
  no_resolve?: boolean  // no-resolve 开关
  group_name?: string  // 分组名称
  udp_fallback?: 'DIRECT' | 'REJECT'  // 仅 MATCH：节点不支持 UDP 时的去向，缺省跟随客户端
  ntp_direct?: boolean  // 仅 MATCH：生成时在最前面加入 UDP 123 直连
}

export interface RuleSet {
  id: string
  name: string
  url: string
  behavior: string
  enabled: boolean
  policy?: string
  order?: number  // 排序顺序
  library_rule_id?: string  // 关联的规则仓库ID
  remark?: string  // 备注
  no_resolve?: boolean  // no-resolve 开关
  group_name?: string  // 分组名称
}

export interface ProxyGroup {
  id: string
  name: string
  type: string
  enabled: boolean
  chain?: { entry: { type: 'node' | 'group'; id: string }; exit: { type: 'node' | 'group'; id: string } }
  aggregations?: string[]
  aggregation_regex?: string
  aggregation_exclude_regex?: string
  follow_group?: string
  proxies_order?: { type: string; id: string }[]
  proxy_order?: string
  use?: string[]
  include_all?: boolean
  'include-all'?: boolean
  url?: string
  interval?: number
  subscriptions?: string[]    // 订阅来源
  regex?: string              // 正则过滤规则
  exclude_regex?: string      // 排除匹配的订阅节点
  manual_nodes?: string[]     // 手动选择的节点
  include_groups?: string[]   // 引用的策略组
  strategy?: 'round-robin' | 'consistent-hashing' | 'sticky-sessions'  // 负载均衡策略
  timeout?: number
  tolerance?: number
  'expected-status'?: string
  lazy?: boolean              // 懒加载
  // 以下为兼容旧数据
  proxies?: string[]          // 已废弃，兼容旧数据
  source?: 'subscription' | 'node' | 'strategy'  // 已废弃，兼容旧数据
}

/** Hosts taken from subscriptions while generating Mihomo (preview response). */
export interface SubscriptionHostsItem {
  subscription: string
  domain: string
  value?: string | string[]
  status: 'merged' | 'ignored'
  /** merged: an address list, or a domain (applies to nodes only through Mihomo's DNS listener). */
  kind?: 'address' | 'domain'
  reason?: string
  detail?: string
}

/** A mapped node domain resolved through Mihomo's DNS listener. */
export interface SubscriptionHostsPolicyEntry {
  domain: string
  /** Where the hosts chain from the node domain ends. */
  target: string
  /** configflow: written into proxy-server-nameserver-policy; user: the user's own policy already points there. */
  source: 'configflow' | 'user'
}

/** Present only when a merged domain-to-domain mapping ends on a domain. */
export interface SubscriptionHostsDns {
  /** Mappings whose hosts chain ends on a domain. */
  mapped: number
  /** At least one mapped node domain now resolves through the listener. */
  applied: boolean
  listener: string | null
  policy: SubscriptionHostsPolicyEntry[]
  /** Patterns ConfigFlow added to fake-ip-filter (written as real-ip rules in rule mode). */
  filter: string[]
  /** Distinct targets kept out of Fake-IP by those entries. */
  targets_filtered: number
  /** Targets the written fake-ip-filter already covered. */
  targets_covered: number
  fake_ip: 'blacklist' | 'rule' | 'whitelist' | 'not_needed' | null
  skipped: Array<{ domain: string; target: string; reason: 'policy_conflict' | 'whitelist_covers' }>
  warnings: string[]
  notes: string[]
  /** Oldest Mihomo that reads proxy-server-nameserver-policy. */
  min_version: string
  /** Agents of this profile whose reported core is older (preview only). */
  outdated_agents?: Array<{ name: string; version: string }>
}

export interface SubscriptionHostsReport {
  merged: number
  ignored: number
  domain_mapped: number
  subscriptions: Array<{ id: string; name: string; total: number; merged: number; ignored: number }>
  items: SubscriptionHostsItem[]
  dns?: SubscriptionHostsDns
}

/** 订阅自带链式节点（dialer-proxy）：自动补上的中转分组与跳过的节点 */
export interface SubscriptionDialersReport {
  groups: Array<{ subscription: string; target: string; group: string; members: string[]; copied: boolean }>
  skipped: Array<{ subscription: string; node: string; target: string; reason: 'refresh' | 'missing' }>
  skipped_count: number
}

export interface AgentContentSync {
  status: 'synced' | 'changed' | 'unknown' | 'never-pushed' | 'checking'
  /** Stable server reason code; never display raw errors as UI copy. */
  reason?: string
  checked_at?: string
}

export interface AgentSystemMetrics {
  cpu?: { usage_percent: number; core_count: number }
  memory?: { total: number; used: number; available: number; used_percent: number }
  disk?: { total: number; used: number; free: number; used_percent: number }
  network?: { bytes_sent: number; bytes_recv: number; speed_sent: number; speed_recv: number }
  collected_at: string
}

export interface Agent {
  id: string
  name: string
  host: string
  port: number
  token: string
  service_type: 'mihomo' | 'surge' | 'mosdns' | 'oxidns'
  deployment_method?: 'shell' | 'docker' | 'unknown'
  status: 'online' | 'offline'
  last_heartbeat: string
  version: string
  config_version: string
  config_revision?: number
  content_sync?: AgentContentSync
  system_metrics?: AgentSystemMetrics
  latest_deployment?: import('@/composables/useAgentDeployments').AgentDeployment
  deployments?: Record<string, import('@/composables/useAgentDeployments').AgentDeployment>
  enabled: boolean
  profile_id?: string
  has_update?: boolean
  upgrade_available?: boolean
  latest_upgrade?: import('@/composables/useAgentUpgrades').AgentUpgrade
  dns_follow?: import('./agentDNSFollow').AgentDNSFollowBinding | null
  dns_follow_state?: import('./agentDNSFollow').AgentDNSFollowState | null
  oxidns_ha?: import('./agentDNSHA').AgentDNSHAConfig | null
  oxidns_ha_state?: import('./agentDNSHA').AgentDNSHAState | null
  oxidns_ha_publication?: import('./agentHAPublication').AgentHAPublication | null
  created_at?: string
  updated_at?: string
  cpu?: number
  memory?: number
}

/** 策略规则的 OxiDNS DNS 处理：`GET/PUT /api/rules/dns` 与 `POST /api/rules/compose` 的词汇表 */
export type RuleDnsBlock = 'refused' | 'nxdomain' | 'nodata' | 'blackhole'
export type RuleDnsRoute = 'direct' | 'proxy' | RuleDnsBlock | `upstream:${string}`
export type RuleDnsValue = 'follow' | 'default' | RuleDnsRoute
export type RuleDnsStateKind = 'follow' | 'follow_drift' | 'default' | 'explicit' | 'disabled' | 'unsupported'

/** 单条规则的显示状态；省略的规则即默认上游 */
export interface RuleDnsState {
  state: RuleDnsStateKind
  route?: RuleDnsRoute | null
  follow_route?: 'direct' | 'proxy' | null
  /** 已停用但启用后跟随策略 */
  follow?: boolean
  upstream_label?: string
  in_lists?: boolean
  reason?: string
}

export interface RuleDnsUpstream {
  key: string
  source: 'local' | 'remote' | 'fallback'
  addr: string
  label: string
  /** 被 DNS 联动绑定的上游不能固定 */
  linked: boolean
}

export interface RuleDnsAgent {
  id: string
  name: string
  service_type: string
  auto_sync: { library: boolean; rules: boolean }
}

export interface RuleDnsEngine {
  mode: 'template' | 'native'
  editable: boolean
  reason: string
  default_forward: string
  /** 服务端按策略组配置解析的默认 DNS 去向；缺省兼容旧版本响应 */
  policy_routes?: Record<string, 'direct' | 'proxy' | null>
  prefer_ipv4: boolean
  local_probe_active: boolean
  oxidns_in_use: boolean
  warnings: string[]
  agents: RuleDnsAgent[]
}

export interface RuleDnsSnapshot {
  engine: RuleDnsEngine
  upstreams: RuleDnsUpstream[]
  rules: Record<string, RuleDnsState>
  mosdns: Record<string, 'direct' | 'proxy'>
}

export type ComposeSource =
  | { kind: 'values'; rule_type: string; text: string }
  | { kind: 'library'; library_rule_ids: string[] }
  | { kind: 'new_library'; library: { name: string; source_type: 'url' | 'content'; url?: string; content?: string; behavior: string; format?: string; enabled: boolean } }

export interface ComposePayload {
  source: ComposeSource
  rule: { policy: string; remark: string; group_name: string; no_resolve: boolean; enabled: boolean; position: 'top' | 'bottom' }
  dns?: RuleDnsValue
  /** 只作用于 IP 行；缺省时等于 dns */
  dns_ip?: RuleDnsValue
  base_url?: string
}

export interface ComposeResult {
  success: true
  data: { rules: Array<Record<string, any>>; library: Record<string, any> | null }
  dns: Record<string, RuleDnsState>
  skipped: Array<{ line: number; value: string; existing_id: string; index: number; enabled: boolean }>
  warnings: string[]
  draft: boolean
  /** 有指定上游被 OxiDNS 规则自动同步暂扣，需手动发布 */
  pinned_withheld?: boolean
}
