import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { agentApi, profileApi, type PublishGuard } from '@/api'
import { notify } from '@/lib/feedback'
import { Select } from '@/components/ui/select'
import SettingsPublishBar from '@/components/generate/SettingsPublishBar.vue'
import { ruleLocationDetails } from './fixtures/agent-failure'

vi.mock('@/api', () => ({
  agentApi: { getAll: vi.fn(), summaries: vi.fn(), pushConfig: vi.fn(), getDeployment: vi.fn(), activateDeployment: vi.fn(), verifyIPv6: vi.fn(), getIPv6Verification: vi.fn() },
  profileApi: { list: vi.fn() },
}))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() } }))
const wrappers: VueWrapper[] = []
const response = (data: any) => ({ data } as any)
const guard = (extra: Partial<PublishGuard> = {}): PublishGuard => ({ expected_profile_id: 'office', expected_service_type: 'oxidns', expected_config_revision: 7, ...extra })
const agent = (extra: Record<string, any> = {}) => ({ id: 'one', name: '客厅 DNS', profile_id: 'office', service_type: 'oxidns', status: 'online', enabled: true, ...extra })
const button = (wrapper: VueWrapper, text: string) => wrapper.findAll('button').find(item => item.text() === text)!
const publishButton = (wrapper: VueWrapper) => wrapper.findAll('button').find(item => /^(保存并发布|发布中…|保存中…)$/.test(item.text()))!
const deferred = <T,>() => {
  let resolve!: (value: T) => void
  let reject!: (value: any) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
async function render(props: Record<string, any> = {}, router?: ReturnType<typeof createRouter>) {
  const wrapper = mount(SettingsPublishBar, {
    props: { open: true, profileId: 'office', serviceType: 'oxidns', save: vi.fn().mockResolvedValue(guard()), ...props },
    attachTo: document.body,
    global: { plugins: router ? [router] : [] },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
async function publish(wrapper: VueWrapper) { await publishButton(wrapper).trigger('click'); await flushPromises() }
async function choose(wrapper: VueWrapper, id: string) {
  wrapper.getComponent(Select).vm.$emit('update:modelValue', id)
  await flushPromises()
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.resetAllMocks()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })))
  vi.mocked(agentApi.summaries).mockResolvedValue(response([agent()]))
  vi.mocked(profileApi.list).mockResolvedValue(response([{ id: 'office', name: '办公室', is_default: true }, { id: 'home', name: '家里' }]))
  vi.mocked(agentApi.pushConfig).mockImplementation(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'succeeded' }))
  vi.mocked(agentApi.getDeployment).mockImplementation(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'uploading' }))
  vi.mocked(agentApi.activateDeployment).mockImplementation(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'succeeded' }))
  vi.mocked(agentApi.verifyIPv6).mockImplementation(async (id, input) => response({ success: true, agent_id: id, profile_id: input.profile_id,
    verification: { status: 'warning', scope: 'mihomo_runtime', deployment_id: input.deployment_id, config_revision: input.expected_config_revision,
      checks: [{ id: 'client', label: '客户端全链路', status: 'unknown', message: '尚未配置独立检测客户端。' }] } }))
  vi.mocked(agentApi.getIPv6Verification).mockResolvedValue(response({ success: true, agent_id: 'one', profile_id: 'office', verification: null }))
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('settings save and publish', () => {
  it('announces only the exact saved Mihomo deployment after confirmed success', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ service_type: 'mihomo' })]))
    const accepted = deferred<any>()
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(() => accepted.promise)
    const wrapper = await render({ serviceType: 'mihomo', save: vi.fn().mockResolvedValue(guard({ expected_service_type: 'mihomo' })) })
    await publish(wrapper)
    const id = vi.mocked(agentApi.pushConfig).mock.lastCall![1]
    expect(wrapper.emitted('published')).toBeUndefined()
    const completed = { deployment_id: id, profile_id: 'office', service_type: 'mihomo', config_revision: 7, status: 'succeeded' }
    vi.mocked(agentApi.getDeployment).mockResolvedValue(response(completed))
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.emitted('published')).toEqual([[{ agent_id: 'one', profile_id: 'office', deployment_id: id, config_revision: 7 }]])
    accepted.resolve(response(completed))
    await flushPromises()
    expect(wrapper.emitted('published')).toHaveLength(1)
  })

  it.each([
    { status: 'ready' }, { status: 'failed' }, { pending: true }, { not_recorded: true }, { request_pending: true },
    { manual_rollback: true }, { profile_id: 'home' }, { service_type: 'oxidns' },
    { config_revision: 8 }, { deployment_id: 'another-deployment' },
  ])('does not announce an unconfirmed or mismatched Mihomo publication (%j)', async change => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ service_type: 'mihomo' })]))
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_id, deploymentId) => response({
      deployment_id: deploymentId, profile_id: 'office', service_type: 'mihomo', config_revision: 7,
      status: 'succeeded', ...change,
    }))
    const wrapper = await render({ serviceType: 'mihomo', save: vi.fn().mockResolvedValue(guard({ expected_service_type: 'mihomo' })) })
    await publish(wrapper)
    expect(wrapper.emitted('published')).toBeUndefined()
  })

  // The server enrolls a Mihomo publication for IPv6 verification from its own snapshot
  // (ipv6: true); the flag is what the bar follows, never a draft saved afterwards.
  async function publishMihomo(extra: Record<string, any>) {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ service_type: 'mihomo' })]))
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_id, deploymentId) => response({
      deployment_id: deploymentId, profile_id: 'office', service_type: 'mihomo', config_revision: 7, status: 'succeeded', ...extra,
    }))
    const wrapper = await render({ serviceType: 'mihomo', save: vi.fn().mockResolvedValue(guard({ expected_service_type: 'mihomo' })) })
    await publish(wrapper)
    return wrapper
  }

  it('verifies a publication whose own snapshot turns IPv6 on', async () => {
    const wrapper = await publishMihomo({ ipv6_verification_requested: true })
    const id = vi.mocked(agentApi.pushConfig).mock.lastCall![1]
    expect(wrapper.find('[data-ipv6-verifier]').exists()).toBe(true)
    expect(agentApi.verifyIPv6).toHaveBeenCalledExactlyOnceWith('one', { profile_id: 'office', expected_config_revision: 7, deployment_id: id })
    expect(wrapper.text()).toContain('IPv6 网络')
    expect(wrapper.text()).not.toContain('IPv6 节点连接')
  })

  it('shows no IPv6 verification, button or device-check hint after publishing with IPv6 off', async () => {
    const wrapper = await publishMihomo({})
    expect(wrapper.emitted('published')).toHaveLength(1)
    expect(wrapper.find('[data-ipv6-verifier]').exists()).toBe(false)
    expect(agentApi.verifyIPv6).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('发布成功')
    expect(wrapper.text()).not.toMatch(/IPv6 网络|客户端分流|验证连接与分流|检查设备|验收/)
    // A later draft (for example switching back to an IPv6 mode) changes nothing about this publication.
    await button(wrapper, '保存草稿').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-ipv6-verifier]').exists()).toBe(false)
    expect(agentApi.verifyIPv6).not.toHaveBeenCalled()
  })

  it('keeps the device-check hint for an IPv6 publication it cannot verify here', async () => {
    const wrapper = await publishMihomo({ ipv6_verification_requested: true, config_revision: 8 })
    expect(wrapper.find('[data-ipv6-verifier]').exists()).toBe(false)
    expect(wrapper.text()).toContain('暂未确认这次发布的验收结果')
    const off = await publishMihomo({ config_revision: 8 })
    expect(off.text()).not.toContain('暂未确认这次发布的验收结果')
  })

  it('does not announce inherited successes or results after leaving the editing session', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ service_type: 'mihomo', latest_deployment: {
      deployment_id: 'old', profile_id: 'office', service_type: 'mihomo', config_revision: 7, status: 'succeeded',
    } })]))
    const pending = deferred<any>()
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(() => pending.promise)
    const wrapper = await render({ serviceType: 'mihomo', save: vi.fn().mockResolvedValue(guard({ expected_service_type: 'mihomo' })) })
    expect(wrapper.emitted('published')).toBeUndefined()
    await publish(wrapper)
    const id = vi.mocked(agentApi.pushConfig).mock.lastCall![1]
    await wrapper.setProps({ open: false })
    pending.resolve(response({ deployment_id: id, profile_id: 'office', service_type: 'mihomo', config_revision: 7, status: 'succeeded' }))
    await flushPromises()
    expect(wrapper.emitted('published')).toBeUndefined()
  })

  it('loads and rechecks publish targets without waiting for full content comparison', async () => {
    vi.mocked(agentApi.getAll).mockReturnValue(new Promise(() => {}))
    const wrapper = await render()
    expect(publishButton(wrapper).attributes('disabled')).toBeUndefined()
    expect(agentApi.summaries).toHaveBeenCalledTimes(1)
    await publish(wrapper)
    expect(agentApi.summaries).toHaveBeenCalledTimes(2)
    expect(agentApi.getAll).not.toHaveBeenCalled()
    expect(agentApi.pushConfig).toHaveBeenCalledWith('one', expect.any(String), guard())
  })

  it('reuses the entry snapshot once on open and still rechecks the binding before publishing', async () => {
    const initialTargets = { agents: [agent(), agent({ id: 'two', name: '书房 DNS' })], profiles: [{ id: 'office', name: '办公室', is_default: true }] }
    const wrapper = await render({ preferredAgentId: 'two', initialTargets })
    expect(agentApi.summaries).not.toHaveBeenCalled()
    expect(profileApi.list).not.toHaveBeenCalled()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('two')
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent(), agent({ id: 'two', name: '书房 DNS', profile_id: 'home' })]))
    await publish(wrapper)
    expect(agentApi.summaries).toHaveBeenCalledTimes(1)
    expect(agentApi.pushConfig).not.toHaveBeenCalled()

    // 再次打开不再沿用入口快照
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(agentApi.summaries).toHaveBeenCalledTimes(2)
    expect(profileApi.list).toHaveBeenCalledTimes(2)
  })

  it('reads the targets again when retrying after the entry snapshot was used', async () => {
    const initialTargets = { agents: [agent()], profiles: [{ id: 'office', name: '办公室', is_default: true }] } as any
    const wrapper = await render({ initialTargets })
    expect(agentApi.summaries).not.toHaveBeenCalled()
    vi.mocked(agentApi.summaries).mockRejectedValueOnce(new Error('网络错误'))
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(wrapper.text()).toContain('暂时无法读取目标设备')
    await button(wrapper, '重试读取').trigger('click')
    await flushPromises()
    expect(agentApi.summaries).toHaveBeenCalledTimes(2)
    expect(profileApi.list).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('发布到：客厅 DNS')
  })

  it('uses real preflight stages without inventing deployment steps before submission', async () => {
    const saved = deferred<PublishGuard>()
    const checked = deferred<any>()
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([agent()])).mockImplementationOnce(() => checked.promise)
    const wrapper = await render({ save: vi.fn(() => saved.promise) })
    await publish(wrapper)
    const row = wrapper.get('[data-agent-status-row="配置发布"]')
    expect(row.text()).toContain('保存草稿')
    expect(row.get('[data-status-notice]').isVisible()).toBe(true)
    expect(row.get('[data-status-notice]').text()).toContain('正在保存草稿…')
    expect(row.find('[data-agent-operation-steps], [aria-expanded]').exists()).toBe(false)
    saved.resolve(guard())
    await flushPromises()
    expect(row.text()).toContain('确认设备')
    expect(row.get('[data-status-notice]').text()).toContain('草稿已保存，正在确认目标设备…')
    expect(row.find('[data-agent-operation-steps], [aria-expanded]').exists()).toBe(false)
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    checked.resolve(response([agent()]))
    await flushPromises()
    expect(agentApi.pushConfig).toHaveBeenCalledWith('one', expect.any(String), guard())
  })

  it('collapses successful scope and steps once while keeping DNS warnings in a separate visible row', async () => {
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_id, deploymentId) => response({
      deployment_id: deploymentId, status: 'succeeded', message: '服务检查均通过。',
      events: [{ at: '2026-10-19T01:00:00Z', status: 'verifying', message: '校验 oxidns.yaml' }],
      dns_checks: [{ name: 'DNS 上游', level: 'warn', message: '部分 DNS 请求超时', fix: '检查上游连接' }],
    }))
    const wrapper = await render()
    await publish(wrapper)
    const row = wrapper.get('[data-agent-status-row="配置发布"]')
    expect(row.get('button[aria-expanded]').attributes('aria-label')).toBe('配置发布：成功')
    expect(row.get('[data-status-details]').isVisible()).toBe(false)
    expect(row.find('[data-status-notice]').exists()).toBe(false)
    const dns = wrapper.get('[data-agent-status-row="DNS 检查"]')
    expect(dns.element.closest('[data-status-details]')).toBeNull()
    expect(dns.get('[data-status-notice]').isVisible()).toBe(true)
    expect(dns.get('[data-status-notice]').text()).toContain('部分 DNS 请求超时')
    expect(dns.get('[data-status-details]').isVisible()).toBe(false)
    await row.get('button[aria-expanded]').trigger('click')
    expect(row.get('ol').isVisible()).toBe(true)
    expect(row.get('ol').text()).toContain('校验 oxidns.yaml')
    expect(row.find('details, summary').exists()).toBe(false)
    expect(row.get('[data-status-details]').text()).toContain('草稿已保存，发布成功，服务检查已通过。')
    expect(row.get('[data-status-details]').text()).toContain('本次发布到「客厅 DNS」，仅包含本次保存的配置')
    expect(dns.get('[data-status-details]').isVisible()).toBe(false)
  })

  it('keeps a pending success snapshot unconfirmed in both its heading and visible notice', async () => {
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'succeeded', pending: true }))
    const wrapper = await render()
    await publish(wrapper)
    const row = wrapper.get('[data-agent-status-row="配置发布"]')
    expect(row.get('button[aria-expanded]').attributes('aria-label')).toBe('配置发布：待确认')
    expect(row.get('[data-status-notice]').isVisible()).toBe(true)
    expect(row.get('[data-status-notice]').text()).toContain('草稿已保存，发布结果待确认。')
    expect(row.get('[data-status-details]').isVisible()).toBe(false)
    expect(row.text()).not.toContain('发布成功')
  })

  it.each([
    ['succeeded', true, true],
    ['succeeded', false, false],
    ['failed', true, false],
  ])('links to separate DNS follow results after source %s with binding=%s', async (status, bound, shown) => {
    const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/', component: { render: () => null } }, { path: '/agents', component: { render: () => null } },
    ] })
    await router.push('/')
    await router.isReady()
    vi.mocked(agentApi.summaries).mockResolvedValue(response([
      agent({ service_type: 'mihomo', name: '家庭 Mihomo' }),
      agent({ id: 'dns-target', dns_follow: bound ? { source_agent_id: 'one' } : null }),
    ]))
    vi.mocked(agentApi.pushConfig).mockImplementation(async (_id, deploymentId) => response({ deployment_id: deploymentId, status }))
    const wrapper = await render({ serviceType: 'mihomo', save: vi.fn().mockResolvedValue(guard({ expected_service_type: 'mihomo' })) }, router)
    await publish(wrapper)
    expect(wrapper.find('[data-dns-follow-hint]').exists()).toBe(shown)
    if (shown) {
      expect(wrapper.get('[data-dns-follow-hint]').text()).toContain('后台会继续处理已绑定的 OxiDNS')
      expect(wrapper.get('[data-dns-follow-hint] a').attributes('href')).toBe('/agents')
      expect(wrapper.text()).not.toContain('OxiDNS DNS 同步完成')
    }
  })

  it('shows a single bound target and publishes only after its saved revision is confirmed', async () => {
    const saved = deferred<PublishGuard>()
    const save = vi.fn(() => saved.promise)
    const wrapper = await render({ save })
    expect(wrapper.text()).toContain('办公室')
    expect(wrapper.text()).toContain('发布到：客厅 DNS')
    await publish(wrapper)
    expect(save).toHaveBeenCalledOnce()
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    saved.resolve(guard())
    await flushPromises()
    expect(agentApi.summaries).toHaveBeenCalledTimes(2)
    expect(agentApi.pushConfig).toHaveBeenCalledWith('one', expect.any(String), guard())
    expect(wrapper.text()).toContain('草稿已保存，发布成功，服务检查已通过。')
  })

  it('keeps saving a draft separate from publication', async () => {
    const save = vi.fn()
    const saveDraft = vi.fn()
    const wrapper = await render({ save, onSaveDraft: saveDraft })
    await button(wrapper, '保存草稿').trigger('click')
    expect(saveDraft).toHaveBeenCalledOnce()
    expect(save).not.toHaveBeenCalled()
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
  })

  it('does not publish after a rejected or canceled save', async () => {
    const save = vi.fn().mockResolvedValueOnce(null).mockRejectedValueOnce(new Error('校验未通过'))
    const wrapper = await render({ save })
    await publish(wrapper)
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('草稿已保存')
    await publish(wrapper)
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('草稿保存失败，未发布：校验未通过')
  })

  it.each([
    undefined,
    {},
    guard({ expected_profile_id: 'home' }),
    guard({ expected_service_type: 'mihomo' }),
    guard({ expected_config_revision: Number.NaN }),
    guard({ expected_config_revision: -1 }),
  ])('does not publish without a valid guard for the saved profile and service (%j)', async savedGuard => {
    const wrapper = await render({ save: vi.fn().mockResolvedValue(savedGuard) })
    await publish(wrapper)
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('无法确认本次保存的配置版本')
  })

  it('uses the actual default profile for unbound legacy agents and excludes other profiles and services', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([
      agent({ profile_id: undefined }),
      agent({ id: 'other', name: '家里 DNS', profile_id: 'home' }),
      agent({ id: 'wrong-service', name: '代理核心', service_type: 'mihomo' }),
    ]))
    const wrapper = await render()
    expect(wrapper.text()).toContain('发布到：客厅 DNS')
    expect(wrapper.findComponent(Select).exists()).toBe(false)
    await publish(wrapper)
    expect(agentApi.pushConfig).toHaveBeenCalledWith('one', expect.any(String), guard())
  })

  it('requires an explicit selection when several targets match and sends to only that device', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent(), agent({ id: 'two', name: '书房 DNS' })]))
    const save = vi.fn().mockResolvedValue(guard())
    const wrapper = await render({ save })
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    await publish(wrapper)
    expect(save).not.toHaveBeenCalled()
    await choose(wrapper, 'two')
    await publish(wrapper)
    expect(agentApi.pushConfig).toHaveBeenCalledOnce()
    expect(agentApi.pushConfig).toHaveBeenCalledWith('two', expect.any(String), guard())
  })

  it('preselects the requested agent among several devices in the same profile and service', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent(), agent({ id: 'two', name: '书房 DNS' })]))
    const wrapper = await render({ preferredAgentId: 'two' })
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('two')
    expect(publishButton(wrapper).attributes('disabled')).toBeUndefined()
    await publish(wrapper)
    expect(agentApi.pushConfig).toHaveBeenCalledOnce()
    expect(agentApi.pushConfig).toHaveBeenCalledWith('two', expect.any(String), guard())
  })

  it.each([
    { unavailable: [] },
    { unavailable: [agent({ profile_id: 'home' })] },
    { unavailable: [agent({ service_type: 'mihomo' })] },
  ])('requires an explicit choice when the requested agent is absent from the current scope (%j)', async ({ unavailable }) => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([...unavailable, agent({ id: 'two', name: '书房 DNS' })]))
    const save = vi.fn().mockResolvedValue(guard())
    const saveDraft = vi.fn()
    const wrapper = await render({ preferredAgentId: 'one', save, onSaveDraft: saveDraft })
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('')
    expect(wrapper.text()).toContain('原目标设备不可用于此配置空间的 OxiDNS 配置')
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    expect(button(wrapper, '保存草稿').attributes('disabled')).toBeUndefined()
    await publish(wrapper)
    expect(save).not.toHaveBeenCalled()
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    await button(wrapper, '保存草稿').trigger('click')
    expect(saveDraft).toHaveBeenCalledOnce()
    await choose(wrapper, 'two')
    expect(wrapper.text()).not.toContain('原目标设备不可用于')
    await publish(wrapper)
    expect(agentApi.pushConfig).toHaveBeenCalledOnce()
    expect(agentApi.pushConfig).toHaveBeenCalledWith('two', expect.any(String), guard())
  })

  it.each([{ status: 'offline' }, { enabled: false }])('preserves an unavailable preferred device instead of selecting an online alternative (%j)', async change => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent(change), agent({ id: 'two', name: '书房 DNS' })]))
    const save = vi.fn().mockResolvedValue(guard())
    const wrapper = await render({ preferredAgentId: 'one', save })
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('one')
    expect(wrapper.text()).toContain(change.enabled === false ? '已禁用' : '离线')
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    expect(button(wrapper, '保存草稿').attributes('disabled')).toBeUndefined()
    await publish(wrapper)
    expect(save).not.toHaveBeenCalled()
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
  })

  it('applies the preferred target after retrying an initial list failure', async () => {
    vi.mocked(agentApi.summaries)
      .mockRejectedValueOnce(new Error('网络错误'))
      .mockResolvedValue(response([agent(), agent({ id: 'two', name: '书房 DNS' })]))
    const wrapper = await render({ preferredAgentId: 'two' })
    await button(wrapper, '重试读取').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('two')
    await publish(wrapper)
    expect(agentApi.pushConfig).toHaveBeenCalledWith('two', expect.any(String), guard())
  })

  it.each([{ status: 'offline' }, { enabled: false }])('keeps offline or disabled targets unavailable (%j)', async extra => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent(extra)]))
    const wrapper = await render()
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    expect(button(wrapper, '保存草稿').attributes('disabled')).toBeUndefined()
    await publish(wrapper)
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
  })

  it('allows saving a draft when no targets exist or the list cannot be read', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([]))
    const empty = await render()
    expect(empty.text()).toContain('尚未绑定 OxiDNS 设备')
    expect(button(empty, '保存草稿').attributes('disabled')).toBeUndefined()
    vi.mocked(agentApi.summaries).mockRejectedValueOnce(new Error('网络错误'))
    const failed = await render()
    expect(failed.text()).toContain('暂时无法读取目标设备')
    expect(button(failed, '保存草稿').attributes('disabled')).toBeUndefined()
    await button(failed, '重试读取').trigger('click')
    await flushPromises()
    expect(failed.text()).toContain('发布到：客厅 DNS')
  })

  it.each([['网络错误', '网络错误'], ['服务暂时不可用。', '服务暂时不可用']])('ends a target read failure (%s) with one full stop before the draft hint', async (message, body) => {
    vi.mocked(agentApi.summaries).mockRejectedValueOnce(new Error(message))
    const failed = await render()
    expect(failed.text()).toContain(`暂时无法读取目标设备：${body}。仍可保存草稿。`)
  })

  it.each([{ profile_id: 'home' }, { service_type: 'mihomo' }, { status: 'offline' }, { enabled: false }])('rechecks the selected device after saving (%j)', async change => {
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([agent()])).mockResolvedValueOnce(response([agent(change)]))
    const wrapper = await render()
    await publish(wrapper)
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('目标设备的绑定、类型或在线状态已改变')
  })

  it.each([undefined, 'one'])('does not fall back to the sole remaining target after a binding changes during save (preferred: %s)', async preferredAgentId => {
    const replacement = agent({ id: 'two', name: '书房 DNS' })
    vi.mocked(agentApi.summaries)
      .mockResolvedValueOnce(response([agent(), replacement]))
      .mockResolvedValue(response([agent({ profile_id: 'home' }), replacement]))
    const save = vi.fn().mockResolvedValue(guard())
    const wrapper = await render({ preferredAgentId, save })
    if (!preferredAgentId) await choose(wrapper, 'one')
    await publish(wrapper)
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('')
    expect(wrapper.text()).toContain('目标设备的绑定、类型或在线状态已改变')
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    await publish(wrapper)
    expect(save).toHaveBeenCalledOnce()
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    await choose(wrapper, 'two')
    await publish(wrapper)
    expect(save).toHaveBeenCalledTimes(2)
    expect(agentApi.pushConfig).toHaveBeenCalledOnce()
    expect(agentApi.pushConfig).toHaveBeenCalledWith('two', expect.any(String), guard())
  })

  it.each([{ open: false }, { profileId: 'home' }, { serviceType: 'mihomo' }, { preferredAgentId: 'two' }])('does not continue from a late save after the settings context changes (%j)', async changed => {
    const saved = deferred<PublishGuard>()
    const wrapper = await render({ save: vi.fn(() => saved.promise) })
    await publish(wrapper)
    await wrapper.setProps(changed)
    saved.resolve(guard())
    await flushPromises()
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('草稿已保存')
  })

  it('does not submit after closing while the final target check is pending', async () => {
    const check = deferred<any>()
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([agent()])).mockImplementationOnce(() => check.promise)
    const wrapper = await render()
    await publish(wrapper)
    await wrapper.setProps({ open: false })
    check.resolve(response([agent()]))
    await flushPromises()
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
  })

  it('shows real stages while POST is pending and cannot submit twice or regress on its late response', async () => {
    const post = deferred<any>()
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(() => post.promise)
    vi.mocked(agentApi.getDeployment)
      .mockRejectedValueOnce({ response: { status: 404 } })
      .mockImplementationOnce(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'uploading', message: '正在上传 8 个文件' }))
      .mockImplementationOnce(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'succeeded' }))
    const wrapper = await render()
    await publish(wrapper)
    await publish(wrapper)
    expect(agentApi.pushConfig).toHaveBeenCalledOnce()
    await vi.advanceTimersByTimeAsync(0)
    expect(wrapper.text()).toContain('准备文件')
    expect(wrapper.text()).not.toContain('暂时无法确认发布结果')
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).toContain('正在上传 8 个文件')
    expect(wrapper.text()).toContain('可以离开此页面')
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).toContain('发布成功')
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    post.resolve(response({ deployment_id: vi.mocked(agentApi.pushConfig).mock.lastCall![1], status: 'verifying' }))
    await flushPromises()
    expect(wrapper.text()).toContain('发布成功')
    expect(wrapper.text()).not.toContain('校验中')
    expect(publishButton(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('continues tracking an uncertain transport result instead of offering a duplicate publication', async () => {
    vi.mocked(agentApi.pushConfig).mockRejectedValueOnce(new Error('timeout'))
    const wrapper = await render()
    await publish(wrapper)
    expect(wrapper.text()).toContain('结果待确认')
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(2000)
    expect(agentApi.getDeployment).toHaveBeenCalled()
    expect(wrapper.text()).toContain('上传中')
  })

  it.each(['failed', 'rolled_back'])('allows a fresh save and retry after a confirmed %s result', async status => {
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_id, deploymentId) => response({ deployment_id: deploymentId, status, error: '规则校验失败' }))
    const save = vi.fn().mockResolvedValue(guard())
    const wrapper = await render({ save })
    await publish(wrapper)
    expect(wrapper.text()).toContain('草稿已保存，发布失败')
    expect(wrapper.text()).toContain('规则校验失败')
    expect(publishButton(wrapper).attributes('disabled')).toBeUndefined()
    await publish(wrapper)
    expect(save).toHaveBeenCalledTimes(2)
    expect(agentApi.pushConfig).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('发布成功')
  })

  it('reports a rejected revision without leaving the user stuck in an in-progress state', async () => {
    vi.mocked(agentApi.pushConfig).mockRejectedValueOnce({ response: { status: 409, data: { message: '配置版本已改变，请重新保存' } } })
    const wrapper = await render()
    await publish(wrapper)
    expect(wrapper.text()).toContain('草稿已保存，发布失败')
    expect(wrapper.text()).toContain('配置版本已改变')
    expect(publishButton(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('does not display or announce an old publication in a new profile', async () => {
    const post = deferred<any>()
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(() => post.promise)
    const wrapper = await render()
    await publish(wrapper)
    const deploymentId = vi.mocked(agentApi.pushConfig).mock.lastCall![1]
    await wrapper.setProps({ profileId: 'home' })
    await flushPromises()
    post.resolve(response({ deployment_id: deploymentId, status: 'succeeded' }))
    await flushPromises()
    expect(wrapper.text()).not.toContain('草稿已保存')
    expect(wrapper.text()).not.toContain('发布成功')
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('does not restore an in-flight old task when the same device is rebound to a new profile', async () => {
    const post = deferred<any>()
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(() => post.promise)
    const wrapper = await render()
    await publish(wrapper)
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ profile_id: 'home' })]))
    await wrapper.setProps({ profileId: 'home' })
    await flushPromises()
    expect(wrapper.text()).not.toContain('准备文件')
    expect(wrapper.text()).not.toContain('草稿已保存')
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    post.resolve(response({ deployment_id: vi.mocked(agentApi.pushConfig).mock.lastCall![1], status: 'succeeded' }))
    await flushPromises()
    expect(wrapper.text()).not.toContain('发布成功')
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('lets the user activate a ready snapshot without saving or publishing it again', async () => {
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'ready' }))
    const save = vi.fn().mockResolvedValue(guard())
    const wrapper = await render({ save })
    await publish(wrapper)
    expect(wrapper.text()).toContain('已校验，待激活')
    expect(wrapper.text()).toContain('不包含之后保存的草稿修改')
    expect(wrapper.get('[data-status-details]').isVisible()).toBe(false)
    expect(button(wrapper, '应用并启动').isVisible()).toBe(true)
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    await button(wrapper, '应用并启动').trigger('click')
    await flushPromises()
    expect(agentApi.activateDeployment).toHaveBeenCalledWith('one', vi.mocked(agentApi.pushConfig).mock.lastCall![1])
    expect(save).toHaveBeenCalledOnce()
    expect(agentApi.pushConfig).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('发布成功')
  })

  it('restores a previously ready snapshot and blocks a new publication', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ latest_deployment: { deployment_id: 'old-ready', profile_id: 'office', status: 'ready' } })]))
    const wrapper = await render()
    expect(wrapper.text()).toContain('设备上次发布：已校验，待激活')
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    await button(wrapper, '应用并启动').trigger('click')
    await flushPromises()
    expect(agentApi.activateDeployment).toHaveBeenCalledWith('one', 'old-ready')
  })

  it('checks the binding again before activating an already validated snapshot', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([agent({ latest_deployment: { deployment_id: 'old-ready', profile_id: 'office', status: 'ready' } })]))
    const wrapper = await render()
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([agent({ profile_id: 'home' })]))
    await button(wrapper, '应用并启动').trigger('click')
    await flushPromises()
    expect(agentApi.activateDeployment).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('未激活：目标设备的绑定、类型或在线状态已改变')
  })

  it('allows an explicit replacement when the preferred device with a ready snapshot is rebound', async () => {
    const replacement = agent({ id: 'two', name: '书房 DNS' })
    vi.mocked(agentApi.summaries)
      .mockResolvedValueOnce(response([agent({ latest_deployment: { deployment_id: 'old-ready', profile_id: 'office', status: 'ready' } }), replacement]))
      .mockResolvedValue(response([agent({ profile_id: 'home' }), replacement]))
    const wrapper = await render({ preferredAgentId: 'one' })
    await button(wrapper, '应用并启动').trigger('click')
    await flushPromises()
    expect(agentApi.activateDeployment).not.toHaveBeenCalled()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('')
    expect(wrapper.getComponent(Select).props('disabled')).toBe(false)
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    await choose(wrapper, 'two')
    await publish(wrapper)
    expect(agentApi.pushConfig).toHaveBeenCalledWith('two', expect.any(String), guard())
  })

  it('does not restore a ready candidate created for a different service type', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ service_type: 'mosdns', latest_deployment: {
      deployment_id: 'old-mihomo', profile_id: 'office', service_type: 'mihomo', status: 'ready',
    } })]))
    const wrapper = await render({ serviceType: 'mosdns', save: vi.fn().mockResolvedValue(guard({ expected_service_type: 'mosdns' })) })
    expect(button(wrapper, '应用并启动')).toBeUndefined()
    expect(wrapper.text()).not.toContain('已校验，待激活')
    expect(agentApi.activateDeployment).not.toHaveBeenCalled()
    await publish(wrapper)
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('设备有尚未结束的发布')
  })

  it.each([{ profile_id: 'home' }, { service_type: 'mosdns' }])('rechecks a ready candidate scope using fresh deployment metadata before activation (%j)', async change => {
    const recorded = { deployment_id: 'old-ready', profile_id: 'office', service_type: 'oxidns', status: 'ready' }
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([agent({ latest_deployment: recorded })]))
    const wrapper = await render()
    vi.mocked(agentApi.summaries).mockResolvedValueOnce(response([agent({ latest_deployment: { ...recorded, ...change } })]))
    await button(wrapper, '应用并启动').trigger('click')
    await flushPromises()
    expect(agentApi.activateDeployment).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('未激活：待应用配置的配置空间或软件类型与当前设备不一致')
  })

  it('does not treat a transactional HTTP success without a deployment result as completed', async () => {
    vi.mocked(agentApi.pushConfig).mockResolvedValueOnce(response({ success: true }))
    const wrapper = await render()
    await publish(wrapper)
    expect(wrapper.text()).toContain('结果待确认')
    expect(wrapper.text()).not.toContain('发布成功')
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
  })

  it('ignores an accepted response with a different deployment identity', async () => {
    vi.mocked(agentApi.pushConfig).mockResolvedValueOnce(response({ deployment_id: 'unrelated', status: 'succeeded' }))
    const wrapper = await render()
    await publish(wrapper)
    expect(wrapper.text()).toContain('结果待确认')
    expect(wrapper.text()).not.toContain('发布成功')
    await vi.advanceTimersByTimeAsync(2000)
    expect(agentApi.getDeployment).toHaveBeenCalledWith('one', vi.mocked(agentApi.pushConfig).mock.lastCall![1])
  })

  it('handles Surge synchronous success without claiming transactional service checks', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ service_type: 'surge' })]))
    vi.mocked(agentApi.pushConfig).mockResolvedValueOnce(response({ success: true, restart: { success: true } }))
    const wrapper = await render({ serviceType: 'surge', save: vi.fn().mockResolvedValue(guard({ expected_service_type: 'surge' })) })
    await publish(wrapper)
    expect(wrapper.text()).toContain('草稿已保存，配置已推送。')
    expect(wrapper.text()).not.toContain('服务检查已通过')
    const row = wrapper.get('[data-agent-status-row="配置发布"]')
    expect(row.get('button[aria-expanded]').attributes('aria-label')).toBe('配置发布：已推送')
    expect(row.get('[data-status-details]').isVisible()).toBe(false)
    expect(row.find('[data-agent-operation-steps]').exists()).toBe(false)
    await row.get('button[aria-expanded]').trigger('click')
    expect(row.get('[data-status-details]').isVisible()).toBe(true)
    expect(row.get('[data-status-details]').text()).toContain('草稿已保存，配置已推送。')
    await vi.advanceTimersByTimeAsync(5000)
    expect(agentApi.getDeployment).not.toHaveBeenCalled()
    expect(publishButton(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('keeps an uncertain Surge result separate from a confirmed restart failure', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ service_type: 'surge' })]))
    vi.mocked(agentApi.pushConfig).mockResolvedValueOnce(response({ success: true, restart: { success: false, message: '启动失败' } }))
    const wrapper = await render({ serviceType: 'surge', save: vi.fn().mockResolvedValue(guard({ expected_service_type: 'surge' })) })
    await publish(wrapper)
    expect(wrapper.text()).toContain('草稿已保存，配置已写入，但重启失败')
    expect(wrapper.text()).toContain('启动失败')
    vi.mocked(agentApi.pushConfig).mockRejectedValueOnce(new Error('timeout'))
    await publish(wrapper)
    expect(wrapper.text()).toContain('发布结果待确认')
    expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(5000)
    expect(agentApi.getDeployment).not.toHaveBeenCalled()
  })

  it('does not continue a save after the component is unmounted', async () => {
    const saved = deferred<PublishGuard>()
    const wrapper = await render({ save: vi.fn(() => saved.promise) })
    await publish(wrapper)
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    saved.resolve(guard())
    await flushPromises()
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
  })
})

