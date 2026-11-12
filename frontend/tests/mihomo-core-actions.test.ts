import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import MihomoCoreActions from '@/components/mihomo/MihomoCoreActions.vue'
import { agentApi, mihomoPanelApi } from '@/api'
import { confirm, confirmDanger, notify } from '@/lib/feedback'

vi.mock('@/api', () => ({
  agentApi: { restart: vi.fn() },
  mihomoPanelApi: {
    version: vi.fn(), reloadConfig: vi.fn(), checkCoreUpdate: vi.fn(), upgradeCore: vi.fn(), flushFakeIP: vi.fn(), flushDNS: vi.fn()
  }
}))
vi.mock('@/lib/feedback', () => ({
  confirm: vi.fn(), confirmDanger: vi.fn(),
  notify: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

type Actions = {
  reloadConfig: () => Promise<void>
  flush: (kind: 'fakeip' | 'dns') => Promise<void>
  checkUpdate: () => Promise<void>
  offerUpdate: (check: unknown) => Promise<void>
  upgradeCore: () => Promise<void>
  restartCore: () => Promise<void>
}
const render = (props: Record<string, unknown> = {}) => {
  const wrapper = mount(MihomoCoreActions, { props: { agentId: 'gw', ...props } })
  return { wrapper, actions: (wrapper.vm as any).$.setupState as Actions }
}

beforeEach(() => { vi.clearAllMocks(); vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers() })

describe('mihomo core actions', () => {
  it('reloads only after confirmation and refreshes the panel', async () => {
    const { wrapper, actions } = render()
    vi.mocked(confirm).mockResolvedValueOnce(false)
    await actions.reloadConfig()
    expect(mihomoPanelApi.reloadConfig).not.toHaveBeenCalled()
    vi.mocked(confirm).mockResolvedValueOnce(true)
    vi.mocked(mihomoPanelApi.reloadConfig).mockResolvedValue({} as any)
    await actions.reloadConfig()
    expect(mihomoPanelApi.reloadConfig).toHaveBeenCalledWith('gw')
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('flushes caches without confirmation', async () => {
    const { actions } = render()
    vi.mocked(mihomoPanelApi.flushFakeIP).mockResolvedValue({} as any)
    await actions.flush('fakeip')
    expect(mihomoPanelApi.flushFakeIP).toHaveBeenCalledWith('gw')
    expect(notify.success).toHaveBeenCalledWith('已清空 FakeIP 缓存')
  })

  it('restarts through the Agent and waits for the core to answer again', async () => {
    const { wrapper, actions } = render()
    vi.mocked(confirmDanger).mockResolvedValue(true)
    vi.mocked(agentApi.restart).mockResolvedValue({ data: { success: true } } as any)
    vi.mocked(mihomoPanelApi.version)
      .mockRejectedValueOnce(new Error('down'))
      .mockResolvedValue({ data: { data: { version: 'v1.19.32' } } } as any)
    const pending = actions.restartCore()
    await vi.advanceTimersByTimeAsync(5000)
    await pending
    await flushPromises()
    expect(agentApi.restart).toHaveBeenCalledWith('gw')
    expect(mihomoPanelApi.version).toHaveBeenCalledTimes(2)
    expect(notify.success).toHaveBeenCalledWith('内核已重启')
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  const check = (data: Record<string, unknown>) =>
    vi.mocked(mihomoPanelApi.checkCoreUpdate).mockResolvedValue({ data: { success: true, data: {
      current: 'v1.19.32', latest: '', channel: 'release', has_update: false, error: '', ...data } } } as any)

  it('reports an up-to-date core without offering an update', async () => {
    const { actions } = render()
    check({ latest: 'v1.19.32' })
    await actions.checkUpdate()
    expect(mihomoPanelApi.checkCoreUpdate).toHaveBeenCalledWith('gw', true)
    expect(confirm).not.toHaveBeenCalled()
    expect(mihomoPanelApi.upgradeCore).not.toHaveBeenCalled()
    expect(notify.success).toHaveBeenCalledWith('已是最新版本', '当前版本 v1.19.32')
  })

  it('checks automatically once the core version is known and marks a new version', async () => {
    check({ latest: 'v1.19.33', has_update: true })
    const { wrapper } = render({ coreVersion: '' })
    await flushPromises()
    expect(mihomoPanelApi.checkCoreUpdate).not.toHaveBeenCalled()
    await wrapper.setProps({ coreVersion: 'v1.19.32' })
    await flushPromises()
    expect(mihomoPanelApi.checkCoreUpdate).toHaveBeenCalledWith('gw')
    expect(wrapper.find('[data-testid="core-update-dot"]').exists()).toBe(true)
    expect(wrapper.get('button').attributes('aria-label')).toBe('内核操作：有新版本 v1.19.33')
    expect(notify.success).not.toHaveBeenCalled()
    expect(confirm).not.toHaveBeenCalled()

    check({ current: 'v1.19.33', latest: 'v1.19.33' })
    await wrapper.setProps({ coreVersion: 'v1.19.33' })
    await flushPromises()
    expect(wrapper.find('[data-testid="core-update-dot"]').exists()).toBe(false)
  })

  it('keeps quiet when the automatic check fails', async () => {
    vi.mocked(mihomoPanelApi.checkCoreUpdate).mockRejectedValue(new Error('down'))
    const { wrapper } = render({ coreVersion: 'v1.19.32' })
    await flushPromises()
    expect(wrapper.find('[data-testid="core-update-dot"]').exists()).toBe(false)
    expect(notify.error).not.toHaveBeenCalled()
  })

  it('ignores an automatic result from a device that is no longer selected', async () => {
    let answer!: (value: any) => void
    vi.mocked(mihomoPanelApi.checkCoreUpdate).mockReturnValueOnce(new Promise(resolve => { answer = resolve }))
    const { wrapper } = render({ coreVersion: 'v1.19.32' })
    check({ latest: 'v1.19.32' })
    await wrapper.setProps({ agentId: 'other' })
    await flushPromises()
    answer({ data: { data: { current: 'v1.19.32', latest: 'v1.19.33', channel: 'release', has_update: true, error: '' } } })
    await flushPromises()
    expect(wrapper.find('[data-testid="core-update-dot"]').exists()).toBe(false)
  })

  it('offers the update only when a newer version is found, and upgrades after confirmation', async () => {
    const { wrapper, actions } = render()
    check({ latest: 'v1.19.33', has_update: true })
    vi.mocked(confirm).mockResolvedValueOnce(false)
    await actions.checkUpdate()
    expect(vi.mocked(confirm).mock.calls[0][0]).toContain('发现新版本 v1.19.33，当前 v1.19.32')
    expect(vi.mocked(confirm).mock.calls[0][1]).toMatchObject({ title: '内核有更新', confirmText: '更新' })
    expect(mihomoPanelApi.upgradeCore).not.toHaveBeenCalled()

    vi.mocked(confirm).mockResolvedValueOnce(true)
    vi.mocked(mihomoPanelApi.upgradeCore).mockResolvedValue({} as any)
    vi.mocked(mihomoPanelApi.version).mockResolvedValue({ data: { data: { version: 'v1.19.33' } } } as any)
    const pending = actions.checkUpdate()
    await vi.advanceTimersByTimeAsync(2000)
    await pending
    expect(mihomoPanelApi.upgradeCore).toHaveBeenCalledWith('gw')
    expect(notify.success).toHaveBeenCalledWith('内核已更新', '当前版本 v1.19.33')
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('names the alpha channel', async () => {
    const { actions } = render()
    check({ current: 'alpha-0a1b2c3', latest: 'alpha-4d5e6f7', channel: 'alpha', has_update: true })
    vi.mocked(confirm).mockResolvedValueOnce(false)
    await actions.checkUpdate()
    expect(vi.mocked(confirm).mock.calls[0][0]).toContain('发现新版本 alpha-4d5e6f7（Alpha 通道），当前 alpha-0a1b2c3')
  })

  it('lets the core check by itself when the server cannot reach GitHub', async () => {
    const { actions } = render()
    check({ error: '无法获取最新版本（连接 GitHub 超时）' })
    vi.mocked(confirm).mockResolvedValueOnce(true)
    vi.mocked(mihomoPanelApi.upgradeCore).mockRejectedValue({ response: { data: { message: 'update error: already using latest version v1.19.32' } } })
    await actions.checkUpdate()
    expect(vi.mocked(confirm).mock.calls[0][0]).toContain('连接 GitHub 超时')
    expect(vi.mocked(confirm).mock.calls[0][1]).toMatchObject({ title: '无法检查更新', confirmText: '仍然更新' })
    expect(notify.success).toHaveBeenCalledWith('已是最新版本', '当前版本 v1.19.32')
    expect(notify.error).not.toHaveBeenCalled()
    // 刷新服务端缓存：手动检查 1 次 + 内核回复已是最新后 1 次
    expect(vi.mocked(mihomoPanelApi.checkCoreUpdate).mock.calls).toEqual([['gw', true], ['gw', true]])
  })

  it('reports a failed check without offering an update', async () => {
    const { actions } = render()
    vi.mocked(mihomoPanelApi.checkCoreUpdate).mockRejectedValue({ response: { data: { message: '暂时无法连接 Agent' } } })
    await actions.checkUpdate()
    expect(notify.error).toHaveBeenCalledWith('检查更新失败', '暂时无法连接 Agent')
    expect(confirm).not.toHaveBeenCalled()
  })

  it('reports an upgrade refused by the core', async () => {
    const { wrapper, actions } = render()
    vi.mocked(mihomoPanelApi.upgradeCore).mockRejectedValue({ response: { data: { message: 'download failed: dial tcp: i/o timeout' } } })
    await actions.upgradeCore()
    expect(notify.error).toHaveBeenCalledWith('更新内核失败', 'download failed: dial tcp: i/o timeout')
    expect(wrapper.emitted('changed')).toBeUndefined()
  })

  it('distinguishes an in-flight core update request from waiting for the core to return', async () => {
    const { wrapper, actions } = render()
    let accept!: (value: any) => void
    vi.mocked(mihomoPanelApi.upgradeCore).mockReturnValue(new Promise(resolve => { accept = resolve }))
    vi.mocked(mihomoPanelApi.version).mockResolvedValue({ data: { data: { version: 'v1.19.32' } } } as any)
    const pending = actions.upgradeCore()
    await flushPromises()
    expect(wrapper.text()).toContain('请求 Mihomo 更新')
    expect(wrapper.get('button').attributes('aria-label')).toBe('请求 Mihomo 更新…')
    expect(wrapper.get('button').attributes('aria-busy')).toBe('true')
    expect(wrapper.emitted('status-change')?.at(-1)).toEqual(['请求 Mihomo 更新…'])
    expect(mihomoPanelApi.version).not.toHaveBeenCalled()
    accept({})
    await flushPromises()
    expect(wrapper.text()).toContain('等待 Mihomo 重新上线')
    expect(wrapper.emitted('status-change')?.at(-1)).toEqual(['等待 Mihomo 重新上线…'])
    expect(notify.success).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2000)
    await pending
    await flushPromises()
    expect(wrapper.emitted('status-change')?.at(-1)).toEqual([''])
    expect(wrapper.get('button').attributes('aria-busy')).toBe('false')
    expect(notify.success).toHaveBeenCalledWith('内核已更新', '当前版本 v1.19.32')
    wrapper.unmount()
  })
})
