import axios from 'axios'
import { notify } from '@/lib/feedback'
import { explainRequestError } from '@/lib/requestError'
import router from '@/router'
import { getAuthSessionGeneration, getAuthToken, invalidateAuthSession } from '@/authSession'
import { beginScopedRequest, clearActiveProfileId, endScopedRequest, getActiveProfileId } from '@/profileContext'
import type { GatewayForwardingEntry } from '@/lib/gatewayForwarding'
import type { ComposePayload, ComposeResult, RuleDnsSnapshot, RuleDnsState, RuleDnsValue } from '@/types'

const api = axios.create({
  baseURL: '/api',
  timeout: 30000
})

declare module 'axios' {
  interface InternalAxiosRequestConfig {
    profileRequestTracked?: boolean
    authSessionToken?: string | null
    authSessionGeneration?: number
  }
}

const scopedPath = /^\/(?:profiles\/[^/]+\/generate|proxy-groups(?:\/|$)|rules(?:\/|$)|rule-sets(?:\/|$)|rule-configs(?:\/|$)|custom-config(?:\/|$)|(?:mosdns|oxidns)(?:\/|$)|stats(?:\/|$)|domain-discovery(?:\/|$)|routers(?:\/|$)|client-devices(?:\/|$|\?))/
const profileOptions = (profileId = getActiveProfileId()) => ({
  headers: { 'X-ConfigFlow-Profile': profileId }
})

let profileRecovery: Promise<void> | null = null

const recoverFromMissingProfile = (): Promise<void> => {
  if (!profileRecovery) {
    clearActiveProfileId()
    profileRecovery = import('@/stores/profile')
      .then(({ useProfileStore }) => useProfileStore().refreshProfiles())
      .then(() => undefined)
      .finally(() => {
        profileRecovery = null
      })
  }
  return profileRecovery
}

// 请求拦截器 - 添加 token
api.interceptors.request.use(
  (config) => {
    const token = getAuthToken()
    config.authSessionToken = token
    config.authSessionGeneration = getAuthSessionGeneration()
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    if (!config.headers.has('X-ConfigFlow-Profile')) {
      config.headers.set('X-ConfigFlow-Profile', getActiveProfileId())
    }
    if (scopedPath.test(config.url || '')) {
      config.profileRequestTracked = true
      beginScopedRequest()
    }
    return config
  },
  (error) => {
    return Promise.reject(error)
  },
  { synchronous: true }
)

// 响应拦截器 - 处理认证错误
api.interceptors.response.use(
  (response) => {
    if (response.config.profileRequestTracked) endScopedRequest()
    return response
  },
  (error) => {
    explainRequestError(error)
    if (error.config?.profileRequestTracked) endScopedRequest()
    const message = error.response?.data?.message
    if (error.response?.status === 404 && typeof message === 'string' && message.startsWith('Profile not found:')) {
      // Use the server-resolved profile: an explicit path/query can override the
      // header, and an Agent editor can belong to a different space than the shell.
      if (message === `Profile not found: ${getActiveProfileId()}`) {
        void recoverFromMissingProfile().catch(() => undefined)
      }
    } else if (
      error.response?.status === 401
      && error.config?.authSessionToken
      && error.config?.authSessionToken === getAuthToken()
      && error.config?.authSessionGeneration === getAuthSessionGeneration()
    ) {
      // 仅携带当前会话 token 的请求才能判定登录失效。
      // 已退出或重新登录后的旧请求不能清除新会话。
      invalidateAuthSession()
      localStorage.removeItem('token')
      localStorage.removeItem('username')
      notify.error('登录已过期，请重新登录')
      router.push('/login')
    }
    return Promise.reject(error)
  }
)

// 订阅相关
export const subscriptionApi = {
  getAll: () => api.get('/subscriptions'),
  create: (data: any) => api.post('/subscriptions', data),
  update: (id: string, data: any) => api.put(`/subscriptions/${id}`, data),
  delete: (id: string) => api.delete(`/subscriptions/${id}`),
  fetch: (id: string, preview: boolean = false) => api.post(`/subscriptions/${id}/fetch`, { preview }),
  /** 最近 24 次拉取记录与 subscription-userinfo 流量信息 */
  health: () => api.get('/subscriptions/health')
}

export type NodeLatencyDnsMode = 'system' | 'custom' | 'managed'
export interface NodeLatencyDnsConfig {
  mode: NodeLatencyDnsMode
  servers: string[]
  bootstrap: string[]
  managed: { agent_id?: string; endpoint_id?: string }
}
export interface NodeLatencyManagedEndpoint {
  agent_id: string
  endpoint_id: string
  status: string
  endpoint?: string
  listen_address?: string
  port?: number
  upstream?: string
  message?: string
}
export interface NodeLatencyManagedUpstream {
  id: string
  label: string
  address: string
  bootstrap: string[]
  eligible: boolean
  reason: string
  validation: 'unverified'
}
export interface NodeLatencyManagedAgent {
  id: string
  name: string
  profile_id: string
  profile_name?: string
  address?: string
  eligible?: boolean
  reason?: string
  endpoints?: NodeLatencyManagedEndpoint[]
  upstreams?: NodeLatencyManagedUpstream[]
  upstreams_reason?: string
}
export type NodeLatencyManagedCreate = { agent_id: string; listen_address: string; port: number } & (
  { upstream_id: string; upstream?: never; bootstrap?: never } |
  { upstream: string; bootstrap?: string[]; upstream_id?: never }
)
export interface NodeLatencyDnsSettings {
  success: boolean
  dns_servers: string[]
  dns?: NodeLatencyDnsConfig
  managed_status?: { status: string; message?: string }
  message?: string
}
export interface NodeLatencyDnsCheck {
  success: boolean
  addresses: string[]
  error_code?: string
  message?: string
  latency_ms?: number
}

