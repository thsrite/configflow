import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import RuleEditorDialog from '@/components/rules/RuleEditorDialog.vue'
import { Select } from '@/components/ui/select'
import { confirmState, notify, settleConfirm } from '@/lib/feedback'
import { CLASSICAL_CONTENT_HINT } from '@/lib/ruleset-content'
import type { RuleDnsSnapshot } from '@/types'

const network = vi.hoisted(() => ({
  compose: vi.fn(),
  update: vi.fn(async (_id: string, _body: Record<string, any>, _profile?: string) => ({ data: { success: true } })),
  create: vi.fn(async (_body: Record<string, any>, _profile?: string) => ({ data: { success: true } })),
  setUpdate: vi.fn(async (_id: string, _body: Record<string, any>, _profile?: string) => ({ data: { success: true } })),
  post: vi.fn(async (path: string, body?: any): Promise<{ data: any }> => (path === '/rule-library/check-content'
    ? { data: { content: body.content, changes: [], errors: [] } }
    : { data: { success: true, available: true } })),
  put: vi.fn(async (_path: string, _body: Record<string, any>) => ({ data: { success: true } })),
  copy: vi.fn(async (_text: string) => undefined)
}))
vi.mock('@/api', () => ({
  default: { post: network.post, put: network.put },
  ruleApi: { compose: network.compose, update: network.update, create: network.create },
  ruleSetApi: { update: network.setUpdate }
}))
vi.mock('@/lib/clipboard', () => ({ copyText: network.copy }))

const snapshot = (patch: Partial<RuleDnsSnapshot['engine']> = {}, rest: Partial<RuleDnsSnapshot> = {}): RuleDnsSnapshot => ({
  engine: { mode: 'template', editable: true, reason: '', default_forward: 'forward_remote', prefer_ipv4: true,
    local_probe_active: false, oxidns_in_use: true, warnings: [], agents: [], ...patch },
  upstreams: [{ key: 'b2e55ef47454', source: 'local', addr: 'udp://223.5.5.5', label: '直连 · 223.5.5.5', linked: false }],
  rules: {}, mosdns: {}, ...rest
})
const existing = [
  { id: 'rule-a', itemType: 'rule', rule_type: 'DOMAIN-SUFFIX', value: 'kept.test', policy: 'DIRECT', enabled: true },
  { id: 'rule-b', itemType: 'rule', rule_type: 'DOMAIN-SUFFIX', value: 'old.test', policy: 'DIRECT', enabled: false },
  { id: 'set-a', itemType: 'ruleset', library_rule_id: 'lib-a', name: 'Ads', behavior: 'domain', policy: 'REJECT', enabled: true }
]
const library = [
  { id: 'lib-a', name: 'Ads', behavior: 'domain', source_type: 'url', url: 'https://rules.test/ads.list', enabled: true },
  { id: 'lib-b', name: 'Corp', behavior: 'classical', source_type: 'content', content: 'DOMAIN,corp.lan', enabled: true, has_ip_part: true }
]
const composeResult = (patch: Record<string, unknown> = {}) => ({ data: {
  success: true, data: { rules: [{ id: 'rule-new' }], library: null }, dns: {}, skipped: [], warnings: [], draft: true, ...patch
} })

const wrappers: VueWrapper[] = []
const scrollDescriptor = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollIntoView')

beforeEach(() => {
  vi.clearAllMocks()
  network.compose.mockImplementation(async () => composeResult())
  network.post.mockImplementation(async (path, body) => (path === '/rule-library/check-content' ? { data: { content: body.content, changes: [], errors: [] } } : { data: { success: true, available: true } }))
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: false, media: query, onchange: null, addListener: vi.fn(), removeListener: vi.fn(),
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn()
  })))
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  for (const kind of ['success', 'warning', 'error'] as const) vi.spyOn(notify, kind).mockImplementation(() => 0)
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  settleConfirm('cancel')
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  if (scrollDescriptor) Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', scrollDescriptor)
  else delete (HTMLElement.prototype as any).scrollIntoView
  document.body.innerHTML = ''
})

