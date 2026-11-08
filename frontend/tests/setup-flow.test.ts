import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import Setup from '@/views/Setup.vue'
import MultiSelect from '@/components/common/MultiSelect.vue'
import SetupInstallation from '@/components/setup/SetupInstallation.vue'
import { subscriptionApi } from '@/api'
import { setupApi, type SetupSession } from '@/api/setup'
import { SETUP_GOALS, useLibraryNamesake } from '@/lib/setupFlow'
import { setActiveProfileId } from '@/profileContext'

vi.mock('@/api', () => ({
  default: { get: vi.fn() }, agentApi: {}, routerApi: {}, ruleApi: {},
  subscriptionApi: { fetch: vi.fn() }
}))
vi.mock('@/api/setup', () => ({ setupApi: { index: vi.fn(), create: vi.fn(), get: vi.fn(), source: vi.fn(), progress: vi.fn(), prepare: vi.fn(), check: vi.fn(), saveRouterDraft: vi.fn(), finish: vi.fn(), abandon: vi.fn() } }))
vi.mock('@/stores/profile', () => ({ useProfileStore: () => ({ refreshProfiles: vi.fn().mockResolvedValue([]) }) }))
vi.mock('@/profileContext', () => ({ setActiveProfileId: vi.fn() }))
vi.mock('@/lib/feedback', () => ({ confirm: vi.fn().mockResolvedValue(false), confirmDanger: vi.fn().mockResolvedValue(true), notify: { error: vi.fn(), success: vi.fn() } }))

const record = (changes: Partial<SetupSession> = {}): SetupSession => ({
  id: 'a'.repeat(32), profile_id: 'setup_test', name: '我的分流配置', goal: 'router', step: 1,
  max_step: 1, prepared: false, completed: false, subscription_ids: [], node_ids: [], updated_at: '', controller_secret_saved: true, ...changes
})
const installation = { id: 'pair', mihomo: { id: 'm', name: 'Mihomo', host: '192.0.2.10' }, oxidns: { id: 'o', name: 'OxiDNS', host: '192.0.2.10' } }
const defaultRules = { direct: ['builtin:direct'], proxy: ['builtin:proxy'] }
const ruleOptions = [
  { id: 'builtin:direct', name: 'Direct', source: 'builtin' },
  { id: 'builtin:proxy', name: 'Proxy', source: 'builtin' },
  { id: 'library:work', name: '工作域名', source: 'library' },
]
const installed = (changes: Partial<SetupSession> = {}) => record({ prepared: true, step: 4, max_step: 4, installation,
  route_instructions: { gateway: '192.0.2.10', dns: '192.0.2.10', routes: [{ cidr: '198.18.0.0/16', sources: ['fake-ip'] }], warnings: [] }, ...changes })
