import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { upstreamRouterApi, type UpstreamRouterStatus } from '@/api/upstreamRouterPending'
import UpstreamRouterPending from '@/components/routers/UpstreamRouterPending.vue'
import { readRouterDeepLink, summarizeUpstreamRouters, upstreamRouterLink } from '@/lib/upstreamRouterPending'
import { checkedAgo } from '@/lib/format'

vi.mock('@/api/upstreamRouterPending', () => ({ upstreamRouterApi: { pending: vi.fn() } }))

const wrappers: VueWrapper[] = []
const router = (extra: Partial<UpstreamRouterStatus> = {}): UpstreamRouterStatus => ({
  id: 'ros', name: '主路由', type: 'routeros', profile_id: 'home', profile_name: '家里', state: 'synced',
  pending: 0, items: [], message: '', ...extra,
})
const reply = (routers: UpstreamRouterStatus[], extra: Record<string, unknown> = {}) =>
  ({ data: { success: true, agent_id: 'gw', agent_profile_id: 'home', checked_at: '', cached: false, routers, ...extra } } as any)
const PENDING = router({
  state: 'pending', pending: 2, items: [
    { key: 'ipv6_fakeip', label: 'IPv6 Fake-IP 分流路由', status: '待添加', count: 1 },
    { key: 'dhcp_dns', label: 'DHCP DNS', status: '待更新', count: 1 }],
})

async function render(props: Record<string, unknown> = {}, withRouter = false) {
  const navigation = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: { template: '<div />' } }, { path: '/routers', component: { template: '<div />' } }] })
  if (withRouter) { await navigation.push('/'); await navigation.isReady() }
  const wrapper = mount(UpstreamRouterPending, {
    props: { agentId: 'gw', profileId: 'home', ...props }, attachTo: document.body,
    global: { plugins: withRouter ? [navigation] : [] },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, navigation }
}

beforeEach(() => { vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([router()])) })
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

