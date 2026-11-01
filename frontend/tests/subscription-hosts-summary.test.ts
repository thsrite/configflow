import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, defineComponent, markRaw } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import GenerateStudio from '@/components/generate/GenerateStudio.vue'
import SubscriptionHostsSummary from '@/components/generate/SubscriptionHostsSummary.vue'
import { generateApi, ruleSetApi, statsApi } from '@/api'
import type { SubscriptionHostsDns, SubscriptionHostsReport } from '@/types'

vi.mock('@/api', () => ({
  agentApi: { pushConfig: vi.fn(), getDeployment: vi.fn() },
  generateApi: { artifact: vi.fn() },
  statsApi: { getOverview: vi.fn() }, ruleSetApi: { getAll: vi.fn() },
}))
vi.mock('@/stores/live', () => ({ useLive: () => ({ boundAgents: computed(() => []), isOnline: () => true, start: vi.fn(), stop: vi.fn(), refresh: vi.fn() }) }))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() } }))

const report = (overrides: Partial<SubscriptionHostsReport> = {}): SubscriptionHostsReport => ({
  merged: 2,
  ignored: 2,
  domain_mapped: 1,
  subscriptions: [{ id: 'air', name: '机场', total: 4, merged: 2, ignored: 2 }],
  items: [
    { subscription: '机场', domain: 'hk.example.net', value: 'hk.example.org', status: 'merged', kind: 'domain' },
    { subscription: '机场', domain: 'jp.example.net', value: ['198.51.100.12', '2001:db8::12'], status: 'merged', kind: 'address' },
    { subscription: '机场', domain: 'google.com', value: '203.0.113.66', status: 'ignored', reason: 'not_node_domain', detail: '不是这个订阅里被使用节点的 server 域名' },
  ],
  ...overrides,
})

const dns = (overrides: Partial<SubscriptionHostsDns> = {}): SubscriptionHostsDns => ({
  mapped: 3,
  applied: true,
  listener: 'udp://127.0.0.1:1053',
  policy: [
    { domain: 'hk.example.net', target: 'hk.example.org', source: 'configflow' },
    { domain: 'jp.example.net', target: 'jp.example.org', source: 'configflow' },
    { domain: 'de.example.net', target: 'de.example.org', source: 'configflow' },
  ],
  filter: ['+.example.org'],
  targets_filtered: 3,
  targets_covered: 0,
  fake_ip: 'blacklist',
  skipped: [],
  warnings: [],
  notes: [],
  min_version: 'v1.19.20',
  ...overrides,
})

const text = (wrapper: VueWrapper, selector: string) => wrapper.get(selector).text().replace(/\s+/g, ' ').trim()

const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); document.body.innerHTML = ''; vi.useRealTimers(); vi.unstubAllGlobals() })

