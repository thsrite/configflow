import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import Agents from '@/views/Agents.vue'
import { agentApi, profileApi } from '@/api'
import { setActiveProfileId } from '@/profileContext'

// Keep the actual Agent cards, stores and deployment state; replace only HTTP.
vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  agentApi: { getAll: vi.fn(), getDeployment: vi.fn(), getUpgrade: vi.fn() },
  profileApi: { list: vi.fn() }
}))

const wrappers: ReturnType<typeof mount>[] = []
const mediaDescriptor = Object.getOwnPropertyDescriptor(window, 'matchMedia')
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
const canvasDescriptor = Object.getOwnPropertyDescriptor(HTMLCanvasElement.prototype, 'getContext')

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: (query: string) => ({
    matches: query.includes('prefers-reduced-motion'), media: query,
    addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn()
  }) })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  // jsdom has no canvas drawing surface; the real topology still renders its DOM.
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
    enabled: true, latest_deployment: deployment(), ...overrides
  }
}

async function render(item = agent(), profiles: Record<string, unknown>[] = [{ id: 'default', name: 'Default', revision: 43 }]) {
  vi.mocked(agentApi.getAll).mockResolvedValue({ data: [item] } as any)
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

describe('Agent configuration status wording', () => {
  it('shows a readable synchronized label and last-push result, with long revisions only in the tooltip', async () => {
    const revision = 1234567890123
    const { wrapper, footer, title } = await render(agent({
      config_revision: revision, latest_deployment: deployment({ config_revision: revision })
    }), [{ id: 'default', name: 'Default', revision }])
    expect(footer.text()).toContain('已同步')
    expect(footer.text()).not.toContain(String(revision))
    expect(wrapper.text()).not.toContain(String(revision))
    expect(title()).toContain(`已推送修订号：v${revision}`)
    expect(title()).toContain(`当前配置修订号：v${revision}`)
    expect(title()).toContain('hash-current')
    expect(wrapper.text()).toContain('上次配置推送：成功')
    expect(wrapper.text()).not.toContain('发布成功')
  })

  it('shows pending changes while retaining successful historical push wording', async () => {
    const { wrapper, footer, title } = await render(agent(), [{ id: 'default', name: 'Default', revision: 50 }])
    expect(footer.text()).toContain('有变更，待推送')
    expect(footer.text()).not.toContain('已同步')
    expect(title()).toContain('已推送修订号：v43')
    expect(title()).toContain('当前配置修订号：v50')
    expect(wrapper.text()).toContain('上次配置推送：成功')
  })

  it('treats a confirmed revision zero as a valid synchronized revision', async () => {
    const { footer } = await render(agent({ config_revision: 0, latest_deployment: deployment({ config_revision: 0 }) }), [
      { id: 'default', name: 'Default', revision: 0 }
    ])
    expect(footer.text()).toContain('已同步')
    expect(footer.text()).not.toContain('尚未推送')
  })

  it('does not equate the same revision number across two bound profiles', async () => {
    const { footer } = await render(agent({ profile_id: 'second' }), [
      { id: 'default', name: 'Default', revision: 43 }, { id: 'second', name: 'Second', revision: 43 }
    ])
    expect(footer.text()).toContain('绑定已变更，待推送')
    expect(footer.text()).not.toContain('已同步')
  })

  it('keeps the previous successful profile after a failed push to the new binding', async () => {
    const success = deployment()
    const failed = deployment({ deployment_id: 'deployment-failed', status: 'failed', profile_id: 'second', updated_at: '2026-10-10T13:00:00Z' })
    const { footer } = await render(agent({
      profile_id: 'second', latest_deployment: failed,
      deployments: { [success.deployment_id]: success, [failed.deployment_id]: failed }
    }), [
      { id: 'default', name: 'Default', revision: 43 }, { id: 'second', name: 'Second', revision: 43 }
    ])
    expect(footer.text()).toContain('绑定已变更，待推送')
    expect(footer.text()).not.toContain('已同步')
  })

  it.each([
    ['missing profile metadata', { profile_id: 'missing' }, [{ id: 'default', name: 'Default', revision: 43 }]],
    ['legacy hash without revision', { config_revision: undefined, latest_deployment: undefined }, [{ id: 'default', name: 'Default', revision: 43 }]],
    ['missing current revision', {}, [{ id: 'default', name: 'Default' }]]
  ])('keeps synchronization unconfirmed for %s', async (_name, overrides, profiles) => {
    const { footer } = await render(agent(overrides as Record<string, unknown>), profiles as Record<string, unknown>[])
    expect(footer.text()).toContain('同步状态待确认')
    expect(footer.text()).not.toContain('已同步')
  })

  it('shows never pushed when there is no hash, revision or successful deployment', async () => {
    const { footer } = await render(agent({ config_version: '0', config_revision: undefined, latest_deployment: undefined }))
    expect(footer.text()).toContain('尚未推送')
    expect(footer.text()).not.toContain('已同步')
  })

  it('does not use an older matching success when the latest success conflicts with the Agent hash', async () => {
    const older = deployment({ updated_at: '2026-10-10T11:00:00Z' })
    const newer = deployment({ deployment_id: 'new-success', config_version: 'different-hash', updated_at: '2026-10-10T13:00:00Z' })
    const { footer } = await render(agent({
      latest_deployment: newer, deployments: { newer, older }
    }))
    expect(footer.text()).toContain('同步状态待确认')
    expect(footer.text()).not.toContain('已同步')
  })
})
