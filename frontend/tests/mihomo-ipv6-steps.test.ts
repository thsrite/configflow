import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { RouterLink } from 'vue-router'
import { agentApi, profileApi, routerApi } from '@/api'
import MihomoIPv6Steps from '@/components/generate/MihomoIPv6Steps.vue'
import MihomoIPv6Verification from '@/components/generate/MihomoIPv6Verification.vue'
import UpstreamRouterPending from '@/components/routers/UpstreamRouterPending.vue'
import { upstreamRouterApi } from '@/api/upstreamRouterPending'
import { Select } from '@/components/ui/select'
import { buildMihomoIPv6Summary } from '@/lib/mihomoIPv6Summary'
import { firewallStep, hostStep, oxidnsStep, publishStep, stepOrder, upstreamStep, verifyStep } from '@/lib/mihomoIPv6Steps'
import type { IPv6ReadinessCheck, MihomoIPv6Readiness as Readiness } from '@/types/mihomoIPv6'

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))
vi.mock('@/lib/clipboard', () => ({ copyText: vi.fn().mockResolvedValue(undefined) }))
vi.mock('@/api/upstreamRouterPending', () => ({ upstreamRouterApi: { pending: vi.fn() } }))

const response = (data: any) => ({ data } as any)
const gateway = { id: 'gw', name: '客厅 Mihomo', service_type: 'mihomo', profile_id: 'bound', status: 'online' }
const dnsAgent = { id: 'dns', name: '旁路 OxiDNS', service_type: 'oxidns', profile_id: 'bound', status: 'online' }
const ok = (id: string, label = id): IPv6ReadinessCheck => ({ id, label, status: 'ok', message: `${label} 通过` })
const command = '/ipv6 firewall filter add chain=forward action=accept in-interface=bridge out-interface=bridge comment="LAN to Mihomo via router" place-before=*5'
const hairpinWarning = 'IPv6 防火墙会丢弃同口转发的后续包：设备经 ROS 同一 LAN 口转给 Mihomo，回包不经过 ROS，新连接可能卡几秒甚至超时。'
const readiness = (overrides: Partial<Readiness> = {}): Readiness => ({
  success: true, agent_id: 'gw', profile_id: 'bound', config_revision: 7,
  configuration: { ipv6: true, dns_ipv6: true, fake_ipv6_range: 'fc00::/64', errors: [] },
  host: { core: { ipv6: true, 'dns.ipv6': true } },
  checks: [ok('configuration'), ok('host_address'), ok('host_default_route'), ok('host_forwarding_ra')],
  routers: [{ id: 'ros', name: '主路由', status: 'ok', can_prepare: false, checks: [ok('fake_ipv6'), { ...ok('fake6_0'), decision: 'allow' }] }],
  can_prepare: false, prepare_interface: 'eth0', check_token: 'token',
  connectivity: { status: 'not_checked', message: '尚未检查' }, ...overrides,
})
const hinted = () => readiness({ routers: [{ id: 'ros', name: '主路由', status: 'warning', can_prepare: false, checks: [ok('fake_ipv6'),
  { id: 'forward_hairpin', label: 'IPv6 同口转发', status: 'warning', message: hairpinWarning, command }] }] })
const context = (overrides = {}) => ({ dirty: false, checking: false, readiness: readiness(), ...overrides })
const passedVerification = {
  status: 'ok' as const, scope: 'client_path' as const, config_revision: 7,
  checks: ['host_address', 'host_default_route', 'runtime_proxy_http', 'runtime_node_ipv6_transport',
    'client_http_ipv4', 'client_mihomo_path', 'client_ros_path', 'client_ipv6_transport'].map(id => ok(id)),
}

