import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { agentApi } from '@/api'
import MihomoIPv6Verification from '@/components/generate/MihomoIPv6Verification.vue'
import MihomoIPv6VerificationResult from '@/components/generate/MihomoIPv6VerificationResult.vue'
import MihomoIPv6CheckSummary from '@/components/generate/MihomoIPv6CheckSummary.vue'

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))
const response = (data: any) => ({ data } as any)
const report = (extra = {}) => ({ success: true, agent_id: 'gw', profile_id: 'bound', verification: {
  status: 'running', scope: 'mihomo_runtime', config_revision: 7, deployment_id: 'current', checks: [], ...extra,
} })
const wrappers: VueWrapper[] = []
async function render(extra = {}) {
  const wrapper = shallowMount(MihomoIPv6Verification, { props: {
    open: true, agentId: 'gw', profileId: 'bound', configRevision: 7, ...extra,
  }, global: { stubs: { Button: false, Primitive: false } } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.spyOn(agentApi, 'verifyIPv6').mockResolvedValue(response(report()))
  vi.spyOn(agentApi, 'getIPv6Verification').mockResolvedValue(response({ success: true, agent_id: 'gw', profile_id: 'bound', verification: null }))
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks(); vi.useRealTimers() })

describe('Mihomo verification lifecycle', () => {
  it('as a group of the step list, puts the pass count and the verify button beside checklist rows', async () => {
    const readiness = { config_revision: 7, checks: [], routers: [], connectivity: { status: 'not_checked', message: '' } }
    const wrapper = shallowMount(MihomoIPv6Verification, { props: { open: true, agentId: 'gw', profileId: 'bound', configRevision: 7, readiness, group: '验收' },
      global: { stubs: { Button: false, Primitive: false, MihomoIPv6StepGroup: false } } })
    wrappers.push(wrapper)
    await flushPromises()
    expect(wrapper.get('h4').text()).toBe('验收')
    expect(wrapper.get('[data-ipv6-group-progress]').text()).toMatch(/^0 \/ \d 通过$/)
    expect(wrapper.getComponent(MihomoIPv6CheckSummary).props('checklist')).toBe(true)
    const button = wrapper.get('button[aria-label="验证连接与分流"]')
    expect(button.text()).toBe('验证')
    await button.trigger('click')
    await flushPromises()
    expect(agentApi.verifyIPv6).toHaveBeenCalledWith('gw', expect.objectContaining({ profile_id: 'bound', expected_config_revision: 7 }))
  })

  it('reports only settled results to an embedding step list', async () => {
    const wrapper = await render()
    expect(wrapper.emitted('result')).toEqual([[{ verification: null, fresh: false }]])
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('result')?.at(-1)).toEqual([{ verification: expect.objectContaining({ status: 'running' }), fresh: true }])
    vi.mocked(agentApi.getIPv6Verification).mockResolvedValue(response(report({ status: 'ok' })))
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.emitted('result')?.at(-1)).toEqual([{ verification: expect.objectContaining({ status: 'ok' }), fresh: true }])
    expect(wrapper.emitted('result')?.every(([value]: any) => value.verification === null || value.verification.status)).toBe(true)
  })

  it('reads saved results without starting a verification until the user asks', async () => {
    const wrapper = await render()
    expect(agentApi.getIPv6Verification).toHaveBeenCalledExactlyOnceWith('gw', 'bound', undefined)
    expect(agentApi.verifyIPv6).not.toHaveBeenCalled()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(agentApi.verifyIPv6).toHaveBeenCalledExactlyOnceWith('gw', { profile_id: 'bound', expected_config_revision: 7 })
    vi.mocked(agentApi.getIPv6Verification).mockResolvedValue(response(report({ status: 'warning', checks: [
      { id: 'client', label: '客户端全链路', status: 'unknown', message: '尚未配置独立检测客户端。' },
    ] })))
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.getComponent(MihomoIPv6VerificationResult).props('verification').status).toBe('warning')
    const count = vi.mocked(agentApi.getIPv6Verification).mock.calls.length
    await vi.advanceTimersByTimeAsync(4000)
    expect(agentApi.getIPv6Verification).toHaveBeenCalledTimes(count)
  })

  it('ensures automatic verification for the exact successful publication and polls its saved result', async () => {
    const wrapper = await render({ auto: true, deploymentId: 'current' })
    expect(agentApi.verifyIPv6).toHaveBeenCalledExactlyOnceWith('gw', { profile_id: 'bound', expected_config_revision: 7, deployment_id: 'current' })
    vi.mocked(agentApi.getIPv6Verification).mockResolvedValue(response(report()))
    await vi.advanceTimersByTimeAsync(2000)
    expect(agentApi.getIPv6Verification).toHaveBeenCalledWith('gw', 'bound', 'current')
    expect(wrapper.text()).toContain('可以离开此窗口')
    await wrapper.setProps({ open: false })
    const count = vi.mocked(agentApi.getIPv6Verification).mock.calls.length
    await vi.advanceTimersByTimeAsync(10000)
    expect(agentApi.getIPv6Verification).toHaveBeenCalledTimes(count)
  })

  it.each([{ config_revision: 8 }, { deployment_id: 'old' }])('rejects saved evidence from another revision or deployment (%j)', async changes => {
    vi.mocked(agentApi.verifyIPv6).mockResolvedValue(response(report({ ...changes, status: 'ok' })))
    const wrapper = await render({ auto: true, deploymentId: 'current' })
    expect(wrapper.get('[role="alert"]').text()).toContain('其他配置版本')
    expect(wrapper.findComponent(MihomoIPv6VerificationResult).exists()).toBe(false)
  })

  it('ignores delayed verification results after the target changes', async () => {
    let resolve!: (value: any) => void
    vi.mocked(agentApi.verifyIPv6).mockReturnValueOnce(new Promise(done => { resolve = done }))
    const wrapper = await render({ auto: true, deploymentId: 'current' })
    await wrapper.setProps({ open: false })
    resolve(response(report({ status: 'ok', message: 'old target result' })))
    await flushPromises()
    expect(wrapper.findComponent(MihomoIPv6VerificationResult).exists()).toBe(false)
    expect(wrapper.text()).not.toContain('old target result')
  })

  it('uses the new verification snapshot after a run, while historical results use the latest inspection', async () => {
    vi.mocked(agentApi.getIPv6Verification).mockResolvedValue(response(report({ status: 'ok' })))
    const wrapper = await render({ readiness: { config_revision: 7 } })
    expect(wrapper.getComponent(MihomoIPv6CheckSummary).props('preferVerificationSnapshot')).toBe(false)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(MihomoIPv6CheckSummary).props('preferVerificationSnapshot')).toBe(true)
    vi.mocked(agentApi.getIPv6Verification).mockResolvedValue(response(report({ status: 'error', checks: [
      { id: 'host_default_route', label: '默认路由', status: 'error', message: '本次验收发现路由已消失。' },
    ] })))
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.getComponent(MihomoIPv6CheckSummary).props('verification').checks[0].status).toBe('error')
    await wrapper.setProps({ configRevision: 8 })
    expect(wrapper.getComponent(MihomoIPv6CheckSummary).props('preferVerificationSnapshot')).toBe(false)
  })

  it('hands a saved result of an older draft to the summary as stale instead of failing', async () => {
    vi.mocked(agentApi.getIPv6Verification).mockResolvedValue(response(report({ status: 'ok', config_revision: 6 })))
    const wrapper = await render({ readiness: { config_revision: 7 } })
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.getComponent(MihomoIPv6CheckSummary).props('verification').config_revision).toBe(6)
    // Without the current readiness there is nothing to compare against: still an error.
    const plain = await render()
    expect(plain.get('[role="alert"]').text()).toContain('其他配置版本')
  })

  it('stops prolonged UI polling without claiming the backend task was canceled or passed', async () => {
    vi.mocked(agentApi.getIPv6Verification).mockResolvedValue(response(report()))
    const wrapper = await render({ auto: true, deploymentId: 'current' })
    await vi.advanceTimersByTimeAsync(152000)
    await flushPromises()
    expect(wrapper.text()).toContain('验收仍在后台执行')
    expect(wrapper.findComponent(MihomoIPv6VerificationResult).exists()).toBe(false)
    const count = vi.mocked(agentApi.getIPv6Verification).mock.calls.length
    await vi.advanceTimersByTimeAsync(10000)
    expect(agentApi.getIPv6Verification).toHaveBeenCalledTimes(count)
  })
})


