import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter, RouterView, useRoute } from 'vue-router'
import { agentApi, clientToolsApi, oxidnsMonitorApi, profileApi } from '@/api'
import { invalidateAgentSummaries } from '@/lib/agentSummaries'
import OxiDNSMonitor from '@/views/OxiDNSMonitor.vue'
import { Select } from '@/components/ui/select'
import { confirm, notify } from '@/lib/feedback'
import { OXIDNS_DEVICE_STORAGE_KEY } from '@/composables/useOxiDNSDevices'
import { forgetOxiDNSRateSamples } from '@/composables/useOxiDNSMonitor'

vi.mock('@/api', () => ({ clientToolsApi: { notes: vi.fn() }, agentApi: { getAll: vi.fn(), summaries: vi.fn() }, profileApi: { list: vi.fn() }, oxidnsMonitorApi: { overview: vi.fn(), queries: vi.fn(), query: vi.fn(), resetStatistics: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({ confirm: vi.fn(), notify: { success: vi.fn(), error: vi.fn() } }))
const wrappers: VueWrapper[] = []
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
const device = (id: string, status = 'online', service_type = 'oxidns') => ({ id, name: 'Device ' + id, host: 'host-' + id, status, service_type })
const response = (data: any) => ({ data } as any)
const snapshot = (id: string) => ({
  success: true, agent: { ...device(id), profile_id: 'default', version: '1.7.0-go' }, collected_at: '2026-10-17T01:00:00Z',
  core: { status: 'running', version: '1.6.1', uptime_ms: 1000, instance_id: id }, recorders: ['queries'],
  metrics: { requests_total: 100, failed_total: 0, avg_latency_ms: 1, inflight: 0, cache_hit_rate: 50 }, caches: [], servers: []
})
async function render(path = '/agents/one/oxidns', remount = true) {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/agents/:agentId/oxidns', component: OxiDNSMonitor }, { path: '/agents', component: { template: '<div>Agents</div>' } },
    { path: '/oxidns', component: { template: '<div data-test="entry">Entry</div>' } }
  ] })
  await router.push(path)
  await router.isReady()
  // App.vue keys routed pages by path; this reproduces a real device switch remount.
  const shell = defineComponent({ components: { RouterView }, setup: () => ({ route: useRoute() }), template: remount ? '<RouterView :key="route.path" />' : '<RouterView />' })
  const wrapper = mount(shell, { global: { plugins: [router], stubs: {
    OxidnsSettings: { props: ['profileId', 'initialFeature', 'contextLabel'], template: '<div data-test="recorder-settings">{{ profileId }} · {{ initialFeature }} · {{ contextLabel }}</div>' },
    OxiDNSOverviewPanel: { props: ['overview', 'resetting'], template: '<div data-test="overview" :data-resetting="String(resetting)">{{ overview.agent.name }}</div>' },
    OxiDNSQueryTable: { props: ['data', 'notes'], emits: ['detail'], template: '<div data-test="queries">{{ data?.records[0]?.client_ip }}<span data-test="client-name">{{ notes?.["192.0.2.10"] }}</span><button data-test="detail" @click="$emit(\'detail\', { id: 12 })">详情</button></div>' },
    OxiDNSQueryDetail: { props: ['agentId'], template: '<div data-test="query-detail">{{ agentId }}</div>' }
  } } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => {
  vi.resetAllMocks()
  invalidateAgentSummaries()
  stubStorage()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: !String(query).includes('639px'), addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })))
  vi.mocked(clientToolsApi.notes).mockResolvedValue(response({ success: true, profile_id: 'default', profile_name: '默认配置', notes: {} }))
  vi.mocked(confirm).mockResolvedValue(true)
  vi.mocked(oxidnsMonitorApi.resetStatistics).mockImplementation(async id => response({ ...snapshot(id), statistics: { generation: 'reset', started_at_ms: 0, reason: 'manual', partial: false } }))
  vi.mocked(agentApi.getAll).mockResolvedValue(response([device('one'), device('two', 'offline'), device('proxy', 'online', 'mihomo')]))
  vi.mocked(agentApi.summaries).mockResolvedValue(response([device('one'), device('two', 'offline'), device('proxy', 'online', 'mihomo')]))
  vi.mocked(profileApi.list).mockResolvedValue(response([{ id: 'default', name: '默认配置', is_default: true }, { id: 'office', name: '办公室' }]))
  vi.mocked(oxidnsMonitorApi.overview).mockImplementation(async id => response(snapshot(id)))
  vi.mocked(oxidnsMonitorApi.queries).mockImplementation(async id => response({ success: true, recorder: 'queries', records: [{ id: 12, client_ip: 'client-' + id }], next_cursor: null }))
})
afterEach(() => {
  forgetOxiDNSRateSamples()
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('OxiDNS monitor device selection', () => {
  it('opens from a fresh lightweight lookup without waiting for content comparison', async () => {
    const { wrapper } = await render()
    vi.mocked(agentApi.getAll).mockReturnValue(new Promise(() => {}))
    // The picker cache and monitor snapshot still refer to the default profile.
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([{ ...device('one'), profile_id: 'office' }]))
    await wrapper.findAll('button').find(button => button.text() === '设置')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="recorder-settings"]').text()).toBe('office ·  · 办公室 · Device one')
    expect(agentApi.getAll).not.toHaveBeenCalled()
    expect(agentApi.summaries).toHaveBeenCalledTimes(2)
  })

  it('opens general settings for the selected offline Agent without requiring monitor data', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockRejectedValueOnce(new Error('核心未运行'))
    const { wrapper } = await render('/agents/two/oxidns')
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([{ ...device('two', 'offline'), profile_id: 'office' }]))
    await wrapper.findAll('button').find(button => button.text() === '设置')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="recorder-settings"]').text()).toBe('office ·  · 办公室 · Device two')
  })
  it('ignores a settings lookup that returns after switching devices', async () => {
    const { wrapper, router } = await render()
    let finish!: (value: any) => void
    vi.mocked(agentApi.summaries).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await wrapper.findAll('button').find(button => button.text() === '设置')!.trigger('click')
    await router.replace('/agents/two/oxidns')
    await flushPromises()
    finish(response([{ ...device('one'), profile_id: 'office' }]))
    await flushPromises()
    expect(wrapper.find('[data-test="recorder-settings"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Device two')
  })
  it('opens recorder settings for the fresh Agent binding, not the sidebar or old snapshot profile', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockImplementation(async id => response({ ...snapshot(id), recorders: [] }))
    const { wrapper } = await render('/agents/one/oxidns?tab=queries')
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([{ ...device('one'), profile_id: 'office' }]))
    await wrapper.findAll('button').find(button => button.text() === '前往设置')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="recorder-settings"]').text()).toBe('office · query_recorder · 办公室 · Device one')
  })
  it('opens the API settings when the core API is not configured', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockRejectedValue({ response: { data: { success: false, code: 'API_NOT_CONFIGURED', message: 'OxiDNS 管理 API 未启用。' } } })
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('OxiDNS 管理 API 未启用。')
    await wrapper.findAll('button').find(button => button.text() === '前往设置')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="recorder-settings"]').text()).toContain('default · api ·')
  })
  it('does not offer API settings for other overview failures', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockRejectedValue({ response: { data: { success: false, code: 'CORE_UNREACHABLE', message: '暂时无法连接 OxiDNS 管理 API。' } } })
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('暂时无法连接 OxiDNS 管理 API。')
    expect(wrapper.findAll('button').some(button => button.text() === '前往设置')).toBe(false)
  })
  it('uses the actual default profile for a legacy Agent with no explicit binding', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockImplementation(async id => response({ ...snapshot(id), recorders: [] }))
    vi.mocked(profileApi.list).mockResolvedValue(response([{ id: 'default', name: '旧默认', is_default: false }, { id: 'home', name: '家庭', is_default: true }]))
    const { wrapper } = await render('/agents/one/oxidns?tab=queries')
    await wrapper.findAll('button').find(button => button.text() === '前往设置')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="recorder-settings"]').text()).toContain('home · query_recorder · 家庭')
  })
  it('shows a settings lookup error without opening an editor for the wrong profile', async () => {
    vi.mocked(profileApi.list).mockRejectedValueOnce(new Error('配置空间读取失败'))
    const { wrapper } = await render()
    await wrapper.findAll('button').find(button => button.text() === '设置')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('配置空间读取失败')
    expect(wrapper.find('[data-test="recorder-settings"]').exists()).toBe(false)
  })
  it('shows the current device identity and status without a back-to-Agent action', async () => {
    const { wrapper } = await render()
    const selector = wrapper.get('button[role="combobox"]')
    expect(selector.text()).toContain('Device one')
    expect(selector.text()).not.toContain('host-one')
    expect(wrapper.get('[aria-label="当前设备 IP"]').text()).toBe('host-one')
    expect(wrapper.text()).not.toContain('返回 Agent')
    expect(window.localStorage.getItem(OXIDNS_DEVICE_STORAGE_KEY)).toBe('one')
  })
  it('keeps the query tab after remount, closes details, and fetches only the newly selected device data', async () => {
    const { wrapper, router } = await render('/agents/one/oxidns?tab=queries')
    await wrapper.get('[data-test="detail"]').trigger('click')
    expect(wrapper.get('[data-test="query-detail"]').text()).toBe('one')
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'two')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/agents/two/oxidns')
    expect(router.currentRoute.value.query.tab).toBe('queries')
    expect(wrapper.find('[data-test="query-detail"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="queries"]').text()).toContain('client-two')
    expect(wrapper.get('[data-test="queries"]').text()).not.toContain('client-one')
    expect(wrapper.text()).toContain('Agent 离线')
    expect(window.localStorage.getItem(OXIDNS_DEVICE_STORAGE_KEY)).toBe('two')
  })
  it('preserves a user-selected overview tab instead of an older tab query', async () => {
    const { wrapper, router } = await render('/agents/one/oxidns?tab=queries')
    await wrapper.findAll('[role="tab"]').find(item => item.text() === '运行概览')!.trigger('keydown', { key: 'Enter' })
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'two')
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBe('overview')
    expect(wrapper.find('[data-test="overview"]').exists()).toBe(true)
  })
  it('keeps device switching available when the current core cannot be read', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockRejectedValueOnce({ response: { data: { message: '核心未运行' } } })
    const { wrapper, router } = await render()
    expect(wrapper.text()).toContain('核心未运行')
    expect(wrapper.get('[aria-label="当前设备 IP"]').text()).toBe('host-one')
    expect(wrapper.get('button[role="combobox"]').attributes('disabled')).toBeUndefined()
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'two')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/agents/two/oxidns')
    expect(wrapper.text()).toContain('Device two')
  })
  it('retries a failed device list without replacing a valid overview', async () => {
    vi.mocked(agentApi.summaries).mockRejectedValueOnce({ response: { data: { message: '设备列表暂不可用' } } })
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('设备列表暂不可用')
    expect(wrapper.find('[data-test="overview"]').exists()).toBe(true)
    await wrapper.findAll('button').find(item => item.text() === '重试设备列表')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('设备列表暂不可用')
    expect(wrapper.get('button[role="combobox"]').attributes('disabled')).toBeUndefined()
  })
  it('shows connection guidance when no OxiDNS devices remain', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([device('proxy', 'online', 'mihomo')]))
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('暂无 OxiDNS Agent')
    expect(wrapper.find('[data-test="overview"]').exists()).toBe(false)
  })
  it('does not navigate to a non-OxiDNS Agent', async () => {
    const { wrapper, router } = await render()
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'proxy')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/agents/one/oxidns')
  })
})


