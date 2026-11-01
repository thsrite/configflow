import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import Subscriptions from '@/views/Subscriptions.vue'
import { subscriptionApi } from '@/api'
import { notify } from '@/lib/feedback'

vi.mock('@/api', () => ({
  default: { get: vi.fn(async () => ({ data: [] })) },
  subscriptionApi: { getAll: vi.fn(), fetch: vi.fn() }
}))

let wrapper: VueWrapper | undefined
let source: Record<string, unknown>

beforeEach(() => {
  vi.clearAllMocks()
  sessionStorage.clear()
  window.localStorage.removeItem('configflow-subscriptions-view')
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  source = {
    id: 'feed', name: 'Feed', url: 'https://feed.test', type: 'universal', enabled: true,
    cached_node_count: 3, cached_updated_at: '2026-10-19T00:00:00Z', last_fetch: { status: 'ok' }
  }
  vi.mocked(subscriptionApi.getAll).mockImplementation(async () => ({ data: [structuredClone(source)] }) as any)
  vi.mocked(subscriptionApi.fetch).mockResolvedValue({ data: {
    success: true, from_cache: true, cached_count: 3,
    nodes: [{ id: 'node', name: 'Cached node', type: 'ss' }]
  } } as any)
  vi.spyOn(notify, 'success').mockImplementation(() => 0)
  vi.spyOn(notify, 'warning').mockImplementation(() => 0)
  vi.spyOn(notify, 'error').mockImplementation(() => 0)
  vi.spyOn(notify, 'loading').mockImplementation(() => 0)
  vi.spyOn(notify, 'dismiss').mockImplementation(() => undefined)
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  sessionStorage.clear()
  window.localStorage.removeItem('configflow-subscriptions-view')
})

async function render(path = '/') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: Subscriptions }] })
  await router.push(path)
  await router.isReady()
  wrapper = mount(Subscriptions, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  if (!path.includes('section=settings')) await wrapper.get('[aria-label="列表视图"]').trigger('click')
  return wrapper
}

it('opens the exact subscription editor from a notification and focuses its URL', async () => {
  await render('/?subscription=feed&section=settings')
  await flushPromises()
  expect(document.body.textContent).toContain('编辑订阅')
  expect((document.getElementById('sub-name') as HTMLInputElement).value).toBe('Feed')
  expect(document.getElementById('sub-url')?.hasAttribute('data-notification-focus')).toBe(true)
  expect(subscriptionApi.fetch).not.toHaveBeenCalled()
})

it('reports a removed subscription without opening an unrelated editor', async () => {
  await render('/?subscription=removed&section=settings')
  expect(notify.warning).toHaveBeenCalledWith('对应订阅不存在或已删除。')
  expect(document.getElementById('sub-url')).toBeNull()
})

async function clickText(text: string) {
  const button = wrapper!.findAll('button').find(item => item.text() === text)
  expect(button).toBeTruthy()
  await button!.trigger('click')
  await flushPromises()
}

it.each([
  ['cache', '使用缓存'],
  ['fail', '获取失败']
])('restores persisted %s feedback even when cached nodes exist', async (status, label) => {
  source.last_fetch = { status }
  const page = await render()
  expect(page.get('tbody').text()).toContain(label)
  expect(page.get('tbody').text()).not.toContain('正常')
  expect(page.get('tbody').text()).toContain('3')
})

it('keeps a cache fallback visible after a single pull reloads the source list', async () => {
  const page = await render()
  await page.get('[aria-label="卡片视图"]').trigger('click')
  await clickText('拉取')
  expect(subscriptionApi.getAll).toHaveBeenCalledTimes(2)
  expect(notify.warning).toHaveBeenCalledWith('「Feed」拉取失败，已使用缓存')
  expect(notify.success).not.toHaveBeenCalled()
  expect(page.text()).toContain('拉取失败，已使用缓存')
  await page.get('[aria-label="列表视图"]').trigger('click')
  expect(page.get('tbody').text()).toContain('使用缓存')
  expect(page.get('tbody').text()).not.toContain('正常')
})

it('keeps cache fallback feedback after pulling all subscriptions', async () => {
  const page = await render()
  await clickText('拉取全部')
  expect(subscriptionApi.fetch).toHaveBeenCalledWith('feed', true)
  expect(subscriptionApi.getAll).toHaveBeenCalledTimes(2)
  expect(page.get('tbody').text()).toContain('使用缓存')
  expect(page.get('tbody').text()).not.toContain('正常')
})

it('warns when the node preview uses a fallback cache', async () => {
  const page = await render()
  await page.get('[aria-label="获取 Feed 的节点"]').trigger('click')
  await flushPromises()
  expect(notify.warning).toHaveBeenCalledWith('「Feed」拉取失败，已使用缓存')
  expect(document.body.textContent).toContain('Cached node')
  expect(page.get('tbody').text()).toContain('使用缓存')
})

it('returns to normal after a later network pull succeeds', async () => {
  source.last_fetch = { status: 'cache' }
  vi.mocked(subscriptionApi.fetch).mockImplementation(async () => {
    source.last_fetch = { status: 'ok' }
    return { data: { success: true, from_cache: false, cached_count: 3, nodes: [] } } as any
  })
  const page = await render()
  await page.get('[aria-label="卡片视图"]').trigger('click')
  await clickText('拉取')
  expect(notify.success).toHaveBeenCalledWith('「Feed」已更新 · 3 节点')
  expect(notify.warning).not.toHaveBeenCalled()
  await page.get('[aria-label="列表视图"]').trigger('click')
  expect(page.get('tbody').text()).toContain('正常')
  expect(page.get('tbody').text()).not.toContain('使用缓存')
})
