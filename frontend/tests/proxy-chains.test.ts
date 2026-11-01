import { beforeAll, beforeEach, afterEach, it, expect, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { AxiosHeaders, type AxiosResponse } from 'axios'
import type { Component } from 'vue'
import type { ProxyNode, ProxyGroup } from '@/types'
import { createMemoryHistory, createRouter } from 'vue-router'
import ProxyGroups from '@/views/ProxyGroups.vue'
import Rules from '@/views/Rules.vue'
import Nodes from '@/views/Nodes.vue'
import ConfirmHost from '@/components/feedback/ConfirmHost.vue'
import api, { nodeApi, proxyGroupApi, ruleApi } from '@/api'
import { notify, settleConfirm } from '@/lib/feedback'
import { setActiveProfileId } from '@/profileContext'

// Only the network seam is mocked; forms, selects, dialogs and confirmations are real.
vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  nodeApi: { getAll: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn(), previewDeletion: vi.fn(), deleteWithReferences: vi.fn() },
  proxyGroupApi: { getAll: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
  ruleApi: { getAll: vi.fn(), create: vi.fn(), update: vi.fn(), compose: vi.fn(), getDns: vi.fn() },
  ruleSetApi: { getAll: vi.fn() },
  profileApi: { list: vi.fn() }
}))
let rows: ProxyNode[], groups: ProxyGroup[]
let wrapper: VueWrapper | undefined, host: VueWrapper | undefined
const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value))
function response<T>(data: T): AxiosResponse<T> {
  return { data, status: 200, statusText: 'OK', headers: new AxiosHeaders(), config: { headers: new AxiosHeaders() } }
}
const chain = (entry: NonNullable<ProxyGroup['chain']>['entry'] = { type: 'node', id: 'relay' }): ProxyGroup => ({
  id: 'chain', name: 'Via relay', type: 'chain', enabled: true, chain: { entry, exit: { type: 'node', id: 'exit' } }
})
beforeAll(() => {
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })) })
  Object.defineProperty(window, 'ResizeObserver', { configurable: true, value: class { observe() {} unobserve() {} disconnect() {} } })
})
beforeEach(() => {
  vi.resetAllMocks()
  sessionStorage.clear()
  setActiveProfileId('default')
  rows = [
    { id: 'exit', name: 'Exit', enabled: true, proxy_string: '{"type":"http","server":"example.test","port":80,"dialer-proxy":"DIRECT"}' },
    { id: 'relay', name: 'Relay', enabled: true, proxy_string: 'http://example.test:80' },
    { id: 'disabled', name: 'Disabled', enabled: false, proxy_string: '' },
    { id: 'shared', name: 'Shared', enabled: true, proxy_string: '' }
  ]
  groups = [
    { id: 'static', name: 'Static', type: 'select', enabled: true, manual_nodes: ['relay'] },
    { id: 'dynamic', name: 'Dynamic', type: 'select', enabled: true, subscriptions: ['sub'] }
  ]
  vi.mocked(nodeApi.getAll).mockImplementation(async () => response(clone(rows)))
  vi.mocked(api.get).mockImplementation(async path => response(path === '/subscriptions' ? [{ id: 'sub', name: 'Subscription' }] : path === '/aggregations' ? [{ id: 'agg', name: 'Aggregation' }] : []))
  vi.mocked(proxyGroupApi.getAll).mockImplementation(async () => response(clone(groups)))
  vi.mocked(proxyGroupApi.create).mockImplementation(async data => { groups.push(clone(data as ProxyGroup)); return response({}) })
  vi.mocked(proxyGroupApi.update).mockImplementation(async (id, data) => { groups = groups.map(group => group.id === id ? clone(data as ProxyGroup) : group); return response({}) })
  vi.mocked(proxyGroupApi.delete).mockImplementation(async id => { groups = groups.filter(group => group.id !== id); return response({}) })
  vi.mocked(ruleApi.getAll).mockResolvedValue(response([]))
  vi.mocked(ruleApi.create).mockResolvedValue(response({}))
  vi.mocked(ruleApi.compose).mockResolvedValue(response({ success: true, data: { rules: [{ id: 'rule-new' }], library: null }, dns: {}, skipped: [], warnings: [], draft: true }) as any)
  vi.mocked(ruleApi.getDns).mockRejectedValue(new Error('not used here'))
  vi.mocked(nodeApi.update).mockResolvedValue(response({}))
  vi.spyOn(notify, 'error').mockImplementation(() => 0)
  vi.spyOn(notify, 'warning').mockImplementation(() => 0)
  vi.spyOn(notify, 'success').mockImplementation(() => 0)
})
afterEach(() => {
  wrapper?.unmount()
  host?.unmount()
  wrapper = host = undefined
  settleConfirm('cancel')
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})
async function render(component: Component = ProxyGroups) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component }] })
  await router.push('/')
  await router.isReady()
  wrapper = mount(component, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return wrapper
}
async function click(selector: string) {
  const element = document.querySelector(selector) as HTMLElement
  expect(element, selector).toBeTruthy()
  element.click()
  await flushPromises()
}
async function button(text: string, root: ParentNode = document) {
  const element = [...root.querySelectorAll('button')].find(element => element.textContent?.trim() === text)
  expect(element, text).toBeTruthy()
  element!.click()
  await flushPromises()
}
async function fill(selector: string, value: string) {
  const input = document.querySelector(selector) as HTMLInputElement
  expect(input, selector).toBeTruthy()
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function options(selector: string) {
  const trigger = document.querySelector(selector)!
  expect(trigger, selector).toBeTruthy()
  trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  await flushPromises()
  return [...document.querySelectorAll('[role="option"]')]
}
async function selectOption(option: Element | undefined) {
  expect(option).toBeTruthy()
  ;(option as HTMLElement).focus()
  option!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await flushPromises()
}
async function select(selector: string, label: string) {
  await selectOption((await options(selector)).find(option => option.textContent?.trim() === label))
}
it('group health clearing sends explicit delete_fields, not type-as-replacement', async () => {
  groups = [{id:'health',name:'Health',type:'url-test',manual_nodes:['DIRECT'],timeout:8250,lazy:false,interval:77}]
  await render()
  await edit('Health')
  await fill('[data-health="timeout"]','')
  const lazy=document.querySelector('[data-health="lazy"]') as HTMLSelectElement
  lazy.value='unset';lazy.dispatchEvent(new Event('change',{bubbles:true}));await flushPromises()
  await button('保存')
  const payload=vi.mocked(proxyGroupApi.update).mock.calls[0]?.[1] as any
  expect(payload.delete_fields).toEqual(expect.arrayContaining(['timeout','lazy']))
  expect(payload.interval).toBe(77)
})

async function closeOptions() {
  const option = document.querySelector('[role="option"]')
  option?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await flushPromises()
}
async function edit(name = 'Via relay') {
  await button('编辑', document.querySelector(`[data-name="${name}"]`)!)
}
async function addChain() {
  await button('添加策略组')
  await fill('#group-name', 'New chain')
  await select('[data-testid="group-type"]', '代理链（仅 Mihomo）')
}
async function source(label: string) {
  const element = [...document.querySelectorAll('[role="dialog"] label')].find(element => element.textContent?.trim() === label)
  expect(element, label).toBeTruthy()
  ;(element!.querySelector('[role="checkbox"]') as HTMLElement).click()
  await flushPromises()
}

it('reopens automatic group health fields without losing false or milliseconds', async () => {
  groups = [{ id: 'auto', name: 'Auto', type: 'url-test', enabled: true, manual_nodes: ['DIRECT'], url: 'https://example.test/204', interval: 61, lazy: false, timeout: 7250, 'expected-status': '204', tolerance: 51 } as ProxyGroup]
  await render()
  await edit('Auto')
  expect(document.querySelector('[data-health-advanced]')).toBeTruthy()
  expect((document.querySelector('[data-health="timeout"]') as HTMLInputElement).value).toBe('7.25')
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('auto', expect.objectContaining({ lazy: false, timeout: 7250, tolerance: 51 }), 'default')
})

it('creates an independent named exit from the shared catalog without writing the original node', async () => {
  const original = clone(rows)
  await render()
  await addChain()
  await select('[data-testid="chain-exit"]', '节点 · Exit')
  const choices = await options('[data-testid="chain-entry"]')
  expect(choices.map(option => option.textContent?.trim())).toEqual(['节点 · Relay', '节点 · Shared', '策略组 · Static', '策略组 · Dynamic'])
  await selectOption(choices[1])
  await button('保存')
  expect(proxyGroupApi.create).toHaveBeenCalledWith({
    id: expect.any(String), name: 'New chain', type: 'chain', enabled: true,
    chain: { entry: { type: 'node', id: 'shared' }, exit: { type: 'node', id: 'exit' } }
  }, 'default')
  expect(document.querySelector('[data-name="New chain"]')).not.toBeNull()
  expect(nodeApi.update).not.toHaveBeenCalled()
  expect(rows).toEqual(original)
  expect(vi.mocked(api.get).mock.calls.map(call => call[0])).toEqual(['/subscriptions', '/aggregations', '/proxy-groups/health-defaults'])
})

it.each<['node' | 'group', string]>([['node', 'relay'], ['group', 'static']])('hydrates %s references by ID, isolates canceled edits, and saves to the mounted profile', async (type, id) => {
  groups.push(chain({ type, id }))
  setActiveProfileId('profile-a')
  await render()
  const original = clone(groups)
  await edit()
  const label = type === 'node' ? '节点 · Relay' : '策略组 · Static'
  expect(document.querySelector('[data-testid="chain-entry"]')?.textContent?.trim()).toBe(label)
  const selected = (await options('[data-testid="chain-entry"]')).find(option => option.textContent?.trim() === label)
  expect(selected?.getAttribute('aria-selected')).toBe('true')
  await closeOptions()
  await select('[data-testid="chain-entry"]', '节点 · Shared')
  await fill('#group-name', 'Canceled name')
  await button('取消')
  expect(proxyGroupApi.update).not.toHaveBeenCalled()
  expect(groups).toEqual(original)
  await edit()
  expect(document.querySelector('[data-testid="chain-entry"]')?.textContent?.trim()).toBe(label)
  await fill('#chain-search', 'No matches')
  expect(document.querySelector('[data-testid="chain-entry"]')?.textContent?.trim()).toBe(label)
  setActiveProfileId('profile-b')
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('chain', chain({ type, id }), 'profile-a')
  expect(nodeApi.update).not.toHaveBeenCalled()
})

it('resolves renamed long shared labels without changing stable IDs', async () => {
  const name = 'Renamed relay with a very long name '.repeat(8)
  rows[1].name = name
  groups.push(chain())
  await render()
  await edit()
  expect(document.querySelector('[data-testid="chain-entry"]')?.textContent).toContain(name)
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('chain', chain(), 'default')
})

it.each([
  { id: 'missing', enabled: true }, { id: 'disabled', enabled: true },
  { id: 'missing', enabled: false }, { id: 'disabled', enabled: false }
])('rejects an unavailable $id entry even when chain enabled=$enabled', async ({ id, enabled }) => {
  groups.push({ ...chain({ type: 'node', id }), enabled })
  await render()
  await edit()
  expect(document.querySelector('[data-testid="chain-entry"]')?.textContent).toContain(id === 'disabled' ? 'Disabled' : 'missing')
  expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull()
  await button('保存')
  expect(proxyGroupApi.update).not.toHaveBeenCalled()
  await select('[data-testid="chain-entry"]', '节点 · Relay')
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('chain', { ...chain(), enabled }, 'default')
})

it('renames a disabled chain with a disabled manual exit without enabling or replacing either', async () => {
  rows[0].enabled = false
  groups.push({ ...chain(), enabled: false })
  const originalNodes = clone(rows)
  await render()
  await edit()
  const selected = (await options('[data-testid="chain-exit"]')).find(option => option.textContent?.trim() === '节点 · Exit')
  expect(selected?.getAttribute('aria-selected')).toBe('true')
  await closeOptions()
  expect(document.querySelector('[role="dialog"] [role="alert"]')).toBeNull()
  await fill('#group-name', 'Inactive renamed')
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('chain', { ...chain(), name: 'Inactive renamed', enabled: false }, 'default')
  expect(document.querySelector('[data-name="Inactive renamed"]')).not.toBeNull()
  expect(nodeApi.update).not.toHaveBeenCalled()
  expect(rows).toEqual(originalNodes)
})

it('requires an enabled exit when re-enabling a chain and preserves the draft until repaired', async () => {
  rows[0].enabled = false
  groups.push({ ...chain(), enabled: false })
  await render()
  await edit()
  await click('#group-enabled')
  await button('保存')
  expect(proxyGroupApi.update).not.toHaveBeenCalled()
  expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull()
  expect(document.querySelector('[data-testid="chain-exit"]')?.textContent?.trim()).toBe('节点 · Exit')
  const choices = await options('[data-testid="chain-exit"]')
  expect(choices.map(option => option.textContent?.trim())).not.toContain('节点 · Exit')
  await selectOption(choices.find(option => option.textContent?.trim() === '节点 · Shared'))
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('chain', {
    ...chain(), chain: { entry: { type: 'node', id: 'relay' }, exit: { type: 'node', id: 'shared' } }
  }, 'default')
  expect(rows[0].enabled).toBe(false)
})

it.each(['missing', 'subscription'])('still rejects a %s exit on a disabled chain', async kind => {
  const saved = { ...chain(), enabled: false }
  if (kind === 'missing') saved.chain!.exit.id = 'missing'
  else rows[0].subscription_id = 'sub'
  groups.push(saved)
  await render()
  await edit()
  await fill('#group-name', 'Invalid exit')
  await button('保存')
  expect(proxyGroupApi.update).not.toHaveBeenCalled()
  expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull()
})

it('allows dynamic and follow groups and nested chains but excludes disabled, cyclic, self and subscription-node entries', async () => {
  rows.push({ id: 'subscription-node', name: 'Fetched', enabled: true, subscription_id: 'sub', proxy_string: '' })
  groups.push(
    { id: 'nested', name: 'Nested dynamic', type: 'select', enabled: true, include_groups: ['dynamic'] },
    { id: 'off', name: 'Off', type: 'select', enabled: false, manual_nodes: ['relay'] },
    { id: 'bad-node', name: 'Bad node', type: 'select', enabled: true, manual_nodes: ['disabled'] },
    { id: 'cycle', name: 'Cycle', type: 'select', enabled: true, include_groups: ['cycle'] },
    { id: 'follow', name: 'Follow', type: 'select', enabled: true, follow_group: 'static' },
    { ...chain(), id: 'other-chain', name: 'Other chain' },
    chain()
  )
  await render()
  await edit()
  const choices = await options('[data-testid="chain-entry"]')
  expect(choices.map(option => option.textContent?.trim())).toEqual(['节点 · Relay', '节点 · Shared', '策略组 · Static', '策略组 · Dynamic', '策略组 · Nested dynamic', '策略组 · Follow', '策略组 · Other chain'])
  await selectOption(choices.find(option => option.textContent?.trim() === '策略组 · Other chain'))
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('chain', chain({ type: 'group', id: 'other-chain' }), 'default')
})

it.each(['dynamic', 'aggregation', 'follow', 'nested-chain'])('creates searchable group endpoints using %s sources without changing the original group', async id => {
  groups.push(
    { id: 'aggregation', name: 'Aggregation group', type: 'url-test', enabled: true, aggregations: ['agg'] },
    { id: 'follow', name: 'Follow group', type: 'fallback', enabled: true, follow_group: 'dynamic' },
    { ...chain(), id: 'nested-chain', name: 'Nested chain' }
  )
  const original = clone(groups)
  const target = groups.find(group => group.id === id)!
  await render()
  await addChain()
  await fill('#chain-search', target.name.toLowerCase())
  await select('[data-testid="chain-entry"]', `策略组 · ${target.name}`)
  await fill('#chain-search', 'Static')
  await select('[data-testid="chain-exit"]', '策略组 · Static')
  await button('保存')
  expect(proxyGroupApi.create).toHaveBeenCalledWith(expect.objectContaining({
    chain: { entry: { type: 'group', id }, exit: { type: 'group', id: 'static' } }
  }), 'default')
  expect(groups.slice(0, original.length)).toEqual(original)
  await edit('New chain')
  await fill('#chain-search', target.name)
  await select('[data-testid="chain-exit"]', `策略组 · ${target.name}`)
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({
    chain: { entry: { type: 'group', id }, exit: { type: 'group', id } }
  }), 'default')
})

