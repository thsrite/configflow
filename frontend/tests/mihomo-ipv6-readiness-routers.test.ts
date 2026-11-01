import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { agentApi, profileApi } from '@/api'
import MihomoIPv6Readiness from '@/components/generate/MihomoIPv6Readiness.vue'
import UpstreamRouterPending from '@/components/routers/UpstreamRouterPending.vue'
import type { MihomoIPv6Readiness as Readiness } from '@/types/mihomoIPv6'

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))
const response = (data: any) => ({ data } as any)
const gateway = { id: 'gw', name: '客厅 Mihomo', service_type: 'mihomo', profile_id: 'bound', status: 'online' }
const report = (overrides: Partial<Readiness> = {}): Readiness => ({
  success: true, agent_id: 'gw', profile_id: 'bound', config_revision: 7,
  configuration: { ipv6: true, dns_ipv6: true, fake_ipv6_range: 'fc00::/64', errors: [] }, host: {},
  checks: [{ id: 'host_persistence', label: '重启恢复配置', status: 'warning', message: '需要保存 eth0 的 RA 设置。' }],
  routers: [], can_prepare: true, prepare_interface: 'eth0', check_token: 'token',
  connectivity: { status: 'not_checked', message: '尚未实际验证节点连接。' }, ...overrides,
})
const wrappers: VueWrapper[] = []
async function mount(props = {}) {
  const wrapper = shallowMount(MihomoIPv6Readiness, {
    props: { open: true, profileId: 'bound', preferredAgentId: 'gw', ...props },
    global: { stubs: { Button: false, Primitive: false } },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
beforeEach(() => {
  vi.spyOn(agentApi, 'summaries').mockResolvedValue(response([gateway]))
  vi.spyOn(profileApi, 'list').mockResolvedValue(response([{ id: 'bound' }, { id: 'default', is_default: true }]))
  vi.spyOn(agentApi, 'ipv6Readiness').mockResolvedValue(response(report()))
  vi.spyOn(agentApi, 'prepareIPv6').mockResolvedValue(response(report({ can_prepare: false })))
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks() })

describe('IPv6 check window upstream router reminder', () => {
  it('replaces the fixed router hint with the live reminder for the checked device', async () => {
    const wrapper = await mount()
    expect(wrapper.text()).toContain('补齐仅处理 Mihomo 主机配置，必要时重新加载 Mihomo。')
    expect(wrapper.text()).not.toContain('上游路由')
    const reminder = wrapper.getComponent(UpstreamRouterPending)
    expect(reminder.props()).toMatchObject({ agentId: 'gw', profileId: 'bound', refreshKey: 1 })
    expect(reminder.props('compact')).toBeFalsy()
  })

  it('refreshes after every check and hides while the draft has unsaved changes', async () => {
    const wrapper = await mount()
    await wrapper.findAll('button').find(item => item.text().includes('重新检查'))!.trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(UpstreamRouterPending).props('refreshKey')).toBe(2)
    await wrapper.setProps({ dirty: true })
    expect(wrapper.findComponent(UpstreamRouterPending).exists()).toBe(false)
  })

  it('waits for a check before reading routers', async () => {
    vi.mocked(agentApi.ipv6Readiness).mockReturnValue(new Promise(() => {}) as any)
    const wrapper = await mount()
    expect(wrapper.findComponent(UpstreamRouterPending).exists()).toBe(false)
  })
})
