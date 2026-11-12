import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { oxidnsMonitorApi } from '@/api'
import { ListboxFilter } from 'reka-ui'
import OxiDNSClientSelect from '@/components/agents/OxiDNSClientSelect.vue'
import OxiDNSQueryTable from '@/components/agents/OxiDNSQueryTable.vue'
import { emptyQueryFilters, type OxiDNSQuery } from '@/types/oxidnsMonitor'

vi.mock('@/api', () => ({ oxidnsMonitorApi: { clients: vi.fn() } }))
const wrappers: VueWrapper[] = []
let scrollDescriptor: PropertyDescriptor | undefined
const result = (ips = ['192.0.2.1', '192.0.2.10']) => ({ data: { success: true, recorder: 'recorder', clients: ips.map(ip => ({ ip, count: 2 })), sample_size: 4, sample_limited: false } } as any)
function deferred() {
  let resolve!: (value: any) => void
  const promise = new Promise(yes => { resolve = yes })
  return { resolve, promise }
}
function mountPicker(props: Record<string, any> = {}) {
  const wrapper = mount(OxiDNSClientSelect, { attachTo: document.body, props: { modelValue: '', agentId: 'one', recorder: 'recorder', range: '1h', ...props } })
  wrappers.push(wrapper)
  return wrapper
}
async function open(wrapper: VueWrapper) {
  await wrapper.get('[role="combobox"]').trigger('click')
  await flushPromises()
}
async function choose(ip: string) {
  const option = [...document.querySelectorAll<HTMLElement>('[role="option"]')].find(node => node.textContent?.includes(ip))
  expect(option).toBeTruthy()
  option!.click()
  await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(oxidnsMonitorApi.clients).mockResolvedValue(result())
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: !String(query).includes('639px'), addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })))
  scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.unstubAllGlobals()
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})

