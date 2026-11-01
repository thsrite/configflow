import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import MihomoConnections from '@/components/mihomo/MihomoConnections.vue'
import { clientToolsApi, mihomoPanelApi } from '@/api'
import { compareSources, connectionMatcher, INNER_SOURCE } from '@/types/mihomo'
import { confirmDanger } from '@/lib/feedback'
import { CONNECTION_COLUMNS, CONNECTION_SETTINGS_KEY, DEFAULT_CONNECTION_COLUMNS } from '@/lib/mihomo-connections'

vi.mock('@/api', () => ({ clientToolsApi: { notes: vi.fn(), ruleTargets: vi.fn() }, mihomoPanelApi: { connections: vi.fn(), closeConnection: vi.fn(), closeAllConnections: vi.fn(), proxies: vi.fn(), proxyProviders: vi.fn(), selectProxy: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({ confirmDanger: vi.fn(), notify: { success: vi.fn(), warning: vi.fn(), error: vi.fn() } }))
vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })

let connectionStart: string
const connection = (id: string, host: string, sourceIP: string, extra: Record<string, unknown> = {}) => ({
  id, upload: 100, download: 200, start: connectionStart, chains: ['HK', 'Proxy'],
  rule: 'RuleSet', rulePayload: 'telegram',
  metadata: { network: 'tcp', type: 'Redir', sourceIP, sourcePort: '5000', destinationIP: '1.1.1.1', destinationPort: '443', host, ...extra }
})
const snapshot = (connections: unknown[], memory = 125 * 1024 * 1024) =>
  ({ data: { success: true, data: { uploadTotal: 1024, downloadTotal: 2048, memory, connections } } } as any)

const wrappers: VueWrapper[] = []
async function render() {
  const wrapper = mount(MihomoConnections, { props: { agentId: 'gw', active: true }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, state: (wrapper.vm as any).$.setupState }
}
beforeEach(() => { connectionStart = new Date(Date.now() - 5000).toISOString(); vi.clearAllMocks(); localStorage.clear(); vi.mocked(clientToolsApi.notes).mockResolvedValue({ data: { success: true, notes: {} } } as any) })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })

describe('connection helpers', () => {
  it('searches by regex and falls back to plain text for invalid patterns', () => {
    expect(connectionMatcher('^www\\.example')('www.example.com:443')).toBe(true)
    expect(connectionMatcher('^example')('www.example.com:443')).toBe(false)
    expect(connectionMatcher('a[b')('xa[by')).toBe(true)
    expect(connectionMatcher('')(undefined)).toBe(true)
  })

  it('orders sources with Inner first and IPv4 numerically', () => {
    expect(['10.0.0.50', '::1', '10.0.0.3', INNER_SOURCE, '10.0.0.161'].sort(compareSources))
      .toEqual([INNER_SOURCE, '10.0.0.3', '10.0.0.50', '10.0.0.161', '::1'])
  })
})