let wrapper: VueWrapper | undefined
async function open(path = '/setup') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/setup', component: Setup }, { path: '/dashboard', component: { template: '<div />' } },
    { path: '/client-devices', component: { template: '<div>客户端设备页</div>' } }, { path: '/agents', component: { template: '<div>Agent 页</div>' } }] })
  await router.push(path); await router.isReady()
  wrapper = mount(RouterView, { global: { plugins: [router], stubs: { SetupInstallation: true, Routers: { name: 'Routers', props: ['profileId', 'setupAgents', 'setupDraftSession'], emits: ['selected', 'busy'], template: '<button @click="$emit(\'selected\', \'router-1\')">选择路由器</button>' } } } })
  await flushPromises()
  return wrapper
}
async function click(text: string) {
  const button = wrapper!.findAll('button').find(item => item.text() === text)
  expect(button, text).toBeTruthy(); await button!.trigger('click'); await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks()
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }) })
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.mocked(setupApi.index).mockResolvedValue({ data: { sessions: [], suggested: true, subscriptions: [], nodes: [{ id: 'node', name: '已有节点' }], domain_rules: ruleOptions } } as never)
  vi.mocked(setupApi.check).mockResolvedValue({ data: installed({ goal: 'gateway' }) } as never)
  vi.mocked(setupApi.create).mockResolvedValue({ data: record() } as never)
  vi.mocked(setupApi.progress).mockImplementation(async (_id, data) => ({ data: installed(data) }) as never)
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('首次使用分流向导', () => {
  it('网关转发未就绪时不能提前进入客户端设置', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ goal: 'gateway', step: 3, max_step: 3, prepared: true }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    const clientStep = wrapper!.get('nav[aria-label="配置向导步骤"]').findAll('button')[4]!
    expect(clientStep.attributes('disabled')).toBeDefined()
    await clientStep.trigger('click'); await flushPromises()
    expect(wrapper!.findComponent(SetupInstallation).exists()).toBe(true)
    expect(wrapper!.text()).not.toContain('测试设备网关')
    expect(setupApi.progress).not.toHaveBeenCalled()
  })

  it('重新打开客户端步骤会核验转发，撤销或不健康时隐藏接入地址并阻止完成', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed({ goal: 'gateway' }) } as never)
    vi.mocked(setupApi.check).mockRejectedValue(new Error('Linux 转发尚未启用'))
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).toContain('暂时保持客户端原来的网关和 DNS')
    expect(wrapper!.text()).not.toContain('转发已就绪')
    expect(wrapper!.text()).not.toContain('测试设备网关')
    expect(wrapper!.findAll('button').find(button => button.text().includes('确认设置并继续'))!.attributes('disabled')).toBeDefined()
    expect(setupApi.finish).not.toHaveBeenCalled()
  })
  it.each(['', '。', '。。', '！', '？', '!?', '...', '； \n'])('客户端检查错误与后续提示之间只保留一个句号（后缀=%j）', async suffix => {
    const message = '暂时无法连接 Agent，请检查网络和 Agent 运行状态'
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed({ goal: 'gateway' }) } as never)
    vi.mocked(setupApi.check).mockRejectedValue({ isAxiosError: true, response: { data: { code: 'AGENT_UNREACHABLE', message: message + suffix } } })
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.get('[role="alert"]').text()).toBe(`${message}。请先返回安装与检查，暂时保持客户端原来的网关和 DNS。`)
    expect(wrapper!.findAll('button').find(button => button.text().includes('确认设置并继续'))!.attributes('disabled')).toBeDefined()
    expect(setupApi.finish).not.toHaveBeenCalled()
  })
  it.each(['router'] as const)('可预览 %s 接入设置，保留安装进度且不能确认完成', async goal => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ goal, step: 3, max_step: 3, prepared: true }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    const nav = wrapper!.get('nav[aria-label="配置向导步骤"]')
    await nav.findAll('button')[4]!.trigger('click'); await flushPromises()
    expect(wrapper!.text()).toContain('安装尚未完成')
    expect(wrapper!.text()).not.toContain('Mihomo、OxiDNS 已发布')
    expect(nav.findAll('button')[3]!.text()).toContain('4')
    expect(wrapper!.get('button:disabled').exists()).toBe(true)
    expect(wrapper!.findAll('[role="checkbox"]').every(item => item.attributes('disabled') !== undefined)).toBe(true)
    // Before install the new profile has no OxiDNS to name devices from, and the way back lands on step 3.
    expect(wrapper!.find('[data-client-devices-link]').exists()).toBe(false)
    await click('完成安装后继续')
    expect(setupApi.finish).not.toHaveBeenCalled()
    expect(setupApi.check).not.toHaveBeenCalled()
    expect(setupApi.progress).not.toHaveBeenCalled()
    await click('返回安装与检查')
    expect(wrapper!.findComponent(SetupInstallation).exists()).toBe(true)
    expect(setupApi.progress).not.toHaveBeenCalled()
  })

  it('安装前可填写主路由草稿，安装通过后才允许完成', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 3, max_step: 3, prepared: true }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    await wrapper!.get('nav[aria-label="配置向导步骤"]').findAll('button')[4]!.trigger('click'); await flushPromises()
    expect(wrapper!.text()).toContain('可直接照填的网段和 Linux IP')
    expect(wrapper!.text()).toContain('安装检查完成后自动显示 DNS 地址')
    await click('自动配置主路由')
    expect(wrapper!.text()).toContain('先添加主路由的管理地址和账号')
    expect(wrapper!.getComponent({ name: 'Routers' }).props('setupDraftSession')).toBe('a'.repeat(32))
    await click('完成安装后继续')
    expect(setupApi.finish).not.toHaveBeenCalled()
    expect(setupApi.progress).not.toHaveBeenCalled()
  })

  it.each(['router', 'gateway'] as const)('安装页接收 %s 方案，完成后重新检查真实安装再进入接入步骤', async goal => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ goal, step: 3, max_step: 3, prepared: true }) } as never)
    vi.mocked(setupApi.check).mockResolvedValue({ data: installed({ goal }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    const installer = wrapper!.getComponent(SetupInstallation)
    expect(installer.props()).toMatchObject({ goal, profileId: 'setup_test' })
    installer.vm.$emit('completed'); await flushPromises()
    expect(setupApi.check).toHaveBeenCalledExactlyOnceWith('a'.repeat(32))
    expect(wrapper!.findComponent(SetupInstallation).exists()).toBe(false)
    expect(wrapper!.text()).toContain(goal === 'router' ? '主路由静态路由' : '客户端接入检查')
    expect(setupApi.finish).not.toHaveBeenCalled()
  })

  it('只提供两个方案，直接说明静态路由或客户端网关的选择条件', async () => {
    await open()
    const choices = wrapper!.findAll('input[name="setup-goal"]')
    expect(choices.map(input => input.attributes('value'))).toEqual(['router', 'gateway'])
    expect(wrapper!.text()).toContain('Mihomo + OxiDNS 分流')
    expect(wrapper!.text()).toContain('能修改主路由的静态路由和 DNS')
    expect(wrapper!.text()).toContain('能修改客户端的网关和 DNS')
    expect(wrapper!.text()).not.toContain('手机或电脑')
    expect(wrapper!.text()).not.toContain('iKuai')
  })

  it('节点来源生成两项核心配置，安装未通过时保持当前步骤', async () => {
    await open(); await click('创建配置并继续'); await click('已有资源')
    await wrapper!.get('[role="checkbox"]').trigger('click')
    vi.mocked(setupApi.source).mockResolvedValue({ data: record({ step: 2, max_step: 2, node_ids: ['node'] }) } as never)
    await click('读取并继续')
    expect(setupApi.source).toHaveBeenCalledWith('a'.repeat(32), { subscription_ids: [], node_ids: ['node'] })
    vi.mocked(setupApi.prepare).mockResolvedValue({ data: record({ step: 3, max_step: 3, prepared: true, node_ids: ['node'], dns_fallback: 'mihomo', traffic_fallback: 'proxy' }) } as never)
    await click('生成配置并继续')
    expect(wrapper!.findComponent(SetupInstallation).exists()).toBe(true)
    vi.mocked(setupApi.check).mockRejectedValue(new Error('OxiDNS 发布尚未完成'))
    wrapper!.getComponent(SetupInstallation).vm.$emit('completed'); await flushPromises()
    expect(wrapper!.get('[role="alert"]').text()).toContain('OxiDNS 发布尚未完成')
    expect(setupApi.finish).not.toHaveBeenCalled()
  })

  it('新配置空间必须自己设置控制面板密码，拒绝默认 123456，提交后不再保留在页面', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2, node_ids: ['node'], controller_secret_saved: false }) } as never)
    vi.mocked(setupApi.prepare).mockResolvedValue({ data: record({ step: 2, max_step: 3, prepared: true, controller_secret_saved: true, controller_address: '0.0.0.0:9090' }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    const submit = () => wrapper!.findAll('button').find(button => button.text() === '生成配置并继续')!
    const input = wrapper!.get('#setup-controller-secret')
    expect(wrapper!.text()).toContain('控制面板密码（必填）')
    expect(wrapper!.text()).toContain('控制面板监听 0.0.0.0:9090，局域网内的设备都能打开，靠这个密码保护')
    expect(input.attributes('type')).toBe('password')
    expect(submit().attributes('disabled')).toBeDefined()
    await input.setValue('123456')
    expect(wrapper!.get('#setup-controller-secret-warning').text()).toContain('默认密码 123456')
    expect(submit().attributes('disabled')).toBeDefined()
    await input.setValue('short')
    expect(wrapper!.get('#setup-controller-secret-warning').text()).toContain('8–64')
    await input.setValue('my-panel-pass')
    await wrapper!.get('[aria-label="显示控制面板密码"]').trigger('click')
    expect(wrapper!.get('#setup-controller-secret').attributes('type')).toBe('text')
    expect(submit().attributes('disabled')).toBeUndefined()
    await click('生成配置并继续')
    expect(setupApi.prepare).toHaveBeenCalledWith('a'.repeat(32), expect.objectContaining({ controller_secret: 'my-panel-pass' }))
    // The saved password is not shown again; leaving the field empty keeps it.
    expect((wrapper!.get('#setup-controller-secret').element as HTMLInputElement).value).toBe('')
    expect(wrapper!.get('#setup-controller-secret').attributes('type')).toBe('password')
    expect(wrapper!.text()).toContain('已设置密码；留空保留原密码')
    expect(wrapper!.html()).not.toContain('my-panel-pass')
    await click('生成配置并继续')
    expect(vi.mocked(setupApi.prepare).mock.calls[1]![1]).not.toHaveProperty('controller_secret')
  })

  it('安装步骤列出向导对 Mihomo 基础配置的调整，没有调整时不显示', async () => {
    const changes = ['dns.listen 由 0.0.0.0:53 改为 127.0.0.1:1053', 'mode 由 global 改为 rule']
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 3, max_step: 3, prepared: true, mihomo_changes: changes }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    const notice = wrapper!.get('[data-mihomo-changes]')
    expect(notice.text()).toContain('其余设置原样保留')
    expect(notice.findAll('li').map(item => item.text())).toEqual(changes)
    wrapper!.unmount()
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 3, max_step: 3, prepared: true, mihomo_changes: [] }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.find('[data-mihomo-changes]').exists()).toBe(false)
  })

  it.each([
    ['mihomo', 'proxy', 'Mihomo DNS', '走代理'],
    ['mihomo', 'direct', 'Mihomo DNS', '直连'],
    ['direct', 'proxy', '直连 DNS', '走代理'],
    ['direct', 'direct', '直连 DNS', '直连'],
  ] as const)('DNS %s 与连接 %s 的漏网选择互相独立', async (dns, traffic, dnsLabel, trafficLabel) => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2, node_ids: ['node'] }) } as never)
    vi.mocked(setupApi.prepare).mockResolvedValue({ data: record({ step: 3, max_step: 3, prepared: true, dns_fallback: dns, traffic_fallback: traffic }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).not.toContain('代理优先')
    expect(wrapper!.text()).not.toContain('基础分流')
    await click(dnsLabel); await click(trafficLabel)
    const dnsGroup = wrapper!.get('[role="radiogroup"][aria-label="OxiDNS 未匹配域名的 DNS"]')
    expect(dnsGroup.findAll('button').find(button => button.attributes('aria-checked') === 'true')!.text()).toBe(dnsLabel)
    expect(wrapper!.text()).toContain('自动配置，无需填写')
    expect(dnsGroup.classes()).toContain('self-start')
    await click('生成配置并继续')
    expect(setupApi.prepare).toHaveBeenCalledWith('a'.repeat(32), { dns_fallback: dns, traffic_fallback: traffic, local_dns: 'https://223.5.5.5/dns-query', domain_rules: defaultRules, query_recorder: true })
  })

  it('DNS 查询记录默认开启，可关闭后随方案提交', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2, node_ids: ['node'] }) } as never)
    vi.mocked(setupApi.prepare).mockResolvedValue({ data: record({ step: 3, max_step: 3, prepared: true, query_recorder: false }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).toContain('记录 DNS 查询（OxiDNS 面板用）')
    const toggle = wrapper!.get('#setup-query-recorder')
    expect(toggle.attributes('aria-checked')).toBe('true')
    await toggle.trigger('click'); await flushPromises()
    expect(toggle.attributes('aria-checked')).toBe('false')
    await click('生成配置并继续')
    expect(setupApi.prepare).toHaveBeenCalledWith('a'.repeat(32), expect.objectContaining({ query_recorder: false }))
  })

  it.each([
    [{ query_recorder: false }, 'false'],
    [{ query_recorder: true }, 'true'],
    // Prepared before the switch existed: the profile was written with it off.
    [{}, 'false'],
  ])('恢复已生成方案的 DNS 查询记录选择 %o', async (changes, checked) => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 3, prepared: true, ...changes }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.get('#setup-query-recorder').attributes('aria-checked')).toBe(checked)
  })

  it('网关模式说明默认接管 Telegram 等应用的 IP，不选这两个规则集时不显示', async () => {
    vi.mocked(setupApi.index).mockResolvedValue({ data: { sessions: [], suggested: true, subscriptions: [], nodes: [], domain_rules: [...ruleOptions, { id: 'builtin:telegram', name: 'Telegram', source: 'builtin' }] } } as never)
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ goal: 'gateway', step: 2, max_step: 2, node_ids: ['node'], domain_rules: { direct: [], proxy: ['builtin:telegram'] } }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    const hint = 'Telegram、Google 推送（FCM）会直接连 IP，它们的 IP 网段也会交给 Mihomo。'
    expect(wrapper!.text()).toContain(hint)
    expect(wrapper!.find('#setup-ip-takeover').exists()).toBe(false)
    wrapper!.unmount()
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ goal: 'gateway', step: 2, max_step: 2, node_ids: ['node'], domain_rules: defaultRules }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).not.toContain(hint)
  })

  it('主路由模式不显示 IP 接管说明', async () => {
    vi.mocked(setupApi.index).mockResolvedValue({ data: { sessions: [], suggested: true, subscriptions: [], nodes: [], domain_rules: [...ruleOptions, { id: 'builtin:telegram', name: 'Telegram', source: 'builtin' }] } } as never)
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2, node_ids: ['node'], domain_rules: { direct: [], proxy: ['builtin:telegram'] } }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).not.toContain('它们的 IP 网段也会交给 Mihomo')
  })

  it('恢复已保存的漏网设置和直连 DNS 上游', async () => {
    const localDns = 'https://223.6.6.6/dns-query\ntls://223.5.5.5:853'
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 3, prepared: true, dns_fallback: 'direct', traffic_fallback: 'proxy', local_dns: localDns, domain_rules: { direct: ['library:work'], proxy: [] } }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.get('[role="radiogroup"][aria-label="OxiDNS 未匹配域名的 DNS"]').text()).toContain('直连 DNS')
    const selected = wrapper!.findAll('[role="radio"][aria-checked="true"]').map(button => button.text())
    expect(selected).toEqual(['直连 DNS', '走代理'])
    expect((wrapper!.get('#setup-local-dns').element as HTMLTextAreaElement).value).toBe(localDns)
    expect(wrapper!.get('#setup-direct-rules').text()).toContain('工作域名')
    expect(wrapper!.get('#setup-proxy-rules').text()).toContain('选择代理规则集')
  })

  it('直连上游始终显示，多行地址提交失败后保留输入以便重试', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2, node_ids: ['node'], dns_fallback: 'mihomo' }) } as never)
    vi.mocked(setupApi.prepare).mockRejectedValueOnce(new Error('直连 DNS 地址无效'))
      .mockResolvedValueOnce({ data: record({ step: 3, max_step: 3, prepared: true }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect((wrapper!.get('#setup-local-dns').element as HTMLTextAreaElement).value).toBe('https://223.5.5.5/dns-query')
    const localDns = 'https://223.6.6.6/dns-query\ntls://223.5.5.5:853'
    await wrapper!.get('#setup-local-dns').setValue(localDns)
    await click('生成配置并继续')
    expect(setupApi.prepare).toHaveBeenLastCalledWith('a'.repeat(32), { dns_fallback: 'mihomo', traffic_fallback: 'proxy', local_dns: localDns, domain_rules: defaultRules, query_recorder: true })
    expect(wrapper!.get('[role="alert"]').text()).toContain('直连 DNS 地址无效')
    expect((wrapper!.get('#setup-local-dns').element as HTMLTextAreaElement).value).toBe(localDns)
    await click('生成配置并继续')
    expect(wrapper!.findComponent(SetupInstallation).exists()).toBe(true)
  })

  it('规则集可替换或清空，限制同一个规则集同时选入两种去向', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2 }) } as never)
    vi.mocked(setupApi.prepare).mockRejectedValue(new Error('保持草稿'))
    await open('/setup?session=' + 'a'.repeat(32))
    const [direct, proxy] = wrapper!.findAllComponents(MultiSelect)
    expect(wrapper!.get('#setup-direct-rules').text()).toContain('Direct')
    expect(wrapper!.get('#setup-proxy-rules').text()).toContain('Proxy')
    direct!.vm.$emit('update:modelValue', ['library:work']); await flushPromises()
    expect(proxy!.props('options').some((item: {value: string}) => item.value === 'library:work')).toBe(false)
    await click('生成配置并继续')
    expect(setupApi.prepare).toHaveBeenLastCalledWith('a'.repeat(32), expect.objectContaining({ domain_rules: { direct: ['library:work'], proxy: ['builtin:proxy'] } }))
    direct!.vm.$emit('update:modelValue', []); proxy!.vm.$emit('update:modelValue', []); await flushPromises()
    expect(wrapper!.text()).toContain('未选择域名规则，所有域名将使用下方的默认 DNS')
  })

  it('直接填写普通 DNS 时就地提示格式，不等到下一步才失败', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2 }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    await wrapper!.get('#setup-local-dns').setValue('223.5.5.5')
    expect(wrapper!.get('#setup-local-dns').attributes('aria-invalid')).toBe('true')
    expect(wrapper!.get('[role="alert"]').text()).toContain('普通 DNS 地址不能用于此方案')
    expect(wrapper!.findAll('button').find(button => button.text() === '生成配置并继续')!.attributes('disabled')).toBeDefined()
    await wrapper!.get('#setup-local-dns').setValue('https://223.5.5.5/dns-query')
    expect(wrapper!.find('[role="alert"]').exists()).toBe(false)
  })

  it('新向导采用服务端的整套推荐规则，并可恢复推荐', async () => {
    const recommended = { direct: ['builtin:direct', 'builtin:steamcn'], proxy: ['builtin:openai', 'builtin:telegram'] }
    vi.mocked(setupApi.index).mockResolvedValue({ data: { sessions: [], suggested: true, subscriptions: [], nodes: [],
      recommended_domain_rules: recommended, domain_rules: [
        { id: 'builtin:direct', name: 'Direct', source: 'builtin', behavior: 'classical' },
        { id: 'builtin:steamcn', name: 'SteamCN', source: 'builtin', behavior: 'classical' },
        { id: 'builtin:openai', name: 'OpenAI', source: 'builtin', behavior: 'classical' },
        { id: 'builtin:telegram', name: 'Telegram', source: 'builtin', behavior: 'classical' },

      ] } } as never)
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2 }) } as never)
    vi.mocked(setupApi.prepare).mockRejectedValue(new Error('保留草稿'))
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.get('#setup-direct-rules').text()).toContain('Direct')
    expect(wrapper!.get('#setup-proxy-rules').text()).toContain('OpenAI')
    expect(wrapper!.text()).toContain('可直接继续')
    expect(wrapper!.findAllComponents(MultiSelect).flatMap(select => select.props('options')).every(item => item.value.startsWith('builtin:'))).toBe(true)
    wrapper!.findAllComponents(MultiSelect)[0]!.vm.$emit('update:modelValue', [])
    await flushPromises(); await click('恢复推荐'); await click('生成配置并继续')
    expect(setupApi.prepare).toHaveBeenCalledWith('a'.repeat(32), expect.objectContaining({ domain_rules: recommended }))
  })

  it('已有来源映射到同一个内置选项，恢复会话时显示已选名称', async () => {
    vi.mocked(setupApi.index).mockResolvedValue({ data: { sessions: [], suggested: true, subscriptions: [], nodes: [],
      domain_rules: [{ id: 'builtin:telegram', name: 'Telegram', source: 'builtin', aliases: ['library:old_telegram'] }] } } as never)
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2,
      domain_rules: { direct: [], proxy: ['library:old_telegram'] } }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.get('#setup-proxy-rules').text()).toContain('Telegram')
    expect(wrapper!.findAllComponents(MultiSelect)[1]!.props('modelValue')).toEqual(['builtin:telegram'])
  })

  it('内置规则与规则库同名但内容不同时注明来源，可改用规则库的规则集', async () => {
    vi.mocked(setupApi.index).mockResolvedValue({ data: { sessions: [], suggested: true, subscriptions: [], nodes: [], domain_rules: [
      ...ruleOptions,
      { id: 'builtin:grok', name: 'Grok', source: 'builtin', conflict: { id: 'library:my_grok', name: 'Grok' } },
      { id: 'library:my_grok', name: 'Grok', source: 'library' },
    ] } } as never)
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2,
      domain_rules: { direct: ['builtin:direct'], proxy: ['builtin:proxy', 'builtin:grok'] } }) } as never)
    vi.mocked(setupApi.prepare).mockRejectedValue(new Error('保持草稿'))
    await open('/setup?session=' + 'a'.repeat(32))
    const labels = wrapper!.findAllComponents(MultiSelect)[1]!.props('options').map((item: { label: string }) => item.label)
    expect(labels).toEqual(expect.arrayContaining(['Grok（内置）', 'Grok（规则库）', '工作域名']))
    expect(wrapper!.get('[data-rule-conflict]').text()).toContain('规则库已有同名的「Grok」')
    await click('改用规则库的「Grok」')
    expect(wrapper!.find('[data-rule-conflict]').exists()).toBe(false)
    await click('生成配置并继续')
    expect(setupApi.prepare).toHaveBeenLastCalledWith('a'.repeat(32), expect.objectContaining({
      domain_rules: { direct: ['builtin:direct'], proxy: ['builtin:proxy', 'library:my_grok'] } }))
  })

  it('副本已建或同名规则集不能选用时，仍注明来源且只提示不提供改用', async () => {
    vi.mocked(setupApi.index).mockResolvedValue({ data: { sessions: [], suggested: true, subscriptions: [], nodes: [], domain_rules: [
      ...ruleOptions,
      { id: 'builtin:grok', name: 'Grok', source: 'builtin', aliases: ['library:builtin_grok'] },
      { id: 'library:my_grok', name: 'Grok', source: 'library' },
      { id: 'builtin:openai', name: 'OpenAI', source: 'builtin', conflict: { name: 'OpenAI' } },
    ] } } as never)
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2,
      domain_rules: { direct: [], proxy: ['builtin:grok', 'builtin:openai'] } }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    const labels = wrapper!.findAllComponents(MultiSelect)[1]!.props('options').map((item: { label: string }) => item.label)
    expect(labels).toEqual(expect.arrayContaining(['Grok（内置）', 'Grok（规则库）', 'OpenAI']))
    const notices = wrapper!.findAll('[data-rule-conflict]')
    expect(notices).toHaveLength(1)
    expect(notices[0]!.text()).toContain('规则库已有同名的「OpenAI」')
    expect(notices[0]!.find('button').exists()).toBe(false)
  })

  it('订阅链接明文显示，读取失败保持来源步骤并使用同一会话重试', async () => {
    vi.mocked(setupApi.source).mockResolvedValue({ data: record({ step: 2, max_step: 2, subscription_ids: ['sub'] }) } as never)
    vi.mocked(subscriptionApi.fetch).mockRejectedValueOnce(new Error('订阅读取失败')).mockResolvedValueOnce({ data: { success: true, count: 3 } } as never)
    await open(); await click('创建配置并继续')
    expect(wrapper!.get('#setup-subscription').attributes('type')).toBe('url')
    await wrapper!.get('#setup-subscription').setValue('https://subscription.test/private')
    await click('读取并继续')
    expect(wrapper!.get('[role="alert"]').text()).toContain('订阅读取失败')
    expect(wrapper!.find('#setup-subscription').exists()).toBe(true)
    await click('读取并继续')
    expect(setupApi.source).toHaveBeenCalledTimes(2)
    expect(setupApi.source).toHaveBeenLastCalledWith('a'.repeat(32), { url: 'https://subscription.test/private' })
  })

  it('手动主路由配置提供实际网段和下一跳，并明确未自动核验', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed() } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).toContain('198.18.0.0/16')
    expect(wrapper!.text()).toContain('192.0.2.10')
    expect(wrapper!.text()).toContain('无法自动核验这台主路由')
    expect(wrapper!.findAll('button').find(button => button.text().includes('确认设置并继续'))!.attributes('disabled')).toBeDefined()
    for (const checkbox of wrapper!.findAll('[role="checkbox"]')) await checkbox.trigger('click')
    vi.mocked(setupApi.finish).mockResolvedValue({ data: installed({ step: 5, max_step: 5, completed: true, router_verification: 'manual' }) } as never)
    await click('确认设置并继续')
    expect(setupApi.finish).toHaveBeenCalledWith('a'.repeat(32), { router_mode: 'manual', router_id: '', ack_routes: true, ack_dns: true })
    expect(wrapper!.text()).toContain('主路由为手动配置，尚未自动核验')
    expect(wrapper!.text()).toContain('客户端连通性尚未由本向导确认')
  })

  it('完成时可设为默认配置空间，并提示原来的默认配置和示例订阅可以删除', async () => {
    const current = { id: 'default', name: '默认配置' }
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed({ default_profile: { is_default: false, suggested: true, current } }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).toContain('设为默认配置空间')
    expect(wrapper!.text()).toContain('现在的默认是「默认配置」')
    const checkboxes = wrapper!.findAll('[role="checkbox"]')
    expect(checkboxes.at(-1)!.attributes('aria-checked')).toBe('true')
    for (const checkbox of checkboxes.slice(0, -1)) await checkbox.trigger('click')
    vi.mocked(setupApi.finish).mockResolvedValue({ data: installed({ step: 5, max_step: 5, completed: true, router_verification: 'manual', previous_default: 'default',
      default_profile: { is_default: true, suggested: false, current: { id: 'setup_test', name: '我的分流配置' },
        leftovers: { profile: current, subscriptions: [{ id: 'sub', name: 'subscribe' }] } } }) } as never)
    await click('确认设置并继续')
    expect(setupApi.finish).toHaveBeenCalledWith('a'.repeat(32), { router_mode: 'manual', router_id: '', ack_routes: true, ack_dns: true, make_default: true })
    expect(wrapper!.text()).toContain('已设为默认配置空间')
    expect(wrapper!.text()).toContain('原来的「默认配置」没有设备在用')
    expect(wrapper!.text()).toContain('示例订阅「subscribe」只被原来的默认配置使用')
  })

  it('手动接入时可先到客户端设备给测试设备命名，再返回本向导', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed() } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    const link = wrapper!.get('[data-client-devices-link] a')
    expect(link.text()).toBe('客户端设备')
    const target = new URL(link.attributes('href')!, 'http://configflow.test')
    expect(target.pathname).toBe('/client-devices')
    expect(target.searchParams.get('profile')).toBe('setup_test')
    expect(target.searchParams.get('from')).toBe('/setup?session=' + 'a'.repeat(32) + '&ack=')
    await link.trigger('click'); await flushPromises()
    expect(wrapper!.text()).toContain('客户端设备页')
  })

  it('从客户端设备返回时保留手动接入和已勾选的确认', async () => {
    // The manual choice is not saved until finish, so the stored mode can still be managed.
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed({ router_mode: 'managed' }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    await click('手动配置主路由')
    const boxes = () => wrapper!.findAll('[role="checkbox"]')
    await boxes()[0]!.trigger('click'); await flushPromises()
    const href = wrapper!.get('[data-client-devices-link] a').attributes('href')!
    const back = new URL(href, 'http://configflow.test').searchParams.get('from')!
    expect(back).toBe('/setup?session=' + 'a'.repeat(32) + '&ack=routes')
    wrapper!.unmount()
    await open(back)
    expect(wrapper!.text()).toContain('1. 在主路由添加静态路由')
    expect(boxes().map(box => box.attributes('aria-checked'))).toEqual(['true', 'false'])
    await boxes()[1]!.trigger('click'); await flushPromises()
    expect(wrapper!.findAll('button').find(item => item.text() === '确认设置并继续')!.attributes('disabled')).toBeUndefined()
  })

  it('返回参数只在第 4 步恢复', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed({ step: 5, max_step: 5, completed: true }) } as never)
    await open('/setup?session=' + 'a'.repeat(32) + '&ack=routes,dns')
    expect(wrapper!.text()).toContain('Mihomo + OxiDNS 已准备好')
  })

  it('完成页进入 Agent 或客户端设备前切到本次配置空间', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed({ step: 5, max_step: 5, completed: true }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).not.toContain('进入设备管理')
    await click('客户端设备')
    expect(setActiveProfileId).toHaveBeenCalledWith('setup_test')
    expect(wrapper!.text()).toContain('客户端设备页')
  })

  it('设为默认时说明原配置空间的客户端设备名称和 DNS 策略不会带过来', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed({ default_profile: { is_default: false, suggested: true, current: { id: 'default', name: '默认配置' } } }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).toContain('「默认配置」中的客户端设备名称和 DNS 策略不会带过来')
  })

  it('默认配置空间有设备在用时不预先勾选', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed({ default_profile: { is_default: false, suggested: false, current: { id: 'default', name: '默认配置' } } }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    const checkboxes = wrapper!.findAll('[role="checkbox"]')
    expect(checkboxes.at(-1)!.attributes('aria-checked')).toBe('false')
    for (const checkbox of checkboxes.slice(0, -1)) await checkbox.trigger('click')
    vi.mocked(setupApi.finish).mockResolvedValue({ data: installed({ step: 5, max_step: 5, completed: true, router_verification: 'manual' }) } as never)
    await click('确认设置并继续')
    expect(setupApi.finish).toHaveBeenCalledWith('a'.repeat(32), expect.objectContaining({ make_default: false }))
    expect(wrapper!.text()).not.toContain('已设为默认配置空间')
  })

  it('自动入口固定配置空间和本次 Agent，完成前调用服务端核验', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed() } as never)
    await open('/setup?session=' + 'a'.repeat(32)); await click('自动配置主路由')
    const routers = wrapper!.getComponent({ name: 'Routers' })
    expect(routers.props('profileId')).toBe('setup_test')
    expect(routers.props('setupAgents')).toEqual({ mihomo: 'm', oxidns: 'o' })
    await click('选择路由器')
    vi.mocked(setupApi.finish).mockRejectedValue(new Error('路由器配置尚未同步'))
    await click('检查路由器并完成')
    expect(setupApi.finish).toHaveBeenCalledWith('a'.repeat(32), expect.objectContaining({ router_mode: 'managed', router_id: 'router-1' }))
    expect(wrapper!.get('[role="alert"]').text()).toContain('路由器配置尚未同步')
  })

  it('网关方案提示客户端网关和 DNS，不要求接入特定品牌路由器', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed({ goal: 'gateway' }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).toContain('测试设备网关')
    expect(wrapper!.text()).toContain('192.0.2.10')
    expect(wrapper!.text()).not.toContain('自动配置主路由')
    expect(setupApi.create).not.toHaveBeenCalled()
  })
})

