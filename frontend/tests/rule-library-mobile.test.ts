import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import RuleLibrary from '@/views/RuleLibrary.vue'
import { confirmState, notify, settleConfirm } from '@/lib/feedback'

// 只替换网络边界；下拉菜单、对话框和勾选框都用真实组件。
const network = vi.hoisted(() => {
  const state = { library: [] as Record<string, unknown>[] }
  const get = vi.fn(async (path: string) => ({
    data: path === '/rule-library' ? structuredClone(state.library)
      : path === '/rule-library/proxy-domains' ? { proxy_domains: '' }
      : []
  }))
  const put = vi.fn(async (_path: string, _body?: unknown) => ({ data: { success: true } }))
  const post = vi.fn(async (_path: string, _body?: unknown) => ({ data: { success: true } }))
  const remove = vi.fn(async (_path: string) => ({ data: { success: true } }))
  return { state, get, put, post, remove }
})

vi.mock('@/api', () => ({
  default: { get: network.get, post: network.post, put: network.put, delete: network.remove },
  ruleApi: { getAll: vi.fn(async () => ({ data: [] })) },
  tasksApi: { list: vi.fn(async () => ({ data: { success: true, tasks: [] } })) }
}))

const wrappers: VueWrapper[] = []

function stubViewport(phone: boolean) {
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: String(query).includes('639px') ? phone : String(query).includes('900px'),
    media: query, onchange: null,
    addListener: vi.fn(), removeListener: vi.fn(),
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn()
  })))
}

