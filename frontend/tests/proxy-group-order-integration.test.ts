import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosHeaders, type AxiosResponse } from 'axios'
import ProxyGroups from '@/views/ProxyGroups.vue'
import api, { nodeApi, proxyGroupApi } from '@/api'
import { setActiveProfileId } from '@/profileContext'
import type { ProxyGroup } from '@/types'

const feedback = vi.hoisted(() => ({
  choose: vi.fn(), confirmDanger: vi.fn(),
  notify: { error: vi.fn(), success: vi.fn(), warning: vi.fn(), info: vi.fn() }
}))
vi.mock('@/lib/feedback', () => feedback)
vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn() },
  nodeApi: { getAll: vi.fn(), latency: vi.fn() },
  proxyGroupApi: { getAll: vi.fn(), previewRegex: vi.fn(), update: vi.fn() }
}))

let wrapper: VueWrapper | undefined
const groups: ProxyGroup[] = ['Alpha', 'Beta', 'Gamma'].map(name => ({
  id: name.toLowerCase(), name, type: 'select', enabled: true,
  subscriptions: ['sub'], regex: `${name} regex`
}))
function response<T>(data: T): AxiosResponse<T> {
  return { data, status: 200, statusText: 'OK', headers: new AxiosHeaders(), config: { headers: new AxiosHeaders() } }
}
function button(container: DOMWrapper<Element> | VueWrapper, text: string) {
  const found = container.findAll('button').find(item => item.text() === text)
  expect(found, `button ${text}`).toBeTruthy()
  return found!
}
function dialog() {
  const element = document.querySelector<HTMLElement>('[role="dialog"]')
  expect(element).not.toBeNull()
  return new DOMWrapper(element!)
}
function mainOrder() {
  return wrapper!.findAll('.group-list [data-name]').map(row => row.attributes('data-name'))
}
async function renderAndOpen() {
  wrapper = mount(ProxyGroups, { attachTo: document.body })
  await flushPromises()
  await button(wrapper, '调整顺序').trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.resetAllMocks()
  // 平板布局：手机断点（639px）以外的查询都命中
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: !String(query).includes('639px'), addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  setActiveProfileId('ordering-profile')
  vi.mocked(proxyGroupApi.getAll).mockResolvedValue(response(structuredClone(groups)))
  vi.mocked(proxyGroupApi.previewRegex).mockResolvedValue(response({ success: true, nodes: [], count: 0, total_candidates: 3 }))
  vi.mocked(nodeApi.getAll).mockResolvedValue(response([]))
  vi.mocked(api.get).mockImplementation(async path => response(path === '/subscriptions'
    ? [{ id: 'sub', name: 'Source', cached_node_count: 3 }] : path === '/aggregations' ? [] : {}))
  vi.mocked(api.post).mockResolvedValue(response({ success: true }))
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
  sessionStorage.clear()
})

describe('strategy group order dialog integration', () => {
  it('submits IDs in the captured profile and preserves the selected group and its unsaved filter', async () => {
    wrapper = mount(ProxyGroups, { attachTo: document.body })
    await flushPromises()
    await wrapper.get('input[aria-label="正则过滤"]').setValue('unsaved filter')
    setActiveProfileId('another-profile')
    await button(wrapper, '调整顺序').trigger('click')
    await flushPromises()
    await dialog().get('button[aria-label="上移 Beta"]').trigger('click')
    await flushPromises()
    expect(mainOrder()).toEqual(['Alpha', 'Beta', 'Gamma'])

    await button(dialog(), '保存顺序').trigger('click')
    await flushPromises()

    expect(api.post).toHaveBeenCalledExactlyOnceWith('/proxy-groups/reorder', {
      ids: ['beta', 'alpha', 'gamma'], position: 'top'
    }, { headers: { 'X-ConfigFlow-Profile': 'ordering-profile' } })
    expect(mainOrder()).toEqual(['Beta', 'Alpha', 'Gamma'])
    expect(wrapper.get('[aria-label="策略组详情"] h2').text()).toBe('Alpha')
    expect(wrapper.get<HTMLInputElement>('input[aria-label="正则过滤"]').element.value).toBe('unsaved filter')
    expect(proxyGroupApi.update).not.toHaveBeenCalled()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })

  it('discards the modal draft without changing the main list or calling the server', async () => {
    await renderAndOpen()
    await dialog().get('button[aria-label="上移 Gamma"]').trigger('click')
    await button(dialog(), '取消').trigger('click')
    await flushPromises()
    expect(mainOrder()).toEqual(['Alpha', 'Beta', 'Gamma'])
    expect(api.post).not.toHaveBeenCalled()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
})
