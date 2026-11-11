import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Logs from '@/views/Logs.vue'
import { Select } from '@/components/ui/select'
import api from '@/api'

// Synthetic data only. Keep the view and its UI components real; intercept HTTP
// and notifications so this suite cannot read or clear an actual log file.
vi.mock('@/api', () => ({ default: { get: vi.fn(), post: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({
  confirmDanger: vi.fn(async () => false),
  notify: { error: vi.fn(), success: vi.fn(), info: vi.fn() }
}))

const longLogger = `backend.synthetic.${'long_component_'.repeat(32)}`
const longUrl = `https://synthetic.invalid/log-test?detail=${'unbroken'.repeat(160)}`
const rawTrace = 'Traceback (most recent call last):\n  File "/synthetic/example.py", line 42\n    raise RuntimeError("synthetic failure")\nRuntimeError: synthetic failure'
const syntheticLogs = [
  `2026-10-09 12:00:00,123 - ${longLogger} - INFO - Request ${longUrl}`,
  '2026-10-09 12:00:01,456 - backend.synthetic - ERROR - Synthetic failure message',
  rawTrace
]

const wrappers: ReturnType<typeof mount>[] = []
const scrollIntoView = vi.fn()
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
let overflowY: 'visible' | 'auto'

beforeAll(() => {
  Object.defineProperty(Element.prototype, 'scrollIntoView', {
    configurable: true,
    value: scrollIntoView
  })
})

afterAll(() => {
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else delete (Element.prototype as any).scrollIntoView
})

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
  overflowY = 'auto'

  // jsdom does not apply responsive CSS or implement scrolling. Supply only
  // the browser geometry branch; actual layout is covered by browser checks.
  const originalGetComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element, pseudoElement) => {
    const style = originalGetComputedStyle(element, pseudoElement)
    if (!element.classList.contains('logs-container')) return style
    return new Proxy(style, {
      get(target, property) {
        if (property === 'overflowY') return overflowY
        const value = Reflect.get(target, property, target)
        return typeof value === 'function' ? value.bind(target) : value
      }
    })
  })

  vi.mocked(api.get).mockImplementation(async (url) => {
    if (url === '/logs/tail') {
      return { data: {
        success: true,
        logs: [...syntheticLogs],
        total_lines: syntheticLogs.length,
        filtered_lines: syntheticLogs.length
      } } as any
    }
    if (url === '/logs/info') {
      return { data: { success: true, exists: true, path: 'synthetic.log', size_mb: 0.01 } } as any
    }
    throw new Error(`Unexpected API request: ${url}`)
  })
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  expect(api.post).not.toHaveBeenCalled()
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

async function render(mode: 'visible' | 'auto' = 'auto') {
  overflowY = mode
  const wrapper = mount(Logs, { attachTo: document.body })
  wrappers.push(wrapper)
  const container = wrapper.get('.logs-container').element as HTMLElement
  Object.defineProperty(container, 'scrollHeight', { configurable: true, value: 1400 })
  container.scrollTop = 23
  await flushPromises()
  return { wrapper, container }
}

function tailCalls() {
  return vi.mocked(api.get).mock.calls.filter(([url]) => url === '/logs/tail')
}

async function clickButton(wrapper: ReturnType<typeof mount>, text: string) {
  const button = wrapper.findAll('button').find(candidate => candidate.text().trim() === text)
  expect(button, `button ${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

async function choose(wrapper: ReturnType<typeof mount>, label: string, value: string) {
  const select = wrapper.findAllComponents(Select).find(component => component.find(`[aria-label="${label}"]`).exists())
  expect(select, `select ${label}`).toBeDefined()
  // Exercise the real Select's public model event without depending on its
  // floating-position calculations, which require a browser layout engine.
  select!.vm.$emit('update:modelValue', value)
  await flushPromises()
}

describe('responsive logs with synthetic API data', () => {
  it('preserves complete long URLs, logger names and multiline raw stack traces', async () => {
    const { wrapper } = await render('visible')
    expect(wrapper.findAll('.log-row')).toHaveLength(syntheticLogs.length)
    expect(wrapper.get('.logs-list').element.textContent).toContain(longUrl)
    expect(wrapper.get('.logs-list').element.textContent).toContain(rawTrace)
    // 来源去掉 backend. 前缀后完整保留
    expect(wrapper.findAll('.log-logger').map(logger => logger.text())).toContain(`[${longLogger.replace(/^backend\./, '')}]`)
    expect(wrapper.findAll('.log-time').map(time => time.text())).toContain('12:00:00.123')
    expect(wrapper.get('.log-time').attributes('title')).toBe('2026-10-09 12:00:00,123')
    expect(wrapper.get('[aria-label="搜索关键词…"]').exists()).toBe(true)
    expect(wrapper.findAll('button').some(button => button.text().trim() === '暂停')).toBe(true)
  })

  it('colours messages like the Mihomo log view and marks the search keyword', async () => {
    const { wrapper } = await render()
    // 引号字符串着色；原始堆栈作为续行，不画分隔线
    expect(wrapper.findAll('.log-tk-str').map(node => node.text())).toContain('"synthetic failure"')
    expect(wrapper.findAll('.log-row').map(row => row.classes().includes('is-raw'))).toEqual([false, false, true])
    expect(wrapper.find('.log-hit').exists()).toBe(false)

    await wrapper.get('[aria-label="搜索关键词…"]').setValue('Failure')
    expect(wrapper.findAll('.log-hit').map(node => node.text())).toEqual(['failure', 'failure', 'failure'])
    expect(wrapper.get('.logs-list').element.textContent).toContain(rawTrace)
  })

  it.each(['visible', 'auto'] as const)('preserves the initial scroll behavior and does not jump on manual refresh (%s)', async mode => {
    const { wrapper, container } = await render(mode)
    expect(api.get).toHaveBeenCalledWith('/logs/tail', { params: { lines: 200 } })
    expect(api.get).toHaveBeenCalledWith('/logs/info')
    expect(container.scrollTop).toBe(mode === 'auto' ? 1400 : 23)
    expect(scrollIntoView).not.toHaveBeenCalled()
    // Desktop starts at its own bottom; simulate reading an older entry before
    // refreshing to ensure refresh does not silently move the reader again.
    container.scrollTop = 23
    await clickButton(wrapper, '刷新')
    expect(tailCalls()).toHaveLength(2)
    expect(container.scrollTop).toBe(23)
    expect(scrollIntoView).not.toHaveBeenCalled()
  })

  it('uses the page bottom marker on mobile instead of changing a nested scroll offset', async () => {
    const { wrapper, container } = await render('visible')
    await clickButton(wrapper, '到底部')
    expect(scrollIntoView).toHaveBeenCalledWith(expect.objectContaining({ block: 'end' }))
    expect(scrollIntoView.mock.contexts[0]).toBe(wrapper.get('[data-testid="logs-end"]').element)
    expect(container.scrollTop).toBe(23)
  })

  it('scrolls the log container on desktop without scrolling the whole page', async () => {
    const { wrapper, container } = await render('auto')
    expect(container.scrollTop).toBe(1400)
    container.scrollTop = 23
    await clickButton(wrapper, '到底部')
    expect(container.scrollTop).toBe(1400)
    expect(scrollIntoView).not.toHaveBeenCalled()
  })

  it('keeps the debounced search and line-count request parameters; level chips filter locally', async () => {
    const { wrapper } = await render()
    await wrapper.get('[aria-label="搜索关键词…"]').setValue('synthetic failure')
    await vi.advanceTimersByTimeAsync(299)
    expect(tailCalls()).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(api.get).toHaveBeenLastCalledWith('/logs/tail', { params: { lines: 200, search: 'synthetic failure' } })

    await choose(wrapper, '显示行数', '1000')
    expect(api.get).toHaveBeenLastCalledWith('/logs/tail', { params: { lines: 1000, search: 'synthetic failure' } })

    // 级别芯片只在本地筛选：关掉 INFO 后 INFO 行消失，ERROR 行及其后的原始堆栈保留，不再请求
    const before = tailCalls().length
    await clickButton(wrapper, 'INFO')
    expect(tailCalls()).toHaveLength(before)
    expect(wrapper.get('.logs-list').element.textContent).not.toContain(longUrl)
    expect(wrapper.get('.logs-list').element.textContent).toContain('Synthetic failure message')
    expect(wrapper.get('.logs-list').element.textContent).toContain(rawTrace)
  })

  it('tails every three seconds, follows the mobile bottom, pauses, and clears timers on unmount', async () => {
    const { wrapper } = await render('visible')
    await vi.advanceTimersByTimeAsync(2999)
    expect(tailCalls()).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(tailCalls()).toHaveLength(2)
    expect(scrollIntoView).toHaveBeenCalledWith(expect.objectContaining({ block: 'end' }))

    // 暂停后不再尾随；继续时立即补拉一次
    await clickButton(wrapper, '暂停')
    await vi.advanceTimersByTimeAsync(9000)
    expect(tailCalls()).toHaveLength(2)
    await clickButton(wrapper, '继续')
    expect(tailCalls()).toHaveLength(3)

    // Both the tail interval and an outstanding search debounce must stop.
    await wrapper.get('[aria-label="搜索关键词…"]').setValue('pending search')
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    await vi.advanceTimersByTimeAsync(10000)
    await flushPromises()
    expect(tailCalls()).toHaveLength(3)
  })
})
