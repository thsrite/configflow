import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import LiveFlow from '@/components/dashboard/LiveFlow.vue'
import { clientToolsApi, mihomoPanelApi } from '@/api'
import { buildLiveFlow, type LiveFlowPath, type LiveFlowSnapshot } from '@/lib/live-flow'

vi.mock('@/api', () => ({ clientToolsApi: { notes: vi.fn() }, mihomoPanelApi: { flow: vi.fn() } }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

const path = (source: string, rule: string, policy: string, outlet: string, down: number, up = 0): LiveFlowPath =>
  ({ source, rule, policy, outlet, up, down, connections: 1 })
const snapshot = (paths: LiveFlowPath[], extra: Partial<LiveFlowSnapshot> = {}): LiveFlowSnapshot => ({
  interval: 3, connections: paths.length, active: paths.length,
  up: paths.reduce((s, p) => s + p.up, 0), down: paths.reduce((s, p) => s + p.down, 0), paths, omitted: 0, ...extra
})
const reply = (data: LiveFlowSnapshot) => ({ data: { success: true, data } }) as any

describe('buildLiveFlow', () => {
  it('builds four columns and sums rates on shared edges', () => {
    const graph = buildLiveFlow([
      path('10.0.0.2', 'RuleSet: proxy', 'Proxy', 'HK-01', 3000),
      path('10.0.0.3', 'RuleSet: proxy', 'Proxy', 'HK-01', 1000),
      path('10.0.0.3', 'Match', 'DIRECT', 'DIRECT', 500)
    ], { labelOf: ip => (ip === '10.0.0.2' ? 'iPhone' : ip), to: '/mihomo?agent=gw&tab=connections' })

    expect(graph.columns.map(c => c.title)).toEqual(['来源设备', '命中规则', '策略组', '出口节点'])
    expect(graph.columns[0].nodes.map(n => n.title)).toEqual(['iPhone', '10.0.0.3'])
    expect(graph.columns[0].nodes[0].to).toBe('/mihomo?agent=gw&tab=connections')
    const ruleToPolicy = graph.edges.filter(e => e.from === 'rule:RuleSet: proxy')
    expect(ruleToPolicy).toHaveLength(1)
    expect(ruleToPolicy[0].weight).toBeCloseTo(1.8)
    expect(graph.edges.find(e => e.from === 'rule:Match')!.weight).toBeLessThan(1.8)
  })

  it('folds columns beyond the limit and drops paths without traffic', () => {
    const paths = [1, 2, 3, 4].map(i => path(`10.0.0.${i}`, 'Match', 'DIRECT', 'DIRECT', i * 100))
    paths.push(path('10.0.0.9', 'Match', 'DIRECT', 'DIRECT', 0))
    const graph = buildLiveFlow(paths, { limit: 2 })
    expect(graph.columns[0].nodes.map(n => n.id)).toEqual(['source:10.0.0.4', 'source:10.0.0.3', 'source:*'])
    expect(graph.columns[0].nodes[2].title).toBe('其余 2 项')
    expect(graph.edges.filter(e => e.to === 'rule:Match').map(e => e.from).sort()).toEqual(['source:*', 'source:10.0.0.3', 'source:10.0.0.4'])
  })
})

const mapStub = { name: 'FlowMapViewer', props: ['columns', 'edges'], template: '<div data-test="map" />' }

describe('LiveFlow', () => {
  const wrappers: VueWrapper[] = []
  const render = async (agents: Array<{ id: string; name?: string }>) => {
    const wrapper = mount(LiveFlow, { props: { agents }, global: { stubs: { FlowMap: mapStub, FlowMapViewer: mapStub } } })
    wrappers.push(wrapper)
    await flushPromises()
    return wrapper
  }

  beforeEach(() => {
    vi.resetAllMocks()
    localStorage.clear()
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    vi.mocked(clientToolsApi.notes).mockResolvedValue({ data: { notes: { '10.0.0.2': 'iPhone' } } } as any)
  })
  afterEach(() => {
    wrappers.splice(0).forEach(w => w.unmount())
    vi.useRealTimers()
  })

  it('shows an empty state without Mihomo agents and never polls', async () => {
    const view = await render([])
    expect(view.text()).toContain('没有在线的 Mihomo Agent')
    expect(mihomoPanelApi.flow).not.toHaveBeenCalled()
  })

  it('waits for a second sample instead of inventing rates', async () => {
    vi.mocked(mihomoPanelApi.flow).mockResolvedValueOnce(reply(snapshot([], { interval: 0, connections: 4 })))
      .mockResolvedValueOnce(reply(snapshot([path('10.0.0.2', 'Match', 'DIRECT', 'DIRECT', 2048)])))
    const view = await render([{ id: 'gw', name: 'Gateway' }])
    expect(view.text()).toContain('正在采样')
    expect(view.find('[data-test="map"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    const map = view.getComponent('[data-test="map"]')
    expect((map.props('columns') as any[])[0].nodes[0].title).toBe('iPhone')
  })

  it('does not pick an agent when none of them carries traffic', async () => {
    vi.mocked(mihomoPanelApi.flow).mockResolvedValue(reply(snapshot([], { connections: 3, active: 0 })))
    const view = await render([{ id: 'a', name: 'A' }, { id: 'b', name: 'B' }])
    expect(view.text()).toContain('当前没有流量')
    expect(view.findAll('[aria-pressed="true"]')).toHaveLength(0)
    expect(view.find('[data-test="map"]').exists()).toBe(false)
  })

  it('defaults to the busiest agent and keeps a manual pick even without traffic', async () => {
    vi.mocked(mihomoPanelApi.flow).mockImplementation(async (id: string) =>
      reply(id === 'b' ? snapshot([path('10.0.0.5', 'Match', 'Proxy', 'JP', 9000)]) : snapshot([], { connections: 2, active: 0 })))
    const view = await render([{ id: 'a', name: 'A' }, { id: 'b', name: 'B' }])
    expect(view.get('[aria-pressed="true"]').text()).toContain('B')
    expect(view.find('[data-test="map"]').exists()).toBe(true)

    await view.findAll('button.chip')[0].trigger('click')
    expect(view.get('[aria-pressed="true"]').text()).toContain('A')
    expect(view.text()).toContain('2 条连接在最近一次采样内都没有收发数据')
    expect(localStorage.getItem('configflow:dashboard:live-agent')).toBe('a')
  })

  it('stops polling while paused and samples again as soon as it resumes', async () => {
    vi.mocked(mihomoPanelApi.flow).mockResolvedValue(reply(snapshot([path('10.0.0.2', 'Match', 'DIRECT', 'DIRECT', 2048)])))
    const view = await render([{ id: 'gw' }])
    expect(mihomoPanelApi.flow).toHaveBeenCalledTimes(1)
    await view.setProps({ paused: true })
    await vi.advanceTimersByTimeAsync(9000)
    expect(mihomoPanelApi.flow).toHaveBeenCalledTimes(1)
    await view.setProps({ paused: false })
    await flushPromises()
    expect(mihomoPanelApi.flow).toHaveBeenCalledTimes(2)
  })

  it('shows the agent error when every agent fails', async () => {
    vi.mocked(mihomoPanelApi.flow).mockRejectedValue({ response: { data: { message: '请将此 Agent 升级到 1.8.26-go 或更新版本后使用实时流量。' } } })
    const view = await render([{ id: 'gw' }])
    expect(view.text()).toContain('暂时读不到实时流量')
    expect(view.text()).toContain('1.8.26-go')
  })
})
