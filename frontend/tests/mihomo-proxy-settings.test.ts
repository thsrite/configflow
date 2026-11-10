import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import MihomoProxies from '@/components/mihomo/MihomoProxies.vue'
import { mihomoPanelApi } from '@/api'
import { notify } from '@/lib/feedback'
import { DEFAULT_PROXY_SETTINGS, finalNode, loadProxySettings, saveProxySettings } from '@/types/mihomo'

vi.mock('@/api', () => ({
  mihomoPanelApi: { groupDelay: vi.fn(), proxyDelay: vi.fn(), providerProxyDelay: vi.fn(), proxies: vi.fn(), proxyProviders: vi.fn(), selectProxy: vi.fn(), connections: vi.fn(), closeConnection: vi.fn() }
}))
vi.mock('@/lib/feedback', () => ({ notify: { info: vi.fn(), success: vi.fn(), error: vi.fn() } }))

function stubStorage() {
  const values = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    get length() { return values.size },
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, String(value)) },
    removeItem: (key: string) => { values.delete(key) },
    clear: () => { values.clear() },
    key: (index: number) => Array.from(values.keys())[index] ?? null
  } satisfies Storage)
}

const history = (delay: number) => [{ time: '', delay }]
const PROXIES = {
  GLOBAL: { name: 'GLOBAL', type: 'Selector', all: ['Proxy', 'Hidden'], now: 'Proxy' },
  Proxy: { name: 'Proxy', type: 'Selector', all: ['Slow', 'Dead', 'Auto', 'Fast'], now: 'Auto' },
  Auto: { name: 'Auto', type: 'URLTest', all: ['Fast'], now: 'Fast' },
  Hidden: { name: 'Hidden', type: 'Selector', all: ['Fast'], now: 'Fast', hidden: true },
  Slow: { name: 'Slow', type: 'Vmess', history: history(600) }
}
const PROVIDERS = { sub: { name: 'sub', type: 'Proxy', vehicleType: 'HTTP', proxies: [
  { name: 'Fast', type: 'Trojan', history: history(40) }, { name: 'Dead', type: 'Trojan', history: history(0) }
] } }

const wrappers: VueWrapper[] = []
async function render() {
  const wrapper = mount(MihomoProxies, { props: { agentId: 'gw', reloadToken: 0 }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  const state = (wrapper.vm as any).$.setupState
  state.expanded = new Set(['Proxy'])
  await flushPromises()
  return { wrapper, state }
}
const memberNames = (wrapper: VueWrapper) => wrapper.findAll('button[aria-pressed]').map(button => button.find('span').text())

beforeEach(() => {
  vi.clearAllMocks()
  stubStorage()
  vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: PROXIES } } } as any)
  vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: PROVIDERS } } } as any)
  vi.mocked(mihomoPanelApi.proxyDelay).mockResolvedValue({ data: { data: { delay: 123 } } } as any)
  vi.mocked(mihomoPanelApi.providerProxyDelay).mockResolvedValue({ data: { data: { delay: 123 } } } as any)
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.unstubAllGlobals() })

describe('proxy panel settings', () => {
  it('validates stored settings and falls back to defaults', () => {
    localStorage.setItem('configflow:mihomo-panel:proxy-settings', JSON.stringify({ sort: 'bogus', hideUnavailable: 'yes', cardMinWidth: 9999, showFinalNode: true }))
    expect(loadProxySettings()).toEqual({ ...DEFAULT_PROXY_SETTINGS, showFinalNode: true, cardMinWidth: 320 })
    saveProxySettings({ ...DEFAULT_PROXY_SETTINGS, sort: 'delay' })
    expect(loadProxySettings().sort).toBe('delay')
  })

  it('resolves the final node through nested groups', () => {
    expect(finalNode(PROXIES as any, 'Proxy')).toBe('Fast')
    expect(finalNode(PROXIES as any, 'Slow')).toBe('Slow')
  })

  it('sorts by delay, hides unavailable nodes and shows hidden groups on request', async () => {
    const { wrapper, state } = await render()
    expect(memberNames(wrapper)).toEqual(['Slow', 'Dead', 'Auto', 'Fast'])
    expect(wrapper.text()).not.toContain('Hidden')
    state.applySettings({ ...state.settings, sort: 'delay', hideUnavailable: true, showHiddenGroups: true, showFinalNode: true })
    await flushPromises()
    expect(memberNames(wrapper)).toEqual(['Auto', 'Fast', 'Slow'])
    expect(wrapper.text()).toContain('Hidden')
    expect(wrapper.text()).toContain('Auto → Fast')
    expect(JSON.parse(localStorage.getItem('configflow:mihomo-panel:proxy-settings')!).sort).toBe('delay')
  })

  it('groups members by provider', async () => {
    const { wrapper, state } = await render()
    state.applySettings({ ...state.settings, groupByProvider: true })
    await flushPromises()
    expect(wrapper.text()).toContain('配置节点 · 1')
    expect(wrapper.text()).toContain('sub · 2')
    expect(wrapper.text()).toContain('策略组 · 1')
  })

  it('closes connections through the group after switching', async () => {
    vi.mocked(mihomoPanelApi.selectProxy).mockResolvedValue({} as any)
    vi.mocked(mihomoPanelApi.closeConnection).mockResolvedValue({} as any)
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue({ data: { data: { connections: [
      { id: 'a', chains: ['Fast', 'Auto', 'Proxy'] }, { id: 'b', chains: ['DIRECT'] }
    ] } } } as any)
    const { state } = await render()
    await state.select(PROXIES.Proxy, 'Fast')
    expect(mihomoPanelApi.selectProxy).toHaveBeenCalledWith('gw', 'Proxy', 'Fast')
    expect(mihomoPanelApi.closeConnection).toHaveBeenCalledTimes(1)
    expect(mihomoPanelApi.closeConnection).toHaveBeenCalledWith('gw', 'a')
    expect(notify.success).toHaveBeenCalledWith('Proxy 已切换到 Fast', '已断开经过该策略组的 1 条连接')

    vi.clearAllMocks()
    state.applySettings({ ...state.settings, closeConnectionsOnSwitch: false })
    await state.select({ ...PROXIES.Proxy, now: 'Fast' }, 'Slow')
    expect(mihomoPanelApi.connections).not.toHaveBeenCalled()
  })
})


