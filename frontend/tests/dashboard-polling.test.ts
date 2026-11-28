import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import Dashboard from '@/views/Dashboard.vue'

const api = vi.hoisted(() => ({
  overview: vi.fn(), subscriptions: vi.fn(), groups: vi.fn(), rules: vi.fn(),
  nodes: vi.fn(), latency: vi.fn(), preview: vi.fn(), health: vi.fn(), logs: vi.fn(),
  liveRefresh: vi.fn(), liveStart: vi.fn(), liveStop: vi.fn()
}))

vi.mock('@/api', () => ({
  default: { get: api.logs },
  statsApi: { getOverview: api.overview },
  subscriptionApi: { getAll: api.subscriptions, health: api.health },
  proxyGroupApi: { getAll: api.groups, previewRegex: api.preview },
  ruleSetApi: { getAll: api.rules },
  nodeApi: { getAll: api.nodes, latency: api.latency },
  mihomoPanelApi: { flow: vi.fn() },
  clientToolsApi: { notes: vi.fn() }
}))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/stores/profile', async () => {
  const { ref } = await import('vue')
  return { useProfileStore: () => ({ activeProfile: ref({ name: 'Default' }), activeProfileId: ref('default') }) }
})
vi.mock('@/stores/live', async () => {
  const { ref } = await import('vue')
  return {
    formatRate: vi.fn(),
    useLive: () => ({
      boundAgents: ref([]), downHistory: ref([]), throughput: ref(null), onlineCount: ref(0), speedOf: vi.fn(), isOnline: vi.fn(),
      refresh: api.liveRefresh, start: api.liveStart, stop: api.liveStop
    })
  }
})

let wrapper: VueWrapper | undefined
let hidden = false
const initialReads = [api.overview, api.subscriptions, api.groups, api.rules, api.nodes, api.latency]
const overview = { data: { success: true, data: {} } }

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

function visibility(value: boolean) {
  hidden = value
  document.dispatchEvent(new Event('visibilitychange'))
}

async function render() {
  wrapper = mount(Dashboard, { global: { stubs: {
    FlowMap: { template: '<div data-test="config-map" />' }, LiveFlow: { template: '<div data-test="live-map" />' }, Segmented: true, Sparkline: true, AnimatedNumber: true,
    SectionCard: { template: '<section><slot /></section>' }, SetupEntry: true
  } } })
  await flushPromises()
  return wrapper
}

function expectInitialReads(count: number) {
  initialReads.forEach(read => expect(read).toHaveBeenCalledTimes(count))
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  hidden = false
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden)
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  api.overview.mockResolvedValue(overview)
  api.subscriptions.mockResolvedValue({ data: [{ id: 'sub', name: 'Subscription', enabled: true }] })
  ;[api.groups, api.rules, api.nodes].forEach(read => read.mockResolvedValue({ data: [] }))
  api.latency.mockResolvedValue({ data: { results: {} } })
  api.preview.mockResolvedValue({ data: { nodes: [] } })
  api.health.mockResolvedValue({ data: { items: [] } })
  api.logs.mockResolvedValue({ data: { logs: [] } })
  api.liveRefresh.mockResolvedValue(undefined)
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('dashboard flow view', () => {
  it('switches between the config and live views and remembers the choice', async () => {
    localStorage.clear()
    const view = await render()
    expect(view.find('[data-test="config-map"]').exists()).toBe(true)
    expect(view.find('[data-test="live-map"]').exists()).toBe(false)
    view.getComponent({ name: 'Segmented' }).vm.$emit('update:modelValue', 'live')
    await flushPromises()
    expect(view.find('[data-test="live-map"]').exists()).toBe(true)
    expect(view.find('[data-test="config-map"]').exists()).toBe(false)
    expect(localStorage.getItem('configflow:dashboard:flow-view')).toBe('live')
    wrapper!.unmount()
    const again = await render()
    expect(again.find('[data-test="live-map"]').exists()).toBe(true)
    localStorage.clear()
  })
})

