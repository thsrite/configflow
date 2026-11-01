import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { aggregateConnections, CHAIN_SEPARATOR, chainLayers, destinationKey, outletKind, ruleHitCounts } from '@/lib/mihomo-overview'
import MihomoLatencyCard from '@/components/mihomo/MihomoLatencyCard.vue'
import MihomoConnectionStats from '@/components/mihomo/MihomoConnectionStats.vue'
import { mihomoPanelApi, trafficHistoryApi } from '@/api'
import { choose } from '@/lib/feedback'
import type { MihomoConnection } from '@/types/mihomo'

vi.mock('@/api', () => ({
  mihomoPanelApi: { proxies: vi.fn(), proxyDelay: vi.fn() },
  trafficHistoryApi: { get: vi.fn(), setEnabled: vi.fn(), clear: vi.fn(), setRetention: vi.fn() }
}))
vi.mock('@/lib/feedback', () => ({ choose: vi.fn(), confirmDanger: vi.fn(), notify: { success: vi.fn(), error: vi.fn() } }))
vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })

const connection = (id: string, options: { source?: string; host?: string; ip?: string; chains?: string[]; up?: number; down?: number }): MihomoConnection => ({
  id,
  upload: options.up ?? 0,
  download: options.down ?? 0,
  start: '2026-11-01T00:00:00Z',
  chains: options.chains ?? ['DIRECT'],
  rule: 'Match',
  rulePayload: '',
  metadata: {
    network: 'tcp', type: 'Tun', sourceIP: options.source ?? '', sourcePort: '50000',
    destinationIP: options.ip ?? '', destinationPort: '443', host: options.host ?? ''
  }
})

describe('connection aggregation', () => {
  const connections = [
    connection('a', { source: '10.0.0.2', host: 'rr1.sn-abc.googlevideo.com', chains: ['HK-01', 'Media', 'Proxy'], down: 3000, up: 10 }),
    connection('b', { source: '10.0.0.2', host: 'rr2.sn-abc.googlevideo.com', chains: ['HK-01', 'Media', 'Proxy'], down: 1000, up: 30 }),
    connection('c', { source: '10.0.0.3', host: 'www.example.com.cn', chains: ['DIRECT'], down: 500 }),
    connection('d', { ip: '203.0.113.9', chains: ['JP-02', 'Proxy'], down: 7 })
  ]
  const speeds = new Map([['a', { up: 1, down: 100 }], ['c', { up: 0, down: 50 }]])

  it('groups the destination by its registrable domain and keeps bare IPs', () => {
    expect(connections.map(destinationKey)).toEqual(['googlevideo.com', 'googlevideo.com', 'example.com.cn', '203.0.113.9'])
  })

  it('sums bytes, current speed and connection count per key', () => {
    const bySource = aggregateConnections(connections, 'source', speeds)
    // 设备按完整链路（入口策略组 → 出口节点）拆开，出站节点、链路按设备拆开
    expect(bySource[0].parts.map(part => [part.key.split(CHAIN_SEPARATOR), part.download])).toEqual([[['Proxy', 'Media', 'HK-01'], 4000]])
    expect(aggregateConnections(connections, 'outbound', speeds)[0].parts.map(part => part.key)).toEqual(['10.0.0.2'])
    expect(aggregateConnections(connections, 'chain', speeds).map(row => [chainLayers(row.key), row.parts.map(part => part.key)])).toEqual([
      [['Proxy', 'Media', 'HK-01'], ['10.0.0.2']], [['DIRECT'], ['10.0.0.3']], [['Proxy', 'JP-02'], ['Inner']]])
    expect(bySource.map(({ parts, ...row }) => row)).toEqual([
      { key: '10.0.0.2', download: 4000, upload: 40, downSpeed: 100, upSpeed: 1, count: 2 },
      { key: '10.0.0.3', download: 500, upload: 0, downSpeed: 50, upSpeed: 0, count: 1 },
      // Mihomo 自身的连接没有来源 IP
      { key: 'Inner', download: 7, upload: 0, downSpeed: 0, upSpeed: 0, count: 1 }
    ])
    // chains 从出口节点排到入口策略组
    expect(aggregateConnections(connections, 'outbound', speeds).map(row => row.key)).toEqual(['HK-01', 'DIRECT', 'JP-02'])
    expect(aggregateConnections(connections, 'group', speeds).map(row => [row.key, row.count])).toEqual([['Proxy', 3], ['DIRECT', 1]])
  })
})

