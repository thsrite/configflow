import { afterEach, describe, expect, it, vi } from 'vitest'
import api, { mihomoPanelApi } from '@/api'
import { cachedPanelData, clearPanelCache, rememberPanelData, rememberPanelProxy } from '@/lib/mihomoPanelCache'

vi.mock('@/router', () => ({ default: { push: vi.fn(), currentRoute: { value: { fullPath: '/' } } } }))
const originalAdapter = api.defaults.adapter
afterEach(() => { api.defaults.adapter = originalAdapter })

describe('mihomo panel cache', () => {
  it('keeps the last reply per device and resource', () => {
    rememberPanelData('gw', 'proxies', { proxies: { HK: { name: 'HK', type: 'Trojan' } } })
    rememberPanelData('gw', 'rules', { rules: [] })
    expect(cachedPanelData('gw', 'proxies')).toEqual({ proxies: { HK: { name: 'HK', type: 'Trojan' } } })
    expect(cachedPanelData('other', 'proxies')).toBeUndefined()
    expect(cachedPanelData('gw', 'providers/proxies')).toBeUndefined()
    rememberPanelData('gw', 'rules', null)
    rememberPanelData('', 'rules', { rules: [1] })
    expect(cachedPanelData('gw', 'rules')).toEqual({ rules: [] })
    clearPanelCache()
    expect(cachedPanelData('gw', 'proxies')).toBeUndefined()
  })

  it('updates a switched group without touching the stored reply object', () => {
    const reply = { proxies: { Proxy: { name: 'Proxy', type: 'Selector', all: ['HK', 'JP'], now: 'HK' } } }
    rememberPanelData('gw', 'proxies', reply)
    rememberPanelProxy('gw', { ...reply.proxies.Proxy, now: 'JP' })
    rememberPanelProxy('gw', { name: 'Unknown', type: 'Selector', now: 'HK' })
    expect(cachedPanelData<typeof reply>('gw', 'proxies')!.proxies).toEqual({ Proxy: { ...reply.proxies.Proxy, now: 'JP' } })
    expect(reply.proxies.Proxy.now).toBe('HK')
    rememberPanelProxy('nothing-read', { name: 'Proxy', type: 'Selector', now: 'JP' })
    expect(cachedPanelData('nothing-read', 'proxies')).toBeUndefined()
  })

  it('stores the slow-changing panel replies as they arrive and leaves live data out', async () => {
    const replies: Record<string, unknown> = {
      version: { version: 'v1.19.0' }, configs: { mode: 'rule' }, proxies: { proxies: {} },
      'providers/proxies': { providers: {} }, rules: { rules: [] }, 'providers/rules': { providers: {} },
      connections: { connections: [] }, flow: { paths: [] },
    }
    api.defaults.adapter = async config => {
      const resource = config.url!.replace('/agents/gw/mihomo/', '')
      return { data: { success: true, data: replies[resource] }, status: 200, statusText: 'OK', headers: {}, config }
    }
    await Promise.all([
      mihomoPanelApi.version('gw'), mihomoPanelApi.configs('gw'), mihomoPanelApi.proxies('gw'), mihomoPanelApi.proxyProviders('gw'),
      mihomoPanelApi.rules('gw'), mihomoPanelApi.ruleProviders('gw'), mihomoPanelApi.connections('gw'), mihomoPanelApi.flow('gw'),
    ])
    for (const resource of ['version', 'configs', 'proxies', 'providers/proxies', 'rules', 'providers/rules'] as const) {
      expect(cachedPanelData('gw', resource)).toEqual(replies[resource])
    }
    expect(cachedPanelData('gw', 'connections' as any)).toBeUndefined()
    expect(cachedPanelData('gw', 'flow' as any)).toBeUndefined()
  })

  it('keeps the previous reply when a read fails', async () => {
    rememberPanelData('gw', 'proxies', { proxies: { HK: { name: 'HK', type: 'Trojan' } } })
    api.defaults.adapter = async config => Promise.reject(Object.assign(new Error('offline'), { config, response: { status: 502, data: { success: false }, config } }))
    await expect(mihomoPanelApi.proxies('gw')).rejects.toBeTruthy()
    expect(cachedPanelData('gw', 'proxies')).toEqual({ proxies: { HK: { name: 'HK', type: 'Trojan' } } })
  })
})
