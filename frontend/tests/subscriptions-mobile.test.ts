import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import Subscriptions from '@/views/Subscriptions.vue'
import api, { subscriptionApi } from '@/api'
import { notify } from '@/lib/feedback'

vi.mock('@/api', () => ({
  default: { get: vi.fn(async () => ({ data: [] })) },
  subscriptionApi: { getAll: vi.fn(), fetch: vi.fn(), update: vi.fn() }
}))

let wrapper: VueWrapper | undefined
let sources: Record<string, unknown>[]

// 手机宽度：< 640px 同时也 < 900px（卡片视图），并关闭数字滚动动画
const phoneMatchMedia = (query: string) => ({
  matches: ['639px', '899.98px', 'prefers-reduced-motion'].some(part => String(query).includes(part)),
  media: query,
  addEventListener: vi.fn(), removeEventListener: vi.fn(), addListener: vi.fn(), removeListener: vi.fn()
})

beforeEach(() => {
  vi.clearAllMocks()
  window.localStorage.removeItem('configflow-subscriptions-view')
  vi.stubGlobal('matchMedia', vi.fn(phoneMatchMedia))
  sources = [{
    id: 'feed', name: 'Feed', url: 'https://feed.test/sub', type: 'mihomo', enabled: true, interval: 0,
    cached_node_count: 150, cached_updated_at: '2026-10-19T00:00:00Z', last_fetch: { status: 'ok' }
  }]
  vi.mocked(subscriptionApi.getAll).mockImplementation(async () => ({ data: structuredClone(sources) }) as any)
  vi.spyOn(notify, 'success').mockImplementation(() => 0)
  vi.spyOn(notify, 'warning').mockImplementation(() => 0)
  vi.spyOn(notify, 'error').mockImplementation(() => 0)
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  window.localStorage.removeItem('configflow-subscriptions-view')
})

async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: Subscriptions }] })
  await router.push('/')
  await router.isReady()
  wrapper = mount(Subscriptions, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return wrapper
}

const card = (id = 'feed') => wrapper!.get(`[data-reorder-item][data-id="${id}"]`)

