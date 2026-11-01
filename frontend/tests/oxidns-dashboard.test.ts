import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import OxiDNSRankingTable from '@/components/agents/OxiDNSRankingTable.vue'
import OxiDNSOverview from '@/components/agents/OxiDNSOverview.vue'
import OxiDNSStatisticsControls from '@/components/agents/OxiDNSStatisticsControls.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import type { OxiDNSOverview as Overview, OxiDNSRankings } from '@/types/oxidnsMonitor'

const wrappers: VueWrapper[] = []
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

describe('OxiDNS dashboard rankings', () => {
  it('shows bounded rankings with complete long addresses and real counts', () => {
    const name = '2001:db8:1234:5678:abcd:ef01:2345:6789'
    const wrapper = mount(OxiDNSRankingTable, { props: {
      title: '客户端排行', nameLabel: '客户端', emptyTitle: '暂无客户端请求', emptyDescription: '等待新的查询记录。',
      entries: Array.from({ length: 12 }, (_, index) => ({ name: index ? 'client-' + index : name, count: index ? 2 : 123456789 }))
    } })
    wrappers.push(wrapper)
    expect(wrapper.findAll('tbody tr')).toHaveLength(10)
    expect(wrapper.text()).toContain(name)
    expect(wrapper.text()).toContain((123456789).toLocaleString())
    expect(wrapper.get('table').classes()).toContain('table-fixed')
    expect(wrapper.get('tbody tr td:nth-child(2)').classes()).toContain('break-all')
  })

  it('explains empty rankings without inventing rows', () => {
    const wrapper = mount(OxiDNSRankingTable, { props: {
      title: '被拦截域名排行', nameLabel: '域名', entries: [],
      emptyTitle: '暂无已识别的拦截域名', emptyDescription: '仅统计执行轨迹明确识别的拦截查询。'
    } })
    wrappers.push(wrapper)
    expect(wrapper.text()).toContain('暂无已识别的拦截域名')
    expect(wrapper.text()).toContain('仅统计执行轨迹明确识别的拦截查询。')
    expect(wrapper.find('table').exists()).toBe(false)
  })
})


const rankings = (): OxiDNSRankings => ({
  success: true, recorder: 'query_recorder', collected_at_ms: 1792227600000, source_revision: 'config-one', instance_id: 'core-one',
  retention_days: 7, sample_limit: 10000,
  clients: { sample_size: 10000, rows: [{ key: '192.0.2.7', count: 320, share: 3.2 }], at_limit: true },
  domains: { sample_size: 25, rows: [{ key: 'example.test', count: 7, share: 28 }], at_limit: false },
  blocked: { available: true, reason: '', sample_size: 2, rows: [{ key: 'blocked.test', count: 2, share: 100 }], at_limit: false }
})
const overview = (): Overview => ({
  success: true, agent: { id: 'one', name: 'DNS', host: '192.0.2.1', version: '1.8.14-go', status: 'online' },
  collected_at: '2026-10-17T09:00:00Z', core: { status: 'running', version: '1.6.1', uptime_ms: 60000, instance_id: 'one' },
  recorders: ['query_recorder'], metrics: { requests_total: 100, failed_total: 0, avg_latency_ms: 1.2, inflight: 2, cache_hit_rate: 50 },
  statistics: { generation: 'one', started_at_ms: 1792227600000, reason: 'manual', partial: false },
  caches: [], servers: [], rankings: rankings(), upstreams: [
    { tag: 'forward_remote', upstream: 'slow', address: 'https://dns.example.test/dns-query', requests: 5, failed: 1, avg_latency_ms: 20.25 },
    { tag: 'forward_local', upstream: 'fast', address: 'udp://192.0.2.53:53', requests: 12, failed: 0, avg_latency_ms: 0 },
    { tag: 'forward_local', upstream: 'unknown', requests: null, failed: null, avg_latency_ms: null }
  ]
})
function panel(data = overview()) {
  const wrapper = mount(OxiDNSOverview, { props: { overview: data, qps: null, recorder: 'query_recorder', resetting: false } })
  wrappers.push(wrapper)
  return wrapper
}
function helpText(wrapper: VueWrapper, label: string) {
  return wrapper.findAllComponents(FieldHelp).find(help => help.props('label') === label)!.props('text')
}
function controls(data = overview()) {
  const wrapper = mount(OxiDNSStatisticsControls, { props: { statistics: data.statistics, resetting: false } })
  wrappers.push(wrapper)
  return wrapper
}

