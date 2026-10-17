import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import Routers from '@/views/Routers.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import StatusDot from '@/components/common/StatusDot.vue'
import MultiSelect from '@/components/common/MultiSelect.vue'
import { Select } from '@/components/ui/select'
import { routerApi, agentApi, ruleApi } from '@/api'
import { NAV_GROUPS, scopeOfPath } from '@/navigation'
import { activeProfileId } from '@/profileContext'
import { notify } from '@/lib/feedback'

vi.mock('@/api', () => ({
  agentApi: { getAll: vi.fn() },
  routerApi: {
    list: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), probe: vi.fn(), report: vi.fn(), plan: vi.fn(), apply: vi.fn(), routes: vi.fn(), routePlan: vi.fn(), routeApply: vi.fn(), policy: vi.fn(), policySettings: vi.fn(), savePolicySettings: vi.fn(), previewPolicySettings: vi.fn()
  },
  ruleApi: { getAll: vi.fn() }, oxidnsMonitorApi: { overview: vi.fn() }
}))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn(), info: vi.fn() }, confirmDanger: vi.fn(), confirm: vi.fn(), confirmState: { resolve: null }, settleConfirm: vi.fn() }))

const wrappers: VueWrapper[] = []
beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  activeProfileId.value = 'default'
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

const ROUTER = {
  id: 'router_1', name: '主路由', type: 'ikuai', endpoint: 'http://192.0.2.1', username: 'admin',
  verify_tls: true, proxy_gateway_agent_id: 'a1', manage_return_routing: true, dns_agent_id: '', has_password: true,
  last_probe: { ok: true, version: '4.0.308', at: '2026-10-17T10:00:00' }
}
const REPORT = {
  in_sync: false,
  summary: { synced: 1, adoptable: 1, missing: 1, extra: 0 },
  desired: { fake_ip_range: '198.18.0.0/16', proxy_gateway: '192.0.2.4', proxy_cidrs: [], warnings: ['规则集「X」没有本地缓存'] },
  routes: {
    items: [
      { cidr: '198.18.0.0/16', sources: ['fake-ip'], status: 'synced', detail: '' },
      { cidr: '91.108.4.0/22', sources: ['Telegram'], status: 'adoptable', detail: '将直接接管' }
    ],
    extra: [],
    unmanaged_other: [{ cidr: '8.8.8.8/32', gateway: '192.0.2.4', comment: 'DNS', enabled: true }]
  },
  nat_bypass: { items: [{ cidr: '198.18.0.0/16', status: 'missing', detail: '仍会被源地址转换' }], disabled_rules: [] },
  dhcp_dns: { status: 'disabled', message: '未关联 OxiDNS Agent，保留现有 DHCP DNS。', target_dns: '', items: [], blockers: [] },
  return_routing: { status: 'ok', nft_available: true, iproute_available: true,
    return_tables: [{ family: 'inet', name: 'mihomo_return', marks: ['0x00400000'] }],
    fwmark_rules: [{ priority: '104', mark: '0x400000/0x400000', table: '104' }],
    table_routes: { 104: ['default via 192.0.2.1 dev eth0'] }, tun_devices: ['Meta'] }
}

const RETURN_DISABLED_MESSAGE = '自动配置回流已关闭，本次不读取或修改 Agent 回流配置，已有规则保留。'
const RETURN_DISABLED_NOTE = '自动配置回流已关闭，本次跳过回流阶段并保留已有规则。'
const DHCP_DNS_PENDING = {
  status: 'pending', message: '将更新包含 mihomo 网关的唯一 DHCP 网段。', target_dns: '192.0.2.53', blockers: [],
  items: [{ id: 'dhcp1', network: '192.0.2.0/24', current_dns: [] as string[], desired_dns: ['192.0.2.53'],
    status: 'pending', detail: '只更新 DNS，不修改网关、地址池或租期。' }]
}

const buttonInDocument = (label: string) => Array.from(document.body.querySelectorAll('button'))
  .find(button => button.textContent?.trim() === label)!
const toggleHelp = async (label: string) => {
  document.querySelector<HTMLButtonElement>(`button[aria-label="${label}说明"]`)!.click()
  await flushPromises()
  return document.querySelector<HTMLElement>('[data-field-help-content]')
}

