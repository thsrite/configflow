import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, defineComponent, markRaw } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import GenerateStudio from '@/components/generate/GenerateStudio.vue'
import { agentApi, generateApi, statsApi, ruleSetApi } from '@/api'
import { notify } from '@/lib/feedback'
import { ruleLocationDetails } from './fixtures/agent-failure'
import { Select } from '@/components/ui/select'

const live = vi.hoisted(() => ({ agents: [] as any[], start: vi.fn(), stop: vi.fn(), refresh: vi.fn() }))
vi.mock('@/api', () => ({
  agentApi: { pushConfig: vi.fn(), getDeployment: vi.fn(), generateConfig: vi.fn() },
  generateApi: { artifact: vi.fn() },
  statsApi: { getOverview: vi.fn() }, ruleSetApi: { getAll: vi.fn() },
}))
vi.mock('@/stores/live', () => ({ useLive: () => ({ ...live, boundAgents: computed(() => live.agents), isOnline: () => true }) }))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn(), loading: vi.fn(), dismiss: vi.fn() } }))
const wrappers: VueWrapper[] = []
const labels = { mihomo: 'Mihomo', mosdns: 'MosDNS', oxidns: 'OxiDNS', surge: 'Surge' } as const
const icon = markRaw(defineComponent({ template: '<i />' }))
const button = (w: VueWrapper, label: string) => w.findAll('button').find(item => item.text() === label)!
beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers()
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => setTimeout(() => cb(1), 1))
  for (const api of Object.values(generateApi)) vi.mocked(api).mockResolvedValue({ data: { content: 'rules: []' } } as any)
  vi.mocked(agentApi.generateConfig).mockImplementation(async (id, profile, service) => ({ data: {
    id: `preview-${id}`, deployment_id: `deployment-${id}`, agent_id: id, profile_id: profile, service_type: service,
    content: 'rules: []', config_path: 'config.yaml', files: [{ path: 'config.yaml', size: 9, role: 'config', sha256: 'hash' }], target_ids: [id], expires_at: 9999999999
  } }) as any)
  vi.mocked(statsApi.getOverview).mockResolvedValue({ data: { success: true, data: {} } } as any)
  vi.mocked(ruleSetApi.getAll).mockResolvedValue({ data: [] } as any)
})
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); document.body.innerHTML = ''; vi.useRealTimers(); vi.unstubAllGlobals() })
async function render(service: keyof typeof labels, agents = [{ id: service as string, name: `${labels[service]} test`, service_type: service as string }]) {
  live.agents = agents
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: icon }, { path: '/agents', component: icon }] })
  await router.push('/')
  const targets = Object.entries(labels).map(([key, title]) => ({ key: key as keyof typeof labels, title, icon, desc: '', url: '', urlDisplay: '', actions: [] }))
  const wrapper = mount(GenerateStudio, { props: { targets, profileId: 'default' }, global: { plugins: [router] }, attachTo: document.body })
  wrappers.push(wrapper)
  if (service !== 'mihomo') await button(wrapper, labels[service]).trigger('click')
  for (const agent of agents) {
    wrapper.findAllComponents(Select)[1].vm.$emit('update:modelValue', agent.id)
    await flushPromises()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    expect(button(wrapper, '推送').attributes('disabled')).toBeUndefined()
    await button(wrapper, '推送').trigger('click')
    await flushPromises()
  }
  return wrapper
}

