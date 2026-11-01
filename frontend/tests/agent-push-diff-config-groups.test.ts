import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AgentPushDiff from '@/components/agents/AgentPushDiff.vue'
import api from '@/api'
import type { PushDiffItem, PushDiffResponse } from '@/api/agentPushDiff'
import { breakSegments, configGroups, countsText, type ConfigGroup } from '@/lib/agentPushDiff'
import type { Agent } from '@/types'

vi.mock('@/api', () => ({ default: { get: vi.fn() } }))

const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
})
beforeEach(() => vi.mocked(api.get).mockReset())

const item = (path: string[], overrides: Partial<PushDiffItem> = {}): PushDiffItem => ({
  key: path.join('.'), path, change: 'added', sensitive: false, old: null, new: '198.51.100.54', ...overrides })
const POLICY = 'proxy-server-nameserver-policy'
const DOMAINS = ['node-1.example.net', 'node-2.example.net', 'node-3.example.net']
const HOSTS = ['printer.lab.invalid', 'nas.lab.invalid', 'camera-01.lab.invalid']
// The reported case: fake-ip-filter +1, three new proxy-server-nameserver-policy domains, three new hosts,
// plus top-level settings, a subscription's URL, a block with a single change and a key without a Chinese name.
const ITEMS: PushDiffItem[] = [
  item(['log-level'], { change: 'modified', old: 'info', new: 'debug' }),
  item(['ipv6'], { new: 'true' }),
  item(['dns', 'fake-ip-filter'], { change: 'modified', new: undefined, list: {
    before: 2, after: 3, added: 1, removed: 0, modified: 0, reordered: false, omitted: 0,
    entries: [{ change: 'added', text: '+.msftconnecttest.com' }] } }),
  ...DOMAINS.map(domain => item(['dns', POLICY, domain])),
  item(['dns', 'fallback-filter', 'geoip-code'], { change: 'modified', old: 'CN', new: 'US' }),
  item(['secret'], { sensitive: true, new: [{ secret: 'controller-secret-77' }] }),
  ...HOSTS.map((host, index) => item(['hosts', host], { new: `198.51.100.${31 + index}` })),
  item(['proxy-providers', '实验机场', 'url'], { change: 'modified', old: 'https://a.lab/sub', new: 'https://b.lab/sub' }),
  item(['rules'], { change: 'modified', new: undefined, list: { before: 2, after: 3, added: 1, removed: 0, modified: 0, reordered: false } }),
  item(['experimental', 'quic-go-disable-gso'], { new: 'true' }),
]

/** Each group as plain lines: its title, then block titles (▸) and rows (label). */
const outline = (group: ConfigGroup) => [`${group.name}${group.raw ? ` · ${group.raw}` : ''} ${countsText(group.counts)}`,
  ...group.lines.map(line => line.kind === 'block' ? `▸ ${line.name} · ${countsText(line.counts)}` : `${line.row.nested ? '  ' : ''}${line.row.label}`)]