describe('IPv6 step derivation', () => {
  it('waits for a saved configuration and a device before claiming any step', () => {
    for (const make of [hostStep, publishStep] as const) {
      expect(make(context({ readiness: null }))).toMatchObject({ status: '待检查', tone: 'neutral' })
      expect(make(context({ readiness: null, checking: true }))).toMatchObject({ status: '检查中', tone: 'progress' })
    }
    expect(publishStep(context({ dirty: true }))).toMatchObject({ status: '待保存', note: expect.stringContaining('保存并发布') })
    expect(firewallStep(context({ dirty: true }), [], false)).toMatchObject({ status: '保存后检查', tone: 'neutral' })
    expect(verifyStep(context({ dirty: true }), [])).toMatchObject({ status: '保存后检查' })
  })

  it('takes host preparation from the existing host checks and their prepare hint', () => {
    expect(hostStep(context())).toMatchObject({ status: '已就绪', tone: 'success', issues: [] })
    const missing = readiness({ can_prepare: true, checks: [{ id: 'host_forwarding_ra', label: '转发与 RA', status: 'warning', message: '未满足' }] })
    expect(hostStep(context({ readiness: missing }))).toEqual(expect.objectContaining({ status: '需处理', tone: 'warning',
      issues: ['IPv6 转发和自动获取网关设置未齐全，可点击「补齐缺少的配置」。'] }))
    const failed = readiness({ checks: [{ id: 'host_ipv6_enabled', label: '网卡 IPv6', status: 'error', message: '上联网卡禁用了 IPv6' }] })
    expect(hostStep(context({ readiness: failed }))).toMatchObject({ status: '未通过', tone: 'danger', issues: ['上联网卡禁用了 IPv6'] })
    // Router checks and the saved-config summary are other steps.
    const routerOnly = readiness({ checks: [{ id: 'deployed_config', label: '设备上的配置', status: 'warning', message: '不同' }] })
    expect(hostStep(context({ readiness: routerOnly })).status).toBe('已就绪')
  })

  it('reports publication from the deployed-config comparison', () => {
    expect(publishStep(context())).toMatchObject({ status: '已发布', tone: 'success' })
    const pending = readiness({ checks: [{ id: 'deployed_config', label: '设备上的配置', status: 'warning', message: '已保存草稿与设备配置不同：DNS IPv6。请通过保存并发布应用配置。' }] })
    expect(publishStep(context({ readiness: pending }))).toMatchObject({ status: '待发布', issues: ['已保存草稿与设备配置不同：DNS IPv6。请通过保存并发布应用配置。'] })
    expect(publishStep(context({ readiness: readiness({ host: {} }) }))).toMatchObject({ status: '无法确认', tone: 'neutral' })
    expect(publishStep(context({ readiness: readiness({ host: { core: { error: '无法读取' } } }) })).status).toBe('无法确认')
  })

  it('reports any other Mihomo change from the server comparison of the delivered files', () => {
    // A router publication or another service's settings only move the revision: still published.
    for (const publication of ['synced', 'unknown', undefined] as const) {
      expect(publishStep(context({ readiness: readiness({ publication }) }))).toMatchObject({ status: '已发布', tone: 'success' })
    }
    expect(publishStep(context({ readiness: readiness({ publication: 'changed' }) }))).toMatchObject({
      status: '待发布', tone: 'neutral', issues: ['已保存的配置还没有发布到这台 Mihomo，请使用底部「保存并发布」。'] })
    expect(publishStep(context({ readiness: readiness({ publication: 'checking' }) }))).toMatchObject({ status: '核对中', tone: 'progress' })
    // The device's own IPv6 values stay the more specific reason.
    const both = readiness({ publication: 'changed', checks: [{ id: 'deployed_config', label: '设备上的配置', status: 'warning', message: '已保存草稿与设备配置不同：DNS IPv6。' }] })
    expect(publishStep(context({ readiness: both })).issues).toEqual(['已保存草稿与设备配置不同：DNS IPv6。'])
  })

  it('maps the upstream reminder states', () => {
    expect(upstreamStep(null, false)).toMatchObject({ status: '检查中', tone: 'progress' })
    expect(upstreamStep({ state: 'synced', pending: 0 }, false)).toMatchObject({ status: '已同步', tone: 'success', issues: [] })
    expect(upstreamStep({ state: 'pending', pending: 2 }, false)).toMatchObject({ status: '2 项待处理', tone: 'neutral' })
    expect(upstreamStep({ state: 'none', pending: 0 }, false)).toMatchObject({ status: '未关联上游路由', tone: 'warning' })
    // Clients on IPv4 need no router: nothing to fix, nothing to publish.
    expect(upstreamStep({ state: 'none', pending: 0 }, false, 'nodes')).toMatchObject({ status: '未关联上游路由', tone: 'neutral', issues: [] })
    expect(upstreamStep({ state: 'pending', pending: 1 }, false, 'nodes')).toMatchObject({ status: '1 项待处理' })
    expect(upstreamStep({ state: 'unknown', pending: 0 }, false).status).toBe('无法确认')
    expect(upstreamStep({ state: 'synced', pending: 0 }, true)).toMatchObject({ status: '保存后核对' })
  })

  it('shows no router work when the device check finds none', () => {
    expect(firewallStep(context())).toMatchObject({ status: '无需处理', tone: 'success', issues: [] })
    expect(firewallStep(context({ readiness: readiness({ routers: [] }) }))).toMatchObject({ status: '未关联上游路由' })
  })

  it('explains the same-port forward drop with the backend command, once per command', () => {
    const data = hinted()
    data.routers.push({ ...data.routers[0], id: 'ros2', name: '备用路由' })
    const result = firewallStep(context({ readiness: data }))
    expect(result).toMatchObject({ status: '需处理', tone: 'warning', issues: [`「主路由」：${hairpinWarning}`, `「备用路由」：${hairpinWarning}`] })
    expect(result.commands).toEqual([{ label: '在 RouterOS 终端执行（主路由）', command }])
    expect(result.note).toContain('ConfigFlow 不修改防火墙和 NAT')
  })

  it('names the NAT and forward-drop findings of the client Fake-IP path', () => {
    const routers = [{ id: 'ros', name: '主路由', status: 'warning', can_prepare: false, checks: [
      { id: 'fake6_0', label: '去程与 NAT', status: 'warning', decision: 'translate', message: '会改写来源地址' },
      { id: 'fake6_1', label: '去程与 NAT', status: 'error', decision: 'drop', message: '命中阻断' },
      // A rule-order guess about the return path is not a definite blocker.
      { id: 'fake6_0_return', label: '返回', status: 'warning', decision: 'drop', message: '' },
    ] as IPv6ReadinessCheck[] }]
    const result = firewallStep(context({ readiness: readiness({ routers }) }))
    expect(result.status).toBe('需处理')
    expect(result.issues[0]).toContain('IPv6 NAT 会改写客户端发往 Mihomo 的连接')
    expect(result.issues[0]).toContain('masquerade')
    expect(result.issues[1]).toContain('IPv6 防火墙会阻断')
    expect(result.issues).toHaveLength(2)
  })

  it('keeps unreadable or manual routers unconfirmed rather than passed', () => {
    const routers = [{ id: 'ikuai', name: '爱快', status: 'warning', can_prepare: false, checks: [{ id: 'manual', label: '路由器 IPv6', status: 'unknown' as const, message: '此型号请手工核对 IPv6 路由与 DNS' }] },
      { id: 'ros', name: '主路由', status: 'warning', can_prepare: false, checks: [{ id: 'forward_hairpin', label: 'IPv6 同口转发', status: 'unknown' as const, message: '未能读取 RouterOS IPv6 防火墙' }] }]
    const result = firewallStep(context({ readiness: readiness({ routers }) }))
    expect(result).toMatchObject({ status: '无法确认', tone: 'neutral' })
    expect(result.issues).toEqual(['「爱快」：此型号请手工核对 IPv6 路由与 DNS。', '「主路由」：未能读取 RouterOS IPv6 防火墙'])
  })

  it('only hints at OxiDNS settings when a linked router uses OxiDNS', () => {
    expect(oxidnsStep([])).toBeNull()
    const result = oxidnsStep([{ router: '主路由', agent: '旁路 OxiDNS' }])!
    expect(result).toMatchObject({ status: '请核对', tone: 'neutral', issues: ['「主路由」的客户端 DNS 使用 OxiDNS「旁路 OxiDNS」。'] })
    expect(result.note).toContain('优先 IPv4')
    expect(result.note).toContain('不会修改')
  })

  it('summarizes acceptance from the existing three results', () => {
    const data = readiness()
    expect(verifyStep(context(), null)).toMatchObject({ status: '待检查' })
    expect(verifyStep(context(), buildMihomoIPv6Summary({ readiness: data }))).toMatchObject({ status: '待验证', tone: 'neutral' })
    expect(verifyStep(context(), buildMihomoIPv6Summary({ readiness: data, verification: passedVerification }))).toMatchObject({ status: '已通过', tone: 'success' })
    expect(verifyStep(context(), buildMihomoIPv6Summary({ readiness: data }), true)).toMatchObject({ status: '验证中', tone: 'progress' })
  })

  it('orders the steps per mode: both publish routes upstream and share the firewall hint', () => {
    expect(stepOrder('nodes', true)).toEqual(['host', 'publish', 'upstream', 'firewall', 'verify'])
    expect(stepOrder('clients', false)).toEqual(['host', 'publish', 'upstream', 'firewall', 'verify'])
    expect(stepOrder('clients', true)).toEqual(['host', 'publish', 'upstream', 'firewall', 'oxidns', 'verify'])
  })

  it('does not fault a published Fake-IP route that clients do not use yet', () => {
    // 「Mihomo 用 IPv6，客户端走 IPv4」: dns.ipv6 off, the route is there; only the forward hint remains.
    const data = readiness({ configuration: { ipv6: true, dns_ipv6: false, fake_ipv6_range: 'fc00::/64', errors: [] },
      routers: [{ id: 'ros', name: '主路由', status: 'warning', can_prepare: false, checks: [
        { ...ok('fake_ipv6'), message: 'fc00::/64 已指向这台 Mihomo，沿用现有路由；DNS IPv6 关闭，客户端暂不使用此网段' },
        { id: 'forward_hairpin', label: 'IPv6 同口转发', status: 'warning', message: hairpinWarning, command }] }] })
    expect(firewallStep(context({ readiness: data }))).toMatchObject({ status: '需处理', commands: [{ command }] })
    const client = buildMihomoIPv6Summary({ readiness: data }).find(group => group.id === 'client')!
    expect(client.tone).not.toBe('danger')
    expect(client.issues.join('')).not.toContain('分流路由')
  })
})

