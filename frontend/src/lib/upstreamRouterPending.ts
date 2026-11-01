import type { LocationQuery, RouteLocationRaw } from 'vue-router'
import type { UpstreamRouterStatus } from '@/api/upstreamRouterPending'

/** `UpstreamRouterPending` 通过 `@status` 发出的汇总；none 表示没有路由器把这台 Mihomo 设为代理网关。 */
export type UpstreamRouterSummaryState = 'none' | 'synced' | 'pending' | 'unknown'
export interface UpstreamRouterSummary {
  state: UpstreamRouterSummaryState
  pending: number
  /** 这次结果读取路由器的时间；服务端在缓存键不变时沿用上次读取 */
  checkedAt?: string
}

/**
 * 有待发布或暂时无法发布（blocked）的路由器时为 pending，项数相加，blocked 至少计 1；
 * 否则任一读取失败为 unknown；全部同步为 synced。
 */
export function summarizeUpstreamRouters(routers: UpstreamRouterStatus[]): UpstreamRouterSummary {
  if (!routers.length) return { state: 'none', pending: 0 }
  const waiting = routers.filter(router => router.state === 'pending' || router.state === 'blocked')
  if (waiting.length) {
    return { state: 'pending', pending: waiting.reduce((total, router) =>
      total + Math.max(router.state === 'blocked' ? 1 : 0, Number(router.pending) || 0), 0) }
  }
  if (routers.some(router => router.state !== 'synced')) return { state: 'unknown', pending: 0 }
  return { state: 'synced', pending: 0 }
}

/** 上游路由页的直达地址：打开该路由器的差异报告，preview 时接着打开「预览发布」。 */
export function upstreamRouterLink(router: Pick<UpstreamRouterStatus, 'id' | 'profile_id'>, preview: boolean): RouteLocationRaw {
  return { path: '/routers', query: { router: router.id, ...(router.profile_id ? { profile: router.profile_id } : {}), ...(preview ? { preview: '1' } : {}) } }
}

export interface RouterDeepLink { router: string; profile: string; preview: boolean; section?: string }
export const ROUTER_DEEP_LINK_KEYS = ['router', 'profile', 'preview', 'section'] as const

const single = (value: LocationQuery[string]) => (typeof value === 'string' ? value.trim() : '')

/** 读取 `/routers?router=<id>&profile=<配置空间>&preview=1`；没有 router 时为 null。 */
export function readRouterDeepLink(query: LocationQuery): RouterDeepLink | null {
  const router = single(query.router)
  if (!router || router.length > 128) return null
  const profile = single(query.profile)
  const section = single(query.section)
  const sections = ['report', 'rule-sync', 'ipv4-forward-hint', 'ipv6-fakeip', 'ipv6-routes', 'ipv6-dns', 'ipv6-pd-guard']
  return { router, profile: profile.length <= 128 ? profile : '', preview: single(query.preview) === '1',
    ...(sections.includes(section) ? { section } : {}) }
}