// 节点相关
export const nodeApi = {
  getAll: () => api.get('/nodes'),
  create: (data: any) => api.post('/nodes', data),
  update: (id: string, data: any) => api.put(`/nodes/${id}`, data),
  delete: (id: string) => api.delete(`/nodes/${id}`),
  /** 最近一次代理延迟结果（按节点来源与身份） */
  latency: () => api.get('/nodes/latency'),
  latencySettings: () => api.get<NodeLatencyDnsSettings>('/nodes/latency/settings'),
  saveLatencySettings: (dns: NodeLatencyDnsConfig | string[]) => api.post<NodeLatencyDnsSettings>('/nodes/latency/settings', Array.isArray(dns) ? { dns_servers: dns } : { dns }),
  checkLatencyDns: (dns: NodeLatencyDnsConfig) => api.post<NodeLatencyDnsCheck>('/nodes/latency/dns/check', { dns }, { timeout: 120000 }),
  latencyManagedDns: () => api.get<{ success: boolean; agents: NodeLatencyManagedAgent[]; default_port?: number; message?: string }>('/nodes/latency/dns/managed'),
  createLatencyManagedDns: (data: NodeLatencyManagedCreate) => api.post<NodeLatencyManagedEndpoint & { success: boolean }>('/nodes/latency/dns/managed', data),
  latencyManagedDnsStatus: (agentId: string, endpointId: string) => api.get<NodeLatencyManagedEndpoint & { success: boolean }>(`/nodes/latency/dns/managed/${encodeURIComponent(agentId)}/${encodeURIComponent(endpointId)}`),
  removeLatencyManagedDns: (agentId: string, endpointId: string) => api.delete<NodeLatencyManagedEndpoint & { success: boolean }>(`/nodes/latency/dns/managed/${encodeURIComponent(agentId)}/${encodeURIComponent(endpointId)}`),
  /** 通过独立组件测试代理延迟；每批最多 20 个节点 */
  testLatency: (keys: string[]) => api.post('/nodes/latency', { keys }, { timeout: 120000 }),
  /** 用内置 Sub-Store 把 Surge / Loon 节点行（如 snell）转成结构化节点，按行返回 */
  parseLines: (lines: string[]) =>
    api.post<{ success: boolean; results: Array<{ proxy?: Record<string, any>; error?: string }> }>(
      '/nodes/parse-lines', { lines }, { timeout: 120000 }
    )
}

// 规则相关
export const ruleApi = {
  getAll: (profileId?: string) => api.get('/rules', profileOptions(profileId)),
  create: (data: unknown, profileId?: string) => api.post('/rules', data, profileOptions(profileId)),
  update: (id: string, data: unknown, profileId?: string) => api.put(`/rules/${id}`, data, profileOptions(profileId)),
  delete: (id: string, profileId?: string) => api.delete(`/rules/${id}`, profileOptions(profileId)),
  batchCreate: (data: unknown, profileId?: string) => api.post('/rules/batch', data, profileOptions(profileId)),
  renameGroup: (ids: string[], groupName: string, profileId?: string) =>
    api.post('/rules/group-name', { ids, group_name: groupName }, profileOptions(profileId)),
  /** 一次改多条规则的策略；跟随策略的 DNS 随之变化，单独设置的 DNS 不变。传 groupName 时同时改组名 */
  setPolicy: (ids: string[], policy: string, options: { groupName?: string } = {}, profileId?: string) =>
    api.put<{ success: true; warnings?: string[]; pinned_withheld?: boolean }>('/rules/policy', {
      ids, policy, ...(options.groupName === undefined ? {} : { group_name: options.groupName }), base_url: getBaseUrl()
    }, profileOptions(profileId)),
  findDuplicates: (profileId?: string, signal?: AbortSignal) => api.post('/rules/find-duplicates', {}, { ...profileOptions(profileId), timeout: 120000, signal }),
  /** 一次写入规则、共享规则库条目与 OxiDNS DNS 处理；id 由服务端生成 */
  compose: (data: ComposePayload, profileId?: string) =>
    api.post<ComposeResult>('/rules/compose', { ...data, base_url: data.base_url ?? getBaseUrl() }, profileOptions(profileId)),
  /** 当前配置空间每条规则的 DNS 处理与可固定的上游 */
  getDns: (profileId?: string) =>
    api.get<RuleDnsSnapshot & { success: true }>('/rules/dns', { ...profileOptions(profileId), params: { base_url: getBaseUrl() } }),
  /** 列表快速切换与分组批量设置；一次最多 200 条 */
  setDns: (routes: Record<string, RuleDnsValue>, options: { skipUnsupported?: boolean } = {}, profileId?: string) =>
    api.put<{ success: true; rules: Record<string, RuleDnsState>; skipped: string[]; warnings: string[]; pinned_withheld?: boolean }>(
      '/rules/dns', { routes, skip_unsupported: options.skipUnsupported === true, base_url: getBaseUrl() }, profileOptions(profileId))
}

// 规则集相关
export const ruleSetApi = {
  getAll: (profileId?: string) => api.get('/rule-sets', profileOptions(profileId)),
  create: (data: unknown, profileId?: string) => api.post('/rule-sets', data, profileOptions(profileId)),
  update: (id: string, data: unknown, profileId?: string) => api.put(`/rule-sets/${id}`, data, profileOptions(profileId)),
  delete: (id: string, profileId?: string) => api.delete(`/rule-sets/${id}`, profileOptions(profileId))
}

// 策略组相关
export const proxyGroupApi = {
  getAll: (profileId?: string) => api.get('/proxy-groups', profileOptions(profileId)),
  create: (data: unknown, profileId?: string) => api.post('/proxy-groups', data, profileOptions(profileId)),
  update: (id: string, data: unknown, profileId?: string) => api.put(`/proxy-groups/${id}`, data, profileOptions(profileId)),
  batchHealthUrl: (data: { ids: string[]; url: string }, profileId?: string) => api.put('/proxy-groups/batch-health-url', data, profileOptions(profileId)),
  delete: (id: string, profileId?: string) => api.delete(`/proxy-groups/${id}`, profileOptions(profileId)),
  previewRegex: (data: unknown, profileId?: string) => api.post('/proxy-groups/preview-regex', data, profileOptions(profileId))
}

// 获取服务域名配置（优先使用配置的域名，否则使用当前页面的 base URL）
const getBaseUrl = () => {
  return localStorage.getItem('serverDomain') || window.location.origin
}

const profilePath = (profileId: string, path: string = '') =>
  `/profiles/${encodeURIComponent(profileId)}${path}`

export interface ProfileAgentBinding {
  id: string
  name: string
  host: string
  port?: number | null
  service_type: string
}

