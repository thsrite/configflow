import { beforeEach, describe, expect, it } from 'vitest'
import { delayText, delayTone, latestDelay, orderedGroups, type MihomoProxy } from '@/types/mihomo'

const proxy = (name: string, type: string, extra: Partial<MihomoProxy> = {}): MihomoProxy => ({ name, type, ...extra })

describe('mihomo panel helpers', () => {
  it('orders groups like the config file and keeps GLOBAL last', () => {
    const proxies = {
      GLOBAL: proxy('GLOBAL', 'Selector', { all: ['DIRECT', 'Media', 'Proxy'] }),
      Proxy: proxy('Proxy', 'Selector'),
      Media: proxy('Media', 'URLTest'),
      Hidden: proxy('Hidden', 'Selector', { hidden: true }),
      HK: proxy('HK', 'Shadowsocks'),
      DIRECT: proxy('DIRECT', 'Direct')
    }
    expect(orderedGroups(proxies).map(group => group.name)).toEqual(['Media', 'Proxy', 'GLOBAL'])
  })

  it('prefers the history of the group test URL', () => {
    const node = proxy('HK', 'Shadowsocks', {
      history: [{ time: '', delay: 120 }],
      extra: { 'https://example.test': { history: [{ time: '', delay: 0 }] } }
    })
    expect(latestDelay(node)).toBe(120)
    expect(latestDelay(node, 'https://example.test')).toBe(0)
    expect(latestDelay(node, 'https://other.test')).toBe(120)
  })

  it('labels delays by speed and treats zero as a timeout', () => {
    expect([undefined, 0, 100, 500, 1200].map(delayTone)).toEqual(['muted', 'danger', 'success', 'warning', 'danger'])
    expect(delayText(0)).toBe('超时')
    expect(delayText(88)).toBe('88 ms')
  })
})

import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { vi } from 'vitest'
import MihomoPanel from '@/views/MihomoPanel.vue'
import MihomoConnections from '@/components/mihomo/MihomoConnections.vue'
import MihomoOverview from '@/components/mihomo/MihomoOverview.vue'
import MihomoStatsTab from '@/components/mihomo/MihomoStatsTab.vue'
import MihomoProxies from '@/components/mihomo/MihomoProxies.vue'
import MihomoRules from '@/components/mihomo/MihomoRules.vue'
import MihomoLogs from '@/components/mihomo/MihomoLogs.vue'
import { agentApi, mihomoPanelApi } from '@/api'
import { invalidateAgentSummaries } from '@/lib/agentSummaries'

