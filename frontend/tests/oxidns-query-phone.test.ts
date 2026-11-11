import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { oxidnsMonitorApi } from '@/api'
import OxiDNSQueryTable from '@/components/agents/OxiDNSQueryTable.vue'
import { useOxiDNSMonitor } from '@/composables/useOxiDNSMonitor'

// 手机宽度（< 640px）：只让 max-width: 639px 命中，其余断点保持桌面行为
vi.mock('@/api', () => ({ oxidnsMonitorApi: { overview: vi.fn(), queries: vi.fn(), clients: vi.fn() } }))
let wrapper: VueWrapper
let model: ReturnType<typeof useOxiDNSMonitor>
const details: unknown[] = []
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
const response = (data: unknown) => ({ data } as any)
const records = () => ({ success: true, recorder: 'queries', next_cursor: null, records: [{
  id: 7, created_at_ms: Date.now(), elapsed_ms: 1.5, client_ip: '192.0.2.5',
  questions_json: [{ name: 'phone-record.test', qtype: 28 }], error: null, has_response: true, rcode: 0,
}] })
const lastQuery = () => vi.mocked(oxidnsMonitorApi.queries).mock.lastCall![1]
const filterButton = () => wrapper.findAll('button').find(button => button.attributes('aria-label')?.startsWith('筛选'))!
const sheet = () => document.querySelector<HTMLElement>('[data-slot="sheet-content"]')

async function chooseInSheet(label: string, option: string) {
  await new DOMWrapper(sheet()!.querySelector<HTMLElement>(`[aria-label="${label}"]`)!).trigger('keydown', { key: 'ArrowDown' })
  await flushPromises()
  const item = [...document.querySelectorAll<HTMLElement>('[role="option"]')].find(node => node.textContent?.trim() === option)
  expect(item, option).toBeTruthy()
  item!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await flushPromises()
}

async function openSheet() {
  await filterButton().trigger('click')
  await flushPromises()
  expect(sheet()).not.toBeNull()
}

