import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AgentPushDiff from '@/components/agents/AgentPushDiff.vue'
import api from '@/api'
import type { PushDiffEntriesQuery, PushDiffListEntry, PushDiffResponse } from '@/api/agentPushDiff'
import { groupPaging, textOf } from '@/lib/agentPushDiff'
import type { Agent } from '@/types'

vi.mock('@/api', () => ({ default: { get: vi.fn() } }))

const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.useRealTimers()
})

const agent = { id: 'lab-mihomo', name: '实验 Mihomo', host: '192.0.2.2', port: 27647, token: '', service_type: 'mihomo',
  status: 'online', last_heartbeat: '', version: '1.8.50-go', config_version: '0a1b2c3d', enabled: true,
  content_sync: { status: 'changed' } } as Agent

const RULES = 'ruleset/实验大规则集.list'
const FEED = 'providers/备用机场.yaml'
const pad = (index: number) => String(index).padStart(3, '0')
// The server's full lists: 130 rules added and 3 removed; 60 nodes modified, each with a password change.
const ADDED: PushDiffListEntry[] = Array.from({ length: 130 }, (_, index) => ({ change: 'added', text: `+.fresh-${pad(index)}.lab` }))
const REMOVED: PushDiffListEntry[] = ['+.a.lab', '+.b.lab', '+.c.lab'].map(text => ({ change: 'removed', text }))
const NODES: PushDiffListEntry[] = Array.from({ length: 60 }, (_, index) => ({
  change: 'modified', text: `D 备用 ${pad(index)}`, fields_omitted: 0, fields: [
    { key: 'port', change: 'modified', sensitive: false, old: '443', new: String(8000 + index) },
    { key: 'password', change: 'modified', sensitive: true, old: [{ secret: `pw-before-${pad(index)}` }], new: [{ secret: `pw-after-${pad(index)}` }] }] }))
const LISTS: Record<string, Record<string, PushDiffListEntry[]>> = { [RULES]: { added: ADDED, removed: REMOVED }, [FEED]: { modified: NODES } }

function response(version = 'v1'): PushDiffResponse {
  return {
    status: 'changed', checked_at: new Date().toISOString(), service_type: 'mihomo', content: 'full', content_reason: null, version,
    counts: { added: 0, removed: 0, modified: 2, unchanged: 0 },
    files: [
      { path: FEED, role: 'provider', name: '备用机场', status: 'modified', summary: { kind: 'nodes', before: 60, after: 60, added: 0, removed: 0, modified: 60 },
        details: { kind: 'nodes', paging: true, groups: [{ change: 'modified', total: 60, available: 60, omitted: 40, entries: NODES.slice(0, 20) }] } },
      { path: RULES, role: 'rule', name: '实验大规则集', status: 'modified', summary: { kind: 'rules', before: 1200, after: 1327, added: 130, removed: 3 },
        details: { kind: 'rules', paging: true, groups: [
          { change: 'added', total: 130, available: 130, omitted: 110, entries: ADDED.slice(0, 20) },
          { change: 'removed', total: 3, available: 3, omitted: 0, entries: REMOVED }] } },
    ],
    unchanged: [], unchanged_omitted: 0, config: null,
  }
}

/** A stand-in for /push-diff and /push-diff/entries: pages and searches the full lists like the server. */
function server(options: { versions?: string[]; stale?: () => boolean; fail?: () => boolean; hold?: () => Promise<void> } = {}) {
  const versions = [...(options.versions || ['v1'])]
  let current = versions[0]!
  const calls: PushDiffEntriesQuery[] = []
  vi.mocked(api.get).mockImplementation((async (url: string, config: any) => {
    if (url.endsWith('/push-diff')) {
      current = versions.shift() || current
      return { data: response(current) }
    }
    const params = config.params as PushDiffEntriesQuery
    calls.push(params)
    await options.hold?.()
    if (options.fail?.()) throw { response: { data: { message: '服务暂时不可用' } } }
    if (options.stale?.() || params.version !== current) return { data: { status: 'stale', reason: 'replaced' } }
    const all = LISTS[params.file]![params.change]!
    const query = params.q?.toLowerCase()
    const matched = query ? all.filter(entry => textOf(entry.text, false).toLowerCase().includes(query)) : all
    const entries = matched.slice(params.offset, params.offset + (params.limit || 50))
    const end = params.offset + entries.length
    return { data: { status: 'ok', total: matched.length, offset: params.offset, entries, next_offset: end < matched.length ? end : null } }
  }) as any)
  return calls
}

