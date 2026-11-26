import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosError, AxiosHeaders, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import DomainDiscovery from '@/views/DomainDiscovery.vue'
import api from '@/api'
import { scopedRequests, setActiveProfileId } from '@/profileContext'
import { confirmState, notify, settleConfirm } from '@/lib/feedback'

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

type Handler = (config: InternalAxiosRequestConfig) => Promise<AxiosResponse>
let domainHandler: Handler
let agentHandler: Handler
let probeHandler: Handler
let settingsHandler: Handler
const requests: InternalAxiosRequestConfig[] = []
const wrappers: VueWrapper[] = []
const originalAdapter = api.defaults.adapter
const hiddenDescriptor = Object.getOwnPropertyDescriptor(document, 'hidden')

const response = (config: InternalAxiosRequestConfig, data: unknown): AxiosResponse => ({
  config, data, status: 200, statusText: 'OK', headers: new AxiosHeaders()
})
const agent = (overrides: Record<string, unknown> = {}) => ({
  id: 'collector', name: 'Existing Mihomo', enabled: true, supported: true, probe_supported: true,
  status: 'running', version: '1.6.0-go', mihomo_version: '1.19.32', last_report: '2026-10-11T12:00:00Z',
  store_state: 'ok', store_message: '', ...overrides
})
const agentsPayload = (agents: unknown[] = [agent()]) => ({
  agents, min_agent_version: '1.6.0-go', min_probe_agent_version: '1.6.0-go'
})
const domainsPayload = (domain = 'existing.test', overrides: Record<string, unknown> = {}) => ({
  items: domain ? [{
    domain, tags: ['uncovered'], conns: 3, fails: 0, fail_kinds: {}, routes: [], hosts: [],
    last_seen: '2026-10-11T12:00:00Z', pending_rule: null, suggestion: null
  }] : [], sniff_coverage: 1, ...agentsPayload(), ...overrides
})
const settingsPayload = (configured = false, overrides: Record<string, unknown> = {}) => ({
  direct: configured ? { id: 'direct', name: 'Direct', behavior: 'domain', active: true, problem: null, references: [{ policy: 'DIRECT' }] } : null,
  proxy: configured ? { id: 'proxy', name: 'Proxy', behavior: 'domain', active: true, problem: null, references: [{ policy: 'Proxy' }] } : null,
  ignored: [], candidates: [], policies: [], auto_probe: false, auto_apply: false,
  auto_apply_min_confidence: 80, confidence_choices: [80], probe_path: configured ? 'Proxy' : null, ...overrides
})
const requestCount = (url: string) => requests.filter(config => config.url === `/domain-discovery/${url}`).length
const timeout = (config: InternalAxiosRequestConfig) => new AxiosError('timeout', 'ECONNABORTED', config)
const serverError = (config: InternalAxiosRequestConfig, message: string) => new AxiosError('Request failed', 'ERR_BAD_RESPONSE', config, null, {
  config, data: { message }, status: 500, statusText: 'Internal Server Error', headers: new AxiosHeaders()
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

beforeEach(() => {
  const storage = () => {
    const values = new Map<string, string>()
    return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key), clear: () => values.clear() }
  }
  vi.stubGlobal('localStorage', storage())
  vi.stubGlobal('sessionStorage', storage())
  requests.length = 0
  scopedRequests.value = 0
  setActiveProfileId('discovery-profile')
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  domainHandler = async config => response(config, domainsPayload())
  agentHandler = async config => response(config, agentsPayload())
  probeHandler = async config => response(config, { job: { id: 'probe-one', kind: 'probe', status: 'done', done: 1, total: 1, errors: [] } })
  settingsHandler = async config => response(config, settingsPayload())
  api.defaults.adapter = async config => {
    requests.push(config)
    if (config.url === '/domain-discovery/domains') return domainHandler(config)
    if (config.url === '/domain-discovery/agents') return agentHandler(config)
    if (config.url === '/domain-discovery/probe/probe-one') return probeHandler(config)
    if (config.url === '/domain-discovery/settings') return settingsHandler(config)
    if (config.url === '/domain-discovery/ignore') return response(config, { success: true })
    if (config.url === '/domain-discovery/history') return response(config, { items: [] })
    if (config.url === '/domain-discovery/region') return response(config, { enabled: false, domains: {}, services: [] })
    if (config.url === '/agents/collector/domain-discovery') return response(config, { success: true })
    if (config.url === '/agents/collector/domain-discovery/clear') return response(config, { success: true })
    throw new Error(`Unexpected request: ${config.url}`)
  }
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: false, media: query, onchange: null, addListener: vi.fn(), removeListener: vi.fn(),
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn()
  })))
})

