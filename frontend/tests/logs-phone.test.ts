import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Logs from '@/views/Logs.vue'
import api from '@/api'

// 只用合成数据，拦截接口，不会读取或清空真实日志；手机宽度只让 max-width: 639px 命中
vi.mock('@/api', () => ({ default: { get: vi.fn(), post: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({
  confirmDanger: vi.fn(async () => false),
  notify: { error: vi.fn(), success: vi.fn(), info: vi.fn() }
}))

const longMessage = `Synthetic request ${'detail-'.repeat(40)}end`
const syntheticLogs = [
  '2026-10-09 12:00:00,123 - backend.synthetic.scheduler - INFO - Short synthetic message',
  `2026-10-09 12:00:01,456 - backend.synthetic - WARNING - ${longMessage}`,
  'Traceback (most recent call last):'
]
const wrappers: ReturnType<typeof mount>[] = []
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: String(query).includes('639px'), media: query,
    addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })))
  vi.mocked(api.get).mockImplementation(async (url) => {
    if (url === '/logs/tail') return { data: { success: true, logs: [...syntheticLogs], total_lines: 3, filtered_lines: 3 } } as any
    if (url === '/logs/info') return { data: { success: true, exists: true, path: 'synthetic.log', size_mb: 0.01 } } as any
    throw new Error(`Unexpected API request: ${url}`)
  })
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  expect(api.post).not.toHaveBeenCalled()
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
  document.body.innerHTML = ''
})

async function render() {
  const wrapper = mount(Logs, { attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

describe('system logs on phones', () => {
  it('lays each entry out on two lines: time, logger and level, then the message', async () => {
    const wrapper = await render()
    expect(wrapper.get('.logs-list').classes()).toContain('logs-phone')
    const [short, long, raw] = wrapper.findAll('.log-row')
    // 沿用原有的四个元素，由手机网格排成 时间 · 来源 · 级别 一行、正文一行
    expect(short.findAll(':scope > span').map(node => node.classes()[0]).sort()).toEqual(['log-level', 'log-logger', 'log-message', 'log-time'])
    expect(short.get('.log-time').text()).toBe('12:00:00.123')
    expect(short.get('.log-logger').text()).toBe('[synthetic.scheduler]')
    expect(short.get('.log-level').text()).toBe('INFO')
    // 短正文不折叠、不可点
    expect(short.get('.log-message').classes()).not.toContain('is-clamped')
    expect(short.get('.log-message').attributes('role')).toBeUndefined()
    expect(raw.classes()).toContain('is-raw')
    expect(long.get('.log-level').text()).toBe('WARN')
  })

  it('clamps long messages to three lines and expands them on tap without losing text', async () => {
    const wrapper = await render()
    const message = () => wrapper.findAll('.log-row')[1].get('.log-message')
    expect(message().classes()).toEqual(expect.arrayContaining(['is-long', 'is-clamped']))
    expect(message().attributes('aria-expanded')).toBe('false')
    expect(message().text()).toContain(longMessage)
    await message().trigger('click')
    expect(message().classes()).not.toContain('is-clamped')
    expect(message().attributes('aria-expanded')).toBe('true')
    await message().trigger('keydown', { key: 'Enter' })
    expect(message().classes()).toContain('is-clamped')
  })

  it('keeps the desktop layout when the phone query does not match', async () => {
    vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: !String(query).includes('639px'), media: query,
      addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })))
    const wrapper = await render()
    expect(wrapper.get('.logs-list').classes()).not.toContain('logs-phone')
    expect(wrapper.findAll('.log-message.is-clamped')).toHaveLength(0)
  })
})