it('retains a disabled group exit until explicitly repaired when enabling the chain', async () => {
  groups[1].enabled = false
  const saved = { ...chain(), enabled: false }
  saved.chain!.exit = { type: 'group', id: 'dynamic' }
  groups.push(saved)
  await render()
  await edit()
  await fill('#group-name', 'Inactive group chain')
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenLastCalledWith('chain', { ...saved, name: 'Inactive group chain' }, 'default')
  await edit('Inactive group chain')
  await click('#group-enabled')
  vi.mocked(proxyGroupApi.update).mockClear()
  await button('保存')
  expect(proxyGroupApi.update).not.toHaveBeenCalled()
  expect(document.querySelector('[data-testid="chain-exit"]')?.textContent).toContain('Dynamic')
  expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull()
  await select('[data-testid="chain-exit"]', '策略组 · Static')
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('chain', expect.objectContaining({
    enabled: true, chain: { entry: { type: 'node', id: 'relay' }, exit: { type: 'group', id: 'static' } }
  }), 'default')
  expect(groups.find(group => group.id === 'dynamic')?.enabled).toBe(false)
})

it.each(['entry', 'exit'] as const)('retains and blocks missing or cyclic group %s references', async side => {
  groups.push({ id: 'mutual', name: 'Mutual', type: 'select', enabled: true, follow_group: 'chain' })
  const saved = { ...chain(), enabled: false }
  saved.chain![side] = { type: 'group', id: 'missing-group' }
  groups.push(saved)
  await render()
  await edit()
  expect(document.querySelector(`[data-testid="chain-${side}"]`)?.textContent).toContain('missing-group')
  await button('保存')
  expect(proxyGroupApi.update).not.toHaveBeenCalled()
  const choices = await options(`[data-testid="chain-${side}"]`)
  expect(choices.map(option => option.textContent?.trim())).not.toContain('策略组 · Mutual')
  expect(choices.map(option => option.textContent?.trim())).not.toContain('策略组 · Via relay')
  await selectOption(choices.find(option => option.textContent?.trim() === '策略组 · Dynamic'))
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('chain', expect.objectContaining({
    chain: { ...chain().chain, [side]: { type: 'group', id: 'dynamic' } }
  }), 'default')
})

