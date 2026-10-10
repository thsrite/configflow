import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import api from '@/api'
import router from '@/router'
import { checkNavigationAuth, invalidateAuthSession } from '@/authSession'
import { notify } from '@/lib/feedback'

// Page rendering has separate component tests; keep the actual router guards,
// Axios instance and request/response interceptors while avoiding UI imports.
vi.mock('@/views/Login.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/Subscriptions.vue', () => ({ default: { render: () => null } }))

const originalAdapter = api.defaults.adapter
const requests: InternalAxiosRequestConfig[] = []
const response = (config: InternalAxiosRequestConfig, data: unknown = {}): AxiosResponse => ({
  config, data, status: 200, statusText: 'OK', headers: {}
})
const paths = () => requests.map(request => request.url)
const count = (path: string) => paths().filter(value => value === path).length
const page = { render: () => null }
router.addRoute({ path: '/navigation-a', component: page })
router.addRoute({ path: '/navigation-b', component: page })
router.addRoute({ path: '/navigation-aggregation', component: page, meta: { requiresSubscriptionAggregation: true } })

beforeEach(async () => {
  await router.push('/login')
  localStorage.clear()
  invalidateAuthSession()
  requests.length = 0
  localStorage.setItem('token', 'current-token')
  localStorage.setItem('username', 'Test')
  api.defaults.adapter = async config => {
    requests.push(config)
    return response(config, config.url === '/auth/status' ? { authEnabled: true } : { enabled: true })
  }
  vi.spyOn(notify, 'error').mockImplementation(() => 0)
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
})

afterEach(async () => {
  await router.push('/login')
  api.defaults.adapter = originalAdapter
  localStorage.clear()
  invalidateAuthSession()
  vi.restoreAllMocks()
})

