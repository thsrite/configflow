import { computed, onMounted, onUnmounted, ref, watch, type Ref } from 'vue'
import { oxidnsMonitorApi } from '@/api'
import { emptyQueryFilters, monitorNumber, type OxiDNSOverview, type OxiDNSQueries, type OxiDNSQueryFilters } from '@/types/oxidnsMonitor'
import { oxiDNSQueryTimeRange, useOxiDNSClock } from '@/composables/useOxiDNSClock'

const messageOf = (error: any) => error?.response?.data?.message || error?.message || '读取失败，请稍后重试。'
const codeOf = (error: any): string => error?.response?.data?.code || error?.code || ''
const checked = <T>(data: T): T => {
  if ((data as any)?.success === false) throw Object.assign(new Error((data as any).message || '读取失败，请稍后重试。'), { code: (data as any).code })
  return data
}
const FIRST_RATE_SAMPLE_DELAY = 500
const MAX_RATE_SAMPLE_GAP = 30

// The last overview of each device, kept in memory for this tab only. Reopening
// a panel within the sample gap computes the rate at once instead of sampling again.
type RateSample = Pick<OxiDNSOverview, 'collected_at'> & {
  metrics: Pick<OxiDNSOverview['metrics'], 'requests_total'>
  core: Pick<OxiDNSOverview['core'], 'instance_id'>
  // The overview reports `statistics: null` before a period starts.
  statistics?: Pick<NonNullable<OxiDNSOverview['statistics']>, 'generation'> | null
}
const recentSamples = new Map<string, { at: number; sample: RateSample }>()
function rememberSample(id: string, next: OxiDNSOverview) {
  recentSamples.set(id, { at: Date.now(), sample: {
    collected_at: next.collected_at, metrics: { requests_total: next.metrics.requests_total },
    core: { instance_id: next.core.instance_id },
    statistics: next.statistics ? { generation: next.statistics.generation } : undefined
  } })
}
// The last full overview of each device, for this tab only: reopening the panel
// shows it at once (its sample time says how old it is) while a fresh one is read.
const lastOverviews = new Map<string, OxiDNSOverview>()
/** Drop every remembered sample and overview, e.g. between tests. */
export function forgetOxiDNSRateSamples() {
  recentSamples.clear()
  lastOverviews.clear()
}
function recentSample(id: string): RateSample | undefined {
  const entry = recentSamples.get(id)
  if (entry && Date.now() - entry.at <= MAX_RATE_SAMPLE_GAP * 1000) return entry.sample
  recentSamples.delete(id)
  return undefined
}