describe('existing OxiDNS clients', () => {
  it('keeps every typed character and focus while filtering and loading clients', async () => {
    vi.useFakeTimers()
    const wrapper = mountPicker({ allowAll: false })
    await open(wrapper)
    const input = new DOMWrapper(document.querySelector<HTMLInputElement>('input[aria-label="搜索已有客户端 IP"]')!)
    input.element.focus()
    let typed = ''
    for (const character of '192.0.2.10') {
      typed += character
      input.element.value += character
      await input.trigger('input')
      await flushPromises()
      expect(input.element.value).toBe(typed)
      expect(document.activeElement).toBe(input.element)
      await vi.advanceTimersByTimeAsync(50)
    }
    expect(oxidnsMonitorApi.clients).toHaveBeenCalledTimes(1)
    expect(document.body.textContent).not.toContain('正在读取客户端')
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.clients).mockImplementationOnce(() => pending.promise as any)
    await vi.advanceTimersByTimeAsync(300)
    expect(vi.mocked(oxidnsMonitorApi.clients).mock.lastCall?.[1].search).toBe(typed)
    expect(document.body.textContent).toContain('正在读取客户端')
    expect(input.element.value).toBe(typed)
    pending.resolve(result(['192.0.2.10']))
    await flushPromises()
    expect(input.element.value).toBe(typed)
    expect(document.activeElement).toBe(input.element)
    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['192.0.2.10'])
  })
  it('scopes clients to the Agent clock even when the browser is eight hours ahead', async () => {
    vi.useFakeTimers()
    const agentNow = Date.parse('2026-10-20T12:30:00Z')
    vi.setSystemTime(agentNow + 8 * 3600000)
    const wrapper = mountPicker({ collectedAt: new Date(agentNow).toISOString() })
    await open(wrapper)
    expect(oxidnsMonitorApi.clients).toHaveBeenLastCalledWith('one', expect.objectContaining({ since_ms: agentNow - 3600000, until_ms: agentNow }), expect.anything())
  })
  it('omits both time bounds when selecting historical clients', async () => {
    const wrapper = mountPicker({ range: 'all' })
    await open(wrapper)
    expect(oxidnsMonitorApi.clients).toHaveBeenLastCalledWith('one', expect.objectContaining({ since_ms: undefined, until_ms: undefined }), expect.anything())
  })
  it('loads a standalone time-scoped list only when opened and selects the full IP', async () => {
    const wrapper = mountPicker()
    expect(oxidnsMonitorApi.clients).not.toHaveBeenCalled()
    await open(wrapper)
    const [id, params] = vi.mocked(oxidnsMonitorApi.clients).mock.calls[0]
    expect(id).toBe('one')
    expect(params).toMatchObject({ recorder: 'recorder' })
    expect(Number(params.until_ms) - Number(params.since_ms)).toBe(3600000)
    expect(Object.keys(params).sort()).toEqual(['recorder', 'search', 'since_ms', 'until_ms'])
    expect(document.body.textContent).toContain('192.0.2.10')
    await choose('192.0.2.10')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['192.0.2.10'])
    expect(wrapper.get('[role="combobox"]').attributes('aria-expanded')).toBe('false')
  })
  it('accepts pasted IPv6 and edits during a pending search without losing text or the cursor', async () => {
    vi.useFakeTimers()
    const wrapper = mountPicker()
    await open(wrapper)
    const input = new DOMWrapper(document.querySelector<HTMLInputElement>('input[aria-label="搜索已有客户端 IP"]')!)
    input.element.focus()
    input.element.value = '2001:db8::51'
    await input.trigger('input', { inputType: 'insertFromPaste' })
    await flushPromises()
    expect(input.element.value).toBe('2001:db8::51')
    expect(document.body.textContent).not.toContain('没有找到匹配')
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.clients).mockImplementationOnce(() => pending.promise as any)
    await vi.advanceTimersByTimeAsync(300)
    const oldSignal = vi.mocked(oxidnsMonitorApi.clients).mock.lastCall?.[2]?.signal
    input.element.value = '2001:db8::5'
    await input.trigger('input', { inputType: 'deleteContentBackward' })
    input.element.setSelectionRange(5, 5)
    await flushPromises()
    expect(oldSignal?.aborted).toBe(true)
    pending.resolve(result(['2001:db8::51']))
    await flushPromises()
    expect(input.element.value).toBe('2001:db8::5')
    expect(input.element.selectionStart).toBe(5)
    expect(document.activeElement).toBe(input.element)
    vi.mocked(oxidnsMonitorApi.clients).mockResolvedValue(result(['2001:db8::5']))
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(vi.mocked(oxidnsMonitorApi.clients).mock.lastCall?.[1].search).toBe('2001:db8::5')
    expect(input.element.value).toBe('2001:db8::5')
    expect(input.element.selectionStart).toBe(5)
    await choose('2001:db8::5')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['2001:db8::5'])
  })
  it('searches historical clients separately and ignores obsolete responses', async () => {
    vi.useFakeTimers()
    const wrapper = mountPicker({ range: 'all' })
    await open(wrapper)
    const stale = deferred()
    vi.mocked(oxidnsMonitorApi.clients).mockImplementationOnce(() => stale.promise as any)
    wrapper.getComponent(ListboxFilter).vm.$emit('update:modelValue', '192.0.2.')
    await vi.advanceTimersByTimeAsync(300)
    const oldSignal = vi.mocked(oxidnsMonitorApi.clients).mock.calls[1][2]?.signal
    vi.mocked(oxidnsMonitorApi.clients).mockResolvedValue(result(['2001:db8::5']))
    wrapper.getComponent(ListboxFilter).vm.$emit('update:modelValue', '2001:db8::')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(oldSignal?.aborted).toBe(true)
    expect(vi.mocked(oxidnsMonitorApi.clients).mock.lastCall?.[1]).toMatchObject({ search: '2001:db8::', since_ms: undefined })
    stale.resolve(result(['192.0.2.99']))
    await flushPromises()
    expect(document.body.textContent).toContain('2001:db8::5')
    expect(document.body.textContent).not.toContain('192.0.2.99')
  })
  it('keeps the selected client available when it falls outside the recent sample', async () => {
    vi.mocked(oxidnsMonitorApi.clients).mockResolvedValue({ data: { ...result().data, sample_size: 10000, sample_limited: true } } as any)
    const wrapper = mountPicker({ modelValue: '192.0.2.250' })
    await open(wrapper)
    expect(document.querySelector('[role="listbox"]')?.textContent).toContain('192.0.2.250')
    expect(document.querySelector('[role="option"][aria-selected="true"]')?.textContent).toContain('192.0.2.250')
    expect(document.body.textContent).toContain('最近 10,000 条匹配记录')
  })
  it('aborts and clears options when changing devices, recorders or time ranges', async () => {
    const pending = deferred()
    vi.mocked(oxidnsMonitorApi.clients).mockImplementationOnce(() => pending.promise as any)
    const wrapper = mountPicker()
    await open(wrapper)
    const signal = vi.mocked(oxidnsMonitorApi.clients).mock.calls[0][2]?.signal
    await wrapper.setProps({ agentId: 'two', recorder: 'other', range: '24h' })
    expect(signal?.aborted).toBe(true)
    pending.resolve(result(['192.0.2.99']))
    await flushPromises()
    vi.mocked(oxidnsMonitorApi.clients).mockResolvedValue(result(['192.0.2.20']))
    await open(wrapper)
    expect(vi.mocked(oxidnsMonitorApi.clients).mock.lastCall?.[0]).toBe('two')
    expect(vi.mocked(oxidnsMonitorApi.clients).mock.lastCall?.[1].recorder).toBe('other')
    expect(document.body.textContent).not.toContain('192.0.2.99')
    expect(document.body.textContent).toContain('192.0.2.20')
  })
  it('explains an old Agent error and allows retry without a misleading empty state', async () => {
    vi.mocked(oxidnsMonitorApi.clients).mockRejectedValueOnce({ response: { data: { message: '请升级 Agent 到 1.7.1-go' } } })
    const wrapper = mountPicker()
    await open(wrapper)
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('1.7.1-go')
    expect(document.body.textContent).not.toContain('暂无客户端记录')
    const retry = [...document.querySelectorAll('button')].find(node => node.textContent?.trim() === '重试')!
    retry.click()
    await flushPromises()
    expect(document.body.textContent).toContain('192.0.2.10')
  })
  it('applies a client selection immediately and can clear it without typing', async () => {
    const wrapper = mount(OxiDNSQueryTable, { attachTo: document.body, props: {
      agentId: 'one', recorders: ['recorder'], recorder: 'recorder', filters: emptyQueryFilters(),
      data: null, loading: false, error: '', page: 1, hasNext: false
    } })
    wrappers.push(wrapper)
    const picker = wrapper.getComponent(OxiDNSClientSelect)
    await open(picker)
    await choose('192.0.2.10')
    expect(wrapper.emitted('filter')?.[0]?.[0]).toMatchObject({ client_ip: '192.0.2.10' })
    await open(picker)
    const all = [...document.querySelectorAll('button')].find(node => node.textContent?.trim() === '全部客户端')!
    all.click()
    await flushPromises()
    expect(wrapper.emitted('filter')?.at(-1)?.[0]).toMatchObject({ client_ip: '' })
  })
})

