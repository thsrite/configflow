import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosHeaders, type AxiosResponse } from 'axios'
import ProxyGroups from '@/views/ProxyGroups.vue'
import MultiSelect from '@/components/common/MultiSelect.vue'
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
  nodeApi: { getAll: vi.fn() },
  proxyGroupApi: { getAll: vi.fn(), previewRegex: vi.fn(), update: vi.fn(), create: vi.fn() }
}))

let wrapper: VueWrapper | undefined
let groups: ProxyGroup[]
function response<T>(data: T): AxiosResponse<T> {
  return { data, status: 200, statusText: 'OK', headers: new AxiosHeaders(), config: { headers: new AxiosHeaders() } }
}
function group(overrides: Partial<ProxyGroup> = {}): ProxyGroup {
  return { id: 'alpha', name: 'Alpha', type: 'select', enabled: true, subscriptions: ['sub'], regex: '', ...overrides }
}
const cases = [
  { source: 'subscription', include: 'regex', exclude: 'exclude_regex', ids: 'subscriptions', input: 'group-exclude-regex', includeInput: 'group-regex', selection: ['sub'] },
  { source: 'aggregation', include: 'aggregation_regex', exclude: 'aggregation_exclude_regex', ids: 'aggregations', input: 'group-agg-exclude-regex', includeInput: 'group-agg-regex', selection: ['agg'] }
] as const

beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  // 平板布局：手机断点（639px）以外的查询都命中
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ matches: !String(query).includes('639px'), addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  setActiveProfileId('exclude-profile')
  groups = [group()]
  feedback.choose.mockResolvedValue('cancel')
  vi.mocked(proxyGroupApi.getAll).mockImplementation(async () => response(structuredClone(groups)))
  vi.mocked(proxyGroupApi.update).mockImplementation(async (id, data) => {
    groups = groups.map(item => item.id === id ? { ...item, ...data as ProxyGroup } : item)
    return response({ success: true })
  })
  vi.mocked(proxyGroupApi.create).mockResolvedValue(response({ success: true }))
  vi.mocked(proxyGroupApi.previewRegex).mockResolvedValue(response({
    success: true, nodes: [{ name: 'HK 01', type: 'vless' }], count: 1, total_candidates: 3
  }))
  vi.mocked(nodeApi.getAll).mockResolvedValue(response([]))
  vi.mocked(api.get).mockImplementation(async path => response(
    path === '/subscriptions' ? [{ id: 'sub', name: 'Subscription' }] :
    path === '/aggregations' ? [{ id: 'agg', name: 'Aggregation', enabled: true }] :
    path === '/proxy-groups/health-defaults' ? { group: {} } : { total_count: 3 }
  ))
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
  sessionStorage.clear()
})

async function render() {
  wrapper = mount(ProxyGroups, { attachTo: document.body })
  await flushPromises()
  return wrapper
}
async function debounce() {
  await vi.advanceTimersByTimeAsync(280)
  await flushPromises()
}
function exclusion() {
  return wrapper!.get<HTMLInputElement>('input[aria-label="排除正则"]')
}
function detailButton(text: string) {
  return wrapper!.findAll('.group-detail-card footer button').find(item => item.text() === text)!
}
function dialog() {
  return new DOMWrapper(document.querySelector<HTMLElement>('[role="dialog"]')!)
}
async function openEdit() {
  await wrapper!.get('button[title="编辑 Alpha"]').trigger('click')
  await flushPromises()
}
async function dialogButton(text: string) {
  await dialog().findAll('button').find(item => item.text() === text)!.trigger('click')
  await flushPromises()
}

