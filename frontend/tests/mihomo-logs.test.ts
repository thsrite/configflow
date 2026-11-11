import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import MihomoLogs from '@/components/mihomo/MihomoLogs.vue'
import AgentLogLines from '@/components/agents/AgentLogLines.vue'
import { Select } from '@/components/ui/select'
import Segmented from '@/components/common/Segmented.vue'
import { agentApi, mihomoPanelApi } from '@/api'
import { filterLogLevel, parseAgentLogs } from '@/lib/agentLogs'

vi.mock('@/api', () => ({
  agentApi: { getLogSources: vi.fn(), getLogs: vi.fn() },
  mihomoPanelApi: { configs: vi.fn(), logs: vi.fn() }
}))
vi.mock('@/lib/mihomoPanelCache', () => ({ cachedPanelData: vi.fn() }))
vi.mock('vue-router', () => ({ RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } }))
vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })

const coreSource = { id: 'core', label: 'Mihomo 运行日志', kind: 'journal', unit: 'mihomo' }
const sources = (items: unknown[] = [coreSource]) => ({ data: { success: true, sources: items } } as any)
const logs = (text: string) => ({ data: { success: true, logs: text } } as any)
const live = (entries: Array<[number, string, string]>, extra: Record<string, unknown> = {}) => ({
  data: {
    success: true,
    data: {
      stream: 'info', status: 'streaming', next: entries.at(-1)?.[0] ?? 0, skipped: 0,
      entries: entries.map(([seq, level, payload]) => ({ seq, level, payload, time: '2026-11-11T08:00:0' + (seq % 10) + 'Z' })),
      ...extra
    }
  }
} as any)
const wrappers: VueWrapper[] = []
const deferred = <T,>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

