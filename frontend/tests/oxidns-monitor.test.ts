import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { oxidnsMonitorApi } from '@/api'
import { forgetOxiDNSRateSamples, useOxiDNSMonitor } from '@/composables/useOxiDNSMonitor'
import { emptyQueryFilters, isAgentProbe, formatDNSCode, querySourceLabel, type OxiDNSOverview } from '@/types/oxidnsMonitor'
import OxiDNSQueryTable from '@/components/agents/OxiDNSQueryTable.vue'
import OxiDNSQueryDetail from '@/components/agents/OxiDNSQueryDetail.vue'
import OxiDNSMetric from '@/components/agents/OxiDNSMetric.vue'
import { labelOfPath, navigationPath, scopeOfPath } from '@/navigation'

vi.mock('@/api', () => ({ oxidnsMonitorApi: { overview: vi.fn(), queries: vi.fn(), query: vi.fn(), resetStatistics: vi.fn() } }))
const wrappers: VueWrapper[] = []
const hiddenDescriptor = Object.getOwnPropertyDescriptor(document, 'hidden')
const snapshot = (count: number | null = 100, at = '2026-10-17T01:00:00Z', instance = 'core-one'): OxiDNSOverview => ({
  success: true, agent: { id: 'one', name: 'OxiDNS', host: '192.0.2.13', profile_id: 'default', version: '1.7.0-go', status: 'online' },
  collected_at: at, core: { version: '1.6.1', status: 'running', uptime_ms: 120000, instance_id: instance },
  recorders: ['queries'], metrics: { requests_total: count, failed_total: 2, avg_latency_ms: 0, inflight: 0, cache_hit_rate: 75 },
  caches: [], servers: []
})
const periodSnapshot = (generation = 'period-one', count = 100, at = '2026-10-17T01:00:00Z'): OxiDNSOverview => ({
  ...snapshot(count, at), statistics: { generation, started_at_ms: Date.parse(at), reason: 'manual', partial: false }
})
const queryRecord = { id: 12, created_at_ms: 1700000000000, elapsed_ms: 0, client_ip: '192.0.2.5',
  questions_json: [{ name: 'example.test', qtype: 1, qclass: 1 }], error: null, has_response: true, rcode: 0,
  answer_count: 1, answers_json: [{ name: 'example.test', rr_type: 1, ttl: 60, payload_text: '192.0.2.1' }] }
const records = (next: string | null = null) => ({ success: true, recorder: 'queries', records: [queryRecord], next_cursor: next })
const response = (data: any) => ({ data } as any)
function deferred() {
  let resolve!: (value: any) => void
  let reject!: (value: any) => void
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function harness(initialTab = 'overview') {
  const id = ref('one')
  const tab = ref(initialTab)
  let model!: ReturnType<typeof useOxiDNSMonitor>
  const wrapper = mount(defineComponent({ setup() { model = useOxiDNSMonitor(id, tab); return () => h('div') } }))
  wrappers.push(wrapper)
  return { id, tab, model, wrapper }
}
beforeEach(() => {
  vi.clearAllMocks()
  forgetOxiDNSRateSamples()
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })))
  vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(snapshot()))
  vi.mocked(oxidnsMonitorApi.queries).mockResolvedValue(response(records()))
  vi.mocked(oxidnsMonitorApi.query).mockResolvedValue(response({ success: true, recorder: 'queries', record: { ...queryRecord, steps: [] } }))
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.useRealTimers()
  vi.unstubAllGlobals()
  if (hiddenDescriptor) Object.defineProperty(document, 'hidden', hiddenDescriptor)
  else Reflect.deleteProperty(document, 'hidden')
  document.body.innerHTML = ''
})