describe('table 2 groups', () => {
  it('groups by top-level key, writes paths inside the group and titles a block with two or more changes', () => {
    const groups = configGroups(ITEMS)
    expect(groups.map(outline)).toEqual([
      ['基础设置 新增 2 项 · 修改 1 项', 'log-level', 'ipv6', 'secret'],
      ['DNS · dns 新增 3 项 · 修改 2 项', 'fake-ip-filter', `▸ ${POLICY} · 新增 3 项`, ...DOMAINS.map(domain => `  ${domain}`),
        'fallback-filter.geoip-code'],
      ['Hosts · hosts 新增 3 项', ...HOSTS],
      ['订阅 · proxy-providers 修改 1 项', '实验机场.url'],
      ['分流规则 · rules 修改 1 项', 'rules'],
      ['experimental 新增 1 项', 'quic-go-disable-gso'],
    ])
    expect(groups.every(group => !group.folded)).toBe(true)
    // Rows keep their place in the response, which the details toggles key on.
    expect(groups[1]!.lines.flatMap(line => line.kind === 'row' ? [line.row.index] : [])).toEqual([2, 3, 4, 5, 6])
  })

  it('folds a group of more than 20 rows', () => {
    const many = Array.from({ length: 21 }, (_, index) => item(['hosts', `host-${index}.lab.invalid`]))
    const [hosts, basic] = configGroups([...many, item(['mode'], { change: 'modified', old: 'rule', new: 'global' })])
    expect([hosts!.size, hosts!.folded, countsText(hosts!.counts)]).toEqual([21, true, '新增 21 项'])
    expect(basic!.folded).toBe(false)
    expect(configGroups(many.slice(0, 20))[0]!.folded).toBe(false)
  })

  it('splits keys from an older server without a path on dots', () => {
    expect(configGroups([{ key: 'hosts.printer.lan', change: 'added', sensitive: false, new: '1' }]).map(outline))
      .toEqual([['Hosts · hosts 新增 1 项', 'printer.lan']])
  })

  it('breaks long names only after a dot, a hyphen or a slash', () => {
    expect(breakSegments('node-1.backbone-edges.example.net')).toEqual(['node-', '1.', 'backbone-', 'edges.', 'example.', 'net'])
    expect(breakSegments('https://b.lab/sub')).toEqual(['https:/', '/', 'b.', 'lab/', 'sub'])
    expect(breakSegments('plain')).toEqual(['plain'])
    expect(breakSegments('')).toEqual([])
  })
})