it('requires both chain endpoints before submitting a new group', async () => {
  await render()
  await addChain()
  await button('保存')
  expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull()
  expect(proxyGroupApi.create).not.toHaveBeenCalled()
  await select('[data-testid="chain-exit"]', '节点 · Exit')
  await button('保存')
  expect(proxyGroupApi.create).not.toHaveBeenCalled()
  await select('[data-testid="chain-entry"]', '节点 · Relay')
  await button('保存')
  expect(proxyGroupApi.create).toHaveBeenCalledTimes(1)
})

it('preserves saved ordinary timing parameters when opening an editor after a chain', async () => {
  groups[0] = { ...groups[0], type: 'url-test', url: 'https://saved.invalid/probe', interval: 180 }
  groups.push({ id: 'other-test', name: 'Other test', type: 'url-test', enabled: true, manual_nodes: ['DIRECT'], url: 'https://other.invalid', interval: 600 }, chain())
  await render()
  await edit()
  await button('取消')
  await edit('Static')
  expect((document.querySelector('#group-url') as HTMLInputElement).value).toBe('https://saved.invalid/probe')
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('static', expect.objectContaining({ type: 'url-test', url: 'https://saved.invalid/probe', interval: 180, manual_nodes: ['relay'] }), 'default')
})

it('removes ordinary sources and timing fields when switching to a chain, and removes chain fields when switching back', async () => {
  groups[0] = { ...groups[0], type: 'load-balance', url: 'https://test.invalid', interval: 180, strategy: 'round-robin', lazy: false }
  await render()
  await edit('Static')
  await select('[data-testid="group-type"]', '代理链（仅 Mihomo）')
  expect(document.querySelector('#group-url')).toBeNull()
  await select('[data-testid="chain-entry"]', '节点 · Relay')
  await select('[data-testid="chain-exit"]', '节点 · Exit')
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenLastCalledWith('static', {
    id: 'static', name: 'Static', type: 'chain', enabled: true,
    chain: { entry: { type: 'node', id: 'relay' }, exit: { type: 'node', id: 'exit' } }
  }, 'default')
  await edit('Static')
  await select('[data-testid="group-type"]', '手动选择 (Select)')
  expect(document.querySelector('[data-testid="chain-entry"]')).toBeNull()
  await source('节点')
  await button('手动选择节点')
  const direct = [...document.querySelectorAll('[role="option"]')].find(option => option.textContent?.trim() === 'DIRECT')!
  ;(direct as HTMLElement).click()
  await flushPromises()
  await button('DIRECT', document.querySelector('[role="dialog"]')!)
  await button('保存')
  const payload = vi.mocked(proxyGroupApi.update).mock.calls.at(-1)![1] as ProxyGroup
  expect(payload.type).toBe('select')
  expect(payload.manual_nodes).toEqual(['DIRECT'])
  expect(payload).not.toHaveProperty('chain')
  expect(payload).not.toHaveProperty('url')
})

