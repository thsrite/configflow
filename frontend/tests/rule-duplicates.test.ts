import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosError, AxiosHeaders, CanceledError, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import Rules from '@/views/Rules.vue'
import api from '@/api'
import { scopedRequests, setActiveProfileId } from '@/profileContext'
import { notify } from '@/lib/feedback'

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))

// Only the HTTP transport is simulated. Rules, its dialogs, pagination, API
// wrapper and request interceptors run unchanged against synthetic responses.
type ScanHandler = (config: InternalAxiosRequestConfig) => Promise<AxiosResponse>
let scanHandler: ScanHandler
const requests: InternalAxiosRequestConfig[] = []
const wrappers: VueWrapper[] = []
const originalAdapter = api.defaults.adapter
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')

function response(config: InternalAxiosRequestConfig, data: unknown): AxiosResponse {
  return { config, data, status: 200, statusText: 'OK', headers: new AxiosHeaders() }
}

function result(groupCount = 1, firstOccurrences = 2, prefix = 'duplicate') {
  return {
    success: true,
    duplicates: Array.from({ length: groupCount }, (_, groupIndex) => ({
      rule_type: 'DOMAIN',
      value: `${prefix}-${groupIndex + 1}.test`,
      count: groupIndex === 0 ? firstOccurrences : 2,
      policy_conflict: groupIndex === 0,
      occurrences: Array.from({ length: groupIndex === 0 ? firstOccurrences : 2 }, (_, index) => ({
        source_type: 'ruleset', source: `Source ${index + 1}`,
        rule_id: `ruleset-${index + 1}`, policy: index % 2 ? 'REJECT' : 'DIRECT',
        priority: index + 1, line: `DOMAIN,${prefix}-${groupIndex + 1}.test`, line_no: index + 1
      }))
    })),
    stats: { rules_checked: 1, rulesets_checked: 64, failed_rulesets: [] as string[] },
    elapsed_time: 123
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

beforeEach(() => {
  requests.length = 0
  scopedRequests.value = 0
  setActiveProfileId('duplicates-profile')
  scanHandler = async config => response(config, result())
  api.defaults.adapter = async config => {
    if (config.url === '/rules/find-duplicates') {
      requests.push(config)
      return scanHandler(config)
    }
    if (config.url === '/rules') return response(config, [{
      id: 'direct-one', itemType: 'rule', rule_type: 'DOMAIN-SUFFIX',
      value: 'example.test', policy: 'DIRECT', enabled: true
    }])
    return response(config, [])
  }
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: query.includes('prefers-reduced-motion'), media: query, onchange: null,
    addListener: vi.fn(), removeListener: vi.fn(),
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn()
  })))
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
})

afterEach(async () => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  await flushPromises()
  api.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
  document.body.innerHTML = ''
  sessionStorage.clear()
})

