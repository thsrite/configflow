import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { agentApi, profileApi, type PublishGuard } from '@/api'
import { upstreamRouterApi } from '@/api/upstreamRouterPending'
import SettingsPublishBar from '@/components/generate/SettingsPublishBar.vue'
import UpstreamRouterPending from '@/components/routers/UpstreamRouterPending.vue'

vi.mock('@/api', () => ({
  agentApi: { summaries: vi.fn(), pushConfig: vi.fn(), getDeployment: vi.fn(), activateDeployment: vi.fn(), verifyIPv6: vi.fn(), getIPv6Verification: vi.fn() },
  profileApi: { list: vi.fn() },
}))
vi.mock('@/api/upstreamRouterPending', () => ({ upstreamRouterApi: { pending: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() } }))

const wrappers: VueWrapper[] = []
const response = (data: any) => ({ data } as any)
const guard = (serviceType = 'mihomo'): PublishGuard => ({ expected_profile_id: 'office', expected_service_type: serviceType, expected_config_revision: 7 })
const agent = (id: string, extra: Record<string, unknown> = {}) => ({ id, name: `Mihomo ${id}`, profile_id: 'office', service_type: 'mihomo', status: 'online', enabled: true, ...extra })
const pendingRouter = { id: 'ros', name: '主路由', type: 'routeros', profile_id: 'office', profile_name: '办公室', state: 'pending', pending: 1,
  items: [{ key: 'ipv6_fakeip', label: 'IPv6 Fake-IP 分流路由', status: '待添加', count: 1 }], message: '' }

async function render(props: Record<string, unknown> = {}) {
  const wrapper = mount(SettingsPublishBar, {
    props: { open: true, profileId: 'office', serviceType: 'mihomo', save: vi.fn().mockResolvedValue(guard()), ...props },
    attachTo: document.body,
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
const button = (wrapper: VueWrapper, text: string) => wrapper.findAll('button').find(item => item.text().includes(text))!

beforeEach(() => {
  vi.mocked(agentApi.summaries).mockResolvedValue(response([agent('one')]))
  vi.mocked(profileApi.list).mockResolvedValue(response([{ id: 'office', name: '办公室', is_default: true }]))
  vi.mocked(agentApi.pushConfig).mockImplementation(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'succeeded' }))
  vi.mocked(agentApi.getIPv6Verification).mockResolvedValue(response({ success: true, verification: null }))
  vi.mocked(upstreamRouterApi.pending).mockImplementation(async id => response({ success: true, agent_id: id, agent_profile_id: 'office', routers: [pendingRouter] }))
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

describe('publish bar upstream router reminder', () => {
  it('appears only after a Mihomo draft is saved, and refreshes after each later save', async () => {
    vi.mocked(upstreamRouterApi.pending).mockImplementation(async id => response({ success: true, agent_id: id, agent_profile_id: 'office',
      routers: [{ ...pendingRouter, profile_id: 'test', profile_name: '测试空间' }, pendingRouter] }))
    const saveDraft = vi.fn().mockResolvedValue(guard())
    const wrapper = await render({ onSaveDraft: saveDraft })
    expect(wrapper.findComponent(UpstreamRouterPending).exists()).toBe(false)
    expect(upstreamRouterApi.pending).not.toHaveBeenCalled()
    await button(wrapper, '保存草稿').trigger('click')
    await flushPromises()
    expect(saveDraft).toHaveBeenCalledTimes(1)
    const reminder = wrapper.getComponent(UpstreamRouterPending)
    expect(reminder.props()).toMatchObject({ agentId: 'one', profileId: 'office', refreshKey: 1, compact: true })
    expect(wrapper.findAll('[data-publish-upstream-routers] a')).toHaveLength(1)
    expect(wrapper.get('[data-publish-upstream-routers]').text()).toContain('上游路由「主路由」有 1 项待发布')
    expect(wrapper.text()).not.toContain('IPv6 Fake-IP 分流路由：待添加')
    expect(wrapper.get('[data-publish-upstream-routers] a').attributes('href')).toBe('/routers?router=ros&profile=office&preview=1')
    await button(wrapper, '保存草稿').trigger('click')
    await flushPromises()
    expect(upstreamRouterApi.pending).toHaveBeenLastCalledWith('one', expect.objectContaining({ profileId: 'office', refresh: true }))
  })

  it('stays hidden when the draft was not saved, and for other software', async () => {
    const failed = await render({ onSaveDraft: vi.fn().mockResolvedValue(null) })
    await button(failed, '保存草稿').trigger('click')
    await flushPromises()
    expect(failed.findComponent(UpstreamRouterPending).exists()).toBe(false)
    const rejected = await render({ onSaveDraft: vi.fn().mockRejectedValue(new Error('invalid')) })
    await button(rejected, '保存草稿').trigger('click')
    await flushPromises()
    expect(rejected.findComponent(UpstreamRouterPending).exists()).toBe(false)
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent('dns', { service_type: 'oxidns' })]))
    const oxidns = await render({ serviceType: 'oxidns', save: vi.fn().mockResolvedValue(guard('oxidns')), onSaveDraft: vi.fn().mockResolvedValue(guard('oxidns')) })
    await button(oxidns, '保存草稿').trigger('click')
    await button(oxidns, '保存并发布').trigger('click')
    await flushPromises()
    expect(oxidns.findComponent(UpstreamRouterPending).exists()).toBe(false)
    expect(upstreamRouterApi.pending).not.toHaveBeenCalled()
  })

  it('appears after the save of 保存并发布 for the selected target', async () => {
    const wrapper = await render()
    await button(wrapper, '保存并发布').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(UpstreamRouterPending).props('agentId')).toBe('one')
    expect(wrapper.text()).toContain('上游路由「主路由」有 1 项待发布')
  })

  it('reminds for each Mihomo of the profile when no target is chosen', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent('one'), agent('two')]))
    const wrapper = await render({ onSaveDraft: vi.fn().mockResolvedValue(guard()) })
    await button(wrapper, '保存草稿').trigger('click')
    await flushPromises()
    expect(wrapper.findAllComponents(UpstreamRouterPending).map(item => item.props('agentId'))).toEqual(['one', 'two'])
  })

  it('still lets a parent without a result handle the draft itself', async () => {
    const handled = vi.fn()
    const wrapper = await render({ onSaveDraft: handled })
    await button(wrapper, '保存草稿').trigger('click')
    await flushPromises()
    expect(handled).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent(UpstreamRouterPending).exists()).toBe(false)
  })
})