it('allows the original exit and its named chain in the same ordinary group', async () => {
  groups.push(chain())
  await render()
  await button('添加策略组')
  await fill('#group-name', 'Both exits')
  await source('节点')
  await button('手动选择节点')
  ;([...document.querySelectorAll('[role="option"]')].find(option => option.textContent?.trim() === 'Exit') as HTMLElement).click()
  await flushPromises()
  await button('Exit', document.querySelector('[role="dialog"]')!)
  await source('策略')
  await button('选择已有策略组')
  ;([...document.querySelectorAll('[role="option"]')].find(option => option.textContent?.trim() === 'Via relay') as HTMLElement).click()
  await flushPromises()
  await button('Via relay', document.querySelector('[role="dialog"]')!)
  await button('保存')
  expect(proxyGroupApi.create).toHaveBeenCalledWith(expect.objectContaining({
    manual_nodes: ['exit'], include_groups: ['chain'],
    proxies_order: [{ type: 'node', id: 'exit' }, { type: 'strategy', id: 'chain' }]
  }), 'default')
})

it('does not offer chains as follow targets', async () => {
  groups.push(chain())
  await render()
  await button('添加策略组')
  await source('跟随')
  const choices = await options('[role="dialog"] [role="combobox"]')
  expect(choices.map(option => option.textContent?.trim())).toEqual(['Static', 'Dynamic'])
})