describe('outlet kind', () => {
  it('uses the current node types so custom direct nodes count as direct', () => {
    const types = { 直连节点: 'Direct', 拦截: 'RejectDrop', HK: 'Shadowsocks' }
    expect(['DIRECT', '直连节点', 'REJECT', '拦截', 'HK', 'provider-node', '—', ''].map(name => outletKind(name, types)))
      .toEqual(['direct', 'direct', 'reject', 'reject', 'proxy', 'proxy', 'none', 'none'])
  })
})

describe('rule hit counts', () => {
  it('reports older cores without extra as unsupported', () => {
    expect(ruleHitCounts([{ type: 'Match', payload: '', proxy: 'DIRECT' }])).toEqual({ supported: false, total: 0, items: [] })
  })

  it('lists hit rules by count and keeps their order for ties', () => {
    const result = ruleHitCounts([
      { index: 0, type: 'DomainSuffix', payload: 'example.com', proxy: 'Proxy', extra: { hitCount: 5, hitAt: '2026-11-01T00:00:00Z' } },
      { index: 1, type: 'RuleSet', payload: 'cn', proxy: 'DIRECT', extra: { hitCount: 9 } },
      { index: 2, type: 'GeoIP', payload: 'LAN', proxy: 'DIRECT', extra: { hitCount: 0 } },
      { index: 3, type: 'Match', payload: '', proxy: 'Proxy', extra: { hitCount: 5 } }
    ])
    expect(result.supported).toBe(true)
    expect(result.total).toBe(19)
    expect(result.items.map(item => item.index)).toEqual([1, 0, 3])
  })
})

describe('connection statistics card', () => {
  it('shows device notes and remembers the aggregation', async () => {
    localStorage.clear()
    const wrapper = mount(MihomoConnectionStats, { props: {
      connections: [connection('a', { source: '10.0.0.2', host: 'a.example.com', down: 2048, chains: ['HK', 'Proxy'] })],
      speeds: new Map(), notes: { '10.0.0.2': '书房电脑' }
    } })
    expect(wrapper.text()).toContain('书房电脑')
    expect(wrapper.text()).toContain('2.0 KB')
    await wrapper.findAll('[data-segment]').find(button => button.text() === '按策略组')!.trigger('click')
    expect(wrapper.text()).toContain('Proxy')
    expect(localStorage.getItem('configflow:mihomo-overview:aggregate')).toBe('group')
    wrapper.unmount()
  })
})

