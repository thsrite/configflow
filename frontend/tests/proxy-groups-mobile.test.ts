import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosHeaders, type AxiosResponse } from 'axios'
import ProxyGroups from '@/views/ProxyGroups.vue'
import api, { nodeApi, proxyGroupApi } from '@/api'
import { setActiveProfileId } from '@/profileContext'
import type { ProxyGroup } from '@/types'

// 手机上的策略组：先列表，点一个进入详情，可返回。只替换 HTTP。
vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn() },
  nodeApi: { getAll: vi.fn(), latency: vi.fn() },
  proxyGroupApi: { getAll: vi.fn(), previewRegex: vi.fn(), update: vi.fn() }
}))

const regex = 'HK|Hong Kong|香港'
const matches = [
  { name: 'HK 01', type: 'vless' },
  { name: 'Hong Kong 02', type: 'vless' },
  { name: '香港 03', type: 'vless' }
]
let wrapper: VueWrapper | undefined
let scrollDescriptor: PropertyDescriptor | undefined
let groups: ProxyGroup[]

function response<T>(data: T): AxiosResponse<T> {
  return { data, status: 200, statusText: 'OK', headers: new AxiosHeaders(), config: { headers: new AxiosHeaders() } }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function result(names = matches.map(node => node.name), total = 263) {
  return response({ success: true, nodes: names.map(name => ({ name, type: 'vless' })), count: names.length, total_candidates: total })
}
function group(id: string, overrides: Partial<ProxyGroup> = {}): ProxyGroup {
  return { id, name: id, type: 'url-test', enabled: true, subscriptions: ['sub'], regex, ...overrides }
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  // 手机宽度：所有查询都命中（含 max-width: 639px）
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
  sessionStorage.clear()
  setActiveProfileId('profile-a')
  groups = [group('Hong Kong'), group('Japan', { type: 'select' }), group('Fallback', { type: 'fallback' })]
  vi.mocked(nodeApi.getAll).mockResolvedValue(response([]))
  vi.mocked(proxyGroupApi.getAll).mockImplementation(async () => response(JSON.parse(JSON.stringify(groups))))
  vi.mocked(proxyGroupApi.previewRegex).mockResolvedValue(result())
  vi.mocked(api.get).mockImplementation(async path => response(
    path === '/subscriptions' ? [{ id: 'sub', name: 'Subscription', cached_node_count: 263 }] :
    path === '/aggregations' ? [{ id: 'agg', name: 'Aggregation', enabled: true }] : { total_count: 263 }
  ))
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.unstubAllGlobals()
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})

async function render() {
  wrapper = mount(ProxyGroups, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

describe('proxy groups on a phone', () => {
  it('starts with the list of every group instead of one group behind a picker', async () => {
    const view = await render()
    expect(view.find('[aria-label="策略组详情"]').exists()).toBe(false)
    expect(view.find('button[aria-haspopup="dialog"][aria-label^="切换策略组"]').exists()).toBe(false)
    expect(view.findAll('[role="list"][aria-label="策略组"] [role="listitem"]').map(item => item.attributes('data-name'))).toEqual(['Hong Kong', 'Japan', 'Fallback'])
  })

  it('opens a group detail with a way back to the list', async () => {
    const view = await render()
    await view.get('[data-name="Japan"] button[title="Japan"]').trigger('click')
    await flushPromises()
    expect(view.find('[role="list"][aria-label="策略组"]').exists()).toBe(false)
    expect(view.get('[aria-label="策略组详情"]').text()).toContain('Japan')
    await view.get('[data-group-back]').trigger('click')
    await flushPromises()
    expect(view.find('[aria-label="策略组详情"]').exists()).toBe(false)
    expect(view.find('[role="list"][aria-label="策略组"]').exists()).toBe(true)
  })

  it('keeps the header actions on one row by showing the secondary ones as icons', async () => {
    const view = await render()
    for (const label of ['批量测速地址', '调整顺序']) {
      const button = view.get(`button[aria-label="${label}"]`)
      expect(button.classes()).toContain('max-sm:size-11')
      expect(button.get('span').classes()).toContain('max-sm:sr-only')
    }
  })
})