async function render(props: Record<string, unknown> = {}) {
  const wrapper = mount(RuleEditorDialog, {
    props: { open: true, mode: 'create', rules: existing, library, policies: ['DIRECT', 'REJECT', 'Proxy'], dns: snapshot(), profileId: 'p1', ...props },
    attachTo: document.body
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]')!
const text = () => dialog().textContent || ''
const button = (label: string) => Array.from(dialog().querySelectorAll('button')).find(item => item.textContent?.trim() === label)
async function click(element: Element | undefined | null) {
  expect(element).toBeTruthy()
  ;(element as HTMLElement).click()
  await flushPromises()
}
async function type(selector: string, value: string) {
  const field = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector)!
  field.value = value
  field.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function choose(wrapper: VueWrapper, label: string, value: string) {
  const select = wrapper.findAllComponents(Select).find(item => item.find(`[aria-label="${label}"]`).exists())
  expect(select, label).toBeTruthy()
  select!.vm.$emit('update:modelValue', value)
  await flushPromises()
}
const dnsTrigger = () => document.querySelector<HTMLButtonElement>('#rule-dns')!
async function pickDns(label: string) {
  await click(dnsTrigger())
  await click(Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find(item => item.querySelector('.block')?.textContent?.trim() === label))
}
async function source(label: string) {
  await click(Array.from(dialog().querySelectorAll('[role="radio"]')).find(item => item.textContent?.trim() === label))
}
const save = () => Array.from(dialog().querySelectorAll<HTMLButtonElement>('footer button')).at(-1)!

describe('rule editor dialog', () => {
  it('blocks misspelled pasted types and invalid values while editing one rule', async () => {
    await render()
    await type('#rule-values', 'DOMAIN-SHUFFIX,tmdb.org')
    expect(text()).toContain('不支持的规则类型 DOMAIN-SHUFFIX')
    expect(save().disabled).toBe(true)
    wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = ''
    const wrapper = await render({ mode: 'edit', item: { id: 'ip', itemType: 'rule', rule_type: 'IP-CIDR6', value: '2001:db8::/32', policy: 'DIRECT' } })
    await type('#rule-value', '+.tmdb.org')
    expect(text()).toContain('不是有效 IP 网段')
    expect(save().disabled).toBe(true)
    expect(network.update).not.toHaveBeenCalled()
    await type('#rule-value', '192.0.2.0/24')
    expect(save().disabled).toBe(false)
    await click(save())
    expect(network.update).toHaveBeenCalledWith('ip', expect.objectContaining({ rule_type: 'IP-CIDR6', value: '192.0.2.0/24' }), 'p1')
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('shows new IPCIDR ruleset errors without losing the draft', async () => {
    const wrapper = await render({ initialSource: 'new_library' })
    await type('#rule-library-name', 'Networks')
    await choose(wrapper, '内容类型', 'ipcidr')
    await source('粘贴内容')
    await type('#rule-library-content', '+.tmdb.org')
    network.post.mockResolvedValueOnce({ data: { content: '+.tmdb.org', changes: [], errors: [{ line: 1, message: 'IPCIDR 只接受 CIDR' }] } })
    await click(save())
    expect(document.querySelector('[data-rule-content-issues]')?.textContent).toContain('第 1 行：IPCIDR 只接受 CIDR')
    expect(network.compose).not.toHaveBeenCalled()
    expect(document.querySelector<HTMLTextAreaElement>('#rule-library-content')?.value).toBe('+.tmdb.org')
    await type('#rule-library-content', '192.0.2.0/24')
    expect(document.querySelector('[data-rule-content-issues]')).toBeNull()
  })

  it('keeps the policy and DNS when switching between sources', async () => {
    const wrapper = await render()
    await choose(wrapper, '策略', 'Proxy')
    await pickDns('不存在')
    await source('规则库')
    expect(dnsTrigger().textContent).toContain('不存在')
    await source('新建规则集')
    await source('输入规则')
    expect(dnsTrigger().textContent).toContain('不存在')
    expect(wrapper.findAllComponents(Select).find(item => item.find('[aria-label="策略"]').exists())!.props('modelValue')).toBe('Proxy')
  })

  it('previews each line with its type, invalid lines and duplicates including disabled ones', async () => {
    await render()
    await type('#rule-values', 'openai.com\nDOMAIN,api.example.org\nIP-CIDR,1.2.3.0/24\nexa mple.net\nkept.test\nold.test')
    const preview = dialog().querySelector('[data-paste-preview]')!.textContent!
    expect(preview).toContain('DOMAIN-SUFFIXopenai.com')
    expect(preview).toContain('DOMAINapi.example.org')
    expect(preview).toContain('IP-CIDR1.2.3.0/24')
    expect(preview).toContain('第 4 行：不是有效域名')
    expect(preview).toContain('第 5 行 kept.test 已在 #1')
    expect(preview).toContain('第 6 行 old.test 已在 #2（已停用）')
    // compose rejects the whole batch when any line is invalid, so saving waits for the fix.
    expect(text()).toContain('有效 3 条，1 行无效需修正或删除 · 第 4 行不是有效域名 · 跳过 2 条重复')
    expect(text()).not.toContain('将添加')
    expect(save().disabled).toBe(true)
    expect(dialog().querySelector('footer')!.textContent).toContain('请先修正或删除 1 行无效内容')
    await type('#rule-values', 'openai.com\nDOMAIN,api.example.org\nIP-CIDR,1.2.3.0/24\nkept.test\nold.test')
    expect(text()).toContain('将添加 3 条 · 跳过 2 条重复')
    expect(save().textContent?.trim()).toBe('保存 3 条规则')
    expect(save().disabled).toBe(false)
  })

  it('updates the follow label with the policy but never changes an explicit choice', async () => {
    const wrapper = await render()
    await type('#rule-values', 'openai.com')
    expect(dnsTrigger().textContent).toContain('跟随策略 · 直连 DNS')
    await choose(wrapper, '策略', 'Proxy')
    expect(dnsTrigger().textContent).toContain('跟随策略 · 代理 DNS')
    await choose(wrapper, '策略', 'REJECT')
    expect(dnsTrigger().textContent).toContain('跟随策略 · 默认上游')
    expect(text()).toContain('需要同时拦截解析请选「拒绝」或「黑洞」')
    await choose(wrapper, '策略', 'DIRECT')
    await pickDns('代理 DNS')
    await choose(wrapper, '策略', 'Proxy')
    await choose(wrapper, '策略', 'DIRECT')
    expect(dnsTrigger().textContent).toContain('代理 DNS')
    expect(text()).toContain('DNS 为单独设置的代理 DNS，不随策略变化')
    await click(button('改为跟随策略'))
    expect(dnsTrigger().textContent).toContain('跟随策略 · 直连 DNS')
  })

  it('disables DNS for types that cannot be routed, sends default and restores the choice afterwards', async () => {
    const item = { id: 'rule-x', itemType: 'rule', rule_type: 'DOMAIN', value: 'x.test', policy: 'DIRECT', enabled: true, remark: '' }
    const wrapper = await render({ mode: 'edit', item, dns: snapshot({}, { rules: { 'rule-x': { state: 'explicit', route: 'blackhole' } } }) })
    expect(dnsTrigger().textContent).toContain('黑洞')
    await choose(wrapper, '规则类型', 'GEOIP')
    expect(dnsTrigger().disabled).toBe(true)
    expect(dnsTrigger().textContent).toContain('默认上游')
    expect(text()).toContain('GEOIP 不参与 DNS 分流')
    await choose(wrapper, '规则类型', 'DOMAIN')
    expect(dnsTrigger().textContent).toContain('黑洞')
    await choose(wrapper, '规则类型', 'GEOIP')
    await click(save())
    expect(network.update).toHaveBeenCalledWith('rule-x', expect.objectContaining({ rule_type: 'GEOIP', dns: 'default' }), 'p1')
  })

  it('keeps IP rules for connections and applies DNS only to classical domain parts', async () => {
    await render()
    await type('#rule-values', 'IP-CIDR,1.2.3.0/24')
    expect(dnsTrigger().disabled).toBe(true)
    expect(text()).toContain('IP-CIDR 不参与 DNS 分流')
    expect(dialog().querySelector('#rule-noresolve')).not.toBeNull()
    await click(save())
    expect(network.compose.mock.calls[0]![0]).not.toHaveProperty('dns')

    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    document.body.innerHTML = ''
    await render({ initialSource: 'library' })
    ;(wrappers[0]!.vm as any).$.setupState.form.libraryIds = ['lib-b']
    await flushPromises()
    expect(dnsTrigger().disabled).toBe(false)
    expect(dnsTrigger().textContent).toContain('跟随策略')
    expect(text()).not.toContain('本地探测')
  })

  it('treats a classical ruleset whose content has no IP lines as domain only', async () => {
    const tiktok = { id: 'lib-c', name: 'TikTok', behavior: 'classical', source_type: 'url', url: 'https://rules.test/tiktok.list', enabled: true, has_ip_part: false }
    await render({ initialSource: 'library', library: [...library, tiktok] })
    ;(wrappers[0]!.vm as any).$.setupState.form.libraryIds = ['lib-c']
    await flushPromises()
    expect(dnsTrigger().textContent).toContain('跟随策略')
    expect(text()).not.toContain('本地探测')
  })

  it('keeps pasted IP lines out of DNS when following the policy', async () => {
    await render()
    await type('#rule-values', 'openai.com\nIP-CIDR,1.2.3.0/24')
    expect(text()).toContain('域名 1 条：跟随策略 · 直连 DNS ／ IP 1 条：仅连接分流')
    await click(save())
    expect(network.compose).toHaveBeenCalledWith({
      source: { kind: 'values', rule_type: 'DOMAIN-SUFFIX', text: 'openai.com\nIP-CIDR,1.2.3.0/24' },
      rule: { policy: 'DIRECT', remark: '', group_name: '', no_resolve: true, enabled: true, position: 'top' },
      dns: 'follow', dns_ip: 'default'
    }, 'p1')
  })

  it('applies an explicit DNS choice only to pasted domain lines', async () => {
    await render()
    await type('#rule-values', 'openai.com\nIP-CIDR,1.2.3.0/24')
    await pickDns('黑洞')
    expect(text()).not.toContain('本地探测')
    await click(save())
    const [payload] = network.compose.mock.calls[0]!
    expect(payload).toMatchObject({ dns: 'blackhole' })
    expect(payload).toHaveProperty('dns_ip', 'default')
  })

  it('points out when MosDNS handles the rule differently', async () => {
    const item = { id: 'rule-m', itemType: 'rule', rule_type: 'DOMAIN-SUFFIX', value: 'm.test', policy: 'DIRECT', enabled: true }
    await render({ mode: 'edit', item, dns: snapshot({}, { rules: { 'rule-m': { state: 'explicit', route: 'blackhole' } }, mosdns: { 'rule-m': 'direct' } }) })
    expect(text()).toContain('MosDNS 仍按直连 DNS处理此规则，与这里的「黑洞」不同；本字段只对 OxiDNS 生效。')
  })

  it('disables DNS up front when native routing is locked and links to the full parameters', async () => {
    await render({ dns: snapshot({ mode: 'native', editable: false, reason: '自定义匹配引用了 rules_domain_3' }) })
    await type('#rule-values', 'openai.com')
    expect(dnsTrigger().disabled).toBe(true)
    expect(dnsTrigger().textContent).toContain('默认上游')
    expect(text()).toContain('DNS 处理需在完整参数修改：自定义匹配引用了 rules_domain_3')
    const link = Array.from(dialog().querySelectorAll('a')).find(item => item.textContent?.includes('打开完整参数'))!
    expect(link.getAttribute('href')).toBe('/generate?oxidns=config')
    expect(link.getAttribute('target')).toBe('_blank')
    await click(save())
    expect(network.compose.mock.calls[0]![0]).not.toHaveProperty('dns')
  })

  it('never sends dns while the DNS state is not loaded', async () => {
    const item = { id: 'rule-u', itemType: 'rule', rule_type: 'DOMAIN-SUFFIX', value: 'u.test', policy: 'DIRECT', enabled: true }
    await render({ mode: 'edit', item, dns: null })
    expect(dnsTrigger().disabled).toBe(true)
    expect(text()).toContain('DNS 状态未加载，本次不修改 DNS')
    await type('#rule-value', 'changed.test')
    await click(save())
    const [, payload] = network.update.mock.calls[0]!
    expect(payload).toMatchObject({ value: 'changed.test' })
    expect(payload).not.toHaveProperty('dns')
  })

  it('offers to save with the default upstream after a 409 routing lock, without leaving the page', async () => {
    const locked = { isAxiosError: true, response: { status: 409, data: { message: '规则仍在使用',
      usages: [{ kind: 'oxidns', code: 'oxidns_routing_custom', target: 'sequence_main', reason: 'sequence_main 第 4 条为手写条目' }] } } }
    network.compose.mockRejectedValueOnce(locked)
    const wrapper = await render()
    await type('#rule-values', 'openai.com')
    await pickDns('黑洞')
    await click(save())
    expect(confirmState.open).toBe(true)
    expect(confirmState.description).toBe('sequence_main 第 4 条为手写条目')
    expect(confirmState.confirmText).toBe('仍保存（DNS 默认上游）')
    expect(confirmState.altText).toBe('复制原因')
    settleConfirm('confirm')
    await flushPromises()
    expect(network.compose).toHaveBeenCalledTimes(2)
    expect(network.compose.mock.calls[0]![0]).toMatchObject({ dns: 'blackhole' })
    expect(network.compose.mock.calls[1]![0]).not.toHaveProperty('dns')
    expect(wrapper.emitted('saved')).toEqual([[{ ids: ['rule-new'], dns: {} }]])
    expect(wrapper.emitted('update:open')).toEqual([[false]])
  })

  it('copies the lock reason and keeps the form open', async () => {
    network.compose.mockRejectedValueOnce({ isAxiosError: true, response: { status: 409, data: { usages: [{ code: 'oxidns_routing_custom', reason: '手写条目' }] } } })
    const wrapper = await render()
    await type('#rule-values', 'openai.com')
    await click(save())
    settleConfirm('alt')
    await flushPromises()
    expect(network.copy).toHaveBeenCalledWith('手写条目')
    expect(network.compose).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('update:open')).toBeUndefined()
  })

  it.each(['domain', 'ipcidr', 'classical'])('preserves %s YAML payload and records its detected format', async behavior => {
    const wrapper = await render({ initialSource: 'new_library' })
    await type('#rule-library-name', 'YAML rules')
    await choose(wrapper, '内容类型', behavior)
    await source('粘贴内容')
    const content = behavior === 'ipcidr' ? '# keep\npayload:\n  - "192.0.2.0/24"\n  - "2001:db8::/32"\n' : '# keep\npayload:\n  - "+.tmdb.org"\n  - "api.example.org"\n'
    await type('#rule-library-content', content)
    expect(text()).toContain('已识别为 YAML payload')
    expect(wrapper.findAllComponents(Select).some(item => item.find('[aria-label="匹配范围"]').exists())).toBe(false)
    expect(save().disabled).toBe(false)
    await click(save())
    expect(network.post).toHaveBeenCalledWith('/rule-library/check-content', { behavior, content, format: 'yaml', append_lines: [] })
    expect(network.compose).toHaveBeenCalledWith(expect.objectContaining({ source: { kind: 'new_library', library: expect.objectContaining({ content, format: 'yaml' }) } }), 'p1')
    expect(confirmState.open).toBe(false)
  })

  it('explains classical delivery and creates a ruleset from the unchanged draft without confirmation', async () => {
    const wrapper = await render({ initialSource: 'new_library' })
    await type('#rule-library-name', 'Mixed rules')
    await choose(wrapper, '内容类型', 'classical')
    // The explanation remains visible for a URL source as well as pasted content.
    expect(text()).toContain(CLASSICAL_CONTENT_HINT)
    await source('粘贴内容')
    expect(text()).toContain(CLASSICAL_CONTENT_HINT)
    const content = '# keep\n  +.tmdb.org \n\n192.0.2.1\n*.manual.test\n'
    await type('#rule-library-content', content)
    network.post.mockResolvedValueOnce({ data: { content, changes: [], errors: [], warnings: [
      { line: 5, before: '*.manual.test', message: '请手动调整' }
    ] } })
    await click(save())
    expect(confirmState.open).toBe(false)
    expect(network.post).toHaveBeenCalledWith('/rule-library/check-content', {
      behavior: 'classical', content, format: 'text', append_lines: []
    })
    expect(network.compose).toHaveBeenCalledWith(expect.objectContaining({
      source: { kind: 'new_library', library: { name: 'Mixed rules', source_type: 'content', content, behavior: 'classical', format: 'text', enabled: true } }
    }), 'p1')
  })

  it('preserves the remote source draft, enabled state and DNS when server type validation fails', async () => {
    const message = '规则集广告拦截（domain）第 4 行：DOMAIN-SUFFIX,example.com\nDomain 不接受 Classical 行。'
    network.compose.mockRejectedValueOnce({ isAxiosError: true, response: { data: { message } } })
    const wrapper = await render({ initialSource: 'new_library' })
    await type('#rule-library-name', '广告拦截')
    await type('#rule-library-url', 'https://rules.test/ads.list')
    await pickDns('黑洞')
    await click(save())
    expect(network.post).not.toHaveBeenCalled()
    expect(confirmState.open).toBe(false)
    expect(network.compose).toHaveBeenCalledWith(expect.objectContaining({
      source: { kind: 'new_library', library: { name: '广告拦截', source_type: 'url', url: 'https://rules.test/ads.list', behavior: 'domain', enabled: true } },
      dns: 'blackhole'
    }), 'p1')
    expect(document.querySelector('[data-rule-save-error]')?.textContent).toBe(message)
    expect(document.querySelector<HTMLInputElement>('#rule-library-url')?.value).toBe('https://rules.test/ads.list')
    expect(notify.error).toHaveBeenCalledWith(message)
    expect(wrapper.emitted('update:open')).toBeUndefined()
    await click(save())
    expect(document.querySelector('[data-rule-save-error]')).toBeNull()
    expect(wrapper.emitted('update:open')).toEqual([[false]])
  })

  it('leaves automatic URL format detection to the server instead of forcing Text', async () => {
    await render({ initialSource: 'new_library' })
    await type('#rule-library-name', 'YAML domains')
    await type('#rule-library-url', 'https://rules.test/domains.yaml')
    await click(save())
    expect(network.compose.mock.calls[0]![0].source.library).toEqual({
      name: 'YAML domains', source_type: 'url', url: 'https://rules.test/domains.yaml', behavior: 'domain', enabled: true
    })
  })

  it('lets users declare YAML for a txt URL after a format error without losing the source', async () => {
    network.compose.mockRejectedValueOnce({ isAxiosError: true, response: { data: { message: '内容是 YAML payload，请选择 YAML 格式。' } } })
    const wrapper = await render({ initialSource: 'new_library' })
    await type('#rule-library-name', 'YAML in txt')
    await type('#rule-library-url', 'https://rules.test/direct.txt')
    await click(save())
    expect(text()).toContain('内容是 YAML payload，请选择 YAML 格式。')
    await choose(wrapper, '内容格式', 'yaml')
    await click(save())
    expect(network.compose.mock.calls.at(-1)![0].source.library).toEqual({
      name: 'YAML in txt', source_type: 'url', url: 'https://rules.test/direct.txt', behavior: 'domain', format: 'yaml', enabled: true
    })
    expect(document.querySelector('[data-rule-save-error]')).toBeNull()
  })

  it('disables saving when nothing can be added', async () => {
    await render()
    expect(save().disabled).toBe(true)
    expect(text()).toContain('没有可添加的规则')
    await type('#rule-values', 'kept.test')
    expect(save().disabled).toBe(true)
    await type('#rule-values', 'kept.test\nnew.test')
    expect(save().disabled).toBe(false)
  })

  it('asks before discarding changes when closing', async () => {
    const wrapper = await render()
    await click(button('取消'))
    expect(wrapper.emitted('update:open')).toEqual([[false]])
    await type('#rule-values', 'draft.test')
    await click(button('取消'))
    expect(confirmState.description).toBe('放弃未保存的修改？')
    settleConfirm('cancel')
    await flushPromises()
    expect(wrapper.emitted('update:open')).toHaveLength(1)
    await click(button('取消'))
    settleConfirm('confirm')
    await flushPromises()
    expect(wrapper.emitted('update:open')).toEqual([[false], [false]])
  })

  it('merges compose warnings into one notification with an enable action for a disabled duplicate', async () => {
    network.compose.mockResolvedValueOnce(composeResult({
      skipped: [{ line: 2, value: 'old.test', existing_id: 'rule-b', index: 2, enabled: false }],
      warnings: ['忽略第 3 行行尾策略 Proxy']
    }))
    const wrapper = await render()
    await type('#rule-values', 'openai.com\nold.test\nDOMAIN,x.test,Proxy')
    await click(save())
    expect(notify.warning).toHaveBeenCalledTimes(1)
    const [message, , action] = vi.mocked(notify.warning).mock.calls[0]!
    expect(message).toBe('已添加 1 条；跳过 1 条重复（#2）；忽略 1 处行尾策略')
    expect(action?.label).toBe('启用 #2')
    action!.onClick()
    expect(wrapper.emitted('enable')).toEqual([['rule-b']])
  })

  it('reports where the change applies after saving', async () => {
    await render({ dns: snapshot({ agents: [
      { id: 'a1', name: 'Main', service_type: 'oxidns', auto_sync: { rules: true, library: false } },
      { id: 'a2', name: 'Edge', service_type: 'mihomo', auto_sync: { rules: false, library: false } }
    ] }) })
    await type('#rule-values', 'openai.com')
    await click(save())
    const [message, description, action] = vi.mocked(notify.success).mock.calls[0]!
    expect(message).toBe('已添加 1 条')
    expect(description).toBe('1 台已开启规则自动同步，将自动更新；1 台需发布后生效')
    expect(action?.label).toBe('去发布')
  })

  it('sends a compose request for library sources with the chosen DNS', async () => {
    await render({ initialSource: 'library' })
    ;(wrappers[0]!.vm as any).$.setupState.form.libraryIds = ['lib-b']
    await flushPromises()
    await pickDns('不存在')
    await click(save())
    expect(network.compose).toHaveBeenCalledWith({
      source: { kind: 'library', library_rule_ids: ['lib-b'] },
      rule: { policy: 'DIRECT', remark: '', group_name: '', no_resolve: false, enabled: true, position: 'top' },
      dns: 'nxdomain'
    }, 'p1')
  })

  it('edits rulesets through the reference update with dns only when it changed', async () => {
    const item = existing[2]!
    const wrapper = await render({ mode: 'edit', item, dns: snapshot({}, { rules: { 'set-a': { state: 'explicit', route: 'nxdomain' } } }) })
    expect(dialog().querySelector('[role="radiogroup"][aria-label="匹配来源"]')).toBeNull()
    expect(button('编辑来源')).toBeTruthy()
    expect(dialog().querySelector('a[href="/rule-library"]')).toBeNull()
    await choose(wrapper, '策略', 'Proxy')
    await click(save())
    const [id, payload] = network.setUpdate.mock.calls[0]!
    expect(id).toBe('set-a')
    expect(payload).toMatchObject({ itemType: 'ruleset', library_rule_id: 'lib-a', policy: 'Proxy', group_name: '' })
    expect(payload).not.toHaveProperty('dns')
    expect(payload).not.toHaveProperty('name')
  })

  it('previews a group follow choice from the DNS snapshot and updates when that route changes', async () => {
    const wrapper = await render({ dns: snapshot({ policy_routes: { Proxy: 'direct' } }) })
    await type('#rule-values', 'apple.com')
    await choose(wrapper, '策略', 'Proxy')
    expect(dnsTrigger().textContent).toContain('跟随策略 · 直连 DNS')
    await wrapper.setProps({ dns: snapshot({ policy_routes: { Proxy: 'proxy' } }) })
    await flushPromises()
    expect(dnsTrigger().textContent).toContain('跟随策略 · 代理 DNS')
    await click(save())
    expect(network.compose.mock.calls[0]![0]).toMatchObject({ rule: { policy: 'Proxy' }, dns: 'follow' })
  })

  it('edits the shared source in the same dialog and preserves unsaved reference settings', async () => {
    const wrapper = await render({ mode: 'edit', item: existing[2], dns: snapshot({}, { rules: { 'set-a': { state: 'follow', route: 'proxy', follow_route: 'proxy' } } }) })
    const originalDialog = dialog()
    await choose(wrapper, '策略', 'Proxy')
    await pickDns('黑洞')
    await type('#rule-remark', 'Keep this draft')
    await click(button('编辑来源'))
    expect(dialog()).toBe(originalDialog)
    expect(text()).toContain('编辑规则集来源')
    expect(text()).toContain('所有引用此规则集的配置')
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    await type('#source-name', 'Updated Ads')
    await type('#source-url', 'https://rules.test/new.list')
    await click(button('保存来源'))
    expect(network.put).toHaveBeenCalledExactlyOnceWith('/rule-library/lib-a', expect.objectContaining({ name: 'Updated Ads', url: 'https://rules.test/new.list' }))
    expect(network.setUpdate).not.toHaveBeenCalled()
    expect(wrapper.emitted('source-saved')?.[0]?.[0]).toMatchObject({ id: 'lib-a', name: 'Updated Ads' })
    expect(wrapper.emitted('update:open')).toBeUndefined()
    expect(dialog()).toBe(originalDialog)
    expect(text()).toContain('Updated Ads')
    expect(dialog().querySelector<HTMLInputElement>('#rule-remark')!.value).toBe('Keep this draft')
    expect(document.activeElement).toBe(button('编辑来源'))
    await wrapper.setProps({ dns: null, dnsLoading: true })
    expect(save().disabled).toBe(true)
    await click(save())
    expect(network.setUpdate).not.toHaveBeenCalled()
    await wrapper.setProps({
      library: [{ ...library[0]!, name: 'Updated Ads', url: 'https://rules.test/new.list' }, library[1]!],
      dns: snapshot({}, { rules: { 'set-a': { state: 'follow', route: 'proxy', follow_route: 'proxy' } } }), dnsLoading: false
    })
    await click(save())
    expect(network.setUpdate).toHaveBeenCalledWith('set-a', expect.objectContaining({ policy: 'Proxy', dns: 'blackhole', remark: 'Keep this draft' }), 'p1')
  })

  it('keeps a DNS draft blocked after refresh failure and offers a retry', async () => {
    const wrapper = await render({ mode: 'edit', item: existing[2] })
    await pickDns('黑洞')
    await click(button('编辑来源'))
    await type('#source-name', 'Updated source')
    await click(button('保存来源'))
    await wrapper.setProps({ dns: null, dnsLoading: false })
    expect(save().disabled).toBe(true)
    expect(text()).toContain('已保留你的选择')
    expect(dnsTrigger().textContent).toContain('黑洞')
    expect(text()).not.toContain('本次不修改 DNS')
    await click(button('重新读取 DNS'))
    expect(wrapper.emitted('reload-dns')).toHaveLength(1)
    expect(network.setUpdate).not.toHaveBeenCalled()
    await wrapper.setProps({ dns: snapshot(), dnsLoading: false })
    await click(save())
    expect(network.setUpdate).toHaveBeenCalledWith('set-a', expect.objectContaining({ dns: 'blackhole' }), 'p1')
  })

  it('asks before discarding source edits and returns without discarding the reference draft', async () => {
    const wrapper = await render({ mode: 'edit', item: existing[2] })
    await choose(wrapper, '策略', 'Proxy')
    await click(button('编辑来源'))
    await type('#source-url', 'https://rules.test/unsaved.list')
    await click(button('返回规则设置'))
    expect(confirmState.description).toBe('放弃未保存的来源修改？')
    settleConfirm('cancel')
    await flushPromises()
    expect(document.querySelector<HTMLInputElement>('#source-url')!.value).toBe('https://rules.test/unsaved.list')
    await click(button('返回规则设置'))
    settleConfirm('confirm')
    await flushPromises()
    expect(network.put).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:open')).toBeUndefined()
    expect(text()).toContain('https://rules.test/ads.list')
    await click(save())
    expect(network.setUpdate).toHaveBeenCalledWith('set-a', expect.objectContaining({ policy: 'Proxy' }), 'p1')
  })

  it('protects source drafts when closing and blocks leaving during a source save', async () => {
    const wrapper = await render({ mode: 'edit', item: existing[2] })
    await click(button('编辑来源'))
    await type('#source-name', 'New name')
    await click(dialog().querySelector('[aria-label="关闭"]'))
    expect(confirmState.description).toBe('放弃未保存的修改？')
    settleConfirm('cancel')
    await flushPromises()
    let finish!: (value: { data: { success: boolean } }) => void
    network.put.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await click(button('保存来源'))
    expect(dialog().querySelector('[aria-label="关闭"]')?.hasAttribute('disabled')).toBe(true)
    expect(button('返回规则设置')?.disabled).toBe(true)
    await click(button('返回规则设置'))
    expect(wrapper.emitted('update:open')).toBeUndefined()
    finish({ data: { success: true } })
    await flushPromises()
    expect(button('编辑来源')).toBeTruthy()
  })

  it('edits a newly selected shared source without prematurely changing the reference', async () => {
    const wrapper = await render({ mode: 'edit', item: existing[2] })
    await click(dialog().querySelector('[aria-label="选择规则集"]'))
    await click(Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find(item => item.textContent?.includes('Corp')))
    await click(button('编辑来源'))
    expect(document.querySelector<HTMLTextAreaElement>('#source-content')!.value).toBe('DOMAIN,corp.lan')
    await type('#source-content', 'DOMAIN,updated.lan')
    await click(button('保存来源'))
    expect(network.put).toHaveBeenCalledWith('/rule-library/lib-b', expect.objectContaining({ content: 'DOMAIN,updated.lan' }))
    expect(network.setUpdate).not.toHaveBeenCalled()
    await click(save())
    expect(network.setUpdate).toHaveBeenCalledWith('set-a', expect.objectContaining({ library_rule_id: 'lib-b' }), 'p1')
  })

  it('offers to update a drifted follow rule to its policy', async () => {
    const item = { id: 'rule-d', itemType: 'rule', rule_type: 'DOMAIN-SUFFIX', value: 'd.test', policy: 'DIRECT', enabled: true }
    await render({ mode: 'edit', item, dns: snapshot({}, { rules: { 'rule-d': { state: 'follow_drift', route: 'proxy', follow_route: 'direct' } } }) })
    expect(text()).toContain('策略已是 DIRECT，DNS 仍为代理 DNS（与策略不一致）')
    await click(button('按策略更新 DNS'))
    await click(save())
    expect(network.update.mock.calls[0]![1]).toMatchObject({ dns: 'follow' })
  })

  it('uses a full-screen layout on phones with the footer outside the scrolling body', async () => {
    await render()
    expect(dialog().classList.contains('rule-editor-dialog')).toBe(true)
    // The scroll container is a div: Chromium does not clip a flex-sized fieldset, so it never scrolled.
    expect(dialog().querySelector(':scope > div.rule-editor-body > fieldset.rule-editor-fields')).not.toBeNull()
    expect(dialog().querySelector('fieldset.rule-editor-body')).toBeNull()
    expect(dialog().querySelector(':scope > footer.rule-editor-footer')).not.toBeNull()
  })

  it('passes the DNS of new rules returned by compose to the page', async () => {
    network.compose.mockResolvedValueOnce(composeResult({ dns: { 'rule-new': { state: 'explicit', route: 'blackhole' } } }))
    const wrapper = await render()
    await type('#rule-values', 'openai.com')
    await pickDns('黑洞')
    await click(save())
    expect(wrapper.emitted('saved')).toEqual([[{ ids: ['rule-new'], dns: { 'rule-new': { state: 'explicit', route: 'blackhole' } } }]])
  })

  it('offers the UDP fallback and NTP switch only on MATCH and clears them when the type changes', async () => {
    const item = { id: 'final', itemType: 'rule', rule_type: 'MATCH', value: '', policy: 'Proxy', enabled: true, udp_fallback: 'DIRECT' }
    let wrapper = await render({ mode: 'edit', item })
    expect(text()).toContain('节点不支持 UDP 时')
    expect(dialog().querySelector('[role="radio"][aria-checked="true"]')!.textContent?.trim()).toBe('直连')
    await source('拒绝')
    expect(text()).toContain('建议同时开启下方「对时（NTP）始终直连」')
    await click(document.querySelector('#rule-ntp-direct'))
    expect(text()).not.toContain('建议同时开启下方「对时（NTP）始终直连」')
    await click(save())
    expect(network.update).toHaveBeenLastCalledWith('final', expect.objectContaining({ udp_fallback: 'REJECT', ntp_direct: true }), 'p1')

    wrappers.splice(0).forEach(entry => entry.unmount())
    document.body.innerHTML = ''
    wrapper = await render({ mode: 'edit', item })
    await choose(wrapper, '规则类型', 'DOMAIN')
    expect(text()).not.toContain('节点不支持 UDP 时')
    await type('#rule-value', 'x.test')
    await click(save())
    expect(document.querySelector('#rule-ntp-direct')).toBeNull()
    expect(network.update).toHaveBeenLastCalledWith('final', expect.objectContaining({ rule_type: 'DOMAIN', udp_fallback: '', ntp_direct: false }), 'p1')

    wrappers.splice(0).forEach(entry => entry.unmount())
    document.body.innerHTML = ''
    wrapper = await render()
    await choose(wrapper, '规则类型', 'MATCH')
    await source('拒绝')
    await click(save())
    expect(network.create).toHaveBeenCalledWith(expect.objectContaining({ rule_type: 'MATCH', udp_fallback: 'REJECT' }), 'p1')
  })

  it('keeps a group name typed together with a policy change, and clears the old one otherwise', async () => {
    const item = { id: 'rule-g', itemType: 'rule', rule_type: 'DOMAIN-SUFFIX', value: 'g.test', policy: 'DIRECT', enabled: true, group_name: '国内' }
    let wrapper = await render({ mode: 'edit', item })
    await choose(wrapper, '策略', 'Proxy')
    await type('#rule-group', '国外')
    await click(save())
    expect(network.update.mock.calls[0]![1]).toMatchObject({ policy: 'Proxy', group_name: '国外' })

    wrappers.splice(0).forEach(entry => entry.unmount())
    document.body.innerHTML = ''
    wrapper = await render({ mode: 'edit', item })
    await choose(wrapper, '策略', 'Proxy')
    await click(save())
    expect(network.update.mock.calls[1]![1]).toMatchObject({ policy: 'Proxy', group_name: '' })
  })

  it.each(['rule', 'ruleset'] as const)('clears an unchanged padded group name when only the %s policy changes', async itemType => {
    const item = { ...existing[itemType === 'rule' ? 0 : 2]!, policy: 'DIRECT', group_name: '国内  ' }
    const wrapper = await render({ mode: 'edit', item })
    expect(dialog().querySelector<HTMLInputElement>('#rule-group')!.value).toBe('国内  ')
    await choose(wrapper, '策略', 'Proxy')
    await click(save())
    const update = itemType === 'rule' ? network.update : network.setUpdate
    expect(update).toHaveBeenCalledExactlyOnceWith(item.id, expect.objectContaining({ policy: 'Proxy', group_name: '' }), 'p1')
    expect(item.group_name).toBe('国内  ')
    expect(wrapper.emitted('saved')).toEqual([[{ ids: [item.id] }]])
  })

  it('does not call a never-configured default upstream a separate choice, but does for a saved one', async () => {
    const item = { id: 'rule-l', itemType: 'rule', rule_type: 'DOMAIN-SUFFIX', value: 'baidu.com', policy: 'DIRECT', enabled: true }
    await render({ mode: 'edit', item })
    expect(dnsTrigger().textContent).toContain('默认上游')
    expect(text()).not.toContain('单独设置')

    wrappers.splice(0).forEach(entry => entry.unmount())
    document.body.innerHTML = ''
    await render({ mode: 'edit', item, dns: snapshot({}, { rules: { 'rule-l': { state: 'explicit', route: 'proxy' } } }) })
    expect(text()).toContain('DNS 为单独设置的代理 DNS，不随策略变化')
  })

  it('swaps the library ruleset of a reference in place, keeping current and used entries apart', async () => {
    const library2 = [...library, { id: 'lib-c', name: 'Nets', behavior: 'ipcidr', source_type: 'url', url: 'https://rules.test/nets.list', enabled: true },
      { id: 'lib-d', name: 'Old', behavior: 'domain', source_type: 'url', url: 'https://rules.test/old.list', enabled: false }]
    const rules = [...existing, { id: 'set-b', itemType: 'ruleset', library_rule_id: 'lib-b', name: 'Corp', behavior: 'classical', policy: 'DIRECT', enabled: true }]
    await render({ mode: 'edit', item: existing[2], rules, library: library2, dns: snapshot({}, { rules: { 'set-a': { state: 'explicit', route: 'nxdomain' } } }) })
    await click(dialog().querySelector('button[aria-label="选择规则集"]'))
    const option = (name: string) => Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find(item => item.textContent?.includes(name))!
    expect(option('Ads').hasAttribute('data-disabled')).toBe(false)
    expect(option('Corp').hasAttribute('data-disabled')).toBe(true) // already referenced as #4
    expect(option('Old').hasAttribute('data-disabled')).toBe(true)
    await click(option('Nets'))
    expect(dialog().querySelector('button[aria-label="选择规则集"]')!.textContent).toContain('Nets')
    expect(dialog().querySelector<HTMLButtonElement>('#rule-noresolve')!.getAttribute('aria-checked')).toBe('true')
    await click(save())
    const [id, payload] = network.setUpdate.mock.calls[0]!
    expect(id).toBe('set-a')
    // Switching a domain source to pure IP clears its former DNS override.
    expect(payload).toMatchObject({ library_rule_id: 'lib-c', no_resolve: true, policy: 'REJECT', dns: 'default' })
  })

  it('compares with MosDNS only when a MosDNS device is bound or the rule is in a MosDNS list', async () => {
    // Other rules sit in MosDNS lists, but this new rule is in none and no MosDNS device is bound.
    await render({ dns: snapshot({}, { mosdns: { 'rule-a': 'direct' } }) })
    await type('#rule-values', 'openai.com')
    await pickDns('代理 DNS')
    expect(text()).not.toContain('MosDNS')

    wrappers.splice(0).forEach(entry => entry.unmount())
    document.body.innerHTML = ''
    const item = { id: 'rule-n', itemType: 'rule', rule_type: 'DOMAIN-SUFFIX', value: 'n.test', policy: 'DIRECT', enabled: true }
    await render({ mode: 'edit', item, dns: snapshot({}, { rules: { 'rule-n': { state: 'explicit', route: 'proxy' } }, mosdns: { 'rule-a': 'direct' } }) })
    expect(text()).not.toContain('MosDNS')

    wrappers.splice(0).forEach(entry => entry.unmount())
    document.body.innerHTML = ''
    await render({ dns: snapshot({ agents: [{ id: 'm', name: 'M', service_type: 'mosdns', auto_sync: { rules: false, library: false } }] }) })
    await type('#rule-values', 'openai.com')
    await pickDns('代理 DNS')
    expect(text()).toContain('MosDNS 仍按默认上游处理此规则，与这里的「代理 DNS」不同')
  })

  it('asks to publish a pinned upstream only when the server says auto-sync withheld it', async () => {
    const agents = [{ id: 'a1', name: 'Main', service_type: 'oxidns', auto_sync: { rules: true, library: false } }]
    network.compose.mockResolvedValueOnce(composeResult({ pinned_withheld: false }))
    await render({ dns: snapshot({ agents }) })
    await type('#rule-values', 'openai.com')
    await pickDns('直连 · 223.5.5.5')
    await click(save())
    expect(vi.mocked(notify.success).mock.calls[0]).toEqual(['已添加 1 条', '1 台已开启规则自动同步，将自动更新', undefined])

    wrappers.splice(0).forEach(entry => entry.unmount())
    document.body.innerHTML = ''
    network.compose.mockResolvedValueOnce(composeResult({ pinned_withheld: true }))
    await render({ dns: snapshot({ agents }) })
    await type('#rule-values', 'openai.com')
    await pickDns('直连 · 223.5.5.5')
    await click(save())
    const [, description, action] = vi.mocked(notify.success).mock.calls[1]!
    expect(description).toBe('1 台已开启规则自动同步，将自动更新；指定上游需手动发布')
    expect(action?.label).toBe('去发布')
  })

  it('moves the DNS field into more options when the profile does not use OxiDNS', async () => {
    await render({ dns: snapshot({ oxidns_in_use: false }) })
    await type('#rule-values', 'openai.com')
    const more = dialog().querySelector('details[data-rule-more]')!
    expect(more.querySelector('#rule-dns')).not.toBeNull()
    expect(more.textContent).toContain('当前配置空间未使用 OxiDNS')
    expect(dnsTrigger().textContent).toContain('默认上游')
    await click(save())
    expect(network.compose.mock.calls[0]![0]).not.toHaveProperty('dns')
  })
})
