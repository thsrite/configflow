import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { createServer, type Server } from 'node:http'
import { transferableAbortController } from 'node:util'
import { writeFile } from 'node:fs/promises'
import { defineComponent, h, ref } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { useOxiDNSMonitor } from '@/composables/useOxiDNSMonitor'
import OxiDNSClientSelect from '@/components/agents/OxiDNSClientSelect.vue'
import { emptyQueryFilters, type OxiDNSQueryFilters } from '@/types/oxidnsMonitor'

// Only the transport is simulated. Production components issue real HTTP
// requests; explicit expected IDs verify boundaries independently of the clock.
const transport = vi.hoisted(() => ({ origin: '' }))
vi.mock('@/api', () => {
  const get = async (path: string, params: Record<string, unknown> = {}, options: { signal?: AbortSignal } = {}) => {
    const url = new URL(path, transport.origin)
    for (const [key, value] of Object.entries(params)) if (value !== undefined) url.searchParams.set(key, String(value))
    // JSDOM's AbortSignal belongs to a different realm than Node's HTTP fetch.
    const controller = transferableAbortController()
    const abort = () => controller.abort()
    options.signal?.addEventListener('abort', abort, { once: true })
    if (options.signal?.aborted) abort()
    try {
      const result = await fetch(url, { signal: controller.signal })
      if (!result.ok) throw new Error(`Fixture HTTP ${result.status}`)
      return { data: await result.json() }
    } finally { options.signal?.removeEventListener('abort', abort) }
  }
  return { oxidnsMonitorApi: {
    overview: vi.fn((_id, options) => get('/overview', {}, options)),
    queries: vi.fn((_id, params, options) => get('/queries', params, options)),
    clients: vi.fn((_id, params, options) => get('/clients', params, options)),
  } }
})

const agentNow = Date.parse('2026-10-20T12:30:00Z') // 20:30 Asia/Shanghai
const hourIds = Array.from({ length: 56 }, (_, i) => i + 1)
const dayIds = [...hourIds, 57, 58]
const allIds = [61, ...dayIds, 59, 60]
const ages = [
  ...Array.from({ length: 55 }, (_, i) => ({ id: i + 1, age: 60000 + i * 1000, client: i % 2 + 1 })),
  { id: 56, age: 3600000, client: 3 },
  { id: 57, age: 3600001, client: 4 },
  { id: 58, age: 86400000, client: 5 },
  { id: 59, age: 86400001, client: 6 },
  { id: 60, age: 3 * 86400000, client: 7 },
  { id: 61, age: -60000, client: 8 },
]
const rows = ages.map(({ id, age, client }) => ({
  id, created_at_ms: agentNow - age, client_ip: `192.0.2.${client}`, elapsed_ms: 1,
  questions_json: [{ name: `record-${id}.example.test.`, qtype: 'A', qclass: 'IN' }],
  error: null, has_response: true, rcode: 'NOERROR', answer_count: 0, answers_json: [], resolution_source: 'local',
})).sort((a, b) => b.created_at_ms - a.created_at_ms || b.id - a.id)
let collectedAt = '2026-10-20T12:30:00.000000+00:00'
const requests: Array<{ path: string; params: Record<string, string> }> = []
const outcomes: Array<{ offset: number; range: string; kind: string; actual: Array<string | number> }> = []
const wrappers: VueWrapper[] = []
const hiddenDescriptor = Object.getOwnPropertyDescriptor(document, 'hidden')
let server: Server