afterEach(async () => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  await flushPromises()
  api.defaults.adapter = originalAdapter
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  if (hiddenDescriptor) Object.defineProperty(document, 'hidden', hiddenDescriptor)
  else Reflect.deleteProperty(document, 'hidden')
  document.body.innerHTML = ''
})

async function render() {
  const wrapper = mount(DomainDiscovery, { attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

async function click(wrapper: VueWrapper, label: string) {
  const button = wrapper.findAll('button').find(item => item.text().trim() === label)
  expect(button, `button ${label}`).toBeTruthy()
  await button!.trigger('click')
  await flushPromises()
}

function setHidden(hidden: boolean) {
  Object.defineProperty(document, 'hidden', { configurable: true, value: hidden })
  document.dispatchEvent(new Event('visibilitychange'))
}

describe('domain discovery request and empty states', () => {
  it('summarizes healthy mobile settings and reopens the same controls without loading them again', async () => {
    settingsHandler = async config => response(config, settingsPayload(true, { auto_probe: true, auto_apply: true }))
    const wrapper = await render()
    const summary = wrapper.get('button[aria-controls="domain-discovery-configuration"]')
    expect(summary.attributes('aria-expanded')).toBe('false')
    expect(summary.text()).toContain('采集中 1 台')
    expect(summary.text()).toContain('直连 / 代理规则集就绪')
    expect(summary.text()).toContain('自动探测已开启')
    expect(summary.text()).toContain('自动采纳 ≥ 80')
    const control = wrapper.get('[aria-label="自动采纳建议"]').element
    await summary.trigger('click')
    expect(summary.attributes('aria-expanded')).toBe('true')
    await summary.trigger('click')
    expect(summary.attributes('aria-expanded')).toBe('false')
    expect(wrapper.get('[aria-label="自动采纳建议"]').element).toBe(control)
    expect(requestCount('settings')).toBe(1)
    expect(requestCount('agents')).toBe(1)
  })

  it.each(['missing ruleset', 'inactive ruleset', 'unwritable ruleset', 'missing probe path', 'collector failed', 'unsupported collector', 'damaged store'])('keeps configuration needing attention expanded: %s', async issue => {
    const data = settingsPayload(true)
    if (issue === 'missing ruleset') data.direct = null
    if (issue === 'inactive ruleset') data.proxy!.active = false
    if (issue === 'unwritable ruleset') Object.assign(data.proxy!, { problem: '规则集不可写入' })
    if (issue === 'missing probe path') data.probe_path = null
    settingsHandler = async config => response(config, data)
    if (issue === 'collector failed') agentHandler = async config => response(config, agentsPayload([agent({ status: 'unreachable' })]))
    if (issue === 'unsupported collector') agentHandler = async config => response(config, agentsPayload([agent({ supported: false })]))
    if (issue === 'damaged store') agentHandler = async config => response(config, agentsPayload([agent({
      status: 'no_data', last_report: null, store_state: 'damaged',
      store_message: '域名数据文件已损坏，上报已暂停。清除后可恢复采集（已采集数据将丢失）。'
    })]))
    const wrapper = await render()
    expect(wrapper.find('button[aria-controls="domain-discovery-configuration"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('采集设备')
    expect(wrapper.text()).toContain('默认规则集')
  })

  it('shows damaged traffic store alert with one-click clear and recovery', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const damagedMessage = '域名数据文件已损坏，上报已暂停。清除后可恢复采集（已采集数据将丢失）。'
    let damaged = true
    agentHandler = async config => response(config, agentsPayload([agent(damaged ? {
      status: 'no_data', last_report: null, mihomo_version: '',
      store_state: 'damaged', store_message: damagedMessage
    } : {
      status: 'no_data', last_report: null, mihomo_version: '',
      store_state: 'ok', store_message: ''
    })]))
    const wrapper = await render()
    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toContain(damagedMessage)
    expect(wrapper.text()).toContain('上报已暂停')
    expect(wrapper.find('button[aria-label="清除 Existing Mihomo 已采集的数据"]').exists()).toBe(false)

    // Damaged agent does not run useless 15s first-report polling until cleared.
    await vi.advanceTimersByTimeAsync(30000)
    expect(requestCount('agents')).toBe(1)

    // Cancelling confirmation keeps the damaged alert and sends no clear request.
    await click(wrapper, '清除并恢复')
    expect(confirmState.open).toBe(true)
    expect(confirmState.title).toBe('清除损坏的域名数据')
    expect(confirmState.confirmText).toBe('清除并恢复')
    expect(confirmState.danger).toBe(true)
    expect(confirmState.description).toContain('Existing Mihomo')
    expect(confirmState.description).toContain('历史域名数据将丢失')
    settleConfirm('cancel')
    await flushPromises()
    expect(requests.some(item => item.url === '/agents/collector/domain-discovery/clear')).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)

    // Confirming clears the file, reloads agents/domains, and restores normal waiting state.
    damaged = false
    await click(wrapper, '清除并恢复')
    expect(confirmState.open).toBe(true)
    settleConfirm('confirm')
    await flushPromises()
    expect(requests.some(item => item.url === '/agents/collector/domain-discovery/clear')).toBe(true)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('等待首次上报')
  })

  it('reveals retained settings when a refresh fails instead of leaving the error inside a collapsed section', async () => {
    settingsHandler = async config => response(config, settingsPayload(true))
    const wrapper = await render()
    expect(wrapper.get('button[aria-controls="domain-discovery-configuration"]').attributes('aria-expanded')).toBe('false')
    settingsHandler = async config => { throw timeout(config) }
    await click(wrapper, '刷新')
    expect(wrapper.find('button[aria-controls="domain-discovery-configuration"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('保留上次默认规则集设置')
    expect(wrapper.text()).toContain('请求超时')
  })

  it('shows existing collection devices even when the first domain request times out', async () => {
    domainHandler = async config => { throw timeout(config) }
    const wrapper = await render()
    expect(wrapper.text()).toContain('Existing Mihomo')
    expect(wrapper.text()).toContain('采集中')
    expect(wrapper.text()).toContain('读取失败')
    expect(wrapper.text()).toContain('请求超时')
    expect(wrapper.text()).not.toContain('当前配置没有 Mihomo Agent')
    expect(requestCount('agents')).toBe(1)
  })

  it('does not tell users to deploy an Agent when device lookup also fails', async () => {
    agentHandler = async config => { throw timeout(config) }
    domainHandler = async config => { throw timeout(config) }
    const wrapper = await render()
    expect(wrapper.text()).toContain('采集设备读取失败')
    expect(wrapper.text()).not.toContain('当前配置没有 Mihomo Agent')
    expect(wrapper.text()).not.toContain('前往 Agent')
  })

  it('shows deployment guidance only after a successful empty device response', async () => {
    agentHandler = async config => response(config, agentsPayload([]))
    domainHandler = async config => response(config, domainsPayload(''))
    const wrapper = await render()
    expect(wrapper.text()).toContain('当前配置没有 Mihomo Agent')
    expect(wrapper.text()).toContain('前往 Agent')
  })

  it('keeps the previous table and devices visible when a refresh fails', async () => {
    const wrapper = await render()
    domainHandler = async config => { throw timeout(config) }
    agentHandler = async config => { throw timeout(config) }
    await click(wrapper, '刷新')
    expect(wrapper.text()).toContain('existing.test')
    expect(wrapper.text()).toContain('Existing Mihomo')
    expect(wrapper.text()).toContain('保留上次成功读取的结果')
    expect(wrapper.text()).toContain('保留上次采集设备状态')
    expect(wrapper.text()).not.toContain('当前配置没有 Mihomo Agent')
  })

  it('joins retained-state notes after server messages without doubled punctuation', async () => {
    settingsHandler = async config => response(config, settingsPayload(true))
    const wrapper = await render()
    domainHandler = async config => { throw serverError(config, '域名数据读取失败。') }
    agentHandler = async config => { throw serverError(config, '采集设备读取失败。') }
    settingsHandler = async config => { throw serverError(config, '默认规则集读取失败。') }
    await click(wrapper, '刷新')
    expect(wrapper.text()).toContain('域名数据读取失败，下方保留上次成功读取的结果。')
    expect(wrapper.text()).toContain('采集设备读取失败，保留上次采集设备状态。')
    expect(wrapper.text()).toContain('默认规则集读取失败，保留上次默认规则集设置。')
    expect(wrapper.text()).not.toContain('。，')
  })

  it('coalesces repeated requests for the same view while refresh is running', async () => {
    const pending = deferred<AxiosResponse>()
    domainHandler = () => pending.promise
    const wrapper = await render()
    expect(wrapper.findAll('button').find(item => item.text().trim() === '刷新')!.attributes('disabled')).toBeDefined()
    await click(wrapper, '未覆盖')
    await click(wrapper, '未覆盖')
    expect(requestCount('domains')).toBe(1)
    const config = requests.find(item => item.url === '/domain-discovery/domains')!
    pending.resolve(response(config, domainsPayload()))
    await flushPromises()
    expect(wrapper.text()).toContain('existing.test')
  })

  it('cancels superseded filters and never lets their late responses replace the current results', async () => {
    const initial = deferred<AxiosResponse>()
    const all = deferred<AxiosResponse>()
    const day = deferred<AxiosResponse>()
    domainHandler = config => config.params.view === 'uncovered' ? initial.promise : config.params.days === '7' ? all.promise : day.promise
    const wrapper = await render()
    const initialConfig = requests.find(item => item.url === '/domain-discovery/domains')!
    await click(wrapper, '全部')
    const allConfig = requests.filter(item => item.url === '/domain-discovery/domains')[1]!
    await click(wrapper, '24 小时')
    const dayConfig = requests.filter(item => item.url === '/domain-discovery/domains')[2]!
    expect(initialConfig.signal!.aborted).toBe(true)
    expect(allConfig.signal!.aborted).toBe(true)
    expect(dayConfig.params).toEqual({ view: 'all', days: '1' })
    day.resolve(response(dayConfig, domainsPayload('latest.test')))
    await flushPromises()
    all.resolve(response(allConfig, domainsPayload('old-all.test')))
    initial.resolve(response(initialConfig, domainsPayload('old-initial.test')))
    await flushPromises()
    expect(wrapper.text()).toContain('latest.test')
    expect(wrapper.text()).not.toContain('old-all.test')
    expect(wrapper.text()).not.toContain('old-initial.test')
    expect(wrapper.text()).not.toContain('读取失败')
  })

  it('cancels a domain scan when changing to history and does not show its stale results', async () => {
    const pending = deferred<AxiosResponse>()
    domainHandler = () => pending.promise
    const wrapper = await render()
    const config = requests.find(item => item.url === '/domain-discovery/domains')!
    await click(wrapper, '已加入')
    expect(config.signal!.aborted).toBe(true)
    expect(wrapper.text()).toContain('还没有加入过域名')
    pending.resolve(response(config, domainsPayload('old-initial.test')))
    await flushPromises()
    expect(wrapper.text()).not.toContain('old-initial.test')
  })

  it('reads fresh results after changing a rule while an older refresh is still running', async () => {
    const wrapper = await render()
    await wrapper.get('button[aria-label="选择 existing.test"]').trigger('click')
    const pending = deferred<AxiosResponse>()
    domainHandler = () => pending.promise
    await click(wrapper, '刷新')
    const config = requests.filter(item => item.url === '/domain-discovery/domains')[1]!
    await click(wrapper, '忽略')
    expect(requestCount('domains')).toBe(2)
    domainHandler = async config => response(config, domainsPayload(''))
    pending.resolve(response(config, domainsPayload()))
    await flushPromises()
    expect(requestCount('domains')).toBe(3)
    expect(wrapper.text()).not.toContain('existing.test')
  })

  it('aborts pending reads and avoids failure notifications when navigating away', async () => {
    const pending = deferred<AxiosResponse>()
    domainHandler = () => pending.promise
    const errorSpy = vi.spyOn(notify, 'error')
    const wrapper = await render()
    const config = requests.find(item => item.url === '/domain-discovery/domains')!
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    expect(requests.every(item => item.signal?.aborted)).toBe(true)
    pending.reject(timeout(config))
    await flushPromises()
    expect(errorSpy).not.toHaveBeenCalled()
  })
})

describe('first report polling', () => {
  it('refreshes the device status while waiting, then refreshes domains once and stops', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    agentHandler = async config => response(config, agentsPayload([agent({ status: 'no_data', last_report: null })]))
    const wrapper = await render()
    expect(wrapper.text()).toContain('等待首次上报')
    await vi.advanceTimersByTimeAsync(15000)
    expect(requestCount('agents')).toBe(2)
    expect(requestCount('domains')).toBe(1)
    agentHandler = async config => response(config, agentsPayload())
    await vi.advanceTimersByTimeAsync(15000)
    await flushPromises()
    expect(wrapper.text()).not.toContain('等待首次上报')
    expect(requestCount('agents')).toBe(3)
    expect(requestCount('domains')).toBe(2)
    await vi.advanceTimersByTimeAsync(60000)
    expect(requestCount('agents')).toBe(3)
    expect(requestCount('domains')).toBe(2)
  })

  it('pauses in the background and stops polling after unmount', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    agentHandler = async config => response(config, agentsPayload([agent({ status: 'no_data', last_report: null })]))
    const wrapper = await render()
    setHidden(true)
    await vi.advanceTimersByTimeAsync(60000)
    expect(requestCount('agents')).toBe(1)
    setHidden(false)
    await vi.advanceTimersByTimeAsync(15000)
    expect(requestCount('agents')).toBe(2)
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    await vi.advanceTimersByTimeAsync(60000)
    expect(requestCount('agents')).toBe(2)
  })

  it('starts first-report polling when collection is enabled without reloading the page', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    agentHandler = async config => response(config, agentsPayload([agent({ enabled: false, status: 'no_data', last_report: null })]))
    const wrapper = await render()
    await wrapper.get('button[aria-label="开启 Existing Mihomo 的域名发现"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('等待首次上报')
    agentHandler = async config => response(config, agentsPayload())
    await vi.advanceTimersByTimeAsync(15000)
    await flushPromises()
    expect(requestCount('agents')).toBe(2)
    expect(requestCount('domains')).toBe(2)
    expect(wrapper.text()).not.toContain('等待首次上报')
  })

  it('defers a first-report result arriving in the background until the page is visible', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    agentHandler = async config => response(config, agentsPayload([agent({ status: 'no_data', last_report: null })]))
    await render()
    const pending = deferred<AxiosResponse>()
    agentHandler = () => pending.promise
    await vi.advanceTimersByTimeAsync(15000)
    const config = requests.filter(item => item.url === '/domain-discovery/agents')[1]!
    setHidden(true)
    pending.resolve(response(config, agentsPayload()))
    await flushPromises()
    expect(requestCount('domains')).toBe(1)
    setHidden(false)
    await flushPromises()
    expect(requestCount('domains')).toBe(2)
    await vi.advanceTimersByTimeAsync(60000)
    expect(requestCount('domains')).toBe(2)
  })

  it('queues one fresh domain snapshot if the first report arrives during the initial scan', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const pending = deferred<AxiosResponse>()
    domainHandler = () => pending.promise
    agentHandler = async config => response(config, agentsPayload([agent({ status: 'no_data', last_report: null })]))
    const wrapper = await render()
    const config = requests.find(item => item.url === '/domain-discovery/domains')!
    agentHandler = async config => response(config, agentsPayload())
    await vi.advanceTimersByTimeAsync(15000)
    expect(requestCount('domains')).toBe(1)
    domainHandler = async config => response(config, domainsPayload('reported.test'))
    pending.resolve(response(config, domainsPayload('')))
    await flushPromises()
    expect(requestCount('domains')).toBe(2)
    expect(wrapper.text()).toContain('reported.test')
  })

  it('does not start a domain refresh when an in-flight probe completes in the background', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    domainHandler = async config => response(config, domainsPayload('existing.test', {
      probe_job: { id: 'probe-one', kind: 'probe', status: 'running', done: 0, total: 1, errors: [] }
    }))
    const pending = deferred<AxiosResponse>()
    probeHandler = () => pending.promise
    await render()
    await vi.advanceTimersByTimeAsync(1500)
    const config = requests.find(item => item.url === '/domain-discovery/probe/probe-one')!
    setHidden(true)
    pending.resolve(response(config, { job: { id: 'probe-one', kind: 'probe', status: 'done', done: 1, total: 1, errors: [] } }))
    await flushPromises()
    expect(requestCount('domains')).toBe(1)
    await vi.advanceTimersByTimeAsync(10000)
    expect(requestCount('domains')).toBe(1)
    setHidden(false)
    await vi.advanceTimersByTimeAsync(1500)
    expect(requestCount('domains')).toBe(2)
    expect(requestCount('probe/probe-one')).toBe(1)
  })

  it('stops retrying automatically when the lightweight status lookup fails', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    agentHandler = async config => response(config, agentsPayload([agent({ status: 'no_data', last_report: null })]))
    const wrapper = await render()
    agentHandler = async config => { throw timeout(config) }
    await vi.advanceTimersByTimeAsync(15000)
    expect(wrapper.text()).toContain('保留上次采集设备状态')
    await vi.advanceTimersByTimeAsync(60000)
    expect(requestCount('agents')).toBe(2)
    expect(requestCount('domains')).toBe(1)
  })
})
