import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosHeaders, type AxiosResponse } from 'axios'
import BatchHealthUrlDialog from '@/components/BatchHealthUrlDialog.vue'
import ProxyGroups from '@/views/ProxyGroups.vue'
import { Checkbox } from '@/components/ui/checkbox'
import api, { nodeApi, proxyGroupApi } from '@/api'
import { setActiveProfileId } from '@/profileContext'
import type { ProxyGroup } from '@/types'

// Mount real controls, including the teleported dialog. Only network requests and
// the shared feedback service are replaced, so disabled controls and draft behavior
// are exercised through the same handlers as the page.
const feedback = vi.hoisted(() => ({
  choose: vi.fn(), confirmDanger: vi.fn(),
  notify: { error: vi.fn(), success: vi.fn(), warning: vi.fn(), info: vi.fn() }
}))
vi.mock('@/lib/feedback', () => feedback)
vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn() },
  nodeApi: { getAll: vi.fn(), latency: vi.fn() },
  proxyGroupApi: {
    getAll: vi.fn(), previewRegex: vi.fn(), update: vi.fn(), delete: vi.fn(), batchHealthUrl: vi.fn()
  }
}))

const defaultUrl = 'https://www.gstatic.com/generate_204'
const customUrl = 'https://latency.example.com/check'
const wrappers: VueWrapper[] = []
let groups: ProxyGroup[]
let scrollDescriptor: PropertyDescriptor | undefined