describe('subscription hosts summary', () => {
  it('states merged and ignored counts; a mapping that ends in an IP needs nothing more', () => {
    const wrapper = mount(SubscriptionHostsSummary, { props: { report: report() } })
    wrappers.push(wrapper)
    expect(wrapper.get('[data-subscription-hosts-counts]').text().replace(/\s+/g, '')).toBe('已合并2条，忽略2条')
    expect(wrapper.get('[data-subscription-hosts-domain-note]').text())
      .toBe('其中 1 条是「域名 → 域名」，在 hosts 内最终指向 IP，Mihomo 直接连接这个 IP。')
    expect(wrapper.find('[data-subscription-hosts-dns]').exists()).toBe(false)
    expect(wrapper.find('#subscription-hosts-items').exists()).toBe(false)
  })

  it('says which node domains go to the DNS listener and lists the entries read-only on request', async () => {
    const wrapper = mount(SubscriptionHostsSummary, { props: { report: report({ dns: dns() }) }, attachTo: document.body })
    wrappers.push(wrapper)
    expect(wrapper.find('[data-subscription-hosts-domain-note]').exists()).toBe(false)
    expect(text(wrapper, '[data-subscription-hosts-dns] p'))
      .toBe('3 个被映射的节点域名改由本机 DNS 监听 127.0.0.1:1053 解析，3 个映射目标已加入 Fake-IP 过滤。')
    const toggle = wrapper.get('button[aria-controls="subscription-hosts-dns-entries"]')
    expect(toggle.text()).toBe('查看条目')
    expect(wrapper.find('#subscription-hosts-dns-entries').exists()).toBe(false)
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    const groups = wrapper.findAll('#subscription-hosts-dns-entries [data-dns-group]')
    expect(groups.map(group => [group.get('p').text(), group.findAll('li').map(li => li.text().replace(/\s+/g, ' '))])).toEqual([
      ['proxy-server-nameserver-policy：交给本机 DNS 监听解析',
        ['hk.example.net → hk.example.org', 'jp.example.net → jp.example.org', 'de.example.net → de.example.org']],
      ['fake-ip-filter：新增条目', ['+.example.org']],
    ])
    // Read-only: nothing to edit or remove here.
    expect(wrapper.findAll('#subscription-hosts-dns-entries input, #subscription-hosts-dns-entries button')).toHaveLength(0)
  })

  it('names conflicts, old cores and targets the filter already covered', async () => {
    const conflict = '你已为 de.example.net 写了 proxy-server-nameserver-policy，保留你的设置；这条映射对该节点不生效'
    const wrapper = mount(SubscriptionHostsSummary, {
      props: { report: report({ dns: dns({
        policy: dns().policy.slice(0, 2).map((item, i) => ({ ...item, source: i ? 'user' : 'configflow' })),
        filter: [], targets_filtered: 0, targets_covered: 2,
        skipped: [{ domain: 'de.example.net', target: 'de.example.org', reason: 'policy_conflict' }],
        warnings: [conflict], outdated_agents: [{ name: '客厅软路由', version: 'v1.19.10' }],
      }) }) },
      attachTo: document.body,
    })
    wrappers.push(wrapper)
    expect(text(wrapper, '[data-subscription-hosts-dns] p')).toBe(
      '2 个被映射的节点域名改由本机 DNS 监听 127.0.0.1:1053 解析，2 个映射目标已在 fake-ip-filter 里。'
      + '其中 1 个是你写的 proxy-server-nameserver-policy，已指向这个监听，保留原值。')
    const warnings = wrapper.findAll('[data-subscription-hosts-dns-warning]')
    expect(warnings.map(item => item.text().replace(/\s+/g, ' '))).toEqual([
      conflict,
      'Agent「客厅软路由」上报的 Mihomo 内核为 v1.19.10，早于 v1.19.20：推送给它的配置不加 proxy-server-nameserver-policy，映射在这台设备上不生效，升级内核后重新推送。',
    ])
    expect(warnings.every(item => item.classes().includes('text-warning-accent'))).toBe(true)
    await wrapper.get('button[aria-controls="subscription-hosts-dns-entries"]').trigger('click')
    expect(wrapper.findAll('#subscription-hosts-dns-entries [data-dns-group]').map(group => group.attributes('data-dns-group'))).toEqual(['policy'])
    expect(wrapper.get('#subscription-hosts-dns-entries li:last-child').text()).toContain('（你写的）')
  })

  it('says why nothing changed when the mapping cannot apply', () => {
    const warning = '没有写 proxy-server-nameserver：Mihomo 要求先有它才接受 proxy-server-nameserver-policy，否则拒绝加载配置；ConfigFlow 不替你填写，在基础配置的 dns 里写上（例如公网 DoH）后重新生成'
    const wrapper = mount(SubscriptionHostsSummary, {
      props: { report: report({ dns: dns({ applied: false, policy: [], filter: [], targets_filtered: 0, fake_ip: null, warnings: [warning] }) }) },
    })
    wrappers.push(wrapper)
    expect(text(wrapper, '[data-subscription-hosts-dns]')).toBe('其中 3 条「域名 → 域名」映射对节点连接不生效，节点仍连接左侧域名自己的地址：')
    const shown = wrapper.get('[data-subscription-hosts-dns-warning]')
    expect(shown.text()).toBe(warning)
    expect(shown.classes()).toContain('text-warning-accent')
    expect(wrapper.find('button[aria-controls="subscription-hosts-dns-entries"]').exists()).toBe(false)
  })

  it('outside Fake-IP only the policy is added, with notes kept visible', async () => {
    const note = '需要 Mihomo v1.19.20 或更新：更旧的内核会忽略 proxy-server-nameserver-policy，这些节点照旧连接左侧域名自己的地址'
    const wrapper = mount(SubscriptionHostsSummary, {
      props: { report: report({ dns: dns({ fake_ip: 'not_needed', filter: [], targets_filtered: 0, notes: [note] }) }) },
      attachTo: document.body,
    })
    wrappers.push(wrapper)
    expect(text(wrapper, '[data-subscription-hosts-dns] p'))
      .toBe('3 个被映射的节点域名改由本机 DNS 监听 127.0.0.1:1053 解析。')
    expect(wrapper.text()).toContain(note)
    await wrapper.get('button[aria-controls="subscription-hosts-dns-entries"]').trigger('click')
    expect(wrapper.findAll('#subscription-hosts-dns-entries [data-dns-group]')).toHaveLength(1)
  })

  it('names real-ip rules in rule mode', async () => {
    const wrapper = mount(SubscriptionHostsSummary, { props: { report: report({ dns: dns({ fake_ip: 'rule' }) }) }, attachTo: document.body })
    wrappers.push(wrapper)
    await wrapper.get('button[aria-controls="subscription-hosts-dns-entries"]').trigger('click')
    expect(wrapper.get('[data-dns-group="filter"] p').text()).toBe('fake-ip-filter：新增 real-ip 规则')
  })

  it('lists each entry with its subscription, value and the reason it was ignored', async () => {
    const wrapper = mount(SubscriptionHostsSummary, { props: { report: report() }, attachTo: document.body })
    wrappers.push(wrapper)
    const toggle = wrapper.get('button[aria-controls="subscription-hosts-items"]')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    const rows = wrapper.findAll('#subscription-hosts-items li')
    // Separate spans keep domain, value and subscription apart for the reader.
    expect(rows.map(row => row.findAll(':scope > span').map(span => span.text()).filter(text => text !== '→'))).toEqual([
      ['已合并', 'hk.example.net', 'hk.example.org', '· 机场'],
      ['已合并', 'jp.example.net', '198.51.100.12, 2001:db8::12', '· 机场'],
      ['已忽略', 'google.com', '203.0.113.66', '· 机场', '不是这个订阅里被使用节点的 server 域名'],
      [],
    ])
    expect(rows[3]!.text()).toBe('另有 1 条未列出，原因见服务日志。')
    expect(rows[0]!.get('.chip').classes()).toContain('chip-ok')
    expect(rows[2]!.get('.chip').classes()).not.toContain('chip-ok')
  })

  it('omits the domain note when every merged entry is an address', () => {
    const wrapper = mount(SubscriptionHostsSummary, { props: { report: report({ domain_mapped: 0 }) } })
    wrappers.push(wrapper)
    expect(wrapper.find('[data-subscription-hosts-domain-note]').exists()).toBe(false)
  })
})

