import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { agentApi, profileApi } from '@/api'
import MihomoIPv6Readiness from '@/components/generate/MihomoIPv6Readiness.vue'
import { Select } from '@/components/ui/select'
import type { MihomoIPv6Readiness as Readiness } from '@/types/mihomoIPv6'

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))
const response = (data: any) => ({ data } as any)
const gateway = { id: 'gw', name: '客厅 Mihomo', service_type: 'mihomo', profile_id: 'bound', status: 'online' }
const other = { ...gateway, id: 'other', name: '另一台 Mihomo' }
const report = (overrides: Partial<Readiness> = {}): Readiness => ({
  success: true, agent_id: 'gw', profile_id: 'bound', config_revision: 7,
  configuration: { ipv6: true, dns_ipv6: true, fake_ipv6_range: 'fc00::/64', errors: [] }, host: {},
  checks: [{ id: 'host_forwarding_ra', label: '网关信息', status: 'ok', message: '通过 RA 自动获得默认路由。' },
    { id: 'host_persistence', label: '重启恢复配置', status: 'warning', message: '需要保存 eth0 的 RA 设置。', details: ['将保存 eth0.accept_ra=2'] }],
  routers: [{ id: 'ros', name: '主路由', status: 'ok', can_prepare: false, checks: [{ id: 'fake_ipv6', label: 'IPv6 Fake-IP 路由', status: 'ok', message: '已有对应路由。' }] }],
  can_prepare: true, prepare_interface: 'eth0', check_token: 'verified-token',
  connectivity: { status: 'not_checked', message: '尚未实际验证节点连接。' }, ...overrides,
})
const wrappers: VueWrapper[] = []
const RowStub = defineComponent({ props: ['label', 'status', 'tone'], setup(props, { slots }) { return () => h('section', { 'data-check': props.label }, [props.label, props.status, slots.notice?.(), slots.default?.()]) } })
function deferred() {
  let resolve!: (data: any) => void
  const promise = new Promise<any>(done => { resolve = done })
  return { promise, resolve }
}
async function mount(props = {}) {
  const wrapper = shallowMount(MihomoIPv6Readiness, {
    props: { open: true, profileId: 'bound', preferredAgentId: 'gw', ...props },
    global: { stubs: { Button: false, Primitive: false, AgentStatusRow: RowStub, MihomoIPv6Verification: false, MihomoIPv6VerificationResult: false, MihomoIPv6CheckSummary: false } },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
const button = (wrapper: VueWrapper, label: string) => wrapper.findAll('button').find(item => item.text().includes(label))!
beforeEach(() => {
  vi.spyOn(agentApi, 'summaries').mockResolvedValue(response([gateway]))
  vi.spyOn(profileApi, 'list').mockResolvedValue(response([{ id: 'bound' }, { id: 'default', is_default: true }]))
  vi.spyOn(agentApi, 'ipv6Readiness').mockResolvedValue(response(report()))
  vi.spyOn(agentApi, 'prepareIPv6').mockResolvedValue(response(report({ can_prepare: false, check_token: 'after-prepare' })))
  vi.spyOn(agentApi, 'verifyIPv6').mockResolvedValue(response({ success: true, agent_id: 'gw', profile_id: 'bound', verification: {
    status: 'ok', scope: 'mihomo_runtime', config_revision: 7,
    checks: [...report().checks, ...report().routers.flatMap(router => router.checks.map(check => ({ ...check, id: `router_${router.id}_${check.id}`, label: `${router.name} · ${check.label}` }))), { id: 'runtime_proxy_http', label: '代理网站访问', status: 'ok', message: '已通过代理访问检测网站。' },
      { id: 'client_not_configured', label: '客户端全链路', status: 'unknown', message: '尚未配置独立检测客户端。' }],
  } }))
  vi.spyOn(agentApi, 'getIPv6Verification').mockResolvedValue(response({ success: true, agent_id: 'gw', profile_id: 'bound', verification: null }))
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks() })

describe('Mihomo per-device IPv6 workflow', () => {
  it('inspects the bound target without mutating the host or claiming node connectivity', async () => {
    const wrapper = await mount()
    expect(agentApi.ipv6Readiness).toHaveBeenCalledExactlyOnceWith('gw', 'bound', { refresh: false })
    expect(agentApi.prepareIPv6).not.toHaveBeenCalled()
    expect(agentApi.verifyIPv6).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('主路由 · IPv6 Fake-IP 路由')
    expect(wrapper.findAll('[data-ipv6-check-group]')).toHaveLength(2)
    expect(wrapper.text()).toContain('待验证的项目会提示下一步操作')
  })

  it('offers 「补齐缺少的配置」 only when the host misses a setting it writes', async () => {
    vi.mocked(agentApi.ipv6Readiness).mockResolvedValue(response(report({ checks: [
      { id: 'host_forwarding_ra', label: '网关信息', status: 'ok', message: '通过 RA 自动获得默认路由。' },
      { id: 'host_persistence', label: '重启恢复配置', status: 'ok', message: '已保存。' }] })))
    const wrapper = await mount()
    expect(button(wrapper, '补齐缺少的配置')).toBeUndefined()
    expect(wrapper.text()).not.toContain('补齐仅处理')
  })

  it('sends the inspected interface and plan token only after rechecking the same binding', async () => {
    const wrapper = await mount()
    await button(wrapper, '补齐缺少的配置').trigger('click')
    await flushPromises()
    expect(agentApi.summaries).toHaveBeenCalledTimes(2)
    expect(agentApi.prepareIPv6).toHaveBeenCalledExactlyOnceWith('gw', { profile_id: 'bound', interface: 'eth0', check_token: 'verified-token' })
    expect(button(wrapper, '补齐缺少的配置')).toBeUndefined()
    expect(wrapper.text()).toContain('补齐操作已完成，请查看检查结果。')
  })

  it('blocks an action when the selected device is rebound rather than silently choosing the remaining one', async () => {
    const wrapper = await mount()
    vi.mocked(agentApi.summaries).mockResolvedValue(response([{ ...gateway, profile_id: 'elsewhere' }, other]))
    await button(wrapper, '补齐缺少的配置').trigger('click')
    await flushPromises()
    expect(agentApi.prepareIPv6).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('原目标设备已不属于此配置空间')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('')
    expect(agentApi.ipv6Readiness).toHaveBeenCalledTimes(1)
  })

  it('keeps a missing entry target unselected even with one other candidate', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([other]))
    const wrapper = await mount()
    expect(agentApi.ipv6Readiness).not.toHaveBeenCalled()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('')
    expect(wrapper.text()).toContain('原目标设备')
  })

  it('shows the last check of an offline target and can refresh it without substituting another device', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([{ ...gateway, status: 'offline' }]))
    vi.mocked(agentApi.ipv6Readiness).mockResolvedValueOnce(response(report({ checked_at: new Date(Date.now() - 12 * 60000).toISOString(),
      cached: true, stale: true, refresh_error: { code: 'AGENT_OFFLINE', message: '设备离线，上线后重新检查', at: '' } })))
    const wrapper = await mount()
    // The server answers from its last result; an offline device is not contacted.
    expect(agentApi.ipv6Readiness).toHaveBeenCalledExactlyOnceWith('gw', 'bound', { refresh: false })
    expect(wrapper.get('[data-ipv6-readiness-recheck-failed]').text()).toBe('这次未能重新检查：设备离线，上线后重新检查。以下是设备或配置变化前的结果。')
    expect(wrapper.get('[data-ipv6-readiness-checked]').text()).toContain('检查于 12 分钟前')
    expect(button(wrapper, '补齐缺少的配置').attributes('disabled')).toBeDefined()
    await button(wrapper, '重新读取设备').trigger('click')
    await flushPromises()
    expect(agentApi.ipv6Readiness).toHaveBeenCalledTimes(2)
    expect(agentApi.ipv6Readiness).toHaveBeenLastCalledWith('gw', 'bound', { refresh: false })
  })

  it('reuses the last check on opening, says when it was taken, and checks again on request', async () => {
    vi.mocked(agentApi.ipv6Readiness).mockResolvedValueOnce(response(report({ checked_at: new Date(Date.now() - 3 * 3600000).toISOString(), cached: true })))
    const wrapper = await mount()
    expect(wrapper.get('[data-ipv6-readiness-checked]').text()).toBe('检查于 3 小时前。路由器或主机上的手动修改不会自动发现，请点「重新检查」。')
    expect(wrapper.find('[data-ipv6-readiness-recheck-failed]').exists()).toBe(false)
    vi.mocked(agentApi.ipv6Readiness).mockResolvedValueOnce(response(report({ checked_at: new Date().toISOString(), cached: false })))
    await button(wrapper, '重新检查').trigger('click')
    await flushPromises()
    expect(agentApi.ipv6Readiness).toHaveBeenLastCalledWith('gw', 'bound', { refresh: true })
    expect(wrapper.get('[data-ipv6-readiness-checked]').text()).toBe('刚刚检查。路由器或主机上的手动修改不会自动发现，请点「重新检查」。')
  })

  it('requires an explicit choice when multiple devices belong to the profile', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([gateway, other]))
    const wrapper = await mount({ preferredAgentId: undefined })
    expect(agentApi.ipv6Readiness).not.toHaveBeenCalled()
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'gw')
    await flushPromises()
    expect(agentApi.ipv6Readiness).toHaveBeenCalledExactlyOnceWith('gw', 'bound', { refresh: false })
  })

  it('invalidates a check when the YAML draft changes and requires a new check after saving', async () => {
    const wrapper = await mount()
    await wrapper.setProps({ dirty: true })
    expect(wrapper.text()).toContain('请先保存草稿')
    expect(button(wrapper, '补齐缺少的配置')).toBeUndefined()
    expect(button(wrapper, '重新检查').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ dirty: false })
    expect(button(wrapper, '补齐缺少的配置')).toBeUndefined()
    await button(wrapper, '重新检查').trigger('click')
    await flushPromises()
    expect(agentApi.ipv6Readiness).toHaveBeenCalledTimes(2)
    expect(agentApi.ipv6Readiness).toHaveBeenLastCalledWith('gw', 'bound', { refresh: true })
  })

  it('does not inspect an unsaved draft when opening the panel', async () => {
    const wrapper = await mount({ dirty: true })
    expect(agentApi.ipv6Readiness).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('有未保存的修改')
  })

  it('drops delayed results after switching profiles', async () => {
    const pending = deferred()
    vi.mocked(agentApi.ipv6Readiness).mockReturnValueOnce(pending.promise)
    const wrapper = await mount()
    await wrapper.setProps({ profileId: 'other-profile' })
    pending.resolve(response(report()))
    await flushPromises()
    expect(wrapper.text()).not.toContain('通过 RA 自动获得默认路由')
    expect(button(wrapper, '补齐缺少的配置')).toBeUndefined()
  })

  it('rejects mismatched response identity and cannot prepare from it', async () => {
    vi.mocked(agentApi.ipv6Readiness).mockResolvedValue(response(report({ agent_id: 'other' })))
    const wrapper = await mount()
    expect(wrapper.get('[role="alert"]').text()).toContain('不一致')
    expect(button(wrapper, '补齐缺少的配置')).toBeUndefined()
  })

  it('invalidates the old plan on failed preparation', async () => {
    const wrapper = await mount()
    vi.mocked(agentApi.prepareIPv6).mockRejectedValue(new Error('预检已过期，请重新检查'))
    await button(wrapper, '补齐缺少的配置').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('预检已过期')
    expect(button(wrapper, '补齐缺少的配置')).toBeUndefined()
  })

  it('runs explicit connectivity verification independently of readiness', async () => {
    const wrapper = await mount()
    await button(wrapper, '验证连接与分流').trigger('click')
    await flushPromises()
    expect(agentApi.verifyIPv6).toHaveBeenCalledExactlyOnceWith('gw', { profile_id: 'bound', expected_config_revision: 7 })
    expect(agentApi.prepareIPv6).not.toHaveBeenCalled()
    // No test client: only 「IPv6 网络」 is shown, and the older node test is ignored.
    expect(wrapper.text()).not.toMatch(/未配置|节点/)
    expect(wrapper.text()).not.toContain('主路由 · IPv6 Fake-IP 路由')
    expect(wrapper.findAll('[data-ipv6-check-group]')).toHaveLength(1)
    expect(button(wrapper, '补齐缺少的配置')).toBeDefined()
  })

  it('keeps one preparation in flight and ignores a delayed read after unmounting', async () => {
    const wrapper = await mount()
    const pending = deferred()
    vi.mocked(agentApi.prepareIPv6).mockReturnValueOnce(pending.promise)
    await button(wrapper, '补齐缺少的配置').trigger('click')
    await flushPromises()
    expect(agentApi.prepareIPv6).toHaveBeenCalledTimes(1)
    expect(button(wrapper, '补齐缺少的配置')).toBeUndefined()
    expect(wrapper.text()).toContain('正在补齐缺少的配置')
    wrapper.unmount()
    pending.resolve(response(report({ message: 'delayed result' })))
    await flushPromises()
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([false])
  })
})
