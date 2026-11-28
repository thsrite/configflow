import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosHeaders, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import DomainDiscovery from '@/views/DomainDiscovery.vue'
import api from '@/api'
import { scopedRequests, setActiveProfileId } from '@/profileContext'

// 只用合成数据；手机宽度（< 640px）只让 max-width: 639px 命中
vi.mock('@/router', () => ({ default: { push: vi.fn() } }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/lib/feedback', () => ({
  confirmDanger: vi.fn(async () => true),
  notify: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warning: vi.fn() }
}))

const requests: InternalAxiosRequestConfig[] = []
const wrappers: VueWrapper[] = []
const originalAdapter = api.defaults.adapter
const response = (config: InternalAxiosRequestConfig, data: unknown): AxiosResponse => ({
  config, data, status: 200, statusText: 'OK', headers: new AxiosHeaders()
})
const agents = {
  agents: [{ id: 'collector', name: 'Synthetic Mihomo', enabled: true, supported: true, probe_supported: true,
    status: 'running', version: '1.6.0-go', mihomo_version: '1.19.32', last_report: '2026-10-11T12:00:00Z' }],
  min_agent_version: '1.6.0-go', min_probe_agent_version: '1.6.0-go'
}
const longDomain = `very-long-synthetic-subdomain-label-${'x'.repeat(40)}.example.test`
const domains = {
  items: [{
    domain: longDomain, tags: ['uncovered', 'failing'], conns: 12, fails: 3, fail_kinds: { timeout: 3 },
    routes: [{ rule: 'MATCH', rule_payload: '', outlet: 'direct', policy: 'DIRECT' }],
    hosts: [{ host: `a.${longDomain}`, conns: 6, fails: 1, uncovered: true, pending_rule: null, probe: null },
      { host: `b.${longDomain}`, conns: 6, fails: 2, uncovered: true, pending_rule: null, probe: null }],
    last_seen: '2026-10-11T12:00:00Z', pending_rule: null, suggestion: null
  }], sniff_coverage: 1, ...agents
}
const settings = {
  direct: { id: 'direct', name: 'Direct', behavior: 'domain', active: true, problem: null, references: [{ policy: 'DIRECT' }] },
  proxy: { id: 'proxy', name: 'Proxy', behavior: 'domain', active: true, problem: null, references: [{ policy: 'Proxy' }] },
  ignored: [], candidates: [], policies: [], auto_probe: false, auto_apply: false,
  auto_apply_min_confidence: 80, confidence_choices: [80], probe_path: 'Proxy'
}
const history = { items: [{ value: 'added.example.test', rule_type: 'DOMAIN-SUFFIX', target: 'proxy', ruleset: 'Proxy',
  source: 'manual', applied_at: '2026-10-11T12:00:00Z', verdict: null, confidence: null, recheck: null }] }

beforeEach(() => {
  const storage = () => {
    const values = new Map<string, string>()
    return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key), clear: () => values.clear() }
  }
  vi.stubGlobal('localStorage', storage())
  vi.stubGlobal('sessionStorage', storage())
  requests.length = 0
  scopedRequests.value = 0
  setActiveProfileId('discovery-profile')
  api.defaults.adapter = async config => {
    requests.push(config)
    if (config.url === '/domain-discovery/domains') return response(config, domains)
    if (config.url === '/domain-discovery/agents') return response(config, agents)
    if (config.url === '/domain-discovery/settings') return response(config, settings)
    if (config.url === '/domain-discovery/history') return response(config, history)
    if (config.url === '/domain-discovery/region') return response(config, { enabled: false, domains: {}, services: [] })
    if (config.url === '/domain-discovery/apply') return response(config, { added: [{ value: longDomain }], skipped: [] })
    if (config.url === '/domain-discovery/ignore') return response(config, { success: true })
    throw new Error(`Unexpected request: ${config.url}`)
  }
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: String(query).includes('639px'), media: query, onchange: null, addListener: vi.fn(), removeListener: vi.fn(),
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn()
  })))
})