describe('table 2 in the dialog', () => {
  const agent = { id: 'lab-mihomo', name: '实验 Mihomo', host: '192.0.2.2', port: 27647, token: '', service_type: 'mihomo',
    status: 'online', last_heartbeat: '', version: '1.8.50-go', config_version: '0a1b2c3d', enabled: true,
    content_sync: { status: 'changed' } } as Agent
  const response = (items: PushDiffItem[]): PushDiffResponse => ({
    status: 'changed', checked_at: new Date().toISOString(), service_type: 'mihomo', content: 'full', content_reason: null,
    counts: { added: 0, removed: 0, modified: 1, unchanged: 0 },
    files: [{ path: 'config.yaml', role: 'config', status: 'modified', summary: { kind: 'config', changes: items.length } }],
    unchanged: [], config: { format: 'yaml', status: 'compared', omitted: 0, items },
  })

  async function open(items: PushDiffItem[]) {
    vi.mocked(api.get).mockResolvedValue({ data: response(items) } as any)
    const wrapper = mount(AgentPushDiff, { attachTo: document.body, props: { agent, canPush: true, pushing: false, offline: false } })
    wrappers.push(wrapper)
    await flushPromises()
    return document.body.querySelector('[data-push-diff-config]')!
  }
  const visible = (element: Element) => {
    const copy = element.cloneNode(true) as Element
    copy.querySelectorAll('[aria-hidden="true"]').forEach(node => node.remove())
    return copy.textContent!.replace(/\s+/g, ' ').trim()
  }
  const toggles = (table: Element) => [...table.querySelectorAll<HTMLButtonElement>('[data-group-toggle]')]

  it('shows a title row per group, block titles, relative paths and the phone line of each record', async () => {
    const table = await open(ITEMS)
    expect([...table.querySelectorAll('thead th')].map(cell => cell.textContent)).toEqual(['配置项', '变化', '原值', '新值'])
    expect(table.querySelectorAll(':scope > tbody')).toHaveLength(6)
    expect(toggles(table).map(visible)).toEqual(['基础设置 新增 2 项 · 修改 1 项', 'DNS dns 新增 3 项 · 修改 2 项', 'Hosts hosts 新增 3 项',
      '订阅 proxy-providers 修改 1 项', '分流规则 rules 修改 1 项', 'experimental 新增 1 项'])
    const title = table.querySelector('[data-config-group="dns"] .push-diff-group th')!
    expect([title.getAttribute('scope'), title.getAttribute('colspan')]).toEqual(['rowgroup', '4'])
    expect(toggles(table).every(button => button.tagName === 'BUTTON' && button.getAttribute('aria-expanded') === 'true')).toBe(true)
    const dns = table.querySelector('[data-config-group="dns"]')!
    expect(visible(dns.querySelector('[data-config-block]')!)).toBe(`${POLICY} · 新增 3 项`)
    const rows = [...dns.querySelectorAll('tr[data-key]')]
    expect(rows.map(row => visible(row.querySelector('[data-config-label]')!)))
      .toEqual(['fake-ip-filter', ...DOMAINS, 'fallback-filter.geoip-code'])
    expect(rows.map(row => row.className.includes('is-nested'))).toEqual([false, true, true, true, false])
    // Long names break only after . - / : no break-all, a <wbr> at each place.
    const label = rows[1]!.querySelector('[data-config-label]')!
    expect(label.className).not.toContain('break-all')
    expect(label.innerHTML).toContain('node-<wbr>1.<wbr>example.<wbr>net')
    // The phone reads a record as 配置项：原值 → 新值; an added one has no empty 原值 or arrow.
    const line = (row: Element) => [...row.querySelectorAll('td')].filter(cell => !cell.classList.contains('push-diff-empty')
      && !cell.classList.contains('push-diff-change')).map(cell => cell.textContent).join('').replace(/\s+/g, ' ').trim()
    expect(line(rows[1]!)).toBe(`${DOMAINS[0]}：198.51.100.54`)
    expect(line(rows[4]!)).toBe('fallback-filter.geoip-code：CN → US')
    expect(line(rows[0]!)).toBe('fake-ip-filter：2 条 → 3 条 +1 / −0 展开明细')
    expect(rows[1]!.querySelector('[data-config-old]')!.className).toContain('push-diff-empty')
    // Lists keep their count and 展开明细.
    rows[0]!.querySelector<HTMLButtonElement>('button')!.click()
    await flushPromises()
    expect(rows[0]!.nextElementSibling!.textContent).toContain('+ +.msftconnecttest.com')
    const camera = table.querySelector('[data-config-group="hosts"] tr[data-key="hosts.camera-01.lab.invalid"]') as HTMLTableRowElement
    expect([...camera.cells].map(visible)).toEqual(['camera-01.lab.invalid', '新增', '—', '198.51.100.33'])
  })

  it('folds a group of more than 20 rows, opens and closes it by its title', async () => {
    const many = Array.from({ length: 22 }, (_, index) => item(['hosts', `host-${index}.lab.invalid`]))
    const table = await open([item(['mode'], { change: 'modified', old: 'rule', new: 'global' }), ...many])
    const [basic, hosts] = toggles(table)
    expect([basic!.getAttribute('aria-expanded'), hosts!.getAttribute('aria-expanded')]).toEqual(['true', 'false'])
    expect(visible(hosts!)).toBe('Hosts hosts 新增 22 项')
    const group = table.querySelector('[data-config-group="hosts"]')!
    expect(group.querySelectorAll('tr[data-key]')).toHaveLength(0)
    hosts!.click()
    await flushPromises()
    expect(hosts!.getAttribute('aria-expanded')).toBe('true')
    expect(group.querySelectorAll('tr[data-key]')).toHaveLength(22)
    basic!.click()
    await flushPromises()
    expect(table.querySelector('[data-config-group="basic"] tr[data-key]')).toBeNull()
  })

  it('hides credentials in grouped rows as well', async () => {
    const table = await open(ITEMS)
    expect(document.body.innerHTML).toContain('controller-secret-77')
    document.body.querySelector<HTMLButtonElement>('[data-push-diff-hide] button[role="switch"]')!.click()
    await flushPromises()
    expect(document.body.innerHTML).not.toContain('controller-secret-77')
    expect([...(table.querySelector('[data-key="secret"]') as HTMLTableRowElement).cells].map(visible)).toEqual(['secret', '新增', '—', '已隐藏'])
  })
})