async function render() {
  const wrapper = mount(Rules, { attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

function dialog() {
  const element = document.querySelector<HTMLElement>('[role="dialog"]')
  expect(element, 'duplicate dialog').toBeTruthy()
  return element!
}

async function clickText(text: string, scope: ParentNode = document) {
  const button = Array.from(scope.querySelectorAll('button')).find(item => item.textContent?.trim() === text)
  expect(button, `button ${text}`).toBeTruthy()
  button!.click()
  await flushPromises()
}

async function openScan() {
  const trigger = Array.from(document.querySelectorAll('button')).find(item => item.textContent?.trim() === '更多')
  expect(trigger).toBeTruthy()
  trigger!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  await flushPromises()
  const item = Array.from(document.querySelectorAll<HTMLElement>('[role="menuitem"]'))
    .find(element => element.textContent?.trim() === '查找重复')
  expect(item).toBeTruthy()
  item!.click()
  await flushPromises()
}

describe('duplicate rule scan', () => {
  it('keeps large results bounded while letting users reach every group and occurrence', async () => {
    scanHandler = async config => response(config, result(5000, 1003))
    await render()
    await openScan()

    expect(dialog().textContent).toContain('5000 组重复')
    expect(dialog().textContent).toContain('策略冲突')
    expect(dialog().querySelectorAll('article').length).toBeLessThanOrEqual(25)
    expect(dialog().querySelectorAll('*').length).toBeLessThan(2000)
    const first = dialog().querySelector<HTMLElement>('article')!
    expect(first.textContent).toContain('Source 1')
    expect(first.textContent).not.toContain('Source 1003')
    const occurrenceNav = first.querySelector('nav')!
    await clickText('下一页', occurrenceNav)
    expect(first.textContent).toContain('Source 11')
    expect(first.textContent).not.toContain('Source 1 ·')
    await clickText('末页', occurrenceNav)
    expect(first.textContent).toContain('Source 1003')
    expect(first.textContent).not.toContain('Source 11 ·')
    await clickText('首页', occurrenceNav)
    expect(first.textContent).toContain('Source 1')

    const groupsNav = dialog().querySelector('[aria-label="重复规则分页"]')!
    await clickText('下一页', groupsNav)
    expect(dialog().textContent).toContain('duplicate-21.test')
    expect(dialog().textContent).not.toContain('duplicate-1.test')
    await clickText('末页', groupsNav)
    expect(dialog().textContent).toContain('duplicate-5000.test')
    expect(dialog().querySelectorAll('article').length).toBeLessThanOrEqual(25)
    expect(dialog().querySelectorAll('*').length).toBeLessThan(2000)
    await clickText('上一页', groupsNav)
    expect(dialog().textContent).toContain('duplicate-4961.test')
    await clickText('首页', groupsNav)
    expect(dialog().textContent).toContain('duplicate-1.test')
    expect(requests).toHaveLength(1)
  })

  it('reuses an in-flight scan and its completed snapshot until explicit rescan', async () => {
    const pending = deferred<AxiosResponse>()
    scanHandler = () => pending.promise
    await render()
    await openScan()
    expect(requests).toHaveLength(1)
    expect(scopedRequests.value).toBe(1)
    expect(requests[0]!.headers.get('X-ConfigFlow-Profile')).toBe('duplicates-profile')
    expect(dialog().querySelector('[role="status"]')).toBeTruthy()
    const rescan = Array.from(dialog().querySelectorAll('button')).find(button => button.textContent?.trim() === '重新扫描')!
    expect(rescan.disabled).toBe(true)
    rescan.click()
    await clickText('关闭', dialog())
    await openScan()
    expect(requests).toHaveLength(1)
    expect(dialog().querySelector('[role="status"]')).toBeTruthy()
    pending.resolve(response(requests[0]!, result(40)))
    await flushPromises()
    expect(scopedRequests.value).toBe(0)
    await clickText('末页', dialog().querySelector('[aria-label="重复规则分页"]')!)
    expect(dialog().textContent).toContain('duplicate-40.test')
    await clickText('关闭', dialog())
    await openScan()
    expect(requests).toHaveLength(1)
    expect(dialog().textContent).toContain('duplicate-40.test')

    scanHandler = async config => response(config, result(1, 2, 'fresh'))
    await clickText('重新扫描', dialog())
    expect(requests).toHaveLength(2)
    expect(dialog().textContent).toContain('fresh-1.test')
    expect(dialog().textContent).not.toContain('duplicate-40.test')
    expect(dialog().querySelector('[aria-label="重复规则分页"]')).toBeNull()
  })

  it('keeps a failed response visible across reopen and allows a successful retry', async () => {
    scanHandler = async config => response(config, { success: false, message: '规则缓存无法读取' })
    await render()
    await openScan()
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('规则缓存无法读取')
    await clickText('关闭', dialog())
    await openScan()
    expect(requests).toHaveLength(1)
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('规则缓存无法读取')
    scanHandler = async config => response(config, result())
    await clickText('重新扫描', dialog())
    expect(requests).toHaveLength(2)
    expect(dialog().textContent).toContain('duplicate-1.test')
    expect(dialog().querySelector('[role="alert"]')).toBeNull()
  })

  it.each(['ECONNABORTED', 'ETIMEDOUT'])('shows a persistent timeout error for %s', async code => {
    scanHandler = async config => { throw new AxiosError('timeout', code, config) }
    await render()
    await openScan()
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('查重超时')
    expect(scopedRequests.value).toBe(0)
    expect(dialog().querySelector('[role="status"]')).toBeNull()
    const rescan = Array.from(dialog().querySelectorAll('button')).find(button => button.textContent?.trim() === '重新扫描')!
    expect(rescan.disabled).toBe(false)
  })

  it('shows the server error in the dialog when the request fails', async () => {
    scanHandler = async config => {
      const failed = { ...response(config, { message: '读取规则集失败，请稍后重试' }), status: 500 }
      throw new AxiosError('server error', 'ERR_BAD_RESPONSE', config, undefined, failed)
    }
    await render()
    await openScan()
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('读取规则集失败，请稍后重试')
    expect(scopedRequests.value).toBe(0)
    expect(dialog().querySelector('[role="status"]')).toBeNull()
  })

  it('aborts the transport and releases the profile lock when leaving the page', async () => {
    const notifyError = vi.spyOn(notify, 'error')
    scanHandler = config => new Promise((_resolve, reject) => {
      config.signal!.addEventListener!('abort', () => reject(new CanceledError('canceled', config)))
    })
    const wrapper = await render()
    await openScan()
    const signal = requests[0]!.signal!
    expect(signal.aborted).toBe(false)
    expect(scopedRequests.value).toBe(1)
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(scopedRequests.value).toBe(0)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(notifyError).not.toHaveBeenCalled()
  })

  it('ignores a late response from an old page after a new scan succeeds', async () => {
    const old = deferred<AxiosResponse>()
    scanHandler = () => old.promise
    const wrapper = await render()
    await openScan()
    const oldRequest = requests[0]!
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    await flushPromises()
    scanHandler = async config => response(config, result(1, 2, 'current'))
    await render()
    await openScan()
    expect(dialog().textContent).toContain('current-1.test')
    old.resolve(response(oldRequest, result(1, 2, 'stale')))
    await flushPromises()
    expect(dialog().textContent).toContain('current-1.test')
    expect(dialog().textContent).not.toContain('stale-1.test')
    expect(dialog().querySelector('[role="alert"]')).toBeNull()
    expect(scopedRequests.value).toBe(0)
  })

  it('preserves incomplete-scan warnings alongside an empty result', async () => {
    const data = result(0)
    data.stats.failed_rulesets = ['unavailable-source']
    scanHandler = async config => response(config, data)
    await render()
    await openScan()
    expect(dialog().textContent).toContain('未发现重复')
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('unavailable-source')
    expect(dialog().querySelectorAll('article')).toHaveLength(0)
    expect(dialog().querySelector('[aria-label="重复规则分页"]')).toBeNull()
  })
})