export interface ProfileRebindResult {
  from_profile: { id: string; name: string }
  to_profile: { id: string; name: string }
  agents: ProfileAgentBinding[]
}

export const profileApi = {
  list: () => api.get('/profiles'),
  get: (id: string) => api.get(profilePath(id)),
  create: (data: any) => api.post('/profiles', data),
  update: (id: string, data: any) => api.put(profilePath(id), data),
  delete: (id: string, newDefault?: string) =>
    api.delete(profilePath(id), newDefault ? { params: { new_default: newDefault } } : undefined),
  agentBindings: (id: string) => api.get<{ profile: { id: string; name: string }; agents: ProfileAgentBinding[] }>(profilePath(id, '/agents')),
  rebindAgents: (id: string, targetProfileId: string, agentIds: string[]) =>
    api.post<ProfileRebindResult>(profilePath(id, '/agents/rebind'), { target_profile_id: targetProfileId, agent_ids: agentIds }),
  clone: (id: string, data: any) => api.post(profilePath(id, '/clone'), data),
  setDefault: (id: string) => api.post<{ success: boolean; default_profile_id: string }>(profilePath(id, '/default')),
  export: (id: string) => api.get(profilePath(id, '/export'), { responseType: 'blob' }),
  import: (id: string, data: any) => api.post(profilePath(id, '/import'), data),
  importClient: (data: {
    client_type: string
    content: string
    name: string
    id?: string
    description?: string
    default_subscription_ids?: string[]
    dry_run?: boolean
  }) => api.post('/profiles/import-client', data),
}

// 配置生成
export const generateApi = {
  mihomo: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/mihomo'), { base_url: getBaseUrl() }, { responseType: 'blob' }),
  surge: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/surge'), { base_url: getBaseUrl() }, { responseType: 'blob' }),
  loon: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/loon'), { base_url: getBaseUrl() }, { responseType: 'blob' }),
  mosdns: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/mosdns'), { base_url: getBaseUrl() }, { responseType: 'blob', timeout: 60000 }),
  previewMihomo: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/mihomo/preview'), { base_url: getBaseUrl() }),
  previewSurge: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/surge/preview'), { base_url: getBaseUrl() }),
  previewLoon: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/loon/preview'), { base_url: getBaseUrl() }),
  oxidns: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/oxidns'), { base_url: getBaseUrl() }, { responseType: 'blob', timeout: 60000 }),
  previewOxidns: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/oxidns/preview'), { base_url: getBaseUrl() }),
  previewMosdns: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/mosdns/preview'), { base_url: getBaseUrl() })
}

export interface StaticRouteValues {
  cidr: string
  gateway: string
  enabled: boolean
  comment: string
}
export interface StaticRouteItem extends StaticRouteValues {
  id: string
  table: string
  editable: boolean
  reason: string
  managed: boolean
  revision: string
}
export interface StaticRouteChange {
  action: 'add' | 'update' | 'delete'
  id?: string
  revision?: string
  values?: StaticRouteValues
}
export interface StaticRoutePlan {
  action: StaticRouteChange['action']
  before: StaticRouteValues | null
  after: StaticRouteValues | null
  summary: string
  warnings: string[]
  fingerprint: string
  empty: boolean
}

// 上游路由器：接入管理、探测、差异报告与预览确认发布
export interface PolicyRoutingObject {
  id: string
  kind: 'addresses' | 'mangle' | 'tables' | 'routes' | 'nat'
  status: 'matched' | 'changed' | 'missing'
  fields: Record<string, string | number | boolean>
  editor?: 'source' | 'destination' | 'settings'
  configured_address?: string
}
export interface PolicyRoutingView {
  objects: PolicyRoutingObject[]
  source_agents: { id: string; name: string }[]
}
export interface PolicyWorkspace {
  profile_id: string; profile_name: string; revision: string; names_revision: string; oxidns_revision: string; config_revision: number
  router: { id: string; name: string; ip_rule_sets: string[]; ros_policy_rule_sets: string[]; proxy_gateway_agent_id: string }
  sources: { policies: import('@/types/oxidnsMonitor').OxiDNSDevicePolicy[]; editable: boolean; reason: string }
  source_agents: { id: string; name: string; host: string; status: string }[]
  gateways: { id: string; name: string; host: string }[]
  rule_sets: { id: string; name: string; policy: string; library_rule_id?: string }[]
}
export interface RuleContentDraft {
  success: boolean; revision: string; usage_count: number
  rule: { id: string; name: string; source_type: string; behavior: string; format?: string; content: string; url: string }
}
export const ruleContentApi = {
  read: (id: string) => api.get<RuleContentDraft>(`/rule-library/${encodeURIComponent(id)}/draft-content`),
  save: (id: string, revision: string, content: string) =>
    api.put<{ success: boolean; revision: string }>(`/rule-library/${encodeURIComponent(id)}/draft-content`, { revision, content })
}
export interface PolicyWorkspacePreview {
  revision: string; profile_id: string; config_revision: number; oxidns_revision: string
  targets: PolicyWorkspace['source_agents']; dns_content: string
  router_plan: { fingerprint: string; empty: boolean; blockers: string[]; warnings: string[];
    stages: { key: string; title: string; enabled?: boolean; note?: string; ops: import('@/lib/routerOperationDetails').RouterPlanOperation[] }[] }
}
export interface RouterRuleSyncSettings { auto_rule_sync: boolean; rule_sync_interval: number }
export interface RouterRuleSyncState {
  enabled: boolean; status: string; message: string; ready: boolean
  checked_at?: string | null; synced_at?: string | null; updated_at?: string | null
  added: number; removed: number; log: { status: string; summary: string }[]
  rules: { id: string; name: string; source_type: string; ip_count: number; domain_count: number;
    ignored_count: number; invalid_count: number; version: string; synced_version: string }[]
}
interface RouterRuleSyncResponse { success: boolean; settings: RouterRuleSyncSettings; sync: RouterRuleSyncState }

