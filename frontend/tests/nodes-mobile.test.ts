import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import Nodes from '@/views/Nodes.vue'
import api, { nodeApi } from '@/api'

vi.mock('sortablejs', () => ({ default: { create: vi.fn(() => ({ destroy: vi.fn() })) } }))

vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn() },
  nodeApi: { getAll: vi.fn(), latency: vi.fn(), testLatency: vi.fn(), update: vi.fn() },
  profileApi: { list: vi.fn() }
}))

const storedNodes = [
  { id: 'hk', name: '香港 01', enabled: true, proxy_string: 'trojan://password@192.0.2.1:443' },
  { id: 'jp', name: '日本 02', enabled: false, proxy_string: 'ss://password@192.0.2.2:443', subscription_id: 'sub-one', subscription_name: '测试订阅' }
]
const previewNodes = [
  { name: '新加坡 预览', type: 'vmess', server: '192.0.2.4', port: 443, source_type: 'subscription', source_id: 'sub-one', subscription_name: '测试订阅' }
]

const matchMediaDescriptor = Object.getOwnPropertyDescriptor(window, 'matchMedia')
let wrapper: VueWrapper | undefined

beforeAll(() => {
  // 模拟手机宽度：所有 max-width 查询（639px、900px 等）都命中
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn((query: string) => ({
      matches: String(query).includes('max-width'),
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn()
    }))
  })
})
afterAll(() => {
  if (matchMediaDescriptor) Object.defineProperty(window, 'matchMedia', matchMediaDescriptor)
  else delete (window as any).matchMedia
})
beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  vi.mocked(nodeApi.getAll).mockResolvedValue({ data: storedNodes.map(node => ({ ...node })) } as any)
  vi.mocked(nodeApi.latency).mockResolvedValue({ data: { results: {
    [JSON.stringify(['node', 'hk'])]: { latency: 42, history: [40, 42] }
  } } } as any)
  vi.mocked(nodeApi.update).mockResolvedValue({ data: {} } as any)
  vi.mocked(api.get).mockResolvedValue({ data: [{ id: 'sub-one', enabled: true }] } as any)
  vi.mocked(api.post).mockResolvedValue({ data: { nodes: previewNodes, imported_node_ids: ['jp'] } } as any)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  localStorage.clear()
  document.body.innerHTML = ''
})

async function render() {
  // 即使记住的是表格视图，手机也只用紧凑列表
  localStorage.setItem('configflow-nodes-view', 'list')
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: Nodes }] })
  await router.push('/')
  await router.isReady()
  wrapper = mount(Nodes, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
}

const row = (name: string) => wrapper!.get(`.node-tile[data-name="${name}"]`)

async function openMenu(name: string) {
  await row(name).get(`button[aria-label="${name} 的更多操作"]`).trigger('keydown', { key: 'Enter' })
  await flushPromises()
  return [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')]
}

describe('节点库手机布局', () => {
  it('节点排成一个列表里的两行紧凑行', async () => {
    await render()
    expect(wrapper!.find('table').exists()).toBe(false)
    const list = wrapper!.get('.node-tiles')
    expect(list.findAll('.node-tile').map(item => item.attributes('data-name'))).toEqual(['香港 01', '日本 02', '新加坡 预览'])

    const hk = row('香港 01')
    expect(hk.get('.node-tile-name').text()).toBe('香港 01')
    expect(hk.get('.node-tile-latency').text()).toBe('42ms')
    expect(hk.get('.node-tile-latency').classes()).toContain('text-success-accent')
    expect(hk.get('[data-node-meta]').text().replace(/\s+/g, '')).toBe('HK·TROJAN·手动添加')
    expect(hk.find('[role="checkbox"]').exists()).toBe(true)

    const sg = row('新加坡 预览')
    expect(sg.get('.node-tile-latency').text()).toBe('未测速')
    expect(sg.get('[data-node-meta]').text()).toContain('测试订阅')
  })

  it('行内编辑、启停图标收进同一个「更多」菜单', async () => {
    await render()
    const hk = row('香港 01')
    expect(hk.find('button[aria-label="编辑 香港 01"]').exists()).toBe(false)
    expect(hk.find('button[aria-label="停用 香港 01"]').exists()).toBe(false)
    expect(hk.findAll('button').filter(button => button.attributes('aria-label')?.includes('更多操作'))).toHaveLength(1)

    const items = await openMenu('香港 01')
    expect(items.map(item => item.textContent?.trim())).toEqual(['编辑', '停用', '单独测速', '节点字符串', '选择', '删除'])

    items.find(item => item.textContent?.trim() === '停用')!.click()
    await flushPromises()
    expect(nodeApi.update).toHaveBeenCalledWith('hk', expect.objectContaining({ enabled: false }))
  })

  it('订阅节点只读，菜单里只有单独测速，点击行不打开编辑', async () => {
    await render()
    const sg = row('新加坡 预览')
    expect(sg.find('button[aria-label="单独测速 新加坡 预览"]').exists()).toBe(false)
    const items = await openMenu('新加坡 预览')
    expect(items.map(item => item.textContent?.trim())).toEqual(['单独测速'])
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()

    await sg.get('.node-tile-name').trigger('click')
    await flushPromises()
    expect(document.body.textContent).not.toContain('编辑节点')
  })

  it('点击行打开编辑，点复选框只切换选择', async () => {
    await render()
    await row('香港 01').get('[role="checkbox"]').trigger('click')
    await flushPromises()
    expect(row('香港 01').attributes('data-selected')).toBeDefined()
    expect(document.body.textContent).not.toContain('编辑节点')
    expect(wrapper!.text()).toContain('删除 1 项')

    await row('香港 01').get('.node-tile-name').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('编辑节点')
    expect((document.getElementById('node-name') as HTMLInputElement).value).toBe('香港 01')
    // 点行不改变选择
    expect(row('香港 01').attributes('data-selected')).toBeDefined()
  })

  it('停用的节点显示「已停用」并变暗', async () => {
    await render()
    const jp = row('日本 02')
    expect(jp.classes()).toContain('opacity-55')
    expect(jp.get('[data-node-meta]').text()).toContain('已停用')
    expect(row('香港 01').get('[data-node-meta]').text()).not.toContain('已停用')

    const items = await openMenu('日本 02')
    expect(items.map(item => item.textContent?.trim())).toContain('启用')
  })
})