async function openMenu(name: string) {
  await wrapper!.get(`button[aria-label="${name} 的更多操作"]`).trigger('keydown', { key: 'Enter' })
  await flushPromises()
  return [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')]
}

it('手机上用紧凑卡片：小流量环、单行统计，去掉三列统计和内联编辑', async () => {
  await render()
  const c = card()
  expect(c.find('.card-compact-actions').exists()).toBe(true)
  expect(c.find('.card-compact-summary').exists()).toBe(true)
  expect(c.find('.card-section').exists()).toBe(false)
  expect(c.find('.card-stats').classes()).toContain('size-10')
  expect(c.find('.card-stats').attributes('title')).toBe('不计量')
  expect(c.find('h3').classes()).not.toContain('font-display')
  expect(c.find('[aria-label="编辑 Feed"]').exists()).toBe(false)
  const summary = c.get('.card-compact-summary').text().replace(/\s+/g, ' ')
  expect(summary).toContain('150 节点')
  expect(summary).toContain('剩余 —')
  expect(summary).toContain('仅手动')
  expect(summary).toContain('上次拉取')
  // 内联图标按钮重置窄屏 44px 最小尺寸，触区交给 ::before
  const pull = c.get('button[aria-label="拉取 Feed"]')
  expect(pull.classes()).toEqual(expect.arrayContaining(['size-8', 'max-md:min-h-0', 'max-md:min-w-0']))
  expect(pull.classes()).not.toContain('max-md:min-h-11')
  // 添加订阅源的虚线卡片保持不变
  expect(wrapper!.findAll('button').some(b => b.text() === '添加订阅来源')).toBe(true)
})

it('编辑收进「⋯」菜单，打开同一个编辑对话框', async () => {
  await render()
  const items = await openMenu('Feed')
  expect(items.map(item => item.textContent?.trim())).toEqual(['编辑', '查看节点', '复制地址', '停用', '删除'])
  items[0]!.click()
  await flushPromises()
  expect(document.body.textContent).toContain('编辑订阅')
  expect((document.getElementById('sub-name') as HTMLInputElement).value).toBe('Feed')
  expect(api.get).toHaveBeenCalledWith('/subscriptions/feed/health-impact')
})

it('拉取图标按钮触发拉取，进行中禁用并旋转', async () => {
  let resolve!: (value: unknown) => void
  vi.mocked(subscriptionApi.fetch).mockReturnValue(new Promise(r => { resolve = r }) as any)
  await render()
  await card().get('button[aria-label="拉取 Feed"]').trigger('click')
  await flushPromises()
  expect(subscriptionApi.fetch).toHaveBeenCalledWith('feed', true)
  const pull = card().get('button[aria-label="拉取 Feed"]')
  expect(pull.attributes('disabled')).toBeDefined()
  expect(pull.attributes('aria-busy')).toBe('true')
  expect(pull.get('svg').classes()).toContain('animate-spin')
  expect(card().find('.sub-sweep').exists()).toBe(true)
  resolve({ data: { success: true, from_cache: false, cached_count: 151, nodes: [] } })
  await flushPromises()
  expect(notify.success).toHaveBeenCalledWith('「Feed」已更新 · 151 节点')
  expect(card().get('button[aria-label="拉取 Feed"]').attributes('disabled')).toBeUndefined()
})

it('失败、缓存、到期、余量不足和停用状态在紧凑卡片里仍然可见', async () => {
  const expire = Math.floor(Date.now() / 1000) + 3 * 86400
  sources = [
    { id: 'bad', name: 'Bad', url: 'https://bad.test', type: 'universal', enabled: false, interval: 86400,
      cached_node_count: 2, last_fetch: { status: 'fail' },
      traffic: { upload: 0, download: 95 * 2 ** 30, total: 100 * 2 ** 30, expire } },
    { id: 'old', name: 'Old', url: 'https://old.test', type: 'universal', enabled: true, interval: 86400,
      cached_node_count: 4, last_fetch: { status: 'cache' } }
  ]
  await render()
  const bad = card('bad')
  expect(bad.classes()).toContain('opacity-60')
  const chips = bad.findAll('.card-compact-summary .chip')
  const chip = (text: string) => chips.find(item => item.text().trim() === text)
  expect(chip('拉取失败')?.classes()).toContain('chip-bad')
  expect(chip('余量不足')?.classes()).toContain('chip-bad')
  expect(chip('3 天后到期')?.classes()).toContain('chip-warn')
  expect(chip('已停用')).toBeTruthy()
  expect(bad.get('.card-stats').attributes('title')).toBe('已用 95%')
  expect(bad.get('.card-compact-summary').text().replace(/\s+/g, ' ')).toContain('剩余 5.0 GB')
  expect(bad.get('.card-compact-summary').text()).toContain('每 1 天更新')
  const cache = card('old').findAll('.card-compact-summary .chip').find(item => item.text().trim() === '拉取失败，已使用缓存')
  expect(cache?.classes()).toContain('chip-warn')
})

it('排序模式仍显示拖动手柄，不渲染紧凑操作', async () => {
  sources.push({ id: 'two', name: 'Two', url: 'https://two.test', type: 'universal', enabled: true, cached_node_count: 1 })
  await render()
  await wrapper!.findAll('button').find(b => b.text() === '调整顺序')!.trigger('click')
  await flushPromises()
  expect(card().find('.card-compact-actions').exists()).toBe(false)
  expect(card().find('.card-compact-summary').exists()).toBe(false)
  expect(wrapper!.findAll('[data-reorder-item]')).toHaveLength(2)
  expect(card().find('[aria-label^="拖动 Feed"]').exists()).toBe(true)
})
