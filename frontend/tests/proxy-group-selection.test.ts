import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosHeaders, type AxiosResponse } from 'axios'
import ProxyGroups from '@/views/ProxyGroups.vue'
import { Checkbox } from '@/components/ui/checkbox'
import api, { nodeApi, proxyGroupApi } from '@/api'
import { setActiveProfileId } from '@/profileContext'
import type { ProxyGroup } from '@/types'

// Only HTTP and the user's answer to the shared confirmation service are mocked.
// The actual group list, picker dialog, draft controls and save handlers are mounted.
const feedback = vi.hoisted(() => ({
  choose: vi.fn(), confirmDanger: vi.fn(),
  notify: { error: vi.fn(), success: vi.fn(), warning: vi.fn(), info: vi.fn() }
}))
vi.mock('@/lib/feedback', () => feedback)
vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn() },
  nodeApi: { getAll: vi.fn(), latency: vi.fn() },
  proxyGroupApi: { getAll: vi.fn(), previewRegex: vi.fn(), update: vi.fn(), delete: vi.fn() }
}))

const wrappers: VueWrapper[] = []
let groups: ProxyGroup[]
let scrollDescriptor: PropertyDescriptor | undefined

function response<T>(data: T): AxiosResponse<T> {
  return { data, status: 200, statusText: 'OK', headers: new AxiosHeaders(), config: { headers: new AxiosHeaders() } }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function group(name: string, overrides: Partial<ProxyGroup> = {}): ProxyGroup {
  return { id: name.toLowerCase(), name, type: 'url-test', enabled: true, subscriptions: ['sub-one'], regex: `${name} regex`, ...overrides }
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  setActiveProfileId('selection-profile')
  groups = [group('Alpha'), group('Beta'), group('Gamma')]
  feedback.choose.mockResolvedValue('cancel')
  feedback.confirmDanger.mockResolvedValue(true)
  vi.mocked(proxyGroupApi.getAll).mockImplementation(async () => response(structuredClone(groups)))
  vi.mocked(proxyGroupApi.update).mockResolvedValue(response({ success: true }))
  vi.mocked(proxyGroupApi.delete).mockImplementation(async id => {
    groups = groups.filter(item => item.id !== id)
    return response({ success: true })
  })
  vi.mocked(proxyGroupApi.previewRegex).mockResolvedValue(response({
    success: true, nodes: [{ name: 'HK 01', type: 'vless' }], count: 1, total_candidates: 3
  }))
  vi.mocked(nodeApi.getAll).mockResolvedValue(response([]))
  vi.mocked(nodeApi.latency).mockResolvedValue(response({ results: {} }))
  vi.mocked(api.get).mockImplementation(async path => response(
    path === '/subscriptions' ? [{ id: 'sub-one', name: 'First source' }, { id: 'sub-two', name: 'Second source' }] :
    path === '/aggregations' ? [{ id: 'agg-one', name: 'Aggregation', enabled: true }] : { total_count: 3 }
  ))
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
  document.body.innerHTML = ''
  sessionStorage.clear()
})