describe('Settings publication failure details', () => {
  it.each(['mihomo', 'mosdns', 'oxidns'])('keeps %s validation and rollback diagnostics in the settings workspace', async serviceType => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ service_type: serviceType })]))
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'rollback_failed', failed_stage: 'verifying', error: '配置检查失败', error_details: ruleLocationDetails, rollback_error: '恢复失败', rollback_error_details: 'failed to bind :53' }))
    const wrapper = await render({ serviceType, save: vi.fn().mockResolvedValue(guard({ expected_service_type: serviceType as any })) })
    await publish(wrapper)
    expect(wrapper.findAll('pre').map(item => item.element.textContent)).toEqual([ruleLocationDetails, 'failed to bind :53'])
    expect(wrapper.text()).toContain('失败步骤：校验中')
    expect(notify.error).toHaveBeenCalledWith('回滚失败，需要处理', '配置检查失败')
  })
  it('keeps Surge partial-success restart details without reporting the file write as failed', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ service_type: 'surge' })]))
    vi.mocked(agentApi.pushConfig).mockResolvedValueOnce(response({ success: true, restart: { success: false, message: 'Surge reload failed', error_details: 'invalid policy on line 4' } }))
    const wrapper = await render({ serviceType: 'surge', save: vi.fn().mockResolvedValue(guard({ expected_service_type: 'surge' })) })
    await publish(wrapper)
    expect(wrapper.text()).toContain('配置已写入，但重启失败')
    expect(wrapper.get('pre').text()).toBe('invalid policy on line 4')
    expect(wrapper.text()).not.toContain('配置已推送')
  })
  it.each(['mihomo', 'mosdns', 'oxidns', 'surge'] as const)('preserves a %s pre-submission rejection and clears it when changing the editing session', async serviceType => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([agent({ service_type: serviceType })]))
    vi.mocked(agentApi.pushConfig).mockRejectedValueOnce({ response: { status: 400, data: { message: '本地校验失败', error_details: ruleLocationDetails } } })
    const wrapper = await render({ serviceType, save: vi.fn().mockResolvedValue(guard({ expected_service_type: serviceType })) })
    await publish(wrapper)
    expect(wrapper.get('pre').element.textContent).toBe(ruleLocationDetails)
    const row = wrapper.get('[data-agent-status-row="配置发布"]')
    expect(row.attributes('data-tone')).toBe('danger')
    expect(row.get('[data-status-notice]').isVisible()).toBe(true)
    expect(row.find('[data-agent-operation-steps], [aria-expanded]').exists()).toBe(false)
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(wrapper.find('details').exists()).toBe(false)
  })
})