describe('navigation authentication with the real router and API interceptors', () => {
  it('checks the initial session once and switches subsequent pages without auth round trips', async () => {
    await router.push('/navigation-a')
    await router.push('/navigation-b')
    expect(router.currentRoute.value.path).toBe('/navigation-b')
    expect(paths()).toEqual(['/auth/status', '/auth/verify'])
  })

  it('shares a pending check across rapid navigation and keeps the last destination', async () => {
    let finish!: () => void
    api.defaults.adapter = config => {
      requests.push(config)
      if (config.url === '/auth/status') return new Promise(resolve => {
        finish = () => resolve(response(config, { authEnabled: true }))
      })
      return Promise.resolve(response(config))
    }
    const first = router.push('/navigation-a')
    await flushPromises()
    const second = router.push('/navigation-b')
    await flushPromises()
    expect(paths()).toEqual(['/auth/status'])
    finish()
    await Promise.all([first, second])
    expect(paths()).toEqual(['/auth/status', '/auth/verify'])
    expect(router.currentRoute.value.path).toBe('/navigation-b')
  })

  it('checks a changed token instead of reusing the previous session', async () => {
    await router.push('/navigation-a')
    localStorage.setItem('token', 'replacement-token')
    await router.push('/navigation-b')
    expect(count('/auth/verify')).toBe(2)
    expect(requests.at(-1)?.headers.get('Authorization')).toBe('Bearer replacement-token')
  })

  it('rechecks after the one-minute cache lifetime', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(1_000_000)
    await router.push('/navigation-a')
    now.mockReturnValue(1_060_001)
    await router.push('/navigation-b')
    expect(count('/auth/status')).toBe(2)
    expect(count('/auth/verify')).toBe(2)
  })

  it('never caches a JWT beyond its expiration', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(1_000_000)
    localStorage.setItem('token', `header.${btoa(JSON.stringify({ exp: 1010 }))}.signature`)
    await router.push('/navigation-a')
    now.mockReturnValue(1_010_001)
    await router.push('/navigation-b')
    expect(count('/auth/verify')).toBe(2)
  })

  it('redirects when authentication is enabled and no token exists', async () => {
    localStorage.removeItem('token')
    await router.push('/navigation-a')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(paths()).toEqual(['/auth/status'])
  })

  it('allows and caches auth-disabled navigation without a token verification', async () => {
    localStorage.removeItem('token')
    api.defaults.adapter = async config => {
      requests.push(config)
      return response(config, { authEnabled: false })
    }
    await router.push('/navigation-a')
    await router.push('/navigation-b')
    expect(router.currentRoute.value.path).toBe('/navigation-b')
    expect(paths()).toEqual(['/auth/status'])
  })

  it.each([true, false])('retains aggregation restrictions when authEnabled is %s', async authEnabled => {
    api.defaults.adapter = async config => {
      requests.push(config)
      return response(config, config.url === '/auth/status' ? { authEnabled } : { enabled: false })
    }
    await router.push('/navigation-aggregation')
    expect(router.currentRoute.value.path).toBe('/subscriptions')
    expect(count('/settings/subscription-aggregation')).toBe(1)
    expect(count('/auth/status')).toBe(1)
  })

  it.each([true, false])('retains the saved aggregation fallback %s on temporary errors', async enabled => {
    localStorage.setItem('subscriptionAggregationEnabled', String(enabled))
    api.defaults.adapter = async config => {
      if (config.url === '/settings/subscription-aggregation') throw { config, response: { status: 503 } }
      return response(config, { authEnabled: true })
    }
    await router.push('/navigation-aggregation')
    expect(router.currentRoute.value.path).toBe(enabled ? '/navigation-aggregation' : '/subscriptions')
    expect(localStorage.getItem('token')).toBe('current-token')
  })

  it.each([undefined, 500])('preserves login and retries after a temporary verify failure (%s)', async status => {
    api.defaults.adapter = async config => {
      requests.push(config)
      if (config.url === '/auth/verify') throw { config, response: status ? { status } : undefined }
      return response(config, { authEnabled: true })
    }
    await router.push('/navigation-a')
    await router.push('/navigation-b')
    expect(localStorage.getItem('token')).toBe('current-token')
    expect(router.currentRoute.value.path).toBe('/navigation-b')
    expect(count('/auth/verify')).toBe(2)
    expect(notify.error).not.toHaveBeenCalled()
  })

  it('invalidates a cached session on a business API 401 and verifies a subsequent login', async () => {
    await router.push('/navigation-a')
    const goodAdapter = api.defaults.adapter
    api.defaults.adapter = async config => { throw { config, response: { status: 401 } } }
    await expect(api.get('/nodes')).rejects.toMatchObject({ response: { status: 401 } })
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(localStorage.getItem('token')).toBeNull()
    api.defaults.adapter = goodAdapter
    localStorage.setItem('token', 'current-token')
    await router.push('/navigation-b')
    expect(count('/auth/verify')).toBe(2)
  })

  it('rejects invalid initial tokens without retaining a successful cache', async () => {
    api.defaults.adapter = async config => {
      if (config.url === '/auth/verify') throw { config, response: { status: 401 } }
      return response(config, { authEnabled: true })
    }
    await router.push('/navigation-a')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(localStorage.getItem('token')).toBeNull()
  })

  it('does not let a stale 401 log out a replacement token', async () => {
    let rejectOld!: () => void
    const goodAdapter = api.defaults.adapter
    api.defaults.adapter = config => new Promise((_resolve, reject) => {
      rejectOld = () => reject({ config, response: { status: 401 } })
    })
    const oldRequest = api.get('/nodes')
    localStorage.setItem('token', 'replacement-token')
    api.defaults.adapter = goodAdapter
    await router.push('/navigation-a')
    rejectOld()
    await expect(oldRequest).rejects.toMatchObject({ response: { status: 401 } })
    expect(localStorage.getItem('token')).toBe('replacement-token')
    expect(router.currentRoute.value.path).toBe('/navigation-a')
    expect(notify.error).not.toHaveBeenCalled()
  })

  it('ignores a previous session 401 even if logging back in returns the same token', async () => {
    await router.push('/navigation-a')
    let rejectOld!: () => void
    const goodAdapter = api.defaults.adapter
    api.defaults.adapter = config => new Promise((_resolve, reject) => {
      rejectOld = () => reject({ config, response: { status: 401 } })
    })
    const oldRequest = api.get('/nodes')
    localStorage.removeItem('token')
    await router.push('/login')
    localStorage.setItem('token', 'current-token')
    api.defaults.adapter = goodAdapter
    await router.push('/navigation-b')
    rejectOld()
    await expect(oldRequest).rejects.toMatchObject({ response: { status: 401 } })
    expect(localStorage.getItem('token')).toBe('current-token')
    expect(router.currentRoute.value.path).toBe('/navigation-b')
  })

  it('does not restore cached authentication when logout races a slow verification', async () => {
    let finish!: () => void
    api.defaults.adapter = config => {
      requests.push(config)
      if (config.url === '/auth/verify') return new Promise(resolve => {
        finish = () => resolve(response(config))
      })
      return Promise.resolve(response(config, { authEnabled: true }))
    }
    const checking = checkNavigationAuth(api)
    await flushPromises()
    localStorage.removeItem('token')
    invalidateAuthSession()
    finish()
    expect(await checking).toBe(false)
    localStorage.setItem('token', 'current-token')
    const checkingAgain = checkNavigationAuth(api)
    await flushPromises()
    expect(count('/auth/verify')).toBe(2)
    finish()
    expect(await checkingAgain).toBe(true)
  })

  it('keeps the new route when an old navigation receives a late verification 401', async () => {
    await router.push('/navigation-b')
    invalidateAuthSession()
    let rejectOld!: () => void
    const goodAdapter = api.defaults.adapter
    api.defaults.adapter = config => {
      if (config.url === '/auth/verify') return new Promise((_resolve, reject) => {
        rejectOld = () => reject({ config, response: { status: 401 } })
      })
      return Promise.resolve(response(config, { authEnabled: true }))
    }
    const oldNavigation = router.push('/navigation-a')
    await flushPromises()
    localStorage.removeItem('token')
    await router.push('/login')
    localStorage.setItem('token', 'current-token')
    api.defaults.adapter = goodAdapter
    await router.push('/navigation-b')
    rejectOld()
    await oldNavigation
    expect(router.currentRoute.value.path).toBe('/navigation-b')
    expect(localStorage.getItem('token')).toBe('current-token')
  })
})