describe('latency card', () => {
  const wrappers: VueWrapper[] = []
  beforeEach(() => {
    vi.resetAllMocks()
    localStorage.clear()
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: {
      GLOBAL: { name: 'GLOBAL', type: 'Selector', all: ['Proxy', 'Media'], now: 'Proxy' },
      Proxy: { name: 'Proxy', type: 'Selector', all: ['HK'], now: 'HK' },
      Media: { name: 'Media', type: 'Selector', all: ['HK'], now: 'HK' },
      HK: { name: 'HK', type: 'Shadowsocks' }
    } } } } as any)
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it('tests domestic sites directly and foreign ones through the remembered group', async () => {
    localStorage.setItem('configflow:mihomo-overview:latency-group:gw', 'Media')
    vi.mocked(mihomoPanelApi.proxyDelay).mockImplementation(async (_id: string, name: string) => {
      if (name === 'Media') throw { response: { data: { message: 'Timeout' } } }
      return { data: { data: { delay: 35 } } } as any
    })
    const wrapper = mount(MihomoLatencyCard, { props: { agentId: 'gw', active: true } })
    wrappers.push(wrapper)
    await flushPromises()
    const calls = vi.mocked(mihomoPanelApi.proxyDelay).mock.calls.map(call => [call[1], call[2]])
    expect(calls).toContainEqual(['DIRECT', 'https://www.baidu.com'])
    expect(calls).toContainEqual(['Media', 'https://www.google.com/generate_204'])
    expect(calls.filter(call => call[0] === 'Media')).toHaveLength(4)
    const rows = wrapper.findAll('[data-latency]').map(row => row.text())
    expect(rows[0]).toContain('35 ms')
    expect(rows[1]).toContain('经 Media')
    expect(rows[1]).toContain('超时')
  })

  it('can measure from the browser instead, like zashboard, and remembers the choice', async () => {
    const loaded: string[] = []
    vi.stubGlobal('Image', class {
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      set src(value: string) {
        if (!value) return
        loaded.push(value)
        // YouTube 的图标加载失败
        setTimeout(() => (value.includes('ggpht') ? this.onerror : this.onload)?.(), 0)
      }
    })
    vi.mocked(mihomoPanelApi.proxyDelay).mockResolvedValue({ data: { data: { delay: 35 } } } as any)
    const wrapper = mount(MihomoLatencyCard, { props: { agentId: 'gw', active: true } })
    wrappers.push(wrapper)
    await flushPromises()
    vi.mocked(mihomoPanelApi.proxyDelay).mockClear()
    await wrapper.findAll('[data-segment]').find(button => button.text() === '经浏览器')!.trigger('click')
    await new Promise(resolve => setTimeout(resolve, 10))
    await flushPromises()
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
    expect(loaded.map(url => url.split('?')[0])).toContain('https://www.cloudflare.com/favicon.ico')
    // 只有经 Mihomo 时才选策略组
    expect(wrapper.find('[aria-label="海外站点经过的策略组"]').exists()).toBe(false)
    const rows = wrapper.findAll('[data-latency]').map(row => row.text())
    expect(rows[0]).toContain('浏览器直接访问')
    expect(rows[0]).toMatch(/\d+ ms/)
    expect(rows[4]).toContain('超时')
    expect(localStorage.getItem('configflow:mihomo-overview:latency-mode')).toBe('browser')
    // 切回经 Mihomo，之前的结果还在
    await wrapper.findAll('[data-segment]').find(button => button.text() === '经 Mihomo')!.trigger('click')
    expect(wrapper.findAll('[data-latency]')[0].text()).toContain('35 ms')
    vi.unstubAllGlobals()
    vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  })

  it('does not poll while hidden', async () => {
    vi.mocked(mihomoPanelApi.proxyDelay).mockResolvedValue({ data: { data: { delay: 35 } } } as any)
    const wrapper = mount(MihomoLatencyCard, { props: { agentId: 'gw', active: false } })
    wrappers.push(wrapper)
    await flushPromises()
    expect(mihomoPanelApi.proxies).not.toHaveBeenCalled()
    expect(mihomoPanelApi.proxyDelay).not.toHaveBeenCalled()
    await wrapper.setProps({ active: true })
    await flushPromises()
    // 默认取配置里第一个策略组
    expect(vi.mocked(mihomoPanelApi.proxyDelay).mock.calls.map(call => call[1])).toContain('Proxy')
  })
})

