import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { routerKey } from 'vue-router'
import LinuxGatewaySettings from '@/components/generate/LinuxGatewaySettings.vue'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import SettingsWorkspace from '@/components/generate/SettingsWorkspace.vue'
import SettingsPublishBar from '@/components/generate/SettingsPublishBar.vue'
import { Switch } from '@/components/ui/switch'
import { Checkbox } from '@/components/ui/checkbox'
import { Select } from '@/components/ui/select'
import { agentApi, profileApi } from '@/api'
import { linuxGatewayApi } from '@/api/linuxGateway'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { gatewaySpecErrors } from '@/lib/linuxGateway'
import FieldHelp from '@/components/common/FieldHelp.vue'
import selectiveBaseline from './fixtures/linux-gateway-selective-0ef9e89.json'
import { DOCKER_COMMANDS, DOCKER_HINT, mayDrop, quiet } from './fixtures/forward-filter'

vi.mock('@/api', () => ({ default: {}, agentApi: { getAll: vi.fn(), summaries: vi.fn(), pushConfig: vi.fn(), getDeployment: vi.fn(), activateDeployment: vi.fn() }, profileApi: { list: vi.fn() } }))
vi.mock('@/api/linuxGateway', () => ({ linuxGatewayApi: {
  get: vi.fn(), save: vi.fn(), status: vi.fn(), preview: vi.fn(), apply: vi.fn(), remove: vi.fn(),
  previewDraft: vi.fn(), previewRestore: vi.fn(), restoreDraft: vi.fn(),
} }))
vi.mock('@/lib/feedback', () => ({ confirm: vi.fn(), confirmDanger: vi.fn(), notify: { success: vi.fn(), error: vi.fn() } }))
const routerPush = vi.fn()