it('abandons the open setup from the page header and lists it as abandoned', async () => {
  vi.mocked(setupApi.index).mockResolvedValue({ data: { sessions: [record({ step: 2, max_step: 2 })], suggested: false, subscriptions: [], nodes: [], domain_rules: ruleOptions } } as never)
  vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 2 }) } as never)
  vi.mocked(setupApi.abandon).mockResolvedValue({ data: { session_id: 'a'.repeat(32), name: '我的分流配置', profile: { id: 'setup_test', name: '我的分流配置' },
    delete_profile: false, kept_reason: '已有设备在使用它（Mihomo）', guidance: '', subscription: null } } as never)
  const { confirm } = await import('@/lib/feedback')
  vi.mocked(confirm).mockResolvedValueOnce(true)
  await open('/setup?session=' + 'a'.repeat(32))
  await click('放弃此向导')
  expect(setupApi.abandon).toHaveBeenNthCalledWith(1, 'a'.repeat(32), true)
  expect(setupApi.abandon).toHaveBeenNthCalledWith(2, 'a'.repeat(32))
  expect(wrapper!.text()).toContain('已放弃')
  expect(wrapper!.findAll('button').some(item => item.text() === '放弃此向导')).toBe(false)
})

describe('主路由分流改为完整 TUN', () => {
  it('说明安装时开启并持久化 IPv4 转发、准备 TUN，网关方案文案不变', () => {
    const [routerGoal, gatewayGoal] = SETUP_GOALS
    expect(routerGoal.help).toContain('安装时开启并持久化 IPv4 转发、准备 TUN')
    expect(routerGoal.help).toContain('Mihomo 以完整 TUN 接管主路由转来的连接')
    expect(routerGoal.help).toContain('客户端默认网关仍指向主路由')
    expect(gatewayGoal.help).toContain('显式启用 Linux 转发')
  })

  it('完成安装后显示转发与 TUN 已检查，真实 IP 网段提示按来源排除 Linux', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: installed({ route_instructions: { gateway: '192.0.2.10', dns: '192.0.2.10', warnings: [],
      routes: [{ cidr: '198.18.0.0/16', sources: ['fake-ip'] }, { cidr: '91.108.4.0/22', sources: ['Telegram'], requires_source_exclusion: true }] } }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.text()).toContain('Mihomo、OxiDNS 已发布，IPv4 转发与 TUN 已检查')
    expect(wrapper!.text()).toContain('真实 IP 网段需在主路由按来源排除 Linux 本机（策略路由），否则 Linux 直连这些地址时会被转回自己；只能按目的地址配置时，只添加 fake-IP 网段。')
    expect(wrapper!.find('[data-legacy-takeover]').exists()).toBe(false)
  })

  it.each([3, 4])('旧版向导准备的方案在第 %s 步提示保持旧版接管', async step => {
    const session = step === 3 ? record({ step: 3, max_step: 3, prepared: true, legacy_takeover: true }) : installed({ legacy_takeover: true })
    vi.mocked(setupApi.get).mockResolvedValue({ data: session } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.get('[data-legacy-takeover]').text()).toBe('此配置空间使用旧版 Linux 网关接管，保持不变。')
    if (step === 4) expect(wrapper!.text()).toContain('Mihomo、OxiDNS 已发布，Linux 转发已检查')
  })

  it('转发规则集未下载时提示重新检查，确认前不能完成，网段变化后需重新勾选', async () => {
    const missing = installed({ route_instructions: { gateway: '192.0.2.10', dns: '192.0.2.10', routes: [{ cidr: '198.18.0.0/16', sources: ['fake-ip'] }],
      warnings: ['规则集「Telegram」没有本地缓存，未计入其中的 IP 段'], incomplete: true, missing_rule_sets: ['Telegram'] } })
    vi.mocked(setupApi.get).mockResolvedValue({ data: missing } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    const finish = () => wrapper!.findAll('button').find(item => item.text() === '确认设置并继续')!
    for (const box of wrapper!.findAll('button[role="checkbox"]')) await box.trigger('click')
    await flushPromises()
    expect(wrapper!.get('[data-routes-incomplete]').text()).toContain('规则集「Telegram」的 IP 段尚未下载，上表缺少这些网段，暂不能确认完成。')
    expect(finish().attributes('disabled')).toBeDefined()
    vi.mocked(setupApi.check).mockResolvedValue({ data: installed({ route_instructions: { gateway: '192.0.2.10', dns: '192.0.2.10', warnings: [], incomplete: false,
      routes: [{ cidr: '91.108.4.0/22', sources: ['Telegram'], requires_source_exclusion: true }, { cidr: '198.18.0.0/16', sources: ['fake-ip'] }] } }) } as never)
    await click('重新检查')
    expect(setupApi.check).toHaveBeenCalledWith('a'.repeat(32))
    expect(wrapper!.find('[data-routes-incomplete]').exists()).toBe(false)
    expect(wrapper!.text()).toContain('91.108.4.0/22')
    // The static routes were confirmed against the shorter table.
    expect(finish().attributes('disabled')).toBeDefined()
    await wrapper!.findAll('button[role="checkbox"]')[0]!.trigger('click'); await flushPromises()
    expect(finish().attributes('disabled')).toBeUndefined()
  })

  it('旧版提示不出现在其他步骤', async () => {
    vi.mocked(setupApi.get).mockResolvedValue({ data: record({ step: 2, max_step: 3, prepared: true, legacy_takeover: true }) } as never)
    await open('/setup?session=' + 'a'.repeat(32))
    expect(wrapper!.find('[data-legacy-takeover]').exists()).toBe(false)
  })
})

describe('useLibraryNamesake', () => {
  const grok = { id: 'builtin:grok', name: 'Grok', source: 'builtin' as const, conflict: { id: 'library:my_grok', name: 'Grok' } }
  it('在原去向就地替换，保留顺序', () => {
    expect(useLibraryNamesake({ direct: [], proxy: ['builtin:a', 'builtin:grok', 'builtin:b'] }, grok))
      .toEqual({ direct: [], proxy: ['builtin:a', 'library:my_grok', 'builtin:b'] })
  })
  it('同名规则集已在本去向或另一去向时只取消内置项', () => {
    expect(useLibraryNamesake({ direct: [], proxy: ['library:my_grok', 'builtin:grok'] }, grok)).toEqual({ direct: [], proxy: ['library:my_grok'] })
    expect(useLibraryNamesake({ direct: ['library:my_grok'], proxy: ['builtin:grok'] }, grok)).toEqual({ direct: ['library:my_grok'], proxy: [] })
  })
})