describe('upstream router pending reminder', () => {
  it('shows nothing when every router is in sync, and reports it', async () => {
    const { wrapper } = await render()
    expect(upstreamRouterApi.pending).toHaveBeenCalledExactlyOnceWith('gw', expect.objectContaining({ profileId: 'home', refresh: false }))
    expect(wrapper.find('[data-upstream-router-pending]').exists()).toBe(false)
    expect(wrapper.emitted('status')).toEqual([[{ state: 'synced', pending: 0 }]])
  })

  it('reports none when no router uses this Mihomo, and does not read without an Agent', async () => {
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([]))
    const { wrapper } = await render()
    expect(wrapper.text()).toBe('')
    expect(wrapper.emitted('status')).toEqual([[{ state: 'none', pending: 0 }]])
    const empty = await render({ agentId: '' })
    expect(upstreamRouterApi.pending).toHaveBeenCalledTimes(1)
    expect(empty.wrapper.emitted('status')).toEqual([[{ state: 'none', pending: 0 }]])
  })

  it('names the router, counts its changes, lists them and links straight to its preview', async () => {
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([PENDING]))
    const { wrapper } = await render()
    const line = wrapper.get('[data-upstream-router="home:ros"]')
    expect(line.attributes('data-state')).toBe('pending')
    expect(line.text()).toContain('上游路由「主路由」有 2 项待发布')
    expect(line.text()).toContain('IPv6 Fake-IP 分流路由：待添加；DHCP DNS：待更新')
    expect(line.text()).not.toContain('配置空间')
    const link = line.get('a')
    expect(link.text()).toBe('查看并发布')
    expect(link.attributes('href')).toBe('/routers?router=ros&profile=home&preview=1')
    expect(wrapper.get('[data-upstream-router-pending]').attributes('role')).toBe('status')
    expect(wrapper.emitted('status')).toEqual([[{ state: 'pending', pending: 2 }]])
  })

  it('says when the routers were read, since the server reuses that read until something it knows changes', async () => {
    const at = new Date(Date.now() - 5 * 60000).toISOString()
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([PENDING], { checked_at: at, cached: true }))
    const { wrapper } = await render()
    expect(wrapper.get('[data-upstream-router="home:ros"] [data-upstream-router-checked]').text()).toBe('检查于 5 分钟前')
    expect(wrapper.emitted('status')).toEqual([[{ state: 'pending', pending: 2, checkedAt: at }]])
  })

  it('words a fresh read as 刚刚检查 and an older one with its age', () => {
    expect(checkedAgo(new Date().toISOString())).toBe('刚刚检查')
    expect(checkedAgo(new Date(Date.now() - 3 * 60000).toISOString(), '读取')).toBe('读取于 3 分钟前')
  })

  it('keeps one line per router in compact mode and shortens long item lists', async () => {
    const many = router({ ...PENDING, items: [...PENDING.items, ...['a', 'b'].map(key => ({ key, label: key, status: '待更新', count: 1 }))], pending: 4 })
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([many]))
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('IPv6 Fake-IP 分流路由：待添加；DHCP DNS：待更新；a：待更新 等 4 项')
    await wrapper.setProps({ compact: true })
    expect(wrapper.text()).toContain('上游路由「主路由」有 4 项待发布')
    expect(wrapper.text()).not.toContain('DHCP DNS')
  })

  it('shows blocked and unreadable routers with their reasons, each on its own line', async () => {
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([
      router({ id: 'a', name: '主路由', state: 'blocked', pending: 1, message: '代理网关不属于当前配置空间' }),
      router({ id: 'b', name: '分店', state: 'unknown', message: '读取超时（25 秒）' }),
      router({ id: 'c', name: '已同步' }),
    ]))
    const { wrapper } = await render({ compact: true })
    const lines = wrapper.findAll('[data-upstream-router]')
    expect(lines.map(line => line.attributes('data-state'))).toEqual(['blocked', 'unknown'])
    expect(lines[0].text()).toContain('上游路由「主路由」有改动暂时无法发布')
    expect(lines[0].text()).toContain('代理网关不属于当前配置空间')
    expect(lines[0].get('a').text()).toBe('查看原因')
    expect(lines[0].get('a').attributes('href')).toBe('/routers?router=a&profile=home&preview=1')
    expect(lines[1].text()).toContain('无法确认上游路由「分店」状态')
    expect(lines[1].text()).toContain('读取超时（25 秒）')
    expect(lines[1].get('a').text()).toBe('查看差异报告')
    expect(lines[1].get('a').attributes('href')).toBe('/routers?router=b&profile=home')
    expect(wrapper.text()).not.toContain('已同步')
    expect(wrapper.emitted('status')).toEqual([[{ state: 'pending', pending: 1 }]])
  })

  it('never treats a failed request as synchronised', async () => {
    vi.mocked(upstreamRouterApi.pending).mockRejectedValue({ response: { data: { message: '读取上游路由状态超时，请稍后重试。' } } })
    const { wrapper } = await render()
    const line = wrapper.get('[data-state="unknown"]')
    expect(line.text()).toContain('无法确认上游路由状态')
    expect(line.text()).toContain('读取上游路由状态超时，请稍后重试。')
    expect(line.get('a').attributes('href')).toBe('/routers')
    expect(wrapper.emitted('status')).toEqual([[{ state: 'unknown', pending: 0 }]])
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([router()], { agent_id: 'another' }))
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    expect(wrapper.text()).toContain('上游路由状态响应无效')
  })

  it('excludes another profile even when its router has the same ID and the server returns all profiles', async () => {
    const foreign = router({ ...PENDING, profile_id: 'test', profile_name: '测试空间' })
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([foreign, router()]))
    const { wrapper } = await render()
    expect(wrapper.text()).toBe('')
    expect(wrapper.emitted('status')!.at(-1)).toEqual([{ state: 'synced', pending: 0 }])
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([foreign, PENDING]))
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    expect(wrapper.findAll('[data-upstream-router]')).toHaveLength(1)
    expect(wrapper.get('a').attributes('href')).toBe('/routers?router=ros&profile=home&preview=1')
    expect(wrapper.emitted('status')!.at(-1)).toEqual([{ state: 'pending', pending: 2 }])
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([foreign]))
    await wrapper.setProps({ refreshKey: 2 })
    await flushPromises()
    expect(wrapper.emitted('status')!.at(-1)).toEqual([{ state: 'none', pending: 0 }])
    expect(wrapper.find('a').exists()).toBe(false)
  })

  it('discards a response from the previous profile when the editor scope changes on the same Agent', async () => {
    let resolveFirst!: (value: any) => void
    vi.mocked(upstreamRouterApi.pending)
      .mockImplementationOnce(() => new Promise(resolve => { resolveFirst = resolve }))
      .mockResolvedValueOnce(reply([router({ profile_id: 'test' })]))
    const { wrapper } = await render()
    await wrapper.setProps({ profileId: 'test' })
    await flushPromises()
    resolveFirst(reply([PENDING]))
    await flushPromises()
    expect(upstreamRouterApi.pending).toHaveBeenLastCalledWith('gw', expect.objectContaining({ profileId: 'test', refresh: false }))
    expect(wrapper.text()).toBe('')
    expect(wrapper.emitted('status')!.at(-1)).toEqual([{ state: 'synced', pending: 0 }])
  })

  it('reads again, skipping the short reuse, when the refresh key changes; a new Agent starts over', async () => {
    vi.mocked(upstreamRouterApi.pending).mockResolvedValueOnce(reply([PENDING]))
    const { wrapper } = await render({ refreshKey: 0 })
    expect(wrapper.text()).toContain('2 项待发布')
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    expect(upstreamRouterApi.pending).toHaveBeenLastCalledWith('gw', expect.objectContaining({ refresh: true }))
    expect(wrapper.text()).toBe('')
    expect(wrapper.emitted('status')!.at(-1)).toEqual([{ state: 'synced', pending: 0 }])
    await wrapper.setProps({ agentId: 'other', refreshKey: 2 })
    await flushPromises()
    expect(upstreamRouterApi.pending).toHaveBeenLastCalledWith('other', expect.objectContaining({ refresh: false }))
  })

  it('ignores an older response that arrives after a newer request', async () => {
    let resolveFirst!: (value: unknown) => void
    vi.mocked(upstreamRouterApi.pending)
      .mockImplementationOnce(() => new Promise(resolve => { resolveFirst = resolve }) as any)
      .mockResolvedValueOnce(reply([router()]))
    const { wrapper } = await render({ refreshKey: 0 })
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    resolveFirst(reply([PENDING]))
    await flushPromises()
    expect(wrapper.text()).toBe('')
    expect(vi.mocked(upstreamRouterApi.pending).mock.calls[0][1]!.signal!.aborted).toBe(true)
  })

  it('navigates inside the app on a plain click', async () => {
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(reply([PENDING]))
    const { wrapper, navigation } = await render({}, true)
    const link = wrapper.get('a')
    expect(link.attributes('href')).toBe('/routers?router=ros&profile=home&preview=1')
    await link.trigger('click', { button: 0 })
    await flushPromises()
    expect(navigation.currentRoute.value.fullPath).toBe('/routers?router=ros&profile=home&preview=1')
  })
})