afterEach(async () => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  await flushPromises()
  api.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

async function render() {
  const wrapper = mount(DomainDiscovery, { attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

async function openMenu(wrapper: VueWrapper) {
  const trigger = wrapper.get(`button[aria-label="${longDomain} 的操作"]`)
  await trigger.trigger('pointerdown', { button: 0, ctrlKey: false, pointerType: 'mouse' })
  await flushPromises()
  if (!document.querySelector('[role="menu"]')) {
    await trigger.trigger('keydown', { key: 'Enter' })
    await flushPromises()
  }
  const menu = document.querySelector<HTMLElement>('[role="menu"]')
  expect(menu).not.toBeNull()
  return menu!
}

describe('domain discovery on phones', () => {
  it('renders each domain as a compact two-line record with actions in a ⋯ menu', async () => {
    const wrapper = await render()
    const shell = wrapper.get('.domain-item-rows')
    expect(shell.classes()).toContain('domain-phone-rows')
    const row = shell.get('tbody tr')
    const cells = row.findAll('td')
    expect(cells).toHaveLength(8)
    // 第一行：完整域名在可截断的元素里，悬停可看全称；连接数带单位
    const toggle = cells[1].get('button.domain-toggle')
    expect(toggle.get('span.truncate').text()).toBe(longDomain)
    expect(toggle.attributes('title')).toBe(longDomain)
    expect(cells[2].text()).toBe('12 连接')
    // 第二行：标签、子域数与失败次数；桌面才有的说明和网站按钮不在行内
    expect(cells[5].text()).toContain('未覆盖')
    expect(cells[5].text()).toContain('2 个子域')
    expect(cells[3].text()).toBe('失败 3')
    expect(cells[1].find('button[aria-label^="查看"]').exists()).toBe(false)
    // 行内只有勾选、域名和「⋯」三个控件
    expect(row.findAll('button').map(button => button.attributes('aria-label'))).toEqual([
      `选择 ${longDomain}`, `展开 ${longDomain} 的子域`, `${longDomain} 的操作`
    ])
    expect(wrapper.findAll('button').some(button => button.text().trim() === '加入代理')).toBe(false)
  })

  it('offers the same row actions from the ⋯ menu', async () => {
    const wrapper = await render()
    const menu = await openMenu(wrapper)
    const items = [...menu.querySelectorAll('[role="menuitem"]')].map(item => item.textContent?.trim())
    expect(items).toEqual(['加入代理', '加入直连', '探测直连与代理', '网站未知 · 识别 / 备注', '忽略'])
    const proxy = [...menu.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(item => item.textContent?.trim() === '加入代理')!
    proxy.click()
    await flushPromises()
    const apply = requests.find(config => config.url === '/domain-discovery/apply')
    expect(apply).toBeTruthy()
    expect(JSON.parse(String(apply!.data))).toEqual({ items: [{ rule_type: 'DOMAIN-SUFFIX', value: longDomain, target: 'proxy' }] })
  })

  it('keeps expanded subdomains and the history view free of wide table columns', async () => {
    const wrapper = await render()
    await wrapper.get('button.domain-toggle').trigger('click')
    const hosts = wrapper.get('tr.domain-hosts-row')
    expect(hosts.text()).toContain(`a.${longDomain}`)
    expect(hosts.findAll('button.domain-compact-action').map(button => button.text())).toContain('仅此子域代理')

    const historyTab = wrapper.findAll('button[role="radio"]').find(button => button.text().trim() === '已加入')!
    await historyTab.trigger('click')
    await flushPromises()
    const shell = wrapper.get('.domain-history-rows')
    const row = shell.get('tbody tr')
    expect(row.findAll('td')[0].text()).toContain('added.example.test')
    expect(row.get('button.domain-compact-action').text()).toContain('撤销')
  })

  it('按窗口渲染并保留展开状态，搜索后的批量提交覆盖全部匹配项', async () => {
    const items = Array.from({ length: 836 }, (_, i) => ({
      ...domains.items[0], domain: `site-${i}.example.com`,
      hosts: [{ ...domains.items[0].hosts[0], host: `${i % 2 ? 'odd' : 'even'}.site-${i}.example.com` }]
    }))
    const previousAdapter = api.defaults.adapter!
    api.defaults.adapter = async config => {
      if (config.url === '/domain-discovery/domains') return response(config, { ...domains, items })
      return previousAdapter(config)
    }
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(61)
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      const wrapper = await render()
      expect(wrapper.findAll('.domain-toggle').length).toBeLessThan(30)
      await wrapper.get('.domain-toggle').trigger('click')
      const scroll = async (top: number) => {
        vi.stubGlobal('scrollY', top)
        window.dispatchEvent(new Event('scroll'))
        await flushPromises()
      }
      await scroll(835 * 61)
      expect(wrapper.find('.domain-hosts-row').exists()).toBe(false)
      expect(wrapper.find('[aria-label="选择 site-835.example.com"]').exists()).toBe(true)
      await scroll(0)
      expect(wrapper.get('.domain-hosts-row').text()).toContain('even.site-0.example.com')

      const search = wrapper.get('input[placeholder="搜索域名…"]')
      await search.setValue('even.site')
      expect(wrapper.text()).toContain('共 836 个域名')
      await vi.advanceTimersByTimeAsync(150)
      expect(wrapper.text()).toContain('共 418 个域名')
      await search.setValue('odd.site')
      await wrapper.get('[aria-label="全选"]').trigger('click')
      expect(wrapper.text()).toContain('已选 418')
      await wrapper.findAll('button').find(b => b.text() === '加入代理')!.trigger('click')
      await flushPromises()
      const sent = requests.find(config => config.url === '/domain-discovery/apply')!
      expect(JSON.parse(sent.data).items).toEqual(items.filter((_, i) => i % 2).map(({ domain }) =>
        ({ rule_type: 'DOMAIN-SUFFIX', value: domain, target: 'proxy' })))
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps damaged store alert expanded on phones with a 44px recovery action', async () => {
    const damagedPayload = {
      ...agents,
      agents: [{
        ...agents.agents[0],
        status: 'no_data',
        last_report: null,
        mihomo_version: '',
        store_state: 'damaged',
        store_message: '域名数据文件已损坏，上报已暂停。清除后可恢复采集（已采集数据将丢失）。'
      }]
    }
    const previousAdapter = api.defaults.adapter!
    api.defaults.adapter = async config => {
      if (config.url === '/domain-discovery/agents') return response(config, damagedPayload)
      if (config.url === '/agents/collector/domain-discovery/clear') return response(config, { success: true })
      return previousAdapter(config)
    }
    const wrapper = await render()
    expect(wrapper.find('button[aria-controls="domain-discovery-configuration"]').exists()).toBe(false)
    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toContain('域名数据文件已损坏，上报已暂停。')
    const recovery = alert.findAll('button').find(button => button.text().trim() === '清除并恢复')
    expect(recovery).toBeTruthy()
    expect(recovery!.classes()).toContain('max-sm:min-h-11')
  })
})
