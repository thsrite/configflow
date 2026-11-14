import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import api from '@/api'
import { notify } from '@/lib/feedback'
import AgentRuleSync from '@/components/agents/AgentRuleSync.vue'
import type { AgentRuleSyncResponse } from '@/api/agentRuleSync'

vi.mock('@/api', () => ({ default: { get: vi.fn(), put: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn() } }))
const wrappers: VueWrapper[] = []
const data = (extra: Partial<AgentRuleSyncResponse> = {}): AgentRuleSyncResponse => ({
  success: true, settings: { library: false, rules: false }, state: { status: 'disabled', message: '' }, baseline_ready: true, ...extra,
})
const response = (value = data()) => ({ data: value } as any)
const enabled = (extra: Partial<AgentRuleSyncResponse> = {}) => data({ settings: { library: true, rules: false }, state: { status: 'idle', message: '' }, ...extra })
function deferred() {
  let resolve!: (value: any) => void
  let reject!: (value: any) => void
  return { promise: new Promise<any>((yes, no) => { resolve = yes; reject = no }), resolve: (value: any) => resolve(value), reject: (value: any) => reject(value) }
}
const toggle = (wrapper: VueWrapper, label: string) => wrapper.get(`[role="switch"][aria-label="${label}"]`)
async function render(agentId = 'one') {
  const wrapper = mount(AgentRuleSync, { props: { agentId }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.resetAllMocks()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.mocked(api.get).mockResolvedValue(response())
  vi.mocked(api.put).mockImplementation(async (_path, settings) => response(enabled({ settings })))
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = ''; vi.unstubAllGlobals(); vi.useRealTimers() })

describe('per-Agent automatic rule synchronization controls', () => {
  it('shows separate disabled-by-default switches without polling disabled synchronization', async () => {
    const wrapper = await render()
    expect(toggle(wrapper, '规则库自动同步').attributes('aria-checked')).toBe('false')
    expect(toggle(wrapper, '单条规则自动同步').attributes('aria-checked')).toBe('false')
    expect(wrapper.text()).not.toContain('仅新增未引用的规则库条目不会触发同步')
    await vi.advanceTimersByTimeAsync(30000)
    expect(api.get).toHaveBeenCalledTimes(1)
    expect(api.put).not.toHaveBeenCalled()
  })

  it.each([
    ['规则库自动同步', '同步已启用引用的规则集内容及新增引用，只下载有变化的规则文件。仅新增未引用的规则库条目不会触发同步。'],
    ['单条规则自动同步', '同步已选内嵌规则。'],
  ])('shows %s help without toggling either switch or saving settings', async (label, help) => {
    const wrapper = await render()
    const button = wrapper.get(`button[aria-label="${label}说明"]`)
    expect(button.element.closest('label')).toBeNull()
    expect(document.querySelector('[data-field-help-content]')).toBeNull()
    await button.trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-field-help-content]')?.textContent).toContain(help)
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(toggle(wrapper, '规则库自动同步').attributes('aria-checked')).toBe('false')
    expect(toggle(wrapper, '单条规则自动同步').attributes('aria-checked')).toBe('false')
    expect(api.put).not.toHaveBeenCalled()
    await button.trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-field-help-content]')).toBeNull()
  })

  it('saves each switch immediately while retaining the other switch and using the encoded Agent ID', async () => {
    const wrapper = await render('host/one')
    await toggle(wrapper, '规则库自动同步').trigger('click')
    await flushPromises()
    expect(api.put).toHaveBeenLastCalledWith('/agents/host%2Fone/rule-sync', { library: true, rules: false }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(toggle(wrapper, '规则库自动同步').attributes('aria-checked')).toBe('true')
    await toggle(wrapper, '单条规则自动同步').trigger('click')
    await flushPromises()
    expect(api.put).toHaveBeenLastCalledWith('/agents/host%2Fone/rule-sync', { library: true, rules: true }, expect.anything())
    expect(notify.success).toHaveBeenCalledWith('自动同步设置已保存')
    expect(wrapper.text()).not.toContain('规则同步完成')
  })

  it('disables both switches during a save to prevent lost changes', async () => {
    const pending = deferred()
    const wrapper = await render()
    vi.mocked(api.put).mockReturnValueOnce(pending.promise)
    await toggle(wrapper, '规则库自动同步').trigger('click')
    expect(wrapper.text()).toContain('正在保存')
    expect(toggle(wrapper, '单条规则自动同步').attributes('disabled')).toBeDefined()
    await toggle(wrapper, '单条规则自动同步').trigger('click')
    expect(api.put).toHaveBeenCalledTimes(1)
    pending.resolve(response(enabled()))
    await flushPromises()
    expect(toggle(wrapper, '单条规则自动同步').attributes('disabled')).toBeUndefined()
  })

  it('preserves the saved values and exposes the backend reason if a save fails', async () => {
    vi.mocked(api.get).mockResolvedValue(response(enabled()))
    const wrapper = await render()
    vi.mocked(api.put).mockRejectedValueOnce({ response: { data: { message: '设置写入失败，请重试。' } } })
    await toggle(wrapper, '单条规则自动同步').trigger('click')
    await flushPromises()
    expect(toggle(wrapper, '规则库自动同步').attributes('aria-checked')).toBe('true')
    expect(toggle(wrapper, '单条规则自动同步').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[role="alert"]').text()).toContain('设置写入失败，请重试。')
    expect(notify.success).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.get('[role="alert"]').text()).toContain('设置写入失败')
    await toggle(wrapper, '单条规则自动同步').trigger('click')
    await flushPromises()
    expect(toggle(wrapper, '单条规则自动同步').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('allows enabling without a baseline and shows the required first publication', async () => {
    vi.mocked(api.get).mockResolvedValue(response(data({ baseline_ready: false })))
    const wrapper = await render()
    expect(wrapper.text()).toContain('先成功发布一次配置，建立自动同步基线')
    expect(toggle(wrapper, '规则库自动同步').attributes('disabled')).toBeUndefined()
    vi.mocked(api.put).mockResolvedValueOnce(response(enabled({ baseline_ready: false, state: { status: 'blocked', message: '等待建立基线。' } })))
    await toggle(wrapper, '规则库自动同步').trigger('click')
    await flushPromises()
    expect(toggle(wrapper, '规则库自动同步').attributes('aria-checked')).toBe('true')
    expect(wrapper.text()).toContain('自动同步等待处理')
    expect(wrapper.text()).toContain('先成功发布一次配置')
  })

  it('polls enabled synchronization and shows pending, running, success, and failure outcomes', async () => {
    vi.mocked(api.get).mockResolvedValueOnce(response(enabled({ state: { status: 'pending', message: '等待设备上线。' } })))
    const wrapper = await render()
    expect(wrapper.text()).toContain('等待同步 · 等待设备上线。')
    vi.mocked(api.get).mockResolvedValueOnce(response(enabled({ state: { status: 'running', message: '正在热重载。' } })))
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.text()).toContain('正在同步规则 · 正在热重载。')
    expect(toggle(wrapper, '规则库自动同步').attributes('disabled')).toBeUndefined()
    vi.mocked(api.get).mockResolvedValueOnce(response(enabled({ state: { status: 'succeeded', message: '同步 2 个规则集。' } })))
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.get('[role="status"]').classes()).toContain('text-success-accent')
    expect(wrapper.text()).toContain('规则同步完成 · 同步 2 个规则集。')
    vi.mocked(api.get).mockResolvedValueOnce(response(enabled({ state: { status: 'failed', message: '热重载失败，请查看日志。' } })))
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.get('[role="status"]').classes()).toContain('text-destructive-accent')
    expect(wrapper.text()).toContain('规则同步失败 · 热重载失败')
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('keeps the current settings on a polling error and offers a read retry', async () => {
    vi.mocked(api.get).mockResolvedValueOnce(response(enabled()))
    const wrapper = await render()
    vi.mocked(api.get).mockRejectedValueOnce({ response: { data: { message: '状态暂不可用。' } } })
    await vi.advanceTimersByTimeAsync(10000)
    expect(toggle(wrapper, '规则库自动同步').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[role="alert"]').text()).toContain('状态暂不可用')
    vi.mocked(api.get).mockResolvedValueOnce(response(enabled()))
    await wrapper.findAll('button').find(button => button.text() === '重新读取')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(api.put).not.toHaveBeenCalled()
  })

  it('does not let a stale poll overwrite newly saved settings', async () => {
    vi.mocked(api.get).mockResolvedValueOnce(response(enabled()))
    const wrapper = await render()
    const old = deferred()
    vi.mocked(api.get).mockReturnValueOnce(old.promise)
    await vi.advanceTimersByTimeAsync(10000)
    const signal = vi.mocked(api.get).mock.calls[1][1]?.signal as AbortSignal
    await toggle(wrapper, '单条规则自动同步').trigger('click')
    await flushPromises()
    expect(signal.aborted).toBe(true)
    old.resolve(response(enabled()))
    await flushPromises()
    expect(toggle(wrapper, '单条规则自动同步').attributes('aria-checked')).toBe('true')
  })

  it('resets values and ignores a stale read after switching Agents', async () => {
    const old = deferred()
    vi.mocked(api.get).mockReturnValueOnce(old.promise)
    const wrapper = await render()
    const signal = vi.mocked(api.get).mock.calls[0][1]?.signal as AbortSignal
    await wrapper.setProps({ agentId: 'two' })
    await flushPromises()
    old.resolve(response(enabled()))
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(toggle(wrapper, '规则库自动同步').attributes('aria-checked')).toBe('false')
    await toggle(wrapper, '单条规则自动同步').trigger('click')
    expect(api.put).toHaveBeenLastCalledWith('/agents/two/rule-sync', { library: false, rules: true }, expect.anything())
  })

  it('ignores a save completed after switching Agents', async () => {
    const wrapper = await render()
    const old = deferred()
    vi.mocked(api.put).mockReturnValueOnce(old.promise)
    await toggle(wrapper, '规则库自动同步').trigger('click')
    const signal = vi.mocked(api.put).mock.calls[0][2]?.signal as AbortSignal
    await wrapper.setProps({ agentId: 'two' })
    await flushPromises()
    old.resolve(response(enabled()))
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(toggle(wrapper, '规则库自动同步').attributes('aria-checked')).toBe('false')
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('aborts requests and removes pending polling when unmounted', async () => {
    vi.mocked(api.get).mockResolvedValueOnce(response(enabled()))
    const wrapper = await render()
    const pending = deferred()
    vi.mocked(api.get).mockReturnValueOnce(pending.promise)
    await vi.advanceTimersByTimeAsync(10000)
    const signal = vi.mocked(api.get).mock.calls[1][1]?.signal as AbortSignal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    pending.resolve(response(enabled()))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30000)
    expect(api.get).toHaveBeenCalledTimes(2)
  })

  it('requires a successful settings read before enabling either switch', async () => {
    vi.mocked(api.get).mockRejectedValueOnce({ response: { data: { message: '请更新主服务后使用自动同步设置。' } } })
    const wrapper = await render()
    expect(wrapper.get('[role="alert"]').text()).toContain('请更新主服务')
    expect(toggle(wrapper, '规则库自动同步').attributes('disabled')).toBeDefined()
    expect(toggle(wrapper, '单条规则自动同步').attributes('disabled')).toBeDefined()
    await toggle(wrapper, '规则库自动同步').trigger('click')
    expect(api.put).not.toHaveBeenCalled()
  })

  it('gives the current minimum Agent version when the server requires an upgrade', async () => {
    vi.mocked(api.get).mockRejectedValueOnce({ response: { data: { code: 'UPGRADE_REQUIRED' } } })
    const wrapper = await render()
    expect(wrapper.get('[role="alert"]').text()).toContain('1.8.30-go')
    expect(api.put).not.toHaveBeenCalled()
  })

  it('refreshes the other member of a high-availability pair when one member saves the shared switches', async () => {
    const first = mount(AgentRuleSync, { props: { agentId: 'one', highAvailability: true }, attachTo: document.body })
    const second = mount(AgentRuleSync, { props: { agentId: 'two', highAvailability: true }, attachTo: document.body })
    const single = mount(AgentRuleSync, { props: { agentId: 'three' }, attachTo: document.body })
    wrappers.push(first, second, single)
    await flushPromises()
    expect(toggle(second, '规则库自动同步').attributes('aria-checked')).toBe('false')
    vi.mocked(api.get).mockClear()
    vi.mocked(api.get).mockResolvedValue(response(enabled()))
    await toggle(first, '规则库自动同步').trigger('click')
    await flushPromises()
    // Only the peer rereads; the saving card already has the answer and a single device is not part of the pair.
    expect(vi.mocked(api.get).mock.calls.map(call => call[0])).toEqual(['/agents/two/rule-sync'])
    expect(toggle(second, '规则库自动同步').attributes('aria-checked')).toBe('true')
    expect(toggle(single, '规则库自动同步').attributes('aria-checked')).toBe('false')
    // An unmounted card no longer listens.
    second.unmount()
    vi.mocked(api.get).mockClear()
    await toggle(first, '规则库自动同步').trigger('click')
    await flushPromises()
    expect(api.get).not.toHaveBeenCalled()
  })
})
