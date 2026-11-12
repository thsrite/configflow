import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosHeaders, type AxiosResponse } from 'axios'
import ProxyGroups from '@/views/ProxyGroups.vue'
import api, { nodeApi, proxyGroupApi } from '@/api'
import { setActiveProfileId } from '@/profileContext'
import type { ProxyGroup } from '@/types'

// Mock only HTTP: exercise the real component's mount/unmount and request queues.
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
  // 平板布局：手机断点（639px）以外的查询都命中
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: !String(query).includes('639px'), addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
  sessionStorage.clear()
  setActiveProfileId('profile-a')
  groups = [group('Hong Kong')]
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
async function debounce() {
  await vi.advanceTimersByTimeAsync(280)
  await flushPromises()
}
function invalidRegex() {
  return { response: { status: 400, data: { message: '正则表达式无效' } } }
}

describe('proxy group navigation lifecycle', () => {
  it('does not dispatch a pending debounce after leaving the page', async () => {
    await render()
    const calls = vi.mocked(proxyGroupApi.previewRegex).mock.calls.length
    wrapper!.unmount()
    wrapper = undefined
    await debounce()
    expect(proxyGroupApi.previewRegex).toHaveBeenCalledTimes(calls)
  })

  it('ignores the result of a preview already running when the page is left', async () => {
    const old = deferred<AxiosResponse>()
    // Initial counts run immediately; only the later detail preview stays pending.
    vi.mocked(proxyGroupApi.previewRegex)
      .mockResolvedValueOnce(result())
      .mockReturnValueOnce(old.promise)
    await render()
    await debounce()
    expect(proxyGroupApi.previewRegex).toHaveBeenCalledTimes(2)
    const state = (wrapper!.vm as any).$.setupState
    expect(state.preview.loading).toBe(true)
    expect(state.preview.nodes).toEqual([])
    const previousNodes = JSON.parse(JSON.stringify(state.preview.nodes))
    const previousCounts = { ...state.matchCounts }
    wrapper!.unmount()
    wrapper = undefined
    old.resolve(result(['Stale Hong Kong']))
    await flushPromises()
    expect(state.preview.nodes).toEqual(previousNodes)
    expect(state.matchCounts).toEqual(previousCounts)
  })

  it('stops the count worker queue and ignores in-flight results after leaving', async () => {
    groups = [group('Manual', { subscriptions: [], manual_nodes: ['DIRECT'] }), ...Array.from({ length: 8 }, (_, index) => group(`Group ${index}`))]
    const requests = Array.from({ length: 3 }, () => deferred<AxiosResponse>())
    requests.forEach(request => vi.mocked(proxyGroupApi.previewRegex).mockReturnValueOnce(request.promise))
    await render()
    await flushPromises()
    expect(proxyGroupApi.previewRegex).toHaveBeenCalledTimes(3)
    const state = (wrapper!.vm as any).$.setupState
    wrapper!.unmount()
    wrapper = undefined
    requests[1].resolve(result(['Second']))
    requests[0].reject(invalidRegex())
    requests[2].resolve(result(['Third']))
    await flushPromises()
    await debounce()
    expect(proxyGroupApi.previewRegex).toHaveBeenCalledTimes(3)
    expect(state.matchCounts).toEqual({})
  })

  it.each(['url-test', 'fallback', 'load-balance'] as const)('does not infer %s runtime failures or selection from cached latency', async type => {
    groups = [group('Automatic', { type })]
    vi.mocked(nodeApi.latency).mockResolvedValue(response({ results: {
      'HK 01': { latency: null }, 'Hong Kong 02': { latency: 1 }
    } }))
    await render()
    await debounce()
    expect(wrapper!.text()).toContain('HK 01')
    expect(wrapper!.text()).toContain('实际延迟与可用性请在目标设备的 Mihomo 面板查看')
    expect(wrapper!.text()).not.toContain('超时')
    expect(wrapper!.text()).not.toContain('按测速预计生效')
    expect(wrapper!.find('.matched-node.is-winner').exists()).toBe(false)
    expect(nodeApi.latency).not.toHaveBeenCalled()
  })
  it('preserves the manual group default without latency history', async () => {
    groups = [group('Manual', { type: 'select' })]
    await render()
    await debounce()
    const details = wrapper!.get('[aria-label="策略组详情"]')
    expect(details.text()).toContain('默认选中：')
    expect(details.get('.matched-node.is-winner').text()).toBe('HK 01')
    expect(nodeApi.latency).not.toHaveBeenCalled()
  })

})