describe('OxiDNS dashboard reset confirmation', () => {
  beforeEach(() => {
    vi.mocked(oxidnsMonitorApi.overview).mockImplementation(async id => response({ ...snapshot(id),
      statistics: { generation: 'period', started_at_ms: 0, reason: 'initial', partial: false }
    }))
  })
  it('explains scope and preserves the dashboard when confirmation is cancelled', async () => {
    vi.mocked(confirm).mockResolvedValue(false)
    const { wrapper } = await render()
    await wrapper.findAll('button').find(button => button.text() === '重置统计')!.trigger('click')
    await flushPromises()
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('查询记录和 DNS 服务保持不变'), expect.objectContaining({ confirmText: '重新开始统计' }))
    expect(oxidnsMonitorApi.resetStatistics).not.toHaveBeenCalled()
    expect(notify.success).not.toHaveBeenCalled()
  })
  it('resets only the selected device after confirmation and reports success', async () => {
    const { wrapper } = await render()
    await wrapper.findAll('button').find(button => button.text() === '重置统计')!.trigger('click')
    await flushPromises()
    expect(oxidnsMonitorApi.resetStatistics).toHaveBeenCalledWith('one', 'queries', expect.anything())
    expect(notify.success).toHaveBeenCalledWith('已重新开始统计', expect.stringContaining('查询记录已保留'))
  })
  it('passes pending reset state to the overview until the request completes', async () => {
    let finish!: (value: any) => void
    vi.mocked(oxidnsMonitorApi.resetStatistics).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const { wrapper } = await render()
    expect(wrapper.get('[data-test="overview"]').attributes('data-resetting')).toBe('false')
    await wrapper.findAll('button').find(button => button.text() === '重置统计')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="overview"]').attributes('data-resetting')).toBe('true')
    finish(response({ ...snapshot('one'), statistics: { generation: 'reset', started_at_ms: 0, reason: 'manual', partial: false } }))
    await flushPromises()
    expect(wrapper.get('[data-test="overview"]').attributes('data-resetting')).toBe('false')
  })
  it('does not reset a newly selected device when an old confirmation is accepted', async () => {
    let accept!: (value: boolean) => void
    vi.mocked(confirm).mockReturnValue(new Promise(resolve => { accept = resolve }))
    const { wrapper, router } = await render()
    await wrapper.findAll('button').find(button => button.text() === '重置统计')!.trigger('click')
    await router.replace('/agents/two/oxidns')
    await flushPromises()
    accept(true)
    await flushPromises()
    expect(oxidnsMonitorApi.resetStatistics).not.toHaveBeenCalled()
  })
})

