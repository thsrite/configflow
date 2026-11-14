import { afterEach, describe, expect, it, vi } from 'vitest'
import api, { nodeApi } from '@/api'

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))
afterEach(() => { vi.restoreAllMocks() })

describe('node latency API identity', () => {
  it('reads node latency settings from their dedicated endpoint', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, dns_servers: [] } })
    await nodeApi.latencySettings()
    expect(get).toHaveBeenCalledWith('/nodes/latency/settings')
  })

  it('saves only the DNS list without changing node or device settings', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { success: true, dns_servers: [] } })
    await nodeApi.saveLatencySettings(['192.0.2.1:5353'])
    expect(post).toHaveBeenCalledExactlyOnceWith('/nodes/latency/settings', { dns_servers: ['192.0.2.1:5353'] })
  })

  it('saves a DNS mode and checks an unsaved draft through separate endpoints', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { success: true } })
    const dns = { mode: 'custom' as const, servers: ['https://dns.example/dns-query'], bootstrap: ['192.0.2.53:15353'] }
    await nodeApi.saveLatencySettings(dns)
    expect(post).toHaveBeenNthCalledWith(1, '/nodes/latency/settings', { dns })
    await nodeApi.checkLatencyDns(dns)
    expect(post).toHaveBeenNthCalledWith(2, '/nodes/latency/dns/check', { dns }, { timeout: 120000 })
  })

  it('posts opaque source keys without reducing them to display names', async () => {
    const result = { data: { results: {} } }
    const post = vi.spyOn(api, 'post').mockResolvedValue(result)
    const keys = [
      JSON.stringify(['node', 'manual-id']),
      JSON.stringify(['subscription', 'sub / ?#', '同名节点 / "香港"'])
    ]
    expect(await nodeApi.testLatency(keys)).toBe(result)
    expect(post).toHaveBeenCalledWith('/nodes/latency', { keys }, { timeout: 120000 })
  })

  it('keeps an empty selection explicit rather than requesting all nodes', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { results: {} } })
    await nodeApi.testLatency([])
    expect(post).toHaveBeenCalledWith('/nodes/latency', { keys: [] }, { timeout: 120000 })
  })
})
