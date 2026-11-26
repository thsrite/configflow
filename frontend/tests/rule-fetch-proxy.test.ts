import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import SystemSettings from '@/views/SystemSettings.vue'
import { ruleFetchProxyApi, serverDomainApi, configTokenApi, tasksApi } from '@/api'
import { canReloadPage } from '@/lib/pageReloadGuard'
import { notify } from '@/lib/feedback'

vi.mock('@/api', () => ({
  default: { get: vi.fn(async () => ({ data: { enabled: false } })) },
  configApi: {},
  configTokenApi: { get: vi.fn(async () => ({ data: { config_token: '' } })), update: vi.fn(), delete: vi.fn() },
  serverDomainApi: { get: vi.fn(async () => ({ data: { server_domain: 'http://configflow.test' } })), update: vi.fn() },
  ruleFetchProxyApi: { get: vi.fn(), update: vi.fn() },
  dependenciesApi: { list: vi.fn(async () => ({ data: { dependencies: [] } })), update: vi.fn() },
  tasksApi: { list: vi.fn(async () => ({ data: { success: true, tasks: [] } })), run: vi.fn(), setInterval: vi.fn() },
  routerApi: {}
}))

const proxy = 'http://proxy-user:proxy-secret@192.0.2.3:7890'
const wrappers: ReturnType<typeof mount>[] = []
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(ruleFetchProxyApi.get).mockResolvedValue({ data: { rule_fetch_proxy: proxy } } as any)
  vi.mocked(ruleFetchProxyApi.update).mockImplementation(async data => ({ data: { success: true, ...data } }) as any)
  vi.mocked(serverDomainApi.update).mockResolvedValue({ data: { success: true } } as any)
  vi.mocked(configTokenApi.update).mockResolvedValue({ data: { success: true } } as any)
  vi.spyOn(notify, 'success').mockImplementation(() => 0)
  vi.spyOn(notify, 'error').mockImplementation(() => 0)
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
})
async function render(path = '/system-settings') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/system-settings', component: SystemSettings }, { path: '/setup', component: { render: () => null } }] })
  await router.push(path)
  const wrapper = mount(SystemSettings, { attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return Object.assign(wrapper, { router })
}

describe('system settings tabs', () => {
  it('opens scheduled tasks from ?tab=tasks, counts failures on the tab and returns to general settings', async () => {
    vi.mocked(tasksApi.list).mockResolvedValue({ data: { success: true, tasks: [
      { key: 'subscription:s', category: 'subscription', name: 'Feed', enabled: true, running: false, last_status: 'failed',
        last_error: '上游返回 HTTP 503', cache_retained: true, interval_seconds: 86400, run_count: 1, failure_count: 1 },
    ] } } as any)
    const wrapper = await render('/system-settings?tab=tasks')
    expect(wrapper.find('[data-scheduled-tasks]').exists()).toBe(true)
    expect(wrapper.find('#rule-fetch-proxy').exists()).toBe(false)
    expect(wrapper.get('[aria-label="1 个任务失败"]').text()).toBe('1')
    const general = wrapper.findAll('[role="tab"]').find(tab => tab.text() === '常规')!
    await general.trigger('mousedown', { button: 0 })
    await flushPromises()
    expect(wrapper.router.currentRoute.value.query.tab).toBeUndefined()
    // 定时任务切走后保持挂载、只是隐藏；切回不重新读取
    expect(wrapper.get('[data-scheduled-tasks]').element.closest('[role="tabpanel"]')!.getAttribute('data-state')).toBe('inactive')
    expect(wrapper.find('#rule-fetch-proxy').exists()).toBe(true)
    const reads = vi.mocked(tasksApi.list).mock.calls.length
    const tasks = wrapper.findAll('[role="tab"]').find(tab => tab.text().startsWith('定时任务'))!
    await tasks.trigger('mousedown', { button: 0 })
    await flushPromises()
    expect(wrapper.router.currentRoute.value.query.tab).toBe('tasks')
    expect(wrapper.get('[data-scheduled-tasks]').element.closest('[role="tabpanel"]')!.getAttribute('data-state')).toBe('active')
    expect(tasksApi.list).toHaveBeenCalledTimes(reads)
  })
})

describe('rule download proxy setting', () => {
  it('loads the saved proxy masked and saves changed values on blur without displaying credentials', async () => {
    const wrapper = await render()
    const input = wrapper.get<HTMLInputElement>('#rule-fetch-proxy')
    expect(input.element.value).toBe(proxy)
    expect(input.attributes('type')).toBe('password')
    await input.trigger('blur')
    expect(ruleFetchProxyApi.update).not.toHaveBeenCalled()
    await input.setValue(' http://192.0.2.3:7890 ')
    await input.trigger('blur')
    await flushPromises()
    expect(ruleFetchProxyApi.update).toHaveBeenCalledWith({ rule_fetch_proxy: 'http://192.0.2.3:7890' })
    expect(input.element.value).toBe('http://192.0.2.3:7890')
    expect(notify.success).toHaveBeenCalledWith('规则下载代理已保存')
  })

  it('persists an empty setting so later downloads use the existing network', async () => {
    const wrapper = await render()
    const input = wrapper.get('#rule-fetch-proxy')
    await input.setValue('')
    await input.trigger('blur')
    await flushPromises()
    expect(ruleFetchProxyApi.update).toHaveBeenCalledWith({ rule_fetch_proxy: '' })
    expect(notify.success).toHaveBeenCalledWith('规则下载代理已清除，将使用现有网络')
  })

  it('reports invalid input without logging or showing credentials and permits retry', async () => {
    const wrapper = await render()
    const input = wrapper.get('#rule-fetch-proxy')
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    vi.mocked(ruleFetchProxyApi.update).mockRejectedValueOnce({
      isAxiosError: true,
      config: { data: { rule_fetch_proxy: proxy } },
      response: { status: 400, data: { message: `Rejected ${proxy}` } }
    })
    await input.setValue(`${proxy}/invalid`)
    await input.trigger('blur')
    await flushPromises()
    expect(notify.error).toHaveBeenCalledWith('代理地址无效，请填写 HTTP/HTTPS 代理地址或留空')
    expect(notify.success).not.toHaveBeenCalled()
    expect(consoleError).not.toHaveBeenCalled()
    await input.setValue('http://192.0.2.3:7890')
    await input.trigger('blur')
    await flushPromises()
    expect(notify.success).toHaveBeenCalledWith('规则下载代理已保存')
  })
})


describe('inline system settings automatic reload protection', () => {
  it.each([
    ['server-domain', 'https://new-configflow.test', serverDomainApi.update],
    ['config-token', 'new-test-token', configTokenApi.update],
    ['rule-fetch-proxy', 'http://proxy.test:8080', ruleFetchProxyApi.update],
  ] as const)('protects %s until blur saving succeeds', async (id, value, save) => {
    const wrapper = await render()
    expect(canReloadPage()).toBe(true)
    const input = wrapper.get<HTMLInputElement>(`#${id}`)
    await input.setValue(value)
    expect(canReloadPage()).toBe(false)
    expect(save).not.toHaveBeenCalled()
    await input.trigger('blur')
    await flushPromises()
    expect(save).toHaveBeenCalledOnce()
    expect(input.element.value).toBe(value)
    expect(canReloadPage()).toBe(true)
  })

  it('keeps failed saves protected and still permits the normal retry', async () => {
    vi.mocked(ruleFetchProxyApi.update).mockRejectedValueOnce(new Error('offline'))
    const wrapper = await render()
    await wrapper.get('#rule-fetch-proxy').setValue('http://retry.test:8080')
    await wrapper.get('#rule-fetch-proxy').trigger('blur')
    await flushPromises()
    expect(canReloadPage()).toBe(false)
    await wrapper.get('#rule-fetch-proxy').trigger('blur')
    await flushPromises()
    expect(canReloadPage()).toBe(true)
  })

  it('does not mistake edits made during an earlier save for saved content', async () => {
    let resolve!: (value: any) => void
    vi.mocked(serverDomainApi.update).mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const wrapper = await render()
    const input = wrapper.get<HTMLInputElement>('#server-domain')
    await input.setValue('https://submitted.test')
    await input.trigger('blur')
    await input.setValue('https://still-editing.test')
    expect(canReloadPage()).toBe(false)
    resolve({ data: { success: true } })
    await flushPromises()
    expect(input.element.value).toBe('https://still-editing.test')
    expect(canReloadPage()).toBe(false)
    await input.trigger('blur')
    await flushPromises()
    expect(canReloadPage()).toBe(true)
  })

  it('blocks while an unchanged field save is pending and releases on response', async () => {
    let resolve!: (value: any) => void
    vi.mocked(serverDomainApi.update).mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const wrapper = await render()
    await wrapper.get('#server-domain').trigger('blur')
    expect(canReloadPage()).toBe(false)
    resolve({ data: { success: true } })
    await flushPromises()
    expect(canReloadPage()).toBe(true)
  })
})