describe('upstream router helpers', () => {
  it('summarizes routers for @status', () => {
    expect(summarizeUpstreamRouters([])).toEqual({ state: 'none', pending: 0 })
    expect(summarizeUpstreamRouters([router(), router({ id: 'b' })])).toEqual({ state: 'synced', pending: 0 })
    expect(summarizeUpstreamRouters([router(), router({ id: 'b', state: 'unknown' })])).toEqual({ state: 'unknown', pending: 0 })
    expect(summarizeUpstreamRouters([router({ state: 'pending', pending: 3 }), router({ id: 'b', state: 'unknown' }),
      router({ id: 'c', state: 'blocked', pending: 0 })])).toEqual({ state: 'pending', pending: 4 })
  })

  it('builds and reads the router page deep link', () => {
    expect(upstreamRouterLink({ id: 'r1', profile_id: 'p' }, true)).toEqual({ path: '/routers', query: { router: 'r1', profile: 'p', preview: '1' } })
    expect(upstreamRouterLink({ id: 'r1', profile_id: '' }, false)).toEqual({ path: '/routers', query: { router: 'r1' } })
    expect(readRouterDeepLink({ router: 'r1', profile: 'p', preview: '1' })).toEqual({ router: 'r1', profile: 'p', preview: true })
    expect(readRouterDeepLink({ router: ' r1 ' })).toEqual({ router: 'r1', profile: '', preview: false })
    expect(readRouterDeepLink({ router: ['a', 'b'] })).toBeNull()
    expect(readRouterDeepLink({ preview: '1' })).toBeNull()
    expect(readRouterDeepLink({ router: 'x'.repeat(129) })).toBeNull()
  })
})
