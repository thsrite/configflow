import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import MihomoConnectionProxy from '@/components/mihomo/MihomoConnectionProxy.vue'
import { mihomoPanelApi } from '@/api'
import { notify } from '@/lib/feedback'
import { cachedPanelData, rememberPanelData } from '@/lib/mihomoPanelCache'
import { DEFAULT_PROXY_SETTINGS, saveProxySettings, type ConnectionRow, type MihomoConnection, type MihomoProvider, type MihomoProxy } from '@/types/mihomo'

vi.mock('@/api', () => ({
  mihomoPanelApi: {
    proxies: vi.fn(), proxyProviders: vi.fn(), selectProxy: vi.fn(), unfixProxy: vi.fn(),
    connections: vi.fn(), closeConnection: vi.fn(), proxyDelay: vi.fn(), providerProxyDelay: vi.fn(), groupDelay: vi.fn()
  }
}))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() } }))

const ROOT = '入口策略'
const REGION = '地区策略'
const NODES = '节点分组'
const OLD = '原节点'
const FAST = '香港 节点 / 一'
const AUTO = '自动选择'
let proxies: Record<string, MihomoProxy>
let providers: Record<string, MihomoProvider>
const wrappers: VueWrapper[] = []

function result<T>(data: T) { return { data: { success: true, data } } as any }
function stubStorage() {
  const values = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    get length() { return values.size },
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, String(value)) },
    removeItem: (key: string) => { values.delete(key) },
    clear: () => { values.clear() },
    key: (index: number) => [...values.keys()][index] ?? null
  } satisfies Storage)
}
function row(chains = [OLD, NODES, REGION, ROOT]): ConnectionRow {
  const raw: MihomoConnection = {
    id: 'connection-1', upload: 10, download: 20, start: '2026-10-27T01:00:00Z',
    chains: [...chains], rule: 'RuleSet', rulePayload: 'example',
    metadata: { network: 'tcp', type: 'TUN', sourceIP: '192.0.2.10', sourcePort: '12345', destinationIP: '1.1.1.1', destinationPort: '443', host: 'example.com' }
  }
  return { ...raw, chains: [...chains], raw, target: 'example.com:443', chain: [...chains].reverse().join(' → '), upSpeed: 0, downSpeed: 0 }
}
async function render(connection = row()) {
  const wrapper = mount(MihomoConnectionProxy, { props: { agentId: 'gateway', row: connection }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
function button(wrapper: VueWrapper, label: string) {
  const found = wrapper.findAll('button').find(item => item.attributes('aria-label') === label)
  expect(found, `button ${label}`).toBeDefined()
  return found!
}
async function browse(wrapper: VueWrapper, name: string) {
  await button(wrapper, `查看 ${name}`).trigger('click')
  await flushPromises()
}
async function choose(wrapper: VueWrapper, name: string) {
  await button(wrapper, `选择 ${name}`).trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.resetAllMocks()
  stubStorage()
  proxies = {
    [ROOT]: { name: ROOT, type: 'Selector', all: [REGION, 'DIRECT'], now: REGION },
    [REGION]: { name: REGION, type: 'Selector', all: [NODES, 'Core'], now: NODES },
    [NODES]: { name: NODES, type: 'Selector', all: [OLD, FAST, AUTO], now: OLD },
    [AUTO]: { name: AUTO, type: 'URLTest', all: [OLD, FAST], now: OLD },
    Core: { name: 'Core', type: 'Trojan', history: [{ time: '', delay: 88 }] },
    DIRECT: { name: 'DIRECT', type: 'Direct' }
  }
  providers = {
    sub: { name: 'sub', type: 'Proxy', vehicleType: 'HTTP', proxies: [
      { name: OLD, type: 'AnyTLS', udp: true, history: [{ time: '', delay: 180 }] },
      { name: FAST, type: 'AnyTLS', udp: true, history: [{ time: '', delay: 42 }] }
    ] }
  }
  vi.mocked(mihomoPanelApi.proxies).mockImplementation(async () => result({ proxies: structuredClone(proxies) }))
  vi.mocked(mihomoPanelApi.proxyProviders).mockImplementation(async () => result({ providers: structuredClone(providers) }))
  vi.mocked(mihomoPanelApi.connections).mockResolvedValue(result({ connections: [] }))
  vi.mocked(mihomoPanelApi.closeConnection).mockResolvedValue(result({}))
  vi.mocked(mihomoPanelApi.selectProxy).mockImplementation(async (_agent, group, name) => {
    const current = proxies[group]
    if (current) proxies[group] = { ...current, now: name, fixed: current.type === 'Selector' ? current.fixed : name }
    return result({})
  })
  vi.mocked(mihomoPanelApi.unfixProxy).mockImplementation(async (_agent, group) => {
    if (proxies[group]) proxies[group] = { ...proxies[group], fixed: undefined }
    return result({})
  })
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.unstubAllGlobals() })

describe('connection proxy navigation and selection', () => {
  it('browses each level and nested candidate groups without switching a policy', async () => {
    const wrapper = await render()
    expect(mihomoPanelApi.proxies).toHaveBeenCalledWith('gateway')
    expect(mihomoPanelApi.proxyProviders).toHaveBeenCalledWith('gateway')
    expect(button(wrapper, `选择 ${REGION}`).attributes('aria-pressed')).toBe('true')
    await browse(wrapper, REGION)
    expect(button(wrapper, `选择 ${NODES}`).attributes('aria-pressed')).toBe('true')
    await browse(wrapper, NODES)
    expect(button(wrapper, `选择 ${OLD}`).attributes('aria-pressed')).toBe('true')
    await button(wrapper, `查看下级 ${AUTO}`).trigger('click')
    await flushPromises()
    expect(button(wrapper, `选择 ${FAST}`).exists()).toBe(true)
    await browse(wrapper, ROOT)
    expect(button(wrapper, `选择 ${REGION}`).exists()).toBe(true)
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
    expect(mihomoPanelApi.connections).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
  })

  it('uses current group selections for navigation without replacing the recorded connection chain', async () => {
    proxies[ROOT] = { ...proxies[ROOT], all: [REGION, AUTO], now: AUTO }
    const connection = row()
    const original = structuredClone(connection)
    const wrapper = await render(connection)
    expect(button(wrapper, `查看 ${AUTO}`).exists()).toBe(true)
    expect(button(wrapper, `选择 ${AUTO}`).attributes('aria-pressed')).toBe('true')
    await browse(wrapper, AUTO)
    expect(button(wrapper, `选择 ${OLD}`).exists()).toBe(true)
    expect(connection).toEqual(original)
  })

  it('selects a provider-only leaf in its own deep group and retains the original connection data', async () => {
    const connection = row()
    const original = structuredClone(connection)
    const wrapper = await render(connection)
    await browse(wrapper, NODES)
    expect(wrapper.text()).toContain('AnyTLS')
    expect(wrapper.text()).toContain('42 ms')
    await choose(wrapper, FAST)
    expect(mihomoPanelApi.selectProxy).toHaveBeenCalledExactlyOnceWith('gateway', NODES, FAST)
    expect(button(wrapper, `选择 ${FAST}`).attributes('aria-pressed')).toBe('true')
    expect(button(wrapper, `选择 ${OLD}`).attributes('aria-pressed')).toBe('false')
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(connection).toEqual(original)
  })

  it('uses authoritative core group data when a provider contains a stale duplicate', async () => {
    providers.sub.proxies!.push({ name: ROOT, type: 'Selector', all: ['DIRECT'], now: 'DIRECT' })
    const wrapper = await render()
    expect(button(wrapper, `选择 ${REGION}`).attributes('aria-pressed')).toBe('true')
    expect(button(wrapper, '选择 DIRECT').attributes('aria-pressed')).toBe('false')
  })

  it('keeps the original selection highlighted when a switch fails', async () => {
    vi.mocked(mihomoPanelApi.selectProxy).mockRejectedValueOnce(new Error('device unavailable'))
    const wrapper = await render()
    await browse(wrapper, NODES)
    await choose(wrapper, FAST)
    expect(button(wrapper, `选择 ${OLD}`).attributes('aria-pressed')).toBe('true')
    expect(button(wrapper, `选择 ${FAST}`).attributes('aria-pressed')).toBe('false')
    expect(notify.error).toHaveBeenCalled()
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(mihomoPanelApi.connections).not.toHaveBeenCalled()
  })

  it.each(['URLTest', 'Fallback'])('can fix a %s choice and restore its automatic selection', async type => {
    proxies[ROOT].now = AUTO
    proxies[ROOT].all!.push(AUTO)
    proxies[AUTO].type = type
    const wrapper = await render()
    await browse(wrapper, AUTO)
    await choose(wrapper, FAST)
    expect(mihomoPanelApi.selectProxy).toHaveBeenCalledExactlyOnceWith('gateway', AUTO, FAST)
    expect(wrapper.text()).toContain('已固定')
    const restore = wrapper.findAll('button').find(item => item.text() === '恢复自动')
    expect(restore).toBeDefined()
    await restore!.trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.unfixProxy).toHaveBeenCalledExactlyOnceWith('gateway', AUTO)
    expect(wrapper.text()).not.toContain('已固定')
  })

  it.each(['URLTest', 'Fallback'])('fixes the current automatic %s choice without disconnecting existing sessions', async type => {
    proxies[ROOT].now = AUTO
    proxies[ROOT].all!.push(AUTO)
    proxies[AUTO].type = type
    const wrapper = await render()
    await browse(wrapper, AUTO)
    expect(button(wrapper, `选择 ${OLD}`).attributes('aria-pressed')).toBe('true')
    await choose(wrapper, OLD)
    expect(mihomoPanelApi.selectProxy).toHaveBeenCalledExactlyOnceWith('gateway', AUTO, OLD)
    expect(wrapper.text()).toContain('已固定')
    expect(mihomoPanelApi.connections).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it.each(['LoadBalance', 'Relay'])('does not offer manual switching for a %s group', async type => {
    proxies[ROOT] = { ...proxies[ROOT], type, now: undefined }
    const wrapper = await render()
    const select = wrapper.findAll('button').find(item => item.attributes('aria-label') === `选择 ${REGION}`)
    if (select) {
      expect(select.attributes('disabled')).toBeDefined()
      ;(select.element as HTMLButtonElement).click()
    }
    await flushPromises()
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
    await button(wrapper, `查看下级 ${REGION}`).trigger('click')
    await flushPromises()
    expect(button(wrapper, `选择 ${NODES}`).exists()).toBe(true)
  })
})

describe('connection switching scope and stale requests', () => {
  it('retains the browsed level when polling replaces the same connection row', async () => {
    const connection = row()
    const wrapper = await render(connection)
    await browse(wrapper, NODES)
    const reads = vi.mocked(mihomoPanelApi.proxies).mock.calls.length
    await wrapper.setProps({ row: { ...connection, download: 2048, downSpeed: 512 } })
    await flushPromises()
    expect(button(wrapper, `选择 ${OLD}`).attributes('aria-pressed')).toBe('true')
    expect(button(wrapper, `选择 ${FAST}`).exists()).toBe(true)
    expect(mihomoPanelApi.proxies).toHaveBeenCalledTimes(reads)
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
  })

  it('finishes a pending switch when polling updates the same connection ID', async () => {
    let finish!: (value: any) => void
    vi.mocked(mihomoPanelApi.selectProxy).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(result({ connections: [
      { id: 'reconnect-me', chains: [OLD, NODES, REGION, ROOT] }
    ] }))
    const connection = row()
    const wrapper = await render(connection)
    await browse(wrapper, NODES)
    await button(wrapper, `选择 ${FAST}`).trigger('click')
    await flushPromises()
    await wrapper.setProps({ row: { ...connection, download: 4096, downSpeed: 1024 } })
    await flushPromises()
    finish(result({}))
    await flushPromises()
    expect(button(wrapper, `选择 ${FAST}`).attributes('aria-pressed')).toBe('true')
    expect(mihomoPanelApi.connections).toHaveBeenCalledExactlyOnceWith('gateway')
    expect(mihomoPanelApi.closeConnection).toHaveBeenCalledExactlyOnceWith('gateway', 'reconnect-me')
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('closes only connections whose recorded chains contain the switched group', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(result({ connections: [
      { id: 'same-a', chains: [OLD, NODES, REGION, ROOT] },
      { id: 'same-b', chains: [OLD, NODES, 'Other Root'] },
      { id: 'other', chains: ['Core', REGION, ROOT] }
    ] }))
    const wrapper = await render()
    await browse(wrapper, NODES)
    await choose(wrapper, FAST)
    expect(mihomoPanelApi.connections).toHaveBeenCalledWith('gateway')
    expect(vi.mocked(mihomoPanelApi.closeConnection).mock.calls).toEqual([
      ['gateway', 'same-a'], ['gateway', 'same-b']
    ])
  })

  it('respects the setting that keeps existing connections open', async () => {
    saveProxySettings({ ...DEFAULT_PROXY_SETTINGS, closeConnectionsOnSwitch: false })
    const wrapper = await render()
    await browse(wrapper, NODES)
    await choose(wrapper, FAST)
    expect(mihomoPanelApi.selectProxy).toHaveBeenCalledExactlyOnceWith('gateway', NODES, FAST)
    expect(mihomoPanelApi.connections).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('does not act on the new device when an old-device switch finishes late', async () => {
    let finish!: (value: any) => void
    vi.mocked(mihomoPanelApi.selectProxy).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = await render()
    await browse(wrapper, NODES)
    await button(wrapper, `选择 ${FAST}`).trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.selectProxy).toHaveBeenCalledExactlyOnceWith('gateway', NODES, FAST)
    await wrapper.setProps({ agentId: 'other-gateway' })
    await flushPromises()
    finish(result({}))
    await flushPromises()
    expect(mihomoPanelApi.connections).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
    expect(wrapper.emitted('changed')).toBeUndefined()
    await browse(wrapper, NODES)
    expect(button(wrapper, `选择 ${OLD}`).attributes('aria-pressed')).toBe('true')
  })

  it('does not perform follow-up actions after the detail component is unmounted', async () => {
    let finish!: (value: any) => void
    vi.mocked(mihomoPanelApi.selectProxy).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = await render()
    await browse(wrapper, NODES)
    await button(wrapper, `选择 ${FAST}`).trigger('click')
    await flushPromises()
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    finish(result({}))
    await flushPromises()
    expect(mihomoPanelApi.connections).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
  })
})

describe('incomplete proxy data', () => {
  it('still displays core groups when provider metadata cannot be loaded and explains the partial failure', async () => {
    vi.mocked(mihomoPanelApi.proxyProviders).mockRejectedValue(new Error('subscription unavailable'))
    const wrapper = await render()
    expect(button(wrapper, `选择 ${REGION}`).exists()).toBe(true)
    expect(wrapper.text()).toMatch(/代理集合|订阅/)
    expect(wrapper.text()).toMatch(/失败|不可用|无法|未能/)
    await browse(wrapper, NODES)
    expect(wrapper.text()).toContain(FAST)
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
  })

  it('terminates a cyclic group path without repeatedly appending breadcrumbs', async () => {
    proxies[REGION] = { ...proxies[REGION], all: [ROOT], now: ROOT }
    const wrapper = await render()
    const navigation = wrapper.findAll('button').filter(item => item.attributes('aria-label')?.startsWith('查看 '))
    expect(navigation.length).toBeGreaterThan(0)
    expect(navigation.length).toBeLessThanOrEqual(2)
    await browse(wrapper, REGION)
    expect(button(wrapper, `选择 ${ROOT}`).exists()).toBe(true)
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
  })

  it('keeps a removed chain entry readable without inventing a selectable group', async () => {
    const wrapper = await render(row(['DIRECT', '已删除策略组']))
    expect(wrapper.text()).toContain('已删除策略组')
    const enabledChoices = wrapper.findAll('button').filter(item => item.attributes('aria-label')?.startsWith('选择 ') && item.attributes('disabled') === undefined)
    expect(enabledChoices).toHaveLength(0)
    expect(mihomoPanelApi.selectProxy).not.toHaveBeenCalled()
  })
})

describe('opening again with the last reply', () => {
  it('shows the cached path and members at once, but switches only after the fresh read', async () => {
    rememberPanelData('gateway', 'proxies', { proxies: structuredClone(proxies) })
    rememberPanelData('gateway', 'providers/proxies', { providers: structuredClone(providers) })
    let answer!: () => void
    vi.mocked(mihomoPanelApi.proxies).mockImplementation(() => new Promise(yes => { answer = () => yes(result({ proxies: structuredClone(proxies) })) }))
    const wrapper = mount(MihomoConnectionProxy, { props: { agentId: 'gateway', row: row() }, attachTo: document.body })
    wrappers.push(wrapper)
    await nextTick()
    expect(wrapper.find('nav[aria-label="代理路径"]').text()).toContain(OLD)
    expect(button(wrapper, `选择 ${REGION}`).attributes('disabled')).toBeDefined()
    answer(); await flushPromises()
    expect(button(wrapper, `选择 ${REGION}`).attributes('disabled')).toBeUndefined()
  })

  it('writes a switch into the cache', async () => {
    rememberPanelData('gateway', 'proxies', { proxies: structuredClone(proxies) })
    const wrapper = await render()
    await browse(wrapper, NODES)
    await choose(wrapper, FAST)
    expect(cachedPanelData<any>('gateway', 'proxies').proxies[NODES].now).toBe(FAST)
  })
})
