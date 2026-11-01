import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import Nodes from '@/views/Nodes.vue'
import NodeLatencySettings from '@/components/nodes/NodeLatencySettings.vue'
import api, { nodeApi } from '@/api'
import { notify } from '@/lib/feedback'

// This suite checks selection while reordering, not Sortable's drag implementation.
vi.mock('sortablejs', () => ({ default: { create: vi.fn(() => ({ destroy: vi.fn() })) } }))

vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn() },
  nodeApi: { getAll: vi.fn(), latency: vi.fn(), testLatency: vi.fn() },
  profileApi: { list: vi.fn() }
}))

const storedNodes = [
  { id: 'manual-hk', name: '香港 手动', enabled: true, proxy_string: 'trojan://password@192.0.2.1:443' },
  { id: 'manual-jp', name: '日本 手动', enabled: false, proxy_string: 'ss://password@192.0.2.2:443', subscription_id: '' },
  { id: 'imported', name: '香港 订阅导入', enabled: true, proxy_string: 'trojan://password@192.0.2.3:443', subscription_id: 'sub-one', subscription_name: '测试订阅' }
]
const previewNodes = [
  { name: '香港 订阅导入', type: 'trojan', source_type: 'subscription', source_id: 'sub-one', subscription_name: '测试订阅' },
  { name: '日本 订阅预览', type: 'ss', server: '192.0.2.4', port: 443, source_type: 'subscription', source_id: 'sub-one', subscription_name: '测试订阅' }
]
const matchMediaDescriptor = Object.getOwnPropertyDescriptor(window, 'matchMedia')
let wrapper: VueWrapper | undefined
const storedKey = (id: string) => JSON.stringify(['node', id])
const subscriptionKey = (id: string, name: string) => JSON.stringify(['subscription', id, name])

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
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
  vi.mocked(nodeApi.latency).mockResolvedValue({ data: { results: {} } } as any)
  vi.mocked(api.get).mockResolvedValue({ data: [{ id: 'sub-one', enabled: true }] } as any)
  vi.mocked(api.post).mockResolvedValue({ data: { nodes: previewNodes, imported_node_ids: ['imported'] } } as any)
})
afterEach(() => {
  window.getSelection()?.removeAllRanges()
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  localStorage.clear()
  document.body.innerHTML = ''
})

async function render(view: 'card' | 'list') {
  localStorage.setItem('configflow-nodes-view', view)
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: Nodes }] })
  await router.push('/')
  await router.isReady()
  wrapper = mount(Nodes, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
}