it('explicitly clears the previous follow group when switching to manual nodes', async () => {
  groups.push({ id: 'follower', name: 'Follower', type: 'select', follow_group: 'static' })
  await render()
  await edit('Follower')
  await source('跟随')
  await source('节点')
  await button('手动选择节点')
  ;([...document.querySelectorAll('[role="option"]')].find(option => option.textContent?.trim() === 'REJECT') as HTMLElement).click()
  await flushPromises()
  await button('REJECT', document.querySelector('[role="dialog"]')!)
  await button('保存')
  expect(proxyGroupApi.update).toHaveBeenCalledWith('follower', expect.objectContaining({
    manual_nodes: ['REJECT'], follow_group: ''
  }), 'default')
})

it('offers a named chain as a rule policy and persists its name', async () => {
  groups.push(chain())
  await render(Rules)
  await button('添加规则')
  await fill('#rule-values', 'example.test')
  document.querySelector('[role="dialog"] [aria-label="策略"]')!.setAttribute('data-testid', 'rule-policy')
  await select('[data-testid="rule-policy"]', 'Via relay')
  await button('保存 1 条规则')
  expect(ruleApi.compose).toHaveBeenCalledWith(expect.objectContaining({
    source: { kind: 'values', rule_type: 'DOMAIN-SUFFIX', text: 'example.test' }, rule: expect.objectContaining({ policy: 'Via relay' })
  }), 'default')
})