async function render() {
  const wrapper = mount(ProxyGroups, { attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
async function select(wrapper: VueWrapper, name: string) {
  await wrapper.get(`[data-name="${name}"] button`).trigger('click')
  await flushPromises()
}
function heading(wrapper: VueWrapper) {
  return wrapper.get('[aria-label="策略组详情"] h2').text()
}
function regex(wrapper: VueWrapper) {
  return wrapper.get<HTMLInputElement>('input[aria-label="正则过滤"]')
}
async function clickSave(wrapper: VueWrapper) {
  const button = wrapper.findAll('.group-detail-card footer button').find(item => item.text() === '保存')
  expect(button).toBeTruthy()
  await button!.trigger('click')
  await flushPromises()
}
async function edit(wrapper: VueWrapper, value = 'Unsaved draft') {
  await regex(wrapper).setValue(value)
  await flushPromises()
}
function unmount(wrapper: VueWrapper) {
  wrapper.unmount()
  wrappers.splice(wrappers.indexOf(wrapper), 1)
}

describe('proxy group selection', () => {
  it('keeps the current draft and scroll positions when the same group is selected', async () => {
    const wrapper = await render()
    await edit(wrapper)
    const details = wrapper.get<HTMLElement>('.group-detail-body').element
    details.scrollTop = 120
    await select(wrapper, 'Alpha')
    expect(heading(wrapper)).toBe('Alpha')
    expect(regex(wrapper).element.value).toBe('Unsaved draft')
    expect(details.scrollTop).toBe(120)
    expect(feedback.choose).not.toHaveBeenCalled()
    expect(proxyGroupApi.update).not.toHaveBeenCalled()
  })

  it('resets only the right detail scroll positions on a clean selection', async () => {
    const wrapper = await render()
    const list = wrapper.get<HTMLElement>('[aria-label="策略组"]').element
    const details = wrapper.get<HTMLElement>('.group-detail-body').element
    const matches = wrapper.get<HTMLElement>('.group-detail-body .overflow-auto').element
    list.scrollTop = 900
    details.scrollTop = 120
    matches.scrollTop = 70
    await select(wrapper, 'Gamma')
    expect(heading(wrapper)).toBe('Gamma')
    expect(regex(wrapper).element.value).toBe('Gamma regex')
    expect(list.scrollTop).toBe(900)
    expect(details.scrollTop).toBe(0)
    expect(matches.scrollTop).toBe(0)
    expect(window.scrollTo).not.toHaveBeenCalled()
    expect(Element.prototype.scrollIntoView).not.toHaveBeenCalled()
  })

  it('keeps the selected group and its draft when the user continues editing', async () => {
    const wrapper = await render()
    await edit(wrapper)
    await select(wrapper, 'Beta')
    expect(feedback.choose).toHaveBeenCalledWith(expect.stringContaining('Alpha'), expect.objectContaining({
      confirmText: '保存并切换', altText: '放弃并切换', cancelText: '继续编辑'
    }))
    expect(heading(wrapper)).toBe('Alpha')
    expect(regex(wrapper).element.value).toBe('Unsaved draft')
    expect(proxyGroupApi.update).not.toHaveBeenCalled()
  })

  it('discards only the unsaved draft when explicitly switching without saving', async () => {
    feedback.choose.mockResolvedValue('alt')
    const wrapper = await render()
    await edit(wrapper)
    await select(wrapper, 'Beta')
    expect(heading(wrapper)).toBe('Beta')
    expect(proxyGroupApi.update).not.toHaveBeenCalled()
    await select(wrapper, 'Alpha')
    expect(regex(wrapper).element.value).toBe('Alpha regex')
    expect(feedback.choose).toHaveBeenCalledTimes(1)
  })

  it('waits for one confirmation and one save, then switches to the original target', async () => {
    const choice = deferred<'confirm'>()
    const save = deferred<AxiosResponse>()
    feedback.choose.mockReturnValue(choice.promise)
    vi.mocked(proxyGroupApi.update).mockReturnValue(save.promise)
    const wrapper = await render()
    await edit(wrapper)
    await select(wrapper, 'Beta')
    await select(wrapper, 'Gamma')
    await clickSave(wrapper)
    expect(feedback.choose).toHaveBeenCalledTimes(1)
    expect(proxyGroupApi.update).not.toHaveBeenCalled()
    choice.resolve('confirm')
    await flushPromises()
    expect(heading(wrapper)).toBe('Alpha')
    expect(regex(wrapper).element.disabled).toBe(true)
    await select(wrapper, 'Gamma')
    await clickSave(wrapper)
    expect(proxyGroupApi.update).toHaveBeenCalledTimes(1)
    expect(proxyGroupApi.update).toHaveBeenCalledWith('alpha', expect.objectContaining({ regex: 'Unsaved draft', subscriptions: ['sub-one'] }), 'selection-profile')
    save.resolve(response({ success: true }))
    await flushPromises()
    expect(heading(wrapper)).toBe('Beta')
    await select(wrapper, 'Alpha')
    expect(regex(wrapper).element.value).toBe('Unsaved draft')
    expect(wrapper.find('.group-detail-card footer').exists()).toBe(false)
  })

  it('keeps a failed save and its draft on the current group', async () => {
    feedback.choose.mockResolvedValue('confirm')
    vi.mocked(proxyGroupApi.update).mockRejectedValue({ response: { data: { message: '无法保存策略组' } } })
    const wrapper = await render()
    await edit(wrapper)
    await select(wrapper, 'Beta')
    expect(heading(wrapper)).toBe('Alpha')
    expect(regex(wrapper).element.value).toBe('Unsaved draft')
    expect(regex(wrapper).element.disabled).toBe(false)
    expect(wrapper.find('.group-detail-card footer').exists()).toBe(true)
    expect(feedback.notify.error).toHaveBeenCalledWith('无法保存策略组')
  })

  it('does not change the requested payload or discard edits delivered during a save', async () => {
    feedback.choose.mockResolvedValue('confirm')
    const save = deferred<AxiosResponse>()
    vi.mocked(proxyGroupApi.update).mockReturnValue(save.promise)
    const wrapper = await render()
    await edit(wrapper, 'Submitted draft')
    await select(wrapper, 'Beta')
    const payload = vi.mocked(proxyGroupApi.update).mock.calls[0]![1] as ProxyGroup
    // Simulate a late model event already queued before the saving controls disabled.
    regex(wrapper).element.value = 'Later draft'
    regex(wrapper).element.dispatchEvent(new Event('input', { bubbles: true }))
    wrapper.findAllComponents(Checkbox)[1]!.vm.$emit('update:modelValue', true)
    await flushPromises()
    expect(payload).toMatchObject({ regex: 'Submitted draft', subscriptions: ['sub-one'] })
    save.resolve(response({ success: true }))
    await flushPromises()
    expect(heading(wrapper)).toBe('Alpha')
    expect(regex(wrapper).element.value).toBe('Later draft')
    expect(wrapper.find('.group-detail-card footer').exists()).toBe(true)
    expect(feedback.notify.warning).toHaveBeenCalledWith(expect.stringContaining('又有改动'))
  })

  it('blocks switching while a direct draft save is already running', async () => {
    const save = deferred<AxiosResponse>()
    vi.mocked(proxyGroupApi.update).mockReturnValue(save.promise)
    const wrapper = await render()
    await edit(wrapper)
    await clickSave(wrapper)
    await select(wrapper, 'Beta')
    await clickSave(wrapper)
    expect(proxyGroupApi.update).toHaveBeenCalledTimes(1)
    expect(feedback.choose).not.toHaveBeenCalled()
    expect(heading(wrapper)).toBe('Alpha')
    save.resolve(response({ success: true }))
    await flushPromises()
    await select(wrapper, 'Beta')
    expect(heading(wrapper)).toBe('Beta')
  })

  it('ignores a confirmation answer that arrives after leaving the page', async () => {
    const choice = deferred<'confirm'>()
    feedback.choose.mockReturnValue(choice.promise)
    const old = await render()
    await edit(old)
    await select(old, 'Beta')
    unmount(old)
    const current = await render()
    choice.resolve('confirm')
    await flushPromises()
    expect(proxyGroupApi.update).not.toHaveBeenCalled()
    expect(heading(current)).toBe('Alpha')
    expect(regex(current).element.value).toBe('Alpha regex')
  })

  it('does not report success or change a new page after an old save completes', async () => {
    feedback.choose.mockResolvedValue('confirm')
    const save = deferred<AxiosResponse>()
    vi.mocked(proxyGroupApi.update).mockReturnValue(save.promise)
    const old = await render()
    await edit(old)
    await select(old, 'Beta')
    unmount(old)
    const current = await render()
    save.resolve(response({ success: true }))
    await flushPromises()
    expect(heading(current)).toBe('Alpha')
    expect(regex(current).element.value).toBe('Alpha regex')
    expect(feedback.notify.success).not.toHaveBeenCalled()
  })

  it('falls back after deleting the selected group without a stale-draft prompt', async () => {
    const wrapper = await render()
    await edit(wrapper)
    await wrapper.get('button[aria-label="删除 Alpha"]').trigger('click')
    await flushPromises()
    expect(heading(wrapper)).toBe('Beta')
    expect(regex(wrapper).element.value).toBe('Beta regex')
    expect(feedback.confirmDanger).toHaveBeenCalledOnce()
    expect(feedback.choose).not.toHaveBeenCalled()
  })

  it.each(['follow', 'no-sources'])('ignores the old preview when switching to a %s group', async kind => {
    groups[1] = group('Beta', kind === 'follow' ? { follow_group: 'alpha' } : { subscriptions: [] })
    vi.mocked(nodeApi.latency).mockReturnValue(new Promise(() => undefined))
    const oldPreview = deferred<AxiosResponse>()
    vi.mocked(proxyGroupApi.previewRegex).mockReturnValue(oldPreview.promise)
    const wrapper = await render()
    await vi.advanceTimersByTimeAsync(280)
    await flushPromises()
    expect(proxyGroupApi.previewRegex).toHaveBeenCalledTimes(1)
    await select(wrapper, 'Beta')
    expect(heading(wrapper)).toBe('Beta')
    oldPreview.resolve(response({ success: true, nodes: [{ name: 'Stale Alpha node' }], count: 1, total_candidates: 100 }))
    await flushPromises()
    const details = wrapper.get('[aria-label="策略组详情"]')
    expect(details.text()).not.toContain('Stale Alpha node')
    expect(details.findAll('.matched-node')).toHaveLength(0)
    expect(details.text()).toContain(kind === 'follow' ? '跟随策略组不单独筛选节点' : '先选择至少一个来源')
  })

  it('still preserves the last valid result for an invalid regex in the same group', async () => {
    vi.mocked(nodeApi.latency).mockReturnValue(new Promise(() => undefined))
    const wrapper = await render()
    await vi.advanceTimersByTimeAsync(280)
    await flushPromises()
    expect(wrapper.get('[aria-label="策略组详情"]').text()).toContain('HK 01')
    vi.mocked(proxyGroupApi.previewRegex).mockRejectedValue({ response: { data: { message: '正则表达式无效' } } })
    await edit(wrapper, '(')
    await vi.advanceTimersByTimeAsync(280)
    await flushPromises()
    const details = wrapper.get('[aria-label="策略组详情"]')
    expect(details.text()).toContain('HK 01')
    expect(details.text()).toContain('正则表达式无效')
    expect(heading(wrapper)).toBe('Alpha')
  })

  it('searches the compact picker, shows an empty state and closes after selection', async () => {
    const wrapper = await render()
    await wrapper.get('button[aria-haspopup="dialog"]').trigger('click')
    await flushPromises()
    const search = document.querySelector<HTMLInputElement>('input[aria-label="搜索策略组名称"]')!
    search.value = 'no matching group'
    search.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('没有匹配的策略组')
    search.value = ' gAmMa '
    search.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    const results = document.querySelector('[aria-label="搜索结果"]')!
    expect(results.querySelectorAll('button')).toHaveLength(1)
    expect(results.textContent).toContain('Gamma')
    results.querySelector('button')!.click()
    await flushPromises()
    expect(heading(wrapper)).toBe('Gamma')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(wrapper.get('button[aria-haspopup="dialog"]').attributes('aria-expanded')).toBe('false')
  })
})