export const routerApi = {
  list: (profileId = getActiveProfileId()) => api.get('/routers', profileOptions(profileId)),
  create: (data: unknown, profileId = getActiveProfileId()) => api.post('/routers', data, profileOptions(profileId)),
  update: (id: string, data: unknown, profileId = getActiveProfileId()) => api.put(`/routers/${id}`, data, profileOptions(profileId)),
  remove: (id: string, profileId = getActiveProfileId()) => api.delete(`/routers/${id}`, profileOptions(profileId)),
  probe: (id: string, profileId = getActiveProfileId()) => api.post(`/routers/${id}/probe`, null, { ...profileOptions(profileId), timeout: 60000 }),
  report: (id: string, profileId = getActiveProfileId()) => api.get(`/routers/${id}/report`, { ...profileOptions(profileId), timeout: 90000 }),
  plan: (id: string, profileId = getActiveProfileId()) => api.post(`/routers/${id}/plan`, null, { ...profileOptions(profileId), timeout: 90000 }),
  apply: (id: string, fingerprint: string, profileId = getActiveProfileId()) =>
    api.post(`/routers/${id}/apply`, { fingerprint }, { ...profileOptions(profileId), timeout: 300000 }),
  routes: (id: string, profileId = getActiveProfileId()) =>
    api.get(`/routers/${id}/routes`, { ...profileOptions(profileId), timeout: 60000 }),
  policy: (id: string, profileId = getActiveProfileId()) =>
    api.get(`/routers/${id}/policy`, { ...profileOptions(profileId), timeout: 60000 }),
  ruleSync: (id: string, profileId: string) => api.get<RouterRuleSyncResponse>(`/routers/${id}/rule-sync`, profileOptions(profileId)),
  saveRuleSync: (id: string, settings: RouterRuleSyncSettings, profileId: string) =>
    api.put<RouterRuleSyncResponse>(`/routers/${id}/rule-sync`, settings, profileOptions(profileId)),
  startRuleSync: (id: string, profileId: string) =>
    api.post<RouterRuleSyncResponse>(`/routers/${id}/rule-sync`, null, profileOptions(profileId)),
  policySettings: (id: string, profileId: string) =>
    api.get<{ success: boolean; message?: string; workspace: PolicyWorkspace }>(`/routers/${id}/policy-settings`, profileOptions(profileId)),
  savePolicySettings: (id: string, draft: { revision: string; router: Pick<PolicyWorkspace['router'], 'ip_rule_sets' | 'ros_policy_rule_sets' | 'proxy_gateway_agent_id'> }, profileId: string) =>
    api.put<{ success: boolean; message?: string; workspace: PolicyWorkspace }>(`/routers/${id}/policy-settings`, { ...draft, base_url: getBaseUrl() }, profileOptions(profileId)),
  previewPolicySettings: (id: string, revision: string, targets: string[], profileId: string) =>
    api.post<{ success: boolean; message?: string; preview: PolicyWorkspacePreview }>(`/routers/${id}/policy-settings/preview`, { revision, targets, base_url: getBaseUrl() }, { ...profileOptions(profileId), timeout: 90000 }),
  routePlan: (id: string, change: StaticRouteChange, profileId = getActiveProfileId()) =>
    api.post(`/routers/${id}/routes/plan`, change, { ...profileOptions(profileId), timeout: 60000 }),
  routeApply: (id: string, change: StaticRouteChange & { fingerprint: string }, profileId = getActiveProfileId()) =>
    api.post(`/routers/${id}/routes/apply`, change, { ...profileOptions(profileId), timeout: 90000 }),
  /** 本配置空间各路由器的代理网关转发状态；上游路由页只用它提示异常，开启入口在 Agent 页。 */
  gatewayForwarding: (profileId = getActiveProfileId()) =>
    api.get<{ success: boolean; gateways: GatewayForwardingEntry[] }>('/routers/gateway-forwarding', { ...profileOptions(profileId), timeout: 60000 })
}

// OxiDNS 统一配置
export const oxidnsApi = {
  getSettings: (profileId?: string) => api.get('/oxidns/settings', profileOptions(profileId)),
  /** ifMatch 为读取时的 ETag；设置已在其他页面修改时返回 409 oxidns_changed_elsewhere */
  saveSettings: (settings: unknown, profileId?: string, ifMatch?: string) => api.post('/oxidns/settings', settings,
    ifMatch ? { headers: { ...profileOptions(profileId).headers, 'If-Match': ifMatch } } : profileOptions(profileId)),
  validateSettings: (settings: unknown, profileId?: string) => api.post('/oxidns/validate', { settings, base_url: getBaseUrl() }, profileOptions(profileId)),
  render: (settings: unknown, profileId?: string) => api.post('/oxidns/render', { settings, base_url: getBaseUrl() }, profileOptions(profileId)),
  prepareEditor: (settings: unknown, profileId?: string) => api.post('/oxidns/editor', { settings, base_url: getBaseUrl() }, profileOptions(profileId)),
  readCommonSettings: (settings: unknown, profileId?: string) => api.post('/oxidns/common-settings', { settings, base_url: getBaseUrl() }, profileOptions(profileId)),
  applyCommonSettings: (settings: unknown, changes: Record<string, unknown>, profileId?: string) => api.post('/oxidns/common-settings/apply', { settings, changes, base_url: getBaseUrl() }, profileOptions(profileId)),
  applyNativeSettings: (settings: unknown, native_config: string, profileId?: string, renames?: Record<string, string>, standard_add?: string[]) => api.post('/oxidns/native-settings/apply', { settings, native_config, renames, standard_add, base_url: getBaseUrl() }, profileOptions(profileId))
}

// 配置导入导出
export const configApi = {
  export: () => api.get('/config/export', { responseType: 'blob' }),
  exportDesensitized: () => api.get('/config/export?desensitize=true', { responseType: 'blob' }),
  import: (data: any) => api.post('/config/import', data)
}

// 自定义配置
export const customConfigApi = {
  getMihomo: (profileId?: string) => api.get('/custom-config/mihomo', profileOptions(profileId)),
  saveMihomo: (data: unknown, profileId?: string) => api.post('/custom-config/mihomo', data, profileOptions(profileId)),
  getSurge: (profileId?: string) => api.get('/custom-config/surge', profileOptions(profileId)),
  saveSurge: (data: unknown, profileId?: string) => api.post('/custom-config/surge', data, profileOptions(profileId)),
  getLoon: (profileId?: string) => api.get('/custom-config/loon', profileOptions(profileId)),
  saveLoon: (data: unknown, profileId?: string) => api.post('/custom-config/loon', data, profileOptions(profileId)),
  getMosdns: (profileId?: string) => api.get('/custom-config/mosdns', profileOptions(profileId)),
  saveMosdns: (data: unknown, profileId?: string) => api.post('/custom-config/mosdns', data, profileOptions(profileId))
}

