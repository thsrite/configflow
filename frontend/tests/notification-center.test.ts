import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import NotificationCenter from '@/components/shell/NotificationCenter.vue'
import { notificationsApi, type NotificationItem } from '@/api/notifications'
import { canReloadPage } from '@/lib/pageReloadGuard'

vi.mock('@/api/notifications', () => ({ notificationsApi: { list: vi.fn(), read: vi.fn() } }))
vi.mock('@/stores/profile', async () => {
  const { ref } = await import('vue')
  return { useProfileStore: () => ({ profiles: ref([{ id: 'home', name: '家庭配置' }]) }) }
})
vi.mock('@/lib/pageReloadGuard', () => ({ canReloadPage: vi.fn(() => true) }))
const row: NotificationItem = { id: 'task:subscription:feed', title: '常用订阅 · 订阅更新', message: '连接上游超时',
  state: 'pending', read: false, version: 'v1', severity: 'warning', status_label: '执行失败', scope: 'shared',
  source: 'task', occurrences: 1, last_seen_at: '2026-11-01T12:00:00Z',
  target: { path: '/subscriptions', query: { subscription: 'feed', section: 'settings' }, label: '检查订阅设置' } }
let wrapper: VueWrapper
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(canReloadPage).mockReturnValue(true)
  vi.mocked(notificationsApi.list).mockResolvedValue({ data: { success: true, items: [structuredClone(row)] } } as any)
  vi.mocked(notificationsApi.read).mockResolvedValue({ data: { success: true } } as any)
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
})
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = ''; vi.unstubAllGlobals() })
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: ['/','/subscriptions'].map(path => ({ path, component: { template: '<div />' } })) })
  await router.push('/')
  wrapper = mount(NotificationCenter, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  await wrapper.get('[data-notifications-trigger]').trigger('click')
  await flushPromises()
  return router
}
const button = (label: string) => [...document.querySelectorAll<HTMLButtonElement>('[data-notifications-panel] button')].find(item => item.textContent?.trim() === label)!

it('clears the list and badge when marking all read', async () => {
  await render()
  expect(wrapper.get('[data-notification-count]').text()).toBe('1')
  button('全部已读').click()
  await flushPromises()
  expect(notificationsApi.read).toHaveBeenCalledWith({ [row.id]: 'v1' })
  expect(wrapper.find('[data-notification-count]').exists()).toBe(false)
  expect(document.querySelector('[data-notification]')).toBeNull()
  expect(document.body.textContent).toContain('暂无新通知')
})

it('can clear one notification while keeping other notifications', async () => {
  vi.mocked(notificationsApi.list).mockResolvedValue({ data: { success: true, items: [row, { ...row, id: 'another', version: 'v2' }] } } as any)
  await render()
  button('已读').click()
  await flushPromises()
  expect(notificationsApi.read).toHaveBeenCalledWith({ [row.id]: 'v1' })
  expect(document.querySelector(`[data-notification="${row.id}"]`)).toBeNull()
  expect(document.querySelector('[data-notification="another"]')).not.toBeNull()
  expect(wrapper.get('[data-notification-count]').text()).toBe('1')
})

it('navigates with the exact object and settings section, without an operation request', async () => {
  const router = await render()
  button('检查订阅设置').click()
  await flushPromises()
  expect(router.currentRoute.value.fullPath).toBe('/subscriptions?subscription=feed&section=settings')
  expect(notificationsApi.read).toHaveBeenCalledWith({ [row.id]: 'v1' })
  expect(wrapper.find('[data-notification-count]').exists()).toBe(false)
})

it('keeps the current page and unread state when unsaved work blocks navigation', async () => {
  vi.mocked(canReloadPage).mockReturnValue(false)
  const router = await render()
  button('检查订阅设置').click()
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/')
  expect(notificationsApi.read).not.toHaveBeenCalled()
})

it('preserves the previous pending list when refresh fails', async () => {
  await render()
  vi.mocked(notificationsApi.list).mockRejectedValue(new Error('offline'))
  document.querySelector<HTMLButtonElement>('[aria-label="刷新通知"]')!.click()
  await flushPromises()
  expect(document.querySelector('[role="alert"]')?.textContent).toContain('保留上次结果')
  expect(document.querySelector('[data-notification]')?.textContent).toContain('常用订阅')
  expect(wrapper.get('[data-notification-count]').text()).toBe('1')
})

it('has no history tab and ignores old read or recovered records', async () => {
  vi.mocked(notificationsApi.list).mockResolvedValue({ data: { success: true, items: [{ ...row, state: 'resolved' }, { ...row, id: 'read', read: true }] } } as any)
  await render()
  expect(wrapper.find('[data-notification-count]').exists()).toBe(false)
  expect(document.querySelector('[data-notification]')).toBeNull()
  expect(document.querySelector('[aria-label="通知状态"]')).toBeNull()
  expect(document.body.textContent).toContain('暂无新通知')
})

it('keeps a notification if marking read fails', async () => {
  vi.mocked(notificationsApi.read).mockRejectedValue(new Error('offline'))
  await render()
  button('已读').click()
  await flushPromises()
  expect(wrapper.get('[data-notification-count]').text()).toBe('1')
  expect(document.querySelector('[data-notification]')).not.toBeNull()
})

it('does not restore a cleared item from an in-flight stale refresh, but allows a new failure', async () => {
  await render()
  let finish!: (value: any) => void
  vi.mocked(notificationsApi.list).mockReturnValue(new Promise(resolve => { finish = resolve }))
  document.querySelector<HTMLButtonElement>('[aria-label="刷新通知"]')!.click()
  button('已读').click()
  await flushPromises()
  expect(wrapper.find('[data-notification-count]').exists()).toBe(false)
  finish({ data: { success: true, items: [row] } })
  await flushPromises()
  expect(document.querySelector('[data-notification]')).toBeNull()
  vi.mocked(notificationsApi.list).mockResolvedValue({ data: { success: true, items: [{ ...row, version: 'new-failure' }] } } as any)
  document.querySelector<HTMLButtonElement>('[aria-label="刷新通知"]')!.click()
  await flushPromises()
  expect(wrapper.get('[data-notification-count]').text()).toBe('1')
  expect(document.querySelector('[data-notification]')).not.toBeNull()
})

it('marks only the selected profile and shared notifications as read', async () => {
  vi.mocked(notificationsApi.list).mockResolvedValue({ data: { success: true, items: [
    row, { ...row, id: 'home', scope: 'profile', profile_id: 'home' },
    { ...row, id: 'office', scope: 'profile', profile_id: 'office' },
  ] } } as any)
  await render()
  document.querySelector<HTMLButtonElement>('[aria-label="通知配置空间"]')!
    .dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  await flushPromises()
  const option = [...document.querySelectorAll<HTMLElement>('[role="option"]')].find(item => item.textContent?.includes('家庭配置与共享事项'))!
  option.focus()
  option.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await flushPromises()
  expect(document.querySelectorAll('[data-notification]')).toHaveLength(2)
  button('全部已读').click()
  await flushPromises()
  expect(notificationsApi.read).toHaveBeenCalledWith({ [row.id]: 'v1', home: 'v1' })
  expect(wrapper.get('[data-notification-count]').text()).toBe('1')
})