const pendingResponse = (routers: any[]) => ({ success: true, agent_id: 'gw', agent_profile_id: 'bound', checked_at: '', cached: true, routers })
const waitingRouter = (state: string, extra = {}) => ({ id: 'ros', name: '主路由', type: 'routeros', profile_id: 'bound', profile_name: '默认', state,
  pending: state === 'pending' ? 2 : 0, items: [], message: state === 'pending' ? '' : '下一跳未确认', ...extra })
const RowStub = defineComponent({ props: ['label', 'status', 'tone'], setup(props, { slots }) {
  return () => h('section', { 'data-row': props.label, 'data-tone': props.tone }, [h('span', props.label), h('span', { 'data-status': '' }, props.status), slots.notice?.()])
} })
const wrappers: VueWrapper[] = []
async function mount(props = {}, slots = false) {
  const wrapper = shallowMount(MihomoIPv6Steps, {
    props: { open: true, profileId: 'bound', preferredAgentId: 'gw', mode: 'clients', dirty: false, ...props },
    global: { renderStubDefaultSlot: slots, stubs: { Button: false, Primitive: false, AgentStatusRow: RowStub } },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
const row = (wrapper: VueWrapper, id: string) => wrapper.get(`[data-ipv6-step="${id}"]`)
const status = (wrapper: VueWrapper, id: string) => row(wrapper, id).get('[data-status]').text()
const shown = (wrapper: VueWrapper, id: string) => (row(wrapper, id).element as HTMLElement).style.display !== 'none'

beforeEach(() => {
  vi.spyOn(agentApi, 'summaries').mockResolvedValue(response([gateway, dnsAgent]))
  vi.spyOn(profileApi, 'list').mockResolvedValue(response([{ id: 'bound' }, { id: 'default', is_default: true }]))
  vi.spyOn(agentApi, 'ipv6Readiness').mockResolvedValue(response(hinted()))
  vi.spyOn(routerApi, 'list').mockResolvedValue(response({ success: true, routers: [{ id: 'ros', name: '主路由', proxy_gateway_agent_id: 'gw', dns_agent_id: 'dns' }] }))
  vi.mocked(upstreamRouterApi.pending).mockReset().mockResolvedValue(response(pendingResponse([])))
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks() })

describe('Mihomo IPv6 step list', () => {
  it('checks the fixed device once and never reads a router report of its own', async () => {
    const wrapper = await mount()
    expect(agentApi.ipv6Readiness).toHaveBeenCalledExactlyOnceWith('gw', 'bound', { refresh: false })
    // The firewall step uses the device check; only the saved router list is read for OxiDNS.
    expect(routerApi.list).toHaveBeenCalledExactlyOnceWith('bound')
    expect(wrapper.findAll('[data-ipv6-step]').map(item => item.attributes('data-ipv6-step'))).toEqual(['host', 'publish', 'upstream', 'firewall', 'oxidns', 'verify'])
    expect(status(wrapper, 'host')).toBe('已就绪')
    expect(status(wrapper, 'publish')).toBe('已发布')
    expect(status(wrapper, 'firewall')).toBe('需处理')
    expect(row(wrapper, 'firewall').get('pre').text()).toBe(command)
    expect(row(wrapper, 'oxidns').text()).toContain('OxiDNS「旁路 OxiDNS」')
    expect(row(wrapper, 'oxidns').text()).toContain('优先 IPv4')
    expect(row(wrapper, 'oxidns').getComponent(RouterLink).props('to')).toBe('/agents')
    expect(wrapper.text()).toContain('设备：客厅 Mihomo')
  })

  it('copies the backend command without assembling one', async () => {
    const { copyText } = await import('@/lib/clipboard')
    const wrapper = await mount()
    await row(wrapper, 'firewall').get('[aria-label="复制 IPv6 同口转发放行命令"]').trigger('click')
    expect(copyText).toHaveBeenCalledWith(command)
  })

  it('lists the router steps for clients on IPv4 too, without the OxiDNS hint or the router list', async () => {
    const wrapper = await mount({ mode: 'nodes' })
    expect(routerApi.list).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-ipv6-step]').map(item => item.attributes('data-ipv6-step'))).toEqual(['host', 'publish', 'upstream', 'firewall', 'verify'])
    expect(['upstream', 'firewall'].every(id => shown(wrapper, id))).toBe(true)
    // The same-port forward hint applies to the routes published in this mode.
    expect(status(wrapper, 'firewall')).toBe('需处理')
    expect(row(wrapper, 'firewall').get('pre').text()).toBe(command)
    wrapper.getComponent(UpstreamRouterPending).vm.$emit('status', { state: 'pending', pending: 4 })
    await flushPromises()
    expect(status(wrapper, 'upstream')).toBe('4 项待处理')
  })

  it('keeps the reminder hidden, follows its status and links to the waiting router', async () => {
    const wrapper = await mount({}, true)
    const pending = wrapper.getComponent(UpstreamRouterPending)
    expect(pending.props()).toMatchObject({ agentId: 'gw', profileId: 'bound', compact: true })
    // Its own lines belong to the publish bar and the device check, never twice on one screen.
    expect(wrapper.get('[data-ipv6-step-upstream-reminder]').classes()).toContain('hidden')
    pending.vm.$emit('status', { state: 'synced', pending: 0 })
    await flushPromises()
    expect(status(wrapper, 'upstream')).toBe('已同步')
    expect(wrapper.find('[data-ipv6-step-upstream]').exists()).toBe(false)
    expect(upstreamRouterApi.pending).not.toHaveBeenCalled()
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(response(pendingResponse([waitingRouter('pending'), { ...waitingRouter('synced'), id: 'other' }])))
    pending.vm.$emit('status', { state: 'pending', pending: 2 })
    await flushPromises()
    expect(status(wrapper, 'upstream')).toBe('2 项待处理')
    // Read without refresh: the reminder's read is reused, no router is contacted again.
    expect(upstreamRouterApi.pending).toHaveBeenCalledExactlyOnceWith('gw', { profileId: 'bound' })
    const link = wrapper.get('[data-ipv6-step-upstream]').getComponent(RouterLink)
    expect(link.props('to')).toEqual({ path: '/routers', query: { router: 'ros', profile: 'bound', preview: '1' } })
    expect(link.text()).toBe('「主路由」查看并发布')
    vi.mocked(upstreamRouterApi.pending).mockRejectedValue(new Error('offline'))
    pending.vm.$emit('status', { state: 'unknown', pending: 0 })
    await flushPromises()
    expect(status(wrapper, 'upstream')).toBe('无法确认')
    expect(wrapper.get('[data-ipv6-step-upstream]').getComponent(RouterLink).props('to')).toBe('/routers')
    pending.vm.$emit('status', { state: 'none', pending: 0 })
    await flushPromises()
    expect(status(wrapper, 'upstream')).toBe('未关联上游路由')
    expect(row(wrapper, 'upstream').getComponent(RouterLink).props('to')).toBe('/routers')
  })

  it('refreshes the reminder after a save, a publish or a manual refresh but not after the device check', async () => {
    const wrapper = await mount()
    const pending = wrapper.getComponent(UpstreamRouterPending)
    const key = pending.props('refreshKey')
    const readiness = vi.mocked(agentApi.ipv6Readiness)
    await wrapper.setProps({ recheckKey: 1 })
    await flushPromises()
    // Closing the device check: the server's last result, which that window may have renewed.
    expect(readiness).toHaveBeenCalledTimes(2)
    expect(readiness).toHaveBeenLastCalledWith('gw', 'bound', { refresh: false })
    expect(pending.props('refreshKey')).toBe(key)
    // A confirmed publish changed the device: check it again.
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    expect(readiness).toHaveBeenCalledTimes(3)
    expect(readiness).toHaveBeenLastCalledWith('gw', 'bound', { refresh: true })
    expect(pending.props('refreshKey')).not.toBe(key)
    const published = pending.props('refreshKey')
    // A save: the server checks again only if the saved content changed.
    await wrapper.setProps({ dirty: true })
    await wrapper.setProps({ dirty: false })
    await flushPromises()
    expect(readiness).toHaveBeenLastCalledWith('gw', 'bound', { refresh: false })
    expect(pending.props('refreshKey')).not.toBe(published)
    const saved = pending.props('refreshKey')
    await wrapper.get('[data-ipv6-steps-recheck]').trigger('click')
    await flushPromises()
    expect(readiness).toHaveBeenLastCalledWith('gw', 'bound', { refresh: true })
    expect(pending.props('refreshKey')).not.toBe(saved)
  })

  it('shows when the device and the routers were last checked and how to catch manual edits', async () => {
    vi.mocked(agentApi.ipv6Readiness).mockResolvedValue(response({ ...hinted(), cached: true, checked_at: new Date(Date.now() - 12 * 60000).toISOString() }))
    const wrapper = await mount()
    const line = () => wrapper.get('[data-ipv6-steps-checked]').text()
    expect(line()).toBe('设备检查于 12 分钟前。路由器或主机上的手动修改不会自动发现，请点「重新检查」。')
    wrapper.getComponent(UpstreamRouterPending).vm.$emit('status', { state: 'synced', pending: 0, checkedAt: new Date(Date.now() - 3 * 60000).toISOString() })
    await flushPromises()
    expect(line()).toContain('设备检查于 12 分钟前，上游路由读取于 3 分钟前。')
    expect(wrapper.get('[data-ipv6-steps-recheck]').text()).toBe('重新检查')
    await wrapper.setProps({ dirty: true })
    expect(wrapper.find('[data-ipv6-steps-checked]').exists()).toBe(false)
  })

  it('keeps the last result when a new check fails and says so', async () => {
    vi.mocked(agentApi.ipv6Readiness).mockResolvedValue(response({ ...hinted(), cached: true, stale: false,
      checked_at: new Date(Date.now() - 2 * 3600000).toISOString(),
      refresh_error: { code: 'AGENT_UNREACHABLE', message: '无法连接 Agent 进行 IPv6 检查', at: new Date().toISOString() } }))
    const wrapper = await mount()
    expect(wrapper.get('[data-ipv6-steps-recheck-failed]').text()).toBe('这次未能重新检查：无法连接 Agent 进行 IPv6 检查。以下是上次的结果。')
    expect(wrapper.get('[data-ipv6-steps-checked]').text()).toContain('设备检查于 2 小时前')
    expect(status(wrapper, 'host')).toBe('已就绪')
    expect(status(wrapper, 'firewall')).toBe('需处理')
  })

  it('counts and links only routers in the editor profile, including with duplicate router IDs', async () => {
    vi.mocked(upstreamRouterApi.pending).mockResolvedValue(response(pendingResponse([
      waitingRouter('blocked', { profile_id: 'test', profile_name: '测试空间' }), waitingRouter('blocked'),
    ])))
    const wrapper = shallowMount(MihomoIPv6Steps, {
      props: { open: true, profileId: 'bound', preferredAgentId: 'gw', mode: 'clients', dirty: false },
      global: { renderStubDefaultSlot: true, stubs: { Button: false, Primitive: false, AgentStatusRow: RowStub, UpstreamRouterPending: false } },
    })
    wrappers.push(wrapper)
    await flushPromises()
    await flushPromises()
    expect(upstreamRouterApi.pending).toHaveBeenNthCalledWith(1, 'gw', expect.objectContaining({ profileId: 'bound', refresh: false }))
    expect(status(wrapper, 'upstream')).toBe('1 项待处理')
    expect(wrapper.get('[data-ipv6-step-upstream-reminder]').text()).toContain('有改动暂时无法发布')
    expect(wrapper.get('[data-ipv6-step-upstream-reminder]').classes()).toContain('hidden')
    expect(wrapper.get('[data-ipv6-step-upstream]').getComponent(RouterLink).text()).toBe('「主路由」查看原因')
    const links = wrapper.get('[data-ipv6-step-upstream]').findAllComponents(RouterLink)
    expect(links).toHaveLength(1)
    expect(links[0].props('to')).toEqual({ path: '/routers', query: { router: 'ros', profile: 'bound', preview: '1' } })
  })

  it('collapses acceptance to one line once the embedded verification passes', async () => {
    const wrapper = await mount()
    const verifier = wrapper.getComponent(MihomoIPv6Verification)
    expect(verifier.props()).toMatchObject({ open: true, agentId: 'gw', profileId: 'bound', configRevision: 7 })
    verifier.vm.$emit('result', { verification: null, fresh: false })
    await flushPromises()
    expect(status(wrapper, 'verify')).toBe('待验证')
    expect((wrapper.get('[data-ipv6-step-verify]').element as HTMLElement).style.display).not.toBe('none')
    verifier.vm.$emit('result', { verification: { ...passedVerification, status: 'running' }, fresh: true })
    await flushPromises()
    expect(status(wrapper, 'verify')).toBe('验证中')
    verifier.vm.$emit('result', { verification: passedVerification, fresh: true })
    await flushPromises()
    expect(status(wrapper, 'verify')).toBe('已通过')
    expect((wrapper.get('[data-ipv6-step-verify]').element as HTMLElement).style.display).toBe('none')
  })

  it('reads only saved configurations: waits while dirty and checks again after saving', async () => {
    const wrapper = await mount({ dirty: true })
    expect(agentApi.ipv6Readiness).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('保存草稿后自动重新检查')
    expect(status(wrapper, 'publish')).toBe('待保存')
    expect(status(wrapper, 'upstream')).toBe('保存后核对')
    expect(wrapper.findComponent(MihomoIPv6Verification).exists()).toBe(false)
    await wrapper.setProps({ dirty: false })
    await flushPromises()
    // The saved content is part of the server's key: a changed draft is checked anew.
    expect(agentApi.ipv6Readiness).toHaveBeenCalledExactlyOnceWith('gw', 'bound', { refresh: false })
    expect(status(wrapper, 'publish')).toBe('已发布')
  })

  it('follows the background comparison of Mihomo files without checking the device again', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(agentApi.ipv6Readiness)
        .mockResolvedValueOnce(response(readiness({ checked_at: 'first', publication: 'checking' })))
        .mockResolvedValueOnce(response(readiness({ checked_at: 'first', publication: 'checking' })))
        .mockResolvedValueOnce(response(readiness({ checked_at: 'first', publication: 'changed' })))
      const wrapper = await mount()
      expect(status(wrapper, 'publish')).toBe('核对中')
      const verifier = wrapper.getComponent(MihomoIPv6Verification)
      await vi.advanceTimersByTimeAsync(3000)
      expect(status(wrapper, 'publish')).toBe('核对中')
      await vi.advanceTimersByTimeAsync(3000)
      expect(status(wrapper, 'publish')).toBe('待发布')
      expect(row(wrapper, 'publish').text()).toContain('还没有发布到这台 Mihomo')
      // Re-reads of an unchanged check: never a forced device check, and the verifier is kept.
      expect(vi.mocked(agentApi.ipv6Readiness).mock.calls).toEqual(Array(3).fill(['gw', 'bound', { refresh: false }]))
      expect(wrapper.getComponent(MihomoIPv6Verification).vm).toBe(verifier.vm)
      await vi.advanceTimersByTimeAsync(10000)
      expect(agentApi.ipv6Readiness).toHaveBeenCalledTimes(3)
    } finally { vi.useRealTimers() }
  })

  it('stops following the comparison when the list closes', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(agentApi.ipv6Readiness).mockResolvedValue(response(readiness({ checked_at: 'first', publication: 'checking' })))
      const wrapper = await mount()
      await wrapper.setProps({ open: false })
      await vi.advanceTimersByTimeAsync(10000)
      expect(agentApi.ipv6Readiness).toHaveBeenCalledTimes(1)
    } finally { vi.useRealTimers() }
  })

  it('asks the device check window to open for the selected device', async () => {
    const wrapper = await mount()
    await wrapper.get('[aria-label="检查设备 IPv6"]').trigger('click')
    expect(wrapper.emitted('check')).toEqual([['gw']])
  })

  it('offers host preparation through the device check when host settings are missing', async () => {
    vi.mocked(agentApi.ipv6Readiness).mockResolvedValue(response(readiness({ can_prepare: true,
      checks: [{ id: 'host_forwarding_ra', label: '转发与 RA', status: 'warning', message: '未满足' }] })))
    const wrapper = await mount({ mode: 'nodes' })
    expect(status(wrapper, 'host')).toBe('需处理')
    expect(row(wrapper, 'host').text()).toContain('补齐缺少的配置')
    await row(wrapper, 'host').findAll('button').find(item => item.text().includes('检查设备'))!.trigger('click')
    expect(wrapper.emitted('check')).toEqual([['gw']])
  })

  it('requires an explicit device when several are bound and none is fixed', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([gateway, { ...gateway, id: 'gw2', name: '书房 Mihomo' }]))
    const wrapper = await mount({ preferredAgentId: undefined })
    expect(agentApi.ipv6Readiness).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('选择要检查的 Mihomo 设备')
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'gw2')
    await flushPromises()
    expect(agentApi.ipv6Readiness).toHaveBeenCalledExactlyOnceWith('gw2', 'bound', { refresh: false })
  })

  it('never replaces an unavailable entry device, and shows the last check of an offline one', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([{ ...gateway, status: 'offline' }]))
    vi.mocked(agentApi.ipv6Readiness).mockRejectedValueOnce({ response: { data: { message: '设备离线，上线后重新检查' } } })
    let wrapper = await mount()
    // Asked for its last result; without one the server says it is offline.
    expect(agentApi.ipv6Readiness).toHaveBeenCalledExactlyOnceWith('gw', 'bound', { refresh: false })
    expect(wrapper.text()).toContain('离线，上线后重新检查')
    vi.mocked(agentApi.summaries).mockResolvedValue(response([gateway, dnsAgent]))
    await wrapper.get('[data-ipv6-steps-recheck]').trigger('click')
    await flushPromises()
    expect(agentApi.ipv6Readiness).toHaveBeenCalledTimes(2)
    expect(status(wrapper, 'host')).toBe('已就绪')
    vi.mocked(agentApi.ipv6Readiness).mockClear()
    vi.mocked(agentApi.ipv6Readiness).mockResolvedValueOnce(response({ ...hinted(), stale: true, checked_at: new Date().toISOString(),
      refresh_error: { code: 'AGENT_OFFLINE', message: '设备离线，上线后重新检查', at: '' } }))
    vi.mocked(agentApi.summaries).mockResolvedValue(response([{ ...gateway, status: 'offline' }]))
    wrapper = await mount()
    expect(wrapper.get('[data-ipv6-steps-recheck-failed]').text()).toBe('这次未能重新检查：设备离线，上线后重新检查。以下是设备或配置变化前的结果。')
    // The offline reason is said once, not again as a notice.
    expect(wrapper.text().match(/上线后重新检查/g)).toHaveLength(1)
    expect(status(wrapper, 'host')).toBe('已就绪')
    vi.mocked(agentApi.ipv6Readiness).mockClear()
    vi.mocked(agentApi.summaries).mockResolvedValue(response([{ ...gateway, status: 'offline' }]))
    wrapper = await mount({ preferredAgentId: 'missing' })
    expect(agentApi.ipv6Readiness).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('原目标设备已不属于此配置空间')
  })

  it('shows a readiness failure once and keeps every step unconfirmed', async () => {
    vi.mocked(agentApi.ipv6Readiness).mockRejectedValue({ response: { data: { message: '无法连接 Agent 进行 IPv6 检查' } } })
    const wrapper = await mount()
    expect(wrapper.text()).toContain('无法连接 Agent 进行 IPv6 检查')
    for (const id of ['host', 'publish', 'firewall', 'verify']) expect(status(wrapper, id)).toBe('待检查')
    expect(routerApi.list).not.toHaveBeenCalled()
  })
})