// Agent 管理
export interface PublishGuard {
  expected_profile_id: string
  expected_service_type: string
  expected_config_revision: number
}

export interface AgentInstallScriptParams {
  name: string; type: string; port?: number; agent_ip?: string; config_path?: string;
  restart_command?: string; server_url?: string; service_manager?: string;
  service_unit?: string; service_binary?: string; stop_command?: string;
  start_command?: string; status_command?: string; install_core?: boolean;
  /** 仅原生 OxiDNS 单服务：自动准备 keepalived 与高可用工具。 */
  prepare_dns_ha?: boolean;
  mihomo_agent_port?: number; oxidns_agent_port?: number;
  mihomo_config_path?: string; oxidns_config_path?: string;
  mihomo_service_unit?: string; oxidns_service_unit?: string;
  mihomo_service_binary?: string; oxidns_service_binary?: string;
  /** 仅 Mihomo 单独安装：省略时默认开启并持久化 IPv4 转发，false 不开启。 */
  ipv4_forwarding?: boolean;
}

export interface InstallCommandResult {
  script: string
  download_path: string
  expires_at: number
}

// Agent 按本机服务配置解析出的日志来源；auto 表示旧版 Agent 的自动检测
export interface AgentLogSource {
  id: string
  label: string
  kind: 'file' | 'journal' | 'unavailable' | 'auto'
  path?: string
  unit?: string
  clearable: boolean
  note?: string
}

export interface AgentLogTarget {
  source?: string
  logPath?: string
}

export const agentApi = {
  getAll: () => api.get('/agents'),
  /** Without content sync status: much cheaper, for pickers and polling. */
  summaries: () => api.get('/agents', { params: { summary: 1 } }),
  create: (data: any) => api.post('/agents', data),
  update: (id: string, data: any = {}) => api.post(`/agents/${id}/update`, data),
  getUpgrade: (id: string) => api.get(`/agents/${id}/upgrade`),
  getDNSFollow: (id: string) => api.get<import('@/types/agentDNSFollow').AgentDNSFollowOptions>(`/agents/${encodeURIComponent(id)}/dns-follow`),
  setDNSFollow: (id: string, data: import('@/types/agentDNSFollow').AgentDNSFollowInput) => api.put(`/agents/${encodeURIComponent(id)}/dns-follow`, data),
  removeDNSFollow: (id: string) => api.delete(`/agents/${encodeURIComponent(id)}/dns-follow`),
  retryDNSFollow: (id: string) => api.post(`/agents/${encodeURIComponent(id)}/dns-follow/retry`, {}),
  /** DNS 高可用：开启先异步同步主备配置，再应用 keepalived；通过 publication 轮询整组进度。 */
  getDNSHA: (id: string) => api.get<import('@/types/agentDNSHA').AgentDNSHAStatus>(`/agents/${encodeURIComponent(id)}/oxidns-ha`, { timeout: 60000 }),
  setDNSHA: (id: string, data: import('@/types/agentDNSHA').AgentDNSHAInput) => api.put<import('@/types/agentDNSHA').AgentDNSHAStatus>(`/agents/${encodeURIComponent(id)}/oxidns-ha`, data, { timeout: 180000 }),
  removeDNSHA: (id: string, force = false) => api.delete<import('@/types/agentDNSHA').AgentDNSHARemoveResult>(`/agents/${encodeURIComponent(id)}/oxidns-ha`, { timeout: 180000, ...(force ? { params: { force: 1 } } : {}) }),
  bindProfile: (id: string, profileId: string) => api.put(`/agents/${id}`, { profile_id: profileId }),
  /** 所有配置空间中被路由器用作代理网关的 Agent 的 IPv4 转发状态 */
  gatewayForwarding: () =>
    api.get<{ success: boolean; gateways: GatewayForwardingEntry[] }>('/agents/gateway-forwarding', { timeout: 60000 }),
  persistGatewayForwarding: (agentId: string) =>
    api.post<{ success: boolean; message?: string; gateway: GatewayForwardingEntry }>('/agents/gateway-forwarding', { agent_id: agentId }, { timeout: 90000 }),
  /** 设备 IPv6 检查：依赖未变时服务端返回上次结果；refresh 用于「重新检查」与发布成功之后。 */
  ipv6Readiness: (id: string, profileId: string, options: { refresh?: boolean } = {}) =>
    api.get<import('@/types/mihomoIPv6').MihomoIPv6Readiness>(`/agents/${encodeURIComponent(id)}/ipv6-readiness`, { params: { profile_id: profileId, ...(options.refresh ? { refresh: 1 } : {}) }, ...profileOptions(profileId), timeout: 60000 }),
  prepareIPv6: (id: string, data: import('@/types/mihomoIPv6').IPv6PrepareInput) =>
    api.post<import('@/types/mihomoIPv6').MihomoIPv6Readiness>(`/agents/${encodeURIComponent(id)}/ipv6-readiness/prepare`, data, { ...profileOptions(data.profile_id), timeout: 90000 }),
  probeIPv6: (id: string, profileId: string) =>
    api.post<import('@/types/mihomoIPv6').IPv6ProbeResult>(`/agents/${encodeURIComponent(id)}/ipv6-readiness/probe`, { profile_id: profileId }, { ...profileOptions(profileId), timeout: 60000 }),
  verifyIPv6: (id: string, data: import('@/types/mihomoIPv6').IPv6VerificationInput) =>
    api.post<import('@/types/mihomoIPv6').IPv6VerificationResult>(`/agents/${encodeURIComponent(id)}/ipv6-readiness/verify`, data, { ...profileOptions(data.profile_id), timeout: 60000 }),
  getIPv6Verification: (id: string, profileId: string, deploymentId?: string) =>
    api.get<import('@/types/mihomoIPv6').IPv6VerificationResult>(`/agents/${encodeURIComponent(id)}/ipv6-readiness/verify`, { params: { profile_id: profileId, ...(deploymentId ? { deployment_id: deploymentId } : {}) }, ...profileOptions(profileId), timeout: 60000 }),
  getIPv6VerificationSettings: (id: string, profileId: string) =>
    api.get<import('@/types/mihomoIPv6').IPv6VerificationSettingsResult>(`/agents/${encodeURIComponent(id)}/ipv6-readiness/verification-settings`, { params: { profile_id: profileId }, ...profileOptions(profileId), timeout: 60000 }),
  setIPv6VerificationSettings: (id: string, profileId: string, settings: import('@/types/mihomoIPv6').IPv6VerificationSettings) =>
    api.put<import('@/types/mihomoIPv6').IPv6VerificationSettingsResult>(`/agents/${encodeURIComponent(id)}/ipv6-readiness/verification-settings`, { profile_id: profileId, ...settings }, { ...profileOptions(profileId), timeout: 60000 }),
  delete: (id: string) => api.delete(`/agents/${id}`),
  restart: (id: string) => api.post(`/agents/${id}/restart`),
  getStatus: (id: string) => api.get(`/agents/${id}/status`),
  getLogSources: (id: string, options: { signal?: AbortSignal } = {}) => api.get(`/agents/${encodeURIComponent(id)}/logs/sources`, options),
  getLogs: (id: string, lines: number = 100, target: AgentLogTarget = {}, options: { signal?: AbortSignal } = {}) => {
    const params = new URLSearchParams({ lines: lines.toString() })
    if (target.source) {
      params.append('source', target.source)
    } else if (target.logPath) {
      params.append('log_path', target.logPath)
    }
    return api.get(`/agents/${encodeURIComponent(id)}/logs?${params.toString()}`, options)
  },
  clearLog: (id: string, target: AgentLogTarget) => api.post(`/agents/${id}/logs/clear`, target.source ? { source: target.source } : { log_path: target.logPath }),
  pushConfig: (id: string, deploymentId?: string, expected?: PublishGuard | { expected_profile_id: string; expected_oxidns_revision: string; expected_config_revision?: number }) => api.post(`/agents/${id}/push-config`, {
    base_url: getBaseUrl(), deployment_id: deploymentId, ...expected
  }, { timeout: 120000 }),
  getDeployment: (id: string, deploymentId: string) => api.get(`/agents/${id}/deployments/${deploymentId}`),
  activateDeployment: (id: string, deploymentId: string) => api.post(`/agents/${id}/deployments/${deploymentId}/activate`),
  rollbackDeployment: (id: string, deploymentId: string) => api.post(`/agents/${id}/deployments/${deploymentId}/rollback`, {}),
  uninstall: (id: string) => api.post(`/agents/${id}/uninstall`),
  generateInstallCommand: (params: AgentInstallScriptParams) =>
    api.post<InstallCommandResult>('/agents/install-command', params),
  generateDockerHAGuide: (params: import('@/types/dockerHAGuide').DockerHAGuideRequest, profileId?: string) =>
    api.post<import('@/types/dockerHAGuide').DockerHAGuideResult>('/agents/docker-oxidns-ha-guide', params, profileOptions(profileId)),
  getDockerHAEnvironment: (id: string) =>
    api.get<import('@/types/dockerHAGuide').DockerHAEnvironment>(`/agents/${encodeURIComponent(id)}/oxidns-ha-environment`, { timeout: 60000 }),
  generateCoreInstallCommand: (target: 'both' | 'mihomo' | 'oxidns') =>
    api.post<InstallCommandResult>('/agents/core-install-command', { target }),
  generateDockerCompose: (params: any) =>
    api.get('/agents/docker-compose', { params, responseType: 'text' }),
  generateDockerRun: (params: any) =>
    api.get('/agents/docker-run', { params, responseType: 'text' })
}

