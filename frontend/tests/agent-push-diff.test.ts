import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AgentPushDiff from '@/components/agents/AgentPushDiff.vue'
import api from '@/api'
import type { PushDiffResponse } from '@/api/agentPushDiff'
import { diffLineTone, fieldText, summaryText, textOf, unavailableCopy, valueText } from '@/lib/agentPushDiff'
import { getAgentConfigSync } from '@/lib/agentConfigSync'
import type { Agent } from '@/types'

vi.mock('@/api', () => ({ default: { get: vi.fn() } }))

const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.useRealTimers()
})
beforeEach(() => vi.mocked(api.get).mockReset())

const agent = (overrides: Partial<Agent> = {}) => ({
  id: 'lab-mihomo', name: '实验 Mihomo', host: '192.0.2.2', port: 27647, token: '', service_type: 'mihomo', status: 'online',
  last_heartbeat: '', version: '1.8.50-go', config_version: '0a1b2c3d', enabled: true,
  content_sync: { status: 'changed' }, ...overrides,
}) as Agent

// Every credential the response below carries; the dialog shows them by default and hides them on request.
const SECRETS = ['old-controller-0000', 'controller-secret-0001', 'ss-pass-before', 'ss-pass-after', 'pull-token-0000',
  'pull-token-1234', 'feed-user:feed-pass', 'hk-node-pass-before', 'hk-node-pass-after']

const changed: PushDiffResponse = {
  status: 'changed', checked_at: new Date().toISOString(), service_type: 'mihomo', content: 'full', content_reason: null,
  baseline: { pushed_at: '2026-10-31T08:00:00Z', config_revision: 12 },
  counts: { added: 1, removed: 0, modified: 3, unchanged: 2 },
  files: [
    { path: 'config.yaml', role: 'config', status: 'modified', summary: { kind: 'config', changes: 4, before: 2048, after: 2300 } },
    { path: 'providers/实验机场.yaml', role: 'provider', name: '实验机场', status: 'modified',
      summary: { kind: 'nodes', before: 2, after: 3, added: 2, removed: 1, modified: 1 },
      details: { kind: 'nodes', repeated: 1, groups: [
        { change: 'added', total: 2, omitted: 0, entries: [
          { change: 'added', text: 'C 新增节点', info: 'socks5 · edge-c.lab:1080' },
          { change: 'added', text: 'B 日本', nth: 2, info: 'ss · [2001:db8::7]:8443' }] },
        { change: 'removed', total: 1, omitted: 0, entries: [{ change: 'removed', text: 'D 下线' }] },
        { change: 'modified', total: 1, omitted: 0, entries: [{ change: 'modified', text: 'HK 01', fields_omitted: 0, fields: [
          { key: 'server', change: 'modified', sensitive: false, old: 'a.lab', new: 'b.lab' },
          { key: 'port', change: 'modified', sensitive: false, old: '11005', new: '11008' },
          { key: 'password', change: 'modified', sensitive: true, old: [{ secret: 'hk-node-pass-before' }], new: [{ secret: 'hk-node-pass-after' }] },
          { key: 'plugin-opts.host', change: 'modified', sensitive: false, old: 'cdn-a.lab', new: 'cdn-b.lab' }] }] },
      ] } },
    { path: 'ruleset/直连.list', role: 'rule', name: '直连', status: 'modified',
      summary: { kind: 'rules', before: 40000, after: 40120, added: 150, removed: 30 },
      details: { kind: 'rules', limited: true, groups: [
        { change: 'added', total: 150, omitted: 147, entries: ['+.new-0.lab', '+.new-1.lab', '+.new-2.lab'].map(text => ({ change: 'added' as const, text })) },
        { change: 'removed', total: 30, omitted: 28, entries: ['+.old-0.lab', '+.old-1.lab'].map(text => ({ change: 'removed' as const, text })) },
      ] } },
    { path: 'ruleset/新规则.list', role: 'rule', name: '新规则', status: 'added',
      summary: { kind: 'rules', before: null, after: 8, added: 8, removed: 0 } },
  ],
  unchanged: [{ path: 'ruleset/广告.list', role: 'rule', name: '广告', status: 'unchanged' },
    { path: 'providers/备用.yaml', role: 'provider', name: ['备用 ', { secret: 'feed-user:feed-pass' }, '@relay.lab'], status: 'unchanged' }],
  unchanged_omitted: 0,
  config: {
    format: 'yaml', status: 'compared', omitted: 0, total: 5,
    items: [
      { key: 'hosts.printer.lan', change: 'added', sensitive: false, old: null, new: '192.168.1.30' },
      { key: 'secret', change: 'modified', sensitive: true, old: [{ secret: 'old-controller-0000' }], new: [{ secret: 'controller-secret-0001' }] },
      { key: 'proxy-providers.实验机场.url', change: 'modified', sensitive: false,
        old: ['https://cf.lab/api/subscriptions/feed/proxies?token=', { secret: 'pull-token-0000' }],
        new: ['https://cf.lab/api/subscriptions/feed/proxies?token=', { secret: 'pull-token-1234' }] },
      { key: 'dns.fake-ip-filter', change: 'modified', sensitive: false, list: {
        before: 2, after: 63, added: 61, removed: 0, modified: 0, reordered: false, omitted: 1,
        entries: Array.from({ length: 60 }, (_, index) => ({ change: 'added' as const, text: `+.site-${index}.example` })) } },
      { key: 'proxies', change: 'modified', sensitive: false, list: {
        before: 1, after: 1, added: 0, removed: 0, modified: 1, reordered: false, omitted: 0, identity: 'name',
        entries: [{ change: 'modified', text: '手动 SS', fields: [
          { key: 'password', change: 'modified', sensitive: true, old: [{ secret: 'ss-pass-before' }], new: [{ secret: 'ss-pass-after' }] },
          { key: 'port', change: 'modified', sensitive: false, old: '443', new: '8388' }], fields_omitted: 0 }] } },
    ],
    raw: { lines: ['--- a/config.yaml', '+++ b/config.yaml', '@@ -1,3 +1,4 @@', ' hosts:', '+  printer.lan: 192.168.1.30', '',
      ['-secret: ', { secret: 'old-controller-0000' }], ['+secret: ', { secret: 'controller-secret-0001' }]], truncated: false, omitted_lines: 0 },
  },
}