describe('proxy group exclusion filters', () => {
  it.each(cases)('loads, previews, saves and restores an exclusion-only $source draft', async item => {
    groups = [group({ subscriptions: [], [item.ids]: [...item.selection], [item.exclude]: '广告' })]
    await render()
    expect(exclusion().element.value).toBe('广告')
    // Count requests use the same filters as the selected group's live preview.
    expect(proxyGroupApi.previewRegex).toHaveBeenCalledWith({
      source: item.source, regex: '.*', exclude_regex: '广告', [item.ids]: item.selection
    }, 'exclude-profile')
    await debounce()
    expect(wrapper!.find('.group-detail-card footer').exists()).toBe(false)
    expect(wrapper!.get('[aria-label="策略组详情"]').text()).toContain('HK 01')
    await exclusion().setValue('广告|到期')
    await debounce()
    expect(proxyGroupApi.previewRegex).toHaveBeenLastCalledWith(expect.objectContaining({ exclude_regex: '广告|到期' }), 'exclude-profile')
    await detailButton('保存').trigger('click')
    await flushPromises()
    expect(proxyGroupApi.update).toHaveBeenCalledWith('alpha', expect.objectContaining({
      [item.exclude]: '广告|到期', [item.include]: '', [item.ids]: item.selection
    }), 'exclude-profile')
    expect(wrapper!.find('.group-detail-card footer').exists()).toBe(false)
    await exclusion().setValue('临时排除')
    await detailButton('还原').trigger('click')
    expect(exclusion().element.value).toBe('广告|到期')
    expect(wrapper!.find('.group-detail-card footer').exists()).toBe(false)
  })

  it('protects an exclusion-only draft while switching groups', async () => {
    groups.push(group({ id: 'beta', name: 'Beta', exclude_regex: '过期' }))
    await render()
    await exclusion().setValue('广告')
    await wrapper!.get('[data-name="Beta"] button').trigger('click')
    await flushPromises()
    expect(feedback.choose).toHaveBeenCalledOnce()
    expect(exclusion().element.value).toBe('广告')
    feedback.choose.mockResolvedValue('confirm')
    await wrapper!.get('[data-name="Beta"] button').trigger('click')
    await flushPromises()
    expect(proxyGroupApi.update).toHaveBeenCalledWith('alpha', expect.objectContaining({ exclude_regex: '广告' }), 'exclude-profile')
    expect(exclusion().element.value).toBe('过期')
  })

  it('keeps backend regex errors visible and prevents saving an invalid exclusion', async () => {
    await render()
    await debounce()
    vi.mocked(proxyGroupApi.previewRegex).mockRejectedValue({ response: { data: { message: '排除正则表达式无效' } } })
    await exclusion().setValue('(')
    await debounce()
    expect(wrapper!.get('[aria-label="策略组详情"]').text()).toContain('排除正则表达式无效')
    expect(detailButton('保存').attributes('disabled')).toBeDefined()
    expect(proxyGroupApi.update).not.toHaveBeenCalled()
  })

  it.each(cases)('loads and saves $source exclusions from the edit dialog and previews without an include regex', async item => {
    groups = [group({ subscriptions: [], [item.ids]: [...item.selection], [item.exclude]: '广告' })]
    await render()
    await openEdit()
    expect(dialog().get<HTMLInputElement>(`#${item.input}`).element.value).toBe('广告')
    await dialog().get(`#${item.input}`).setValue('广告|到期')
    await dialogButton('预览')
    expect(proxyGroupApi.previewRegex).toHaveBeenLastCalledWith({
      source: item.source, regex: '.*', exclude_regex: '广告|到期', [item.ids]: item.selection
    }, 'exclude-profile')
    const previewDialog = new DOMWrapper(Array.from(document.querySelectorAll<HTMLElement>('[role="dialog"]')).find(el => el.textContent?.includes('匹配预览'))!)
    await previewDialog.findAll('button').find(button => button.text() === '关闭')!.trigger('click')
    await flushPromises()
    await dialog().get(`#${item.includeInput}`).setValue('HK|香港')
    await dialogButton('保存')
    expect(proxyGroupApi.update).toHaveBeenCalledWith('alpha', expect.objectContaining({
      [item.include]: 'HK|香港', [item.exclude]: '广告|到期'
    }), 'exclude-profile')
  })

  it.each(cases)('clears the $source exclusion when that source is disabled', async item => {
    groups = [group({ aggregations: ['agg'], regex: 'HK', exclude_regex: '订阅广告', aggregation_regex: 'JP', aggregation_exclude_regex: '聚合广告' })]
    await render()
    await openEdit()
    const label = item.source === 'subscription' ? '订阅' : '聚合'
    await dialog().findAll('label').find(el => el.text() === label)!.get('[role="checkbox"]').trigger('click')
    await flushPromises()
    expect(dialog().find(`#${item.input}`).exists()).toBe(false)
    await dialogButton('保存')
    expect(proxyGroupApi.update).toHaveBeenCalledWith('alpha', expect.objectContaining({
      [item.ids]: [], [item.include]: '', [item.exclude]: ''
    }), 'exclude-profile')
    const other = item.source === 'subscription' ? 'aggregation_exclude_regex' : 'exclude_regex'
    expect((vi.mocked(proxyGroupApi.update).mock.calls[0]![1] as ProxyGroup)[other]).toBe(item.source === 'subscription' ? '聚合广告' : '订阅广告')
  })

  it('creates a new group with an exclusion and no include filter', async () => {
    await render()
    await wrapper!.findAll('button').find(item => item.text() === '添加策略组')!.trigger('click')
    await flushPromises()
    await dialog().get('#group-name').setValue('Clean nodes')
    await dialog().findAll('label').find(el => el.text() === '订阅')!.get('[role="checkbox"]').trigger('click')
    wrapper!.findComponent(MultiSelect).vm.$emit('update:modelValue', ['sub'])
    await flushPromises()
    expect(dialog().get<HTMLInputElement>('#group-exclude-regex').element.value).toBe('')
    await dialog().get('#group-exclude-regex').setValue('广告|剩余流量|到期')
    await dialogButton('保存')
    expect(proxyGroupApi.create).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Clean nodes', subscriptions: ['sub'], regex: '', exclude_regex: '广告|剩余流量|到期'
    }), 'exclude-profile')
  })
})