export const clientToolsApi = {
  notes: (id: string) => api.get(`/agents/${encodeURIComponent(id)}/client-notes`),
  saveNote: (id: string, data: { profile_id: string; ip: string; name: string; previous_name: string }) =>
    api.put(`/agents/${encodeURIComponent(id)}/client-notes`, data),
  ruleTargets: (id: string) => api.get(`/agents/${encodeURIComponent(id)}/mihomo/rule-targets`),
  addRule: (id: string, data: { profile_id: string; ruleset_id: string; target: 'direct' | 'proxy'; value: string; rule_type: import('@/lib/device-notes').ConnectionRuleType }) =>
    api.post(`/agents/${encodeURIComponent(id)}/mihomo/rule-targets/apply`, data)
}

export type ClientDeviceMode = '' | 'smart' | 'direct'
export interface ClientFixedIPRecord { router_id: string; mac: string; id: string; comment: string; vendor: string }
export interface ClientDevice { ip: string; name: string; mode: ClientDeviceMode; fixed_ip?: ClientFixedIPRecord }
export interface ClientFixedIPStatus {
  eligible: boolean; reason: string; status: 'dynamic' | 'static' | 'none'; owned: boolean; router_id: string
  binding?: ClientFixedIPRecord
}
export interface ClientFixedIPTarget { ip: string; mac: string; router_id: string; name: string }
export interface ClientFixedIPPreview {
  token: string
  actions: { ip: string; router_id: string; router_name: string; router_type: string; description: string }[]
}
export interface ClientDeviceAgent {
  oxidns_ha?: import('@/types/agentDNSHA').AgentDNSHAConfig | null
  id: string; name: string; host: string; status: string
  deployment: import('@/composables/useAgentDeployments').AgentDeployment | null
}
export interface ClientDevicesState {
  success: true; profile_id: string; profile_name: string; oxidns_revision: string
  devices: ClientDevice[]; editable: boolean; reason: string; agents: ClientDeviceAgent[]
}
export type ClientPresence = 'online' | 'recent' | 'offline' | 'unknown' | 'network'
export type ClientPresenceBasis = '' | 'arp' | 'lease' | 'probe' | 'terminal' | 'stale' | 'absent' | 'router' | 'outside'
export type ClientSighting = 'arp' | 'lease' | 'online'
/** 路由器对一个地址的判断；last_seen 是距上次 DHCP 通信的秒数 */
export interface ClientPresenceInfo {
  ip: string; mac: string; hostname: string; sources: ClientSighting[]
  presence: ClientPresence; basis: ClientPresenceBasis; last_seen: number | null; router_id: string
  fixed_ip?: ClientFixedIPStatus
}
export interface DiscoveredClient extends ClientPresenceInfo { network: ClientDevice | null }
export interface ClientDiscoverySource {
  id: string; name: string; type: 'routeros' | 'ikuai'; ok: boolean; error: string; notes: string[]
  /** 仅 RouterOS：已启用按来源策略路由；gateway 是不作为来源的代理网关地址 */
  policy_routing?: boolean; gateway?: string
}
/** 已保存的行；discoverable 表示移除后会出现在「未添加」 */
export type DiscoveredSavedClient = Omit<ClientDevice, 'fixed_ip'> & ClientPresenceInfo & { discoverable: boolean }
export interface ClientDiscovery {
  success: true; profile_id: string; state: 'ok' | 'partial' | 'failed' | 'unsupported'; message: string; checked_at: string
  sources: ClientDiscoverySource[]; devices: DiscoveredSavedClient[]; discovered: DiscoveredClient[]
  cached: boolean; stale: boolean; refresh_error: string
}
/** 当前配置空间的设备名称与 OxiDNS DNS 策略；从 Agent 进入时按设备绑定的空间读取 */
export const clientDeviceApi = {
  /** 优先读取路由器快照；stale 时后台 refresh，手动刷新也传 refresh */
  discover: (profileId: string, options: { signal?: AbortSignal; refresh?: boolean } = {}) =>
    api.get<ClientDiscovery>('/client-devices/discovery', { params: { profile: profileId, refresh: options.refresh ? '1' : undefined }, timeout: 45000, signal: options.signal }),
  get: (options: { agentId?: string; profileId?: string; signal?: AbortSignal } = {}) =>
    api.get<ClientDevicesState>('/client-devices', {
      ...(options.profileId ? profileOptions(options.profileId) : {}),
      params: options.agentId ? { agent: options.agentId } : undefined, signal: options.signal
    }),
  /** 只保存一行；previous 是编辑前看到的值，已被其他页面修改时返回 409 */
  save: (profileId: string, device: ClientDevice, previous: Omit<ClientDevice, 'ip'>) =>
    api.put<ClientDevicesState>('/client-devices', {
      profile_id: profileId, ip: device.ip, name: device.name, mode: device.mode,
      previous_name: previous.name, previous_mode: previous.mode, base_url: getBaseUrl()
    }, profileOptions(profileId)),
  previewFixedIP: (profileId: string, devices: ClientFixedIPTarget[], operation: 'add' | 'delete' = 'add') =>
    api.post<ClientFixedIPPreview>('/client-devices/fixed-ip/preview', { profile_id: profileId, devices, operation }, { ...profileOptions(profileId), timeout: 45000 }),
  applyFixedIP: (profileId: string, token: string) =>
    api.post<ClientDevicesState>('/client-devices/fixed-ip/apply', { profile_id: profileId, token }, { ...profileOptions(profileId), timeout: 90000 })
}