describe('OxiDNS dashboard periods and availability', () => {
  it('explains publication continuity without presenting a new statistics period', () => {
    const data = overview()
    data.statistics = { ...data.statistics!, reason: 'publication', publication_count: 2 }
    const wrapper = panel(data)
    expect(wrapper.text()).toContain('已接续 2 次配置发布，沿用原统计起点')
    expect(wrapper.text()).toContain('发布切换期间可能有少量漏计')
    expect(helpText(controls(data), '统计周期')).toContain('配置发布后继续累计')
    expect(wrapper.text()).toContain('自本轮统计开始以来')
  })

  it('disables recorder selection only while statistics are resetting', async () => {
    const data = overview()
    data.recorders = ['query_recorder', 'other_recorder']
    const wrapper = panel(data)
    const selector = wrapper.get('[aria-label="排行记录器"]')
    expect(selector.attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ resetting: true })
    expect(selector.attributes('disabled')).toBeDefined()
    await wrapper.setProps({ resetting: false })
    expect(selector.attributes('disabled')).toBeUndefined()
  })

  it('labels the period and bounded record samples separately from cumulative metrics', () => {
    const wrapper = panel()
    expect(controls().text()).toContain('本轮统计开始于')
    expect(wrapper.text()).toContain('自本轮统计开始以来')
    expect(helpText(wrapper, '请求排行')).toContain('记录样本排行，不是全量累计')
    expect(wrapper.text()).toContain('已达到样本上限')
    expect(helpText(wrapper, '请求排行')).toContain('核心保留期 7 天')
    expect(wrapper.text()).toContain('192.0.2.7')
    expect(wrapper.text()).toContain('example.test')
    expect(wrapper.text()).toContain('blocked.test')
  })

  it('ranks actual upstream endpoints and distinguishes missing latency from zero', () => {
    const wrapper = panel()
    const rows = wrapper.get('[aria-label="上游服务器排行"]').findAll('tbody tr')
    expect(rows[0].text()).toContain('udp://192.0.2.53:53')
    expect(rows[0].text()).toContain('0 ms')
    expect(rows[1].text()).toContain('https://dns.example.test/dns-query')
    expect(rows[1].text()).toContain('20.25 ms')
    expect(rows[2].text()).toContain('—')
    expect(rows[2].text()).not.toContain('0 ms')
    expect(helpText(wrapper, '上游服务器排行')).toContain('竞速或重试')
    expect(helpText(wrapper, '上游服务器排行')).toContain('包含成功和失败的已完成尝试')
  })

  it('keeps older Agent metrics visible while disabling reset and explaining unavailable rankings', () => {
    const data = overview()
    data.statistics = null
    data.rankings = { available: false, reason: '当前 Agent 尚未提供排行接口' }
    const wrapper = panel(data)
    expect(wrapper.text()).toContain('自核心启动以来')
    const toolbar = controls(data)
    expect(helpText(toolbar, '统计周期')).toContain('Agent 1.8.14-go')
    expect(wrapper.text()).toContain('当前 Agent 尚未提供排行接口')
    expect(toolbar.findAll('button').find(button => button.text() === '重置统计')!.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('100')
  })

  it('explains unavailable blocked classification instead of inventing zero blocks', () => {
    const data = overview()
    ;(data.rankings as OxiDNSRankings).blocked = { available: false, reason: '自定义配置没有标准拦截匹配器', sample_size: 0, rows: [], at_limit: false }
    const wrapper = panel(data)
    expect(wrapper.text()).toContain('暂无法识别拦截查询')
    expect(wrapper.text()).toContain('自定义配置没有标准拦截匹配器')
    expect(wrapper.text()).not.toContain('暂无已识别的拦截域名')
  })

  it('signals reset and keeps it disabled during the request', async () => {
    const wrapper = controls()
    const button = wrapper.findAll('button').find(item => item.text() === '重置统计')!
    await button.trigger('click')
    expect(wrapper.emitted('reset')).toHaveLength(1)
    await wrapper.setProps({ resetting: true })
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.text()).toContain('正在重置')
  })
})