describe('traffic history on the statistics card', () => {
  const state = (extra: Record<string, unknown> = {}) => ({ data: { success: true, data: {
    enabled: false, since: null, supported: true, min_version: '1.8.51-go', retention_days: 90, retention_choices: [30, 90, 180, 365], ...extra
  } } }) as any
  const history = state({
    enabled: true, since: '2026-11-01T02:00:00+00:00', range: '24h', by: 'source', step: 3600,
    rows: [{ key: '10.0.0.2', up: 10, down: 4096, conns: 7 }, { key: '', up: 1, down: 1024, conns: 0 }],
    total: { up: 11, down: 5120, conns: 7 }, trend: [{ at: 'a', up: 1, down: 100 }, { at: 'b', up: 10, down: 5020 }]
  })
  const mountCard = async () => {
    const wrapper = mount(MihomoConnectionStats, { props: {
      connections: [connection('a', { source: '10.0.0.2', host: 'a.example.com', down: 2048 })],
      speeds: new Map(), notes: { '10.0.0.2': '书房电脑' }, agentId: 'gw', active: true
    } })
    await flushPromises()
    return wrapper
  }
  beforeEach(() => {
    vi.resetAllMocks()
    localStorage.clear()
  })

  it('turns recording on and shows only the live view until it is on', async () => {
    vi.mocked(trafficHistoryApi.get).mockResolvedValueOnce(state()).mockResolvedValue(history)
    vi.mocked(trafficHistoryApi.setEnabled).mockResolvedValue({} as any)
    const wrapper = await mountCard()
    expect(wrapper.text()).toContain('开启后 Agent 在后台持续记录')
    expect(wrapper.findAll('[data-segment]').map(button => button.text())).not.toContain('24 小时')
    await wrapper.get('button[role="switch"]').trigger('click')
    await flushPromises()
    expect(trafficHistoryApi.setEnabled).toHaveBeenCalledWith('gw', true, false)
    expect(wrapper.text()).toContain('记录中')
    await wrapper.findAll('[data-segment]').find(button => button.text() === '24 小时')!.trigger('click')
    await flushPromises()
    expect(trafficHistoryApi.get).toHaveBeenLastCalledWith('gw', '24h', 'source', expect.anything())
    // 历史范围：没有速度列，「未归属」单独一行，设备数不算它
    expect(wrapper.text()).toContain('新建连接')
    expect(wrapper.text()).not.toContain('速度')
    expect(wrapper.text()).toContain('未归属')
    expect(wrapper.text()).toContain('5.0 KB')
    expect(wrapper.findAll('tbody tr')[0].text()).toContain('书房电脑')
    expect(localStorage.getItem('configflow:mihomo-overview:range')).toBe('24h')
    wrapper.unmount()
  })

  it('asks whether to keep the data when recording stops', async () => {
    vi.mocked(trafficHistoryApi.get).mockResolvedValueOnce(history).mockResolvedValue(state())
    vi.mocked(trafficHistoryApi.setEnabled).mockResolvedValue({} as any)
    vi.mocked(choose).mockResolvedValue('alt')
    const wrapper = await mountCard()
    await wrapper.get('button[role="switch"]').trigger('click')
    await flushPromises()
    expect(trafficHistoryApi.setEnabled).toHaveBeenCalledWith('gw', false, true)
    wrapper.unmount()
  })

  it('lists each device with its chains expanded, direct share and a fold for the rest', async () => {
    vi.mocked(trafficHistoryApi.get).mockResolvedValue(state())
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: { 直连节点: { type: 'Direct' }, HK: { type: 'Shadowsocks' } } } } } as any)
    const wrapper = mount(MihomoConnectionStats, { props: {
      connections: [
        connection('a', { source: '10.0.0.2', host: 'a.example.com', down: 3072, chains: ['HK', '美国自建', '人工智能'] }),
        connection('b', { source: '10.0.0.2', host: 'b.example.cn', down: 1024, chains: ['直连节点', '全球直连'] }),
        connection('c', { source: '10.0.0.2', host: 'c.example', down: 10, chains: ['JP', 'Proxy'] }),
        connection('d', { source: '10.0.0.2', host: 'd.example', down: 5, chains: ['US', 'Proxy'] })
      ],
      speeds: new Map(), notes: {}, agentId: 'gw', active: true
    } })
    await flushPromises()
    const tiles = wrapper.findAll('[data-tile]').map(tile => tile.text())
    expect(tiles.find(text => text.startsWith('直连'))).toContain('1.0 KB')
    expect(tiles.find(text => text.startsWith('代理'))).toContain('3.0 KB')
    // 设备行显示直连占比；链路不用点开就列出，前 3 条之外折成「其余」
    const group = wrapper.get('tbody tr[data-group]')
    expect(group.text()).toContain('25%')
    const parts = wrapper.findAll('tbody tr.stats-part').map(row => row.text())
    expect(parts[0]).toContain('人工智能→美国自建→HK')
    expect(parts[0]).toContain('代理')
    expect(parts[1]).toContain('全球直连→直连节点')
    expect(parts[1]).toContain('直连')
    expect(parts[3]).toContain('其余 1 条链路 · 5 B')
    await wrapper.get('.stats-more-button').trigger('click')
    expect(wrapper.findAll('tbody tr.stats-part').map(row => row.text())[3]).toContain('Proxy→US')
    // 收起全部明细
    await wrapper.get('.stats-toggle-all').trigger('click')
    expect(wrapper.findAll('tbody tr.stats-part')).toHaveLength(0)
    // 按链路汇总：链路行标出直连 / 代理，明细是设备
    await wrapper.findAll('[data-segment]').find(button => button.text() === '按链路')!.trigger('click')
    expect(wrapper.findAll('thead th')[1].text()).toBe('类型')
    const first = wrapper.findAll('tbody tr[data-group]')[0]
    expect(first.text()).toContain('人工智能→美国自建→HK')
    expect(first.text()).toContain('代理')
    expect(wrapper.get('tbody tr.stats-part').text()).toContain('10.0.0.2')
    wrapper.unmount()
  })

  it('tells 1.8.51-go agents that full chains need an update', async () => {
    vi.mocked(trafficHistoryApi.get).mockResolvedValue(state({ enabled: true, since: '2026-11-01T02:00:00+00:00', full_chain: false, full_chain_version: '1.8.52-go' }))
    const wrapper = await mountCard()
    expect(wrapper.text()).toContain('Agent 更新到 1.8.52-go 后记录完整链路')
    wrapper.unmount()
  })

  it('keeps the switch off for agents that are too old', async () => {
    vi.mocked(trafficHistoryApi.get).mockResolvedValue(state({ supported: false }))
    const wrapper = await mountCard()
    expect(wrapper.text()).toContain('需要将 Agent 更新到 1.8.51-go')
    expect(wrapper.get('button[role="switch"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