function response<T>(data: T): AxiosResponse<T> {
  return { data, status: 200, statusText: 'OK', headers: new AxiosHeaders(), config: { headers: new AxiosHeaders() } }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function group(name: string, overrides: Partial<ProxyGroup> = {}): ProxyGroup {
  return {
    id: name.toLowerCase(), name, type: 'url-test', enabled: true,
    url: 'http://www.gstatic.com/generate_204', interval: 300,
    subscriptions: ['sub-one'], regex: `${name} regex`, ...overrides
  }
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  // 平板布局：手机断点（639px）以外的查询都命中
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: !String(query).includes('639px'), addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  setActiveProfileId('batch-health-profile')
  groups = [
    group('Alpha'),
    group('Fallback', { type: 'fallback' }),
    group('Disabled balance', { type: 'load-balance', enabled: false }),
    group('Follower', { follow_group: 'alpha' }),
    group('Select', { type: 'select' }),
    group('Chain', { type: 'chain' })
  ]
  feedback.choose.mockResolvedValue('cancel')
  vi.mocked(proxyGroupApi.getAll).mockImplementation(async () => response(structuredClone(groups)))
  vi.mocked(proxyGroupApi.update).mockResolvedValue(response({ success: true }))
  vi.mocked(proxyGroupApi.batchHealthUrl).mockResolvedValue(response({ success: true }))
  vi.mocked(proxyGroupApi.previewRegex).mockResolvedValue(response({ success: true, nodes: [], count: 0, total_candidates: 3 }))
  vi.mocked(nodeApi.getAll).mockResolvedValue(response([]))
  vi.mocked(nodeApi.latency).mockResolvedValue(response({ results: {} }))
  vi.mocked(api.get).mockImplementation(async path => response(
    path === '/proxy-groups/health-defaults' ? { group: { url: defaultUrl } } :
    path === '/subscriptions' ? [{ id: 'sub-one', name: 'First source' }, { id: 'sub-two', name: 'Second source' }] :
    path === '/aggregations' ? [] : { total_count: 3 }
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

async function render(page = false) {
  const wrapper = page
    ? mount(ProxyGroups, { attachTo: document.body })
    : mount(BatchHealthUrlDialog, { attachTo: document.body, props: { groups, profileId: 'batch-health-profile' } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
async function openDialog(wrapper: VueWrapper) {
  const button = wrapper.findAll('button').find(item => item.text() === '批量测速地址')!
  expect(button).toBeTruthy()
  await button.trigger('click')
  await flushPromises()
}
function dialog() {
  const element = document.querySelector<HTMLElement>('[role="dialog"]')
  expect(element).not.toBeNull()
  return new DOMWrapper(element!)
}
function urlInput() {
  return dialog().get<HTMLInputElement>('#batch-health-url')
}
async function submit() {
  await dialog().get('form').trigger('submit')
  await flushPromises()
}
async function unselect(name: string) {
  await dialog().get(`[role="checkbox"][aria-label="选择 ${name}"]`).trigger('click')
}

describe('batch proxy group health URL', () => {
  it('opens from the page with HTTPS and lists only ordinary health-check groups, including disabled groups', async () => {
    const wrapper = await render(true)
    await openDialog(wrapper)
    expect(api.get).toHaveBeenCalledWith('/proxy-groups/health-defaults')
    expect(urlInput().element.value).toBe(defaultUrl)
    expect(dialog().findAll('[role="checkbox"][aria-label]').map(item => item.attributes('aria-label'))).toEqual([
      '选择 Alpha', '选择 Fallback', '选择 Disabled balance'
    ])
    expect(dialog().get('button[type="submit"]').text()).toBe('应用到 3 个策略组')
    expect(proxyGroupApi.batchHealthUrl).not.toHaveBeenCalled()
  })

  it('submits a chosen subset once using the profile captured by the page', async () => {
    const wrapper = await render(true)
    // Changing global context must not send an already mounted page to another profile.
    setActiveProfileId('another-profile')
    await openDialog(wrapper)
    await unselect('Fallback')
    await urlInput().setValue(`  ${customUrl}  `)
    await submit()
    expect(proxyGroupApi.batchHealthUrl).toHaveBeenCalledExactlyOnceWith({
      ids: ['alpha', 'disabled balance'], url: customUrl
    }, 'batch-health-profile')
    expect(proxyGroupApi.update).not.toHaveBeenCalled()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(feedback.notify.success).toHaveBeenCalledWith('已更新 2 个策略组的测速地址')
  })

  it('updates the URL without losing the current source and regex draft', async () => {
    const wrapper = await render(true)
    const regex = wrapper.get<HTMLInputElement>('input[aria-label="正则过滤"]')
    await regex.setValue('My unsaved regex')
    const sourceCheckboxes = wrapper.get('[aria-label="策略组详情"]').findAllComponents(Checkbox)
    expect(sourceCheckboxes).toHaveLength(2)
    sourceCheckboxes[1]!.vm.$emit('update:modelValue', true)
    await flushPromises()
    await openDialog(wrapper)
    await urlInput().setValue(customUrl)
    await submit()
    expect(regex.element.value).toBe('My unsaved regex')
    expect(sourceCheckboxes[1]!.props('modelValue')).toBe(true)
    expect(proxyGroupApi.getAll).toHaveBeenCalledTimes(1)
    expect(proxyGroupApi.update).not.toHaveBeenCalled()
    const save = wrapper.findAll('.group-detail-card footer button').find(button => button.text() === '保存')!
    expect(save).toBeTruthy()
    await save.trigger('click')
    await flushPromises()
    expect(proxyGroupApi.update).toHaveBeenCalledExactlyOnceWith('alpha', expect.objectContaining({
      url: customUrl, regex: 'My unsaved regex', subscriptions: ['sub-one', 'sub-two'], interval: 300
    }), 'batch-health-profile')
    expect(wrapper.find('.group-detail-card footer').exists()).toBe(false)
  })

  it('keeps a successful batch URL when an older group-list request finishes later', async () => {
    const wrapper = await render(true)
    const staleList = deferred<AxiosResponse>()
    const staleGroups = structuredClone(groups)
    vi.mocked(proxyGroupApi.getAll).mockReturnValueOnce(staleList.promise)
    // A refresh can already be in flight when the user starts the batch edit.
    const state = (wrapper.vm as any).$.setupState as { loadProxyGroups: () => Promise<void> }
    const refresh = state.loadProxyGroups()
    expect(proxyGroupApi.getAll).toHaveBeenCalledTimes(2)

    await wrapper.get<HTMLInputElement>('input[aria-label="正则过滤"]').setValue('Draft during refresh')
    await openDialog(wrapper)
    await urlInput().setValue(customUrl)
    await submit()
    expect(proxyGroupApi.batchHealthUrl).toHaveBeenCalledTimes(1)

    staleList.resolve(response(staleGroups))
    await refresh
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('input[aria-label="正则过滤"]').element.value).toBe('Draft during refresh')
    const save = wrapper.findAll('.group-detail-card footer button').find(button => button.text() === '保存')!
    expect(save).toBeTruthy()
    await save.trigger('click')
    await flushPromises()
    expect(proxyGroupApi.update).toHaveBeenCalledExactlyOnceWith('alpha', expect.objectContaining({
      url: customUrl, regex: 'Draft during refresh', subscriptions: ['sub-one']
    }), 'batch-health-profile')
  })

  it.each([
    'ftp://latency.example.com/check',
    'https://user:password@latency.example.com/check',
    'not a URL'
  ])('rejects invalid or credential-bearing URL %s without writing', async invalidUrl => {
    const wrapper = await render()
    await openDialog(wrapper)
    await urlInput().setValue(invalidUrl)
    await submit()
    expect(dialog().get('[role="alert"]').text()).toBe('请输入不含用户名或密码的 HTTP(S) 测速地址')
    expect(urlInput().element.value).toBe(invalidUrl)
    expect(proxyGroupApi.batchHealthUrl).not.toHaveBeenCalled()
    expect(wrapper.emitted('updated')).toBeUndefined()
  })

  it('retains the dialog, input and selection after a failed save, and permits retry', async () => {
    vi.mocked(proxyGroupApi.batchHealthUrl).mockRejectedValueOnce({ response: { data: { message: '保存失败，请重试' } } })
    const wrapper = await render()
    await openDialog(wrapper)
    await unselect('Fallback')
    await urlInput().setValue(customUrl)
    await submit()
    expect(dialog().get('[role="alert"]').text()).toBe('保存失败，请重试')
    expect(urlInput().element.value).toBe(customUrl)
    expect(dialog().get('[aria-label="选择 Fallback"]').attributes('aria-checked')).toBe('false')
    expect(dialog().get<HTMLButtonElement>('button[type="submit"]').element.disabled).toBe(false)
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(feedback.notify.success).not.toHaveBeenCalled()
    await submit()
    expect(proxyGroupApi.batchHealthUrl).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('updated')).toEqual([[{ ids: ['alpha', 'disabled balance'], url: customUrl }]])
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })

  it('blocks submission until defaults load and prevents duplicate saves while a request is pending', async () => {
    const defaults = deferred<AxiosResponse>()
    const saving = deferred<AxiosResponse>()
    vi.mocked(api.get).mockReturnValueOnce(defaults.promise)
    vi.mocked(proxyGroupApi.batchHealthUrl).mockReturnValueOnce(saving.promise)
    const wrapper = await render()
    await openDialog(wrapper)
    expect(urlInput().element.disabled).toBe(true)
    expect(dialog().get<HTMLButtonElement>('button[type="submit"]').element.disabled).toBe(true)
    await submit()
    expect(proxyGroupApi.batchHealthUrl).not.toHaveBeenCalled()
    defaults.resolve(response({ group: { url: defaultUrl } }))
    await flushPromises()
    expect(urlInput().element.disabled).toBe(false)
    await submit()
    await submit()
    expect(proxyGroupApi.batchHealthUrl).toHaveBeenCalledTimes(1)
    expect(urlInput().element.disabled).toBe(true)
    expect(dialog().get<HTMLButtonElement>('button[type="submit"]').element.disabled).toBe(true)
    expect(dialog().get<HTMLButtonElement>('[aria-label="选择 Alpha"]').element.disabled).toBe(true)
    const cancel = dialog().findAll('button').find(button => button.text() === '取消')!
    expect((cancel.element as HTMLButtonElement).disabled).toBe(true)
    saving.resolve(response({ success: true }))
    await flushPromises()
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })

  it('cancels without writing or emitting an update', async () => {
    const wrapper = await render()
    await openDialog(wrapper)
    await urlInput().setValue(customUrl)
    await unselect('Fallback')
    await dialog().findAll('button').find(button => button.text() === '取消')!.trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(proxyGroupApi.batchHealthUrl).not.toHaveBeenCalled()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(groups.every(item => item.url === 'http://www.gstatic.com/generate_204')).toBe(true)
  })
})