describe('individual and all-group delay tests', () => {
  it('shows text and progress while testing and restores the action after completion', async () => {
    const pending: Array<(value: any) => void> = []
    vi.mocked(mihomoPanelApi.groupDelay).mockImplementation(() => new Promise(resolve => pending.push(resolve)))
    const { wrapper } = await render()
    const button = wrapper.findAll('button').find(item => item.text() === '全部分组测速')!
    await button.trigger('click')
    await flushPromises()
    expect(button.text()).toBe('正在测速…')
    expect(button.attributes('aria-busy')).toBe('true')
    expect(button.attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="status"]').text()).toContain('已完成 0 / 4 组')
    pending.shift()!({ data: { data: {} } })
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('已完成 1 / 4 组')
    for (let i = 0; i < 3; i++) {
      pending.shift()!({ data: { data: {} } })
      await flushPromises()
    }
    expect(button.text()).toBe('全部分组测速')
    expect(button.attributes('aria-busy')).toBe('false')
    expect(button.attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })
  it('tests a provider node without selecting it and isolates the test URL', async () => {
    const { wrapper, state } = await render()
    await wrapper.get('button[aria-label="单节点测速 Fast（Proxy）"]').trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.providerProxyDelay).toHaveBeenCalledWith('gw', 'sub', 'Fast', undefined)
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(123)
    expect(state.delayOf('Fast', { ...PROXIES.Proxy, testUrl: 'https://different.test' })).toBe(40)
    expect(wrapper.find('button[aria-label="当前出口测速 Auto（Proxy）"]').exists()).toBe(true)
  })
  it('keeps ordinary node tests on the existing endpoint', async () => {
    const { wrapper, state } = await render()
    await wrapper.get('button[aria-label="单节点测速 Slow（Proxy）"]').trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledWith('gw', 'Slow', undefined)
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(state.delayOf('Slow', PROXIES.Proxy)).toBe(123)
  })
  it.each(['URLTest', 'Selector'])('tests the current provider exit of a nested %s card without bubbling or switching', async type => {
    const testUrl = 'https://parent.test/generate_204?key=a%20b&check=1'
    const parent = { ...PROXIES.Proxy, now: 'Slow', testUrl }
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: {
      ...PROXIES,
      Proxy: parent,
      Auto: { ...PROXIES.Auto, type, all: ['Nested'], now: 'Nested', history: history(900), testUrl: 'https://auto.test' },
      Nested: { name: 'Nested', type: 'Selector', all: ['Fast'], now: 'Fast', testUrl: 'https://nested.test' }
    } } } } as any)
    const { wrapper, state } = await render()
    const button = wrapper.get('button[aria-label="当前出口测速 Auto（Proxy）"]')
    const bubbled = vi.fn()
    button.element.parentElement!.addEventListener('click', bubbled)
    await button.trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.providerProxyDelay).toHaveBeenCalledExactlyOnceWith('gw', 'sub', 'Fast', testUrl)
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.groupDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
    expect(mihomoPanelApi.connections).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
    expect(bubbled).not.toHaveBeenCalled()
    expect(state.proxies.Proxy.now).toBe('Slow')
    expect(state.delayOf('Fast', parent)).toBe(123)
    expect(state.delayOf('Auto', parent)).toBe(123)
    expect(button.text()).toBe('123 ms')
    expect(state.delayOf('Fast', { ...parent, testUrl: 'https://different.test' })).toBe(40)
    expect(state.delayOf('Auto', { ...parent, testUrl: 'https://different.test' })).toBe(900)
  })
  it.each(['Slow', 'DIRECT'])('tests the current core exit %s through the single-node endpoint', async target => {
    const testUrl = 'https://parent.test/generate_204'
    const parent = { ...PROXIES.Proxy, testUrl }
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: {
      ...PROXIES, Proxy: parent,
      Auto: { ...PROXIES.Auto, all: [target], now: target },
      DIRECT: { name: 'DIRECT', type: 'Direct' }
    } } } } as any)
    const { wrapper, state } = await render()
    await wrapper.get('button[aria-label="当前出口测速 Auto（Proxy）"]').trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledExactlyOnceWith('gw', target, testUrl)
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.groupDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
    expect(state.delayOf(target, parent)).toBe(123)
    expect(state.delayOf('Auto', parent)).toBe(123)
  })
  it.each(['LoadBalance', 'Relay'])('tests a native %s group without now through the single-item core endpoint', async type => {
    const testUrl = 'https://parent.test/generate_204'
    const parent = { ...PROXIES.Proxy, testUrl }
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: {
      ...PROXIES, Proxy: parent, Auto: { ...PROXIES.Auto, type, now: undefined }
    } } } } as any)
    const { state } = await render()
    expect(state.canTestNode('Auto')).toBe(true)
    await state.testNode('Auto', parent)
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledExactlyOnceWith('gw', 'Auto', testUrl)
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.groupDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
    expect(state.delayOf('Auto', parent)).toBe(123)
    expect(notify.error).not.toHaveBeenCalled()
  })
  it('follows a changed group exit without reusing the previous exit result', async () => {
    const { wrapper, state } = await render()
    const button = wrapper.get('button[aria-label="当前出口测速 Auto（Proxy）"]')
    await button.trigger('click')
    await flushPromises()
    expect(button.text()).toBe('123 ms')
    state.proxies.Auto = { ...state.proxies.Auto, now: 'Slow' }
    await flushPromises()
    expect(button.text()).toBe('600 ms')
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(123)
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: { ...PROXIES, Auto: { ...PROXIES.Auto, now: 'Slow' } } } } } as any)
    vi.mocked(mihomoPanelApi.proxyDelay).mockResolvedValueOnce({ data: { data: { delay: 85 } } } as any)
    await button.trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledExactlyOnceWith('gw', 'Slow', undefined)
    expect(button.text()).toBe('85 ms')
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(123)
  })
  it('reads the live exit before testing a group member instead of the cached one', async () => {
    const { wrapper, state } = await render()
    expect(state.proxies.Auto.now).toBe('Fast')
    // 核心已自行切到另一个出口（如自动测速、重载后），页面仍是旧出口
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: {
      ...PROXIES, Auto: { ...PROXIES.Auto, all: ['Fast', 'Slow'], now: 'Slow' }
    } } } } as any)
    vi.mocked(mihomoPanelApi.proxyDelay).mockRejectedValueOnce({ response: { status: 503, data: { message: 'An error occurred in the delay test' } } })
    const button = wrapper.get('button[aria-label="当前出口测速 Auto（Proxy）"]')
    await button.trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledExactlyOnceWith('gw', 'Slow', undefined)
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(notify.error).toHaveBeenCalledWith('Auto 测速失败', '当前出口 Slow：An error occurred in the delay test')
    expect(state.proxies.Auto.now).toBe('Slow')
    expect(button.text()).toBe('600 ms')
    // 普通节点不需要读出口
    vi.mocked(mihomoPanelApi.proxies).mockClear()
    await state.testNode('Slow', PROXIES.Proxy)
    expect(mihomoPanelApi.proxies).not.toHaveBeenCalled()
  })
  it('stores an in-flight result on its original exit when the group changes exit', async () => {
    let resolve!: (value: any) => void
    vi.mocked(mihomoPanelApi.providerProxyDelay).mockImplementationOnce(() => new Promise(yes => { resolve = yes }))
    const { wrapper, state } = await render()
    const button = wrapper.get('button[aria-label="当前出口测速 Auto（Proxy）"]')
    await button.trigger('click')
    state.proxies.Auto = { ...state.proxies.Auto, now: 'Slow' }
    await flushPromises()
    expect(button.text()).toBe('600 ms')
    resolve({ data: { data: { delay: 77 } } })
    await flushPromises()
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(77)
    expect(state.delayOf('Slow', PROXIES.Proxy)).toBe(600)
    expect(state.delayOf('Auto', PROXIES.Proxy)).toBe(600)
    expect(button.text()).toBe('600 ms')
    expect(button.attributes('aria-busy')).toBe('false')
    expect(button.attributes('disabled')).toBeUndefined()
    expect(state.nodeTesting.size).toBe(0)
    expect(mihomoPanelApi.providerProxyDelay).toHaveBeenCalledTimes(1)
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
  })
  it('lets a group result replace the earlier exit test without following the child to a new exit', async () => {
    const parent = { ...PROXIES.Proxy, all: ['Auto'] }
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: { ...PROXIES, Proxy: parent } } } } as any)
    vi.mocked(mihomoPanelApi.groupDelay).mockResolvedValueOnce({ data: { data: { Auto: 500 } } } as any)
    const { wrapper, state } = await render()
    const button = wrapper.get('button[aria-label="当前出口测速 Auto（Proxy）"]')
    await state.testNode('Auto', parent)
    expect(state.delayOf('Auto', parent)).toBe(123)
    await state.testGroup(parent)
    await flushPromises()
    expect(mihomoPanelApi.groupDelay).toHaveBeenCalledExactlyOnceWith('gw', 'Proxy', undefined)
    expect(state.delayOf('Fast', parent)).toBe(123)
    expect(button.text()).toBe('500 ms')
    state.proxies.Auto = { ...state.proxies.Auto, now: 'Slow' }
    await flushPromises()
    expect(button.text()).toBe('600 ms')
    expect(state.delayOf('Fast', parent)).toBe(123)
  })
  it.each([
    { reason: 'both members return delays', result: { Auto: 500, Fast: 80 }, autoDelay: 500, fastDelay: 80 },
    { reason: 'the direct member is missing', result: { Auto: 500 }, autoDelay: 500, fastDelay: 0 },
    { reason: 'the child group is missing', result: { Fast: 80 }, autoDelay: 0, fastDelay: 80 }
  ])('keeps batch results independent for members sharing an exit when $reason', async ({ result, autoDelay, fastDelay }) => {
    const parent = { ...PROXIES.Proxy, all: ['Auto', 'Fast'] }
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: { ...PROXIES, Proxy: parent } } } } as any)
    vi.mocked(mihomoPanelApi.groupDelay).mockResolvedValueOnce({ data: { data: result } } as any)
    const { state } = await render()
    await state.testGroup(parent)
    expect(state.delayOf('Auto', parent)).toBe(autoDelay)
    expect(state.delayOf('Fast', parent)).toBe(fastDelay)
    await state.testNode('Fast', parent)
    expect(state.delayOf('Auto', parent)).toBe(123)
    expect(state.delayOf('Fast', parent)).toBe(123)
  })
  it('discards only changed exits from an in-flight group result', async () => {
    const parent = { ...PROXIES.Proxy, all: ['Auto', 'Slow'] }
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: { ...PROXIES, Proxy: parent } } } } as any)
    let resolve!: (value: any) => void
    vi.mocked(mihomoPanelApi.groupDelay).mockImplementationOnce(() => new Promise(yes => { resolve = yes }))
    const { state } = await render()
    await state.testNode('Auto', parent)
    const pending = state.testGroup(parent)
    state.proxies.Auto = { ...state.proxies.Auto, now: 'Dead' }
    resolve({ data: { data: { Auto: 500, Slow: 55 } } })
    await pending
    expect(state.delayOf('Auto', parent)).toBe(0)
    expect(state.delayOf('Dead', parent)).toBe(0)
    expect(state.delayOf('Fast', parent)).toBe(123)
    expect(state.delayOf('Slow', parent)).toBe(55)
    expect(state.testing.size).toBe(0)
    expect(notify.error).not.toHaveBeenCalled()
  })
  it.each([
    { reason: 'Selector without now', auto: { type: 'Selector', now: undefined }, error: '策略组没有可用的当前出口，请刷新后重试。' },
    { reason: 'URLTest without now', auto: { type: 'URLTest', now: undefined }, error: '策略组没有可用的当前出口，请刷新后重试。' },
    { reason: 'a cyclic now chain', auto: { now: 'Proxy' }, error: '策略组没有可用的当前出口，请刷新后重试。' },
    { reason: 'a missing exit', auto: { now: 'Missing' }, error: '找不到该节点，请刷新后重试。' },
    { reason: 'a rejected exit', auto: { now: 'REJECT' }, error: '当前出口拒绝连接，无法测速。' }
  ])('does not send a request for $reason', async ({ auto, error }) => {
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: {
      ...PROXIES, Auto: { ...PROXIES.Auto, ...auto }, REJECT: { name: 'REJECT', type: 'Reject' }
    } } } } as any)
    const { wrapper, state } = await render()
    const button = wrapper.get('button[aria-label="当前出口测速 Auto（Proxy）"]')
    await button.trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.groupDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
    expect(notify.error).toHaveBeenCalledWith('Auto 测速失败', error)
    expect(state.nodeTesting.size).toBe(0)
    expect(state.delays.size).toBe(0)
    expect(button.attributes('aria-busy')).toBe('false')
    expect(button.attributes('disabled')).toBeUndefined()
  })
  it('shows pending state for a group exit and recovers after failure without duplicate requests', async () => {
    let reject!: (reason: any) => void
    vi.mocked(mihomoPanelApi.providerProxyDelay).mockImplementationOnce(() => new Promise((_yes, no) => { reject = no }))
    const { wrapper, state } = await render()
    const button = wrapper.get('button[aria-label="当前出口测速 Auto（Proxy）"]')
    await button.trigger('click')
    expect(button.attributes('aria-busy')).toBe('true')
    expect(button.attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[aria-label="测速 Proxy"]').attributes('disabled')).toBeDefined()
    await state.testNode('Auto', PROXIES.Proxy)
    expect(mihomoPanelApi.providerProxyDelay).toHaveBeenCalledTimes(1)
    reject({ response: { status: 504, data: { message: 'exit timed out' } } })
    await flushPromises()
    expect(notify.error).toHaveBeenCalledWith('Auto 测速失败', '当前出口 Fast：exit timed out')
    expect(button.attributes('aria-busy')).toBe('false')
    expect(button.attributes('disabled')).toBeUndefined()
    expect(wrapper.get('button[aria-label="测速 Proxy"]').attributes('disabled')).toBeUndefined()
    expect(button.text()).toBe('40 ms')
    expect(state.nodeTesting.size).toBe(0)
    expect(state.delays.size).toBe(0)
    await button.trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.providerProxyDelay).toHaveBeenCalledTimes(2)
    expect(button.text()).toBe('123 ms')
    expect(mihomoPanelApi.groupDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
  })
  it('tests DIRECT normally and displays a zero delay as a timeout', async () => {
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: {
      ...PROXIES, DIRECT: { name: 'DIRECT', type: 'Direct' }
    } } } } as any)
    vi.mocked(mihomoPanelApi.proxyDelay).mockResolvedValue({ data: { data: { delay: 0 } } } as any)
    const { state } = await render()
    expect(state.canTestNode('DIRECT')).toBe(true)
    await state.testNode('DIRECT', PROXIES.Proxy)
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledWith('gw', 'DIRECT', undefined)
    expect(state.delayOf('DIRECT', PROXIES.Proxy)).toBe(0)
    expect(notify.error).not.toHaveBeenCalled()
  })
  it('supports older cores exposing the same single-provider node through the normal endpoint', async () => {
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: {
      ...PROXIES, Fast: PROVIDERS.sub.proxies[0]
    } } } } as any)
    const { state } = await render()
    expect(state.proxies.Fast.type).toBe('Trojan')
    await state.testNode('Fast', PROXIES.Proxy)
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledWith('gw', 'Fast', undefined)
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(notify.error).not.toHaveBeenCalled()
  })
  it.each(['default', 'other'])('does not mistake a top-level node for a subscribed node with the same name in %s', async source => {
    const ordinaryNode = { name: 'Fast', type: 'Vless', history: history(15) }
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: {
      ...PROXIES, Fast: ordinaryNode
    } } } } as any)
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: {
      ...PROVIDERS,
      [source]: { name: source, type: 'Proxy', vehicleType: source === 'default' ? 'Compatible' : 'HTTP', proxies: [ordinaryNode] }
    } } } } as any)
    const { state } = await render()
    await state.testNode('Fast', PROXIES.Proxy)
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(notify.error).toHaveBeenCalledWith('Fast 测速失败', '多个来源中存在同名节点，请使用所属策略组的分组测速。')
    expect(state.nodeTesting.size).toBe(0)
    expect(state.delays.size).toBe(0)
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
  })
  it('does not guess the provider for duplicate node names and leaves group tests available', async () => {
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: {
      ...PROVIDERS, other: { ...PROVIDERS.sub, name: 'other' }
    } } } } as any)
    vi.mocked(mihomoPanelApi.groupDelay).mockResolvedValue({ data: { data: { Fast: 70 } } } as any)
    const { state } = await render()
    await state.testNode('Fast', PROXIES.Proxy)
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(notify.error).toHaveBeenCalledWith('Fast 测速失败', '多个来源中存在同名节点，请使用所属策略组的分组测速。')
    expect(state.nodeTesting.size).toBe(0)
    await state.testGroup(PROXIES.Proxy)
    expect(mihomoPanelApi.groupDelay).toHaveBeenCalledWith('gw', 'Proxy', undefined)
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(70)
  })
  it.each(['Proxy_sub_筛选', 'Proxy_sub_筛选 (2)'])('treats the ConfigFlow filter copy %s as the same source as its provider', async copy => {
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: {
      ...PROVIDERS, [copy]: { ...PROVIDERS.sub, name: copy }
    } } } } as any)
    const { state, wrapper } = await render()
    await state.testNode('Fast', PROXIES.Proxy)
    expect(mihomoPanelApi.providerProxyDelay).toHaveBeenCalledWith('gw', 'sub', 'Fast', undefined)
    expect(notify.error).not.toHaveBeenCalled()
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(123)
    state.applySettings({ ...state.settings, groupByProvider: true })
    await flushPromises()
    expect(wrapper.text()).not.toContain('多个代理集合')
  })
  it('does not merge a provider that only resembles a filter copy of another', async () => {
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: {
      ...PROVIDERS, Proxy_other_筛选: { ...PROVIDERS.sub, name: 'Proxy_other_筛选' }
    } } } } as any)
    const { state } = await render()
    await state.testNode('Fast', PROXIES.Proxy)
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(notify.error).toHaveBeenCalledWith('Fast 测速失败', '多个来源中存在同名节点，请使用所属策略组的分组测速。')
  })
  it('tests an ambiguous group exit along the group itself', async () => {
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: {
      ...PROVIDERS, other: { ...PROVIDERS.sub, name: 'other' }
    } } } } as any)
    const { state } = await render()
    await state.testNode('Auto', PROXIES.Proxy)
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledWith('gw', 'Auto', undefined)
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(notify.error).not.toHaveBeenCalled()
    expect(state.delayOf('Auto', PROXIES.Proxy)).toBe(123)
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
  })
  it('supports the default Compatible provider without treating repeated entries as ambiguity', async () => {
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: {
      default: { name: 'default', type: 'Proxy', vehicleType: 'Compatible', proxies: [
        { name: 'Fast', type: 'Trojan' }, { name: 'Fast', type: 'Trojan' }, PROXIES.Slow
      ] }
    } } } } as any)
    const { state, wrapper } = await render()
    await state.testNode('Fast', PROXIES.Proxy)
    expect(mihomoPanelApi.providerProxyDelay).toHaveBeenCalledWith('gw', 'default', 'Fast', undefined)
    await state.testNode('Slow', PROXIES.Proxy)
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledWith('gw', 'Slow', undefined)
    expect(wrapper.text()).not.toContain('代理集合')
  })
  it('counts Compatible wrappers of multiple groups as the same ordinary configuration source', async () => {
    const node = { ...PROXIES.Slow, id: 'same-proxy-id', 'provider-name': '' }
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: {
      default: { name: 'default', type: 'Proxy', vehicleType: 'Compatible', proxies: [node] },
      TestGroup: { name: 'TestGroup', type: 'Proxy', vehicleType: 'Compatible', proxies: [node] },
      SecondGroup: { name: 'SecondGroup', type: 'Proxy', vehicleType: 'Compatible', proxies: [node] }
    } } } } as any)
    const { state, wrapper } = await render()
    state.applySettings({ ...state.settings, groupByProvider: true })
    await flushPromises()
    expect(wrapper.text()).toContain('配置节点')
    expect(wrapper.text()).not.toContain('多个代理集合')
    await state.testNode('Slow', PROXIES.Proxy)
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledWith('gw', 'Slow', undefined)
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(notify.error).not.toHaveBeenCalled()
  })
  it.each(['HTTP', 'File', 'Inline'])('still detects an external %s node conflicting with multiple Compatible wrappers', async vehicleType => {
    const node = { ...PROXIES.Slow, id: 'same-proxy-id', 'provider-name': '' }
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: {
      default: { name: 'default', type: 'Proxy', vehicleType: 'Compatible', proxies: [node] },
      TestGroup: { name: 'TestGroup', type: 'Proxy', vehicleType: 'Compatible', proxies: [node] },
      SecondGroup: { name: 'SecondGroup', type: 'Proxy', vehicleType: 'Compatible', proxies: [node] },
      sub: { name: 'sub', type: 'Proxy', vehicleType, proxies: [{ ...node, id: 'different-proxy-id', 'provider-name': 'sub' }] }
    } } } } as any)
    const { state } = await render()
    await state.testNode('Slow', PROXIES.Proxy)
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    expect(notify.error).toHaveBeenCalledWith('Slow 测速失败', '多个来源中存在同名节点，请使用所属策略组的分组测速。')
    expect(state.nodeTesting.size).toBe(0)
  })
  it('preserves provider and node names and the group URL, without switching nodes or testing whole providers', async () => {
    const name = '🇯🇵 JP-05 · AWS2 / 中文 %?#'
    const provider = '订阅 / 🇯🇵 100%?#'
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: {
      [provider]: { name: provider, type: 'Proxy', vehicleType: 'HTTP', proxies: [{ name, type: 'Trojan' }] }
    } } } } as any)
    const { state } = await render()
    const testUrl = 'https://example.test/generate_204?key=a%20b&check=1'
    await state.testNode(name, { ...PROXIES.Proxy, testUrl })
    expect(mihomoPanelApi.providerProxyDelay).toHaveBeenCalledWith('gw', provider, name, testUrl)
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.groupDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
    expect(state.delayOf(name, { ...PROXIES.Proxy, testUrl })).toBe(123)
  })
  it('retains ordinary-node tests when the providers request fails', async () => {
    vi.mocked(mihomoPanelApi.proxyProviders).mockRejectedValue(new Error('unavailable'))
    const { state } = await render()
    await state.testNode('Slow', PROXIES.Proxy)
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledWith('gw', 'Slow', undefined)
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
  })
  it.each([404, 426, 504])('reports provider test HTTP %s errors and clears the pending state', async status => {
    vi.mocked(mihomoPanelApi.providerProxyDelay).mockRejectedValue({ response: { status, data: { message: `failure-${status}` } } })
    const { state } = await render()
    await state.testNode('Fast', PROXIES.Proxy)
    expect(notify.error).toHaveBeenCalledWith('Fast 测速失败', `failure-${status}`)
    expect(state.nodeTesting.size).toBe(0)
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(40)
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
    expect(mihomoPanelApi.groupDelay).not.toHaveBeenCalled()
  })
  it.each([undefined, -1, '20', Number.NaN, Number.POSITIVE_INFINITY])('rejects an invalid provider delay %s', async delay => {
    vi.mocked(mihomoPanelApi.providerProxyDelay).mockResolvedValue({ data: { data: { delay } } } as any)
    const { state } = await render()
    await state.testNode('Fast', PROXIES.Proxy)
    expect(notify.error).toHaveBeenCalledWith('Fast 测速失败', '测速结果无效，请重试。')
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(40)
  })
  it.each(['reload', 'agent', 'unmount'])('discards provider results after %s', async change => {
    let resolve!: (value: any) => void
    vi.mocked(mihomoPanelApi.providerProxyDelay).mockImplementationOnce(() => new Promise(yes => { resolve = yes }))
    const { state, wrapper } = await render()
    const pending = state.testNode('Fast', PROXIES.Proxy)
    if (change === 'reload') await wrapper.setProps({ reloadToken: 1 })
    else if (change === 'agent') await wrapper.setProps({ agentId: 'other' })
    else wrapper.unmount()
    resolve({ data: { data: { delay: 999 } } }); await pending
    expect(state.delays.size).toBe(0)
    expect(notify.error).not.toHaveBeenCalled()
  })
  it('does not let an old result clear a newer pending node test after reload', async () => {
    let resolveOld!: (value: any) => void
    let resolveNew!: (value: any) => void
    vi.mocked(mihomoPanelApi.providerProxyDelay)
      .mockImplementationOnce(() => new Promise(yes => { resolveOld = yes }))
      .mockImplementationOnce(() => new Promise(yes => { resolveNew = yes }))
    const { state, wrapper } = await render()
    const oldTest = state.testNode('Fast', PROXIES.Proxy)
    await wrapper.setProps({ reloadToken: 1 }); await flushPromises()
    const newTest = state.testNode('Fast', PROXIES.Proxy)
    resolveOld({ data: { data: { delay: 999 } } }); await oldTest
    expect(state.nodeTesting.size).toBe(1)
    expect(state.delays.size).toBe(0)
    resolveNew({ data: { data: { delay: 25 } } }); await newTest
    expect(state.nodeTesting.size).toBe(0)
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(25)
  })
  it('discards provider failures after reload without showing a stale toast', async () => {
    let reject!: (reason: any) => void
    vi.mocked(mihomoPanelApi.providerProxyDelay).mockImplementationOnce(() => new Promise((_yes, no) => { reject = no }))
    const { state, wrapper } = await render()
    const pending = state.testNode('Fast', PROXIES.Proxy)
    await wrapper.setProps({ reloadToken: 1 })
    reject(new Error('old failure')); await pending
    expect(notify.error).not.toHaveBeenCalled()
    expect(state.nodeTesting.size).toBe(0)
  })
  it('tests hidden and visible groups sequentially and continues after failure', async () => {
    let running = 0, peak = 0
    vi.mocked(mihomoPanelApi.groupDelay).mockImplementation(async (_id, name) => {
      peak = Math.max(peak, ++running); await Promise.resolve(); running--
      if (name === 'Auto') throw new Error('failed')
      return { data: { data: { Fast: 10 } } } as any
    })
    const { state } = await render()
    state.search = 'Slow'
    await state.testAll()
    expect(vi.mocked(mihomoPanelApi.groupDelay).mock.calls.map(call => call[1]).sort()).toEqual(['Auto', 'GLOBAL', 'Hidden', 'Proxy'])
    expect(peak).toBe(1); expect(state.batchDone).toBe(4); expect(state.batchRunning).toBe(false)
    expect(notify.info).toHaveBeenCalledWith('全部分组测速完成', '已测 4 组，1 组失败')
  })
  it('stops queued tests after stop and unmount', async () => {
    let resolve!: (value: any) => void
    vi.mocked(mihomoPanelApi.groupDelay).mockImplementation(() => new Promise(yes => { resolve = yes }))
    const { state, wrapper } = await render()
    const batch = state.testAll(); await flushPromises(); state.batchStop = true
    resolve({ data: { data: {} } }); await batch
    expect(mihomoPanelApi.groupDelay).toHaveBeenCalledTimes(1)
    const second = state.testAll(); wrapper.unmount()
    resolve({ data: { data: {} } }); await second
    expect(mihomoPanelApi.groupDelay).toHaveBeenCalledTimes(2)
  })
  it('discards old results when changing Agent', async () => {
    let resolve!: (value: any) => void
    vi.mocked(mihomoPanelApi.groupDelay).mockImplementationOnce(() => new Promise(yes => { resolve = yes }))
    const { state, wrapper } = await render()
    const batch = state.testAll()
    await wrapper.setProps({ agentId: 'other' })
    resolve({ data: { data: { Fast: 999 } } }); await batch
    expect(mihomoPanelApi.groupDelay).toHaveBeenCalledTimes(1)
    expect(state.batchRunning).toBe(false); expect(state.delays.size).toBe(0)
  })
  it('stops old queued group tests after a panel reload and permits new tests', async () => {
    let resolve!: (value: any) => void
    vi.mocked(mihomoPanelApi.groupDelay).mockImplementationOnce(() => new Promise(yes => { resolve = yes }))
    const { state, wrapper } = await render()
    const batch = state.testAll()
    await wrapper.setProps({ reloadToken: 1 }); await flushPromises()
    resolve({ data: { data: { Fast: 999 } } }); await batch
    expect(mihomoPanelApi.groupDelay).toHaveBeenCalledTimes(1)
    expect(state.batchRunning).toBe(false)
    expect(state.testing.size).toBe(0)
    expect(state.delays.size).toBe(0)
    expect(notify.info).not.toHaveBeenCalled()
    await state.testNode('Fast', PROXIES.Proxy)
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(123)
  })
})

