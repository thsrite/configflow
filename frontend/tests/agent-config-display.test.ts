import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import Agents from '@/views/Agents.vue'
import api, { agentApi, profileApi } from '@/api'
import { setActiveProfileId } from '@/profileContext'
import { getAgentConfigSync } from '@/lib/agentConfigSync'
import { Select } from '@/components/ui/select'
import { ruleLocationDetails } from './fixtures/agent-failure'

// Keep the actual Agent cards, stores and deployment state; mock their HTTP requests.
vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  agentApi: { getAll: vi.fn(), getDeployment: vi.fn(), getUpgrade: vi.fn(), bindProfile: vi.fn(), pushConfig: vi.fn() },
  profileApi: { list: vi.fn() }
}))

const wrappers: ReturnType<typeof mount>[] = []
let narrowViewport = false
const mediaDescriptor = Object.getOwnPropertyDescriptor(window, 'matchMedia')
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
const canvasDescriptor = Object.getOwnPropertyDescriptor(HTMLCanvasElement.prototype, 'getContext')

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: (query: string) => ({
    matches: query.includes('prefers-reduced-motion') || (narrowViewport && query === '(max-width: 639px)'), media: query,
    addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn()
  }) })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  // jsdom has no canvas drawing surface.
  Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', { configurable: true, value: () => null })
})

afterAll(() => {
  if (mediaDescriptor) Object.defineProperty(window, 'matchMedia', mediaDescriptor)
  else Reflect.deleteProperty(window, 'matchMedia')
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
  if (canvasDescriptor) Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', canvasDescriptor)
  else Reflect.deleteProperty(HTMLCanvasElement.prototype, 'getContext')
})

beforeEach(() => {
  narrowViewport = false
  vi.clearAllMocks()
  setActiveProfileId('default')
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  sessionStorage.clear()
})

function deployment(overrides: Record<string, unknown> = {}) {
  return {
    deployment_id: 'deployment-success', status: 'succeeded', profile_id: 'default',
    config_revision: 43, config_version: 'hash-current', updated_at: '2026-10-10T12:00:00Z',
    ...overrides
  }
}

function agent(overrides: Record<string, unknown> = {}) {
  return {
    id: 'config-display-agent', name: 'Config display test', host: '192.0.2.20', port: 8080,
    profile_id: 'default', status: 'online', service_type: 'mosdns', deployment_method: 'shell',
    version: '1.3.0-go', last_heartbeat: '', config_version: 'hash-current', config_revision: 43,
    content_sync: { status: 'synced', checked_at: '2026-10-11T12:00:00Z' },
    enabled: true, latest_deployment: deployment(), ...overrides
  }
}