const spec = { interface: 'eth0', address: '192.168.1.2', lan_cidr: '192.168.1.0/24', upstream_gateway: '192.168.1.1' }
const guard = { expected_profile_id: 'office', expected_service_type: 'mihomo', expected_config_revision: 10 }
const response = (data: unknown) => ({ data }) as any
const originalClipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
const originalExecCommand = Object.getOwnPropertyDescriptor(document, 'execCommand')
const writeText = vi.fn()
const config = (enabled = false) => ({
  config_revision: 9, restore_point: null,
  settings: { enabled, rule_set_ids: ['telegram', 'disabled'] }, fake_ip_range: '198.18.0.1/16', refresh_interval: 300, warnings: [],
  candidates: [
    { id: 'telegram', name: 'Telegram 已改名', behavior: 'ipcidr', enabled: true, available: true, policy: '国外代理' },
    { id: 'disabled', name: '已停用 IP 集合', behavior: 'classical', enabled: false, available: false },
  ],
})
const draftPreview = (action: 'save' | 'restore' = 'save') => ({
  success: true, action, config_revision: 9, preview_token: 'reviewed-config', restore_point: null,
  changes: [{ path: 'Mihomo.dns.listen', before: '0.0.0.0:53', after: '127.0.0.1:1053' }], warnings: [],
})
const draftGuard = { expected_config_revision: 9, preview_token: 'reviewed-config' }
const hostStatus = () => ({ success: true, enabled_in_config: true, detection: { suggested: { ...spec } }, managed: { active: false } })
const agent = (id = 'one', extra = {}) => ({ id, name: id, profile_id: 'office', service_type: 'mihomo', status: 'online', enabled: true, deployment_method: 'shell', ...extra })
const workspaceStub = defineComponent({
  name: 'SettingsWorkspace', props: ['open', 'dirty', 'busy'], emits: ['save', 'update:open'],
  setup(props, { slots }) { return () => h('section', { role: 'dialog', 'data-state': props.open ? 'open' : 'closed' }, [slots.default?.(), slots.footer?.({ disabled: props.busy })]) },
})
const buttonStub = defineComponent({
  name: 'Button', setup(_props, { slots, attrs }) { return () => h('button', attrs, slots.default?.()) },
})
const wrappers: ReturnType<typeof shallowMount>[] = []
const attachedRoots: HTMLElement[] = []

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
async function render(enabled = false, attached = false, extra = {}, realPublish = false, stubs: Record<string, boolean> = {}) {
  vi.mocked(linuxGatewayApi.get).mockResolvedValue(response({ ...config(enabled), ...extra }))
  let attachTo: HTMLElement | undefined
  if (attached) {
    attachTo = document.createElement('div'); document.body.appendChild(attachTo); attachedRoots.push(attachTo)
  }
  const wrapper = shallowMount(LinuxGatewaySettings, {
    attachTo,
    props: { open: true, profileId: 'office' },
    global: { renderStubDefaultSlot: true, provide: { [routerKey as symbol]: { push: routerPush } }, stubs: { SettingsWorkspace: workspaceStub, Button: buttonStub, Input: false, AgentStatusRow: false, ...(realPublish ? { SettingsPublishBar: false } : {}), ...stubs } },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
function button(wrapper: ReturnType<typeof shallowMount>, label: string) {
  return wrapper.findAll('button').find(item => item.attributes('aria-label') === label || item.text() === label)!
}

beforeEach(() => {
  vi.resetAllMocks()
  writeText.mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  vi.mocked(agentApi.getAll).mockResolvedValue(response([agent()]))
  // Both endpoints read the same fixture, including later binding changes.
  vi.mocked(agentApi.summaries).mockImplementation(() => agentApi.getAll())
  vi.mocked(agentApi.pushConfig).mockImplementation(async (_id, id) => response({ deployment_id: id, status: 'succeeded' }))
  vi.mocked(agentApi.getDeployment).mockImplementation(async (_id, id) => response({ deployment_id: id, status: 'uploading' }))
  vi.mocked(agentApi.activateDeployment).mockImplementation(async (_id, id) => response({ deployment_id: id, status: 'succeeded' }))
  vi.mocked(profileApi.list).mockResolvedValue(response([{ id: 'office', name: '办公室' }]))
  vi.mocked(linuxGatewayApi.status).mockResolvedValue(response(hostStatus()))
  vi.mocked(linuxGatewayApi.save).mockImplementation(async (_profile, settings) => response({ ...config(), success: true, settings, config_revision: 10, publish_guard: guard }))
  vi.mocked(linuxGatewayApi.previewDraft).mockResolvedValue(response(draftPreview()))
  vi.mocked(linuxGatewayApi.previewRestore).mockResolvedValue(response(draftPreview('restore')))
  vi.mocked(linuxGatewayApi.restoreDraft).mockResolvedValue(response({ ...config(), success: true, config_revision: 10 }))
  vi.mocked(linuxGatewayApi.preview).mockResolvedValue(response({ ...hostStatus(), ready: true, blockers: [], warnings: [], commands: ['nft list ruleset'], spec }))
  vi.mocked(linuxGatewayApi.apply).mockResolvedValue(response({ success: true, managed: { active: true, healthy: true, spec } }))
  vi.mocked(linuxGatewayApi.remove).mockResolvedValue(response({ success: true, managed: { active: false } }))
  vi.mocked(confirm).mockResolvedValue(true)
})
afterEach(() => {
  wrappers.splice(0).forEach(item => item.unmount())
  attachedRoots.splice(0).forEach(root => root.remove())
  if (originalClipboard) Object.defineProperty(navigator, 'clipboard', originalClipboard)
  else delete (navigator as any).clipboard
  if (originalExecCommand) Object.defineProperty(document, 'execCommand', originalExecCommand)
  else delete (document as any).execCommand
})

describe('Linux gateway opt-in workflow', () => {
  it('highlights an unchanged preview instead of listing differences', async () => {
    vi.mocked(linuxGatewayApi.previewDraft).mockResolvedValueOnce(response({ ...draftPreview(), changes: [] }))
    const wrapper = await render()
    wrapper.getComponent(Switch).vm.$emit('update:modelValue', true)
    await flushPromises()
    await button(wrapper, '预览配置变更').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('配置没有变化')
    expect(wrapper.text()).toContain('无需保存')
    expect(wrapper.text()).not.toContain('项配置')
  })
  it('previews local settings offline without saving or changing host networking, then invalidates edited previews', async () => {
    vi.mocked(agentApi.getAll).mockResolvedValue(response([agent('one', { status: 'offline' })]))
    const wrapper = await render()
    wrapper.getComponent(Switch).vm.$emit('update:modelValue', true)
    await flushPromises()
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
    await button(wrapper, '预览配置变更').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.previewDraft).toHaveBeenCalledWith('office', { enabled: true, rule_set_ids: ['telegram', 'disabled'] }, 9)
    expect(wrapper.text()).toContain('试用预览 · 尚未保存')
    expect(wrapper.text()).toContain('127.0.0.1:1053')
    expect(wrapper.text()).toContain('保存后会改变 1 项配置')
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(false)
    expect(linuxGatewayApi.save).not.toHaveBeenCalled()
    expect(linuxGatewayApi.status).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
    wrapper.findAllComponents(Checkbox)[0].vm.$emit('update:modelValue', false)
    await flushPromises()
    expect(wrapper.text()).not.toContain('试用预览 · 尚未保存')
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
  })

  it('canceling the explicit save leaves the trial dirty and does not claim success', async () => {
    const wrapper = await render()
    wrapper.getComponent(Switch).vm.$emit('update:modelValue', true)
    vi.mocked(confirm).mockResolvedValue(false)
    await flushPromises()
    await wrapper.getComponent(SettingsPublishBar).vm.$emit('save-draft')
    await flushPromises()
    expect(linuxGatewayApi.save).not.toHaveBeenCalled()
    expect(notify.success).not.toHaveBeenCalled()
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(true)
  })

  it('keeps a stale-save conflict visible without discarding the local trial', async () => {
    const wrapper = await render()
    wrapper.getComponent(Switch).vm.$emit('update:modelValue', true)
    vi.mocked(linuxGatewayApi.save).mockRejectedValue({ response: { status: 409, data: { message: '配置或共享资源已更新，请重新打开' } } })
    await flushPromises()
    await expect(wrapper.getComponent(SettingsPublishBar).props('save')()).rejects.toBeTruthy()
    expect(wrapper.text()).toContain('配置或共享资源已更新')
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(true)
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })

  it('ignores draft preview responses after closing or changing profiles', async () => {
    const pending = deferred<any>()
    vi.mocked(linuxGatewayApi.previewDraft).mockImplementationOnce(() => pending.promise)
    const wrapper = await render()
    await button(wrapper, '预览配置变更').trigger('click')
    await wrapper.setProps({ profileId: 'other' })
    pending.resolve(response(draftPreview()))
    await flushPromises()
    expect(wrapper.text()).not.toContain('试用预览 · 尚未保存')
    expect(notify.error).not.toHaveBeenCalled()
    expect(linuxGatewayApi.save).not.toHaveBeenCalled()
  })

  it('does not save to the old profile after switching during confirmation', async () => {
    const pending = deferred<boolean>()
    const wrapper = await render()
    vi.mocked(confirm).mockImplementationOnce(() => pending.promise)
    const saving = wrapper.getComponent(SettingsPublishBar).props('save')()
    await flushPromises()
    await wrapper.setProps({ profileId: 'other' })
    pending.resolve(true)
    expect(await saving).toBeNull()
    expect(linuxGatewayApi.save).not.toHaveBeenCalled()
  })

  it('previews recovery before allowing confirmation, supports cancellation and never publishes or applies networking', async () => {
    const wrapper = await render(true, false, { restore_point: { id: 'snapshot', created_at: '2026-10-20T01:00:00Z' } })
    await button(wrapper, '预览恢复变更').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('恢复预览 · 尚未恢复')
    expect(confirm).not.toHaveBeenCalled()
    expect(linuxGatewayApi.restoreDraft).not.toHaveBeenCalled()
    vi.mocked(confirm).mockResolvedValueOnce(false)
    await button(wrapper, '恢复这份草稿').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.restoreDraft).not.toHaveBeenCalled()
    await button(wrapper, '恢复这份草稿').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.restoreDraft).toHaveBeenCalledExactlyOnceWith('office', draftGuard)
    expect(wrapper.getComponent(Switch).props('modelValue')).toBe(false)
    expect(wrapper.findAll('button').some(item => item.text() === '预览恢复变更')).toBe(false)
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
    expect(linuxGatewayApi.remove).not.toHaveBeenCalled()
    expect(agentApi.pushConfig).not.toHaveBeenCalled()
  })

  it('blocks a stale recovery without losing the original saved state', async () => {
    const wrapper = await render(true, false, { restore_point: { id: 'snapshot', created_at: '2026-10-20T01:00:00Z' } })
    await button(wrapper, '预览恢复变更').trigger('click'); await flushPromises()
    vi.mocked(linuxGatewayApi.restoreDraft).mockRejectedValueOnce(new Error('配置已更新，请重新预览'))
    await button(wrapper, '恢复这份草稿').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('配置已更新，请重新预览')
    expect(wrapper.getComponent(Switch).props('modelValue')).toBe(true)
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('continues normal save-and-publish with exactly the committed revision after preview', async () => {
    const wrapper = await render(false, false, {}, true)
    wrapper.getComponent(Switch).vm.$emit('update:modelValue', true)
    await flushPromises()
    expect(button(wrapper, '保存并发布').attributes('disabled')).toBeDefined()
    await button(wrapper, '预览配置变更').trigger('click'); await flushPromises()
    await button(wrapper, '保存并发布').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.save).toHaveBeenCalledExactlyOnceWith('office', { enabled: true, rule_set_ids: ['telegram', 'disabled'] }, draftGuard)
    expect(agentApi.pushConfig).toHaveBeenCalledExactlyOnceWith('one', expect.any(String), guard)
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(false)
  })

  it('keeps ready-candidate activation available after saving clears the draft preview', async () => {
    vi.mocked(agentApi.pushConfig).mockImplementationOnce(async (_id, id) => response({ deployment_id: id, status: 'ready' }))
    const wrapper = await render(false, false, {}, true)
    wrapper.getComponent(Switch).vm.$emit('update:modelValue', true)
    await flushPromises()
    await button(wrapper, '预览配置变更').trigger('click'); await flushPromises()
    await button(wrapper, '保存并发布').trigger('click'); await flushPromises()
    expect(wrapper.text()).not.toContain('试用预览 · 尚未保存')
    const activate = button(wrapper, '应用并启动')
    expect(activate.attributes('disabled')).toBeUndefined()
    await activate.trigger('click'); await flushPromises()
    expect(agentApi.activateDeployment).toHaveBeenCalledWith('one', expect.any(String))
  })

  it('stays off by default and neither mode switches nor saves execute host networking', async () => {
    const wrapper = await render()
    expect(wrapper.getComponent(Switch).props('modelValue')).toBe(false)
    expect(button(wrapper, '预览并检查').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('自动接管的临时地址范围（fake-IP）：198.18.0.1/16')
    wrapper.getComponent(Switch).vm.$emit('update:modelValue', true)
    await flushPromises()
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(true)
    expect(button(wrapper, '预览并检查').attributes('disabled')).toBeDefined()
    expect(await wrapper.getComponent(SettingsPublishBar).props('save')()).toEqual(guard)
    await flushPromises()
    expect(linuxGatewayApi.save).toHaveBeenCalledWith('office', { enabled: true, rule_set_ids: ['telegram', 'disabled'] }, draftGuard)
    expect(linuxGatewayApi.preview).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
    expect(linuxGatewayApi.remove).not.toHaveBeenCalled()
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(false)
  })

  it('preserves stable rule ids through renames and permits removing inactive selections', async () => {
    const wrapper = await render(true)
    const boxes = wrapper.findAllComponents(Checkbox)
    expect(wrapper.text()).toContain('Telegram 已改名')
    expect(boxes[0].props('modelValue')).toBe(true)
    expect(boxes[1].props('disabled')).toBe(false)
    boxes[1].vm.$emit('update:modelValue', false)
    await flushPromises()
    await wrapper.getComponent(SettingsPublishBar).props('save')()
    expect(linuxGatewayApi.save).toHaveBeenLastCalledWith('office', { enabled: true, rule_set_ids: ['telegram'] }, draftGuard)
  })

  it('requires successful preview of the current host inputs before explicit apply', async () => {
    const wrapper = await render(true)
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeDefined()
    await button(wrapper, '预览并检查').trigger('click')
    await flushPromises()
    expect(linuxGatewayApi.preview).toHaveBeenCalledWith('one', 'office', spec)
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeUndefined()
    await wrapper.get('#gateway-address').setValue('192.168.1.3')
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).not.toContain('检查通过，可以应用')
    await button(wrapper, '预览并检查').trigger('click')
    await flushPromises()
    await button(wrapper, '应用网关转发').trigger('click')
    await flushPromises()
    expect(linuxGatewayApi.apply).toHaveBeenCalledExactlyOnceWith('one', 'office', { ...spec, address: '192.168.1.3' })
    expect(wrapper.findAllComponents(AgentStatusRow).find(item => item.props('label') === '网络状态')?.props()).toMatchObject({ status: '已应用', tone: 'success' })
  })

  it('does not reuse a preview after a rule selection changes', async () => {
    const wrapper = await render(true)
    await button(wrapper, '预览并检查').trigger('click')
    await flushPromises()
    wrapper.findAllComponents(Checkbox)[0].vm.$emit('update:modelValue', false)
    await flushPromises()
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeDefined()
    expect(button(wrapper, '预览并检查').attributes('disabled')).toBeDefined()
  })

  it('blocks apply on failed preflight and keeps the concrete reason visible', async () => {
    vi.mocked(linuxGatewayApi.preview).mockResolvedValueOnce(response({ ...hostStatus(), ready: false, blockers: ['请先发布 Mihomo 网关配置'], commands: [], warnings: [], spec }))
    const wrapper = await render(true)
    await button(wrapper, '预览并检查').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('请先发布 Mihomo 网关配置')
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeDefined()
  })

  describe('operation notes clipboard fallback', () => {
    async function setupCopy(failure?: 'false' | 'throws') {
      const commands = ['创建 ConfigFlow 网关转发表', '设置 LAN 接口 eth0 的转发规则']
      vi.mocked(linuxGatewayApi.preview).mockResolvedValue(response({ ...hostStatus(), ready: true,
        blockers: [], warnings: [], commands, spec }))
      const wrapper = await render(true, true)
      await button(wrapper, '预览并检查').trigger('click'); await flushPromises()
      const copyButton = wrapper.get('button[aria-label="复制操作说明"]')
      const details = copyButton.element.closest('details')
      if (details) details.open = true
      const dialog = wrapper.get('[role="dialog"]').element
      const focusTarget = copyButton.element as HTMLButtonElement
      focusTarget.focus()
      const trap = () => { if (!dialog.contains(document.activeElement)) focusTarget.focus() }
      document.addEventListener('focusin', trap)
      const previousTextareas = document.querySelectorAll('textarea').length
      const selections: { insideDialog: boolean; text: string }[] = []
      const execCommand = vi.fn(() => {
        const active = document.activeElement
        selections.push({ insideDialog: dialog.contains(active), text: active instanceof HTMLTextAreaElement
          ? active.value.slice(active.selectionStart, active.selectionEnd) : '' })
        if (failure === 'throws') throw new Error('Clipboard denied')
        return failure !== 'false'
      })
      Object.defineProperty(document, 'execCommand', { configurable: true, value: execCommand })
      vi.mocked(notify.success).mockClear(); vi.mocked(notify.error).mockClear()
      return { copyButton, expected: commands.join('\n'), execCommand, selections, focusTarget, previousTextareas,
        cleanup: () => document.removeEventListener('focusin', trap) }
    }

    it.each(['unavailable', 'rejected'])('copies complete operation notes inside the dialog when clipboard is %s', async modern => {
      const state = await setupCopy()
      if (modern === 'unavailable') Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
      else writeText.mockRejectedValue(new Error('Permission denied'))
      try {
        await state.copyButton.trigger('click'); await flushPromises()
        expect(state.execCommand).toHaveBeenCalledExactlyOnceWith('copy')
        expect(state.selections).toEqual([{ insideDialog: true, text: state.expected }])
        expect(document.querySelectorAll('textarea')).toHaveLength(state.previousTextareas)
        expect(document.activeElement).toBe(state.focusTarget)
        expect(notify.success).toHaveBeenCalledOnce()
        expect(notify.error).not.toHaveBeenCalled()
        if (modern === 'rejected') expect(writeText).toHaveBeenCalledExactlyOnceWith(state.expected)
        else expect(writeText).not.toHaveBeenCalled()
        expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
        expect(linuxGatewayApi.remove).not.toHaveBeenCalled()
      } finally { state.cleanup() }
    })

    it.each(['false', 'throws'] as const)('reports failure without success when selection copy %s', async failure => {
      const state = await setupCopy(failure)
      Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
      try {
        await state.copyButton.trigger('click'); await flushPromises()
        expect(state.execCommand).toHaveBeenCalledExactlyOnceWith('copy')
        expect(state.selections).toEqual([{ insideDialog: true, text: state.expected }])
        expect(notify.error).toHaveBeenCalledOnce()
        expect(notify.success).not.toHaveBeenCalled()
        expect(document.querySelectorAll('textarea')).toHaveLength(state.previousTextareas)
        expect(document.activeElement).toBe(state.focusTarget)
      } finally { state.cleanup() }
    })
  })

  it('clears host inputs and ignores late detection after switching devices', async () => {
    vi.mocked(agentApi.getAll).mockResolvedValue(response([agent(), agent('two')]))
    const old = deferred<any>()
    const next = deferred<any>()
    vi.mocked(linuxGatewayApi.status).mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
    const wrapper = await render(true)
    const select = wrapper.getComponent(Select)
    select.vm.$emit('update:modelValue', 'one')
    await flushPromises()
    select.vm.$emit('update:modelValue', 'two')
    await flushPromises()
    old.resolve(response(hostStatus()))
    await flushPromises()
    expect((wrapper.get('#gateway-address').element as HTMLInputElement).value).toBe('')
    next.resolve(response({ ...hostStatus(), detection: { suggested: { ...spec, address: '192.168.1.8' } } }))
    await flushPromises()
    expect((wrapper.get('#gateway-address').element as HTMLInputElement).value).toBe('192.168.1.8')
  })

  it('ignores a late preview from a closed settings session', async () => {
    const pending = deferred<any>()
    vi.mocked(linuxGatewayApi.preview).mockReturnValueOnce(pending.promise)
    const wrapper = await render(true)
    await button(wrapper, '预览并检查').trigger('click')
    await wrapper.setProps({ open: false })
    pending.resolve(response({ ...hostStatus(), ready: true, blockers: [], commands: [], warnings: [], spec }))
    await flushPromises()
    expect(wrapper.text()).not.toContain('检查通过，可以应用')
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })

  it('shows an actionable old-Agent error while draft saving remains available', async () => {
    vi.mocked(linuxGatewayApi.status).mockRejectedValueOnce({ response: { status: 404 } })
    const wrapper = await render(true)
    expect(wrapper.text()).toContain('请在 Agent 页面升级后重试')
    expect(button(wrapper, '预览并检查').attributes('disabled')).toBeDefined()
    await wrapper.getComponent(SettingsPublishBar).props('save')()
    expect(linuxGatewayApi.save).toHaveBeenCalledOnce()
  })

  it('offers restore for managed hosts even when the mode is switched off', async () => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValueOnce(response({ ...hostStatus(), managed: { active: true, spec } }))
    const wrapper = await render(true)
    wrapper.getComponent(Switch).vm.$emit('update:modelValue', false)
    await flushPromises()
    expect(button(wrapper, '撤销网关转发').attributes('disabled')).toBeUndefined()
    await button(wrapper, '撤销网关转发').trigger('click')
    await flushPromises()
    expect(confirm).toHaveBeenCalledOnce()
    expect(linuxGatewayApi.remove).toHaveBeenCalledExactlyOnceWith('one', 'office')
    expect(linuxGatewayApi.save).not.toHaveBeenCalled()
  })

  it('requires a new preview after a transport failure instead of reusing an uncertain apply', async () => {
    vi.mocked(linuxGatewayApi.apply).mockRejectedValueOnce(new Error('连接中断'))
    const wrapper = await render(true)
    await button(wrapper, '预览并检查').trigger('click')
    await flushPromises()
    await button(wrapper, '应用网关转发').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('请刷新检测，确认设备实际状态')
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeDefined()
  })

  it('allows restoring a partially applied pending journal and displays its error', async () => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValueOnce(response({ ...hostStatus(), managed: { active: false, pending: true, error: '上次应用中断，需要恢复', spec } }))
    const wrapper = await render(false)
    expect(wrapper.text()).toContain('应用未完成，需要恢复或重新检查')
    expect(wrapper.text()).toContain('上次应用中断，需要恢复')
    expect(button(wrapper, '撤销网关转发').attributes('disabled')).toBeUndefined()
    await button(wrapper, '撤销网关转发').trigger('click')
    await flushPromises()
    expect(linuxGatewayApi.remove).toHaveBeenCalledExactlyOnceWith('one', 'office')
  })

  it('does not apply without a deployed gateway marker even if a preview says ready', async () => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValueOnce(response({ ...hostStatus(), enabled_in_config: false }))
    const wrapper = await render(true)
    await button(wrapper, '预览并检查').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('设备尚未检测到已部署的 Linux 网关配置')
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeDefined()
  })
})