describe('policy groups before the provider list', () => {
  const providerReply = (providers = PROVIDERS) => ({ data: { data: { providers } } } as any)
  function deferProviders() {
    let resolve!: (value: any) => void
    vi.mocked(mihomoPanelApi.proxyProviders).mockImplementation(() => new Promise(yes => { resolve = yes }))
    return (providers = PROVIDERS) => resolve(providerReply(providers))
  }
  const fastCard = (wrapper: VueWrapper) => wrapper.findAll('button[aria-pressed]').find(button => button.find('span').text() === 'Fast')!

  it('shows the groups from /proxies and fills subscription nodes in when the providers arrive', async () => {
    const finish = deferProviders()
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('Proxy')
    expect(memberNames(wrapper)).toEqual(['Slow', 'Dead', 'Auto', 'Fast'])
    expect(fastCard(wrapper).text()).toContain('—')
    finish(); await flushPromises()
    expect(fastCard(wrapper).text()).toContain('Trojan')
    expect(wrapper.text()).toContain('代理集合')
  })

  it('keeps the previous subscription nodes while a reload reads the providers again', async () => {
    const { wrapper } = await render()
    expect(fastCard(wrapper).text()).toContain('Trojan')
    const finish = deferProviders()
    await wrapper.setProps({ reloadToken: 1 }); await flushPromises()
    expect(fastCard(wrapper).text()).toContain('Trojan')
    finish({ sub: { ...PROVIDERS.sub, proxies: [{ name: 'Fast', type: 'Vless', history: history(55) }] } }); await flushPromises()
    expect(fastCard(wrapper).text()).toContain('Vless')
  })

  it('waits for the providers before testing a subscription node', async () => {
    const finish = deferProviders()
    const { state } = await render()
    const test = state.testNode('Fast', PROXIES.Proxy)
    await flushPromises()
    expect(mihomoPanelApi.providerProxyDelay).not.toHaveBeenCalled()
    finish(); await test
    expect(mihomoPanelApi.providerProxyDelay).toHaveBeenCalledWith('gw', 'sub', 'Fast', undefined)
    expect(notify.error).not.toHaveBeenCalled()
  })

  it('takes only names from Compatible providers', async () => {
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue(providerReply({
      ...PROVIDERS, Proxy: { name: 'Proxy', type: 'Proxy', vehicleType: 'Compatible', proxies: [{ name: 'Fast' }, { name: 'Slow' }] },
    } as any))
    const { wrapper, state } = await render()
    expect(fastCard(wrapper).text()).toContain('Trojan')
    expect(state.delayOf('Fast', PROXIES.Proxy)).toBe(40)
    expect(state.sourcesOf('Slow')).toEqual(['default'])
  })
})