beforeEach(async () => {
  vi.clearAllMocks()
  details.length = 0
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: String(query).includes('639px'), media: query,
    addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })))
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response({
    success: true, recorders: ['queries', 'other'], core: { instance_id: 'core' }, metrics: {},
  }))
  vi.mocked(oxidnsMonitorApi.queries).mockResolvedValue(response(records()))
  vi.mocked(oxidnsMonitorApi.clients).mockResolvedValue(response({ success: true, clients: [] }))
  wrapper = mount(defineComponent({ setup() {
    model = useOxiDNSMonitor(ref('one'), ref('queries'))
    model.autoRefresh.value = false
    return () => h(OxiDNSQueryTable, {
      agentId: 'one', recorders: model.overview.value?.recorders || [], recorder: model.recorder.value,
      filters: model.filters.value, data: model.queries.value, loading: model.queriesLoading.value,
      error: model.queriesError.value, page: model.page.value, hasNext: model.hasNext.value,
      onFilter: model.applyFilters, onRecorder: model.selectRecorder, onDetail: (record: unknown) => details.push(record),
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

describe('OxiDNS query records on phones', () => {
  it('keeps search and 查询 visible and moves the other conditions into the filter sheet', async () => {
    expect(wrapper.find('input[placeholder="查询域名"]').exists()).toBe(true)
    expect(wrapper.findAll('button').some(button => button.text().includes('查询'))).toBe(true)
    // 列表上方不再平铺七个条件
    for (const label of ['查询记录器', '查询时间范围', '查询结果筛选', '客户端 IP']) expect(wrapper.find(`[aria-label="${label}"]`).exists()).toBe(false)
    expect(wrapper.find('input[placeholder="例如 A、AAAA"]').exists()).toBe(false)
    expect(filterButton().attributes('aria-label')).toBe('筛选')

    await openSheet()
    const content = sheet()!
    for (const label of ['查询记录器', '查询时间范围', '查询结果筛选', '客户端 IP']) expect(content.querySelector(`[aria-label="${label}"]`), label).not.toBeNull()
    for (const placeholder of ['例如 A、AAAA', '例如 NOERROR', '按实际执行标签查询']) expect(content.querySelector(`input[placeholder="${placeholder}"]`), placeholder).not.toBeNull()
    expect(content.textContent).toContain('数据来源')
    expect(content.textContent).toContain('匹配器标签')
  })

  it('applies filters set in the sheet to the query request and counts them on the badge', async () => {
    await wrapper.get('input[placeholder="查询域名"]').setValue('phone.test')
    await openSheet()
    // 选择项立即生效，面板保持打开
    await chooseInSheet('查询结果筛选', '处理错误')
    expect(lastQuery()).toMatchObject({ status: 'error', qname: 'phone.test' })
    expect(filterButton().attributes('aria-label')).toBe('筛选，已启用 1 项')
    expect(sheet()).not.toBeNull()

    // 输入框点「应用」后生效并关闭面板
    await new DOMWrapper(sheet()!.querySelector<HTMLInputElement>('input[placeholder="例如 A、AAAA"]')!).setValue('AAAA')
    await new DOMWrapper(sheet()!.querySelector<HTMLInputElement>('input[placeholder="例如 NOERROR"]')!).setValue('NXDOMAIN')
    const calls = vi.mocked(oxidnsMonitorApi.queries).mock.calls.length
    const apply = [...sheet()!.querySelectorAll('button')].find(button => button.textContent?.trim() === '应用')!
    apply.click()
    await flushPromises()
    expect(oxidnsMonitorApi.queries).toHaveBeenCalledTimes(calls + 1)
    expect(lastQuery()).toMatchObject({ status: 'error', qtype: 'AAAA', rcode: 'NXDOMAIN', qname: 'phone.test' })
    expect(model.filters.value).toMatchObject({ status: 'error', qtype: 'AAAA', rcode: 'NXDOMAIN' })
    expect(filterButton().attributes('aria-label')).toBe('筛选，已启用 3 项')
    expect(filterButton().text()).toContain('3')

    // 关闭面板后条件仍然生效：页面上的「查询」带着全部条件
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(lastQuery()).toMatchObject({ status: 'error', qtype: 'AAAA', rcode: 'NXDOMAIN', qname: 'phone.test' })

    await openSheet()
    const clear = [...sheet()!.querySelectorAll('button')].find(button => button.textContent?.trim() === '清除筛选')!
    clear.click()
    await flushPromises()
    expect(lastQuery()).toMatchObject({ status: 'all', qname: 'phone.test' })
    expect(lastQuery().qtype).toBeUndefined()
    expect(lastQuery().rcode).toBeUndefined()
    expect(filterButton().attributes('aria-label')).toBe('筛选')
  })

  it('switches the data source from the sheet', async () => {
    await openSheet()
    await chooseInSheet('查询记录器', 'other')
    expect(lastQuery()).toMatchObject({ recorder: 'other' })
  })

  it('renders records as compact two-line rows that open the existing detail', async () => {
    const shell = wrapper.get('.oxidns-query-phone')
    const row = shell.get('tbody tr')
    const cells = row.findAll('td')
    expect(cells).toHaveLength(7)
    expect(cells[1].text()).toContain('phone-record.test')
    expect(cells[1].text()).toContain('AAAA')
    // 今天的记录只显示时分秒，完整时间留在悬停提示
    expect(cells[0].text()).toMatch(/^\d{2}:\d{2}:\d{2}$/)
    expect(cells[0].attributes('title')).toBeTruthy()
    expect(cells[2].text()).toContain('192.0.2.5')
    expect(cells[3].text()).toContain('NOERROR')
    expect(cells[5].text()).toContain('1.5')
    const detail = row.get('button[aria-label="查看查询 7 详情"]')
    expect(detail.classes()).toContain('query-detail')
    await detail.trigger('click')
    expect(details).toHaveLength(1)
  })
})
