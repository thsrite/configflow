import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import api from '@/api'
import router from '@/router'
import { invalidateAuthSession } from '@/authSession'
import { OXIDNS_DEVICE_STORAGE_KEY, forgetOxiDNSDevice, rememberOxiDNSDevice, rememberedOxiDNSDevice } from '@/lib/oxidnsDevice'

// Only the route guard is under test; the pages have their own component tests.
vi.mock('@/views/Login.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/OxiDNSMonitorEntry.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/OxiDNSMonitor.vue', () => ({ default: { render: () => null } }))

const originalAdapter = api.defaults.adapter
const requests: string[] = []
beforeEach(async () => {
  await router.push('/login')
  localStorage.clear()
  invalidateAuthSession()
  requests.length = 0
  localStorage.setItem('token', 'current-token')
  api.defaults.adapter = async (config: InternalAxiosRequestConfig): Promise<AxiosResponse> => {
    requests.push(config.url || '')
    return { config, data: config.url === '/auth/status' ? { authEnabled: false } : {}, status: 200, statusText: 'OK', headers: {} }
  }
})
afterEach(async () => {
  await router.push('/login')
  api.defaults.adapter = originalAdapter
  localStorage.clear()
})

describe('OxiDNS sidebar entry', () => {
  it('opens the remembered device without reading the device list', async () => {
    rememberOxiDNSDevice('dns/1')
    await router.push('/oxidns')
    expect(router.currentRoute.value.fullPath).toBe('/agents/dns%2F1/oxidns')
    expect(router.currentRoute.value.name).not.toBe('OxiDNSMonitorEntry')
    expect(requests).not.toContain('/agents')
  })

  it('keeps the query when it opens the remembered device', async () => {
    rememberOxiDNSDevice('dns')
    await router.push({ path: '/oxidns', query: { tab: 'queries' } })
    expect(router.currentRoute.value.fullPath).toBe('/agents/dns/oxidns?tab=queries')
  })

  it('stays on the entry page when no device is remembered', async () => {
    await router.push('/oxidns')
    expect(router.currentRoute.value.name).toBe('OxiDNSMonitorEntry')
  })
})

describe('remembered OxiDNS device', () => {
  it('forgets only the device that is gone', () => {
    rememberOxiDNSDevice('kept')
    forgetOxiDNSDevice('other')
    expect(rememberedOxiDNSDevice()).toBe('kept')
    forgetOxiDNSDevice('kept')
    expect(rememberedOxiDNSDevice()).toBe('')
    expect(localStorage.getItem(OXIDNS_DEVICE_STORAGE_KEY)).toBeNull()
  })

  it('treats unavailable storage as nothing remembered', () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    expect(rememberedOxiDNSDevice()).toBe('')
    expect(() => forgetOxiDNSDevice('any')).not.toThrow()
    getItem.mockRestore()
  })
})