it('keeps rejected drafts open with inline backend guidance and no shared writes', async () => {
  groups.push(chain())
  await render()
  await edit()
  vi.mocked(proxyGroupApi.update).mockRejectedValue({ response: { data: { message: '代理链依赖存在循环' } } })
  await button('保存')
  expect(document.querySelector('[role="dialog"] [role="alert"]')?.textContent).toBe('代理链依赖存在循环')
  expect(document.querySelector('[data-testid="chain-entry"]')?.textContent).toContain('Relay')
  expect(nodeApi.update).not.toHaveBeenCalled()
})

it('deletes an unreferenced chain through group CRUD', async () => {
  groups.push(chain())
  await render()
  host = mount(ConfirmHost, { attachTo: document.body })
  await button('删除', document.querySelector('[data-name="Via relay"]')!)
  await button('删除', document.querySelector('[role="alertdialog"]')!)
  expect(proxyGroupApi.delete).toHaveBeenCalledWith('chain', 'default')
  expect(document.querySelector('[data-name="Via relay"]')).toBeNull()
  expect(nodeApi.update).not.toHaveBeenCalled()
})

it('shared node editing preserves raw dialer-proxy without adding a profile chain field', async () => {
  await render(Nodes)
  await click('[aria-label="编辑 Exit"]')
  await button('保存')
  const payload = vi.mocked(nodeApi.update).mock.calls[0][1]
  expect(JSON.parse(payload.proxy_string)['dialer-proxy']).toBe('DIRECT')
  expect(payload).not.toHaveProperty('dialer_ref')
  expect(payload).not.toHaveProperty('chain')
  expect(proxyGroupApi.getAll).not.toHaveBeenCalled()
})