describe('OxiDNS monitoring requests', () => {
  it('takes one early second sample, then keeps the normal ten-second cadence', async () => {
    vi.useFakeTimers()
    vi.mocked(oxidnsMonitorApi.overview)
      .mockResolvedValueOnce(response(snapshot()))
      .mockResolvedValueOnce(response(snapshot(105, '2026-10-17T01:00:01Z')))
      .mockResolvedValue(response(snapshot(150, '2026-10-17T01:00:10Z')))
    const { model } = harness()
    await flushPromises()
    expect(model.qps.value).toBeNull()
    await vi.advanceTimersByTimeAsync(499)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(model.qps.value).toBe(5)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(9499)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(3)
    expect(model.qps.value).toBe(5)
    expect(oxidnsMonitorApi.queries).not.toHaveBeenCalled()
  })
  it('reopening a device within the sample gap shows its rate at once', async () => {
    vi.useFakeTimers()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValueOnce(response(snapshot()))
    const first = harness()
    await flushPromises()
    first.wrapper.unmount()
    // Reopened 3 s later: the remembered sample gives the rate, no early sample is taken.
    await vi.advanceTimersByTimeAsync(3000)
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValueOnce(response(snapshot(130, '2026-10-17T01:00:03Z')))
    const { model } = harness()
    await flushPromises()
    expect(model.qps.value).toBe(10)
    await vi.advanceTimersByTimeAsync(2000)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(2)
  })
  it('samples again when the remembered sample is too old or from another core', async () => {
    vi.useFakeTimers()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValueOnce(response(snapshot()))
    const first = harness()
    await flushPromises()
    first.wrapper.unmount()
    await vi.advanceTimersByTimeAsync(31000)
    vi.mocked(oxidnsMonitorApi.overview)
      .mockResolvedValueOnce(response(snapshot(400, '2026-10-17T01:00:31Z')))
      .mockResolvedValueOnce(response(snapshot(410, '2026-10-17T01:00:32Z')))
    const late = harness()
    await flushPromises()
    expect(late.model.qps.value).toBeNull()
    await vi.advanceTimersByTimeAsync(500)
    expect(late.model.qps.value).toBe(10)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(3)
    late.wrapper.unmount()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValueOnce(response(snapshot(5, '2026-10-17T01:00:33Z', 'core-two')))
    const restarted = harness()
    await flushPromises()
    expect(restarted.model.qps.value).toBeNull()
  })
  it('cancels the early sample if a manual refresh already supplies a valid rate', async () => {
    vi.useFakeTimers()
    const { model } = harness()
    await flushPromises()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(snapshot(101, '2026-10-17T01:00:00.500Z')))
    await model.refresh()
    await vi.advanceTimersByTimeAsync(2000)
    expect(model.qps.value).toBe(2)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(2)
  })
  it('does not repeatedly retry an unavailable rate or a failed early sample', async () => {
    vi.useFakeTimers()
    vi.mocked(oxidnsMonitorApi.overview)
      .mockResolvedValueOnce(response(snapshot()))
      .mockRejectedValueOnce(new Error('temporarily unavailable'))
      .mockResolvedValue(response(snapshot(150, '2026-10-17T01:00:10Z')))
    const { model } = harness()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(9000)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(2)
    expect(model.qps.value).toBeNull()
    expect(model.overviewError.value).toBe('temporarily unavailable')
    await vi.advanceTimersByTimeAsync(1000)
    expect(model.qps.value).toBe(5)
    expect(model.overviewError.value).toBe('')
  })
  it('waits for usable counters and does not loop on duplicate snapshots', async () => {
    vi.useFakeTimers()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(snapshot(null)))
    const { model } = harness()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(1)
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(snapshot()))
    await model.refresh()
    await vi.advanceTimersByTimeAsync(3000)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(3)
    expect(model.qps.value).toBeNull()
  })
  it('cancels an early sample when paused or unmounted and refreshes when resumed', async () => {
    vi.useFakeTimers()
    const { model, wrapper } = harness()
    await flushPromises()
    model.autoRefresh.value = false
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(1)
    model.autoRefresh.value = true
    await flushPromises()
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(2000)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(2)
  })
  it('does not turn a long time in the background into a recent request rate', async () => {
    vi.useFakeTimers()
    const { model } = harness()
    await flushPromises()
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(60000)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(1)
    vi.mocked(oxidnsMonitorApi.overview)
      .mockResolvedValueOnce(response(snapshot(400, '2026-10-17T01:01:00Z')))
      .mockResolvedValueOnce(response(snapshot(407, '2026-10-17T01:01:01Z')))
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(model.qps.value).toBeNull()
    await vi.advanceTimersByTimeAsync(1000)
    expect(model.qps.value).toBe(7)
  })
  it('discards an old early sample after switching devices and warms up the new device', async () => {
    vi.useFakeTimers()
    const { id, model } = harness()
    await flushPromises()
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.overview).mockImplementationOnce(() => pending.promise as any)
    await vi.advanceTimersByTimeAsync(1000)
    const oldSignal = vi.mocked(oxidnsMonitorApi.overview).mock.calls[1][1]?.signal
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValueOnce(response(snapshot(1000, '2026-10-17T01:00:10Z', 'core-two')))
    id.value = 'second'
    await flushPromises()
    expect(oldSignal?.aborted).toBe(true)
    pending.resolve(response(snapshot(150, '2026-10-17T01:00:01Z')))
    await flushPromises()
    expect(model.overview.value?.core.instance_id).toBe('core-two')
    expect(model.qps.value).toBeNull()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValueOnce(response(snapshot(1003, '2026-10-17T01:00:11Z', 'core-two')))
    await vi.advanceTimersByTimeAsync(1000)
    expect(model.qps.value).toBe(3)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(4)
  })
  it('does not request records until the query tab is opened, and never requests an absent recorder', async () => {
    const { tab, model } = harness()
    await flushPromises()
    expect(oxidnsMonitorApi.queries).not.toHaveBeenCalled()
    tab.value = 'queries'
    await flushPromises()
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(1)
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response({ ...snapshot(), recorders: [] }))
    await model.refresh()
    expect(model.recorder.value).toBe('')
    expect(model.queries.value).toBeNull()
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(1)
  })
  it.each([-8, 8])('finds recent records when the browser clock differs from the Agent by %s hours', async hours => {
    vi.useFakeTimers()
    const agentNow = Date.parse('2026-10-20T12:30:00Z')
    vi.setSystemTime(agentNow + hours * 3600000)
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(snapshot(100, new Date(agentNow).toISOString())))
    const recentRecord = { ...queryRecord, created_at_ms: agentNow - 5 * 60000 }
    vi.mocked(oxidnsMonitorApi.queries).mockImplementation(async (_id, params) => response({ ...records(), records:
      (params.since_ms === undefined || recentRecord.created_at_ms >= Number(params.since_ms)) &&
      (params.until_ms === undefined || recentRecord.created_at_ms <= Number(params.until_ms)) ? [recentRecord] : [] }))
    const { model } = harness('queries')
    await flushPromises()
    expect(model.queries.value?.records).toEqual([recentRecord])
    expect(oxidnsMonitorApi.queries).toHaveBeenLastCalledWith('one', expect.objectContaining({ since_ms: agentNow - 3600000, until_ms: agentNow }), expect.anything())
    model.applyFilters({ ...emptyQueryFilters(), range: '24h' })
    await flushPromises()
    expect(oxidnsMonitorApi.queries).toHaveBeenLastCalledWith('one', expect.objectContaining({ since_ms: agentNow - 86400000, until_ms: agentNow }), expect.anything())
  })
  it('does not cap all-time records at a browser clock that may be behind the Agent', async () => {
    const { model } = harness('queries')
    await flushPromises()
    model.applyFilters({ ...emptyQueryFilters(), range: 'all' })
    await flushPromises()
    expect(oxidnsMonitorApi.queries).toHaveBeenLastCalledWith('one', expect.objectContaining({ since_ms: undefined, until_ms: undefined }), expect.anything())
  })
  it('advances the Agent time monotonically while paused, regardless of browser clock corrections', async () => {
    vi.useFakeTimers()
    const agentNow = Date.parse('2026-10-20T12:30:00Z')
    vi.setSystemTime(agentNow)
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(snapshot(100, new Date(agentNow).toISOString())))
    const { model } = harness('queries')
    await flushPromises()
    model.autoRefresh.value = false
    await flushPromises()
    vi.setSystemTime(agentNow + 8 * 3600000)
    await vi.advanceTimersByTimeAsync(60000)
    model.applyFilters(emptyQueryFilters())
    await flushPromises()
    expect(oxidnsMonitorApi.queries).toHaveBeenLastCalledWith('one', expect.objectContaining({ until_ms: agentNow + 60000 }), expect.anything())
  })
  it('computes QPS only from matching core instances and nondecreasing real counters', async () => {
    const { model } = harness()
    await flushPromises()
    expect(model.qps.value).toBeNull()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(snapshot(150, '2026-10-17T01:00:10Z')))
    await model.refresh()
    expect(model.qps.value).toBe(5)
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(snapshot(180, '2026-10-17T01:00:20Z', 'new-core')))
    await model.refresh()
    expect(model.qps.value).toBeNull()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(snapshot(null, '2026-10-17T01:00:30Z', 'new-core')))
    await model.refresh()
    expect(model.qps.value).toBeNull()
  })
  it('keeps successful overview data when refresh fails, without pretending it is fresh', async () => {
    const { model } = harness()
    await flushPromises()
    vi.mocked(oxidnsMonitorApi.overview).mockRejectedValue({ response: { data: { code: 'CORE_UNREACHABLE', message: '无法连接 OxiDNS 核心' } } })
    await model.refresh()
    expect(model.overview.value?.collected_at).toBe('2026-10-17T01:00:00Z')
    expect(model.overviewError.value).toBe('无法连接 OxiDNS 核心')
  })
  it('preserves cursor history and its time window while background refresh leaves old pages alone', async () => {
    vi.mocked(oxidnsMonitorApi.queries).mockResolvedValue(response(records('1700000000000:12')))
    const { model } = harness('queries')
    await flushPromises()
    const firstParams = vi.mocked(oxidnsMonitorApi.queries).mock.calls[0][1]
    model.nextPage()
    await flushPromises()
    expect(model.page.value).toBe(2)
    const nextParams = vi.mocked(oxidnsMonitorApi.queries).mock.calls[1][1]
    expect(nextParams.cursor).toBe('1700000000000:12')
    expect(nextParams.until_ms).toBe(firstParams.until_ms)
    await model.refresh()
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(2)
    expect(model.page.value).toBe(2)
    await model.refresh(true)
    expect(vi.mocked(oxidnsMonitorApi.queries).mock.calls[2][1].cursor).toBe('1700000000000:12')
    model.previousPage()
    await flushPromises()
    expect(model.page.value).toBe(1)
    expect(vi.mocked(oxidnsMonitorApi.queries).mock.calls[3][1].until_ms).toBe(firstParams.until_ms)
  })
  it('aborts old filters and ignores their late results', async () => {
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.queries).mockImplementationOnce(() => pending.promise as any)
    const { model } = harness('queries')
    await flushPromises()
    const oldSignal = vi.mocked(oxidnsMonitorApi.queries).mock.calls[0][2]?.signal
    model.applyFilters({ ...emptyQueryFilters(), qname: 'current.test', status: 'error' })
    await flushPromises()
    expect(oldSignal?.aborted).toBe(true)
    expect(vi.mocked(oxidnsMonitorApi.queries).mock.calls[1][1]).toMatchObject({ qname: 'current.test', status: 'error', limit: 50 })
    pending.resolve(response({ ...records(), records: [{ ...queryRecord, id: 99 }] }))
    await flushPromises()
    expect(model.queries.value?.records[0].id).toBe(12)
  })
  it('stops automatic refresh while hidden or paused and clears requests on unmount', async () => {
    vi.useFakeTimers()
    const { model, wrapper } = harness()
    await flushPromises()
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    await vi.advanceTimersByTimeAsync(20000)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(1)
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(2)
    model.autoRefresh.value = false
    await vi.advanceTimersByTimeAsync(20000)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(20000)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(2)
  })
  it('does not start a query request after an in-flight refresh is unmounted', async () => {
    const { model, wrapper } = harness('queries')
    await flushPromises()
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.overview).mockImplementationOnce(() => pending.promise as any)
    const refreshing = model.refresh(true)
    const queryCount = vi.mocked(oxidnsMonitorApi.queries).mock.calls.length
    wrapper.unmount()
    pending.resolve(response(snapshot()))
    await refreshing
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(queryCount)
  })
  it('resets data and ignores old responses when moving to a different Agent', async () => {
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.overview).mockImplementationOnce(() => pending.promise as any)
    const { id, model } = harness()
    await flushPromises()
    const oldSignal = vi.mocked(oxidnsMonitorApi.overview).mock.calls[0][1]?.signal
    id.value = 'second'
    await flushPromises()
    expect(oldSignal?.aborted).toBe(true)
    pending.resolve(response({ ...snapshot(), agent: { ...snapshot().agent, name: 'Stale Agent' } }))
    await flushPromises()
    expect(model.overview.value?.agent.name).not.toBe('Stale Agent')
    expect(oxidnsMonitorApi.overview).toHaveBeenLastCalledWith('second', expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })
})