async function click(label: string, group?: string) {
  const root = group ? wrapper!.get(`[aria-label="${group}"]`) : wrapper!
  const button = root.findAll('button').find(button => button.text() === label)
  expect(button, label).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

function names() {
  return wrapper!.findAll('[data-name]').map(node => node.attributes('data-name'))
}

describe.each(['card', 'list'] as const)('%s 整项选择', view => {
  it('点击名称或空白区域可连续多选和取消，包括已停用节点', async () => {
    await render(view)
    const first = wrapper!.get('[data-name="香港 手动"]')
    const second = wrapper!.get('[data-name="日本 手动"]')
    const name = first.findAll('span').find(span => span.text() === '香港 手动')!
    await name.trigger('click')
    await second.trigger('click')
    expect(first.get('[role="checkbox"]').attributes('aria-checked')).toBe('true')
    expect(second.get('[role="checkbox"]').attributes('aria-checked')).toBe('true')
    expect(wrapper!.text()).toContain('删除 2 项')
    await first.trigger('click')
    expect(first.get('[role="checkbox"]').attributes('aria-checked')).toBe('false')
    expect(wrapper!.text()).toContain('删除 1 项')
  })

  it('点击复选框只切换一次，不被整项点击再次取消', async () => {
    await render(view)
    const checkbox = wrapper!.get('[aria-label="选择 香港 手动"]')
    await checkbox.trigger('click')
    expect(checkbox.attributes('aria-checked')).toBe('true')
    await checkbox.get('svg').trigger('click')
    expect(checkbox.attributes('aria-checked')).toBe('false')
    expect(wrapper!.text()).not.toContain('删除 1 项')
  })

  it('点击编辑按钮内的图标不改变已有选择', async () => {
    await render(view)
    const first = wrapper!.get('[data-name="香港 手动"]')
    await first.trigger('click')
    await first.get('[aria-label="编辑 香港 手动"] svg').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('编辑节点')
    expect(first.get('[role="checkbox"]').attributes('aria-checked')).toBe('true')
  })

  it('订阅预览节点的内容不可选', async () => {
    await render(view)
    const preview = wrapper!.get('[data-name="日本 订阅预览"]')
    await preview.trigger('click')
    expect(preview.find('[role="checkbox"]').exists()).toBe(false)
    expect(wrapper!.text()).not.toContain('删除 1 项')
  })

  it('拖选节点文字时不切换选择', async () => {
    await render(view)
    const first = wrapper!.get('[data-name="香港 手动"]')
    const name = first.findAll('span').find(span => span.text() === '香港 手动')!
    const range = document.createRange()
    range.selectNodeContents(name.element)
    window.getSelection()!.addRange(range)
    await name.trigger('click')
    expect(first.get('[role="checkbox"]').attributes('aria-checked')).toBe('false')
    window.getSelection()!.removeAllRanges()
    await name.trigger('click')
    expect(first.get('[role="checkbox"]').attributes('aria-checked')).toBe('true')
  })

  it('排序时暂停整项选择，退出后恢复', async () => {
    await render(view)
    await click('调整顺序')
    const first = wrapper!.get('[data-name="香港 手动"]')
    await first.trigger('click')
    expect(wrapper!.text()).not.toContain('删除 1 项')
    if (view === 'list') expect(first.get('[role="checkbox"]').attributes('disabled')).toBeDefined()
    else expect(first.find('[role="checkbox"]').exists()).toBe(false)
    await click('取消')
    await first.trigger('click')
    expect(first.get('[role="checkbox"]').attributes('aria-checked')).toBe('true')
  })
})

describe.each(['card', 'list'] as const)('%s 来源筛选', view => {
  it('手动节点仅显示无订阅来源的节点，订阅节点包含导入和预览节点', async () => {
    await render(view)
    expect(names()).toEqual(['香港 手动', '日本 手动', '香港 订阅导入', '日本 订阅预览'])
    expect(wrapper!.get('[aria-label="按来源筛选"]').text()).not.toContain('节点库')

    await click('手动节点')
    expect(names()).toEqual(['香港 手动', '日本 手动'])
    expect(wrapper!.get('[aria-label="按来源筛选"] button[aria-pressed="true"]').text()).toBe('手动节点')
    await click('全选')
    expect(wrapper!.text()).toContain('删除 2 项')

    await click('订阅节点')
    expect(names()).toEqual(['香港 订阅导入', '日本 订阅预览'])
    await click('全部来源')
    expect(names()).toHaveLength(4)
  })

  it('手动来源可叠加地区、协议和关键词筛选', async () => {
    await render(view)
    await click('手动节点')
    await click('香港', '按地区筛选')
    expect(names()).toEqual(['香港 手动'])
    await click('SS', '按协议筛选')
    expect(names()).toEqual([])
    expect(wrapper!.text()).toContain('没有匹配的节点')
    await click('SS', '按协议筛选')
    await click('全部', '按地区筛选')
    await wrapper!.get('input[placeholder="搜索名称、地址或备注…"]').setValue('日本')
    expect(names()).toEqual(['日本 手动'])
  })

  it('没有额外订阅预览时仍能排除库内的订阅导入节点', async () => {
    vi.mocked(api.post).mockResolvedValue({ data: { nodes: [], imported_node_ids: ['imported'] } } as any)
    await render(view)
    await click('手动节点')
    expect(names()).toEqual(['香港 手动', '日本 手动'])
    await click('订阅节点')
    expect(names()).toEqual(['香港 订阅导入'])
  })

  it('手动节点同名不会遮蔽订阅节点，不同订阅同名也均保留', async () => {
    vi.mocked(api.get).mockResolvedValue({ data: [{ id: 'sub-one' }, { id: 'sub-two' }] } as any)
    vi.mocked(api.post).mockResolvedValue({ data: {
      imported_node_ids: ['imported'],
      nodes: [
        ...previewNodes,
        { name: '香港 手动', type: 'trojan', source_type: 'subscription', source_id: 'sub-one', source_name: '测试订阅' },
        { name: '香港 订阅导入', type: 'trojan', source_type: 'subscription', source_id: 'sub-two', source_name: '另一订阅' }
      ]
    } } as any)
    await render(view)
    expect(names()).toEqual(['香港 手动', '日本 手动', '香港 订阅导入', '日本 订阅预览', '香港 手动', '香港 订阅导入'])
    await click('订阅节点')
    expect(names()).toEqual(['香港 订阅导入', '日本 订阅预览', '香港 手动', '香港 订阅导入'])
    expect(wrapper!.text()).toContain('另一订阅')
    await click('手动节点')
    expect(names()).toEqual(['香港 手动', '日本 手动'])
    expect(api.post).toHaveBeenCalledWith('/proxy-groups/preview-regex', expect.objectContaining({ deduplicate_by_source: true }))
  })

  it('订阅筛选排除后端正则未通过的导入节点，全部来源仍可管理它们', async () => {
    // 后端使用 Python 正则，此处只模拟最终筛选结果，不在浏览器重复解释正则。
    vi.mocked(api.get).mockResolvedValue({ data: [{ id: 'sub-one', regex_filter: '(?i)日本' }] } as any)
    vi.mocked(api.post).mockResolvedValue({ data: { imported_node_ids: [], nodes: [previewNodes[1]] } } as any)
    await render(view)
    await click('订阅节点')
    expect(names()).toEqual(['日本 订阅预览'])
    await click('全部来源')
    expect(names()).toContain('香港 订阅导入')
  })

  it('禁用或已删除订阅的导入节点不出现在订阅筛选中', async () => {
    vi.mocked(api.get).mockResolvedValue({ data: [{ id: 'sub-one', enabled: false }] } as any)
    await render(view)
    expect(api.post).not.toHaveBeenCalled()
    await click('订阅节点')
    expect(names()).toEqual([])
    await click('全部来源')
    expect(names()).toEqual(storedNodes.map(node => node.name))
  })

  it('订阅加载失败时提示错误，不将未验证的导入节点当作合格结果', async () => {
    const error = vi.spyOn(notify, 'error').mockImplementation(() => 0)
    vi.mocked(api.post).mockRejectedValue(new Error('network error'))
    await render(view)
    expect(error).toHaveBeenCalledWith('加载订阅节点失败')
    await click('订阅节点')
    expect(names()).toEqual([])
    await click('全部来源')
    expect(names()).toEqual(storedNodes.map(node => node.name))
  })
})

it('表格中的订阅预览节点保持只读，不能编辑、删除或选中', async () => {
  await render('list')
  const row = wrapper!.get('[data-name="日本 订阅预览"]')
  expect(row.find('[role="checkbox"]').exists()).toBe(false)
  expect(row.find('button[aria-label="编辑 日本 订阅预览"]').exists()).toBe(false)
  expect(row.find('button[aria-label="删除 日本 订阅预览"]').exists()).toBe(false)
  expect(row.find('button[aria-label="单独测速 日本 订阅预览"]').exists()).toBe(true)
  expect(row.text()).toContain('测试订阅')
})

it('离屏跳过渲染覆盖全部节点磁贴，包括不能排序的订阅节点', async () => {
  await render('card')
  const tiles = wrapper!.findAll('[data-tile]')
  expect(tiles.map(tile => tile.attributes('data-name'))).toContain('日本 订阅预览')
  expect(tiles.every(tile => tile.classes('cf-cv-row'))).toBe(true)
})

it('保存测速设置后刷新显示的测速历史', async () => {
  vi.mocked(nodeApi.latency).mockResolvedValueOnce({ data: { results: { [storedKey('manual-hk')]: { latency: 123 } } } } as any)
  await render('card')
  expect(wrapper!.get('[data-name="香港 手动"]').text()).toContain('123')
  wrapper!.findComponent(NodeLatencySettings).vm.$emit('saved')
  await flushPromises()
  expect(nodeApi.latency).toHaveBeenCalledTimes(2)
  expect(wrapper!.get('[data-name="香港 手动"]').text()).toContain('未测速')
})

it('代理测速每批最多二十个并展示真实进度，失败保留原因，已停用节点不测试', async () => {
  const records = Array.from({ length: 22 }, (_, i) => ({
    id: `node-${i}`, name: `node-${i}`, enabled: i !== 21, type: 'trojan', server: 'example.invalid', port: 443, params: { password: 'secret' }
  }))
  vi.mocked(nodeApi.getAll).mockResolvedValue({ data: records } as any)
  vi.mocked(api.get).mockResolvedValue({ data: [] } as any)
  let finishFirst!: (value: any) => void
  let finishSecond!: (value: any) => void
  vi.mocked(nodeApi.testLatency)
    .mockReturnValueOnce(new Promise(resolve => { finishFirst = resolve }))
    .mockReturnValueOnce(new Promise(resolve => { finishSecond = resolve }))
  await render('card')
  await click('全部测速')
  expect(nodeApi.testLatency).toHaveBeenCalledTimes(1)
  expect(nodeApi.testLatency).toHaveBeenNthCalledWith(1, records.slice(0, 20).map(node => storedKey(node.id)))
  expect(wrapper!.text()).toContain('正在测速… 0/21')
  const active = () => wrapper!.get('[data-name="node-0"]')
  const queued = () => wrapper!.get('[data-name="node-20"]')
  expect(active().text()).toContain('正在测速…')
  expect(active().text()).not.toContain('等待测速')
  expect(queued().text()).toContain('等待测速')
  expect(queued().text()).not.toContain('正在测速…')
  expect(wrapper!.get('[data-name="node-21"]').text()).not.toMatch(/正在测速|等待测速/)
  // Switching views keeps the same active batch and queued targets.
  await wrapper!.get('button[aria-label="列表视图"]').trigger('click')
  await flushPromises()
  expect(active().text()).toContain('正在测速…')
  expect(queued().text()).toContain('等待测速')
  await wrapper!.get('button[aria-label="卡片视图"]').trigger('click')
  await flushPromises()
  expect(wrapper!.findAll('button').find(button => button.text() === '测速设置')!.attributes('disabled')).toBeDefined()
  expect(wrapper!.get('button[title="从 ConfigFlow 所在机器，经节点访问测试地址"]').attributes('disabled')).toBeDefined()
  const results = Object.fromEntries(records.slice(0, 20).map(node => [storedKey(node.id), { latency: 150 }]))
  finishFirst({ data: { results, missing: [] } })
  await flushPromises()
  expect(wrapper!.text()).toContain('正在测速… 20/21')
  expect(active().text()).toContain('150')
  expect(active().text()).not.toMatch(/正在测速|等待测速/)
  expect(queued().text()).toContain('正在测速…')
  expect(queued().text()).not.toContain('等待测速')
  expect(nodeApi.testLatency).toHaveBeenNthCalledWith(2, [storedKey('node-20')])
  finishSecond({ data: { results: { [storedKey('node-20')]: { latency: null, error_code: 'auth', error: '代理认证失败，请检查节点凭据' } } } })
  await flushPromises()
  const failed = wrapper!.get('[data-name="node-20"]').text()
  expect(failed).toContain('代理认证失败')
  expect(failed).not.toContain('超时')
  wrapper!.findAll('[data-name]').forEach(row => expect(row.text()).not.toMatch(/正在测速|等待测速/))
  expect(wrapper!.text()).toContain('全部测速')
  expect(wrapper!.findAll('button').find(button => button.text() === '测速设置')!.attributes('disabled')).toBeUndefined()
  expect(nodeApi.testLatency).toHaveBeenCalledTimes(2)
  expect(wrapper!.get('button[title="从 ConfigFlow 所在机器，经节点访问测试地址"]').attributes('disabled')).toBeUndefined()
})

it.each([
  { kind: '结构化', batchSize: 20, total: 41 },
  { kind: 'URI', batchSize: 5, total: 11 }
])('$kind 后续批次请求失败时按实际完成数提示并保留结果', async ({ kind, batchSize, total }) => {
  const error = vi.spyOn(notify, 'error').mockImplementation(() => 0)
  const records = Array.from({ length: total }, (_, i) => ({
    id: `n${i}`, name: `n${i}`, enabled: true,
    ...(kind === 'URI' ? { proxy_string: 'trojan://secret@example.invalid:443' } : { type: 'trojan', server: 'example.invalid', port: 443 })
  }))
  vi.mocked(nodeApi.getAll).mockResolvedValue({ data: records } as any)
  vi.mocked(api.get).mockResolvedValue({ data: [] } as any)
  vi.mocked(nodeApi.latency).mockResolvedValue({ data: { results: { [storedKey(`n${batchSize}`)]: { latency: 321 } } } } as any)
  const results = Object.fromEntries(records.slice(0, batchSize).map(node => [storedKey(node.id),
    node.name === 'n1' ? { latency: null, error_code: 'auth', error: '代理认证失败' } : { latency: 150 }
  ]))
  let failSecond!: (reason: any) => void
  vi.mocked(nodeApi.testLatency)
    .mockResolvedValueOnce({ data: { results } } as any)
    .mockReturnValueOnce(new Promise((_, reject) => { failSecond = reject }))
  await render('card')
  await click('全部测速')
  expect(wrapper!.text()).toContain(`正在测速… ${batchSize}/${total}`)
  expect(nodeApi.testLatency).toHaveBeenNthCalledWith(2, records.slice(batchSize, batchSize * 2).map(node => storedKey(node.id)))
  expect(wrapper!.get(`[data-name="n${batchSize}"]`).text()).toContain('正在测速…')
  expect(wrapper!.get(`[data-name="n${total - 1}"]`).text()).toContain('等待测速')
  failSecond({ response: { data: { message: '独立测速组件暂时不可用' } } })
  await flushPromises()
  expect(error).toHaveBeenCalledWith('独立测速组件暂时不可用', `已完成 ${batchSize}/${total}，结果已保留`)
  expect(nodeApi.testLatency).toHaveBeenCalledTimes(2)
  expect(wrapper!.get('[data-name="n0"]').text()).toContain('150')
  expect(wrapper!.get('[data-name="n1"]').text()).toContain('代理认证失败')
  expect(wrapper!.get(`[data-name="n${batchSize}"]`).text()).toContain('321')
  expect(wrapper!.get(`[data-name="n${total - 1}"]`).text()).toContain('未测速')
  wrapper!.findAll('[data-name]').forEach(row => expect(row.text()).not.toMatch(/正在测速|等待测速/))
  expect(wrapper!.text()).toContain('全部测速')
  expect(wrapper!.get('button[title="从 ConfigFlow 所在机器，经节点访问测试地址"]').attributes('disabled')).toBeUndefined()
})

it('组件不可用时提示原因，不将现有结果覆盖为超时', async () => {
  const error = vi.spyOn(notify, 'error').mockImplementation(() => 0)
  vi.mocked(nodeApi.latency).mockResolvedValue({ data: { results: { [storedKey('manual-hk')]: { latency: 123 } } } } as any)
  vi.mocked(nodeApi.testLatency).mockRejectedValue({ response: { data: { message: '独立测速组件未安装' } } })
  await render('list')
  await wrapper!.get('button[aria-label="单独测速 香港 手动"]').trigger('click')
  await flushPromises()
  expect(error).toHaveBeenCalledWith('独立测速组件未安装', undefined)
  expect(wrapper!.get('[data-name="香港 手动"]').text()).toContain('123')
})

it.each([
  { kind: '结构化', batchSize: 20 },
  { kind: 'URI', batchSize: 5 }
])('$kind 离开页面后不再提交后续测速批次或更新完成数', async ({ kind, batchSize }) => {
  vi.mocked(nodeApi.getAll).mockResolvedValue({ data: Array.from({ length: batchSize + 1 }, (_, i) => ({
    id: `n${i}`, name: `n${i}`, enabled: true,
    ...(kind === 'URI' ? { proxy_string: 'trojan://secret@example.invalid:443' } : { type: 'trojan', server: 'example.invalid', port: 443 })
  })) } as any)
  vi.mocked(api.get).mockResolvedValue({ data: [] } as any)
  let finish!: (value: any) => void
  vi.mocked(nodeApi.testLatency).mockReturnValue(new Promise(resolve => { finish = resolve }))
  await render('list')
  await click('全部测速')
  expect(nodeApi.testLatency).toHaveBeenCalledWith(Array.from({ length: batchSize }, (_, i) => storedKey(`n${i}`)))
  expect(wrapper!.text()).toContain(`正在测速… 0/${batchSize + 1}`)
  const state = (wrapper!.vm as any).$.setupState
  wrapper!.unmount()
  wrapper = undefined
  finish({ data: { results: {} } })
  await flushPromises()
  expect(nodeApi.testLatency).toHaveBeenCalledTimes(1)
  expect(state.completedTests).toBe(0)
})

it('二十一个 URI 节点按五个一批保序提交，完成数随真实响应增加', async () => {
  const records = Array.from({ length: 21 }, (_, i) => ({
    id: `uri-${i}`, name: `uri-${i}`, enabled: true, proxy_string: 'trojan://secret@example.invalid:443'
  }))
  vi.mocked(nodeApi.getAll).mockResolvedValue({ data: records } as any)
  vi.mocked(api.get).mockResolvedValue({ data: [] } as any)
  const finishes: Array<(value: any) => void> = []
  vi.mocked(nodeApi.testLatency).mockImplementation(() => new Promise(resolve => { finishes.push(resolve) }))
  await render('list')
  await click('全部测速')
  for (let index = 0; index < 5; index++) {
    const keys = records.slice(index * 5, (index + 1) * 5).map(node => storedKey(node.id))
    expect(nodeApi.testLatency).toHaveBeenCalledTimes(index + 1)
    expect(nodeApi.testLatency).toHaveBeenNthCalledWith(index + 1, keys)
    expect(wrapper!.text()).toContain(`正在测速… ${index * 5}/21`)
    finishes[index]!({ data: { results: Object.fromEntries(keys.map(key => [key, { latency: 120 }])) } })
    await flushPromises()
  }
  expect(nodeApi.testLatency).toHaveBeenCalledTimes(5)
  expect(wrapper!.text()).toContain('全部测速')
})

it.each(['JSON', 'YAML'] as const)('%s 结构化节点字符串保留二十个一批的探测并发', async format => {
  const records = Array.from({ length: 20 }, (_, i) => ({
    id: `local-${i}`, name: `local-${i}`, enabled: true,
    proxy_string: format === 'JSON'
      ? JSON.stringify({ type: 'trojan', server: 'example.invalid', port: 443, password: 'secret' })
      : 'type: trojan\nserver: example.invalid\nport: 443\npassword: secret'
  }))
  vi.mocked(nodeApi.getAll).mockResolvedValue({ data: records } as any)
  vi.mocked(api.get).mockResolvedValue({ data: [] } as any)
  vi.mocked(nodeApi.testLatency).mockResolvedValue({ data: { results: {} } } as any)
  await render('list')
  await click('全部测速')
  expect(nodeApi.testLatency).toHaveBeenCalledOnce()
  expect(nodeApi.testLatency).toHaveBeenCalledWith(records.map(node => storedKey(node.id)))
})

it('混合节点保序装批，同时限制二十个总节点和五个外部转换节点', async () => {
  const raw = ['trojan://secret@example.invalid:443', 'dHJvamFuOi8vc2VjcmV0', 'type: [', '- type: trojan', '{"server":"example.invalid"}', 'vless://uuid@example.invalid:443']
  const records = Array.from({ length: 35 }, (_, i) => ({
    id: `mixed-${i}`, name: `mixed-${i}`, enabled: true,
    ...(i <= 10 && i % 2 === 0 ? { proxy_string: raw[i / 2] } : { type: 'trojan', server: 'example.invalid', port: 443 })
  }))
  vi.mocked(nodeApi.getAll).mockResolvedValue({ data: records } as any)
  vi.mocked(api.get).mockResolvedValue({ data: [] } as any)
  vi.mocked(nodeApi.testLatency).mockResolvedValue({ data: { results: {} } } as any)
  await render('list')
  await click('全部测速')
  expect(vi.mocked(nodeApi.testLatency).mock.calls.map(([keys]) => keys)).toEqual([
    records.slice(0, 10).map(node => storedKey(node.id)),
    records.slice(10, 30).map(node => storedKey(node.id)),
    records.slice(30).map(node => storedKey(node.id))
  ])
})

describe.each(['card', 'list'] as const)('%s 同名节点测速隔离', view => {
  const name = '同名 香港'
  const keys = [storedKey('same-manual'), storedKey('same-imported'), subscriptionKey('sub-two', name)]
  beforeEach(() => {
    vi.mocked(nodeApi.getAll).mockResolvedValue({ data: [
      { id: 'same-manual', name, enabled: true },
      { id: 'same-imported', name, enabled: true, subscription_id: 'sub-one', subscription_name: '订阅一' }
    ] } as any)
    vi.mocked(api.get).mockResolvedValue({ data: [{ id: 'sub-one' }, { id: 'sub-two' }] } as any)
    vi.mocked(api.post).mockResolvedValue({ data: {
      imported_node_ids: ['same-imported'],
      nodes: [
        { name, type: 'trojan', source_type: 'subscription', source_id: 'sub-one', subscription_name: '订阅一' },
        { name, type: 'trojan', source_type: 'subscription', source_id: 'sub-two', subscription_name: '订阅二' }
      ]
    } } as any)
  })

  function rows() {
    return wrapper!.findAll('[data-name]').filter(row => row.attributes('data-name') === name)
  }

  async function testRow(index: number) {
    const row = rows()[index]!
    const direct = row.find(`button[aria-label="单独测速 ${name}"]`)
    if (direct.exists()) {
      await direct.trigger('click')
    } else {
      await row.get(`button[aria-label="${name} 的更多操作"]`).trigger('keydown', { key: 'Enter' })
      await flushPromises()
      const action = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')]
        .find(item => item.textContent?.trim() === '单独测速')
      expect(action).toBeDefined()
      action!.click()
    }
    await flushPromises()
  }

  it('分别测试手动、导入和另一订阅的同名节点，加载态、延迟、错误和历史互不串用', async () => {
    vi.mocked(nodeApi.latency).mockResolvedValue({ data: { results: {
      [name]: { latency: 999, error: '旧名称缓存不应显示', history: [null] }
    } } } as any)
    await render(view)
    expect(rows()).toHaveLength(3)
    rows().forEach(row => {
      expect(row.text()).toContain('未测速')
      expect(row.text()).not.toContain('旧名称缓存不应显示')
    })
    const records = [
      { latency: 111, history: [111], url: 'https://manual.test/' },
      { latency: null, history: [null], error_code: 'auth', error: '订阅一认证失败', url: 'https://sub-one.test/' },
      { latency: 333, history: [333], url: 'https://sub-two.test/' }
    ]
    for (let index = 0; index < keys.length; index++) {
      let finish!: (value: any) => void
      vi.mocked(nodeApi.testLatency).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
      await testRow(index)
      expect(nodeApi.testLatency).toHaveBeenLastCalledWith([keys[index]])
      expect(wrapper!.text()).toContain('正在测速… 0/1')
      rows().forEach((row, rowIndex) => {
        expect(row.text().includes('正在测速…')).toBe(rowIndex === index)
        expect(row.text()).not.toContain('等待测速')
      })
      finish({ data: { results: { [keys[index]!]: records[index] } } })
      await flushPromises()
    }
    const [manual, imported, subscription] = rows()
    expect(manual!.text()).toContain('111')
    expect(manual!.text()).not.toContain('333')
    expect(imported!.text()).toContain('失败')
    expect(imported!.text()).not.toContain('111')
    expect(imported!.text()).not.toContain('333')
    expect(subscription!.text()).toContain('333')
    expect(subscription!.text()).not.toContain('111')
    rows().forEach((row, index) => {
      const titles = row.findAll('[title]').map(item => item.attributes('title')).join(' ')
      expect(titles).toContain(records[index]!.url)
      expect(titles.includes('订阅一认证失败')).toBe(index === 1)
    })
    if (view === 'card') {
      expect(manual!.findAll('i').at(-1)!.attributes('style')).toContain('height: 63%')
      expect(imported!.findAll('i').at(-1)!.attributes('style')).toContain('height: 18%')
      expect(subscription!.findAll('i').at(-1)!.attributes('style')).toContain('height: 12%')
    }
  })

  it('单独重新测速时用进行中状态替换旧失败，返回后显示新结果', async () => {
    vi.mocked(nodeApi.latency).mockResolvedValue({ data: { results: {
      [keys[1]!]: { latency: null, error_code: 'auth', error: '旧认证失败', history: [null] }
    } } } as any)
    await render(view)
    expect(rows()[1]!.text()).toContain('失败')
    let finish!: (value: any) => void
    vi.mocked(nodeApi.testLatency).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await testRow(1)
    expect(nodeApi.testLatency).toHaveBeenCalledExactlyOnceWith([keys[1]])
    expect(rows()[1]!.text()).toContain('正在测速…')
    expect(rows()[1]!.text()).not.toContain('失败')
    expect(rows()[0]!.text()).toContain('未测速')
    expect(rows()[2]!.text()).toContain('未测速')
    finish({ data: { results: { [keys[1]!]: { latency: 220, history: [null, 220] } } } })
    await flushPromises()
    expect(rows()[1]!.text()).toContain('220')
    expect(rows()[1]!.text()).not.toMatch(/失败|正在测速|等待测速/)
  })

  it('批量包含所有同名来源，不可用结果仅属于对应来源且通知使用节点名称', async () => {
    const success = vi.spyOn(notify, 'success').mockImplementation(() => 0)
    let finish!: (value: any) => void
    vi.mocked(nodeApi.testLatency).mockReturnValue(new Promise(resolve => { finish = resolve }))
    await render(view)
    await click('全部测速')
    expect(nodeApi.testLatency).toHaveBeenCalledWith(keys)
    expect(wrapper!.text()).toContain('正在测速… 0/3')
    finish({ data: { results: {
      [keys[0]!]: { latency: 31 }, [keys[2]!]: { latency: 63 }
    }, missing: [keys[1]] } })
    await flushPromises()
    expect(rows()[0]!.text()).toContain('31')
    expect(rows()[0]!.text()).not.toContain('不可用')
    expect(rows()[1]!.text()).toContain('不可用')
    expect(rows()[2]!.text()).toContain('63')
    expect(rows()[2]!.text()).not.toContain('不可用')
    expect(success).toHaveBeenCalledWith(`测速完成 · 最快 ${name} 31ms`, '1 个已删除或停用')
  })
})
