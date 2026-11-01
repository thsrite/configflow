import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import Routers from '@/views/Routers.vue'
import { agentApi, profileApi, routerApi, ruleApi } from '@/api'
import { activeProfileId, scopedRequests } from '@/profileContext'
import { notify } from '@/lib/feedback'

vi.mock('@/api', () => ({
  agentApi: { getAll: vi.fn() },
  profileApi: { list: vi.fn() },
  routerApi: {
    list: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), probe: vi.fn(), report: vi.fn(), plan: vi.fn(), apply: vi.fn(),
    routes: vi.fn(), routePlan: vi.fn(), routeApply: vi.fn(), policy: vi.fn(), gatewayForwarding: vi.fn(),
  },
  ruleApi: { getAll: vi.fn() },
}))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() }, confirmDanger: vi.fn(), confirm: vi.fn(), confirmState: { resolve: null }, settleConfirm: vi.fn() }))

const ROUTER = {
  id: 'router_1', name: '主路由', type: 'routeros', endpoint: 'http://192.0.2.1', username: 'admin', verify_tls: true,
  proxy_gateway_agent_id: 'gw', manage_return_routing: false, dns_agent_id: '', has_password: true,
}
const REPORT = {
  in_sync: false, summary: { missing: 1 }, desired: { fake_ip_range: '198.18.0.0/16', proxy_gateway: '192.0.2.4', proxy_cidrs: [], warnings: [] },
  routes: { items: [{ cidr: '198.18.0.0/16', sources: ['fake-ip'], status: 'missing', detail: '' }], extra: [], unmanaged_other: [] },
  nat_bypass: { items: [], disabled_rules: [] }, return_routing: { status: 'disabled', message: '未启用自动配置回流。' },
}
const PLAN = { stages: [{ key: 'routes', title: '主路由代理路由', ops: [{ kind: 'route_add', summary: '添加 198.18.0.0/16' }] }],
  blockers: [], warnings: [], fingerprint: 'f1', empty: false }
const wrappers: VueWrapper[] = []

async function open(path: string) {
  const navigation = createRouter({ history: createMemoryHistory(), routes: [{ path: '/routers', component: { template: '<div />' } }] })
  await navigation.push(path)
  await navigation.isReady()
  const wrapper = mount(Routers, { attachTo: document.body, global: { plugins: [navigation] } })
  wrappers.push(wrapper)
  await flushPromises()
  await flushPromises()
  return { wrapper, navigation }
}

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  activeProfileId.value = 'default'
  vi.mocked(routerApi.list).mockResolvedValue({ data: { success: true, routers: [ROUTER] } } as any)
  vi.mocked(agentApi.getAll).mockResolvedValue({ data: [] } as any)
  vi.mocked(ruleApi.getAll).mockResolvedValue({ data: [] } as any)
  vi.mocked(routerApi.gatewayForwarding).mockResolvedValue({ data: { success: true, gateways: [] } } as any)
  vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, report: REPORT, generated_at: '2026-10-31T10:00:00' } } as any)
  vi.mocked(routerApi.plan).mockResolvedValue({ data: { success: true, plan: PLAN, generated_at: '2026-10-31T10:00:01' } } as any)
  vi.mocked(profileApi.list).mockResolvedValue({ data: [{ id: 'default', name: '默认配置', is_default: true }, { id: 'shop', name: '分店' }] } as any)
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  activeProfileId.value = 'default'
  scopedRequests.value = 0
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

describe('router page deep link', () => {
  it('locates the exact firewall section without publishing or opening a release dialog', async () => {
    vi.mocked(routerApi.report).mockResolvedValue({ data: { success: true, report: {
      ...REPORT, routes: { ...REPORT.routes, forward_hint: { status: 'drop', warning: '需要放行同口转发', fix_command: 'example command' } }
    } } } as any)
    const { wrapper } = await open('/routers?router=router_1&profile=default&section=ipv4-forward-hint')
    expect(wrapper.get('[data-router-ipv4-forward-hint]').attributes('data-notification-focus')).toBe('')
    expect(routerApi.plan).not.toHaveBeenCalled()
    expect(routerApi.apply).not.toHaveBeenCalled()
  })
  it('opens the router report and its release preview, then drops the link from the address', async () => {
    const { wrapper, navigation } = await open('/routers?router=router_1&profile=default&preview=1&tab=x')
    expect(routerApi.report).toHaveBeenCalledExactlyOnceWith('router_1')
    expect(routerApi.plan).toHaveBeenCalledExactlyOnceWith('router_1')
    expect(wrapper.get('[data-router-report]').text()).toContain('差异报告 · 主路由')
    expect(document.body.textContent).toContain('发布到 主路由')
    expect(document.body.textContent).toContain('添加 198.18.0.0/16')
    expect(routerApi.apply).not.toHaveBeenCalled()
    expect(navigation.currentRoute.value.query).toEqual({ tab: 'x' })
    expect(profileApi.list).not.toHaveBeenCalled()
  })

  it('opens only the report without preview=1', async () => {
    await open('/routers?router=router_1')
    expect(routerApi.report).toHaveBeenCalledTimes(1)
    expect(routerApi.plan).not.toHaveBeenCalled()
    expect(document.body.textContent).not.toContain('发布到 主路由')
  })

  it('switches to the router profile first and says so', async () => {
    const { navigation } = await open('/routers?router=router_1&profile=shop&preview=1')
    expect(activeProfileId.value).toBe('shop')
    expect(notify.info).toHaveBeenCalledWith('已切换到配置空间「分店」，查看链接中的路由器。')
    expect(routerApi.list).toHaveBeenCalledTimes(1)
    expect(routerApi.report).toHaveBeenCalledWith('router_1')
    expect(routerApi.plan).toHaveBeenCalledWith('router_1')
    expect(navigation.currentRoute.value.query).toEqual({})
  })

  it('keeps the current profile when the linked one is gone or busy, and opens nothing', async () => {
    await open('/routers?router=router_1&profile=removed&preview=1')
    expect(activeProfileId.value).toBe('default')
    expect(notify.error).toHaveBeenCalledWith('链接中的配置空间不存在或已删除，下面是当前配置空间的路由器。')
    expect(routerApi.list).toHaveBeenCalledTimes(1)
    expect(routerApi.report).not.toHaveBeenCalled()
    scopedRequests.value = 1
    await open('/routers?router=router_1&profile=shop')
    expect(activeProfileId.value).toBe('default')
    expect(notify.warning).toHaveBeenCalledWith('这台路由器属于配置空间「分店」。当前配置正在保存，完成后请切换到该空间再查看。')
    expect(routerApi.report).not.toHaveBeenCalled()
  })

  it('explains a router that no longer exists', async () => {
    const { navigation } = await open('/routers?router=gone&preview=1')
    expect(notify.error).toHaveBeenCalledWith('链接中的路由器不存在或已删除，请从提醒处重新打开。')
    expect(routerApi.report).not.toHaveBeenCalled()
    expect(navigation.currentRoute.value.query).toEqual({})
  })

  it('follows a new link while the page stays open', async () => {
    const { navigation } = await open('/routers')
    expect(routerApi.report).not.toHaveBeenCalled()
    await navigation.push('/routers?router=router_1&preview=1')
    await flushPromises()
    await flushPromises()
    expect(routerApi.report).toHaveBeenCalledTimes(1)
    expect(routerApi.plan).toHaveBeenCalledTimes(1)
  })
})