describe('OxiDNS query names follow the Agent binding', () => {
  const notes = (name: string, profile = 'office') => response({
    success: true, profile_id: profile, profile_name: profile, notes: { '192.0.2.10': name }
  })
  it('uses the Agent notes endpoint even when the overview belongs to a different profile', async () => {
    vi.mocked(clientToolsApi.notes).mockResolvedValue(notes('办公室电脑'))
    const { wrapper } = await render('/agents/one/oxidns?tab=queries')
    expect(clientToolsApi.notes).toHaveBeenCalledWith('one')
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('办公室电脑')
    expect(profileApi.list).not.toHaveBeenCalled()
    expect(agentApi.getAll).not.toHaveBeenCalled()
  })
  it('clears the previous profile name while a newly selected Agent is loading', async () => {
    vi.mocked(clientToolsApi.notes).mockResolvedValueOnce(notes('办公室电脑'))
    const { wrapper, router } = await render('/agents/one/oxidns?tab=queries', false)
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('办公室电脑')
    let finish!: (value: any) => void
    vi.mocked(clientToolsApi.notes).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await router.replace('/agents/two/oxidns?tab=queries')
    await flushPromises()
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('')
    finish(notes('家庭电脑', 'home'))
    await flushPromises()
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('家庭电脑')
    expect(wrapper.text()).not.toContain('办公室电脑')
  })
  it.each([true, false])('ignores old Agent notes after switching (remount=%s)', async remount => {
    let finish!: (value: any) => void
    vi.mocked(clientToolsApi.notes).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const { wrapper, router } = await render('/agents/one/oxidns?tab=queries', remount)
    vi.mocked(clientToolsApi.notes).mockResolvedValueOnce(notes('家庭电脑', 'home'))
    await router.replace('/agents/two/oxidns?tab=queries')
    await flushPromises()
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('家庭电脑')
    finish(notes('旧空间电脑'))
    await flushPromises()
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('家庭电脑')
    expect(wrapper.text()).not.toContain('旧空间电脑')
  })
  it.each(['rejected', 'unsuccessful'] as const)('preserves successful notes after a %s refresh and recovers on retry', async failure => {
    vi.mocked(clientToolsApi.notes).mockResolvedValueOnce(notes('办公室电脑'))
    const { wrapper } = await render('/agents/one/oxidns?tab=queries')
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('办公室电脑')
    if (failure === 'rejected') vi.mocked(clientToolsApi.notes).mockRejectedValueOnce(new Error('备注暂不可用'))
    else vi.mocked(clientToolsApi.notes).mockResolvedValueOnce(response({ success: false, message: '备注暂不可用', notes: {} }))
    await wrapper.get('[aria-label="刷新监控"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('办公室电脑')
    expect(wrapper.get('[role="alert"]').text()).toContain('备注暂不可用')
    expect(wrapper.get('[role="alert"]').text()).toContain('保留上次成功读取的备注')
    vi.mocked(clientToolsApi.notes).mockResolvedValueOnce(notes('更新后的名称'))
    await wrapper.get('[aria-label="刷新监控"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('更新后的名称')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
  it('keeps queries usable after a notes failure and retries on manual refresh', async () => {
    vi.mocked(clientToolsApi.notes).mockRejectedValueOnce(new Error('备注暂不可用'))
    const { wrapper } = await render('/agents/one/oxidns?tab=queries')
    expect(wrapper.get('[data-test="queries"]').text()).toContain('client-one')
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('')
    vi.mocked(clientToolsApi.notes).mockResolvedValueOnce(notes('恢复后的名称'))
    await wrapper.get('[aria-label="刷新监控"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="client-name"]').text()).toBe('恢复后的名称')
  })
})

describe('opening the remembered device directly', () => {
  it.each(['AGENT_NOT_FOUND', 'WRONG_SERVICE'])('forgets a device the server reports as %s and goes back to the entry page', async code => {
    localStorage.setItem(OXIDNS_DEVICE_STORAGE_KEY, 'gone')
    vi.mocked(oxidnsMonitorApi.overview).mockRejectedValue({ response: { data: { code, message: '未找到这台 Agent。' } } })
    const { router, wrapper } = await render('/agents/gone/oxidns')
    expect(router.currentRoute.value.path).toBe('/oxidns')
    expect(wrapper.find('[data-test="entry"]').exists()).toBe(true)
    expect(localStorage.getItem(OXIDNS_DEVICE_STORAGE_KEY)).toBeNull()
  })

  it('keeps another remembered device and stays on the panel for other failures', async () => {
    localStorage.setItem(OXIDNS_DEVICE_STORAGE_KEY, 'two')
    vi.mocked(oxidnsMonitorApi.overview).mockRejectedValueOnce({ response: { data: { code: 'AGENT_NOT_FOUND' } } })
    await render('/agents/gone/oxidns')
    expect(localStorage.getItem(OXIDNS_DEVICE_STORAGE_KEY)).toBe('two')
    vi.mocked(oxidnsMonitorApi.overview).mockRejectedValueOnce({ response: { data: { code: 'AGENT_UNREACHABLE', message: '暂时无法连接 Agent' } } })
    const { router, wrapper } = await render('/agents/one/oxidns')
    expect(router.currentRoute.value.path).toBe('/agents/one/oxidns')
    expect(wrapper.text()).toContain('暂时无法连接 Agent')
  })
})

describe('reopening with the last overview', () => {
  it('shows the last overview of the device at once and replaces it with the fresh one', async () => {
    const first = await render('/agents/one/oxidns')
    expect(first.wrapper.get('[data-test="overview"]').text()).toBe('Device one')
    first.wrapper.unmount()
    wrappers.splice(wrappers.indexOf(first.wrapper), 1)
    let answer!: () => void
    vi.mocked(oxidnsMonitorApi.overview).mockImplementation(id => new Promise(yes => { answer = () => yes(response({ ...snapshot(id), agent: { ...snapshot(id).agent, name: 'Fresh one' } })) }))
    const { wrapper } = await render('/agents/one/oxidns')
    expect(wrapper.get('[data-test="overview"]').text()).toBe('Device one')
    expect(wrapper.text()).toContain('采样')
    answer(); await flushPromises()
    expect(wrapper.get('[data-test="overview"]').text()).toBe('Fresh one')
  })

  it('opens a device that was never read with the loading state', async () => {
    await render('/agents/one/oxidns')
    vi.mocked(oxidnsMonitorApi.overview).mockReturnValue(new Promise(() => {}))
    const { wrapper } = await render('/agents/two/oxidns')
    expect(wrapper.find('[data-test="overview"]').exists()).toBe(false)
  })
})
