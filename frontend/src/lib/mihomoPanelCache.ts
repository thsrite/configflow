import type { MihomoProxy } from '@/types/mihomo'

/**
 * Mihomo 面板最近一次读到的内核状态、策略、代理集合和规则，按设备留在内存里。
 * 再次打开页面时先显示这些内容，同时照常重新读取、读到后替换，所以请求次数和新数据到达的时间都不变。
 * 只放变化不频繁的数据，连接、流量等实时数据不进来；刷新浏览器页面后清空。
 */
export type CachedPanelResource = 'version' | 'configs' | 'proxies' | 'providers/proxies' | 'rules' | 'providers/rules'

const entries = new Map<string, unknown>()
const keyOf = (agentId: string, resource: CachedPanelResource) => JSON.stringify([agentId, resource])

/** 上次读到的内核返回内容（接口的 data 字段），没读过时为 undefined */
export function cachedPanelData<T>(agentId: string, resource: CachedPanelResource): T | undefined {
  return entries.get(keyOf(agentId, resource)) as T | undefined
}

export function rememberPanelData(agentId: string, resource: CachedPanelResource, data: unknown) {
  if (agentId && data && typeof data === 'object') entries.set(keyOf(agentId, resource), data)
}

/** 切换或取消固定后改写缓存里的这个策略组，下次打开时不会先闪回旧的选择 */
export function rememberPanelProxy(agentId: string, proxy: MihomoProxy) {
  const body = cachedPanelData<{ proxies?: Record<string, MihomoProxy> }>(agentId, 'proxies')
  if (body?.proxies?.[proxy.name]) rememberPanelData(agentId, 'proxies', { ...body, proxies: { ...body.proxies, [proxy.name]: proxy } })
}

export function clearPanelCache() {
  entries.clear()
}
