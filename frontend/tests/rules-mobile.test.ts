import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import Rules from '@/views/Rules.vue'
import { setActiveProfileId } from '@/profileContext'
import { notify } from '@/lib/feedback'

// 手机宽度下的规则列表：只替换 HTTP 边界，页面、行和菜单都是真实组件。
const network = vi.hoisted(() => {
  const state = { rules: [] as Record<string, any>[] }
  const rules = {
    getAll: vi.fn(async (_profileId?: string) => ({ data: structuredClone(state.rules) })),
    renameGroup: vi.fn(async () => ({ data: { success: true } })),
    findDuplicates: vi.fn(async () => ({ data: {} })),
    getDns: vi.fn(async (_profileId?: string): Promise<{ data: any }> => ({ data: { engine: { oxidns_in_use: false }, rules: {} } })),
    setDns: vi.fn(),
    compose: vi.fn()
  }
  const get = vi.fn(async () => ({ data: [] }))
  const post = vi.fn(async (_path: string, _body?: any, _config?: any): Promise<{ data: any }> => ({ data: { success: true } }))
  return { state, rules, get, post }
})

vi.mock('@/api', () => ({
  default: { get: network.get, post: network.post, put: vi.fn(), delete: vi.fn() },
  ruleApi: network.rules,
  ruleSetApi: { update: vi.fn(), delete: vi.fn() },
  proxyGroupApi: { getAll: vi.fn(async () => ({ data: [{ id: 'g', name: 'Proxy' }] })), previewRegex: vi.fn() }
}))

const rule = (id: string, value: string, policy: string, patch: Record<string, unknown> = {}) =>
  ({ id, itemType: 'rule', rule_type: 'DOMAIN-SUFFIX', value, policy, enabled: true, ...patch })

const wrappers: VueWrapper[] = []

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  setActiveProfileId('default')
  network.state.rules = [
    rule('r-lan', 'lan', 'DIRECT', { rule_type: 'GEOIP' }),
    rule('r-a', 'a.test', 'Proxy', { group_name: 'DNS地址' }),
    rule('r-b', 'b.test', 'Proxy', { group_name: 'DNS地址' }),
    rule('r-c', 'c.test', 'Proxy', { group_name: 'DNS地址' }),
    rule('r-home', '192.168.0.0/24', 'Home', { rule_type: 'IP-CIDR' }),
    rule('r-x', 'x.test', 'DIRECT'),
    rule('r-y', 'y.test', 'DIRECT')
  ]
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: query.includes('max-width: 639px'), media: query, onchange: null, addListener: vi.fn(), removeListener: vi.fn(),
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn()
  })))
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('scrollTo', vi.fn())
  for (const kind of ['success', 'warning', 'error'] as const) vi.spyOn(notify, kind).mockImplementation(() => 0)
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

async function render() {
  const wrapper = mount(Rules, { attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
const heads = (wrapper: VueWrapper) => wrapper.findAll('[data-group-head]')
const rowValues = (wrapper: VueWrapper) => wrapper.findAll('.rule-row').map(row => row.find('.font-mono.text-\\[12\\.5px\\]').text())
const toggle = (wrapper: VueWrapper, name: string) => wrapper.find(`button[aria-label$="分组 ${name}"]`)
const clickText = async (text: string) => {
  const button = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(item => item.textContent?.trim() === text)
  expect(button, `button ${text}`).toBeTruthy()
  button!.click()
  await flushPromises()
}

describe('rules on a phone', () => {
  it('drops the wide table header and the fixed minimum width', async () => {
    const wrapper = await render()
    const header = wrapper.find('[aria-hidden="true"].uppercase')
    expect(header.classes()).toContain('max-sm:hidden')
    expect(wrapper.get('#sortable-rules').element.parentElement!.className).toContain('max-sm:min-w-0')
  })

  it('folds enable, edit and delete into one menu per rule', async () => {
    const wrapper = await render()
    const row = wrapper.get('[data-rule-key]')
    expect(row.find('.rule-actions').exists()).toBe(false)
    expect(row.find('button[aria-label="编辑"]').exists()).toBe(false)
    await row.get('.rule-menu').trigger('pointerdown', { button: 0, pointerType: 'mouse' })
    await row.get('.rule-menu').trigger('click')
    await flushPromises()
    const items = [...document.querySelectorAll('[role="menuitem"]')].map(item => item.textContent!.trim())
    expect(items).toEqual(['编辑', '停用', '删除'])
  })

  it('opens the editor when the row itself is tapped, but not from its controls', async () => {
    const wrapper = await render()
    const row = wrapper.get('[data-rule-key]')
    await row.get('.rule-value').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('编辑规则')
    document.body.querySelectorAll('[role="dialog"]').forEach(dialog => dialog.remove())
  })

  it('marks disabled rules in text because the eye icon is gone', async () => {
    network.state.rules[0].enabled = false
    const wrapper = await render()
    expect(wrapper.get('[data-rule-key]').text()).toContain('已停用')
  })

  it('collapses hit simulation behind a button until it is needed', async () => {
    const wrapper = await render()
    expect(wrapper.find('[data-sim-card]').exists()).toBe(false)
    await wrapper.get('[data-sim-toggle]').trigger('click')
    expect(wrapper.find('[data-sim-card]').exists()).toBe(true)
    expect(wrapper.get('[data-sim-toggle]').attributes('aria-pressed')).toBe('true')
  })
})