async function render(item = agent(), profiles: Record<string, unknown>[] = [{ id: 'default', name: 'Default', revision: 43 }]) {
  vi.mocked(agentApi.getAll).mockResolvedValue({ data: Array.isArray(item) ? item : [item] } as any)
  vi.mocked(profileApi.list).mockResolvedValue({ data: profiles } as any)
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/agents', component: { render: () => null } }] })
  await router.push('/agents')
  await router.isReady()
  const wrapper = mount(Agents, { attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(wrapper)
  await flushPromises()
  const footer = wrapper.get('footer')
  return { wrapper, footer, title: () => footer.get('[title]').attributes('title') }
}

describe('Agent page layout', () => {
  it('shows the device cards directly, without a delivery topology', async () => {
    const { wrapper } = await render([
      agent(),
      agent({ id: 'offline-agent', name: 'Offline test', status: 'offline' })
    ] as any)
    expect(wrapper.find('[data-agent-topology]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Offline test')
    expect(wrapper.get('footer').text()).toContain('已同步')
  })
})

describe('Agent configuration status wording', () => {
  it('polls real preparation steps before the push request returns', async () => {
    vi.useFakeTimers()
    try {
      let resolvePost!: (value: any) => void
      let requestId = ''
      vi.mocked(agentApi.pushConfig).mockImplementationOnce((_id, deploymentId) => {
        requestId = deploymentId!
        return new Promise(resolve => { resolvePost = resolve })
      })
      const events = [{ at: '2026-10-18T12:00:00Z', status: 'preparing', message: '正在校验规则集「本地服务」' }]
      vi.mocked(agentApi.getDeployment).mockImplementation(async (_id, deploymentId) => ({ data: {
        deployment_id: deploymentId, status: 'preparing', message: events[0].message, events,
      } }) as any)
      const { wrapper, footer } = await render()
      await footer.findAll('button').find(button => button.text() === '推送')!.trigger('click')
      await flushPromises()
      await vi.advanceTimersByTimeAsync(0)
      await flushPromises()
      expect(wrapper.get('[data-agent-operation-steps] [role="status"]').text()).toContain(events[0].message)
      resolvePost({ data: { deployment_id: requestId, status: 'preparing', message: '请求已创建' } })
      await flushPromises()
      expect(wrapper.get('[data-agent-operation-steps] [role="status"]').text()).toContain(events[0].message)
    } finally { vi.useRealTimers() }
  })
  it('trusts matching complete contents even when revisions differ, keeping revisions only in the tooltip', async () => {
    const revision = 1234567890123
    const { wrapper, footer, title } = await render(agent({
      config_revision: revision, latest_deployment: deployment({ config_revision: revision })
    }), [{ id: 'default', name: 'Default', revision: revision + 1 }])
    expect(footer.text()).toContain('已同步')
    expect(footer.text()).not.toContain(String(revision))
    expect(wrapper.text()).not.toContain(String(revision))
    expect(title()).toContain(`已推送修订号：v${revision}`)
    expect(title()).toContain(`当前配置修订号：v${revision + 1}`)
    expect(title()).toContain('hash-current')
    expect(title()).toContain('完整文件')
    expect(wrapper.get('[data-agent-status-row="配置推送"] > div > button').text()).toBe('配置推送成功')
    expect(wrapper.text()).not.toContain('发布成功')
  })

  it('shows changed complete contents even with the same revision and main-config hash', async () => {
    const { wrapper, footer, title } = await render(agent({ content_sync: { status: 'changed' } }))
    expect(footer.text()).toContain('有变更，待推送')
    expect(footer.text()).not.toContain('已同步')
    expect(title()).toContain('已推送修订号：v43')
    expect(title()).toContain('当前配置修订号：v43')
    expect(wrapper.get('[data-agent-status-row="配置推送"] > div > button').text()).toBe('配置推送成功')
  })

  it('opens what the next push changes from the changed label, as a keyboard button', async () => {
    narrowViewport = true
    vi.mocked(api.get).mockImplementation(async (path: string) => ({
      data: path.endsWith('/push-diff') ? { status: 'synced', baseline: { pushed_at: '2026-10-10T12:00:00Z' } } : {},
    }) as any)
    const { footer } = await render(agent({ content_sync: { status: 'changed' } }))
    const label = footer.get('button[data-sync-diff]')
    expect(label.attributes('type')).toBe('button')
    expect(label.text()).toContain('有变更，待推送')
    expect(label.attributes('aria-label')).toContain('查看「Config display test」这次推送会改动的内容')
    expect(label.classes()).toContain('max-md:min-h-11')
    await label.trigger('click')
    await flushPromises()
    expect(api.get).toHaveBeenCalledWith('/agents/config-display-agent/push-diff', expect.anything())
    expect(document.body.querySelector('[data-push-diff]')!.textContent).toContain('无变更')
  })

  it('keeps states without a server conclusion as plain text', async () => {
    const { footer } = await render(agent({ content_sync: { status: 'checking' } }))
    expect(footer.text()).toContain('正在核对配置')
    expect(footer.find('button[data-sync-diff]').exists()).toBe(false)
  })

  it('lets phones read why there is no baseline from the same entry', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => ({
      data: path.endsWith('/push-diff') ? { status: 'unknown', reason: 'baseline_missing' } : {},
    }) as any)
    const { footer } = await render(agent({ content_sync: { status: 'unknown', reason: 'baseline_missing' } }))
    await footer.get('button[data-sync-diff]').trigger('click')
    await flushPromises()
    const dialog = document.body.querySelector('[data-push-diff]')!
    expect(dialog.textContent).toContain('没有可对比的基准')
    expect(dialog.textContent).toContain('旧版本')
  })

  it('uses the content comparison after a rebind instead of inferring from profile identity', async () => {
    const { footer } = await render(agent({ profile_id: 'second', content_sync: { status: 'changed' } }), [
      { id: 'default', name: 'Default', revision: 43 }, { id: 'second', name: 'Second', revision: 43 }
    ])
    expect(footer.text()).toContain('有变更，待推送')
    expect(footer.text()).not.toContain('已同步')
  })

  it.each(['failed', 'rolled_back'])('keeps a confirmed successful content baseline after a %s push', async status => {
    const success = deployment()
    const failed = deployment({ deployment_id: 'deployment-failed', status, config_version: 'failed-hash', updated_at: '2026-10-10T13:00:00Z' })
    const { footer } = await render(agent({
      latest_deployment: failed,
      deployments: { [success.deployment_id]: success, [failed.deployment_id]: failed }
    }), [{ id: 'default', name: 'Default', revision: 50 }])
    expect(footer.text()).toContain('已同步')
    expect(footer.text()).not.toContain('有变更，待推送')
  })

  it.each([
    ['matching revisions', {}],
    ['a later current revision', { config_revision: 20 }],
    ['revision zero', { config_revision: 0 }],
    ['legacy hash without revision', { config_revision: undefined, latest_deployment: undefined }],
    ['successful history without a hash or revision', { config_version: '0', config_revision: undefined }],
  ])('does not infer synchronization on an old server with %s', async (_name, overrides) => {
    const { footer } = await render(agent({ ...overrides, content_sync: undefined }))
    expect(footer.text()).toContain('同步状态待确认')
    expect(footer.text()).not.toContain('已同步')
  })

  it('shows never pushed on an old server only when there is no success evidence', async () => {
    const { footer } = await render(agent({ content_sync: undefined, config_version: '0', config_revision: undefined, latest_deployment: undefined }))
    expect(footer.text()).toContain('尚未推送')
    expect(footer.text()).not.toContain('已同步')
  })

  it.each([
    ['checking', '正在核对配置'],
    ['unknown', '同步状态待确认'],
    ['never-pushed', '尚未推送'],
  ])('renders the server %s state', async (status, label) => {
    const { footer } = await render(agent({ content_sync: { status } }))
    expect(footer.text()).toContain(label)
  })

  it('explains a missing baseline without asserting equality from old revisions or hashes', async () => {
    const { footer, title } = await render(agent({ content_sync: { status: 'unknown', reason: 'baseline_missing' } }))
    expect(footer.text()).toContain('同步状态待确认')
    expect(title()).toContain('尚无完整下发内容记录，成功推送一次后可准确比较')
  })

  it('uses a generic explanation for comparison failures without displaying raw errors', async () => {
    const reason = 'synthetic-private-error-detail'
    const { wrapper, footer, title } = await render(agent({ content_sync: { status: 'unknown', reason } }))
    expect(footer.text()).toContain('同步状态待确认')
    expect(title()).toContain('暂时无法确认完整下发内容是否一致')
    expect(wrapper.html()).not.toContain(reason)
  })

  it('keeps Mihomo and MosDNS content comparisons independent within the same profile', async () => {
    const rows = [
      agent({ id: 'mihomo-test', service_type: 'mihomo', content_sync: { status: 'changed' } }),
      agent({ id: 'mosdns-test', service_type: 'mosdns', content_sync: { status: 'synced' } }),
    ]
    const { wrapper } = await render(rows as any, [{ id: 'default', name: 'Default', revision: 50 }])
    const footers = wrapper.findAll('footer')
    expect(footers[0]!.text()).toContain('有变更，待推送')
    expect(footers[1]!.text()).toContain('已同步')
  })

  it('invalidates a previous content comparison immediately after binding another profile', async () => {
    const { wrapper, footer } = await render(agent(), [
      { id: 'default', name: 'Default', revision: 43 }, { id: 'second', name: 'Second', revision: 43 },
    ])
    let finishRefresh!: (value: unknown) => void
    vi.mocked(agentApi.getAll).mockImplementationOnce(() => new Promise(resolve => { finishRefresh = resolve }) as any)
    vi.mocked(agentApi.bindProfile).mockResolvedValueOnce({ data: { success: true } } as any)
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'second')
    await flushPromises()
    expect(agentApi.bindProfile).toHaveBeenCalledWith('config-display-agent', 'second')
    expect(footer.text()).toContain('正在核对配置')
    finishRefresh({ data: [agent({ profile_id: 'second', content_sync: { status: 'changed' } })] })
    await flushPromises()
    expect(footer.text()).toContain('有变更，待推送')
  })
})