describe('Gateway LAN input validation', () => {
  it('accepts a single-NIC LAN and rejects wrong subnet, duplicate addresses and non-address input', () => {
    expect(gatewaySpecErrors(spec)).toEqual([])
    expect(gatewaySpecErrors({ ...spec, upstream_gateway: spec.address })).toContain('Linux 地址与主路由地址不能相同。')
    expect(gatewaySpecErrors({ ...spec, address: '10.1.1.2' })).toContain('Linux 地址必须在填写的 LAN 网段内。')
    expect(gatewaySpecErrors({ ...spec, lan_cidr: '192.168.1.5/24' })).toContain('LAN 网段请填写网络地址，例如 192.0.2.0/24，而不是设备地址。')
    expect(gatewaySpecErrors({ ...spec, address: 'example.com' }).length).toBeGreaterThan(0)
  })
})

describe('router split guards', () => {
  const split = (mode: 'full_tun' | 'invalid' | 'legacy', changes = {}) => ({ router_split: { mode, source: mode === 'legacy' ? 'setup' : 'marker', issues: [], route_rule_set_ids: mode === 'full_tun' ? ['telegram'] : [], ...changes } })
  const conflict = (code: string, message = '服务端原文') => Object.assign(new Error('Request failed'), { response: { status: 409, data: { success: false, code, message } } })

  it('makes the selective gateway unavailable for a full-TUN router split', async () => {
    const wrapper = await render(false, false, split('full_tun'))
    expect(wrapper.get('[data-router-split="full_tun"]').text()).toContain('主路由分流（完整 TUN）')
    expect(wrapper.getComponent(Switch).props('disabled')).toBe(true)
    expect(wrapper.text()).toContain('不适用于主路由分流')
    expect(wrapper.get('[data-router-routes]').text()).toContain('安装时选择的转发网段：fake-IP + 1 个规则集')
    // The marker keeps the install-time list; the upstream router records may differ since.
    expect(wrapper.get('[data-router-routes]').text()).toContain('以「上游路由」中的设置为准')
    expect(wrapper.get('[data-router-routes]').text()).toContain('Telegram 已改名')
    expect(wrapper.findAllComponents(Checkbox)).toHaveLength(0)
    for (const label of ['预览配置变更', '预览并检查', '应用网关转发', '撤销网关转发', '预览恢复变更']) expect(button(wrapper, label)).toBeUndefined()
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
    // The stub keeps saveDisabled as a fallthrough attribute.
    expect(wrapper.getComponent(SettingsWorkspace).attributes('save-disabled')).toBe('true')
    await button(wrapper, '到上游路由管理转发网段').trigger('click')
    expect(routerPush).toHaveBeenCalledWith('/routers')
    expect(wrapper.emitted('update:open')).toEqual([[false]])
  })

  it('cleans a leftover gateway record only after confirming forwarding stays on', async () => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), managed: { active: true, healthy: true, preserves_forwarding: true, spec } }))
    // The server replaces the Agent's text about client gateways, which a router split never changed.
    vi.mocked(linuxGatewayApi.remove).mockResolvedValue(response({ success: true, message: '旧网关记录已清理，IPv4 转发保持开启。', managed: { active: false } }))
    const wrapper = await render(false, false, split('full_tun'))
    expect(wrapper.findAllComponents(AgentStatusRow).find(item => item.props('label') === '旧网关记录')?.props()).toMatchObject({ status: '仍存在', tone: 'warning' })
    await button(wrapper, '清理旧网关记录').trigger('click'); await flushPromises()
    expect(confirm).toHaveBeenCalledWith('将删除旧的 configflow_gateway 规则和网关记录；IPv4 转发已持久化，会保持开启。', expect.objectContaining({ confirmText: '清理旧记录' }))
    expect(linuxGatewayApi.remove).toHaveBeenCalledExactlyOnceWith('one', 'office')
    expect(notify.success).toHaveBeenCalledWith('旧网关记录已清理')
    expect(wrapper.text()).toContain('旧网关记录已清理，IPv4 转发保持开启。')
    expect(wrapper.text()).not.toContain('请恢复客户端原来的网关')
  })

  it.each([
    [{ active: false, pending: true, preserves_forwarding: true }, '仍存在'],
    [{ active: false, error: '网关状态文件无法读取', preserves_forwarding: true }, '无法确认'],
  ])('offers cleanup for a pending or unreadable gateway record too %#', async (managed, label) => {
    // The setup check refuses these as well, so the page must not call them 「无」.
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), managed }))
    const wrapper = await render(false, false, split('full_tun'))
    expect(wrapper.findAllComponents(AgentStatusRow).find(item => item.props('label') === '旧网关记录')?.props()).toMatchObject({ status: label, tone: 'warning' })
    expect(button(wrapper, '清理旧网关记录')).toBeDefined()
  })

  it('does not offer cleanup when forwarding is not persisted yet', async () => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), managed: { active: true, healthy: true, preserves_forwarding: false, spec } }))
    const wrapper = await render(false, false, split('full_tun'))
    expect(button(wrapper, '清理旧网关记录')).toBeUndefined()
    expect(wrapper.text()).toContain('清理会关闭转发')
  })

  it('pauses publishing when the base YAML lost the full TUN, without offering an automatic repair', async () => {
    const wrapper = await render(false, false, split('full_tun', { issues: ['tun_missing'] }))
    expect(wrapper.get('[data-tun-missing]').text()).toContain('基础 YAML 缺少完整 TUN，发布已暂停。')
    expect(wrapper.get('[data-tun-missing]').text()).toContain('请在 Mihomo 基础 YAML 中恢复 tun 后再发布')
    expect(button(wrapper, '补回完整 TUN')).toBeUndefined()
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
  })

  it('stops every operation for an invalid marker', async () => {
    const wrapper = await render(false, false, split('invalid'))
    expect(wrapper.get('[data-router-split="invalid"]').text()).toContain('主路由分流标记 mihomo.router_split 无效')
    expect(wrapper.find('[data-router-routes]').exists()).toBe(false)
    expect(wrapper.find('[data-router-split="full_tun"]').exists()).toBe(false)
    expect(wrapper.getComponent(Switch).props('disabled')).toBe(true)
    expect(wrapper.findAllComponents(Checkbox)).toHaveLength(0)
    for (const label of ['预览配置变更', '预览并检查', '应用网关转发', '撤销网关转发', '清理旧网关记录']) expect(button(wrapper, label)).toBeUndefined()
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
  })

  it('keeps a legacy router split on and lets the server decide a restore, without offering a migration', async () => {
    const wrapper = await render(true, false, { ...split('legacy'), restore_point: { id: 'snapshot', created_at: '2026-10-20T10:00:00Z' } })
    expect(wrapper.get('[data-router-split="legacy"]').text()).toContain('旧版主路由分流')
    expect(wrapper.getComponent(Switch).props('disabled')).toBe(true)
    expect(wrapper.text()).toContain('新增网段也需加入主路由的静态路由。')
    expect(wrapper.findAllComponents(Checkbox)[0].props('disabled')).toBe(false)
    expect(button(wrapper, '迁移到完整 TUN')).toBeUndefined()
    expect(wrapper.text()).not.toContain('迁移')
    // A snapshot that keeps the gateway on (only the rule sets changed) can be restored.
    expect(button(wrapper, '预览恢复变更').attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).not.toContain('恢复后 Linux 网关为关闭状态')
    await button(wrapper, '预览恢复变更').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.previewRestore).toHaveBeenCalledOnce()
    // One that turns it off is refused by the server, whose text is shown in place.
    vi.mocked(linuxGatewayApi.previewRestore).mockRejectedValueOnce(conflict('ROUTER_SPLIT_RESTORE_BLOCKED', '恢复后 Linux 网关为关闭状态，主路由转来的连接会中断，已阻止。'))
    const blocked = await render(true, false, { ...split('legacy'), restore_point: { id: 'snapshot', created_at: '2026-10-20T10:00:00Z' } })
    await button(blocked, '预览恢复变更').trigger('click'); await flushPromises()
    expect(blocked.get('[role="alert"]').text()).toContain('恢复后 Linux 网关为关闭状态，主路由转来的连接会中断，已阻止。')
  })

  it('requires a danger confirmation before tearing down a legacy router split', async () => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), managed: { active: true, healthy: true, spec } }))
    const wrapper = await render(true, false, split('legacy'))
    vi.mocked(confirmDanger).mockResolvedValueOnce(false)
    await button(wrapper, '撤销网关转发').trigger('click'); await flushPromises()
    expect(confirmDanger).toHaveBeenCalledWith('主路由仍把代理网段转给这台 Linux。撤销后 TUN 接管与 IPv4 转发会还原，这些连接会中断，直到重新应用转发。', { title: '撤销「one」的网关转发？', confirmText: '仍然撤销' })
    expect(confirm).not.toHaveBeenCalled()
    expect(linuxGatewayApi.remove).not.toHaveBeenCalled()
    vi.mocked(confirmDanger).mockResolvedValueOnce(true)
    // The server replaces the Agent's text for a confirmed teardown; without one the local copy says the same.
    vi.mocked(linuxGatewayApi.remove).mockResolvedValue(response({ success: true, message: '', managed: { active: false } }))
    await button(wrapper, '撤销网关转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.remove).toHaveBeenCalledExactlyOnceWith('one', 'office', true)
    // What needs handling is the main router's static routes, not the clients.
    expect(wrapper.text()).toContain('请删除主路由上指向这台 Linux 的静态路由')
    expect(wrapper.text()).not.toContain('请恢复客户端原来的网关')
  })

  it('shows the forward-filter commands for a full-TUN host as a warning only', async () => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), enabled_in_config: false, managed: { active: false }, forward_filter: mayDrop() }))
    const wrapper = await render(false, false, split('full_tun'), false, { ForwardFilterNotice: false })
    const section = wrapper.get('[data-forward-filter-section]')
    expect(section.get('h2').text()).toBe('转发过滤')
    expect(section.text()).toContain('「one」的防火墙检查结果，只作提醒，不影响发布。')
    expect(wrapper.findAllComponents(AgentStatusRow).find(item => item.props('label') === '转发过滤')?.props()).toMatchObject({ status: '可能拦截 UDP', tone: 'warning' })
    expect(section.text()).toContain(DOCKER_HINT)
    expect(section.get('pre').text()).toBe(DOCKER_COMMANDS.join('\n'))
    // Publishing stays exactly as the router split defines it.
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
    expect(wrapper.findAllComponents(AgentStatusRow).find(item => item.props('label') === '旧网关记录')?.props()).toMatchObject({ status: '无', tone: 'success' })
  })

  it.each([
    ['an older Agent', undefined],
    ['ok', quiet('ok')],
    ['unknown', quiet('unknown', 'inspect_failed')],
  ])('shows no forward-filter section for %s', async (_case, forward_filter) => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), managed: { active: false }, ...(forward_filter ? { forward_filter } : {}) }))
    const wrapper = await render(false, false, split('full_tun'))
    expect(wrapper.find('[data-forward-filter-section]').exists()).toBe(false)
  })

  it.each([
    ['a selective gateway', {}],
    ['a legacy router split', split('legacy')],
    ['an invalid marker', split('invalid')],
  ])('never shows the forward-filter section for %s', async (_case, extra) => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), managed: { active: true, healthy: true, spec }, forward_filter: mayDrop() }))
    const wrapper = await render(true, false, extra, false, { ForwardFilterNotice: false })
    expect(wrapper.find('[data-forward-filter-section]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain(DOCKER_HINT)
  })

  it('keeps the selective gateway unchanged without a router split', async () => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), managed: { active: true, healthy: true, spec } }))
    const wrapper = await render(true)
    expect(wrapper.find('[data-router-split]').exists()).toBe(false)
    expect(wrapper.getComponent(Switch).props('disabled')).toBe(false)
    await button(wrapper, '撤销网关转发').trigger('click'); await flushPromises()
    expect(confirmDanger).not.toHaveBeenCalled()
    expect(linuxGatewayApi.remove).toHaveBeenCalledExactlyOnceWith('one', 'office')
  })

  // Selective-gateway client steps that a router split never follows (lab copy scan).
  const selectiveCopy = ['网关和 DNS 都设为', '主路由无需静态路由', '客户端原网关', '客户端原来的网关', '修改客户端网关', '切换客户端', '网关说明与安装要求', '1.8.25-go', '其余流量由 Linux 直连转发', '迁移']
  const managedHost = () => vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), managed: { active: true, healthy: true, preserves_forwarding: true, spec } }))
  const helpTexts = (wrapper: ReturnType<typeof shallowMount>) => wrapper.findAllComponents(FieldHelp).map(item => item.props('text') as string)

  const selectiveScenarios: Record<string, (extra: object) => Promise<ReturnType<typeof shallowMount>>> = {
    off: extra => render(false, false, extra),
    managed: extra => { vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), managed: { active: true, healthy: true, spec } })); return render(true, false, extra) },
    'no-agent': extra => { vi.mocked(agentApi.getAll).mockResolvedValue(response([])); return render(false, false, extra) },
  }
  it.each(Object.keys(selectiveScenarios).flatMap(name => [
    [name, 'absent', {}], [name, 'null', { router_split: null }], [name, 'mode null', { router_split: { mode: null, source: null, issues: [], route_rule_set_ids: [] } }],
  ] as const))('renders a gateway-scheme profile exactly as 0ef9e89 did (%s, router_split %s)', async (name, _label, extra) => {
    const wrapper = await selectiveScenarios[name](extra)
    const expected = (selectiveBaseline.scenarios as Record<string, { text: string; help: string[] }>)[name]
    expect(wrapper.text()).toBe(expected.text)
    expect(helpTexts(wrapper)).toEqual(expected.help)
    expect(wrapper.find('[data-router-split-guide]').exists()).toBe(false)
  })

  it('explains the full TUN split instead of the selective client steps', async () => {
    managedHost()
    const wrapper = await render(false, false, split('full_tun'))
    for (const text of selectiveCopy) expect(wrapper.text()).not.toContain(text)
    expect(wrapper.text()).not.toContain('192.168.1.2')
    expect(helpTexts(wrapper).join('\n')).not.toContain('设备以 Linux 为网关和 DNS')
    const guide = wrapper.get('[data-router-split-guide]').text()
    expect(guide).toContain('分流说明')
    expect(guide).toContain('fake-IP 网段 198.18.0.1/16 和所选规则集的 IPv4 网段转给这台 Linux')
    expect(guide).toContain('完整 TUN，接管主路由转来的全部连接')
    expect(guide).toContain('需要 Mihomo Agent 1.8.40-go 或更新版本')
    expect(wrapper.get('[data-router-split-clients]').text()).toContain('客户端网关保持主路由，无需改为 Linux；DNS 保持接入主路由时的设置')
    const stop = wrapper.get('[data-router-split-stop]').text()
    expect(stop).toContain('先删除主路由上指向这台 Linux 的静态路由，再把主路由 DHCP 的 DNS 改回原值')
    expect(stop).toContain('完整 TUN 不使用本页的开关与网关转发')
    // The OxiDNS settings guide applies to both schemes and stays.
    expect(wrapper.text()).toContain('DNS 配置说明')
  })

  it('tells a legacy split to keep its takeover and drops the client gateway steps', async () => {
    managedHost()
    const wrapper = await render(true, false, split('legacy'))
    for (const text of selectiveCopy) expect(wrapper.text()).not.toContain(text)
    expect(wrapper.text()).toContain('客户端网关保持主路由，DNS 保持接入主路由时的设置，无需修改。')
    expect(wrapper.text()).toContain('撤销前，先删除主路由上指向这台 Linux 的静态路由。')
    const guide = wrapper.get('[data-router-split-guide]').text()
    expect(guide).toContain('不要关闭 Linux 网关或撤销网关转发')
    expect(guide).not.toContain('1.8.40-go')
    expect(wrapper.get('[data-router-split-stop]').text()).toContain('静态路由删除前，不要撤销网关转发')
    expect(button(wrapper, '撤销网关转发')).toBeDefined()
  })

  it('keeps the selective client steps out of an invalid marker too', async () => {
    const wrapper = await render(false, false, split('invalid'))
    for (const text of selectiveCopy) expect(wrapper.text()).not.toContain(text)
    expect(wrapper.get('[data-router-split-guide]').text()).not.toContain('完整 TUN')
    expect(wrapper.get('[data-router-split-stop]').text()).toContain('请先按上方提示恢复标记')
  })

  it('pauses publishing when the base YAML tun filters the forwarded connections', async () => {
    const wrapper = await render(false, false, split('full_tun', { issues: ['tun_filtered'] }))
    expect(wrapper.get('[data-tun-filtered]').text()).toContain('exclude-interface')
    expect(wrapper.get('[data-tun-filtered]').text()).toContain('基础 YAML 的 tun 带有过滤，发布已暂停。')
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
    expect(wrapper.find('[data-tun-missing]').exists()).toBe(false)
    const plain = await render(false, false, split('full_tun'))
    expect(plain.find('[data-tun-filtered]').exists()).toBe(false)
  })

  it.each([
    ['设备上没有旧网关记录，无需清理；IPv4 转发保持不变。', '设备上没有旧网关记录，无需清理；IPv4 转发保持不变。'],
    ['', '旧网关记录已清理，IPv4 转发保持开启。'],
  ])('shows the server outcome of a cleanup (%s)', async (message, shown) => {
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ ...hostStatus(), managed: { active: true, healthy: true, preserves_forwarding: true, spec } }))
    vi.mocked(linuxGatewayApi.remove).mockResolvedValue(response({ success: true, message, managed: { active: false } }))
    const wrapper = await render(false, false, split('full_tun'))
    await button(wrapper, '清理旧网关记录').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain(shown)
  })

  it('explains a refused gateway-off edit of an old router split that cannot keep forwarding', async () => {
    vi.mocked(linuxGatewayApi.previewDraft).mockRejectedValueOnce(conflict('ROUTER_SPLIT_FORWARDING_NOT_PERSISTED', '')).mockRejectedValueOnce(conflict('ROUTER_SPLIT_FORWARDING_NOT_PERSISTED'))
    // An old router split whose base YAML gained a full TUN reports no mode.
    const wrapper = await render(true, false)
    wrapper.findAllComponents(Checkbox)[0].vm.$emit('update:modelValue', false); await flushPromises()
    await button(wrapper, '预览配置变更').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('这台设备尚未持久化 IPv4 转发，已阻止；请保持 Linux 网关开启。')
    await button(wrapper, '预览配置变更').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('服务端原文')
  })

  it('explains a refused cleanup while forwarding is not persisted', async () => {
    const code = 'FORWARDING_NOT_PERSISTED'
    managedHost()
    vi.mocked(linuxGatewayApi.remove).mockRejectedValueOnce(conflict(code)).mockRejectedValueOnce(conflict(code, ''))
    const wrapper = await render(false, false, split('full_tun'))
    await button(wrapper, '清理旧网关记录').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('服务端原文')
    await button(wrapper, '清理旧网关记录').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('IPv4 转发尚未持久化')
    expect(notify.success).not.toHaveBeenCalled()
  })

  it.each(['ROUTER_SPLIT_LEGACY', 'ROUTER_SPLIT_RESTORE_BLOCKED'])('shows the server text for %s where the request failed', async code => {
    vi.mocked(linuxGatewayApi.previewDraft).mockRejectedValueOnce(conflict(code))
    const wrapper = await render(true, false, split('legacy'))
    wrapper.findAllComponents(Checkbox)[0].vm.$emit('update:modelValue', false); await flushPromises()
    await button(wrapper, '预览配置变更').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('服务端原文')
  })

  it.each(['ROUTER_SPLIT_LEGACY', 'ROUTER_SPLIT_FULL_TUN', 'UPGRADE_REQUIRED'])('falls back to the shared copy for %s without a server text', async code => {
    vi.mocked(linuxGatewayApi.previewDraft).mockRejectedValueOnce(conflict(code, ''))
    const wrapper = await render(true, false, split('legacy'))
    wrapper.findAllComponents(Checkbox)[0].vm.$emit('update:modelValue', false); await flushPromises()
    await button(wrapper, '预览配置变更').trigger('click'); await flushPromises()
    const { ROUTER_SPLIT_MESSAGES } = await import('@/lib/routerSplit')
    expect(wrapper.text()).toContain(ROUTER_SPLIT_MESSAGES[code])
  })
})