it('offers the same verification action when an automatic result needs more evidence', async () => {
  vi.mocked(agentApi.verifyIPv6).mockResolvedValue(response(report({ status: 'warning', checks: [
    { id: 'runtime_node_ipv6_transport', label: '节点 IPv6', status: 'unknown', message: '未取得 IPv6 连接证据。' },
  ] })))
  const wrapper = await render({ auto: true, deploymentId: 'current' })
  const retry = wrapper.findAll('button').find(button => button.text() === '验证连接与分流')!
  expect(retry).toBeDefined()
  expect(retry.attributes('disabled')).toBeUndefined()
  await retry.trigger('click')
  await flushPromises()
  expect(agentApi.verifyIPv6).toHaveBeenCalledTimes(2)
})

// 「关闭」: the server answers that there is nothing to verify, which is neither a failure nor a pass.
const notApplicable = () => response({ success: true, agent_id: 'gw', profile_id: 'bound', verification: null,
  applicable: false, message: '未启用 IPv6，无需验收' })

it.each([
  { auto: true, deploymentId: 'current' },
  { readiness: { config_revision: 7, checks: [], routers: [], connectivity: { status: 'not_checked', message: '' } } },
])('says IPv6 is off neutrally, without results, a verify button or polling (%j)', async props => {
  vi.mocked(agentApi.verifyIPv6).mockResolvedValue(notApplicable())
  vi.mocked(agentApi.getIPv6Verification).mockResolvedValue(notApplicable())
  const wrapper = await render(props)
  const note = wrapper.get('[data-ipv6-not-applicable]')
  expect(note.text()).toBe('未启用 IPv6，无需验收。')
  expect(note.classes()).toContain('text-muted-foreground')
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  expect(wrapper.findComponent(MihomoIPv6VerificationResult).exists()).toBe(false)
  expect(wrapper.findComponent(MihomoIPv6CheckSummary).exists()).toBe(false)
  expect(wrapper.findAll('button').some(button => button.text() === '验证连接与分流')).toBe(false)
  expect(wrapper.text()).not.toMatch(/正在验证|可以离开此窗口|未通过|待验证/)
  const reads = vi.mocked(agentApi.getIPv6Verification).mock.calls.length
  await vi.advanceTimersByTimeAsync(10000)
  expect(agentApi.getIPv6Verification).toHaveBeenCalledTimes(reads)
  expect(wrapper.emitted('result')?.at(-1)).toEqual([{ verification: null, fresh: props.auto === true }])
})