describe('OxiDNS dashboard resets', () => {
  it('starts a new early sample after reset without comparing against the previous period', async () => {
    vi.useFakeTimers()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(periodSnapshot()))
    vi.mocked(oxidnsMonitorApi.resetStatistics).mockResolvedValue(response(periodSnapshot('period-two', 0)))
    const { model } = harness()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(250)
    expect(await model.resetStatistics()).toBe(true)
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(periodSnapshot('period-two', 2, '2026-10-17T01:00:01Z')))
    await vi.advanceTimersByTimeAsync(250)
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(1)
    expect(model.qps.value).toBeNull()
    await vi.advanceTimersByTimeAsync(250)
    expect(model.qps.value).toBe(2)
  })
  it('starts a fresh period without deleting records, filters, or query pagination', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(periodSnapshot()))
    vi.mocked(oxidnsMonitorApi.queries).mockResolvedValue(response(records('1700000000000:12')))
    vi.mocked(oxidnsMonitorApi.resetStatistics).mockResolvedValue(response(periodSnapshot('period-two', 0)))
    const { model } = harness('queries')
    await flushPromises()
    model.applyFilters({ ...emptyQueryFilters(), qname: 'kept.test' })
    await flushPromises()
    model.nextPage()
    await flushPromises()
    const priorRecords = model.queries.value
    const queryCalls = vi.mocked(oxidnsMonitorApi.queries).mock.calls.length
    expect(await model.resetStatistics()).toBe(true)
    expect(oxidnsMonitorApi.resetStatistics).toHaveBeenCalledWith('one', 'queries', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(model.overview.value?.statistics?.generation).toBe('period-two')
    expect(model.overview.value?.metrics.requests_total).toBe(0)
    expect(model.qps.value).toBeNull()
    expect(model.queries.value).toBe(priorRecords)
    expect(model.filters.value.qname).toBe('kept.test')
    expect(model.page.value).toBe(2)
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(queryCalls)
  })

  it('discards a pre-reset overview response arriving after the reset', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(periodSnapshot()))
    const { model } = harness()
    await flushPromises()
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.overview).mockImplementationOnce(() => pending.promise as any)
    const refreshing = model.refresh()
    const oldSignal = vi.mocked(oxidnsMonitorApi.overview).mock.calls.at(-1)![1]?.signal
    vi.mocked(oxidnsMonitorApi.resetStatistics).mockResolvedValue(response(periodSnapshot('period-two', 0)))
    expect(await model.resetStatistics()).toBe(true)
    expect(oldSignal?.aborted).toBe(true)
    pending.resolve(response(periodSnapshot('period-one', 900)))
    await refreshing
    expect(model.overview.value?.statistics?.generation).toBe('period-two')
    expect(model.overview.value?.metrics.requests_total).toBe(0)
  })

  it('keeps data on reset failure and prevents duplicate resets while pending', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(periodSnapshot()))
    const { model } = harness()
    await flushPromises()
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.resetStatistics).mockImplementationOnce(() => pending.promise as any)
    const resetting = model.resetStatistics()
    expect(model.resettingStatistics.value).toBe(true)
    expect(await model.resetStatistics()).toBe(false)
    await model.refresh()
    expect(oxidnsMonitorApi.overview).toHaveBeenCalledTimes(1)
    pending.reject({ response: { data: { message: '核心连接已断开' } } })
    expect(await resetting).toBe(false)
    expect(model.resettingStatistics.value).toBe(false)
    expect(model.resetError.value).toBe('核心连接已断开')
    expect(model.overview.value?.metrics.requests_total).toBe(100)
    expect(oxidnsMonitorApi.resetStatistics).toHaveBeenCalledTimes(1)
  })

  it('ignores a late reset response after switching the monitored device', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(periodSnapshot()))
    const { model, id } = harness()
    await flushPromises()
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.resetStatistics).mockImplementationOnce(() => pending.promise as any)
    const resetting = model.resetStatistics()
    const oldSignal = vi.mocked(oxidnsMonitorApi.resetStatistics).mock.calls[0][2]?.signal
    id.value = 'second'
    await flushPromises()
    pending.resolve(response(periodSnapshot('old-reset', 0)))
    expect(await resetting).toBe(false)
    expect(oldSignal?.aborted).toBe(true)
    expect(model.overview.value?.statistics?.generation).toBe('period-one')
    expect(model.resettingStatistics.value).toBe(false)
  })

  it('does not compute QPS across periods reset by another browser', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(periodSnapshot()))
    const { model } = harness()
    await flushPromises()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(periodSnapshot('period-two', 200, '2026-10-17T01:00:10Z')))
    await model.refresh()
    expect(model.qps.value).toBeNull()
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(periodSnapshot('period-two', 250, '2026-10-17T01:00:20Z')))
    await model.refresh()
    expect(model.qps.value).toBe(5)
  })

  it('reloads rankings and queries from the selected recorder, ignoring old rankings', async () => {
    const multi = { ...periodSnapshot(), recorders: ['queries', 'second-recorder'] }
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(multi))
    const { model } = harness('queries')
    await flushPromises()
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.overview).mockImplementationOnce(() => pending.promise as any)
    const refreshing = model.refresh()
    model.selectRecorder('second-recorder')
    await flushPromises()
    expect(oxidnsMonitorApi.overview).toHaveBeenLastCalledWith('one', expect.objectContaining({ recorder: 'second-recorder' }))
    expect(oxidnsMonitorApi.queries).toHaveBeenLastCalledWith('one', expect.objectContaining({ recorder: 'second-recorder' }), expect.anything())
    pending.resolve(response({ ...multi, recorders: ['queries'] }))
    await refreshing
    expect(model.recorder.value).toBe('second-recorder')
  })

  it('asks for the selected rankings range and keeps it on later refreshes', async () => {
    vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response(periodSnapshot()))
    const { model } = harness()
    await flushPromises()
    expect(oxidnsMonitorApi.overview).toHaveBeenLastCalledWith('one', expect.objectContaining({ range: 'round' }))
    model.selectRankingRange('7d')
    await flushPromises()
    expect(model.rankingRange.value).toBe('7d')
    expect(oxidnsMonitorApi.overview).toHaveBeenLastCalledWith('one', expect.objectContaining({ range: '7d' }))
    const calls = vi.mocked(oxidnsMonitorApi.overview).mock.calls.length
    model.selectRankingRange('7d')  // the same range asks nothing
    await flushPromises()
    expect(vi.mocked(oxidnsMonitorApi.overview).mock.calls.length).toBe(calls)
    await model.refresh()
    expect(oxidnsMonitorApi.overview).toHaveBeenLastCalledWith('one', expect.objectContaining({ range: '7d' }))
  })

  it('does not send a reset for an older Agent without period support', async () => {
    const { model } = harness()
    await flushPromises()
    expect(await model.resetStatistics()).toBe(false)
    expect(oxidnsMonitorApi.resetStatistics).not.toHaveBeenCalled()
  })
})