describe('Generated config publication diagnostics', () => {
  it('pushes the displayed generation and never asks the server to render again', async () => {
    vi.mocked(agentApi.pushConfig).mockImplementation(async (_id, deploymentId) => ({ data: { deployment_id: deploymentId, status: 'succeeded' } }) as any)
    const wrapper = await render('mihomo')
    expect(wrapper.get('.gen-code').text()).toContain('rules: []')
    expect(agentApi.generateConfig).toHaveBeenCalledTimes(1)
    expect(agentApi.generateConfig).toHaveBeenCalledWith('mihomo', 'default', 'mihomo')
    expect(agentApi.pushConfig).toHaveBeenCalledWith('mihomo', 'deployment-mihomo', { generation_id: 'preview-mihomo' })
    expect(generateApi.artifact).not.toHaveBeenCalled()
  })

  it('keeps the old preview visible but prevents another push after a stale rejection', async () => {
    vi.mocked(agentApi.pushConfig).mockRejectedValueOnce({ response: { status: 409, data: { code: 'preview_changed', message: '配置已改变，请重新生成并检查后推送。' } } })
    const wrapper = await render('mihomo')
    expect(wrapper.get('.gen-code').text()).toContain('rules: []')
    expect(wrapper.get('[role="alert"]').text()).toContain('重新生成')
    expect(button(wrapper, '推送').attributes('disabled')).toBeDefined()
    expect(agentApi.generateConfig).toHaveBeenCalledTimes(1)
  })

  it('clears an old result when the target or profile changes', async () => {
    vi.mocked(agentApi.pushConfig).mockImplementation(async (_id, deploymentId) => ({ data: { deployment_id: deploymentId, status: 'succeeded' } }) as any)
    const wrapper = await render('mihomo')
    wrapper.findAllComponents(Select)[1].vm.$emit('update:modelValue', 'manual')
    await flushPromises()
    expect(wrapper.get('.gen-code').text()).toBe('')
    expect(button(wrapper, '推送').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ profileId: 'different' })
    expect(button(wrapper, '推送').attributes('disabled')).toBeDefined()
  })

  it('lists separate device outcomes with one shared history link and a single disclosure for successful logs', async () => {
    const completedName = '家庭网络与办公设备的长名称主网关'
    const tasks: Record<string, Record<string, unknown>> = {
      home: { status: 'succeeded', message: '配置校验、重载和服务检查均通过。', events: [
        { at: '2026-10-19T01:00:00Z', status: 'verifying', message: '校验 home.yaml' },
        { at: '2026-10-19T01:00:03Z', status: 'succeeded', message: '服务检查通过' },
      ] },
      office: { status: 'preparing', message: '正在下载规则集「办公服务」（2/3）', events: [
        { at: '2026-10-19T01:00:00Z', status: 'preparing', message: '正在下载规则集「办公服务」（2/3）' },
      ] },
      standby: { status: 'ready', message: '配置已校验，请到 Agent 页面激活本次发布。' },
    }
    vi.mocked(agentApi.pushConfig).mockImplementation(async (id, deploymentId) => ({ data: { deployment_id: deploymentId, ...tasks[id] } }) as any)
    const wrapper = await render('mihomo', [
      { id: 'home', name: completedName, service_type: 'mihomo' },
      { id: 'office', name: '办公网关', service_type: 'mihomo' },
      { id: 'standby', name: '备用网关', service_type: 'mihomo' },
    ])
    const result = wrapper.get('[aria-label="配置发布结果"]')
    expect(agentApi.pushConfig).toHaveBeenCalledTimes(3)
    expect(result.findAll('a[href="/agents"]')).toHaveLength(1)
    expect(result.get('a[href="/agents"]').element.closest('[data-agent-status-row]')).toBeNull()

    const completed = result.get(`[data-agent-status-row="${completedName}"]`)
    expect(completed.get('button[aria-expanded]').attributes('aria-label')).toBe(`${completedName}：成功`)
    expect(completed.get('[data-status-details]').isVisible()).toBe(false)
    expect(completed.find('[data-status-notice]').exists()).toBe(false)
    expect(completed.get('ol').isVisible()).toBe(false)
    await completed.get('button[aria-expanded]').trigger('click')
    expect(completed.get('[data-status-details]').isVisible()).toBe(true)
    expect(completed.get('ol').isVisible()).toBe(true)
    expect(completed.get('ol').text()).toContain('校验 home.yaml')
    expect(completed.get('ol').text()).toContain('服务检查通过')
    expect(completed.find('details, summary').exists()).toBe(false)

    const running = result.get('[data-agent-status-row="办公网关"]')
    expect(running.get('button[aria-expanded]').text()).toContain('准备文件')
    expect(running.get('[data-status-notice]').isVisible()).toBe(true)
    expect(running.get('[data-status-notice]').text()).toContain('正在下载规则集「办公服务」（2/3）')
    expect(running.get('[data-status-details]').isVisible()).toBe(false)
    expect(running.get('ol').isVisible()).toBe(false)
    const ready = result.get('[data-agent-status-row="备用网关"]')
    expect(ready.get('button[aria-expanded]').text()).toContain('待激活')
    expect(ready.get('[data-status-notice]').isVisible()).toBe(true)
    expect(ready.get('[data-status-notice]').text()).toContain('请到 Agent 页面激活')
    expect(ready.get('[data-status-details]').isVisible()).toBe(false)
  })

  it('keeps a pending report visibly unconfirmed without opening the recorded steps', async () => {
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_agent, deploymentId) => ({ data: {
      deployment_id: deploymentId, status: 'checking', pending: true,
      message: '等待 Agent 回执，暂未确认服务启动。',
      events: [{ at: '2026-10-19T01:00:00Z', status: 'starting', message: '正在启动服务' }],
    } }) as any)
    const wrapper = await render('mihomo')
    const row = wrapper.get('[data-agent-status-row="Mihomo test"]')
    expect(row.get('button[aria-expanded]').attributes('aria-label')).toBe('Mihomo test：待确认')
    expect(row.attributes('data-tone')).toBe('warning')
    expect(row.get('[data-status-notice]').isVisible()).toBe(true)
    expect(row.get('[data-status-notice]').text()).toContain('等待 Agent 回执，暂未确认服务启动')
    expect(row.get('[data-status-details]').isVisible()).toBe(false)
    expect(row.get('ol').isVisible()).toBe(false)
    expect(notify.success).not.toHaveBeenCalledWith('配置发布成功，服务检查已通过')
    expect(notify.success).not.toHaveBeenCalledWith('已推送到 1 台 Agent')
  })

  it('shows real preparation objects while POST is pending and does not regress to its older response', async () => {
    let resolvePost!: (value: any) => void
    let requestId = ''
    vi.mocked(agentApi.pushConfig).mockImplementationOnce((_agent, deploymentId) => {
      requestId = deploymentId!
      return new Promise(resolve => { resolvePost = resolve })
    })
    const events = [{ at: '2026-10-18T12:00:00Z', status: 'preparing', message: '正在下载规则集「海外服务」' }]
    vi.mocked(agentApi.getDeployment).mockImplementation(async (_id, deploymentId) => ({ data: {
      deployment_id: deploymentId, status: 'preparing', message: events[0].message, events,
    } }) as any)
    const wrapper = await render('mihomo')
    expect(wrapper.text()).toContain('正在提交发布请求')
    await vi.advanceTimersByTimeAsync(0)
    await flushPromises()
    const progress = wrapper.get('[aria-label="配置发布结果"]')
    expect(progress.get('[role="status"]').text()).toContain('正在下载规则集「海外服务」')
    expect(progress.findAll('li')).toHaveLength(1)
    expect(progress.get('[role="status"]').isVisible()).toBe(true)
    expect(progress.get('[data-status-details]').isVisible()).toBe(false)
    expect(notify.loading).not.toHaveBeenCalled()
    resolvePost({ data: { deployment_id: requestId, status: 'preparing', message: '请求已创建' } })
    await flushPromises()
    expect(progress.get('[role="status"]').text()).toContain('正在下载规则集「海外服务」')
    expect(agentApi.pushConfig).toHaveBeenCalledOnce()
  })
  it('retains DNS warnings beside the push controls after a successful publication', async () => {
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_agent, deploymentId) => ({ data: { deployment_id: deploymentId, status: 'succeeded', dns_checks: [{ name: 'DNS 解析', level: 'warn', message: '当前 DNS 查询超时', fix: '检查 DNS 上游' }] } }) as any)
    const wrapper = await render('mihomo')
    const result = wrapper.get('[aria-label="配置发布结果"]')
    expect(result.text()).toContain('成功')
    expect(result.text()).toContain('警告 · DNS 解析')
    expect(result.text()).toContain('修复建议：检查 DNS 上游')
    const publication = result.get('[data-agent-status-row="Mihomo test"]')
    const dns = result.get('[data-agent-status-row="DNS 检查"]')
    expect(publication.get('[data-status-details]').isVisible()).toBe(false)
    expect(dns.element.closest('[data-status-details]')).toBeNull()
    expect(dns.get('[data-status-notice]').isVisible()).toBe(true)
    expect(dns.get('[data-status-notice]').text()).toContain('当前 DNS 查询超时')
    expect(dns.get('[data-status-details]').isVisible()).toBe(false)
    await dns.get('button[aria-expanded]').trigger('click')
    expect(dns.get('[data-status-details]').isVisible()).toBe(true)
    expect(dns.get('[data-status-details]').text()).toContain('修复建议：检查 DNS 上游')
    expect(publication.get('[data-status-details]').isVisible()).toBe(false)
    expect(notify.warning).toHaveBeenCalledExactlyOnceWith('配置已发布，DNS 检查有提醒', '当前 DNS 查询超时')
    expect(notify.success).not.toHaveBeenCalledWith('配置发布成功，服务检查已通过')
    expect(notify.success).not.toHaveBeenCalledWith('已推送到 1 台 Agent')
  })
  it.each(['mihomo', 'mosdns', 'oxidns'] as const)('shows %s terminal task details by the push controls', async service => {
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_agent, deploymentId) => ({ data: { deployment_id: deploymentId, status: 'failed', failed_stage: 'verifying', error: '配置校验失败', error_details: ruleLocationDetails } }) as any)
    const wrapper = await render(service)
    expect(wrapper.get('[aria-label="配置发布结果"]').text()).toContain('失败步骤：校验中')
    const row = wrapper.get(`[data-agent-status-row="${labels[service]} test"]`)
    expect(row.get('button[aria-expanded]').attributes('aria-label')).toBe(`${labels[service]} test：失败`)
    expect(row.get('[data-status-notice]').isVisible()).toBe(true)
    expect(row.get('[data-status-notice]').text()).toContain('失败步骤：校验中')
    expect(row.get('[data-status-notice]').findAll('p').find(item => item.text() === '配置校验失败')?.isVisible()).toBe(true)
    expect(row.get('[data-status-details]').isVisible()).toBe(false)
    expect(wrapper.get('pre[tabindex="0"]').element.textContent).toBe(ruleLocationDetails)
    expect(wrapper.get('a[href="/agents"]').text()).toContain('查看发布记录')
    expect(notify.error).toHaveBeenCalledWith('发布失败', '配置校验失败')
  })
  it('does not treat Surge config-write success as restart success', async () => {
    vi.mocked(agentApi.pushConfig).mockResolvedValueOnce({ data: { success: true, restart: { success: false, message: '重启失败', error_details: 'line 17: invalid policy' } } } as any)
    const wrapper = await render('surge')
    expect(wrapper.get('[aria-label="配置发布结果"]').text()).toContain('配置已写入，但重启失败')
    expect(wrapper.get('pre[tabindex="0"]').text()).toBe('line 17: invalid policy')
    expect(notify.success).not.toHaveBeenCalledWith('已推送到 1 台 Agent')
  })
  it.each(['mihomo', 'mosdns', 'oxidns', 'surge'] as const)('preserves a %s HTTP rejection even when no deployment id was assigned', async service => {
    vi.mocked(agentApi.pushConfig).mockRejectedValueOnce({ response: { status: 400, data: { message: '生成失败', error_details: ruleLocationDetails } } })
    const wrapper = await render(service)
    expect(wrapper.get('pre[tabindex="0"]').element.textContent).toBe(ruleLocationDetails)
    expect(wrapper.get('[aria-label="配置发布结果"]').text()).toContain('生成失败')
    const row = wrapper.get(`[data-agent-status-row="${labels[service]} test"]`)
    expect(row.attributes('data-tone')).toBe('danger')
    expect(row.get('[data-status-notice]').isVisible()).toBe(true)
    expect(row.find('[aria-expanded], [data-agent-operation-steps], [aria-label="步骤记录"]').exists()).toBe(false)
  })
})
