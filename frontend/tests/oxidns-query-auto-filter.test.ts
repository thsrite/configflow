import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { oxidnsMonitorApi } from '@/api'
import OxiDNSQueryTable from '@/components/agents/OxiDNSQueryTable.vue'
import { useOxiDNSMonitor } from '@/composables/useOxiDNSMonitor'

vi.mock('@/api', () => ({ oxidnsMonitorApi: { overview: vi.fn(), queries: vi.fn(), clients: vi.fn() } }))
let wrapper: VueWrapper
let model: ReturnType<typeof useOxiDNSMonitor>
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
const response = (data: unknown) => ({ data } as any)
const records = (id = 1) => ({ success: true, recorder: 'queries', next_cursor: 'older-page', records: [{
  id, created_at_ms: 1700000000000, elapsed_ms: 1, client_ip: '192.0.2.5',
  questions_json: [{ name: `record-${id}.test`, qtype: 1 }], error: null, has_response: true, rcode: 0,
}] })

async function choose(label: string, option: string) {
  await wrapper.get(`[aria-label="${label}"]`).trigger('keydown', { key: 'ArrowDown' })
  await flushPromises()
  const item = [...document.querySelectorAll<HTMLElement>('[role="option"]')]
    .find(node => node.textContent?.trim() === option)
  expect(item, option).toBeTruthy()
  item!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await flushPromises()
}

beforeEach(async () => {
  vi.clearAllMocks()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: !String(query).includes('639px'), addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })))
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response({
    success: true, recorders: ['queries', 'other'], core: { instance_id: 'core' }, metrics: {},
  }))
  vi.mocked(oxidnsMonitorApi.queries).mockResolvedValue(response(records()))
  wrapper = mount(defineComponent({ setup() {
    model = useOxiDNSMonitor(ref('one'), ref('queries'))
    model.autoRefresh.value = false
    return () => h(OxiDNSQueryTable, {
      agentId: 'one', recorders: model.overview.value?.recorders || [], recorder: model.recorder.value,
      filters: model.filters.value, data: model.queries.value, loading: model.queriesLoading.value,
      error: model.queriesError.value, page: model.page.value, hasNext: model.hasNext.value,
      onFilter: model.applyFilters, onRecorder: model.selectRecorder,
      onNext: model.nextPage, onPrevious: model.previousPage,
    })
  } }), { attachTo: document.body })
  await flushPromises()
})

afterEach(() => {
  wrapper?.unmount()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
  document.body.innerHTML = ''
})

describe('OxiDNS query selection applies immediately', () => {
  it('loads each selected result and time range once without submitting the form', async () => {
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(1)
    let requests = 1
    for (const [label, status] of [['处理错误', 'error'], ['有应答', 'has_response'], ['无应答', 'no_response'], ['全部结果', 'all']]) {
      await choose('查询结果筛选', label)
      expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(++requests)
      expect(vi.mocked(oxidnsMonitorApi.queries).mock.lastCall?.[1]).toMatchObject({ status, cursor: undefined })
    }
    await choose('查询时间范围', '最近 24 小时')
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(++requests)
    const params = vi.mocked(oxidnsMonitorApi.queries).mock.lastCall![1]
    expect(Number(params.until_ms) - Number(params.since_ms)).toBe(86400000)
    await choose('查询时间范围', '全部时间')
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(++requests)
    expect(vi.mocked(oxidnsMonitorApi.queries).mock.lastCall?.[1].since_ms).toBeUndefined()
    await choose('查询记录器', 'other')
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(++requests)
    expect(vi.mocked(oxidnsMonitorApi.queries).mock.lastCall?.[1]).toMatchObject({ recorder: 'other', status: 'all' })
  })

  it('keeps text entry manual and includes the whole draft when selecting a filter', async () => {
    await wrapper.get('input[placeholder="查询域名"]').setValue('example.test')
    await wrapper.get('input[placeholder="例如 A、AAAA"]').setValue('AAAA')
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(1)
    await choose('查询结果筛选', '有应答')
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(2)
    expect(vi.mocked(oxidnsMonitorApi.queries).mock.lastCall?.[1]).toMatchObject({
      qname: 'example.test', qtype: 'AAAA', status: 'has_response',
    })
    await wrapper.get('input[placeholder="查询域名"]').setValue('updated.test')
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(2)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(3)
    expect(vi.mocked(oxidnsMonitorApi.queries).mock.lastCall?.[1].qname).toBe('updated.test')
  })

  it('returns to the first page and ignores an older request after a new selection', async () => {
    model.nextPage()
    await flushPromises()
    expect(model.page.value).toBe(2)
    let finish!: (result: unknown) => void
    vi.mocked(oxidnsMonitorApi.queries).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }) as any)
    await choose('查询结果筛选', '处理错误')
    expect(model.page.value).toBe(1)
    const oldSignal = vi.mocked(oxidnsMonitorApi.queries).mock.lastCall![2]?.signal
    expect(vi.mocked(oxidnsMonitorApi.queries).mock.lastCall![1].cursor).toBeUndefined()
    await choose('查询结果筛选', '无应答')
    expect(oldSignal?.aborted).toBe(true)
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(4)
    finish(response(records(99)))
    await flushPromises()
    expect(model.queries.value?.records[0].id).toBe(1)
    expect(model.filters.value.status).toBe('no_response')
  })
})