describe('mihomo connections', () => {
  it('opens rule creation for an IP-only connection using its destination', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([connection('ip', '', '10.0.0.3', { destinationIP: '192.0.2.8' })]))
    vi.mocked(clientToolsApi.ruleTargets).mockResolvedValue({ data: { success: true, profile_id: 'office', profile_name: 'Office', proxy: null, direct: null, ip_rulesets: [] } } as any)
    const { state } = await render()
    state.openRule(state.rows[0], 'proxy'); await flushPromises()
    expect(state.ruleAction).toEqual({ values: ['192.0.2.8'], target: 'proxy' })
    expect(document.body.textContent).toContain('IP-CIDR,192.0.2.8/32,no-resolve')
  })
  it('records connections that disappear as closed', async () => {
    vi.mocked(mihomoPanelApi.connections)
      .mockResolvedValueOnce(snapshot([connection('a', 'www.example.com', '10.0.0.3'), connection('b', '223.5.5.5', '')]))
      .mockResolvedValueOnce(snapshot([connection('a', 'www.example.com', '10.0.0.3')]))
    const { wrapper, state } = await render()
    // 连接数、累计流量与内存已移到概览
    expect(wrapper.text()).not.toContain('125.0 MB')
    expect(wrapper.text()).toContain('活跃 (2)')
    await state.load()
    await flushPromises()
    expect(wrapper.text()).toContain('活跃 (1)')
    expect(wrapper.text()).toContain('已关闭 (1)')
    state.view = 'closed'
    await flushPromises()
    expect(wrapper.text()).toContain('223.5.5.5:443')
    expect(wrapper.text()).toContain(INNER_SOURCE)
    expect(wrapper.text()).not.toContain('www.example.com:443')
  })

  it('filters by source and regex, and limits bulk close to the matches', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([
      connection('a', 'www.example.com', '10.0.0.3'), connection('b', 'api.example.com', '10.0.0.50'), connection('c', 'cdn.test', '10.0.0.3')
    ]))
    const { wrapper, state } = await render()
    expect(state.sources).toEqual(['10.0.0.3', '10.0.0.50'])
    state.filters.sources = ['10.0.0.3']
    state.search = 'example\\.com$'
    await flushPromises()
    expect(state.rows.map((row: { id: string }) => row.id)).toEqual(['a'])
    expect(wrapper.text()).toContain('断开筛选结果')
    vi.mocked(confirmDanger).mockResolvedValue(true)
    await state.closeAll()
    expect(mihomoPanelApi.closeConnection).toHaveBeenCalledExactlyOnceWith('gw', 'a')
    expect(mihomoPanelApi.closeAllConnections).not.toHaveBeenCalled()
  })

  it('sorts numeric headers in both directions and remembers only table preferences', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([
      { ...connection('small', 'a', '10.0.0.3'), download: 900, upload: 50_000 },
      { ...connection('large', 'z', '10.0.0.50'), download: 12_000, upload: 100 }
    ]))
    const { wrapper, state } = await render()
    const ids = () => wrapper.findAll('[data-connection-id]').map(row => row.attributes('data-connection-id'))
    state.toggleColumn('download', true)
    state.toggleColumn('upload', true)
    await flushPromises()
    async function sortBy(label: string) {
      await wrapper.get(`button[aria-label="按${label}排序"]`).trigger('click')
      expect(document.body.querySelector('[role="menu"]')).toBeNull()
    }
    await sortBy('下载量')
    expect(ids()).toEqual(['large', 'small'])
    expect(wrapper.get('button[aria-label="按下载量排序"]').attributes('data-active')).toBe('true')
    await sortBy('下载量')
    expect(ids()).toEqual(['small', 'large'])
    await sortBy('上传量')
    expect(ids()).toEqual(['small', 'large'])
    state.toggleColumn('upload', false)
    state.filters.sources = ['10.0.0.3']
    state.search = 'a'
    await flushPromises()
    const next = await render()
    expect(next.state.settings.sort).toBe('upload')
    expect(next.state.visibleColumns.map((column: any) => column.key)).not.toContain('upload')
    expect(next.state.filtering).toBe(false)
    expect(next.state.rows).toHaveLength(2)
  })

  it.each([
    ['upload', '上传量'], ['start', '开始时间'], ['process', '进程']
  ])('shows %s immediately when selected in a narrow desktop column menu', async (key, label) => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([connection('a', 'example.com', '10.0.0.3', { process: 'browser' })]))
    localStorage.setItem(CONNECTION_SETTINGS_KEY, JSON.stringify({ layoutVersion: 3, columns: CONNECTION_COLUMNS.map(column => column.key) }))
    const { wrapper, state } = await render()
    state.panelWidth = 1200
    await flushPromises()
    expect(wrapper.find(`button[aria-label="按${label}排序"]`).exists()).toBe(false)
    await wrapper.get('button[aria-label="设置显示列"]').trigger('click')
    await flushPromises()
    const checkbox = document.querySelector<HTMLElement>(`[role="checkbox"][aria-label="${label}"]`)
      ?? Array.from(document.querySelectorAll('label')).find(item => item.textContent?.trim() === label)?.querySelector<HTMLElement>('[role="checkbox"]')
    expect(checkbox).not.toBeNull()
    checkbox!.click()
    await flushPromises()
    expect(wrapper.find(`button[aria-label="按${label}排序"]`).exists()).toBe(true)
    expect(wrapper.find(`td[data-column="${key}"]`).exists()).toBe(true)
    expect(checkbox!.getAttribute('aria-checked')).toBe('true')
    const next = await render()
    next.state.panelWidth = 1200
    await flushPromises()
    expect(next.wrapper.find(`td[data-column="${key}"]`).exists()).toBe(true)
  })

  it('matches column checkboxes to actual headers and retains preferences through resize', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([connection('a', 'example.com', '10.0.0.3')]))
    localStorage.setItem(CONNECTION_SETTINGS_KEY, JSON.stringify({ layoutVersion: 3, columns: CONNECTION_COLUMNS.map(column => column.key) }))
    const { wrapper, state } = await render()
    state.panelWidth = 1200
    await flushPromises()
    await wrapper.get('button[aria-label="设置显示列"]').trigger('click')
    await flushPromises()
    const columnCheckbox = (label: string) => {
      const checkbox = document.querySelector<HTMLElement>(`[role="checkbox"][aria-label="${label}"]`)
        ?? Array.from(document.querySelectorAll('label')).find(item => item.textContent?.trim() === label)?.querySelector<HTMLElement>('[role="checkbox"]')
      expect(checkbox).not.toBeNull()
      return checkbox!
    }
    const expectMenuMatchesHeaders = () => {
      for (const { label } of CONNECTION_COLUMNS) {
        expect(columnCheckbox(label).getAttribute('aria-checked')).toBe(String(wrapper.find(`button[aria-label="按${label}排序"]`).exists()))
      }
    }
    expectMenuMatchesHeaders()
    expect(columnCheckbox('进程').closest('label')?.textContent).toContain('收起')
    for (const label of ['上传量', '开始时间', '进程']) {
      const checkbox = columnCheckbox(label)
      if (checkbox.getAttribute('aria-checked') === 'false') checkbox.click()
      await flushPromises()
      expect(wrapper.find(`button[aria-label="按${label}排序"]`).exists()).toBe(true)
      expectMenuMatchesHeaders()
    }
    // Resizing may fold other fields, but it must never clear the selected preferences.
    state.panelWidth = 741
    await flushPromises()
    expect(wrapper.find('td[data-column="process"]').exists()).toBe(true)
    expectMenuMatchesHeaders()
    state.panelWidth = 375
    await flushPromises()
    expect(wrapper.findAll('td[data-column]')).toHaveLength(CONNECTION_COLUMNS.length)
    expectMenuMatchesHeaders()
    columnCheckbox('开始时间').click()
    await flushPromises()
    expect(JSON.parse(localStorage.getItem(CONNECTION_SETTINGS_KEY)!).preferredColumns ?? []).not.toContain('start')
    state.panelWidth = 2000
    await flushPromises()
    expect(wrapper.find('td[data-column="start"]').exists()).toBe(false)
    expect(wrapper.findAll('td[data-column]')).toHaveLength(CONNECTION_COLUMNS.length - 1)
    expectMenuMatchesHeaders()
    const next = await render()
    next.state.panelWidth = 2000
    await flushPromises()
    expect(next.wrapper.find('td[data-column="start"]').exists()).toBe(false)
  })

  it('restores the default columns without resetting grouping or sort preferences', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([connection('a', 'example.com', '10.0.0.3')]))
    localStorage.setItem(CONNECTION_SETTINGS_KEY, JSON.stringify({
      layoutVersion: 3, columns: CONNECTION_COLUMNS.map(column => column.key), preferredColumns: ['upload', 'start', 'process'],
      sort: 'upload', direction: 'asc', groupBy: 'source'
    }))
    const { wrapper, state } = await render()
    state.panelWidth = 2000
    await flushPromises()
    await wrapper.get('button[aria-label="设置显示列"]').trigger('click')
    await flushPromises()
    const reset = Array.from(document.querySelectorAll('button')).find(button => button.textContent?.trim() === '恢复默认')
    expect(reset).toBeDefined()
    reset!.click()
    await flushPromises()
    expect(wrapper.findAll('thead button[aria-label$="排序"]').map(button => button.attributes('aria-label')))
      .toEqual(CONNECTION_COLUMNS.filter(column => DEFAULT_CONNECTION_COLUMNS.includes(column.key)).map(column => `按${column.label}排序`))
    expect(state.settings).toMatchObject({ sort: 'upload', direction: 'asc', groupBy: 'source' })
    expect(JSON.parse(localStorage.getItem(CONNECTION_SETTINGS_KEY)!)).not.toHaveProperty('preferredColumns')
    expect(wrapper.findAll('[data-group-id]')).toHaveLength(1)
  })

  it('keeps selected filters when a source disappears, and allows clearing an empty result', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([connection('a', 'a', '10.0.0.3')]))
    const { wrapper, state } = await render()
    state.filters.sources = ['10.0.0.99']
    await flushPromises()
    expect(wrapper.text()).toContain('没有匹配的连接')
    expect(state.filterGroups[0].options.map((option: { value: string }) => option.value)).toContain('10.0.0.99')
    await state.closeAll()
    expect(confirmDanger).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeAllConnections).not.toHaveBeenCalled()
    state.clearFilters()
    await flushPromises()
    expect(state.rows).toHaveLength(1)
  })

  it('sorts hosts and each routing field directly without opening a menu', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([
      { ...connection('a', 'z.example.com', '10.0.0.3'), rulePayload: 'alpha', chains: ['Zulu'] },
      { ...connection('b', 'a.example.com', '10.0.0.50'), rulePayload: 'beta', chains: ['Alpha'] }
    ]))
    const { wrapper } = await render()
    const ids = () => wrapper.findAll('[data-connection-id]').map(row => row.attributes('data-connection-id'))
    await wrapper.get('button[aria-label="按主机排序"]').trigger('click')
    expect(ids()).toEqual(['b', 'a'])
    await wrapper.get('button[aria-label="按主机排序"]').trigger('click')
    expect(ids()).toEqual(['a', 'b'])
    await wrapper.get('button[aria-label="按规则排序"]').trigger('click')
    expect(ids()).toEqual(['a', 'b'])
    await wrapper.get('button[aria-label="按链路排序"]').trigger('click')
    expect(ids()).toEqual(['b', 'a'])
    expect(document.body.querySelector('[role="menu"]')).toBeNull()
  })

  it('groups from headers without sorting and keeps individual actions after expansion', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([
      connection('a', 'same.example', '10.0.0.3'), connection('b', 'same.example', '10.0.0.50'), connection('c', 'other.example', '10.0.0.3')
    ]))
    const { wrapper, state } = await render()
    const before = state.settings.sort
    await wrapper.get('button[aria-label="按主机分组"]').trigger('click')
    expect(state.settings.sort).toBe(before)
    expect(wrapper.findAll('[data-group-id]')).toHaveLength(2)
    expect(wrapper.findAll('[data-connection-id]')).toHaveLength(0)
    await wrapper.get('button[aria-label="展开 same.example:443（2 条连接）"]').trigger('click')
    expect(wrapper.findAll('[data-connection-id]')).toHaveLength(2)
    expect(wrapper.findAll('button[aria-label="断开 same.example:443"]')).toHaveLength(2)
    await wrapper.get('[data-connection-id="a"] button[aria-label="断开 same.example:443"]').trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.closeConnection).toHaveBeenCalledExactlyOnceWith('gw', 'a')
    expect(state.groups.find((g: any) => g.label === 'same.example:443').members).toHaveLength(1)
    await wrapper.get('button[aria-label="取消按主机分组"]').trigger('click')
    expect(wrapper.findAll('[data-group-id]')).toHaveLength(0)
    expect(wrapper.findAll('[data-connection-id]')).toHaveLength(2)
  })

  it('aggregates before paging and renders large groups in batches without losing members', async () => {
    const connections = Array.from({ length: 105 }, (_, i) => connection(String(i), 'same.example', '10.0.0.3'))
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot(connections))
    const { wrapper, state } = await render()
    state.setGroup('target'); await flushPromises()
    expect(state.groups[0].download).toBe(21_000)
    expect(state.pageCount).toBe(1)
    const id = state.groups[0].id
    state.toggleExpanded(id); await flushPromises()
    expect(wrapper.findAll('[data-connection-id]')).toHaveLength(20)
    state.showMore(id); await flushPromises()
    expect(wrapper.findAll('[data-connection-id]')).toHaveLength(40)
    // Polling updates the group without collapsing it.
    await state.load(); await flushPromises()
    expect(wrapper.findAll('[data-connection-id]')).toHaveLength(40)
    state.search = 'missing'; await flushPromises()
    expect(state.groups).toHaveLength(0)
    expect(state.expanded.size).toBe(0)
    state.search = ''; await flushPromises()
    expect(state.groups).toHaveLength(1)
    await wrapper.setProps({ agentId: 'new-agent' }); await flushPromises()
    expect(state.expanded.size).toBe(0)
    expect(mihomoPanelApi.closeAllConnections).not.toHaveBeenCalled()
  })

  it('paginates all results and clamps the page when connections disappear', async () => {
    const connections = Array.from({ length: 305 }, (_, i) => connection(String(i).padStart(3, '0'), 'a', '10.0.0.3'))
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot(connections))
    const { wrapper, state } = await render()
    expect(wrapper.findAll('[data-connection-id]')).toHaveLength(100)
    state.page = 4
    await flushPromises()
    expect(wrapper.findAll('[data-connection-id]')).toHaveLength(5)
    expect(wrapper.text()).toContain('301–305')
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot(connections.slice(0, 2)))
    await state.load()
    await flushPromises()
    expect(state.page).toBe(1)
    expect(wrapper.findAll('[data-connection-id]')).toHaveLength(2)
  })

  it('freezes all matching active IDs across pages before confirmation and limits close concurrency', async () => {
    const matches = Array.from({ length: 105 }, (_, i) => connection(`match-${i}`, 'match.example', '10.0.0.3'))
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([...matches, connection('other', 'other', '10.0.0.50')]))
    const { state } = await render()
    state.closed = [state.toRow(connection('closed', 'match.example', '10.0.0.3'), Date.now())]
    state.view = 'all'
    state.filters.sources = ['10.0.0.3']
    await flushPromises()
    let approve!: (value: boolean) => void
    vi.mocked(confirmDanger).mockImplementationOnce(() => new Promise(resolve => { approve = resolve }))
    let concurrent = 0, peak = 0
    vi.mocked(mihomoPanelApi.closeConnection).mockImplementation(async () => {
      peak = Math.max(peak, ++concurrent)
      await Promise.resolve()
      concurrent--
      return {} as any
    })
    const pending = state.closeAll()
    expect(confirmDanger).toHaveBeenCalledWith(expect.stringContaining('105 条活动连接'), expect.anything())
    state.clearFilters()
    approve(true)
    await pending
    expect(mihomoPanelApi.closeConnection).toHaveBeenCalledTimes(105)
    expect(vi.mocked(mihomoPanelApi.closeConnection).mock.calls.map(call => call[1]).sort()).toEqual(matches.map(row => row.id).sort())
    expect(peak).toBeLessThanOrEqual(8)
    expect(mihomoPanelApi.closeAllConnections).not.toHaveBeenCalled()
  })

  it('does not close after cancellation or switching agents during confirmation', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([connection('a', 'a', '')]))
    const { state, wrapper } = await render()
    vi.mocked(confirmDanger).mockResolvedValueOnce(false)
    await state.closeAll()
    expect(state.closingAll).toBe(false)
    let approve!: (value: boolean) => void
    vi.mocked(confirmDanger).mockImplementationOnce(() => new Promise(resolve => { approve = resolve }))
    const pending = state.closeAll()
    await wrapper.setProps({ agentId: 'other' })
    approve(true)
    await pending
    expect(mihomoPanelApi.closeConnection).not.toHaveBeenCalled()
    expect(mihomoPanelApi.closeAllConnections).not.toHaveBeenCalled()
  })

  it('opens the detail dialog with overview, raw data and proxy chain', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([connection('a', 'www.example.com', '10.0.0.3', { remoteDestination: '1.1.1.1' })]))
    const { wrapper } = await render()
    expect(wrapper.findAll('thead th')).toHaveLength(11)
    expect(wrapper.get('td[data-column="rule"]').text()).toContain('RuleSet: telegram')
    expect(wrapper.get('td[data-column="chain"]').text()).toContain('Proxy → HK')
    expect(wrapper.get('td[data-column="download"]').text()).toBe('200 B')
    await wrapper.find('button[aria-label="查看 www.example.com:443 的连接详情"]').trigger('click')
    await flushPromises()
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog?.textContent).toContain('连接详情')
    expect(dialog?.textContent).toContain('Redir | tcp')
    expect(dialog?.textContent).toContain('RuleSet: telegram')
    expect(dialog?.textContent).toContain('Proxy → HK')
    expect(dialog?.textContent).toContain('IPv4')
    expect(dialog?.textContent).toContain('200 B')
    expect(dialog?.textContent).toContain('100 B')
  })

  it('loads the detail proxy selector only on its tab and switches on the connection agent', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([connection('a', 'www.example.com', '10.0.0.3')]))
    vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: {
      Proxy: { name: 'Proxy', type: 'Selector', all: ['HK', 'DIRECT'], now: 'HK' },
      HK: { name: 'HK', type: 'AnyTLS' }, DIRECT: { name: 'DIRECT', type: 'Direct' }
    } } } } as any)
    vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue({ data: { data: { providers: {} } } } as any)
    vi.mocked(mihomoPanelApi.selectProxy).mockResolvedValue({} as any)
    localStorage.setItem('configflow:mihomo-panel:proxy-settings', JSON.stringify({ closeConnectionsOnSwitch: false }))
    const { wrapper } = await render()
    await wrapper.get('button[aria-label="查看 www.example.com:443 的连接详情"]').trigger('click')
    await flushPromises()
    expect(mihomoPanelApi.proxies).not.toHaveBeenCalled()
    const proxyTab = Array.from(document.body.querySelectorAll<HTMLButtonElement>('[role="tab"]')).find(button => button.textContent === '代理')!
    proxyTab.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(mihomoPanelApi.proxies).toHaveBeenCalledExactlyOnceWith('gw')
    document.body.querySelector<HTMLButtonElement>('button[aria-label="选择 DIRECT"]')!.click()
    await flushPromises()
    expect(mihomoPanelApi.selectProxy).toHaveBeenCalledExactlyOnceWith('gw', 'Proxy', 'DIRECT')
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('此连接建立时：Proxy → HK')
  })
})