beforeEach(() => {
  vi.clearAllMocks()
  network.state.library = [
    {
      id: 'lib-content', name: 'Shared domains', source_type: 'content',
      content: '# 注释\nexample.com\n\nexample.org\n', url: '', behavior: 'domain', format: 'text', enabled: true
    },
    {
      id: 'lib-url', name: 'Remote list', source_type: 'url', content: '',
      url: 'https://www.rules.example.com/sets/list.yaml', behavior: 'classical', format: 'yaml', enabled: false
    }
  ]
  stubViewport(true)
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.spyOn(notify, 'success').mockImplementation(() => 0)
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  settleConfirm('cancel')
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

async function renderLibrary() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/rule-library', component: RuleLibrary }] })
  await router.push('/rule-library')
  await router.isReady()
  const wrapper = mount(defineComponent({ setup: () => () => h(RouterView) }), {
    attachTo: document.body,
    global: { plugins: [router] }
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

async function openMenu(wrapper: VueWrapper, id: string) {
  const trigger = wrapper.get(`[data-id="${id}"] [data-rule-menu]`)
  trigger.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  await flushPromises()
  return Array.from(document.querySelectorAll<HTMLElement>('[role="menuitem"]'))
}

describe('rule library on phones', () => {
  it('renders each rule set as a compact two-line row without the content preview', async () => {
    const wrapper = await renderLibrary()
    const list = wrapper.get('[data-phone-list]')
    const rows = list.findAll('[data-reorder-item]')
    expect(rows).toHaveLength(2)
    for (const row of rows) {
      expect(row.classes()).toContain('cf-rl-row')
      expect(row.classes()).not.toContain('rounded-[18px]')
      expect(row.find('.cf-rl-line1').exists()).toBe(true)
      expect(row.find('.cf-rl-line2').exists()).toBe(true)
      expect(row.find('footer').exists()).toBe(false)
    }
    expect(list.find('pre').exists()).toBe(false)
    expect(list.text()).not.toContain('example.org')

    const content = wrapper.get('[data-id="lib-content"]')
    expect(content.get('[data-rule-open]').text()).toBe('Shared domains')
    expect(content.get('[data-rule-count]').text()).toBe('2 条')
    expect(content.get('.cf-rl-line2').text()).toContain('domain')
    expect(content.get('.cf-rl-line2').text()).toContain('内容')

    const remote = wrapper.get('[data-id="lib-url"]')
    expect(remote.find('[data-rule-count]').exists()).toBe(false)
    expect(remote.get('.cf-rl-source').text()).toBe('rules.example.com')
    expect(remote.get('.cf-rl-source').attributes('title')).toBe('https://www.rules.example.com/sets/list.yaml')
    // 全选工具栏仍在
    expect(wrapper.text()).toContain('全选')
  })

  it('collects every row action into a single menu with the same handlers', async () => {
    const wrapper = await renderLibrary()
    const row = wrapper.get('[data-id="lib-content"]')
    // 行内只剩勾选框、名称和「⋯」三个可点控件
    expect(row.findAll('[data-rule-menu]')).toHaveLength(1)
    expect(row.find('[aria-label="停用 Shared domains"]').exists()).toBe(false)
    expect(row.find('[aria-label="编辑"]').exists()).toBe(false)
    expect(row.find('[aria-label="删除"]').exists()).toBe(false)

    let items = await openMenu(wrapper, 'lib-content')
    expect(items.map(item => item.textContent?.trim())).toEqual(['添加规则', '加入当前配置', '复制下载地址', '编辑', '停用', '删除'])
    items.find(item => item.textContent?.trim() === '停用')!.click()
    await flushPromises()
    expect(network.put).toHaveBeenCalledWith('/rule-library/lib-content', expect.objectContaining({ id: 'lib-content', enabled: false }))
    expect(row.attributes('data-disabled')).toBe('true')

    items = await openMenu(wrapper, 'lib-content')
    items.find(item => item.textContent?.trim() === '删除')!.click()
    await flushPromises()
    expect(confirmState.open).toBe(true)
    expect(confirmState.title).toBe('删除共享规则集')
    settleConfirm('confirm')
    await flushPromises()
    expect(network.remove).toHaveBeenCalledWith('/rule-library/lib-content')

    // 远端地址没有「添加规则」，启用项写「启用」
    items = await openMenu(wrapper, 'lib-url')
    expect(items.map(item => item.textContent?.trim())).toEqual(['加入当前配置', '复制下载地址', '编辑', '启用', '删除'])
  })

  it('opens the edit dialog with the full content when the row body is tapped', async () => {
    const wrapper = await renderLibrary()
    const row = wrapper.get('[data-id="lib-content"]')

    // 勾选框只负责选择，不打开窗口
    await row.get('[role="checkbox"]').trigger('click')
    await flushPromises()
    expect(row.get('[role="checkbox"]').attributes('data-state')).toBe('checked')
    expect(document.querySelector('[role="dialog"]')).toBeNull()

    await row.get('.cf-rl-meta').trigger('click')
    await flushPromises()
    const dialog = document.querySelector('[role="dialog"]')
    expect(dialog?.textContent).toContain('编辑规则')
    expect((document.getElementById('lib-content') as HTMLTextAreaElement).value).toBe('# 注释\nexample.com\n\nexample.org\n')
  })

  it('opens the same dialog from the name button', async () => {
    const wrapper = await renderLibrary()
    await wrapper.get('[data-id="lib-url"] [data-rule-open]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('编辑规则')
    expect((document.getElementById('lib-url') as HTMLInputElement).value).toBe('https://www.rules.example.com/sets/list.yaml')
  })

  it('dims disabled rule sets with an 已停用 chip', async () => {
    const wrapper = await renderLibrary()
    const disabled = wrapper.get('[data-id="lib-url"]')
    expect(disabled.attributes('data-disabled')).toBe('true')
    expect(disabled.get('[data-rule-disabled]').text()).toBe('已停用')
    const enabled = wrapper.get('[data-id="lib-content"]')
    expect(enabled.attributes('data-disabled')).toBeUndefined()
    expect(enabled.find('[data-rule-disabled]').exists()).toBe(false)
  })

  it('keeps the card layout with the content preview on tablets', async () => {
    stubViewport(false)
    const wrapper = await renderLibrary()
    expect(wrapper.find('[data-phone-list]').exists()).toBe(false)
    expect(wrapper.find('[data-rule-menu]').exists()).toBe(false)
    expect(wrapper.get('[data-id="lib-content"] pre').text()).toContain('example.org')
    expect(wrapper.find('[aria-label="停用 Shared domains"]').exists()).toBe(true)
    // 平板上点卡片空白处不打开编辑窗口
    await wrapper.get('[data-id="lib-content"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
})