describe('generation studio', () => {
  const icon = markRaw(defineComponent({ template: '<i />' }))
  beforeEach(() => {
    vi.resetAllMocks()
    vi.useFakeTimers()
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => setTimeout(() => cb(1), 1))
    vi.mocked(statsApi.getOverview).mockResolvedValue({ data: { success: true, data: {} } } as any)
    vi.mocked(ruleSetApi.getAll).mockResolvedValue({ data: [] } as any)
  })

  async function generate(key: 'mihomo' | 'surge', hosts: SubscriptionHostsReport | null) {
    vi.mocked(generateApi.artifact).mockResolvedValue({ data: { id: 'preview', agent_id: 'manual', content: 'hosts: {}', config_path: 'config.yaml', files: [{ path: 'config.yaml', size: 9 }], target_ids: [], subscription_hosts: hosts } } as any)
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: icon }] })
    await router.push('/')
    const targets = [{ key: 'mihomo', title: 'Mihomo' }, { key: 'surge', title: 'Surge' }]
      .map(item => ({ ...item, icon, desc: '', url: '', urlDisplay: '', actions: [] })) as any
    const wrapper = mount(GenerateStudio, { props: { targets, profileId: 'default' }, global: { plugins: [router] }, attachTo: document.body })
    wrappers.push(wrapper)
    if (key === 'surge') await wrapper.findAll('button').find(b => b.text() === 'Surge')!.trigger('click')
    await wrapper.findAll('button').find(b => b.text() === '生成')!.trigger('click')
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    return wrapper
  }

  it('shows the summary after a Mihomo generation that used subscription hosts', async () => {
    const wrapper = await generate('mihomo', report())
    expect(wrapper.get('[data-subscription-hosts]').text()).toContain('订阅 hosts')
  })

  it('stays silent when no subscription carries hosts or for other targets', async () => {
    expect((await generate('mihomo', report({ merged: 0, ignored: 0, domain_mapped: 0, items: [] }))).find('[data-subscription-hosts]').exists()).toBe(false)
    expect((await generate('mihomo', null)).find('[data-subscription-hosts]').exists()).toBe(false)
    expect((await generate('surge', report())).find('[data-subscription-hosts]').exists()).toBe(false)
  })
})