describe('mihomo connection summary', () => {
  it('stops reading while its tab is hidden and keeps the closed history for when it is shown again', async () => {
    vi.mocked(clientToolsApi.notes).mockImplementation(async () => ({ data: { success: true, notes: { '192.0.2.1': '书房电脑' } } }) as any)
    vi.mocked(mihomoPanelApi.connections).mockResolvedValueOnce(snapshot([connection('a', 'a.example', '192.0.2.1'), connection('b', 'b.example', '192.0.2.1')]))
    const { wrapper, state } = await render()
    vi.mocked(mihomoPanelApi.connections).mockResolvedValueOnce(snapshot([connection('a', 'a.example', '192.0.2.1')]))
    await state.load(); await flushPromises()
    expect(state.closed.map((row: any) => row.id)).toEqual(['b'])
    let signal: AbortSignal | undefined
    vi.mocked(mihomoPanelApi.connections).mockImplementationOnce((_id: string, options: any) => { signal = options.signal; return new Promise(() => {}) })
    void state.load()
    state.measuredWidth = 900; await flushPromises()
    const columns = state.visibleColumns.map((column: any) => column.key)
    await wrapper.setProps({ active: false }); await flushPromises()
    expect(signal?.aborted).toBe(true)
    // 隐藏后读到的宽度是 0，列布局保持不变
    state.measuredWidth = 0; await flushPromises()
    expect(state.panelWidth).toBe(900)
    expect(state.visibleColumns.map((column: any) => column.key)).toEqual(columns)
    const calls = vi.mocked(mihomoPanelApi.connections).mock.calls.length
    const notes = state.notes
    vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([connection('a', 'a.example', '192.0.2.1')]))
    await wrapper.setProps({ active: true }); await flushPromises()
    expect(mihomoPanelApi.connections).toHaveBeenCalledTimes(calls + 1)
    expect(state.closed.map((row: any) => row.id)).toEqual(['b'])
    // 切回时重读的备注没变，不替换（避免整表多渲染一次）
    expect(clientToolsApi.notes).toHaveBeenCalledTimes(2)
    expect(state.notes).toBe(notes)
  })

  it('lets a pending read finish when live refresh is paused', async () => {
    vi.mocked(mihomoPanelApi.connections).mockResolvedValueOnce(snapshot([]))
    const { state } = await render()
    let finish!: (value: unknown) => void
    let signal: AbortSignal | undefined
    vi.mocked(mihomoPanelApi.connections).mockImplementationOnce((_id: string, options: any) => { signal = options.signal; return new Promise(resolve => { finish = resolve }) as any })
    void state.load()
    state.live = false; await flushPromises()
    expect(signal?.aborted).toBe(false)
    finish(snapshot([connection('a', 'a.example', '192.0.2.1')])); await flushPromises()
    expect(state.snapshot.connections.map((row: any) => row.id)).toEqual(['a'])
  })
})

it('shows and searches shared device notes without polling the notes endpoint', async () => {
  vi.mocked(clientToolsApi.notes).mockResolvedValue({ data: { success: true, notes: { '10.0.0.3': '工作电脑' } } } as any)
  vi.mocked(mihomoPanelApi.connections).mockResolvedValue(snapshot([
    connection('a', 'www.example.com', '::ffff:10.0.0.3'), connection('b', 'www.example.com', '10.0.0.50')
  ]))
  const { wrapper, state } = await render()
  expect(wrapper.text()).toContain('工作电脑')
  state.search = '工作电脑'; await flushPromises()
  expect(state.rows.map((row: any) => row.id)).toEqual(['a'])
  vi.mocked(confirmDanger).mockResolvedValue(true); await state.closeAll()
  expect(mihomoPanelApi.closeConnection).toHaveBeenCalledExactlyOnceWith('gw', 'a')
  await state.load(); expect(clientToolsApi.notes).toHaveBeenCalledTimes(1)
})
