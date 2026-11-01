import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { aggregateConnections, destinationKey, ruleHitCounts } from '@/lib/mihomo-overview'
import MihomoLatencyCard from '@/components/mihomo/MihomoLatencyCard.vue'
import MihomoConnectionStats from '@/components/mihomo/MihomoConnectionStats.vue'
import { mihomoPanelApi } from '@/api'
import type { MihomoConnection } from '@/types/mihomo'

vi.mock('@/api', () => ({ mihomoPanelApi: { proxies: vi.fn(), proxyDelay: vi.fn() } }))
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
    expect(bySource).toEqual([
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
    const rows = wrapper.findAll('li').map(row => row.text())
    expect(rows[0]).toContain('35 ms')
    expect(rows[1]).toContain('经 Media')
    expect(rows[1]).toContain('超时')
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