// OxiDNS monitoring never sends core credentials or addresses from the browser.
export const oxidnsMonitorApi = {
  clients: (id: string, params: Record<string, string | number | undefined>, options: { signal?: AbortSignal } = {}) =>
    api.get(`/agents/${encodeURIComponent(id)}/oxidns/clients`, { params, ...options }),
  overview: (id: string, options: { signal?: AbortSignal; recorder?: string } = {}) => {
    const { recorder, ...requestOptions } = options
    return api.get(`/agents/${encodeURIComponent(id)}/oxidns/overview`, { ...requestOptions, params: recorder ? { recorder } : undefined })
  },
  resetStatistics: (id: string, recorder?: string, options: { signal?: AbortSignal } = {}) =>
    api.post(`/agents/${encodeURIComponent(id)}/oxidns/dashboard/reset`, recorder ? { recorder } : {}, options),
  queries: (id: string, params: Record<string, string | number | undefined>, options: { signal?: AbortSignal } = {}) =>
    api.get(`/agents/${encodeURIComponent(id)}/oxidns/queries`, { params, ...options }),
  query: (id: string, recordId: string | number, recorder: string, options: { signal?: AbortSignal } = {}) =>
    api.get(`/agents/${encodeURIComponent(id)}/oxidns/queries/${encodeURIComponent(recordId)}`, { params: { recorder }, ...options })
}

// Mihomo 面板：经 Agent 访问本机 Mihomo 控制接口，浏览器不接触控制接口地址与 secret
const mihomoPath = (id: string, resource: string) => `/agents/${encodeURIComponent(id)}/mihomo/${resource}`
const mihomoName = (name: string) => ({ params: { name } })
export const mihomoPanelApi = {
  version: (id: string) => api.get(mihomoPath(id, 'version')),
  configs: (id: string) => api.get(mihomoPath(id, 'configs')),
  setMode: (id: string, mode: 'rule' | 'global' | 'direct') => api.patch(mihomoPath(id, 'configs'), { mode }),
  proxies: (id: string) => api.get(mihomoPath(id, 'proxies')),
  selectProxy: (id: string, group: string, name: string) => api.put(mihomoPath(id, 'proxy'), { name }, mihomoName(group)),
  /** 取消自动策略组（url-test / fallback）的手动固定 */
  unfixProxy: (id: string, group: string) => api.delete(mihomoPath(id, 'proxy'), mihomoName(group)),
  proxyDelay: (id: string, name: string, url?: string, timeout = 5000) =>
    api.get(mihomoPath(id, 'proxy/delay'), { params: { name, url, timeout }, timeout: 45000 }),
  /** proxy-providers 内单个节点测速（Agent 1.8.18-go+） */
  providerProxyDelay: (id: string, provider: string, name: string, url?: string, timeout = 5000) =>
    api.get(mihomoPath(id, 'providers/proxy/delay'), { params: { provider, name, url, timeout }, timeout: 45000 }),
  groupDelay: (id: string, name: string, url?: string, timeout = 5000) =>
    api.get(mihomoPath(id, 'group/delay'), { params: { name, url, timeout }, timeout: 45000 }),
  connections: (id: string, options: { signal?: AbortSignal } = {}) => api.get(mihomoPath(id, 'connections'), options),
  /** Agent 端聚合的实时流向（Agent 1.8.26-go+） */
  flow: (id: string, options: { signal?: AbortSignal } = {}) => api.get(mihomoPath(id, 'flow'), options),
  closeConnection: (id: string, connectionId: string) => api.delete(mihomoPath(id, 'connection'), mihomoName(connectionId)),
  closeAllConnections: (id: string) => api.delete(mihomoPath(id, 'connections')),
  rules: (id: string) => api.get(mihomoPath(id, 'rules')),
  proxyProviders: (id: string) => api.get(mihomoPath(id, 'providers/proxies')),
  updateProxyProvider: (id: string, name: string) => api.put(mihomoPath(id, 'providers/proxy'), {}, { ...mihomoName(name), timeout: 60000 }),
  healthcheckProxyProvider: (id: string, name: string) =>
    api.get(mihomoPath(id, 'providers/proxy/healthcheck'), { ...mihomoName(name), timeout: 60000 }),
  ruleProviders: (id: string) => api.get(mihomoPath(id, 'providers/rules')),
  updateRuleProvider: (id: string, name: string) => api.put(mihomoPath(id, 'providers/rule'), {}, { ...mihomoName(name), timeout: 60000 }),
  /** 从内核当前使用的配置文件重载（Agent 1.8.16-go+） */
  reloadConfig: (id: string) => api.put(mihomoPath(id, 'configs/reload'), {}, { timeout: 80000 }),
  /** 内核自更新并重启（Agent 1.8.16-go+） */
  upgradeCore: (id: string) => api.post(mihomoPath(id, 'upgrade'), {}, { timeout: 210000 }),
  flushFakeIP: (id: string) => api.post(mihomoPath(id, 'cache/fakeip/flush')),
  flushDNS: (id: string) => api.post(mihomoPath(id, 'cache/dns/flush'))
}

