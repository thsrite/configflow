import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import MihomoConnections from '@/components/mihomo/MihomoConnections.vue'
import { clientToolsApi, mihomoPanelApi } from '@/api'

vi.mock('@/api', () => ({ clientToolsApi: { notes: vi.fn(), ruleTargets: vi.fn() }, mihomoPanelApi: { connections: vi.fn(), closeConnection: vi.fn(), closeAllConnections: vi.fn(), proxies: vi.fn(), proxyProviders: vi.fn(), selectProxy: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({ confirmDanger: vi.fn(), notify: { success: vi.fn(), warning: vi.fn(), error: vi.fn() } }))

// 手机宽度：只有 max-width: 639px 命中
const phone = (query: string) => ({
  matches: query.includes('max-width: 639px'), media: query, onchange: null,
  addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false
})

const start = () => new Date(Date.now() - 125_000).toISOString()
const connection = (id: string, host: string, sourceIP: string, chains: string[], extra: Record<string, unknown> = {}) => ({
  id, upload: 100, download: 200, start: start(), chains, rule: 'RuleSet', rulePayload: 'YouTube',
  metadata: { network: 'tcp', type: 'Redir', sourceIP, sourcePort: '5000', destinationIP: '1.1.1.1', destinationPort: '443', host, ...extra }
})
const snapshot = (connections: unknown[]) => ({ data: { success: true, data: { uploadTotal: 0, downloadTotal: 0, connections } } } as any)

const wrappers: VueWrapper[] = []
async function render() {
  const wrapper = mount(MihomoConnections, { props: { agentId: 'gw', active: true }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn(phone))
  vi.mocked(clientToolsApi.notes).mockResolvedValue({ data: { success: true, notes: { '10.0.0.3': '客厅 Apple TV' } } } as any)
  vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([
    connection('a', 'www.youtube.com', '10.0.0.3', ['香港 01', 'YouTube']),
    connection('b', 'www.baidu.com', '10.0.0.9', ['DIRECT', '全球直连'], { host: 'www.baidu.com' }),
    connection('c', 'ads.example.net', '10.0.0.9', ['REJECT'])
  ]))
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = ''; vi.unstubAllGlobals() })

describe('mihomo connections on a phone', () => {
  it('shows each connection as two compact lines instead of the table', async () => {
    const wrapper = await render()
    expect(wrapper.find('table.connection-table').exists()).toBe(false)
    const rows = wrapper.findAll('[data-mobile-connections] [data-connection-id]')
    expect(rows).toHaveLength(3)
    const first = wrapper.get('[data-connection-id="a"]')
    expect(first.text()).toContain('www.youtube.com:443')
    expect(first.text()).toContain('客厅 Apple TV')
    expect(first.text()).toContain('YouTube · 香港 01')
    expect(first.text()).toContain('2m')
    // 第一次采样没有速率，显示空闲
    expect(first.text()).toContain('空闲')
    expect(first.get('.mobile-policy').classes()).toContain('policy-proxy')
    expect(wrapper.get('[data-connection-id="b"] .mobile-policy').classes()).toContain('policy-direct')
    expect(wrapper.get('[data-connection-id="b"]').text()).toContain('全球直连 · DIRECT')
    expect(wrapper.get('[data-connection-id="c"] .mobile-policy').classes()).toContain('policy-reject')
    expect(wrapper.get('[data-connection-id="c"]').text()).toContain('REJECT')
    // 没有备注的设备显示 IP
    expect(wrapper.get('[data-connection-id="b"]').text()).toContain('10.0.0.9')
  })

  it('keeps the toolbar to search, one filter button and the status row', async () => {
    const wrapper = await render()
    const toolbar = wrapper.get('[data-mobile-toolbar]')
    expect(toolbar.find('input[aria-label="搜索连接"]').exists()).toBe(true)
    expect(toolbar.text()).toContain('活跃 (3)')
    expect(toolbar.find('[aria-label="全部断开"]').exists()).toBe(true)
    expect(wrapper.find('[aria-label="设置显示列"]').exists()).toBe(false)
    expect(wrapper.get('[data-mobile-summary]').text()).toContain('最新活动')
    await toolbar.get('input[aria-label="搜索连接"]').setValue('baidu')
    await flushPromises()
    expect(wrapper.findAll('[data-mobile-connections] [data-connection-id]')).toHaveLength(1)
    expect(wrapper.get('[data-mobile-summary]').text()).toContain('匹配 1 条')
  })

  it('opens sorting, grouping and filters in one bottom sheet', async () => {
    const wrapper = await render()
    await wrapper.get('[data-mobile-filter]').trigger('click')
    await flushPromises()
    const sheet = document.querySelector('[role="dialog"][data-state="open"]')!
    expect(sheet.textContent).toContain('排序与筛选')
    for (const label of ['排序', '分组', '来源', '协议', '规则', '链路', '隐藏匹配的连接']) expect(sheet.textContent).toContain(label)
  })

  it('offers rule, note and disconnect actions in the detail dialog', async () => {
    const wrapper = await render()
    await wrapper.get('[data-connection-id="a"] button').trigger('click')
    await flushPromises()
    const actions = document.querySelector('[data-connection-actions]')!
    expect([...actions.querySelectorAll('button')].map(button => button.textContent!.trim()))
      .toEqual(['加入代理规则集', '加入直连规则集', '编辑设备备注', '断开连接'])
    const disconnect = [...actions.querySelectorAll('button')].find(button => button.textContent!.includes('断开'))!
    disconnect.click()
    await flushPromises()
    expect(mihomoPanelApi.closeConnection).toHaveBeenCalledExactlyOnceWith('gw', 'a')
    expect(document.querySelector('[data-connection-actions]')).toBeNull()
  })
})