const mountRouter = async (router: Record<string, unknown> | null = ROUTER, agents: Record<string, unknown>[] = []) => {
  vi.mocked(routerApi.list).mockResolvedValue({ data: { success: true, routers: router ? [router] : [] } } as any)
  vi.mocked(agentApi.getAll).mockResolvedValue({ data: agents } as any)
  vi.mocked(ruleApi.getAll).mockResolvedValue({ data: [] } as any)
  vi.mocked(routerApi.policySettings).mockResolvedValue({ data: { success: true, workspace: { profile_id: 'default', profile_name: '默认配置', revision: 'one', oxidns_revision: 'dns-one', router: { id: 'router_1', name: '主路由', ip_rule_sets: [], ros_policy_rule_sets: ['telegram'], proxy_gateway_agent_id: 'a1' }, sources: { policies: [], editable: true, reason: '' }, source_agents: [], gateways: [], rule_sets: [] } } } as any)
  const wrapper = mount(Routers, { attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

describe('routers view', () => {
  it('opens the unified policy workspace without opening another editor or publishing', async () => {
    await mountRouter({ ...ROUTER, type: 'routeros', ros_policy_rule_sets: ['telegram'] })
    buttonInDocument('策略路由').click()
    await flushPromises()
    expect(routerApi.policySettings).toHaveBeenCalledWith('router_1', 'default')
    expect(document.body.textContent).toContain('共用来源设备')
    expect(document.body.textContent).toContain('规则集与网关')
    expect(document.body.querySelector('#router-password')).toBeNull()
    expect(routerApi.apply).not.toHaveBeenCalled()
  })

  it('only shows the policy entry on ROS and switches it with other panels', async () => {
    vi.mocked(routerApi.policy).mockResolvedValue({ data: { success: true, policy: { objects: [], source_agents: [] }, generated_at: '2026-10-17T10:00:00' } } as any)
    vi.mocked(routerApi.routes).mockResolvedValue({ data: { success: true, routes: [] } } as any)
    const ikuai = await mountRouter()
    expect(ikuai.findAll('button').some(button => button.text() === '策略路由')).toBe(false)
    ikuai.unmount()
    const ros = await mountRouter({ ...ROUTER, type: 'routeros' })
    buttonInDocument('策略路由').click()
    await flushPromises()
    expect(ros.text()).toContain('策略路由 · 主路由')
    buttonInDocument('静态路由').click()
    await flushPromises()
    expect(ros.text()).not.toContain('策略路由 · 主路由')
    buttonInDocument('策略路由').click()
    await flushPromises()
    expect(ros.text()).not.toContain('静态路由 · 主路由')
  })

  it('selects policy rule sets directly without selecting any static rule sets', async () => {
    vi.mocked(ruleApi.getAll).mockResolvedValueOnce({ data: [
      { id: 'telegram', itemType: 'ruleset', name: 'Telegram', policy: '代理' },
      { id: 'other', itemType: 'ruleset', name: '其他 IP', policy: '代理' },
      { id: 'unused', itemType: 'ruleset', name: '未选规则集', policy: '代理' }
    ] } as any)
    vi.mocked(routerApi.update).mockResolvedValue({ data: { success: true } } as any)
    const wrapper = await mountRouter({ ...ROUTER, type: 'routeros',
      ip_rule_sets: [], ros_policy_rule_sets: [] })
    await wrapper.find('button[aria-label="编辑"]').trigger('click')
    await flushPromises()
    const [staticSets, policy] = wrapper.findAllComponents(MultiSelect)
    expect(policy.props('options').map(item => item.value)).toEqual(['telegram', 'other', 'unused'])
    expect(document.body.textContent).toContain('使用静态路由的 IP 规则集')
    expect(document.body.textContent).toContain('使用策略路由的 IP 规则集')
    policy.vm.$emit('update:modelValue', ['telegram'])
    await flushPromises()
    expect(staticSets.props('modelValue')).toEqual([])
    buttonInDocument('保存').click()
    await flushPromises()
    expect(routerApi.update).toHaveBeenCalledWith('router_1', expect.objectContaining({
      ip_rule_sets: [], ros_policy_rule_sets: ['telegram']
    }))
    expect(routerApi.apply).not.toHaveBeenCalled()
  })

  it('moves a rule set between routing modes without leaving duplicate selections', async () => {
    const wrapper = await mountRouter({ ...ROUTER, type: 'routeros',
      ip_rule_sets: ['other'], ros_policy_rule_sets: ['telegram'] })
    await wrapper.find('button[aria-label="编辑"]').trigger('click')
    await flushPromises()
    const [staticSets, policy] = wrapper.findAllComponents(MultiSelect)
    policy.vm.$emit('update:modelValue', ['telegram', 'other'])
    await flushPromises()
    expect(staticSets.props('modelValue')).toEqual([])
    staticSets.vm.$emit('update:modelValue', ['telegram'])
    await flushPromises()
    expect(policy.props('modelValue')).toEqual(['other'])
    expect(staticSets.props('modelValue')).toEqual(['telegram'])
  })

  it('keeps selections independent when either group is cleared', async () => {
    const wrapper = await mountRouter({ ...ROUTER, type: 'routeros',
      ip_rule_sets: ['other'], ros_policy_rule_sets: ['telegram'] })
    await wrapper.find('button[aria-label="编辑"]').trigger('click')
    await flushPromises()
    const [staticSets, policy] = wrapper.findAllComponents(MultiSelect)
    staticSets.vm.$emit('update:modelValue', [])
    await flushPromises()
    expect(policy.props('modelValue')).toEqual(['telegram'])
    policy.vm.$emit('update:modelValue', [])
    await flushPromises()
    expect(staticSets.props('modelValue')).toEqual([])
    expect(policy.props('placeholder')).toBe('不使用策略路由')
  })

  it('opens legacy overlapping selections with their actual routing modes preserved', async () => {
    const wrapper = await mountRouter({ ...ROUTER, type: 'routeros',
      ip_rule_sets: ['telegram', 'other'], ros_policy_rule_sets: ['telegram'] })
    await wrapper.find('button[aria-label="编辑"]').trigger('click')
    await flushPromises()
    const [staticSets, policy] = wrapper.findAllComponents(MultiSelect)
    expect(staticSets.props('modelValue')).toEqual(['other'])
    expect(policy.props('modelValue')).toEqual(['telegram'])
    expect(routerApi.update).not.toHaveBeenCalled()
    expect(routerApi.apply).not.toHaveBeenCalled()
  })

  it('shows source policy drift separately from static routes', async () => {
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, report: {
      ...REPORT, routes: { ...REPORT.routes, items: [REPORT.routes.items[0]] },
      policy_routing: { enabled: true, sources: ['192.168.0.101/32'],
        blockers: ['策略与静态网段重叠，请调整选择'], warnings: [],
        items: [{ cidr: '91.108.4.0/22', sources: ['Telegram'], status: 'pending', detail: '按来源转发' }] }
    } } } as any)
    const wrapper = await mountRouter({ ...ROUTER, type: 'routeros' })
    buttonInDocument('差异报告').click()
    await flushPromises()
    const sections = wrapper.findAllComponents(SectionCard)
    const staticRoutes = sections.find(item => item.props('title') === '代理路由')!
    const policy = sections.find(item => item.props('title') === '按设备和目标 IP 分流')!
    expect(staticRoutes.text()).not.toContain('91.108.4.0/22')
    expect(policy.text()).toContain('91.108.4.0/22')
    expect(policy.find('[role="alert"]').text()).toContain('网段重叠')
    expect(routerApi.apply).not.toHaveBeenCalled()
  })

  it('switches between static routes and the report instead of stacking both panels', async () => {
    vi.mocked(routerApi.routes).mockResolvedValue({ data: { success: true, routes: [] } } as any)
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, report: REPORT, generated_at: '2026-10-17T10:00:00' } } as any)
    const wrapper = await mountRouter()
    buttonInDocument('静态路由').click()
    await flushPromises()
    expect(wrapper.text()).toContain('静态路由 · 主路由')
    buttonInDocument('差异报告').click()
    await flushPromises()
    expect(wrapper.text()).toContain('差异报告 · 主路由')
    expect(wrapper.text()).not.toContain('静态路由 ·')
    // Also exercise the shortcut inside the report, not just the device list.
    const otherRoutes = wrapper.findAllComponents(SectionCard).find(card => card.props('title') === '其他路由')!
    await otherRoutes.find('button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('静态路由 · 主路由')
    expect(wrapper.text()).not.toContain('差异报告 ·')
    buttonInDocument('收起路由列表').click()
    await flushPromises()
    expect(wrapper.text()).not.toContain('差异报告 ·')
  })

  it('switches immediately to report loading and lets a failed report retry', async () => {
    vi.mocked(routerApi.routes).mockResolvedValue({ data: { success: true, routes: [] } } as any)
    let rejectReport!: (error: any) => void
    vi.mocked(routerApi.report).mockImplementationOnce(() => new Promise((_, reject) => { rejectReport = reject }))
      .mockResolvedValue({ data: { success: true, report: REPORT, generated_at: '2026-10-17T10:00:00' } } as any)
    const wrapper = await mountRouter()
    buttonInDocument('静态路由').click()
    await flushPromises()
    buttonInDocument('差异报告').click()
    await flushPromises()
    expect(wrapper.text()).not.toContain('静态路由 ·')
    expect(wrapper.text()).toContain('正在生成差异报告…')
    rejectReport({ response: { data: { message: '设备暂时离线' } } })
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toBe('设备暂时离线')
    buttonInDocument('重试').click()
    await flushPromises()
    expect(wrapper.text()).toContain('预览发布')
    expect(wrapper.text()).not.toContain('设备暂时离线')
  })

  it('ignores a pending route list after switching to the report', async () => {
    let resolveRoutes!: (value: any) => void
    vi.mocked(routerApi.routes).mockImplementationOnce(() => new Promise(resolve => { resolveRoutes = resolve }))
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, report: REPORT, generated_at: '2026-10-17T10:00:00' } } as any)
    const wrapper = await mountRouter()
    buttonInDocument('静态路由').click()
    await flushPromises()
    buttonInDocument('差异报告').click()
    await flushPromises()
    resolveRoutes({ data: { success: true, routes: [] } })
    await flushPromises()
    expect(wrapper.text()).toContain('差异报告 · 主路由')
    expect(wrapper.text()).not.toContain('静态路由 ·')
  })

  it('ignores a pending report after switching back to static routes', async () => {
    let resolveReport!: (value: any) => void
    vi.mocked(routerApi.routes).mockResolvedValue({ data: { success: true, routes: [] } } as any)
    vi.mocked(routerApi.report).mockImplementationOnce(() => new Promise(resolve => { resolveReport = resolve }))
    const wrapper = await mountRouter()
    buttonInDocument('差异报告').click()
    await flushPromises()
    buttonInDocument('静态路由').click()
    await flushPromises()
    resolveReport({ data: { success: true, report: REPORT, generated_at: '2026-10-17T10:00:00' } })
    await flushPromises()
    expect(wrapper.text()).toContain('静态路由 · 主路由')
    expect(wrapper.text()).not.toContain('差异报告 ·')
  })

  it('does not let an old report overwrite a newer request after switching twice', async () => {
    const pending: ((value: any) => void)[] = []
    vi.mocked(routerApi.routes).mockResolvedValue({ data: { success: true, routes: [] } } as any)
    vi.mocked(routerApi.report).mockImplementation(() => new Promise(resolve => { pending.push(resolve) }))
    const wrapper = await mountRouter()
    buttonInDocument('差异报告').click()
    await flushPromises()
    buttonInDocument('静态路由').click()
    await flushPromises()
    buttonInDocument('差异报告').click()
    await flushPromises()
    pending[0]({ data: { success: true, report: REPORT, generated_at: '2026-10-17T10:00:00' } })
    await flushPromises()
    expect(wrapper.text()).toContain('正在生成差异报告…')
    expect(buttonInDocument('差异报告').disabled).toBe(true)
    pending[1]({ data: { success: true, report: REPORT, generated_at: '2026-10-17T10:01:00' } })
    await flushPromises()
    expect(wrapper.text()).toContain('预览发布')
    expect(wrapper.text()).not.toContain('正在生成差异报告…')
  })

  it('is reachable from the profile-scoped navigation', () => {
    const profile = NAV_GROUPS.find(group => group.scope === 'profile')
    expect(profile?.items.some(item => item.path === '/routers' && item.label === '上游路由')).toBe(true)
    expect(scopeOfPath('/routers')).toBe('profile')
  })

  it('creates a router with automatic return routing disabled by default', async () => {
    vi.mocked(routerApi.create).mockResolvedValue({ data: { success: true } } as any)
    const wrapper = await mountRouter(null)
    await wrapper.findAll('button').find(button => button.text().includes('添加路由器'))!.trigger('click')
    await flushPromises()

    const toggle = document.querySelector<HTMLButtonElement>('#router-return-routing')!
    expect(toggle.getAttribute('aria-checked')).toBe('false')
    expect(document.body.textContent).not.toContain('关闭只停止管理，已有回流规则会保留')
    expect((await toggleHelp('自动配置回流'))?.textContent).toContain('关闭只停止管理，已有回流规则会保留')
    expect(toggle.getAttribute('aria-checked')).toBe('false')
    await toggleHelp('自动配置回流')
    expect(document.body.textContent).toContain('DHCP DNS 服务器（OxiDNS Agent）')
    expect((await toggleHelp('DHCP DNS 服务器（OxiDNS Agent）'))?.textContent).toContain('默认不关联，不修改现有 DNS')
    await toggleHelp('DHCP DNS 服务器（OxiDNS Agent）')
    for (const [id, value] of [
      ['router-name', '新主路由'], ['router-endpoint', 'http://192.0.2.1'],
      ['router-username', 'admin'], ['router-password', 'example-password']
    ]) {
      await new DOMWrapper(document.querySelector<HTMLInputElement>(`#${id}`)!).setValue(value)
    }
    buttonInDocument('保存').click()
    await flushPromises()
    expect(routerApi.create).toHaveBeenCalledWith(expect.objectContaining({
      name: '新主路由', type: 'ikuai', manage_return_routing: false, dns_agent_id: ''
    }))
  })

  it.each([
    { type: 'ikuai', initial: undefined, saved: false },
    { type: 'routeros', initial: false, saved: true },
    { type: 'ikuai', initial: true, saved: false }
  ])('saves explicit return-routing preference for $type: $initial → $saved', async ({ type, initial, saved }) => {
    vi.mocked(routerApi.update).mockResolvedValue({ data: { success: true } } as any)
    const wrapper = await mountRouter({ ...ROUTER, type, manage_return_routing: initial })
    await wrapper.find('button[aria-label="编辑"]').trigger('click')
    await flushPromises()
    const toggle = document.querySelector<HTMLButtonElement>('#router-return-routing')!
    expect(toggle.getAttribute('aria-checked')).toBe(String(initial === true))
    if ((initial === true) !== saved) toggle.click()
    await flushPromises()
    expect(toggle.getAttribute('aria-checked')).toBe(String(saved))
    buttonInDocument('保存').click()
    await flushPromises()
    expect(routerApi.update).toHaveBeenCalledWith('router_1', expect.objectContaining({ manage_return_routing: saved }))
  })

  it('shows disabled return routing neutrally and keeps its skip note visible in an empty plan', async () => {
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, generated_at: '2026-10-17T10:00:00', report: {
      ...REPORT,
      in_sync: true, summary: { synced: 1, not_required: 1 },
      desired: { ...REPORT.desired, warnings: [] },
      routes: { ...REPORT.routes, items: [REPORT.routes.items[0]] },
      nat_bypass: { items: [{ cidr: '198.18.0.0/16', status: 'not_required', detail: '旁路出口不匹配源 NAT' }], disabled_rules: [] },
      return_routing: { status: 'disabled', message: RETURN_DISABLED_MESSAGE }
    } } } as any)
    vi.mocked(routerApi.plan).mockResolvedValue({ data: { success: true, plan: {
      blockers: [], warnings: [], fingerprint: 'disabled123', empty: true,
      stages: [
        { key: 'return_routing', title: 'mihomo 回流规则', enabled: false, note: RETURN_DISABLED_NOTE, ops: [] },
        { key: 'routes', title: '主路由代理路由', enabled: true, ops: [] },
        { key: 'nat_bypass', title: '主路由 NAT 免转换', enabled: true, ops: [] }
      ]
    } } } as any)
    const wrapper = await mountRouter({ ...ROUTER, manage_return_routing: false })
    await wrapper.findAll('button').find(button => button.text().includes('差异报告'))!.trigger('click')
    await flushPromises()
    const returnCard = wrapper.findAllComponents(SectionCard).find(card => card.props('title') === '回流规则（旁路网关）')!
    expect(returnCard.findComponent(StatusDot).props()).toMatchObject({ tone: 'muted', label: '自动配置已关闭' })
    expect(returnCard.text()).toContain(RETURN_DISABLED_MESSAGE)
    for (const text of ['未发现', '无法读取', '未配置', '缺失']) expect(returnCard.text()).not.toContain(text)

    await wrapper.findAll('button').find(button => button.text().includes('预览发布'))!.trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('已全部同步，无需发布')
    expect(document.body.textContent).toContain(RETURN_DISABLED_NOTE)
    expect(document.body.textContent).toContain('已跳过')
    expect(buttonInDocument('确认发布').disabled).toBe(true)
    expect(routerApi.apply).not.toHaveBeenCalled()
  })

  it('discards a stale pending preview after changing the return-routing preference', async () => {
    vi.mocked(routerApi.update).mockResolvedValue({ data: { success: true } } as any)
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, report: REPORT, generated_at: '2026-10-17T10:00:00' } } as any)
    let resolvePlan!: (value: any) => void
    vi.mocked(routerApi.plan).mockImplementation(() => new Promise(resolve => { resolvePlan = resolve }))
    const wrapper = await mountRouter()
    await wrapper.findAll('button').find(button => button.text().includes('差异报告'))!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('预览发布'))!.trigger('click')
    await flushPromises()
    await wrapper.find('button[aria-label="编辑"]').trigger('click')
    await flushPromises()
    document.querySelector<HTMLButtonElement>('#router-return-routing')!.click()
    await flushPromises()
    buttonInDocument('保存').click()
    await flushPromises()
    expect(routerApi.update).toHaveBeenCalledWith('router_1', expect.objectContaining({ manage_return_routing: false }))
    expect(wrapper.text()).not.toContain('差异报告 ·')

    resolvePlan({ data: { success: true, plan: {
      blockers: [], warnings: [], fingerprint: 'stale123', empty: false,
      stages: [{ key: 'return_routing', title: 'mihomo 回流规则', enabled: true,
        ops: [{ kind: 'return_routing_apply', summary: '旧回流发布计划' }] }]
    } } })
    await flushPromises()
    expect(document.body.textContent).not.toContain('发布到 主路由')
    expect(document.body.textContent).not.toContain('旧回流发布计划')
    expect(routerApi.apply).not.toHaveBeenCalled()
  })

  it.each(['dns1', '__none__'])('saves DHCP DNS management using the selected Agent: %s', async (selected) => {
    vi.mocked(routerApi.update).mockResolvedValue({ data: { success: true } } as any)
    const wrapper = await mountRouter({ ...ROUTER, dns_agent_id: 'dns1' }, [
      { id: 'dns1', name: 'OxiDNS', host: '192.0.2.53', service_type: 'oxidns' }
    ])
    await wrapper.find('button[aria-label="编辑"]').trigger('click')
    await flushPromises()
    const dnsSelect = wrapper.findAllComponents(Select).at(-1)!
    expect(dnsSelect.props('modelValue')).toBe('dns1')
    dnsSelect.vm.$emit('update:modelValue', selected)
    await flushPromises()
    buttonInDocument('保存').click()
    await flushPromises()
    expect(routerApi.update).toHaveBeenCalledWith('router_1', expect.objectContaining({
      dns_agent_id: selected === '__none__' ? '' : selected
    }))
  })

  it.each([
    { status: 'disabled', label: '未关联', tone: 'muted' },
    { status: 'synced', label: '已同步', tone: 'success' },
    { status: 'pending', label: '待更新', tone: 'warning' },
    { status: 'unknown', label: '待确认', tone: 'warning' }
  ])('renders the DHCP DNS $status state and its scope', async ({ status, label, tone }) => {
    const blockers = status === 'unknown' ? ['mihomo 网关匹配多个 DHCP 网段，无法确定唯一修改目标。'] : []
    const dns = status === 'disabled' ? REPORT.dhcp_dns : {
      ...DHCP_DNS_PENDING, status, blockers,
      message: status === 'unknown' ? '需要先确认 DHCP 网段。' : DHCP_DNS_PENDING.message,
      items: status === 'unknown' ? [
        { ...DHCP_DNS_PENDING.items[0], status, detail: blockers[0] },
        { ...DHCP_DNS_PENDING.items[0], id: 'dhcp2', network: '192.0.2.0/25', status, detail: blockers[0] }
      ] : [{ ...DHCP_DNS_PENDING.items[0], status,
        current_dns: status === 'synced' ? ['192.0.2.53'] : [] }]
    }
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, generated_at: '2026-10-17T10:00:00', report: {
      ...REPORT, dhcp_dns: dns,
      in_sync: status === 'disabled' || status === 'synced',
      summary: { synced: 1, not_required: 1, ...(status === 'disabled' ? {} : { [status]: status === 'synced' ? 2 : 1 }) },
      desired: { ...REPORT.desired, warnings: [] },
      routes: { ...REPORT.routes, items: [REPORT.routes.items[0]] },
      nat_bypass: { items: [{ cidr: '198.18.0.0/16', status: 'not_required', detail: '无需免转换' }], disabled_rules: [] }
    } } } as any)
    const wrapper = await mountRouter({ ...ROUTER, dns_agent_id: status === 'disabled' ? '' : 'dns1' })
    await wrapper.findAll('button').find(button => button.text().includes('差异报告'))!.trigger('click')
    await flushPromises()
    const card = wrapper.findAllComponents(SectionCard).find(section => section.props('title') === 'DHCP DNS')!
    expect(card.findComponent(StatusDot).props()).toMatchObject({ label, tone })
    expect(card.text()).toContain('包含所选 mihomo 网关的唯一 DHCP 网段')
    if (status === 'disabled') {
      expect(card.text()).toContain('保留现有 DHCP DNS')
      expect(card.find('table').exists()).toBe(false)
    } else {
      expect(card.text()).toContain('192.0.2.0/24')
      expect(card.text()).toContain('192.0.2.53')
      expect(card.text()).toContain('终端在 DHCP 续租或重新联网后获得新 DNS')
      expect(card.text()).toContain('不保留旧备用 DNS')
      if (status === 'pending') expect(card.text()).toContain('自动/未指定')
    }
    if (status === 'unknown') {
      expect(card.text()).toContain('192.0.2.0/25')
      expect(card.text()).toContain(blockers[0])
      vi.mocked(routerApi.plan).mockResolvedValue({ data: { success: true, plan: {
        blockers, warnings: [], fingerprint: 'blocked-dns', empty: true,
        stages: [{ key: 'dhcp_dns', title: 'DHCP DNS', enabled: true, ops: [] }]
      } } } as any)
      await wrapper.findAll('button').find(button => button.text().includes('预览发布'))!.trigger('click')
      await flushPromises()
      expect(buttonInDocument('确认发布').disabled).toBe(true)
    }
    expect(routerApi.apply).not.toHaveBeenCalled()
  })

  it.each(['ikuai', 'routeros'])('previews the old and new DHCP DNS and applies it last for %s', async (type) => {
    const change = 'DHCP 网段 192.0.2.0/24：DNS 192.0.2.1、1.1.1.1 → 192.0.2.53'
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, report: {
      ...REPORT, dhcp_dns: { ...DHCP_DNS_PENDING,
        items: [{ ...DHCP_DNS_PENDING.items[0], current_dns: ['192.0.2.1', '1.1.1.1'] }] }
    }, generated_at: '2026-10-17T10:00:00' } } as any)
    vi.mocked(routerApi.plan).mockResolvedValue({ data: { success: true, plan: {
      blockers: [], warnings: [], fingerprint: 'dns123', empty: false,
      stages: [
        { key: 'return_routing', title: 'mihomo 回流规则', enabled: false, note: RETURN_DISABLED_NOTE, ops: [] },
        { key: 'routes', title: '主路由代理路由', enabled: true, ops: [] },
        { key: 'nat_bypass', title: '主路由 NAT 免转换', enabled: true, ops: [] },
        { key: 'dhcp_dns', title: 'DHCP DNS', enabled: true, note: '终端续租或重新联网后获得新 DNS。',
          ops: [{ kind: 'dhcp_dns_update', summary: change }] }
      ]
    } } } as any)
    vi.mocked(routerApi.apply).mockResolvedValue({ data: { success: true, result: {
      ok: true, rollback: [], log: [{ stage: 'dhcp_dns', status: 'verified', summary: '回读校验通过' }]
    } } } as any)
    const wrapper = await mountRouter({ ...ROUTER, type, dns_agent_id: 'dns1', manage_return_routing: false })
    await wrapper.findAll('button').find(button => button.text().includes('差异报告'))!.trigger('click')
    await flushPromises()
    const card = wrapper.findAllComponents(SectionCard).find(section => section.props('title') === 'DHCP DNS')!
    expect(card.text()).toContain('192.0.2.1、1.1.1.1')
    await wrapper.findAll('button').find(button => button.text().includes('预览发布'))!.trigger('click')
    await flushPromises()
    const stages = Array.from(document.querySelectorAll('[role="dialog"] ol > li'))
    expect(stages).toHaveLength(4)
    expect(stages.at(-1)?.textContent).toContain(change)
    expect(routerApi.apply).not.toHaveBeenCalled()
    buttonInDocument('确认发布').click()
    await flushPromises()
    expect(routerApi.apply).toHaveBeenCalledWith('router_1', 'dns123')
    expect(document.body.textContent).toContain('DHCP DNS · 回读校验通过')
    expect(document.body.textContent).toContain('发布完成')
  })

  it.each(['ikuai', 'routeros'])('keeps account guidance behind its field help when editing %s', async (type) => {
    vi.mocked(routerApi.list).mockResolvedValue({ data: { success: true, routers: [{ ...ROUTER, type }] } } as any)
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [] } as any)
    vi.mocked(ruleApi.getAll).mockResolvedValue({ data: [] } as any)
    const wrapper = mount(Routers, { attachTo: document.body })
    wrappers.push(wrapper)
    await flushPromises()
    await wrapper.find('button[aria-label="编辑"]').trigger('click')
    await flushPromises()

    expect(document.body.textContent).not.toContain('如何设置 RouterOS 账号权限')
    expect(document.body.textContent).toContain('保存仅更新 ConfigFlow 设置')
    const guide = (await toggleHelp('用户名'))!
    if (type === 'ikuai') {
      expect(guide.textContent).toContain('iKuai 需要管理员账号')
      expect(guide.textContent).not.toContain('RouterOS')
      return
    }
    for (const text of ['如何设置 RouterOS 账号权限', 'System → Users', 'read、write、api、rest-api',
      '当前填写的账号：admin', '不代表已验证写入权限']) {
      expect(guide.textContent).toContain(text)
    }
    const username = document.querySelector<HTMLInputElement>('#router-username')!
    username.value = 'cf-agent'
    username.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    expect(guide.textContent).toContain('当前填写的账号：cf-agent')
    expect(guide.textContent).not.toContain('当前填写的账号：admin')
    expect(routerApi.update).not.toHaveBeenCalled()
    expect(routerApi.apply).not.toHaveBeenCalled()
  })

  it('lists routers without passwords and renders the drift report', async () => {
    vi.mocked(routerApi.list).mockResolvedValue({ data: { success: true, routers: [ROUTER] } } as any)
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [{ id: 'a1', name: 'mihomo', host: '192.0.2.4', service_type: 'mihomo' }] } as any)
    vi.mocked(ruleApi.getAll).mockResolvedValue({ data: [{ id: 'r1', itemType: 'ruleset', name: 'Telegram', policy: '📲 Telegram' }] } as any)
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, report: REPORT, generated_at: '2026-10-17T10:00:00' } } as any)

    const wrapper = mount(Routers, { attachTo: document.body })
    wrappers.push(wrapper)
    await flushPromises()
    expect(wrapper.text()).toContain('主路由')
    expect(wrapper.text()).toContain('4.0.308')
    expect(wrapper.html()).not.toContain('password')

    const reportButton = wrapper.findAll('button').find(button => button.text().includes('差异报告'))
    await reportButton!.trigger('click')
    await flushPromises()
    expect(routerApi.report).toHaveBeenCalledWith('router_1')
    const text = wrapper.text()
    for (const label of ['已同步', '可接管', '缺失', '存在差异', '已配置', 'mihomo_return', '8.8.8.8/32', '没有本地缓存']) {
      expect(text).toContain(label)
    }
  })

  it.each([
    {
      status: 'not_required', label: '无需免转换', chip: 'chip-ok', inSync: true,
      detail: '代理下一跳经 lan4 转发；现有源 NAT 仅匹配 pppoe-out1/WAN，无需新增免转换规则。'
    },
    {
      status: 'unknown', label: '待确认', chip: 'chip-warn', inSync: false,
      detail: '无法确认代理下一跳的出口接口，证据不足，不自动新增 NAT 免转换规则。'
    }
  ])('renders RouterOS NAT $status without treating it as a missing rule', async ({ status, label, chip, inSync, detail }) => {
    vi.mocked(routerApi.list).mockResolvedValue({ data: { success: true, routers: [{
      ...ROUTER, type: 'routeros', endpoint: 'https://192.0.2.1'
    }] } } as any)
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [] } as any)
    vi.mocked(ruleApi.getAll).mockResolvedValue({ data: [] } as any)
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, generated_at: '2026-10-17T10:00:00', report: {
      ...REPORT,
      in_sync: inSync,
      summary: { synced: 1, [status]: 1 },
      desired: { ...REPORT.desired, warnings: [] },
      routes: { ...REPORT.routes, items: [REPORT.routes.items[0]] },
      nat_bypass: { items: [{ cidr: '198.18.0.0/16', status, detail }], disabled_rules: [] }
    } } } as any)

    const wrapper = mount(Routers, { attachTo: document.body })
    wrappers.push(wrapper)
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('差异报告'))!.trigger('click')
    await flushPromises()

    const natRow = wrapper.findAll('tr').find(row => row.text().includes(detail))
    expect(natRow).toBeDefined()
    expect(natRow!.find('.chip').text()).toBe(label)
    expect(natRow!.find('.chip').classes()).toContain(chip)
    expect(wrapper.findAll('.chip').map(item => item.text())).toContain(`${label} 1`)
    expect(wrapper.text()).not.toContain('缺失')
    expect(wrapper.text()).toContain(inSync ? '已全部同步' : '存在待确认项')
    expect(routerApi.apply).not.toHaveBeenCalled()

    if (status === 'unknown') {
      vi.mocked(routerApi.plan).mockResolvedValue({ data: { success: true, plan: {
        blockers: ['198.18.0.0/16：NAT 是否需要免转换尚待确认，请先检查出口接口与源 NAT 规则。'],
        warnings: [], stages: [], fingerprint: '', empty: true
      } } } as any)
      await wrapper.findAll('button').find(button => button.text().includes('预览发布'))!.trigger('click')
      await flushPromises()
      expect(document.body.textContent).toContain('NAT 是否需要免转换尚待确认')
      const confirm = Array.from(document.body.querySelectorAll('button')).find(button => button.textContent?.includes('确认发布'))
      expect(confirm?.disabled).toBe(true)
      confirm!.click()
      await flushPromises()
      expect(routerApi.apply).not.toHaveBeenCalled()
    }
  })

  it.each(['ikuai', 'routeros'])('previews and automatically applies a %s release with the previewed fingerprint', async (type) => {
    vi.mocked(routerApi.list).mockResolvedValue({ data: { success: true, routers: [{
      ...ROUTER, type, endpoint: type === 'routeros' ? 'https://192.0.2.1' : ROUTER.endpoint
    }] } } as any)
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [] } as any)
    vi.mocked(ruleApi.getAll).mockResolvedValue({ data: [] } as any)
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, report: REPORT, generated_at: '2026-10-17T10:00:00' } } as any)
    vi.mocked(routerApi.plan).mockResolvedValue({ data: { success: true, plan: {
      blockers: [], warnings: ['149.154.160.0/20：同网段路由下一跳为 192.0.2.9。本次跳过该网段'], script: '', fingerprint: 'abc123', empty: false,
      stages: [
        { key: 'return_routing', title: 'mihomo 回流规则', enabled: true, manual: false, ops: [{ kind: 'return_routing_apply', summary: '接管并移除手工规则 inet mihomo_return' }] },
        { key: 'routes', title: '主路由代理路由', manual: false, ops: [{ kind: 'route_add', summary: '新增路由 91.108.0.0/16 → 192.0.2.4' }] },
        { key: 'nat_bypass', title: '主路由 NAT 免转换', manual: false, ops: [] }
      ]
    } } } as any)
    vi.mocked(routerApi.apply).mockResolvedValue({ data: { success: true, result: {
      ok: true, rollback: [],
      log: [{ stage: 'routes', summary: '新增路由 91.108.0.0/16 → 192.0.2.4', status: 'ok' }, { stage: 'routes', summary: '回读校验通过', status: 'verified' }]
    } } } as any)

    const wrapper = mount(Routers, { attachTo: document.body })
    wrappers.push(wrapper)
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('差异报告'))!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('预览发布'))!.trigger('click')
    await flushPromises()
    expect(routerApi.plan).toHaveBeenCalledWith('router_1')
    expect(routerApi.apply).not.toHaveBeenCalled()
    const body = document.body.textContent || ''
    for (const label of ['mihomo 回流规则', '接管并移除手工规则', '新增路由 91.108.0.0/16', '无变更', '本次跳过该网段']) {
      expect(body).toContain(label)
    }
    expect(body).not.toContain('手动导入')
    expect(body).not.toContain('RouterOS 脚本')

    const confirm = Array.from(document.body.querySelectorAll('button')).find(button => button.textContent?.includes('确认发布'))
    confirm!.click()
    await flushPromises()
    expect(routerApi.apply).toHaveBeenCalledWith('router_1', 'abc123')
    expect(document.body.textContent).toContain('发布完成')
    expect(document.body.textContent).toContain('已校验')
  })

  it('shows an incomplete RouterOS rollback without claiming that every change was reverted', async () => {
    vi.mocked(routerApi.list).mockResolvedValue({ data: { success: true, routers: [{
      ...ROUTER, type: 'routeros', endpoint: 'https://192.0.2.1'
    }] } } as any)
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [] } as any)
    vi.mocked(ruleApi.getAll).mockResolvedValue({ data: [] } as any)
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, report: REPORT, generated_at: '2026-10-17T10:00:00' } } as any)
    vi.mocked(routerApi.plan).mockResolvedValue({ data: { success: true, plan: {
      blockers: [], warnings: [], script: '', fingerprint: 'ros123', empty: false,
      stages: [
        { key: 'return_routing', title: 'mihomo 回流规则', enabled: true, manual: false, ops: [{ kind: 'return_routing_apply', summary: '配置回流规则' }] },
        { key: 'routes', title: '主路由代理路由', manual: false, ops: [{ kind: 'route_add', summary: '新增代理路由' }] },
        { key: 'nat_bypass', title: '主路由 NAT 免转换', manual: false, ops: [{ kind: 'nat_update', summary: '保留源地址限制，扩充 NAT 免转换目的网段' }] }
      ]
    } } } as any)
    vi.mocked(routerApi.apply).mockRejectedValue({ response: { data: {
      message: '发布失败',
      result: {
        ok: false,
        log: [{ stage: 'nat_bypass', summary: '回读校验失败', status: 'failed' }],
        rollback: [
          { stage: 'routes', summary: '撤销失败：新增代理路由（连接中断）', status: 'failed' },
          { stage: 'return_routing', summary: '已撤销：配置回流规则', status: 'ok' }
        ]
      }
    } } })

    const wrapper = mount(Routers, { attachTo: document.body })
    wrappers.push(wrapper)
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('差异报告'))!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('预览发布'))!.trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('保留源地址限制，扩充 NAT 免转换目的网段')
    expect(document.body.textContent).not.toContain('手动导入')
    const confirm = Array.from(document.body.querySelectorAll('button')).find(button => button.textContent?.includes('确认发布'))
    confirm!.click()
    await flushPromises()

    expect(routerApi.apply).toHaveBeenCalledWith('router_1', 'ros123')
    const text = document.body.textContent || ''
    expect(text).toContain('发布失败，回滚未完成')
    expect(text).toContain('撤销失败：新增代理路由（连接中断）')
    expect(text).toContain('已撤销：配置回流规则')
    expect(text).not.toContain('发布失败，已回滚')
    expect(text).not.toContain('发布失败，已撤销已完成的变更')
  })
})