async function open() {
  const wrapper = mount(AgentPushDiff, { attachTo: document.body, props: { agent, canPush: true, pushing: false, offline: false } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

const body = () => document.body
const button = (root: Element, text: string) => [...root.querySelectorAll('button')].find(item => item.textContent!.includes(text))

async function expand(path: string) {
  const row = body().querySelector(`[data-file="${path}"]`)!
  button(row, '展开明细')!.click()
  await flushPromises()
  return row.nextElementSibling!.querySelector('[data-file-details]')!
}

const group = (detail: Element, change: string) => detail.querySelector(`[data-group="${change}"]`)!
// Rules are list items, nodes are table row groups (one tbody per node).
const items = (detail: Element, change: string) => [...group(detail, change).querySelectorAll(':scope > ul > li, :scope > table > tbody')]
const itemTexts = (detail: Element, change: string) => items(detail, change)
  .map(item => (item.querySelector('th') || item.querySelector('span'))!.textContent!.replace(/\s+/g, ' ').trim())
const flat = (element: Element) => element.textContent!.replace(/\s+/g, ' ').trim()

async function type(detail: Element, text: string) {
  const input = detail.querySelector<HTMLInputElement>('[data-detail-search]')!
  input.value = text
  input.dispatchEvent(new Event('input'))
  await flushPromises()
}

// A block body: a function returned from beforeEach would run as a cleanup hook.
beforeEach(() => { vi.mocked(api.get).mockReset() })

describe('push diff details: load more', () => {
  it('counts what is left, kept and cut', () => {
    const group = { change: 'added' as const, total: 130, available: 100, omitted: 110, entries: [] }
    expect(groupPaging(group, 20, true)).toEqual({ remaining: 80, notKept: 30 })
    expect(groupPaging(group, 100, true)).toEqual({ remaining: 0, notKept: 30 })
    expect(groupPaging(group, 20, false)).toEqual({ remaining: 0, notKept: 0 })
  })

  it('loads 50 more at a time, says how many are left and when everything is there', async () => {
    let release: () => void = () => {}
    let holding = false
    const calls = server({ hold: () => holding ? new Promise(resolve => { release = resolve }) : Promise.resolve() })
    await open()
    const detail = await expand(RULES)
    expect(items(detail, 'added')).toHaveLength(20)
    expect(flat(group(detail, 'added'))).toContain('加载更多（还剩 110 条）')
    expect(flat(group(detail, 'removed'))).not.toMatch(/加载更多|已全部加载/)  // a small group is complete as it is

    holding = true
    button(group(detail, 'added'), '加载更多')!.click()
    await flushPromises()
    const busy = group(detail, 'added').querySelector<HTMLButtonElement>('[data-load-more]')!
    expect(busy.disabled).toBe(true)
    expect(busy.textContent).toContain('正在加载…')
    busy.click()  // no second request while one runs
    release()
    await flushPromises()
    holding = false
    expect(calls).toEqual([{ version: 'v1', file: RULES, change: 'added', offset: 20, limit: 50 }])
    expect(items(detail, 'added')).toHaveLength(70)
    expect(itemTexts(detail, 'added')[20]).toBe('+ +.fresh-020.lab')
    expect(flat(group(detail, 'added'))).toContain('加载更多（还剩 60 条）')

    button(group(detail, 'added'), '加载更多')!.click()
    await flushPromises()
    button(group(detail, 'added'), '加载更多')!.click()
    await flushPromises()
    expect(calls.map(call => call.offset)).toEqual([20, 70, 120])
    expect(itemTexts(detail, 'added')).toEqual(ADDED.map(entry => `+ ${entry.text}`))
    expect(button(group(detail, 'added'), '加载更多')).toBeUndefined()
    expect(group(detail, 'added').querySelector('[data-all-loaded]')!.textContent).toBe('已全部加载')
    // The list scrolls inside the expanded area, which takes keyboard focus for scrolling.
    const scroller = detail.querySelector<HTMLElement>('[data-details-scroll]')!
    expect(scroller.className).toMatch(/max-h-\[420px\].*overflow-y-auto/)
    expect(scroller.className).toContain('max-sm:max-h-[60dvh]')
    expect(scroller.getAttribute('tabindex')).toBe('0')
    expect(scroller.getAttribute('aria-label')).toBe('规则明细')
  })

  it('reports a failed page and retries it', async () => {
    let failing = true
    const calls = server({ fail: () => failing })
    await open()
    const detail = await expand(RULES)
    button(group(detail, 'added'), '加载更多')!.click()
    await flushPromises()
    expect(group(detail, 'added').querySelector('[data-load-error]')!.textContent).toBe('加载失败：服务暂时不可用')
    expect(items(detail, 'added')).toHaveLength(20)
    failing = false
    button(group(detail, 'added'), '重试')!.click()
    await flushPromises()
    expect(group(detail, 'added').querySelector('[data-load-error]')).toBeNull()
    expect(items(detail, 'added')).toHaveLength(70)
    expect(calls.map(call => call.offset)).toEqual([20, 20])
  })

  it('keeps the old note without paging (an older server, or the kept entries ran out)', async () => {
    const old = response()
    const details = old.files![1]!.details!
    old.files![1]!.details = { ...details, paging: undefined, limited: true,
      groups: details.groups.map(({ available: _available, ...rest }) => rest) }
    vi.mocked(api.get).mockResolvedValue({ data: old } as any)
    await open()
    const detail = await expand(RULES)
    expect(flat(group(detail, 'added'))).toContain('还有 110 条未列出')
    expect(detail.querySelector('[data-detail-search]')).toBeNull()
    expect(detail.querySelector('[data-load-more]')).toBeNull()
    expect(detail.textContent).toContain('明细已达总量上限')
  })

  it('says how many entries were over the kept limit once the kept ones are loaded', async () => {
    const capped = response()
    capped.files![1]!.details!.groups[0] = { ...capped.files![1]!.details!.groups[0]!, available: 70 }
    vi.mocked(api.get).mockImplementation((async (url: string, config: any) => url.endsWith('/push-diff') ? { data: capped }
      : { data: { status: 'ok', total: 70, offset: config.params.offset, entries: ADDED.slice(20, 70), next_offset: null } }) as any)
    await open()
    const detail = await expand(RULES)
    expect(flat(group(detail, 'added'))).toContain('还剩 50 条')
    button(group(detail, 'added'), '加载更多')!.click()
    await flushPromises()
    expect(items(detail, 'added')).toHaveLength(70)
    expect(flat(group(detail, 'added').querySelector('[data-not-kept]')!)).toBe('另有 60 条超出明细上限，未列出。')
  })
})

describe('push diff details: search', () => {
  it('searches on the server after a pause, pages the matches and returns to the list when cleared', async () => {
    vi.useFakeTimers()
    const calls = server()
    await open()
    const detail = await expand(RULES)
    button(group(detail, 'added'), '加载更多')!.click()
    await flushPromises()
    expect(items(detail, 'added')).toHaveLength(70)
    const input = detail.querySelector<HTMLInputElement>('[data-detail-search]')!
    expect(input.getAttribute('aria-label')).toBe('搜索规则')
    calls.length = 0

    await type(detail, 'FRESH-1')
    await type(detail, 'fresh-0')  // typing on: only the last text is searched
    await vi.advanceTimersByTimeAsync(299)
    expect(calls).toEqual([])
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(calls).toEqual([
      { version: 'v1', file: RULES, change: 'added', offset: 0, limit: 50, q: 'fresh-0' },
      { version: 'v1', file: RULES, change: 'removed', offset: 0, limit: 50, q: 'fresh-0' }])
    // fresh-000..099: 100 matches, 50 on the first page.
    expect(flat(detail.querySelector('[data-search-summary]')!)).toBe('匹配 100 条')
    expect(flat(group(detail, 'added').querySelector('p')!)).toBe('新增 130 条规则 · 匹配 100 条')
    expect(flat(group(detail, 'removed').querySelector('p')!)).toBe('删除 3 条规则 · 匹配 0 条')
    expect(items(detail, 'added')).toHaveLength(50)
    expect(items(detail, 'removed')).toHaveLength(0)
    expect(flat(group(detail, 'added'))).toContain('加载更多（还剩 50 条）')
    button(group(detail, 'added'), '加载更多')!.click()
    await flushPromises()
    expect(calls.at(-1)).toEqual({ version: 'v1', file: RULES, change: 'added', offset: 50, limit: 50, q: 'fresh-0' })
    expect(items(detail, 'added')).toHaveLength(100)
    expect(itemTexts(detail, 'added').at(-1)).toBe('+ +.fresh-099.lab')
    expect(group(detail, 'added').querySelector('[data-all-loaded]')).not.toBeNull()

    await type(detail, 'no-such-rule')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(flat(detail.querySelector('[data-search-summary]')!)).toBe('匹配 0 条')
    expect(detail.querySelector('[data-search-empty]')!.textContent).toContain('没有匹配「no-such-rule」的规则')

    // Clearing goes straight back to the list as it was, the loaded pages included.
    const sent = calls.length
    detail.querySelector<HTMLButtonElement>('[aria-label="清空搜索"]')!.click()
    await flushPromises()
    expect(input.value).toBe('')
    expect(calls).toHaveLength(sent)
    expect(items(detail, 'added')).toHaveLength(70)
    expect(flat(group(detail, 'added').querySelector('p')!)).toBe('新增 130 条规则')
    expect(detail.querySelector('[data-search-summary]')!.textContent).toBe('')
  })

  it('applies Enter at once and offers no search where everything is shown', async () => {
    const calls = server()
    await open()
    const detail = await expand(FEED)
    expect(detail.querySelector<HTMLInputElement>('[data-detail-search]')!.getAttribute('aria-label')).toBe('搜索节点名、服务器或字段')
    await type(detail, '备用 05')
    detail.querySelector<HTMLInputElement>('[data-detail-search]')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await flushPromises()
    expect(calls).toEqual([{ version: 'v1', file: FEED, change: 'modified', offset: 0, limit: 50, q: '备用 05' }])
    // Matches show in the same table as the list.
    expect(itemTexts(detail, 'modified')).toEqual(Array.from({ length: 10 }, (_, index) => `D 备用 05${index}`))
    expect(group(detail, 'modified').querySelectorAll('[data-node-table]')).toHaveLength(1)
    expect([...group(detail, 'modified').querySelectorAll('[data-node-table] > tbody:first-of-type tr')].map(row => row.textContent))
      .toEqual(['D 备用 050port：443 → 8050', 'password：pw-before-050 → pw-after-050'])

    const small = response()
    small.files![0]!.details!.groups[0] = { change: 'modified', total: 2, available: 2, omitted: 0, entries: NODES.slice(0, 2) }
    vi.mocked(api.get).mockResolvedValue({ data: small } as any)
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    await open()
    expect((await expand(FEED)).querySelector('[data-detail-search]')).toBeNull()
  })
})

describe('push diff details: stale results and hidden values', () => {
  it('asks to reload when the comparison changed, in every open detail, and reloads keeping them open', async () => {
    let stale = false
    const calls = server({ versions: ['v1', 'v2'], stale: () => stale })
    await open()
    const rules = await expand(RULES)
    const feed = await expand(FEED)
    stale = true
    button(group(rules, 'added'), '加载更多')!.click()
    await flushPromises()
    for (const detail of [rules, feed]) {
      const alert = detail.querySelector('[data-details-stale]')!
      expect(alert.getAttribute('role')).toBe('alert')
      expect(flat(alert)).toContain('对比结果已更新，请重新加载。')
      expect(alert.querySelector('[data-details-reload]')!.textContent).toContain('重新加载')
      expect(detail.querySelector('[data-load-more]')).toBeNull()  // nothing from the old list is added
      expect(detail.querySelector<HTMLInputElement>('[data-detail-search]')!.disabled).toBe(true)
    }
    expect(items(rules, 'added')).toHaveLength(20)
    expect(document.activeElement).toBe(rules.querySelector('[data-details-reload]'))  // focus moves to the way out

    stale = false
    rules.querySelector<HTMLButtonElement>('[data-details-reload]')!.click()
    await flushPromises()
    expect(api.get).toHaveBeenLastCalledWith('/agents/lab-mihomo/push-diff', expect.anything())
    const reopened = body().querySelector(`[data-file="${RULES}"]`)!.nextElementSibling!.querySelector('[data-file-details]')!
    expect(body().querySelector(`[data-file="${FEED}"]`)!.nextElementSibling!.querySelector('[data-file-details]')).not.toBeNull()
    expect(reopened.querySelector('[data-details-stale]')).toBeNull()
    button(group(reopened, 'added'), '加载更多')!.click()
    await flushPromises()
    expect(calls.at(-1)).toMatchObject({ version: 'v2', offset: 20 })
    expect(items(reopened, 'added')).toHaveLength(70)
  })

  it('a stale answer to a search asks to reload as well', async () => {
    vi.useFakeTimers()
    let stale = false
    server({ stale: () => stale })
    await open()
    const detail = await expand(RULES)
    stale = true
    await type(detail, 'fresh')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(detail.querySelector('[data-details-stale]')).not.toBeNull()
    expect(detail.querySelector('[data-search-summary]')!.textContent).toBe('')
  })

  it('hides credentials in loaded entries as well, before and after they arrive', async () => {
    server()
    await open()
    const detail = await expand(FEED)
    const html = () => body().innerHTML
    expect(html()).toContain('pw-after-000')
    const hide = body().querySelector<HTMLButtonElement>('[data-push-diff-hide] button[role="switch"]')!
    hide.click()
    await flushPromises()
    button(group(detail, 'modified'), '加载更多')!.click()  // loaded while hidden
    await flushPromises()
    expect(items(detail, 'modified')).toHaveLength(60)
    for (let index = 0; index < 60; index += 1) {
      expect(html()).not.toContain(`pw-before-${pad(index)}`)
      expect(html()).not.toContain(`pw-after-${pad(index)}`)
    }
    expect(detail.querySelectorAll('[data-secret-hidden]')).toHaveLength(120)
    // Loaded rows join the same table: still one table, each node's name across its two field rows.
    expect(group(detail, 'modified').querySelectorAll('[data-node-table]')).toHaveLength(1)
    const last = items(detail, 'modified')[59]!
    expect(last.querySelector('th')!.getAttribute('rowspan')).toBe('2')
    expect([...last.querySelectorAll('tr')].map(row => [...row.querySelectorAll('td')].map(cell => cell.textContent).join('')))
      .toEqual(['port：443 → 8059', 'password：已隐藏 → 已隐藏'])
    hide.click()
    await flushPromises()
    expect(html()).toContain('pw-after-059')
    hide.click()
    await flushPromises()
    // Searching while hidden: matches render hidden too.
    vi.useFakeTimers()
    await type(detail, '备用 04')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(items(detail, 'modified')).toHaveLength(10)
    expect(html()).not.toContain('pw-after-04')
    expect(detail.querySelectorAll('[data-secret-hidden]')).toHaveLength(20)
  })
})