beforeAll(async () => {
  server = createServer((request, response) => {
    const url = new URL(request.url!, 'http://fixture.local')
    requests.push({ path: url.pathname, params: Object.fromEntries(url.searchParams) })
    const params = url.searchParams
    const filtered = rows.filter(row =>
      (!params.has('since_ms') || row.created_at_ms >= Number(params.get('since_ms'))) &&
      (!params.has('until_ms') || row.created_at_ms <= Number(params.get('until_ms'))))
    let result: unknown
    if (url.pathname === '/overview') result = {
      success: true, agent: { id: 'clock-fixture', name: 'Clock fixture', host: '192.0.2.25', status: 'online' },
      collected_at: collectedAt, core: { status: 'running', version: '1.6.1', uptime_ms: 1000, instance_id: 'fixture' },
      recorders: ['records'], metrics: { requests_total: 0, failed_total: 0, avg_latency_ms: null, inflight: 0, cache_hit_rate: null }, caches: [], servers: [],
    }
    else if (url.pathname === '/queries') {
      const [timestamp, id] = (params.get('cursor') || '').split(':').map(Number)
      const remaining = params.has('cursor') ? filtered.filter(row => row.created_at_ms < timestamp || row.created_at_ms === timestamp && row.id < id) : filtered
      const page = remaining.slice(0, Number(params.get('limit') || 50))
      const last = page.at(-1)
      result = { success: true, recorder: 'records', records: page, next_cursor: remaining.length > page.length && last ? `${last.created_at_ms}:${last.id}` : null }
    } else if (url.pathname === '/clients') {
      const clients = new Map<string, number>()
      for (const row of filtered) clients.set(row.client_ip, (clients.get(row.client_ip) || 0) + 1)
      result = { success: true, recorder: 'records', clients: [...clients].map(([ip, count]) => ({ ip, count })), sample_size: filtered.length, sample_limited: false }
    } else { response.writeHead(404).end(); return }
    response.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' })
    response.end(JSON.stringify(result))
  })
  await new Promise<void>(done => server.listen(0, '127.0.0.1', done))
  const address = server.address()
  if (!address || typeof address === 'string') throw new Error('Missing HTTP fixture address')
  transport.origin = `http://127.0.0.1:${address.port}`
})
afterAll(async () => {
  server.closeAllConnections()
  await new Promise<void>(done => server.close(() => done()))
  if (process.env.CONFIGFLOW_TIME_FIXTURE_REPORT) {
    await writeFile(process.env.CONFIGFLOW_TIME_FIXTURE_REPORT, JSON.stringify({ agentNow, rows, outcomes }, null, 2))
  }
})
beforeEach(() => {
  requests.length = 0
  collectedAt = '2026-10-20T12:30:00.000000+00:00'
  vi.useFakeTimers({ toFake: ['Date'] })
  // Freeze elapsed time for exact inclusive boundaries, keeping HTTP timers real.
  // Separate monitor tests verify advancement when the browser clock changes.
  vi.spyOn(performance, 'now').mockReturnValue(0)
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: !String(query).includes('639px'), addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })))
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.restoreAllMocks(); vi.useRealTimers(); vi.unstubAllGlobals()
  if (hiddenDescriptor) Object.defineProperty(document, 'hidden', hiddenDescriptor)
  else Reflect.deleteProperty(document, 'hidden')
})
// Do not advance fake wall time while waiting for real loopback HTTP I/O.
async function eventually(check: () => void, options = { timeout: 2000, interval: 5 }) {
  for (let elapsed = 0; ; elapsed += options.interval) {
    try { check(); return } catch (error) { if (elapsed >= options.timeout) throw error }
    await new Promise(resolve => setTimeout(resolve, options.interval))
  }
}
async function monitoring(range: OxiDNSQueryFilters['range']) {
  let model!: ReturnType<typeof useOxiDNSMonitor>
  wrappers.push(mount(defineComponent({ setup() { model = useOxiDNSMonitor(ref('clock-fixture'), ref('queries')); return () => h('div') } })))
  await eventually(() => { expect(model.overviewError.value).toBe(''); expect(model.queriesError.value).toBe(''); expect(model.queries.value).not.toBeNull() }, { timeout: 2000, interval: 5 })
  model.autoRefresh.value = false
  if (range !== '1h') {
    model.applyFilters({ ...emptyQueryFilters(), range })
    await eventually(() => { expect(model.overviewError.value).toBe(''); expect(model.queriesError.value).toBe(''); expect(model.queries.value).not.toBeNull() }, { timeout: 2000, interval: 5 })
  }
  return model
}
async function everyRecord(model: ReturnType<typeof useOxiDNSMonitor>) {
  const ids: Array<string | number> = []
  for (let page = 0; page < 3; page++) {
    ids.push(...model.queries.value!.records.map(row => row.id))
    if (!model.hasNext.value) return ids
    model.nextPage()
    await eventually(() => { expect(model.overviewError.value).toBe(''); expect(model.queriesError.value).toBe(''); expect(model.queries.value).not.toBeNull() }, { timeout: 2000, interval: 5 })
  }
  throw new Error('Fixture pagination did not terminate')
}

const ranges = [
  { range: '1h' as const, ids: hourIds, clients: [1, 2, 3] },
  { range: '24h' as const, ids: dayIds, clients: [1, 2, 3, 4, 5] },
  { range: 'all' as const, ids: allIds, clients: [1, 2, 3, 4, 5, 6, 7, 8] },
]
describe.each([-8, 0, 8])('HTTP time fixture: browser clock offset %s hours', offset => {
  it.each(ranges)('$range returns the exact records across pages', async ({ range, ids }) => {
    vi.setSystemTime(agentNow + offset * 3600000)
    const model = await monitoring(range)
    const actual = await everyRecord(model)
    outcomes.push({ offset, range, kind: 'queries', actual })
    expect(actual).toEqual(ids)
    const queryRequests = requests.filter(request => request.path === '/queries' && request.params.cursor)
    expect(queryRequests).toHaveLength(1)
    const original = requests.filter(request => request.path === '/queries').at(-2)!.params
    expect(queryRequests[0].params.since_ms).toBe(original.since_ms)
    expect(queryRequests[0].params.until_ms).toBe(original.until_ms)
    if (range === 'all') {
      expect(original).not.toHaveProperty('since_ms')
      expect(original).not.toHaveProperty('until_ms')
    }
  })
  it.each(ranges)('$range lists exactly the clients in that time window', async ({ range, clients }) => {
    vi.setSystemTime(agentNow + offset * 3600000)
    const picker = mount(OxiDNSClientSelect, { attachTo: document.body, props: {
      modelValue: '', agentId: 'clock-fixture', recorder: 'records', range, collectedAt,
    } })
    wrappers.push(picker)
    await picker.get('[role="combobox"]').trigger('click')
    await eventually(() => {
      expect(requests.some(request => request.path === '/clients')).toBe(true)
      expect(document.body.textContent).not.toContain('正在读取客户端')
      expect(document.querySelector('[role="alert"]')).toBeNull()
    })
    const actual = [...document.querySelectorAll('[role="option"]')].map(node => node.textContent!.trim()).sort()
    outcomes.push({ offset, range, kind: 'clients', actual })
    expect(actual).toEqual(clients.map(client => `192.0.2.${client}`))
    const params = requests.filter(request => request.path === '/clients').at(-1)!.params
    if (range === 'all') {
      expect(params).not.toHaveProperty('since_ms')
      expect(params).not.toHaveProperty('until_ms')
    } else expect(params).toMatchObject({ since_ms: String(agentNow - (range === '1h' ? 3600000 : 86400000)), until_ms: String(agentNow) })
  })
})
it.each(['2026-10-20T20:30:00.000000+08:00', '2026-10-20T14:30:00.000000+02:00'])('normalizes the sample offset in %s without shifting the selected hour', async sample => {
  collectedAt = sample
  vi.setSystemTime(agentNow)
  const model = await monitoring('1h')
  expect(await everyRecord(model)).toEqual(hourIds)
})