describe('Docker installation choices', () => {
  it('generates Mihomo + OxiDNS AIO commands while retaining standalone MosDNS and existing Agents', async () => {
    const existing = agent()
    const { wrapper } = await render(existing)
    vi.mocked(api.get).mockImplementation(async path => ({ data: path.endsWith('compose') ? 'generated-compose' : 'generated-run' }) as any)
    const click = async (selector: string) => {
      const element = document.querySelector<HTMLElement>(selector)
      expect(element).not.toBeNull()
      element!.click()
      await flushPromises()
    }
    const generate = async () => {
      const button = Array.from(document.querySelectorAll('button')).find(item => item.textContent?.trim() === '生成 Docker 部署命令')
      expect(button).toBeDefined()
      button!.click()
      await flushPromises()
    }
    await wrapper.findAll('button').find(button => button.text() === '生成安装脚本')!.trigger('click')
    await flushPromises()
    await click('[role="radio"][value="docker"]')
    await click('[role="radio"][value="aio"]')
    const dialog = document.querySelector('[role="dialog"]')!
    const name = dialog.querySelector<HTMLInputElement>('#agent-name')!
    name.value = 'new-aio'
    name.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    await click('button[aria-label="Docker 模式说明"]')
    expect(document.querySelector('[data-field-help-content]')?.textContent).toContain('Agent + Mihomo + OxiDNS')
    await click('button[aria-label="Docker 模式说明"]')
    expect(dialog.textContent).not.toContain('MosDNS Agent')
    const port = (text: string) => {
      const label = Array.from(dialog.querySelectorAll('label')).find(item => item.textContent?.trim() === text)
      return label?.htmlFor ? dialog.querySelector<HTMLInputElement>(`#${label.htmlFor}`) ?? undefined : undefined
    }
    expect(port('Mihomo Agent 端口')?.value).toBe('8080')
    expect(port('OxiDNS Agent 端口')?.value).toBe('8082')
    port('OxiDNS Agent 端口')!.value = '8182'
    port('OxiDNS Agent 端口')!.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    await generate()
    for (const endpoint of ['/agents/docker-agent-compose', '/agents/docker-agent-run']) {
      expect(api.get).toHaveBeenCalledWith(endpoint, { params: expect.objectContaining({
        enable_mihomo: true, enable_oxidns: true, enable_mosdns: false,
        mihomo_port: 8080, oxidns_port: 8182, data_dir: './aio_data'
      }) })
    }
    expect(Array.from(dialog.querySelectorAll('textarea')).map(input => input.value))
      .toEqual(expect.arrayContaining(['generated-compose', 'generated-run']))
    vi.mocked(api.get).mockClear()
    await click('[role="radio"][value="mosdns"]')
    expect(port('MosDNS Agent 端口')?.value).toBe('8081')
    expect(port('OxiDNS Agent 端口')).toBeUndefined()
    await generate()
    for (const endpoint of ['/agents/docker-agent-compose', '/agents/docker-agent-run']) {
      expect(api.get).toHaveBeenCalledWith(endpoint, { params: expect.objectContaining({
        enable_mihomo: false, enable_oxidns: false, enable_mosdns: true,
        mosdns_port: 8081, data_dir: './mosdns_data'
      }) })
    }
    expect(api.post).not.toHaveBeenCalled()
    expect(api.put).not.toHaveBeenCalled()
    expect(api.delete).not.toHaveBeenCalled()
    expect(agentApi.bindProfile).not.toHaveBeenCalled()
    expect(wrapper.get('footer').text()).toContain('已同步')
    expect(existing.service_type).toBe('mosdns')
  })
})