describe('OxiDNS monitor display', () => {
  it('does not turn unavailable metrics into a fabricated zero', () => {
    const wrapper = mount(OxiDNSMetric, { props: { label: '失败请求', value: null } })
    wrappers.push(wrapper)
    expect(wrapper.text()).toContain('—')
    expect(wrapper.text()).not.toContain('0')
  })
  it('shows zero elapsed time and never labels a normal answer as a cache hit', async () => {
    const wrapper = mount(OxiDNSQueryTable, { props: {
      agentId: 'one', recorders: ['queries'], recorder: 'queries', filters: emptyQueryFilters(), data: records(),
      loading: false, error: '', page: 1, hasNext: false
    } })
    wrappers.push(wrapper)
    await flushPromises()
    expect(wrapper.text()).toContain('0 ms')
    expect(wrapper.text()).toContain('NOERROR')
    expect(wrapper.text()).toContain('解析来源')
    expect(wrapper.text()).toContain('未记录')
    expect(wrapper.text()).not.toContain('缓存命中')
    expect(wrapper.text()).not.toContain('命中规则')
    expect(wrapper.text()).toContain('本页 1 条')
    expect(oxidnsMonitorApi.query).toHaveBeenCalledTimes(1)
  })
  it('labels Agent DNS checks over loopback and leaves device queries unlabelled', async () => {
    const probe = { ...queryRecord, id: 13, client_ip: '127.0.0.1', questions_json: [{ name: 'example.com.', qtype: 1, qclass: 1 }] }
    const wrapper = mount(OxiDNSQueryTable, { props: {
      agentId: 'one', recorders: ['queries'], recorder: 'queries', filters: emptyQueryFilters(),
      data: { ...records(), records: [probe, queryRecord, { ...queryRecord, id: 14, questions_json: probe.questions_json }] },
      loading: false, error: '', page: 1, hasNext: false
    } })
    wrappers.push(wrapper)
    await flushPromises()
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('Agent 检查')
    expect(rows[1].text()).not.toContain('Agent 检查')
    expect(rows[2].text()).not.toContain('Agent 检查')
  })
  it('keeps rows compact: the device name and IP share a line and the detail button keeps a 44px hit area', async () => {
    const wrapper = mount(OxiDNSQueryTable, { props: {
      agentId: 'one', recorders: ['queries'], recorder: 'queries', filters: emptyQueryFilters(), data: records(),
      notes: { [queryRecord.client_ip as string]: '群晖' }, loading: false, error: '', page: 1, hasNext: false
    } })
    wrappers.push(wrapper)
    await flushPromises()
    const row = wrapper.get('tbody tr')
    expect(row.classes()).toContain('[&>td]:py-1.5')
    const client = row.findAll('td')[2]
    expect(client.findAll(':scope > span')).toHaveLength(1)
    expect(client.text()).toContain('群晖')
    expect(client.text()).toContain(queryRecord.client_ip as string)
    // 看起来 28px，上下各 8px 透明区，点击区仍为 44px；手机上也不被按钮默认的 44px 撑高
    const detail = row.get('button[aria-label^="查看查询"]')
    expect(detail.classes()).toEqual(expect.arrayContaining(['h-7', 'max-md:min-h-0', 'after:-inset-y-2']))
    expect(detail.classes()).not.toContain('min-h-11')
  })
  it('recognizes only loopback example.com checks as Agent probes', () => {
    const probe = (client_ip: string, name: string) => isAgentProbe({ client_ip, questions_json: [{ name, qtype: 1, qclass: 1 }] })
    expect(probe('127.0.0.1', 'example.com.')).toBe(true)
    expect(probe('::1', 'EXAMPLE.COM')).toBe(true)
    expect(probe('::ffff:127.0.0.1', 'example.com')).toBe(true)
    expect(probe('192.168.3.8', 'example.com.')).toBe(false)
    expect(probe('127.0.0.1', 'www.example.com.')).toBe(false)
    expect(isAgentProbe({ client_ip: '127.0.0.1', questions_json: [] })).toBe(false)
  })
  it('explains an Agent DNS check in query details', async () => {
    vi.mocked(oxidnsMonitorApi.query).mockResolvedValue(response({ success: true, recorder: 'queries', record: {
      ...queryRecord, client_ip: '127.0.0.1', questions_json: [{ name: 'example.com.', qtype: 1, qclass: 1 }], steps: [] } }))
    wrappers.push(mount(OxiDNSQueryDetail, { attachTo: document.body, props: { agentId: 'one', recorder: 'queries', recordId: 12 } }))
    await flushPromises()
    expect(document.body.textContent).toContain('并非局域网设备的查询')
  })
  it('explains an IPv4 preference empty answer without inventing an upstream or cache hit', async () => {
    vi.mocked(oxidnsMonitorApi.query).mockResolvedValue(response({ success: true, recorder: 'queries', record: {
      ...queryRecord, questions_json: [{ name: 'api.telegram.org.', qtype: 'AAAA', qclass: 'IN' }],
      rcode: 'No Error', answer_count: 0, answers_json: [],
      steps: ['entered', 'stop'].map((outcome, event_index) => ({
        event_index, sequence_tag: 'sequence_main', node_index: 2, kind: 'executor',
        tag: 'qs.exec.sequence_main.2.prefer_ipv4', outcome
      }))
    } }))
    wrappers.push(mount(OxiDNSQueryDetail, { attachTo: document.body, props: { agentId: 'one', recorder: 'queries', recordId: 12 } }))
    await flushPromises()
    const source = document.querySelector('[aria-label="解析来源"]')!
    expect(source.textContent).toContain('IPv4 优先（抑制 AAAA）')
    expect(source.textContent).toContain('由本地 IPv4 优先策略生成空应答，使客户端优先使用 IPv4。')
    expect(source.textContent).not.toContain('远程 DNS')
    expect(source.textContent).not.toContain('缓存')
    expect(document.body.textContent).toContain('NOERROR')
    expect(document.body.textContent).toContain('这次查询没有应答记录。')
    expect(document.body.textContent).toContain('qs.exec.sequence_main.2.prefer_ipv4')
  })
  it('renders actual answer content and only actual recorded steps in query details', async () => {
    vi.mocked(oxidnsMonitorApi.query).mockResolvedValue(response({ success: true, recorder: 'queries', record: {
      ...queryRecord, steps: [{ event_index: 0, sequence_tag: 'main', node_index: 1, kind: 'matcher', tag: 'allow_list', outcome: 'matched' }]
    } }))
    const wrapper = mount(OxiDNSQueryDetail, { attachTo: document.body, props: { agentId: 'one', recorder: 'queries', recordId: 12 } })
    wrappers.push(wrapper)
    await flushPromises()
    expect(document.body.textContent).toContain('192.0.2.1')
    expect(document.body.textContent).toContain('allow_list')
    expect(document.body.textContent).toContain('matched')
    expect(oxidnsMonitorApi.query).toHaveBeenCalledWith('one', 12, 'queries', expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })
  it('shows entered plugins as a path without claiming an upstream, cache hit or local answer', async () => {
    const step = (event_index: number, tag: string, outcome: string, kind = 'executor') => ({
      event_index, sequence_tag: 'main', node_index: null, kind, tag, outcome
    })
    vi.mocked(oxidnsMonitorApi.query).mockResolvedValue(response({ success: true, recorder: 'queries', record: {
      ...queryRecord, steps: [
        step(0, 'allow_list', 'not_matched', 'matcher'), step(1, 'cache', 'entered'),
        step(2, 'hosts', 'entered'), step(3, 'hosts', 'next'),
        step(4, 'forward_remote', 'entered'), step(5, 'forward_remote', 'next'),
        step(6, 'cache', 'stop')
      ]
    } }))
    const wrapper = mount(OxiDNSQueryDetail, { attachTo: document.body, props: { agentId: 'one', recorder: 'queries', recordId: 12 } })
    wrappers.push(wrapper)
    await flushPromises()
    const source = document.querySelector('section[aria-label="解析来源"]')!
    expect(source.querySelector('.chip')?.textContent).toBe('远程 DNS')
    expect(source.textContent).not.toContain('192.0.2.1') // The answer IP is not the DNS server.
    expect(source.textContent).not.toContain('缓存命中')
    expect(Array.from(source.querySelectorAll('li'), row => row.textContent?.replace('→', '').trim()))
      .toEqual(['cache', 'hosts', 'forward_remote'])
    expect(source.textContent).not.toContain('allow_list')
  })
  it.each(['NOERROR', 'NXDOMAIN', 'REFUSED', 'SERVFAIL'])('does not infer a source from response code %s', rcode => {
    expect(querySourceLabel({ ...queryRecord, rcode })).toBe('未记录')
    expect(querySourceLabel({ ...queryRecord, rcode, has_response: false })).toBe('无应答')
  })
  it('explains missing source information when the core recorded no plugin entries', async () => {
    const wrapper = mount(OxiDNSQueryDetail, { attachTo: document.body, props: { agentId: 'one', recorder: 'queries', recordId: 12 } })
    wrappers.push(wrapper)
    await flushPromises()
    const source = document.querySelector('section[aria-label="解析来源"]')!
    expect(source.querySelector('.chip')?.textContent).toBe('未记录')
    expect(source.textContent).toContain('没有记录插件进入路径')
    expect(source.querySelector('ol')).toBeNull()
  })
  it('highlights the dedicated OxiDNS navigation entry in overview scope', () => {
    expect(navigationPath('/agents/oxi-13/oxidns')).toBe('/oxidns')
    expect(scopeOfPath('/agents/oxi-13/oxidns')).toBe('overview')
    expect(labelOfPath('/agents/oxi-13/oxidns')).toBe('OxiDNS 面板')
    expect(navigationPath('/agents')).toBe('/agents')
    expect(navigationPath('/agents/oxi-13/unrelated')).toBe('/agents/oxi-13/unrelated')
  })
})

describe('OxiDNS response code display', () => {
  it.each([
    ['No Error', 'NOERROR'], ['Form Error', 'FORMERR'], ['Server Failure', 'SERVFAIL'],
    ['Non-Existent Domain', 'NXDOMAIN'], ['Not Implemented', 'NOTIMP'], ['Query Refused', 'REFUSED'],
    ['Name should not exist', 'YXDOMAIN'], ['RR Set should not exist', 'YXRRSET'],
    ['RR Set does not exist', 'NXRRSET'], ['Not authorized', 'NOTAUTH'], ['Name not in zone', 'NOTZONE'],
    ['Bad OPT Version', 'BADVERS'], ['TSIG Signature Failure', 'BADSIG'], ['Key not recognized', 'BADKEY'],
    ['Signature out of time window', 'BADTIME'], ['Bad TKEY mode', 'BADMODE'], ['Duplicate key name', 'BADNAME'],
    ['Algorithm not supported', 'BADALG'], ['Bad truncation', 'BADTRUNC'], ['Bad server cookie', 'BADCOOKIE']
  ])('shows official response %s as %s', (official, mnemonic) => {
    expect(formatDNSCode(official)).toBe(mnemonic)
  })
  it('normalizes known case variants, numeric values, and existing mnemonics', () => {
    expect(formatDNSCode('NO ERROR')).toBe('NOERROR')
    expect(formatDNSCode('nxdomain')).toBe('NXDOMAIN')
    expect(formatDNSCode(0)).toBe('NOERROR')
    expect(formatDNSCode('23')).toBe('BADCOOKIE')
    expect(formatDNSCode(null)).toBe('—')
  })
  it('preserves unrecognized core response codes verbatim', () => {
    expect(formatDNSCode('RCODE42')).toBe('RCODE42')
    expect(formatDNSCode('Future Response')).toBe('Future Response')
    expect(formatDNSCode(65535)).toBe('65535')
    expect(formatDNSCode('constructor')).toBe('constructor')
  })
})