it('searches notes locally without passing a name to the core IP search', async () => {
  vi.useFakeTimers()
  const wrapper = mountPicker({ allowAll: false, notes: { '192.0.2.10': '客厅电视' } })
  await open(wrapper)
  expect(document.body.textContent).toContain('客厅电视')
  const input = new DOMWrapper(document.querySelector<HTMLInputElement>('input[aria-label="搜索已有客户端 IP"]')!)
  await input.setValue('电视'); await vi.advanceTimersByTimeAsync(300); await flushPromises()
  expect(vi.mocked(oxidnsMonitorApi.clients).mock.lastCall?.[1].search).toBeUndefined()
  const options = [...document.querySelectorAll('[role="option"]')]
  expect(options).toHaveLength(1); expect(options[0].textContent).toContain('192.0.2.10')
  await choose('192.0.2.10'); expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['192.0.2.10'])
})

describe('query table client names', () => {
  function table(notes?: Record<string, string>) {
    const row = (id: number, ip: string): OxiDNSQuery => ({
      id, client_ip: ip, created_at_ms: 1700000000000, elapsed_ms: 1,
      questions_json: [{ name: 'example.com.', qtype: 'A', qclass: 'IN' }],
      error: null, has_response: true, rcode: 'NOERROR', answer_count: 0, answers_json: [], resolution_source: 'local'
    })
    const wrapper = mount(OxiDNSQueryTable, { attachTo: document.body, props: {
      agentId: 'one', recorders: ['recorder'], recorder: 'recorder', filters: emptyQueryFilters(), notes,
      data: { success: true, recorder: 'recorder', next_cursor: null,
        records: [row(1, '192.0.2.10'), row(2, '192.0.2.1'), row(3, '127.0.0.1')] },
      loading: false, error: '', page: 1, hasNext: false
    } })
    wrappers.push(wrapper)
    return wrapper
  }
  it('shows names with the IP, keeps unnamed clients as IPs, and retains Agent checks', () => {
    const wrapper = table({ '192.0.2.10': '客厅电视', '127.0.0.1': '本机' })
    const clients = wrapper.findAll('tbody tr').map(row => row.findAll('td')[2])
    expect(clients[0].text()).toContain('客厅电视')
    expect(clients[0].text()).toContain('192.0.2.10')
    expect(clients[0].classes()).toContain('whitespace-normal')
    expect(clients[0].classes()).not.toContain('whitespace-nowrap')
    expect(clients[1].text()).toBe('192.0.2.1')
    expect(clients[2].text()).toContain('本机')
    expect(clients[2].text()).toContain('127.0.0.1')
    expect(clients[2].text()).toContain('Agent 检查')
  })
  it('renders IPs when no notes are available', () => {
    const wrapper = table()
    const clients = wrapper.findAll('tbody tr').map(row => row.findAll('td')[2].text())
    expect(clients[0]).toBe('192.0.2.10')
    expect(clients[1]).toBe('192.0.2.1')
  })
  it.each([null, undefined])('renders missing client addresses as a dash (%s)', async ip => {
    const wrapper = table({ '192.0.2.10': '客厅电视' })
    const data = wrapper.props('data')!
    // Core history may carry null or omit client_ip entirely.
    const record: Partial<OxiDNSQuery> = { ...data.records[0] }
    if (ip === undefined) delete record.client_ip
    else record.client_ip = ip as unknown as string
    await wrapper.setProps({ data: { ...data, records: [record as OxiDNSQuery] } })
    const client = wrapper.get('tbody tr').findAll('td')[2]
    expect(client.text()).toBe('—')
    expect(client.text()).not.toContain('客厅电视')
  })
  it('passes query names to the existing picker for display and local name search', async () => {
    vi.useFakeTimers()
    const wrapper = table({ '192.0.2.10': '客厅电视' })
    const picker = wrapper.getComponent(OxiDNSClientSelect)
    await open(picker)
    const input = new DOMWrapper(document.querySelector<HTMLInputElement>('input[aria-label="搜索已有客户端 IP"]')!)
    await input.setValue('电视')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    const options = [...document.querySelectorAll('[role="option"]')]
    expect(options).toHaveLength(1)
    expect(options[0].textContent).toContain('客厅电视')
    expect(options[0].textContent).toContain('192.0.2.10')
    expect(vi.mocked(oxidnsMonitorApi.clients).mock.lastCall?.[1].search).toBeUndefined()
    await choose('192.0.2.10')
    expect(wrapper.emitted('filter')?.[0]?.[0]).toMatchObject({ client_ip: '192.0.2.10' })
    expect(picker.get('[role="combobox"]').text()).toContain('客厅电视')
  })
})