describe('deployment progress precedes the content comparison', () => {
  it.each([
    [{ status: 'preparing' }, '正在推送'],
    [{ status: 'ready' }, '已校验，待应用'],
    [{ status: 'checking' }, '正在推送'],
    [{ status: 'rolling_back', manual_rollback: true }, '正在回滚'],
    [{ status: 'unknown' }, '同步状态待确认'],
    [{ status: 'rollback_failed' }, '同步状态待确认'],
    [{ status: 'succeeded', pending: true }, '同步状态待确认'],
    [{ status: 'succeeded', query_error: true }, '同步状态待确认'],
  ])('keeps %j visible over an earlier synchronized snapshot', (progress, label) => {
    expect(getAgentConfigSync(agent() as any, 43, progress).label).toBe(label)
  })

  it('also respects progress supplied by the Agent list', () => {
    expect(getAgentConfigSync(agent({ latest_deployment: deployment({ status: 'rollback_failed' }) }) as any, 43).label)
      .toBe('同步状态待确认')
  })
})

describe('Agent card diagnostics', () => {
  it('keeps successful operation details collapsed while DNS warning reasons remain visible', async () => {
    const { wrapper } = await render(agent({
      latest_deployment: deployment({
        message: '配置已应用，服务检查通过',
        dns_checks: [{ name: 'DNS 劫持范围', level: 'warn', message: 'any:53 可能绕过 OxiDNS', fix: '核对路由与劫持规则' }],
      }),
      latest_upgrade: { update_id: 'upgrade-success', status: 'succeeded', target_version: '1.8.24-go', previous_version: '1.8.23-go', message: '身份与迁移检查通过' },
    }))
    const publication = wrapper.get('[data-agent-status-row="配置推送"]')
    const dns = wrapper.get('[data-agent-status-row="DNS 检查"]')
    const upgrade = wrapper.get('[data-agent-status-row="Agent 更新"]')
    expect(publication.get('[data-status-details]').isVisible()).toBe(false)
    expect(upgrade.get('[data-status-details]').isVisible()).toBe(false)
    expect(dns.get('[data-status-notice]').isVisible()).toBe(true)
    expect(dns.get('[data-status-notice]').text()).toContain('any:53 可能绕过 OxiDNS')
    expect(dns.get('[data-status-details]').isVisible()).toBe(false)
    await publication.get('button[aria-expanded]').trigger('click')
    expect(publication.get('[data-status-details]').isVisible()).toBe(true)
    expect(publication.get('[data-status-details]').text()).toContain('配置已应用，服务检查通过')
    expect(publication.find('details').exists()).toBe(false)
  })

  it('retains the OxiDNS panel entry when an Agent has not reported system metrics', async () => {
    const { wrapper } = await render(agent({ service_type: 'oxidns', system_metrics: undefined }))
    const monitor = wrapper.findAll('button').find(button => button.text() === 'OxiDNS 面板')!
    expect(monitor.isVisible()).toBe(true)
  })

  it.each(['mihomo', 'mosdns', 'oxidns'])('shows %s publication and rollback failure details in its card', async service_type => {
    const { wrapper } = await render(agent({ service_type, latest_deployment: deployment({ status: 'rollback_failed', error: '配置检查失败', error_details: ruleLocationDetails, rollback_error: '旧服务启动失败', rollback_error_details: 'permission denied' }) }))
    expect(wrapper.findAll('pre').map(item => item.element.textContent)).toContain(ruleLocationDetails)
    expect(wrapper.findAll('pre').map(item => item.text())).toContain('permission denied')
  })
})