async function open(responses: PushDiffResponse[], props: Partial<{ canPush: boolean; pushing: boolean; offline: boolean }> = {},
  item = agent()) {
  responses.forEach(response => vi.mocked(api.get).mockResolvedValueOnce({ data: response } as any))
  const wrapper = mount(AgentPushDiff, { attachTo: document.body, props: {
    agent: item, canPush: true, pushing: false, offline: false, ...props } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

const body = () => document.body

describe('push diff copy', () => {
  it('summarises nodes, rules, sizes and configuration changes', () => {
    expect(summaryText(changed.files![1])).toBe('节点 +2 / −1 · 修改 1 · 共 3 个')
    expect(summaryText(changed.files![2])).toBe('规则 +150 / −30 · 共 40120 条')
    expect(summaryText(changed.files![0])).toBe('4 项配置变化 · 2.0 KB → 2.2 KB（+252 B）')
    expect(summaryText({ path: 'extra.txt', role: 'custom', status: 'removed', summary: { kind: 'size', before: 3, after: null } })).toBe('原 3 B')
    expect(summaryText({ path: 'x', role: 'rule', status: 'modified' })).toBe('上次推送的内容未保存，无法细分')
  })

  it('shows values as they are and replaces only the marked pieces when hidden', () => {
    const secret = changed.config!.items[1]!
    expect(valueText(secret, 'old')).toEqual({ value: [{ secret: 'old-controller-0000' }], empty: false })
    expect(textOf(valueText(secret, 'old').value, false)).toBe('old-controller-0000')
    expect(textOf(valueText(secret, 'old').value, true)).toBe('已隐藏')
    expect(textOf(changed.config!.items[2]!.new, true)).toBe('https://cf.lab/api/subscriptions/feed/proxies?token=已隐藏')
    expect(valueText(changed.config!.items[0]!, 'old')).toEqual({ value: '—', empty: true })
    const [password, port] = changed.config!.items[4]!.list!.entries![0]!.fields!
    expect(fieldText(password!)).toBe('password：ss-pass-before → ss-pass-after')
    expect(fieldText(password!, true)).toBe('password：已隐藏 → 已隐藏')
    expect(fieldText(port!, true)).toBe('port：443 → 8388')
    expect(fieldText({ key: 'proxies', change: 'modified', sensitive: false, list: {
      before: 3, after: 3, added: 2, removed: 1, modified: 0, reordered: false,
      added_items: ['US', ['relay ', { secret: 'u:p' }, '@h']], removed_items: ['JP'] } }, true))
      .toBe('proxies：+ US、relay 已隐藏@h；− JP')
    expect(fieldText({ key: 'token', change: 'added', sensitive: true, old: null, new: [{ secret: 'abc' }] }, true)).toBe('token：新增 已隐藏')
  })

  it('explains each missing baseline by its server reason', () => {
    expect(unavailableCopy({ status: 'never-pushed' }).description).toContain('还没有成功推送过')
    expect(unavailableCopy({ status: 'unknown', reason: 'baseline_missing' }).description).toContain('旧版本')
    expect(unavailableCopy({ status: 'unknown', reason: 'deployment_pending' })).toMatchObject({ title: '正在推送', pushable: false })
    expect(unavailableCopy({ status: 'unknown', reason: 'raw private detail' }).description).not.toContain('raw private detail')
    expect(diffLineTone('+a')).toBe('added')
    expect(diffLineTone('+++ b/config.yaml')).toBe('meta')
  })

  it('opens the dialog for every server conclusion, not while pushing or checking', () => {
    for (const status of ['changed', 'synced', 'never-pushed'] as const) {
      expect(getAgentConfigSync({ content_sync: { status } }, 1).diffable).toBe(true)
    }
    expect(getAgentConfigSync({ content_sync: { status: 'unknown', reason: 'baseline_missing' } }, 1).diffable).toBe(true)
    expect(getAgentConfigSync({ content_sync: { status: 'checking' } }, 1).diffable).toBe(false)
    expect(getAgentConfigSync({ content_sync: { status: 'changed' } }, 1, { status: 'uploading' }).diffable).toBe(false)
    expect(getAgentConfigSync({ content_sync: { status: 'changed' } }, 1, { status: 'succeeded', pending: true }).diffable).toBe(false)
    // An old server without a comparison has no /push-diff to open.
    expect(getAgentConfigSync({ config_version: 'abc', config_revision: 3 }, 3).diffable).toBe(false)
  })
})

describe('AgentPushDiff dialog', () => {
  it('polls while the server prepares, then shows files, configuration items and a collapsed raw diff', async () => {
    vi.useFakeTimers()
    vi.mocked(api.get).mockResolvedValueOnce({ data: { status: 'checking' } } as any)
    vi.mocked(api.get).mockResolvedValueOnce({ data: changed } as any)
    const wrapper = mount(AgentPushDiff, { attachTo: document.body, props: { agent: agent(), canPush: true, pushing: false, offline: false } })
    wrappers.push(wrapper)
    await flushPromises()
    expect(body().querySelector('[data-push-diff-loading]')?.textContent).toContain('正在生成这次将要推送的内容')
    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(api.get).toHaveBeenLastCalledWith('/agents/lab-mihomo/push-diff', expect.anything())

    const files = body().querySelector('[data-push-diff-files]')!
    const rows = [...files.querySelectorAll('tbody tr[data-file]')].map(row => row.textContent!.replace(/\s+/g, ' ').trim())
    expect(rows[0]).toContain('主配置')
    expect(rows[1]).toMatch(/实验机场.*订阅.*修改.*节点 \+2 \/ −1 · 修改 1/)
    expect(rows[2]).toContain('规则 +150 / −30')
    expect(rows[3]).toMatch(/新规则.*新增.*规则 \+8/)
    expect(body().querySelector('[data-push-diff-counts]')!.textContent).toMatch(/修改\s*3.*新增\s*1.*删除\s*0.*未变\s*2/)
    expect(files.textContent).toContain('未变的 2 个文件')
    expect(files.textContent).not.toContain('广告')

    const config = body().querySelector('[data-push-diff-config]')!
    const hosts = config.querySelector('[data-key="hosts.printer.lan"]')!
    expect(hosts.textContent).toContain('新增')
    expect(hosts.textContent).toContain('192.168.1.30')
    const secret = config.querySelector('[data-key="secret"]')!
    expect(secret.textContent!.replace(/\s+/g, '')).toBe('secret修改old-controller-0000controller-secret-0001')
    const filter = config.querySelector('[data-key="dns.fake-ip-filter"]')!
    expect(filter.textContent).toContain('2 条')
    expect(filter.textContent).toContain('63 条 +61 / −0')
    expect(config.textContent).not.toContain('+.site-0.example')

    const raw = body().querySelector('[data-push-diff-raw]')!
    expect(raw.querySelector('pre')).toBeNull()
    expect(raw.textContent).toContain('主配置的 unified diff')
    expect(raw.textContent).not.toContain('已隐藏')
    vi.useRealTimers()
  })

  it('expands long list details with the truncation note and opens the raw diff', async () => {
    await open([changed])
    const filter = body().querySelector('[data-key="dns.fake-ip-filter"]')!
    const toggle = [...filter.querySelectorAll('button')].find(button => button.textContent!.includes('展开明细'))!
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    toggle.click()
    await flushPromises()
    const detail = body().querySelector(`#${toggle.getAttribute('aria-controls')}`)!
    expect(detail.querySelectorAll('li')).toHaveLength(60)
    expect(detail.textContent).toContain('+ +.site-0.example')
    expect(detail.textContent).toContain('还有 1 条未列出，完整内容见下方「原始差异」')
    const proxies = body().querySelector('[data-key="proxies"]')!
    ;[...proxies.querySelectorAll('button')].find(button => button.textContent!.includes('展开明细'))!.click()
    await flushPromises()
    expect(body().textContent).toContain('~ 手动 SS')
    expect(body().textContent).toContain('password：ss-pass-before → ss-pass-after')

    const rawToggle = [...body().querySelectorAll('[data-push-diff-raw] button')].find(button => button.textContent!.includes('原始差异'))!
    rawToggle.dispatchEvent(new Event('click'))
    await flushPromises()
    const lines = [...body().querySelectorAll('#push-diff-raw-text > span')]
    expect(lines.map(line => line.textContent)).toContain('+  printer.lan: 192.168.1.30')
    expect(lines.find(line => line.textContent === '+  printer.lan: 192.168.1.30')!.className).toContain('text-success-accent')
    expect(lines.map(line => line.textContent)).toContain('+secret: controller-secret-0001')
    expect(lines[5]!.textContent).toBe(' ')  // an empty line keeps its height
  })

  /** A detail group: its title and each entry's own line (without the field changes below it). */
  const groupOf = (detail: Element, change: string) => {
    const group = detail.querySelector(`[data-group="${change}"]`)!
    const clean = (text: string) => text.replace(/\s+/g, ' ').trim()
    return {
      title: clean(group.querySelector('p')!.textContent!),
      entries: [...group.querySelectorAll(':scope > ul > li')].map(item =>
        clean([...item.childNodes].filter(node => node.nodeName !== 'UL' && node.nodeType !== Node.COMMENT_NODE)
          .map(node => node.textContent).join(''))),
    }
  }

  const expandButton = (row: Element) => [...row.querySelectorAll('button')].find(button => button.textContent!.includes('展开明细'))!

  it('expands subscription and rule set rows into nodes and rules grouped by change', async () => {
    await open([changed])
    const files = body().querySelector('[data-push-diff-files]')!
    // Only files with details offer it: not the main configuration (table 2) or a file without node or rule detail.
    expect(files.querySelector('[data-file="config.yaml"] button')).toBeNull()
    expect(files.querySelector('[data-file="ruleset/新规则.list"] button')).toBeNull()
    const feed = files.querySelector('[data-file="providers/实验机场.yaml"]')!
    const toggle = expandButton(feed)
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(files.querySelector('[data-file-details]')).toBeNull()  // collapsed until asked
    toggle.click()
    await flushPromises()
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(toggle.textContent).toContain('收起明细')
    expect(feed.className).toContain('is-expanded')
    const detail = body().querySelector(`#${toggle.getAttribute('aria-controls')}`)!
    expect(detail.previousElementSibling).toBe(feed)  // right below its file
    expect(groupOf(detail, 'added')).toEqual({ title: '新增 2 个节点', entries: [
      '+ C 新增节点 · socks5 · edge-c.lab:1080', '+ B 日本（同名第 2 个） · ss · [2001:db8::7]:8443'] })
    expect(groupOf(detail, 'removed')).toEqual({ title: '删除 1 个节点', entries: ['− D 下线'] })
    expect(groupOf(detail, 'modified')).toEqual({ title: '修改 1 个节点', entries: ['~ HK 01'] })
    const fields = [...detail.querySelectorAll('[data-group="modified"] li li')].map(item => item.textContent)
    expect(fields).toEqual(['server：a.lab → b.lab', 'port：11005 → 11008', 'password：hk-node-pass-before → hk-node-pass-after',
      'plugin-opts.host：cdn-a.lab → cdn-b.lab'])
    expect(detail.querySelector('[data-group="added"] .text-success-accent')!.textContent).toContain('+ C 新增节点')
    expect(detail.textContent).toContain('有 1 个节点名被多个节点共用')
    expect(detail.textContent).not.toContain('未列出')

    const rules = files.querySelector('[data-file="ruleset/直连.list"]')!
    expandButton(rules).click()
    await flushPromises()
    const ruleDetail = rules.nextElementSibling!
    expect(groupOf(ruleDetail, 'added')).toEqual({ title: '新增 150 条规则', entries: ['+ +.new-0.lab', '+ +.new-1.lab', '+ +.new-2.lab'] })
    expect(groupOf(ruleDetail, 'removed')).toEqual({ title: '删除 30 条规则', entries: ['− +.old-0.lab', '− +.old-1.lab'] })
    expect(ruleDetail.querySelector('[data-group="added"]')!.textContent).toContain('还有 147 条未列出')
    expect(ruleDetail.querySelector('[data-group="removed"]')!.textContent).toContain('还有 28 条未列出')
    expect(ruleDetail.textContent).toContain('明细已达总量上限')

    toggle.click()
    await flushPromises()
    expect(body().querySelector('[data-file-details="nodes"]')).toBeNull()
    expect(toggle.textContent).toContain('展开明细')
    expect(body().querySelector('[data-file-details="rules"]')).not.toBeNull()  // each file on its own
  })

  async function expandAll() {
    for (const key of ['dns.fake-ip-filter', 'proxies']) expandButton(body().querySelector(`[data-key="${key}"]`)!).click()
    for (const path of ['providers/实验机场.yaml', 'ruleset/直连.list']) expandButton(body().querySelector(`[data-file="${path}"]`)!).click()
    ;[...body().querySelectorAll('[data-push-diff-files] button')].find(button => button.textContent!.includes('未变的'))!.click()
    ;[...body().querySelectorAll('[data-push-diff-raw] button')].find(button => button.textContent!.includes('原始差异'))!.click()
    await flushPromises()
  }

  const hideSwitch = () => body().querySelector<HTMLButtonElement>('[data-push-diff-hide] button[role="switch"]')!

  it('hides every marked value on request, also in details, file names and the raw diff, and shows them again', async () => {
    await open([changed])
    await expandAll()
    const html = () => body().innerHTML
    expect(hideSwitch().getAttribute('aria-checked')).toBe('false')
    for (const secret of SECRETS) expect(html()).toContain(secret)
    expect(body().querySelector('[data-push-diff-hide]')!.textContent).toContain('隐藏敏感信息')

    hideSwitch().click()
    await flushPromises()
    expect(hideSwitch().getAttribute('aria-checked')).toBe('true')
    for (const secret of SECRETS) expect(html()).not.toContain(secret)
    // secret and URL rows (old and new), the manual node's and the subscription node's passwords (old and new),
    // the file name, two raw diff lines
    expect(body().querySelectorAll('[data-secret-hidden]')).toHaveLength(11)
    expect(body().querySelector('[data-file-details="nodes"]')!.textContent).toContain('password：已隐藏 → 已隐藏')
    expect(body().querySelector('[data-file-details="nodes"]')!.textContent).toContain('server：a.lab → b.lab')  // addresses stay
    expect(body().querySelector('[data-key="secret"]')!.textContent!.replace(/\s+/g, '')).toBe('secret修改已隐藏已隐藏')
    expect(body().querySelector('[data-key="proxy-providers.实验机场.url"]')!.textContent).toContain('?token=已隐藏')
    expect(body().textContent).toContain('password：已隐藏 → 已隐藏')
    expect(body().textContent).toContain('备用 已隐藏@relay.lab')
    const lines = [...body().querySelectorAll('#push-diff-raw-text > span')].map(line => line.textContent)
    expect(lines).toContain('+secret: 已隐藏')
    expect(lines).toContain('+  printer.lan: 192.168.1.30')  // domains and addresses stay
    expect(body().querySelector('[data-push-diff-raw]')!.textContent).toContain('敏感信息已隐藏')

    hideSwitch().click()
    await flushPromises()
    for (const secret of SECRETS) expect(html()).toContain(secret)
    expect(body().querySelector('[data-secret-hidden]')).toBeNull()
  })

  it('does not remember the switch: every opening shows values', async () => {
    await open([changed])
    hideSwitch().click()
    await flushPromises()
    expect(body().innerHTML).not.toContain('controller-secret-0001')
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    await open([changed])
    expect(hideSwitch().getAttribute('aria-checked')).toBe('false')
    expect(body().innerHTML).toContain('controller-secret-0001')
  })

  it('offers the existing push and reports it to the page', async () => {
    const wrapper = await open([changed])
    const push = body().querySelector<HTMLButtonElement>('[data-push-diff-push]')!
    expect(push.disabled).toBe(false)
    push.click()
    expect(wrapper.emitted('push')).toHaveLength(1)
  })

  it('disables push while busy and explains offline devices', async () => {
    await open([changed], { canPush: false, pushing: true })
    expect(body().querySelector<HTMLButtonElement>('[data-push-diff-push]')!.disabled).toBe(true)
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    await open([changed], { offline: true })
    expect(body().querySelector('[data-push-diff-push]')).toBeNull()
    expect(body().textContent).toContain('Agent 离线，上线后才能推送')
  })

  it('shows no changes when synced and asks the page to refresh an outdated card', async () => {
    const wrapper = await open([{ status: 'synced', baseline: { pushed_at: '2026-10-31T08:00:00Z' } }])
    expect(body().querySelector('[data-push-diff-synced]')!.textContent).toContain('无变更')
    expect(body().querySelector('[data-push-diff-hide]')).toBeNull()  // nothing to hide
    expect(body().querySelector('[data-push-diff-push]')).toBeNull()
    expect(wrapper.emitted('refresh')).toHaveLength(1)
  })

  it('explains a missing baseline and still offers the push that creates one', async () => {
    await open([{ status: 'never-pushed' }], {}, agent({ content_sync: { status: 'synced' } }))
    expect(body().querySelector('[data-push-diff-unavailable]')!.textContent).toContain('没有可对比的基准')
    expect(body().querySelector('[data-push-diff-push]')).not.toBeNull()
  })

  it('lists files by digest only when the previous bundle was not kept', async () => {
    await open([{ ...changed, content: 'digests', content_reason: 'service_without_bundle',
      files: [{ path: 'config.yaml', role: 'config', status: 'modified' }],
      config: { format: 'yaml', status: 'unavailable', reason: 'service_without_bundle', items: [], omitted: 0 } }])
    expect(body().querySelector('[data-push-diff-notice]')!.textContent).toContain('MosDNS 推送时不保存发布包')
    expect(body().querySelector('[data-push-diff-config-notice]')!.textContent).toContain('无法按配置项对比')
    expect(body().querySelector('[data-push-diff-config]')).toBeNull()
    expect(body().querySelector('[data-file="config.yaml"]')!.textContent).toContain('上次推送的内容未保存')
  })

  it('shows a readable request error and retries', async () => {
    vi.mocked(api.get).mockRejectedValueOnce({ response: { data: { message: '找不到这台 Agent，请刷新设备列表。' } } })
    vi.mocked(api.get).mockResolvedValueOnce({ data: changed } as any)
    const wrapper = mount(AgentPushDiff, { attachTo: document.body, props: { agent: agent(), canPush: true, pushing: false, offline: false } })
    wrappers.push(wrapper)
    await flushPromises()
    const error = body().querySelector('[data-push-diff-error]')!
    expect(error.getAttribute('role')).toBe('alert')
    expect(error.textContent).toContain('找不到这台 Agent')
    ;[...error.querySelectorAll('button')].find(button => button.textContent!.includes('重试'))!.click()
    await flushPromises()
    expect(body().querySelector('[data-push-diff-changed]')).not.toBeNull()
  })

  it('stops polling when closed', async () => {
    vi.useFakeTimers()
    vi.mocked(api.get).mockResolvedValue({ data: { status: 'checking' } } as any)
    const wrapper = mount(AgentPushDiff, { attachTo: document.body, props: { agent: agent(), canPush: true, pushing: false, offline: false } })
    await flushPromises()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(10_000)
    expect(api.get).toHaveBeenCalledTimes(1)
  })
})