async function render(props: Partial<{ agentId: string; active: boolean; reloadToken: number }> = {}) {
  const wrapper = mount(MihomoLogs, { props: { agentId: 'gw', active: true, reloadToken: 0, ...props } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

const logText = (wrapper: VueWrapper) => wrapper.findComponent(AgentLogLines).props('text')
const button = (wrapper: VueWrapper, label: string) => {
  const match = wrapper.findAll('button').find(item => item.attributes('aria-label') === label || item.text() === label)
  if (!match) throw new Error(`Missing button: ${label}`)
  return match
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
  vi.resetAllMocks()
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  vi.mocked(agentApi.getLogSources).mockResolvedValue(sources())
  vi.mocked(agentApi.getLogs).mockResolvedValue(logs('core startup complete'))
  vi.mocked(mihomoPanelApi.configs).mockResolvedValue({ data: { success: true, data: { 'log-level': 'info' } } } as any)
  vi.mocked(mihomoPanelApi.logs).mockResolvedValue(live([]))
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.useRealTimers()
  delete (document as any).visibilityState
  delete (document as any).hidden
})

describe('Mihomo logs', () => {
  it('reads only a core source and searches the bounded snapshot as literal text', async () => {
    vi.mocked(agentApi.getLogSources).mockResolvedValue(sources([
      { id: 'agent', label: 'Agent 自身日志', kind: 'file' },
      coreSource,
      { id: 'core-error', label: 'Mihomo 错误日志', kind: 'file' },
      { id: 'supervisor', label: 'Supervisor 日志', kind: 'file' }
    ]))
    vi.mocked(agentApi.getLogs).mockResolvedValue(logs('connection example.test[1]\nconnection exampleXtest1\nother entry'))
    const wrapper = await render()
    expect(agentApi.getLogs).toHaveBeenCalledWith('gw', 500, { source: 'core' }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).not.toContain('Agent 自身日志')
    expect(wrapper.text()).not.toContain('Supervisor 日志')
    await wrapper.get('input[placeholder="搜索日志"]').setValue('EXAMPLE.TEST[1]')
    expect(logText(wrapper)).toContain('connection example.test[1]')
    expect(logText(wrapper)).not.toContain('exampleXtest1')
    await wrapper.get('input[placeholder="搜索日志"]').setValue('absent')
    expect(wrapper.text()).toContain('没有匹配的日志')
  })

  it('keeps the last 500 actual lines when the snapshot ends with a newline', async () => {
    const lines = Array.from({ length: 501 }, (_, index) => `log entry ${index + 1}`)
    vi.mocked(agentApi.getLogs).mockResolvedValue(logs(`${lines.join('\n')}\n`))
    const wrapper = await render()
    expect(logText(wrapper).split('\n')).toEqual(lines.slice(1))
  })

  it('does not overlap requests and pauses automatic refresh until resumed', async () => {
    const next = deferred<any>()
    vi.mocked(agentApi.getLogs).mockResolvedValueOnce(logs('first')).mockReturnValueOnce(next.promise)
    const wrapper = await render()
    await vi.advanceTimersByTimeAsync(3000)
    expect(agentApi.getLogs).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(12000)
    expect(agentApi.getLogs).toHaveBeenCalledTimes(2)
    next.resolve(logs('second'))
    await flushPromises()
    await button(wrapper, '暂停刷新').trigger('click')
    await vi.advanceTimersByTimeAsync(9000)
    expect(agentApi.getLogs).toHaveBeenCalledTimes(2)
    await button(wrapper, '恢复刷新').trigger('click')
    await flushPromises()
    expect(agentApi.getLogs).toHaveBeenCalledTimes(3)
  })

  it('waits until active and cancels a pending request when the tab is deactivated', async () => {
    const wrapper = await render({ active: false })
    expect(agentApi.getLogSources).not.toHaveBeenCalled()
    await wrapper.setProps({ active: true })
    await flushPromises()
    const pending = deferred<any>()
    vi.mocked(agentApi.getLogs).mockReturnValueOnce(pending.promise)
    await vi.advanceTimersByTimeAsync(3000)
    const signal = vi.mocked(agentApi.getLogs).mock.calls.at(-1)![3]!.signal!
    await wrapper.setProps({ active: false })
    expect(signal.aborted).toBe(true)
    pending.resolve(logs('stale response'))
    await flushPromises()
    expect(logText(wrapper)).not.toContain('stale response')
    const requests = vi.mocked(agentApi.getLogs).mock.calls.length
    await vi.advanceTimersByTimeAsync(12000)
    expect(agentApi.getLogs).toHaveBeenCalledTimes(requests)
  })

  it('suspends refresh while the page is hidden and cancels on unmount', async () => {
    const wrapper = await render()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(9000)
    expect(agentApi.getLogs).toHaveBeenCalledTimes(1)
    const pending = deferred<any>()
    vi.mocked(agentApi.getLogs).mockReturnValueOnce(pending.promise)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(agentApi.getLogs).toHaveBeenCalledTimes(2)
    const signal = vi.mocked(agentApi.getLogs).mock.calls.at(-1)![3]!.signal!
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    expect(signal.aborted).toBe(true)
    pending.resolve(logs('late unmounted response'))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(9000)
    expect(agentApi.getLogs).toHaveBeenCalledTimes(2)
  })

  it('discards a previous device response without mixing it into the new device', async () => {
    const previous = deferred<any>()
    vi.mocked(agentApi.getLogs).mockReturnValueOnce(previous.promise).mockResolvedValue(logs('new gateway log'))
    const wrapper = await render()
    const signal = vi.mocked(agentApi.getLogs).mock.calls[0][3]!.signal!
    await wrapper.setProps({ agentId: 'gw-next' })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(agentApi.getLogSources).toHaveBeenLastCalledWith('gw-next', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(logText(wrapper)).toBe('new gateway log')
    previous.resolve(logs('old gateway log'))
    await flushPromises()
    expect(logText(wrapper)).toBe('new gateway log')
  })

  // The core's log has no choice of source: the first readable one is read.
  it('offers no source choice and reads the first readable core log', async () => {
    vi.mocked(agentApi.getLogSources).mockResolvedValue(sources([
      { id: 'core-file', label: 'Mihomo 日志文件', kind: 'unavailable', note: '文件不存在' },
      coreSource,
      { id: 'core-error', label: 'Mihomo 错误日志', kind: 'file' }
    ]))
    const wrapper = await render()
    expect(wrapper.find('[aria-label="日志来源"]').exists()).toBe(false)
    expect(agentApi.getLogs).toHaveBeenLastCalledWith('gw', 500, { source: 'core' }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(logText(wrapper)).toBe('core startup complete')
    expect(wrapper.text()).not.toContain('Mihomo 错误日志')
  })

  it('keeps the last successful snapshot visible after failure and supports retry', async () => {
    const wrapper = await render()
    vi.mocked(agentApi.getLogs).mockRejectedValueOnce({ response: { data: { message: 'Agent 暂时不可达' } } })
    await button(wrapper, '刷新日志').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Agent 暂时不可达')
    expect(logText(wrapper)).toBe('core startup complete')
    vi.mocked(agentApi.getLogs).mockResolvedValueOnce(logs('recovered output'))
    await button(wrapper, '刷新日志').trigger('click')
    await flushPromises()
    expect(logText(wrapper)).toBe('recovered output')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('explains unreadable core logs instead of falling back to the Agent log', async () => {
    vi.mocked(agentApi.getLogSources).mockResolvedValue(sources([
      { id: 'agent', label: 'Agent 日志', kind: 'file' },
      { ...coreSource, kind: 'unavailable', note: '请在宿主机用 docker logs 查看' }
    ]))
    const wrapper = await render()
    expect(wrapper.text()).toContain('请在宿主机用 docker logs 查看')
    expect(agentApi.getLogs).not.toHaveBeenCalled()
  })

  it('requires a source-aware Agent rather than showing ambiguous legacy logs', async () => {
    vi.mocked(agentApi.getLogSources).mockResolvedValue({ data: { success: true, legacy: true } } as any)
    const wrapper = await render()
    expect(wrapper.text()).toContain('旧版 Agent')
    expect(wrapper.text()).toContain('升级 Agent')
    expect(agentApi.getLogs).not.toHaveBeenCalled()
  })

  it('defaults the level to the configured log-level and filters file logs by it', async () => {
    vi.mocked(mihomoPanelApi.configs).mockResolvedValue({ data: { success: true, data: { 'log-level': 'warning' } } } as any)
    vi.mocked(agentApi.getLogs).mockResolvedValue(logs([
      'time="2026-11-11T10:00:00+08:00" level=info msg="[TCP] a"',
      'time="2026-11-11T10:00:01+08:00" level=warning msg="[DNS] b"',
      'time="2026-11-11T10:00:02+08:00" level=error msg="c"'
    ].join('\n')))
    const wrapper = await render()
    const pane = wrapper.findComponent(AgentLogLines)
    expect(pane.props('minLevel')).toBe('WARN')
    expect(pane.text()).not.toContain('[TCP] a')
    expect(pane.text()).toContain('[DNS] b')
    expect(wrapper.find('[data-file-level-hint]').exists()).toBe(false)

    // 选得比配置更详细：文件里没有这些日志，提示切到实时
    wrapper.findAllComponents(Select).at(-1)!.vm.$emit('update:modelValue', 'debug')
    await flushPromises()
    expect(wrapper.findComponent(AgentLogLines).text()).toContain('[TCP] a')
    expect(wrapper.get('[data-file-level-hint]').text()).toContain('只记录 warning 及以上')
    expect(mihomoPanelApi.logs).not.toHaveBeenCalled()
  })

  it('streams live logs incrementally at the chosen level every second', async () => {
    vi.mocked(mihomoPanelApi.configs).mockResolvedValue({ data: { success: true, data: { 'log-level': 'silent' } } } as any)
    vi.mocked(mihomoPanelApi.logs)
      .mockResolvedValueOnce(live([[1, 'info', '[TCP] first'], [2, 'warning', '[DNS] second']]))
      .mockResolvedValueOnce(live([[3, 'error', 'third']], { skipped: 4 }))
      .mockResolvedValue(live([], { next: 3 }))
    const wrapper = await render()
    wrapper.getComponent(Segmented).vm.$emit('update:modelValue', 'live')
    await flushPromises()
    // silent 时按 info 订阅
    expect(mihomoPanelApi.logs).toHaveBeenLastCalledWith('gw', { level: 'info', after: 0 }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).toContain('log-level 是 silent')
    await vi.advanceTimersByTimeAsync(1000)
    expect(mihomoPanelApi.logs).toHaveBeenLastCalledWith('gw', { level: 'info', after: 2 }, expect.anything())
    const items = wrapper.findComponent(AgentLogLines).props('items') as Array<{ message: string; level: string }>
    expect(items.map(item => `${item.level}:${item.message}`)).toEqual(['INFO:[TCP] first', 'WARN:[DNS] second', 'ERROR:third'])
    expect(wrapper.text()).toContain('已跳过 4 条')
    expect(wrapper.text()).toContain('实时接收中')

    // 改等级：清空并从头按新等级取
    vi.mocked(mihomoPanelApi.logs).mockResolvedValueOnce(live([[4, 'debug', 'verbose']], { stream: 'debug' }))
    wrapper.findAllComponents(Select).at(-1)!.vm.$emit('update:modelValue', 'debug')
    await flushPromises()
    expect(mihomoPanelApi.logs).toHaveBeenLastCalledWith('gw', { level: 'debug', after: 0 }, expect.anything())
    expect((wrapper.findComponent(AgentLogLines).props('items') as Array<{ message: string }>).map(item => item.message)).toEqual(['verbose'])
    expect(wrapper.text()).not.toContain('已跳过')

    // 回到日志文件后不再轮询实时接口
    const calls = vi.mocked(mihomoPanelApi.logs).mock.calls.length
    wrapper.getComponent(Segmented).vm.$emit('update:modelValue', 'file')
    await vi.advanceTimersByTimeAsync(5000)
    expect(mihomoPanelApi.logs).toHaveBeenCalledTimes(calls)
  })

  it('asks for an Agent upgrade and stops polling when live logs are unsupported', async () => {
    vi.mocked(mihomoPanelApi.logs).mockRejectedValue({ response: { status: 409, data: { code: 'UPGRADE_REQUIRED', message: '请将此 Agent 升级到 1.8.56-go 或更新版本后使用实时日志。' } } })
    const wrapper = await render()
    wrapper.getComponent(Segmented).vm.$emit('update:modelValue', 'live')
    await flushPromises()
    expect(wrapper.text()).toContain('1.8.56-go')
    expect(wrapper.text()).toContain('前往升级 Agent')
    await vi.advanceTimersByTimeAsync(5000)
    expect(mihomoPanelApi.logs).toHaveBeenCalledTimes(1)
  })

  it('keeps continuation lines with their entry when filtering by level', () => {
    const entries = parseAgentLogs([
      'time="2026-11-11T10:00:00+08:00" level=debug msg="noise"',
      'stack line of noise',
      'time="2026-11-11T10:00:01+08:00" level=error msg="boom"',
      'stack line of boom'
    ].join('\n'))
    expect(filterLogLevel(entries, 'WARN').map(entry => entry.message)).toEqual(['boom', 'stack line of boom'])
    expect(filterLogLevel(entries, '')).toHaveLength(4)
  })
})