it('shows server diagnostics while a 5xx publication result remains uncertain', async () => {
  vi.mocked(agentApi.pushConfig).mockRejectedValueOnce({ response: { status: 503, data: { message: '目标设备暂时无响应', error_details: 'response ended while checking startup' } } })
  const wrapper = await render()
  await publish(wrapper)
  expect(wrapper.text()).toContain('结果待确认')
  expect(wrapper.get('pre').text()).toBe('response ended while checking startup')
  expect(publishButton(wrapper).attributes('disabled')).toBeDefined()
})

it('preserves late server diagnostics even when polling already confirmed the deployment', async () => {
  const post = deferred<any>()
  vi.mocked(agentApi.pushConfig).mockImplementationOnce(() => post.promise)
  vi.mocked(agentApi.getDeployment).mockImplementationOnce(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'succeeded' }))
  const wrapper = await render()
  await publish(wrapper)
  await vi.advanceTimersByTimeAsync(0)
  post.reject({ response: { status: 503, data: { message: '返回响应时连接中断', error_details: 'connection closed after service check' } } })
  await flushPromises()
  const row = wrapper.get('[data-agent-status-row="配置发布"]')
  expect(row.get('button[aria-expanded]').attributes('aria-label')).toBe('配置发布：成功')
  expect(row.get('[data-status-notice]').isVisible()).toBe(true)
  expect(row.get('[data-status-notice] pre').text()).toBe('connection closed after service check')
  expect(row.get('[data-status-details]').isVisible()).toBe(false)
})