const routeState = vi.hoisted(() => ({ route: null as any, replace: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => routeState.route, useRouter: () => ({ replace: routeState.replace, push: vi.fn() }) }))
vi.mock('@/api', () => {
  const ok = (data: unknown) => vi.fn().mockResolvedValue({ data: { success: true, data } })
  return {
    clientToolsApi: { notes: vi.fn().mockResolvedValue({ data: { success: true, notes: {} } }) },
    trafficHistoryApi: { get: vi.fn().mockResolvedValue({ data: { success: true, data: { enabled: false, since: null, supported: true, min_version: '1.8.51-go', retention_days: 90, retention_choices: [30, 90] } } }) },
    agentApi: {
      getAll: vi.fn(), summaries: vi.fn(),
      getLogSources: vi.fn().mockResolvedValue({ data: { success: true, sources: [{ id: 'core', label: 'Mihomo 日志', kind: 'journal', unit: 'mihomo' }] } }),
      getLogs: vi.fn().mockResolvedValue({ data: { success: true, logs: 'Mihomo log entry' } })
    },
    mihomoPanelApi: {
      groupDelay: vi.fn(),
      proxyDelay: vi.fn().mockResolvedValue({ data: { success: true, data: { delay: 120 } } }),
      flow: vi.fn().mockResolvedValue({ data: { success: true, data: { interval: 0, connections: 0, active: 0, up: 0, down: 0, paths: [], omitted: 0 } } }),
      version: ok({ version: 'v1.19.0' }),
      configs: ok({ mode: 'Rule' }),
      proxies: ok({ proxies: {
        GLOBAL: { name: 'GLOBAL', type: 'Selector', all: ['Proxy'], now: 'Proxy' },
        Proxy: { name: 'Proxy', type: 'Selector', all: ['HK', 'DIRECT', 'SH'], now: 'HK' },
        HK: { name: 'HK', type: 'Shadowsocks', history: [{ time: '', delay: 88 }] },
        DIRECT: { name: 'DIRECT', type: 'Direct' }
      } }),
      proxyProviders: ok({ providers: { sh: { name: 'sh', type: 'Proxy', vehicleType: 'HTTP', proxies: [{ name: 'SH', type: 'Trojan', history: [{ time: '', delay: 42 }] }] } } }),
      connections: ok({ uploadTotal: 2048, downloadTotal: 4096, connections: [{
        id: 'c1', upload: 10, download: 20, start: new Date().toISOString(), chains: ['HK', 'Proxy'], rule: 'DomainSuffix', rulePayload: 'example.com',
        metadata: { network: 'tcp', type: 'HTTP', sourceIP: '192.0.2.10', sourcePort: '5000', destinationIP: '', destinationPort: '443', host: 'www.example.com' }
      }] }),
      rules: ok({ rules: [{ type: 'DomainSuffix', payload: 'example.com', proxy: 'Proxy' }, { type: 'Match', payload: '', proxy: 'DIRECT' }] }),
      ruleProviders: ok({ providers: { ads: { name: 'ads', type: 'Rule', vehicleType: 'HTTP', behavior: 'Domain', format: 'MrsRule', ruleCount: 12 } } })
    }
  }
})

vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })

describe('mihomo panel view', () => {
  beforeEach(() => invalidateAgentSummaries())
  it('picks a mihomo agent and renders groups with the current node', async () => {
    routeState.route = reactive({ query: { tab: 'proxies' } })
    vi.mocked(agentApi.summaries).mockResolvedValue({ data: [
      { id: 'dns', name: 'DNS', service_type: 'oxidns', status: 'online' },
      { id: 'gw', name: 'Gateway', service_type: 'mihomo', status: 'online', host: '192.0.2.1' }
    ] } as any)
    routeState.replace.mockImplementation(({ query }) => { routeState.route.query = query })
    const wrapper = mount(MihomoPanel)
    await flushPromises()
    expect(routeState.replace).toHaveBeenCalledWith({ query: { tab: 'proxies', agent: 'gw' } })
    await flushPromises()
    expect(wrapper.text()).toContain('Mihomo v1.19.0')
    expect(wrapper.text()).toContain('Proxy')
    expect(wrapper.text()).toContain('88 ms')
    // provider 节点不在 /proxies 里，展开后也要有类型和延迟
    await wrapper.findAll('button[aria-expanded]').find(button => button.text().includes('手动选择'))!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Trojan')
    expect(wrapper.text()).toContain('42 ms')
    expect(vi.mocked(mihomoPanelApi.proxies)).toHaveBeenCalledWith('gw')
    wrapper.unmount()
  })

  it('places logs after rules and opens a directly linked logs tab', async () => {
    routeState.route = reactive({ query: { agent: 'gw', tab: 'logs' } })
    vi.mocked(agentApi.summaries).mockResolvedValue({ data: [
      { id: 'gw', name: 'Gateway', service_type: 'mihomo', status: 'online' }
    ] } as any)
    routeState.replace.mockImplementation(({ query }) => { routeState.route.query = query })
    const wrapper = mount(MihomoPanel)
    await flushPromises()
    expect(wrapper.findAll('[role="tab"]').map(tab => tab.text())).toEqual(['概览', '统计', '策略', '连接', '规则', '日志'])
    expect(wrapper.get('[role="tab"][data-state="active"]').text()).toBe('日志')
    expect(wrapper.getComponent(MihomoLogs).props('agentId')).toBe('gw')
    expect(wrapper.text()).toContain('Mihomo log entry')
    wrapper.unmount()
  })

  it('keeps the logs tab available when the core controller cannot be reached', async () => {
    routeState.route = reactive({ query: { agent: 'gw', tab: 'logs' } })
    vi.mocked(agentApi.summaries).mockResolvedValue({ data: [
      { id: 'gw', name: 'Gateway', service_type: 'mihomo', status: 'online' }
    ] } as any)
    vi.mocked(mihomoPanelApi.version).mockRejectedValueOnce(new Error('core unavailable'))
    const wrapper = mount(MihomoPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('无法连接核心')
    expect(wrapper.findComponent(MihomoLogs).exists()).toBe(true)
    expect(wrapper.text()).toContain('Mihomo log entry')
    wrapper.unmount()
  })

  it('keeps visited tabs mounted, reloads hidden ones only when shown and rebuilds on device change', async () => {
    routeState.route = reactive({ query: { agent: 'gw', tab: 'proxies' } })
    vi.mocked(agentApi.summaries).mockResolvedValue({ data: [
      { id: 'gw', name: 'Gateway', service_type: 'mihomo', status: 'online' },
      { id: 'gw2', name: 'Gateway 2', service_type: 'mihomo', status: 'online' }
    ] } as any)
    routeState.replace.mockImplementation(({ query }) => { routeState.route.query = query })
    const { proxies, rules, connections } = vi.mocked(mihomoPanelApi)
    proxies.mockClear(); rules.mockClear(); connections.mockClear()
    const wrapper = mount(MihomoPanel)
    await flushPromises()
    const show = async (tab: string) => { routeState.route.query = { ...routeState.route.query, tab }; await flushPromises() }
    expect(wrapper.findComponent(MihomoRules).exists()).toBe(false)
    expect(wrapper.findComponent(MihomoConnections).exists()).toBe(false)
    await show('rules')
    await show('connections')
    await show('proxies')
    await show('rules')
    expect(proxies).toHaveBeenCalledTimes(1)
    expect(rules).toHaveBeenCalledTimes(1)
    expect(connections).toHaveBeenCalledTimes(1)
    // 是否为当前标签由 Tabs 注入，不经过 props
    const shown = (component: any) => wrapper.getComponent(component).vm.$.setupState.active
    expect([MihomoProxies, MihomoConnections, MihomoRules].map(shown)).toEqual([false, false, true])
    expect(wrapper.get('[role="tabpanel"][data-state="inactive"]').classes()).toContain('data-[state=inactive]:hidden')
    // 刷新只读取当前标签，隐藏的「策略」切回时再读
    await wrapper.get('button[aria-label="刷新面板"]').trigger('click')
    await flushPromises()
    expect(rules).toHaveBeenCalledTimes(2)
    expect(proxies).toHaveBeenCalledTimes(1)
    await show('proxies')
    expect(proxies).toHaveBeenCalledTimes(2)
    await show('rules')
    expect(rules).toHaveBeenCalledTimes(2)
    // 换设备后重建，只挂载当前标签
    routeState.route.query = { agent: 'gw2', tab: 'rules' }
    await flushPromises()
    expect(rules).toHaveBeenLastCalledWith('gw2')
    expect(wrapper.findComponent(MihomoProxies).exists()).toBe(false)
    expect(wrapper.findComponent(MihomoConnections).exists()).toBe(false)
    expect(proxies).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('keeps a batch speed test running while its tab is hidden until the panel is refreshed', async () => {
    routeState.route = reactive({ query: { agent: 'gw', tab: 'proxies' } })
    vi.mocked(agentApi.summaries).mockResolvedValue({ data: [
      { id: 'gw', name: 'Gateway', service_type: 'mihomo', status: 'online' }
    ] } as any)
    routeState.replace.mockImplementation(({ query }) => { routeState.route.query = query })
    const pending: Array<(value: unknown) => void> = []
    vi.mocked(mihomoPanelApi.groupDelay).mockReset().mockImplementation(() => new Promise(resolve => pending.push(resolve)) as any)
    const wrapper = mount(MihomoPanel)
    await flushPromises()
    const show = async (tab: string) => { routeState.route.query = { ...routeState.route.query, tab }; await flushPromises() }
    await wrapper.findAll('button').find(button => button.text().includes('全部分组测速'))!.trigger('click')
    await flushPromises()
    expect(pending).toHaveLength(1)
    // 切走后测速继续，结果写回隐藏的策略页，切回可见
    await show('rules')
    pending.shift()!({ data: { data: { HK: 123 } } })
    await flushPromises()
    expect(pending).toHaveLength(1)
    await show('proxies')
    expect(wrapper.text()).toContain('123 ms')
    expect(wrapper.text()).toContain('正在测速')
    // 刷新面板仍立即停止测速并清空结果
    await wrapper.get('button[aria-label="刷新面板"]').trigger('click')
    await flushPromises()
    pending.shift()!({ data: { data: { Proxy: 99 } } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('123 ms')
    expect(wrapper.text()).not.toContain('正在测速')
    expect(mihomoPanelApi.groupDelay).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('opens the overview by default and drops the tab from the address when it is chosen', async () => {
    routeState.route = reactive({ query: { agent: 'gw', tab: 'rules' } })
    vi.mocked(agentApi.summaries).mockResolvedValue({ data: [{ id: 'gw', name: 'Gateway', service_type: 'mihomo', status: 'online' }] } as any)
    routeState.replace.mockReset().mockImplementation(({ query }) => { routeState.route.query = query })
    const wrapper = mount(MihomoPanel)
    await flushPromises()
    routeState.route.query = { agent: 'gw' }
    await flushPromises()
    expect(wrapper.get('[role="tab"][data-state="active"]').text()).toBe('概览')
    const overview = wrapper.getComponent(MihomoOverview)
    expect(overview.text()).toContain('累计下载')
    // 连接统计在单独的「统计」标签，概览里没有
    expect(overview.text()).not.toContain('连接统计')
    expect(wrapper.findComponent(MihomoStatsTab).exists()).toBe(false)
    // 延迟：百度走直连，其余站点走配置里的第一个策略组
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledWith('gw', 'DIRECT', 'https://www.baidu.com', 5000)
    expect(mihomoPanelApi.proxyDelay).toHaveBeenCalledWith('gw', 'Proxy', 'https://www.google.com/generate_204', 5000)
    // 内核没有 extra 字段时说明需要新版内核
    expect(overview.text()).toContain('内核没有提供命中次数')
    await wrapper.findAll('[role="tab"]').find(tab => tab.text() === '规则')!.trigger('mousedown', { button: 0 })
    await flushPromises()
    expect(routeState.replace).toHaveBeenLastCalledWith({ query: { agent: 'gw', tab: 'rules' } })
    routeState.route.query = { agent: 'gw', tab: 'stats' }
    await flushPromises()
    expect(wrapper.getComponent(MihomoStatsTab).text()).toContain('192.0.2.10')
    routeState.replace.mockClear()
    await wrapper.findAll('[role="tab"]').find(tab => tab.text() === '概览')!.trigger('mousedown', { button: 0 })
    await flushPromises()
    expect(routeState.replace).toHaveBeenLastCalledWith({ query: { agent: 'gw', tab: undefined } })
    wrapper.unmount()
  })

  it('lists connections with the outbound chain', async () => {
    const wrapper = mount(MihomoConnections, { props: { agentId: 'gw', active: true } })
    await flushPromises()
    expect(wrapper.text()).toContain('www.example.com:443')
    expect(wrapper.text()).toContain('Proxy → HK')
    wrapper.unmount()
  })

  it('lists rules and rule providers', async () => {
    const wrapper = mount(MihomoRules, { props: { agentId: 'gw', reloadToken: 0 } })
    await flushPromises()
    expect(wrapper.text()).toContain('2 / 2 条')
    expect(wrapper.text()).toContain('ads')
    wrapper.unmount()
  })
})
