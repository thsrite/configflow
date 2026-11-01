import api from '@/api'

/** 单台上游路由相对当前设置的状态：同步、待发布、有改动但暂时无法发布、读取失败。 */
export type UpstreamRouterState = 'synced' | 'pending' | 'blocked' | 'unknown'

export interface UpstreamRouterPendingItem {
  /** 发布阶段，如 routes、ipv6_fakeip、dhcp_dns */
  key: string
  /** 与「预览发布」相同的阶段名，如「IPv6 Fake-IP 分流路由」 */
  label: string
  /** 待添加 / 待更新 / 待移除 / 待撤回 / 待关闭 / 冲突 / 待确认 / 本次跳过 */
  status: string
  /** 该阶段预览中的改动数；本次跳过为 0 */
  count: number
}

export interface UpstreamRouterStatus {
  id: string
  name: string
  type: 'routeros' | 'ikuai' | string
  profile_id: string
  profile_name: string
  state: UpstreamRouterState
  /** 预览发布中已启用阶段的改动数 */
  pending: number
  items: UpstreamRouterPendingItem[]
  /** blocked / unknown 的原因 */
  message: string
}

export interface UpstreamRouterPendingResponse {
  success: boolean
  agent_id: string
  /** Agent 绑定的配置空间；路由器在其他空间时界面注明空间名 */
  agent_profile_id: string
  checked_at: string
  cached: boolean
  routers: UpstreamRouterStatus[]
  message?: string
}

const path = (agentId: string) => `/agents/${encodeURIComponent(agentId)}/upstream-routers`

export const upstreamRouterApi = {
  /**
   * 把这台 Mihomo 设为代理网关的上游路由及其实时发布状态；设置页显式传入正在编辑的空间。
   * 路由器记录、配置空间修订号和 Agent 记录不变时，服务端沿用上次读取（checked_at 为读取时间）；
   * refresh 强制重新读取，用于保存、发布成功或重新检查之后。
   */
  pending: (agentId: string, options: { profileId?: string; refresh?: boolean; signal?: AbortSignal } = {}) =>
    api.get<UpstreamRouterPendingResponse>(path(agentId), {
      params: { ...(options.profileId !== undefined ? { profile: options.profileId } : {}), ...(options.refresh ? { refresh: 1 } : {}) },
      signal: options.signal,
      timeout: 60000,
    }),
}
