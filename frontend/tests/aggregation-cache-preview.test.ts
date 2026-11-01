import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import SubscriptionAggregation from '@/views/SubscriptionAggregation.vue'
import api from '@/api'

vi.mock('@/api', () => ({ default: { get: vi.fn(), post: vi.fn() } }))

let wrapper: VueWrapper | undefined
let cached: Record<string, unknown>
let failure: boolean

beforeEach(() => {
  vi.clearAllMocks()
  failure = false
  cached = { success: true, cache_available: true, nodes: [{ id: 'cached', name: 'HK cached', type: 'ss' }] }
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
  vi.mocked(api.get).mockImplementation(async (url) => {
    if (url === '/aggregations') return { data: [{ id: 'a', name: 'Aggregate', subscriptions: ['s'], nodes: [] }] }
    if (url === '/subscriptions') return { data: [{ id: 's', name: 'Feed', interval: 0 }] }
    if (url === '/nodes') return { data: [] }
    if (url === '/aggregations/a/count') return { data: { success: true, total_count: 1 } }
    if (url === '/aggregations/a/preview') return { data: {
      success: true, nodes: [{ name: 'Aggregate node', type: 'ss' }], subscription_node_counts: { s: 1 }
    } }
    if (url === '/subscriptions/s/nodes') {
      if (failure) throw new Error('cache read failed')
      return { data: cached }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
})

afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

async function click(text: string) {
  const button = Array.from(document.querySelectorAll('button')).find(item => item.textContent?.trim() === text)
  expect(button).toBeTruthy()
  button!.click()
  await flushPromises()
}

async function openMember() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: SubscriptionAggregation }] })
  await router.push('/')
  await router.isReady()
  wrapper = mount(SubscriptionAggregation, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  await click('预览节点')
  const source = Array.from(document.querySelectorAll('button')).find(item => item.textContent?.includes('Feed'))
  expect(source).toBeTruthy()
  source!.click()
  await flushPromises()
}

it('reads a manual-only source through the existing nodes endpoint without posting a fetch', async () => {
  await openMember()
  expect(api.get).toHaveBeenCalledWith('/subscriptions/s/nodes', { params: { source: 'cache' } })
  expect(document.body.textContent).toContain('HK cached')
  expect(document.body.textContent).toContain('共 1 个缓存节点')
  expect(api.post).not.toHaveBeenCalled()
})

it.each([
  [false, '暂无可用缓存', '请先到「订阅来源」手动拉取'],
  [true, '缓存中暂无匹配节点', '请检查订阅自身的节点筛选条件'],
])('explains an empty cache (available=%s) without downloading', async (available, title, description) => {
  cached = { success: true, cache_available: available, nodes: [] }
  await openMember()
  expect(document.body.textContent).toContain(title)
  expect(document.body.textContent).toContain(description)
  expect(api.post).not.toHaveBeenCalled()
})

it('shows a cache read error without retrying upstream', async () => {
  failure = true
  await openMember()
  expect(document.querySelector('[role="alert"]')?.textContent).toContain('读取缓存失败')
  expect(document.body.textContent).not.toContain('暂无可用缓存')
  expect(api.post).not.toHaveBeenCalled()
})