it('shared batch delete conflict never rewrites groups and shows backend guidance', async () => {
  await render(Nodes)
  host = mount(ConfirmHost, { attachTo: document.body })
  vi.mocked(nodeApi.previewDeletion).mockImplementation(async (ids: string[]) => response({
    nodes: ids.map(id => ({ id, name: id })), usages: [], groups: [], blockers: [], empty_sources: [], confirmation: 'confirm'
  }) as any)
  vi.mocked(nodeApi.deleteWithReferences).mockRejectedValue({ response: { data: { message: '节点仍被代理链引用' } } })
  await button('全选')
  await button('删除 4 项')
  await button('删除节点')
  expect(nodeApi.deleteWithReferences).toHaveBeenCalledTimes(1)
  // 由服务端统一解除引用，前端不自己改写策略组；失败原因显示在删除窗口里
  expect(api.put).not.toHaveBeenCalled()
  expect(document.querySelector('[role="dialog"] [role="alert"]')?.textContent).toContain('节点仍被代理链引用')
})

it('shared node disable conflict restores enabled row and shows backend guidance', async () => {
  await render(Nodes)
  vi.mocked(nodeApi.update).mockRejectedValue({ response: { data: { message: '节点仍被代理链引用' } } })
  await click('[aria-label="停用 Relay"]')
  expect(document.querySelector('[aria-label="停用 Relay"]')).not.toBeNull()
  expect(notify.error).toHaveBeenCalledWith('节点仍被代理链引用')
})