export function useOxiDNSMonitor(agentId: Ref<string>, tab: Ref<string>) {
  const overview = ref<OxiDNSOverview | null>(null)
  const queryClock = useOxiDNSClock(computed(() => overview.value?.collected_at))
  const overviewLoading = ref(false)
  const overviewError = ref('')
  const overviewErrorCode = ref('')
  const resettingStatistics = ref(false)
  const resetError = ref('')
  const qps = ref<number | null>(null)
  const queries = ref<OxiDNSQueries | null>(null)
  const queriesLoading = ref(false)
  const queriesError = ref('')
  const recorder = ref('')
  const filters = ref<OxiDNSQueryFilters>(emptyQueryFilters())
  const cursors = ref<Array<string | undefined>>([undefined])
  const autoRefresh = ref(true)
  const page = computed(() => cursors.value.length)
  const hasNext = computed(() => Boolean(queries.value?.next_cursor))
  let overviewController: AbortController | undefined
  let resetController: AbortController | undefined
  let overviewRequest = 0
  let queryController: AbortController | undefined
  let epoch = 0
  let disposed = false
  let queryEpoch = 0
  let timer: ReturnType<typeof setInterval> | undefined
  let rateTimer: ReturnType<typeof setTimeout> | undefined
  let rateSampleAttempted = false
  let rangeSince: number | undefined
  let rangeUntil: number | undefined

  function resetQueryRange() {
    const bounds = oxiDNSQueryTimeRange(filters.value.range, queryClock.now())
    rangeUntil = bounds.until_ms
    rangeSince = bounds.since_ms
  }
  async function loadQueries(force = false) {
    if (disposed || !recorder.value || !overview.value?.recorders.includes(recorder.value)) return
    if (queriesLoading.value && !force) return
    queryController?.abort()
    const controller = queryController = new AbortController()
    const requestEpoch = ++queryEpoch
    const requestAgent = agentId.value
    queriesLoading.value = true
    queriesError.value = ''
    const params = {
      recorder: recorder.value, limit: 50, cursor: cursors.value[cursors.value.length - 1],
      qname: filters.value.qname.trim() || undefined,
      client_ip: filters.value.client_ip.trim() || undefined,
      qtype: filters.value.qtype.trim() || undefined,
      rcode: filters.value.rcode.trim() || undefined,
      matcher_tag: filters.value.matcher_tag.trim() || undefined,
      status: filters.value.status, since_ms: rangeSince, until_ms: rangeUntil
    }
    try {
      const { data } = await oxidnsMonitorApi.queries(requestAgent, params, { signal: controller.signal })
      if (controller.signal.aborted || requestEpoch !== queryEpoch || requestAgent !== agentId.value) return
      queries.value = checked(data) as OxiDNSQueries
    } catch (error) {
      if (!controller.signal.aborted && requestEpoch === queryEpoch) queriesError.value = messageOf(error)
    } finally {
      if (requestEpoch === queryEpoch) queriesLoading.value = false
    }
  }
  function cancelRateSample() {
    if (rateTimer !== undefined) clearTimeout(rateTimer)
    rateTimer = undefined
  }
  function scheduleRateSample() {
    cancelRateSample()
    const current = overview.value
    if (disposed || document.hidden || !autoRefresh.value || resettingStatistics.value
      || rateSampleAttempted || qps.value !== null || !current?.core.instance_id
      || !monitorNumber(current.metrics.requests_total) || !Number.isFinite(Date.parse(current.collected_at))) return
    // Only one extra overview per new series; normal polling remains at 10s.
    // Reuse the authenticated overview and never estimate QPS from query logs.
    rateTimer = setTimeout(() => {
      rateTimer = undefined
      if (disposed || document.hidden || !autoRefresh.value || resettingStatistics.value || overviewLoading.value) return
      rateSampleAttempted = true
      void loadOverview()
    }, FIRST_RATE_SAMPLE_DELAY)
  }
  function applyOverview(next: OxiDNSOverview, clearQps = false) {
    const prior: RateSample | undefined = overview.value ?? recentSample(agentId.value)
    const seconds = prior ? (Date.parse(next.collected_at) - Date.parse(prior.collected_at)) / 1000 : 0
    const currentCount = next.metrics.requests_total
    const priorCount = prior?.metrics.requests_total
    const sameSeries = prior && next.core.instance_id && prior.core.instance_id === next.core.instance_id
      && prior.statistics?.generation === next.statistics?.generation
    if (clearQps || !sameSeries || seconds > MAX_RATE_SAMPLE_GAP
      || (monitorNumber(currentCount) && monitorNumber(priorCount) && currentCount < priorCount)) rateSampleAttempted = false
    qps.value = !clearQps && sameSeries
      && monitorNumber(currentCount) && monitorNumber(priorCount) && currentCount >= priorCount
      && seconds > 0 && seconds <= MAX_RATE_SAMPLE_GAP
      ? (currentCount - priorCount) / seconds : null
    overview.value = next
    rememberSample(agentId.value, next)
    lastOverviews.set(agentId.value, next)
    if (!next.recorders.includes(recorder.value)) {
      recorder.value = next.recorders[0] || ''
      resetQueries()
    }
    scheduleRateSample()
  }
  async function loadOverview(force = false) {
    if (disposed || resettingStatistics.value || (overviewLoading.value && !force)) return
    cancelRateSample()
    overviewController?.abort()
    const controller = overviewController = new AbortController()
    const requestEpoch = epoch
    const requestNumber = ++overviewRequest
    overviewLoading.value = true
    overviewError.value = ''
    overviewErrorCode.value = ''
    try {
      const { data } = await oxidnsMonitorApi.overview(agentId.value, { signal: controller.signal, recorder: recorder.value || undefined })
      if (controller.signal.aborted || requestEpoch !== epoch || requestNumber !== overviewRequest) return
      applyOverview(checked(data) as OxiDNSOverview)
    } catch (error) {
      if (!controller.signal.aborted && requestEpoch === epoch && requestNumber === overviewRequest) {
        overviewError.value = messageOf(error)
        overviewErrorCode.value = codeOf(error)
      }
    } finally {
      if (requestEpoch === epoch && requestNumber === overviewRequest) overviewLoading.value = false
    }
  }
  async function resetStatistics(): Promise<boolean> {
    if (disposed || resettingStatistics.value || !overview.value?.statistics) return false
    cancelRateSample()
    overviewController?.abort()
    overviewRequest++
    overviewLoading.value = false
    const controller = resetController = new AbortController()
    const requestEpoch = epoch
    const requestAgent = agentId.value
    resettingStatistics.value = true
    resetError.value = ''
    try {
      const { data } = await oxidnsMonitorApi.resetStatistics(requestAgent, recorder.value || undefined, { signal: controller.signal })
      if (controller.signal.aborted || requestEpoch !== epoch || requestAgent !== agentId.value) return false
      applyOverview(checked(data) as OxiDNSOverview, true)
      overviewError.value = ''
      overviewErrorCode.value = ''
      return true
    } catch (error) {
      if (!controller.signal.aborted && requestEpoch === epoch) resetError.value = messageOf(error)
      return false
    } finally {
      if (requestEpoch === epoch) {
        resettingStatistics.value = false
        scheduleRateSample()
      }
    }
  }
  function resetQueries() {
    queryController?.abort()
    queryEpoch++
    queriesLoading.value = false
    queries.value = null
    queriesError.value = ''
    cursors.value = [undefined]
    resetQueryRange()
    if (tab.value === 'queries' && recorder.value) void loadQueries()
  }
  function selectRecorder(value: string) {
    if (value === recorder.value || resettingStatistics.value || !overview.value?.recorders.includes(value)) return
    recorder.value = value
    resetQueries()
    void loadOverview(true)
  }
  function applyFilters(value: OxiDNSQueryFilters) {
    filters.value = { ...value }
    resetQueries()
  }
  function nextPage() {
    if (queriesLoading.value || !queries.value?.next_cursor) return
    cursors.value.push(queries.value.next_cursor)
    queries.value = null
    void loadQueries()
  }
  function previousPage() {
    if (queriesLoading.value || page.value <= 1) return
    cursors.value.pop()
    queries.value = null
    void loadQueries()
  }
  async function refresh(manual = false) {
    const requestEpoch = epoch
    await loadOverview()
    if (disposed || requestEpoch !== epoch) return
    if (tab.value === 'queries' && recorder.value && (page.value === 1 || manual)) {
      if (page.value === 1) resetQueryRange()
      await loadQueries()
    }
  }
  const onVisible = () => {
    cancelRateSample()
    if (!document.hidden && autoRefresh.value) void refresh()
  }
  watch(autoRefresh, enabled => {
    cancelRateSample()
    if (enabled && !document.hidden) void refresh()
  })
  watch(tab, value => {
    if (value === 'queries' && !queries.value && recorder.value) void loadQueries()
    if (value !== 'queries') {
      queryController?.abort()
      queryEpoch++
      queriesLoading.value = false
    }
  })
  watch(agentId, () => {
    epoch++
    cancelRateSample()
    rateSampleAttempted = false
    overviewController?.abort()
    resetController?.abort()
    overviewRequest++
    resettingStatistics.value = false
    resetError.value = ''
    queryController?.abort()
    queryEpoch++
    overviewLoading.value = false
    queriesLoading.value = false
    overview.value = lastOverviews.get(agentId.value) ?? null
    overviewError.value = ''
    overviewErrorCode.value = ''
    qps.value = null
    recorder.value = ''
    filters.value = emptyQueryFilters()
    resetQueries()
    void loadOverview()
  }, { immediate: true })
  onMounted(() => {
    timer = setInterval(() => { if (autoRefresh.value && !document.hidden) void refresh() }, 10000)
    document.addEventListener('visibilitychange', onVisible)
  })
  onUnmounted(() => {
    disposed = true
    cancelRateSample()
    epoch++
    queryEpoch++
    overviewController?.abort()
    resetController?.abort()
    queryController?.abort()
    if (timer) clearInterval(timer)
    document.removeEventListener('visibilitychange', onVisible)
  })
  return { overview, overviewLoading, overviewError, overviewErrorCode, resettingStatistics, resetError, resetStatistics, clockSkewMs: queryClock.skewMs, qps, queries, queriesLoading, queriesError, recorder,
    filters, page, hasNext, autoRefresh, refresh, loadQueries, selectRecorder, applyFilters, nextPage, previousPage }
}
