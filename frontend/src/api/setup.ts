import api from '@/api'

export type SetupGoal = 'router' | 'gateway'
export interface SetupDomainSelection { direct: string[]; proxy: string[] }
export interface SetupDomainRule {
  id: string; name: string; source: 'builtin' | 'library'; behavior?: 'domain' | 'classical'; aliases?: string[]
  /** 规则库里同名但内容不同的规则集；选择内置项会另建一份。id 仅在可改用时提供 */
  conflict?: { name: string; id?: string }
}
export interface SetupRouterDraft {
  id: string; name: string; type: 'ikuai' | 'routeros'; endpoint: string; username: string; verify_tls: boolean
  proxy_gateway_agent_id: string; dns_agent_id: string; has_password: boolean
  manage_return_routing?: boolean; ip_rule_sets?: string[]; ros_policy_rule_sets?: string[]
}
export interface SetupDefaultProfile {
  is_default: boolean; suggested: boolean; current: { id: string; name: string }
  leftovers?: { profile: { id: string; name: string } | null; subscriptions: { id: string; name: string }[] }
}
export interface SetupSession {
  id: string; profile_id: string; name: string; goal: SetupGoal
  step: number; max_step: number; prepared: boolean; completed: boolean; abandoned?: boolean
  /** 主路由分流仍使用旧版 Linux 网关接管（升级前准备的方案） */
  legacy_takeover?: boolean
  /** 最近一次生成方案时对 Mihomo 基础配置所做的调整；其余设置原样保留 */
  mihomo_changes?: string[]
  /** 基础配置已有用户自己的控制面板密码（只返回是否已设置，不返回密码） */
  controller_secret_saved?: boolean
  /** 基础配置的 external-controller 监听地址 */
  controller_address?: string
  subscription_ids: string[]; node_ids: string[]; dns_fallback?: 'mihomo' | 'direct'; traffic_fallback?: 'proxy' | 'direct'
  local_dns?: string; query_recorder?: boolean
  domain_rules?: SetupDomainSelection
  node_count?: number; config_revision?: number; updated_at: string
  installation?: { id: string; mihomo: { id: string; name: string; host: string }; oxidns: { id: string; name: string; host: string } }
  router_id?: string; router_mode?: 'manual' | 'managed'; router_verification?: 'manual' | 'verified' | 'client_pending'
  router_draft?: SetupRouterDraft
  default_profile?: SetupDefaultProfile; previous_default?: string
  route_instructions?: { gateway: string; dns: string; routes: { cidr: string; sources: string[]; requires_source_exclusion?: boolean }[]; warnings: string[]; incomplete?: boolean; missing_rule_sets?: string[] }
}
export interface SetupAbandonPlan {
  session_id: string; name: string; profile: { id: string; name: string }
  delete_profile: boolean; kept_reason: string | null; guidance: string
  subscription: { id: string; name: string } | null; subscription_kept?: boolean
}
export interface SetupIndex {
  sessions: SetupSession[]; suggested: boolean
  subscriptions: { id: string; name: string }[]; nodes: { id: string; name: string }[]
  domain_rules?: SetupDomainRule[]
  recommended_domain_rules?: SetupDomainSelection
}
const path = (id: string) => `/setup/${encodeURIComponent(id)}`
export const setupApi = {
  index: () => api.get<SetupIndex>('/setup'),
  create: (data: { id: string; name: string; goal: SetupGoal }) => api.post<SetupSession>('/setup', data),
  get: (id: string) => api.get<SetupSession>(path(id)),
  progress: (id: string, data: { step?: number; router_id?: string; router_mode?: 'manual' | 'managed' }) => api.patch<SetupSession>(path(id), data),
  source: (id: string, data: { url?: string; subscription_ids?: string[]; node_ids?: string[] }) => api.post<SetupSession>(`${path(id)}/source`, data),
  prepare: (id: string, settings: { dns_fallback: 'mihomo' | 'direct'; traffic_fallback: 'proxy' | 'direct'; local_dns: string; domain_rules: SetupDomainSelection; query_recorder: boolean; controller_secret?: string }) => api.post<SetupSession>(`${path(id)}/prepare`, settings, { timeout: 90000 }),
  check: (id: string) => api.post<SetupSession>(`${path(id)}/check`, {}, { timeout: 90000 }),
  saveRouterDraft: (id: string, router: unknown) => api.post<SetupSession>(`${path(id)}/router-draft`, { router }),
  abandon: (id: string, dryRun = false) => api.post<SetupAbandonPlan>(`${path(id)}/abandon`, dryRun ? { dry_run: true } : {}),
  finish: (id: string, data: { router_mode?: 'manual' | 'managed'; router_id?: string; ack_routes?: boolean; ack_dns?: boolean; make_default?: boolean } = {}) => api.post<SetupSession>(`${path(id)}/finish`, data, { timeout: 90000 })
}