describe('dashboard polling', () => {
  it('keeps the visible-page statistics and event refresh intervals', async () => {
    await render()
    expectInitialReads(1)
    expect(api.logs).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expectInitialReads(1)
    expect(api.logs).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(25000)
    expectInitialReads(2)
    expect(api.health).toHaveBeenCalledTimes(2)
    expect(api.logs).toHaveBeenCalledTimes(7) // 初次读取和六个 5 秒周期
  })

  it('pauses all dashboard reads in a hidden tab and refreshes immediately on return', async () => {
    await render()
    visibility(true)
    await vi.advanceTimersByTimeAsync(120000)
    expectInitialReads(1)
    expect(api.logs).toHaveBeenCalledTimes(1)
    expect(api.health).toHaveBeenCalledTimes(1)
    expect(api.liveRefresh).toHaveBeenCalledTimes(1)

    visibility(false)
    await flushPromises()
    expectInitialReads(2)
    expect(api.logs).toHaveBeenCalledTimes(2)
    expect(api.health).toHaveBeenCalledTimes(2)
    expect(api.liveRefresh).toHaveBeenCalledTimes(2)
  })

  it('waits for visibility when first mounted in a hidden tab', async () => {
    hidden = true
    await render()
    await vi.advanceTimersByTimeAsync(60000)
    expectInitialReads(0)
    expect(api.logs).not.toHaveBeenCalled()
    expect(api.health).not.toHaveBeenCalled()
    visibility(false)
    await flushPromises()
    expectInitialReads(1)
  })

  it('does not overlap slow statistics reads when timers and visibility events coincide', async () => {
    const pending = deferred<typeof overview>()
    api.overview.mockReturnValueOnce(pending.promise)
    const view = await render()
    await vi.advanceTimersByTimeAsync(90000)
    visibility(true)
    visibility(false)
    await view.get('[aria-label="刷新"]').trigger('click')
    expectInitialReads(1)
    expect(view.get('[aria-label="刷新"]').attributes('disabled')).toBeDefined()
    pending.resolve(overview)
    await flushPromises()
    expect(view.get('[aria-label="刷新"]').attributes('disabled')).toBeUndefined()
    await view.get('[aria-label="刷新"]').trigger('click')
    await flushPromises()
    expectInitialReads(2)
  })

  it('keeps a refresh in flight while subscription node preview is pending', async () => {
    const pending = deferred<{ data: { nodes: [] } }>()
    api.preview.mockReturnValueOnce(pending.promise)
    await render()
    await vi.advanceTimersByTimeAsync(60000)
    expectInitialReads(1)
    expect(api.preview).toHaveBeenCalledTimes(1)
    pending.resolve({ data: { nodes: [] } })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30000)
    expectInitialReads(2)
  })

  it('does not overlap slow log and health reads with periodic or manual refreshes', async () => {
    const logs = deferred<{ data: { logs: [] } }>()
    const health = deferred<{ data: { items: [] } }>()
    api.logs.mockReturnValueOnce(logs.promise)
    api.health.mockReturnValueOnce(health.promise)
    const view = await render()
    await vi.advanceTimersByTimeAsync(60000)
    await view.get('[aria-label="刷新订阅健康"]').trigger('click')
    expectInitialReads(1)
    expect(api.logs).toHaveBeenCalledTimes(1)
    expect(api.health).toHaveBeenCalledTimes(1)
    logs.resolve({ data: { logs: [] } })
    health.resolve({ data: { items: [] } })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30000)
    expectInitialReads(2)
    expect(api.health).toHaveBeenCalledTimes(2)
  })

  it('releases refresh guards after failures so a later refresh can recover', async () => {
    api.overview.mockRejectedValueOnce(new Error('offline'))
    api.health.mockRejectedValueOnce(new Error('offline'))
    api.logs.mockRejectedValueOnce(new Error('offline'))
    const view = await render()
    expect(view.find('[role="alert"]').exists()).toBe(true)
    await view.get('[aria-label="刷新"]').trigger('click')
    await flushPromises()
    expectInitialReads(2)
    expect(api.health).toHaveBeenCalledTimes(2)
    expect(api.logs).toHaveBeenCalledTimes(2)
    expect(view.find('[role="alert"]').exists()).toBe(false)
  })

  it.each(['hidden', 'unmounted'])('does not start follow-up reads after the page becomes %s', async state => {
    const pending = deferred<typeof overview>()
    api.overview.mockReturnValueOnce(pending.promise)
    await render()
    if (state === 'hidden') visibility(true)
    else { wrapper!.unmount(); wrapper = undefined }
    pending.resolve(overview)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(60000)
    expectInitialReads(1)
    expect(api.preview).not.toHaveBeenCalled()
    expect(api.health).not.toHaveBeenCalled()
    expect(api.logs).not.toHaveBeenCalled()
    expect(api.liveRefresh).not.toHaveBeenCalled()
    if (state === 'unmounted') {
      visibility(false)
      await flushPromises()
      expectInitialReads(1)
      expect(api.liveStop).toHaveBeenCalledTimes(1)
    }
  })

  it.each(['hidden', 'unmounted'])('stops chained reads when a slow preview finishes after becoming %s', async state => {
    const pending = deferred<{ data: { nodes: [] } }>()
    api.preview.mockReturnValueOnce(pending.promise)
    await render()
    expect(api.preview).toHaveBeenCalledTimes(1)
    if (state === 'hidden') visibility(true)
    else { wrapper!.unmount(); wrapper = undefined }
    pending.resolve({ data: { nodes: [] } })
    await flushPromises()
    expect(api.health).not.toHaveBeenCalled()
    expect(api.logs).not.toHaveBeenCalled()
    expect(api.liveRefresh).not.toHaveBeenCalled()
    if (state === 'hidden') {
      visibility(false)
      await flushPromises()
      expectInitialReads(2)
      expect(api.health).toHaveBeenCalledTimes(1)
    }
  })
})
