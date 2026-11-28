import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from '@/App.vue'
import Login from '@/views/Login.vue'
import Profiles from '@/views/Profiles.vue'
import api, { agentApi, profileApi } from '@/api'
import { setActiveProfileId } from '@/profileContext'
import { useProfileStore } from '@/stores/profile'
import { invalidateAgentSummaries } from '@/lib/agentSummaries'

const authState = vi.hoisted(() => ({ enabled: true }))

vi.mock('@/api', () => ({
  default: { get: vi.fn(async (path: string) => ({ data: path === '/auth/status' ? { authEnabled: authState.enabled } : { enabled: true } })) },
  // The shell's live poll lists Agents through the cheap summary endpoint.
  agentApi: { getAll: vi.fn(async () => ({ data: [] })), summaries: vi.fn(async () => ({ data: [] })) },
  profileApi: { list: vi.fn(async () => ({ data: [{ id: 'default', name: 'Default' }, { id: 'second', name: 'Second' }] })) },
  systemApi: { getVersion: vi.fn(async () => ({ data: { version: 'test' } })) }
}))

const wrappers: VueWrapper[] = []
const originalMatchMedia = Object.getOwnPropertyDescriptor(window, 'matchMedia')
const originalScroll = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
beforeAll(() => {
  // jsdom supplies neither media queries nor scrolling; application components stay real.
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn(), addListener: vi.fn(), removeListener: vi.fn() })) })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
})
afterAll(() => {
  if (originalMatchMedia) Object.defineProperty(window, 'matchMedia', originalMatchMedia)
  else Reflect.deleteProperty(window, 'matchMedia')
  if (originalScroll) Object.defineProperty(Element.prototype, 'scrollIntoView', originalScroll)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})
beforeEach(() => {
  vi.clearAllMocks()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  authState.enabled = true
  invalidateAgentSummaries()
  localStorage.clear()
  sessionStorage.clear()
  setActiveProfileId('default')
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
  vi.useRealTimers()
  document.body.innerHTML = ''
})

async function renderApp(path = '/login') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/login', component: Login }, { path: '/profiles', name: 'Profiles', component: Profiles },
    { path: '/shell-other', component: { render: () => null } }
  ] })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { router, wrapper }
}

describe('authenticated shell startup', () => {
  it('配置空间冷启动只读取一次，再次进入仍刷新', async () => {
    useProfileStore().profileReady.value = false
    const { wrapper, router } = await renderApp('/profiles/')
    expect(profileApi.list).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Second')
    await router.push('/shell-other')
    await router.push('/profiles')
    await flushPromises()
    expect(profileApi.list).toHaveBeenCalledTimes(2)
  })

  it('does not poll protected agents while an unauthenticated login screen is idle', async () => {
    // Keep motion and flushPromises on real timers; only advance the shared poller.
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] })
    await renderApp()
    await vi.advanceTimersByTimeAsync(10_000)
    expect(agentApi.summaries).not.toHaveBeenCalled()
    expect(profileApi.list).not.toHaveBeenCalled()
    expect(api.get).not.toHaveBeenCalledWith('/settings/subscription-aggregation')
  })

  it('starts one poller after login, stops at logout, and restarts after the next login', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] })
    const interval = vi.spyOn(window, 'setInterval')
    const pollerStarts = () => interval.mock.calls.filter(([, delay]) => delay === 5000).length
    const { router } = await renderApp()

    localStorage.setItem('token', 'synthetic-token')
    localStorage.setItem('username', 'Synthetic')
    await router.push('/profiles')
    await flushPromises()
    expect(agentApi.summaries).toHaveBeenCalledTimes(1)
    expect(pollerStarts()).toBe(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(agentApi.summaries).toHaveBeenCalledTimes(2)

    await router.push('/shell-other')
    await flushPromises()
    expect(agentApi.summaries).toHaveBeenCalledTimes(2)
    expect(pollerStarts()).toBe(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(agentApi.summaries).toHaveBeenCalledTimes(3)

    localStorage.removeItem('token')
    localStorage.removeItem('username')
    await router.push('/login')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(10_000)
    expect(agentApi.summaries).toHaveBeenCalledTimes(3)

    localStorage.setItem('token', 'new-synthetic-token')
    await router.push('/shell-other')
    await flushPromises()
    expect(agentApi.summaries).toHaveBeenCalledTimes(4)
    expect(pollerStarts()).toBe(2)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(agentApi.summaries).toHaveBeenCalledTimes(6)
  })

  it('keeps polling application pages without a token when authentication is disabled', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] })
    authState.enabled = false
    await renderApp('/shell-other')
    expect(localStorage.getItem('token')).toBeNull()
    expect(api.get).toHaveBeenCalledWith('/auth/status')
    expect(agentApi.summaries).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(agentApi.summaries).toHaveBeenCalledTimes(3)
  })

  it('never loads protected profiles or aggregation settings on the login screen, and loads after login navigation', async () => {
    const { router } = await renderApp()
    expect(profileApi.list).not.toHaveBeenCalled()
    expect(api.get).not.toHaveBeenCalledWith('/settings/subscription-aggregation')
    expect(document.querySelector('[aria-label="当前配置空间"]')).toBeNull()
    localStorage.setItem('token', 'synthetic-token')
    localStorage.setItem('username', 'Synthetic')
    await router.push('/profiles')
    await flushPromises()
    expect(profileApi.list).toHaveBeenCalled()
    expect(api.get).toHaveBeenCalledWith('/settings/subscription-aggregation')
    expect(document.querySelector('[aria-label="当前配置空间"]')).not.toBeNull()
  })

  it('does not remount a system editor when the tab-local profile changes', async () => {
    const { router } = await renderApp()
    await router.push('/profiles')
    await flushPromises()
    const create = [...document.querySelectorAll('button')].find(button => button.textContent?.trim() === '新建配置空间')
    expect(create).toBeTruthy()
    create!.click()
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('#profile-name')!
    input.value = 'Unsaved global draft'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    setActiveProfileId('second')
    await flushPromises()
    expect(document.querySelector<HTMLInputElement>('#profile-name')?.value).toBe('Unsaved global draft')
  })
})