/** Mihomo 流量记录（Agent 1.8.51-go+）：开关、按范围读取、清空与保留天数 */
export const trafficHistoryApi = {
  get: (id: string, range: string, by: string, options: { signal?: AbortSignal } = {}) =>
    api.get(`/agents/${encodeURIComponent(id)}/traffic-history`, { params: { range, by }, ...options }),
  setEnabled: (id: string, enabled: boolean, clear = false) =>
    api.put(`/agents/${encodeURIComponent(id)}/traffic-history`, { enabled, clear }),
  clear: (id: string) => api.delete(`/agents/${encodeURIComponent(id)}/traffic-history`),
  setRetention: (days: number) => api.put('/agents/traffic-history/retention', { days })
}

// 系统信息
export const systemApi = {
  getVersion: () => api.get('/version')
}

// 服务域名管理
export const serverDomainApi = {
  get: () => api.get('/server-domain'),
  update: (data: { new_domain: string }) => api.post('/server-domain', data)
}

export interface WebPortStatus {
  supported: boolean
  port: number | null
  editable: boolean
  message?: string
  ipv6: boolean
  source: 'default' | 'saved' | 'environment'
}

export const webPortApi = {
  get: () => api.get<WebPortStatus>('/settings/web-port'),
  update: (data: { port: number }) => api.post<WebPortStatus & { success: boolean }>('/settings/web-port', data),
}

// 配置令牌管理
export const configTokenApi = {
  get: () => api.get('/config-token'),
  update: (data: { token?: string, generate?: boolean }) => api.post('/config-token', data),
  delete: () => api.delete('/config-token')
}

// 第三方依赖（内置 Sub-Store 等）检测与在线更新
export interface DependencyUpdateState {
  state: 'idle' | 'running' | 'success' | 'failed'
  message: string
  target_version: string
  finished_at: number
}

export interface DependencyLifecycle {
  managed: boolean
  mode: 'on_demand' | 'always' | 'external'
  state: 'sleeping' | 'starting' | 'running' | 'updating' | 'failed'
  idle_seconds: number
  error: string
}

export interface DependencyStatus {
  key: string
  name: string
  description: string
  homepage: string
  running: boolean
  lifecycle?: DependencyLifecycle
  current_version: string
  builtin_version: string
  online_updated: boolean
  runtime: string
  latest_version: string
  latest_published_at: string
  release_url: string
  release_notes: string
  check_error: string
  has_update: boolean
  updatable: boolean
  update: DependencyUpdateState
}

export const dependenciesApi = {
  list: (refresh = false) =>
    api.get<{ dependencies: DependencyStatus[] }>('/settings/dependencies', { params: refresh ? { refresh: 1 } : {} }),
  update: (key: string) =>
    api.post<{ success: boolean; message: string }>(`/settings/dependencies/${key}/update`),
  setSubStoreMode: (mode: 'on_demand' | 'always') =>
    api.post<{ success: boolean; mode: 'on_demand' | 'always' }>('/settings/dependencies/sub-store/mode', { mode })
}

// 规则下载 HTTP 代理（与 GitHub 镜像域名前缀独立）
export const ruleFetchProxyApi = {
  get: () => api.get<{ rule_fetch_proxy: string }>('/settings/rule-fetch-proxy'),
  update: (data: { rule_fetch_proxy: string }) =>
    api.post<{ success: boolean; rule_fetch_proxy: string }>('/settings/rule-fetch-proxy', data)
}

// GitHub 代理域名：原地址下载失败后改用的镜像前缀
export const githubProxyDomainApi = {
  get: () => api.get<{ proxy_domains: string }>('/rule-library/proxy-domains'),
  update: (proxyDomain: string) =>
    api.post<{ success: boolean; proxy_domains: string }>('/rule-library/proxy-domains', { proxy_domains: proxyDomain })
}

// 系统设置「定时任务」：订阅、规则集与各后台任务的最近结果
export const tasksApi = {
  list: () => api.get<{ success: boolean; tasks: import('@/lib/scheduledTasks').ScheduledTask[] }>('/tasks'),
  run: (key: string) => api.post<{ success: boolean; message: string }>('/tasks/run', { key }),
  setInterval: (key: string, intervalSeconds: number) =>
    api.put<{ success: boolean; task: import('@/lib/scheduledTasks').ScheduledTask | null }>('/tasks/interval', { key, interval_seconds: intervalSeconds })
}

// 统计数据
export const statsApi = {
  getOverview: () => api.get('/stats/overview')
}

export default api
