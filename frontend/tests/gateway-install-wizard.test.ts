import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { defineComponent, h, inject, provide } from 'vue'
import GatewayInstallWizard from '@/components/agents/GatewayInstallWizard.vue'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import SettingsWorkspace from '@/components/generate/SettingsWorkspace.vue'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import FormField from '@/components/common/FormField.vue'
import InstallerDownloaderPicker from '@/components/agents/InstallerDownloaderPicker.vue'
import { buildInstallCommand } from '@/lib/coreInstallCommand'
import { gatewayOnboardingApi } from '@/api/gatewayOnboarding'
import { linuxGatewayApi } from '@/api/linuxGateway'
import { gatewayEnvironmentErrors, installationMatches } from '@/lib/gatewayOnboarding'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { DOCKER_COMMANDS, DOCKER_HINT, mayDrop, quiet } from './fixtures/forward-filter'
vi.mock('@/api/gatewayOnboarding', () => ({ gatewayOnboardingApi: { get: vi.fn(), preview: vi.fn(), save: vi.fn(), create: vi.fn(), sessions: vi.fn(), session: vi.fn(), publish: vi.fn(), network: vi.fn() } }))
vi.mock('@/api/linuxGateway', () => ({ linuxGatewayApi: { status: vi.fn(), preview: vi.fn(), apply: vi.fn(), remove: vi.fn(), persistForwarding: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({ confirm: vi.fn(), confirmDanger: vi.fn(), notify: { success: vi.fn(), error: vi.fn() } }))
const response = (data: unknown) => ({ data }) as any
const clipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
const originalExecCommand = Object.getOwnPropertyDescriptor(document, 'execCommand')
const writeText = vi.fn()
const spec = { interface: 'eth0', address: '192.168.1.2', lan_cidr: '192.168.1.0/24', upstream_gateway: '192.168.1.1' }
const networkInputs = { interface: 'interface', address: 'address', cidr: 'lan_cidr', upstream: 'upstream_gateway' } as const
function expectNetwork(wrapper: ReturnType<typeof shallowMount>, network = spec) {
  for (const [id, field] of Object.entries(networkInputs)) expect(wrapper.get(`#onboarding-${id}`).element).toHaveProperty('value', network[field])
}
const settings = { rule_set_ids: ['ip'], direct_rule_set_ids: ['cn'], proxy_rule_set_ids: ['global'], local_dns: 'https://223.5.5.5/dns-query', default_forward: 'forward_remote' as const }
const config = () => ({ profile_id: 'office', config_revision: 7, settings, fake_ip_range: '198.18.0.0/16', oxidns_mode: 'template', warnings: [], blockers: [], can_prepare: true,
  ip_candidates: [{ id: 'ip', name: 'Telegram IP', behavior: 'ipcidr', available: true, enabled: true }],
  domain_candidates: [{ id: 'cn', name: '国内域名', behavior: 'domain', available: true, enabled: true }, { id: 'global', name: '国外域名', behavior: 'domain', available: true, enabled: true }],
})
const session = (phase = 'waiting', extra = {}) => ({ id: 'installation-1', profile_id: 'office', name: 'home-gateway', network: { ...spec }, server_url: 'http://192.168.1.3:8000', command: 'curl https://example.test/install | sudo bash', expires_at: '2026-10-20T10:00:00Z', phase,
  agents: phase === 'waiting' ? {} : { mihomo: { id: 'new-mihomo', name: 'Home Mihomo', host: spec.address, port: 8080, profile_id: 'office', service_type: 'mihomo', status: 'online' }, oxidns: { id: 'new-oxidns', name: 'Home OxiDNS', host: spec.address, port: 8082, profile_id: 'office', service_type: 'oxidns', status: 'online' } }, deployments: {}, ...extra,
}) as any
const workspaceStub = defineComponent({ name: 'SettingsWorkspace', props: ['open', 'dirty', 'busy'], emits: ['update:open'], setup(props, { slots }) { return () => h('section', { role: 'dialog', 'data-state': props.open ? 'open' : 'closed' }, [slots.default?.(), slots.footer?.()]) } })
const buttonStub = defineComponent({ name: 'Button', setup(_props, { slots, attrs }) { return () => h('button', attrs, slots.default?.()) } })
const selectTabKey = Symbol('selectTab')
const tabsStub = defineComponent({ name: 'Tabs', props: ['modelValue'], emits: ['update:modelValue'], setup(_props, { slots, emit }) {
  provide(selectTabKey, (value: string) => emit('update:modelValue', value))
  return () => h('div', slots.default?.())
} })
const tabsTriggerStub = defineComponent({ name: 'TabsTrigger', props: ['value', 'disabled'], setup(props, { slots, attrs }) {
  const select = inject<(value: string) => void>(selectTabKey)!
  return () => h('button', { ...attrs, type: 'button', role: 'tab', disabled: props.disabled, onClick: () => { if (!props.disabled) select(props.value) } }, slots.default?.())
} })
const wrappers: ReturnType<typeof shallowMount>[] = []
const attachedRoots: HTMLElement[] = []
function button(wrapper: ReturnType<typeof shallowMount>, text: string) { return wrapper.findAll('button').find(item => item.attributes('aria-label') === text || item.text() === text)! }
function stepButton(wrapper: ReturnType<typeof shallowMount>, index: number) { return wrapper.get('[data-gateway-steps]').findAll('[role="tab"]')[index] }
async function render(attached = false, embedded = false, goal: 'gateway' | 'router' = 'gateway', extraStubs: Record<string, boolean> = {}) {
  let attachTo: HTMLElement | undefined
  if (attached) {
    attachTo = document.createElement('div'); document.body.appendChild(attachTo); attachedRoots.push(attachTo)
  }
  const wrapper = shallowMount(GatewayInstallWizard, { attachTo, props: { open: true, profileId: 'office', profileName: '办公室', embedded, goal }, global: { renderStubDefaultSlot: true, stubs: { SettingsWorkspace: workspaceStub, Button: buttonStub, Tabs: tabsStub, TabsTrigger: tabsTriggerStub, Input: false, Textarea: false, GatewayNetworkFields: false, GatewayClientInstructions: false, ...extraStubs } } })
  wrappers.push(wrapper); await flushPromises(); return wrapper
}
async function fillEnvironment(wrapper: ReturnType<typeof shallowMount>) {
  for (const [id, value] of Object.entries({ name: 'home-gateway', server: 'http://192.168.1.3:8000', address: spec.address, upstream: spec.upstream_gateway, interface: spec.interface, cidr: spec.lan_cidr })) await wrapper.get(`#onboarding-${id}`).setValue(value)
  await button(wrapper, '下一步：选择规则').trigger('click'); await flushPromises()
}
async function prepare(wrapper: ReturnType<typeof shallowMount>) {
  await fillEnvironment(wrapper)
  await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises()
  await button(wrapper, '保存并生成安装命令').trigger('click'); await flushPromises()
}
function deferred<T>() { let resolve!: (data: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { resolve, promise } }
beforeEach(() => {
  vi.resetAllMocks(); localStorage.clear()
  writeText.mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response(config()))
  vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: null }))
  vi.mocked(gatewayOnboardingApi.preview).mockResolvedValue(response({ success: true, ready: true, blockers: [], warnings: [], changes: ['启用选择性网关', '配置 OxiDNS'] }))
  vi.mocked(gatewayOnboardingApi.save).mockResolvedValue(response({ success: true, ready: true, config_revision: 8, blockers: [], warnings: [], changes: [] }))
  vi.mocked(gatewayOnboardingApi.create).mockResolvedValue(response(session()))
  vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session('enrolled')))
  vi.mocked(gatewayOnboardingApi.publish).mockResolvedValue(response(session('publishing')))
  vi.mocked(gatewayOnboardingApi.network).mockResolvedValue(response(session('enrolled', { auto_network: true })))
  vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ success: true, enabled_in_config: true, supported: true, managed: { active: false } }))
  vi.mocked(linuxGatewayApi.preview).mockResolvedValue(response({ success: true, ready: true, blockers: [], warnings: [], commands: ['设置 IP 转发', '创建本向导专用 nftables 表'], spec, managed: { active: false } }))
  vi.mocked(linuxGatewayApi.apply).mockResolvedValue(response({ success: true, managed: { active: true, healthy: true } }))
  vi.mocked(linuxGatewayApi.remove).mockResolvedValue(response({ success: true, managed: { active: false } }))
})
afterEach(() => {
  wrappers.splice(0).forEach(item => item.unmount()); vi.useRealTimers(); localStorage.clear()
  attachedRoots.splice(0).forEach(root => root.remove())
  if (clipboard) Object.defineProperty(navigator, 'clipboard', clipboard)
  else delete (navigator as any).clipboard
  if (originalExecCommand) Object.defineProperty(document, 'execCommand', originalExecCommand)
  else delete (document as any).execCommand
})
describe('gateway installation wizard', () => {
  it('defaults ConfigFlow to the current browser origin instead of a saved loopback address', async () => {
    localStorage.setItem('serverDomain', 'http://127.0.0.1:5001')
    const wrapper = await render()
    expect(wrapper.get('#onboarding-server').element).toHaveProperty('value', window.location.origin)
    expect(localStorage.getItem('serverDomain')).toBe('http://127.0.0.1:5001')
  })

  it('does no writes on entry, and validates environment before continuing', async () => {
    const wrapper = await render()
    expect(button(wrapper, '下一步：选择规则').attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('安装登记后自动检测网络')
    expect(wrapper.text()).not.toContain('请补全或修正以下信息')
    for (const id of ['name', 'server', 'address']) expect(wrapper.get(`#onboarding-${id}`).element.closest('details')).toBeNull()
    for (const id of ['interface', 'cidr', 'upstream']) expect(wrapper.get(`#onboarding-${id}`).element.closest('details')).toHaveProperty('open', false)
    for (const message of gatewayEnvironmentErrors('home-gateway', 'http://localhost:3000', { interface: '', address: '', lan_cidr: '', upstream_gateway: '' })) expect(wrapper.text()).not.toContain(message)
    await button(wrapper, '下一步：选择规则').trigger('click'); await flushPromises()
    expect(wrapper.find('section[aria-label="设备与网络"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('请补全或修正以下信息')
    expect(gatewayOnboardingApi.save).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
    await fillEnvironment(wrapper)
    expect(wrapper.text()).toContain('自动包含 fake-IP 网段')
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(true)
    expect(button(wrapper, '保存并生成安装命令').attributes('disabled')).toBeDefined()
  })
  it.each(['', spec.address])('uses automatic network discovery with optional host IP %s', async address => {
    const wrapper = await render()
    expect(wrapper.find('#onboarding-network-command').exists()).toBe(false)
    expect(wrapper.find('#onboarding-network-result').exists()).toBe(false)
    await wrapper.get('#onboarding-server').setValue('http://192.168.1.3:8000')
    await wrapper.get('#onboarding-address').setValue(address)
    await button(wrapper, '下一步：选择规则').trigger('click'); await flushPromises()
    await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises()
    await button(wrapper, '保存并生成安装命令').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.create).toHaveBeenCalledWith({ profile_id: 'office', name: 'home-gateway', server_url: 'http://192.168.1.3:8000', auto_network: true, agent_ip: address, config_revision: 8 })
    expect(linuxGatewayApi.status).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.publish).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })
  it('requires complete advanced parameters when overriding automatic discovery', async () => {
    const wrapper = await render()
    await wrapper.get('#onboarding-server').setValue('http://192.168.1.3:8000')
    await wrapper.get('#onboarding-interface').setValue('eth0')
    await button(wrapper, '下一步：选择规则').trigger('click'); await flushPromises()
    expect(wrapper.find('section[aria-label="分流规则"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('请补全或修正以下信息')
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
  })
  describe('installation command clipboard fallback', () => {
    async function setupCopy(failure?: 'false' | 'throws') {
      vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session() }))
      const wrapper = await render(true)
      const expected = session().command
      const copyButton = button(wrapper, '复制安装命令')
      const dialog = wrapper.get('[role="dialog"]').element
      const previousTextareas = document.querySelectorAll('textarea').length
      const focusTarget = copyButton.element as HTMLButtonElement
      focusTarget.focus()
      const trap = () => { if (!dialog.contains(document.activeElement)) focusTarget.focus() }
      document.addEventListener('focusin', trap)
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
      return { copyButton, expected, execCommand, selections, focusTarget, previousTextareas,
        cleanup: () => document.removeEventListener('focusin', trap) }
    }

    it.each(['unavailable', 'rejected'])('copies the complete command inside the dialog when clipboard is %s', async modern => {
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
  it.each(['profile', 'reopen'])('clears advanced network overrides on %s', async transition => {
    const wrapper = await render()
    for (const [id, field] of Object.entries(networkInputs)) await wrapper.get(`#onboarding-${id}`).setValue(spec[field])
    if (transition === 'profile') await wrapper.setProps({ profileId: 'home', profileName: '家里' })
    else {
      await wrapper.setProps({ open: false })
      await wrapper.setProps({ open: true })
    }
    await flushPromises()
    expectNetwork(wrapper, { interface: '', address: '', lan_cidr: '', upstream_gateway: '' })
    expect(wrapper.text()).not.toContain('请补全或修正以下信息')
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
  })
  it('keeps the browser origin when system settings provide a different address', async () => {
    vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response({ ...config(), server_url: 'https://configflow.example.test' }))
    const wrapper = await render()
    expect(wrapper.get('#onboarding-server').element).toHaveProperty('value', window.location.origin)
  })
  it('does not overwrite an address being edited while settings load', async () => {
    const pending = deferred<any>()
    vi.mocked(gatewayOnboardingApi.get).mockReturnValue(pending.promise)
    const wrapper = await render()
    await wrapper.get('#onboarding-server').setValue('https://entered.example.test')
    pending.resolve(response({ ...config(), server_url: 'https://stored.example.test' })); await flushPromises()
    expect(wrapper.get('#onboarding-server').element).toHaveProperty('value', 'https://entered.example.test')
  })
  it('preserves OxiDNS direct fallback through preview and saved installation', async () => {
    vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response({ ...config(), settings: { ...settings, default_forward: 'forward_local' } }))
    const wrapper = await render(); await prepare(wrapper)
    expect(gatewayOnboardingApi.preview).toHaveBeenCalledWith('office', expect.objectContaining({ default_forward: 'forward_local' }))
    expect(gatewayOnboardingApi.save).toHaveBeenCalledWith('office', expect.objectContaining({ default_forward: 'forward_local' }))
  })
  it('requires preview, saves both configs and creates installation from the saved revision', async () => {
    const wrapper = await render(); await prepare(wrapper)
    expect(gatewayOnboardingApi.preview).toHaveBeenCalledWith('office', { ...settings, query_recorder: false, confirm_template_conversion: false, expected_config_revision: 7 })
    expect(gatewayOnboardingApi.save).toHaveBeenCalledWith('office', { ...settings, query_recorder: false, confirm_template_conversion: false, expected_config_revision: 7 })
    expect(gatewayOnboardingApi.create).toHaveBeenCalledWith({ profile_id: 'office', name: 'home-gateway', server_url: 'http://192.168.1.3:8000', network: spec, config_revision: 8 })
    expect(wrapper.get('#onboarding-command').element).toHaveProperty('value', session().command)
    expect(gatewayOnboardingApi.publish).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
    expect(localStorage.getItem('configflow.gateway-installation.office')).toBe('installation-1')
    expect(localStorage.length).toBe(1)
  })
  it('offers the same curl / wget choice as the Agent page for the short install link', async () => {
    const url = 'http://192.168.1.3:8000/api/agents/i/short-link'
    vi.mocked(gatewayOnboardingApi.create).mockResolvedValue(response(session('waiting', { download_url: url })))
    const wrapper = await render(); await prepare(wrapper)
    const picker = wrapper.getComponent(InstallerDownloaderPicker)
    expect(picker.props('modelValue')).toBe('curl')
    expect(picker.attributes('aria-label')).toBe('安装命令下载方式')
    expect(wrapper.get('#onboarding-command').element).toHaveProperty('value', buildInstallCommand(url, 'curl'))
    // Native Alpine has only BusyBox wget.
    expect(wrapper.text()).toContain('原生 Alpine 未自带 curl，请选择 wget。')
    picker.vm.$emit('update:modelValue', 'wget'); await flushPromises()
    expect(wrapper.get('#onboarding-command').element).toHaveProperty('value', `wget -O configflow-install.sh '${url}' && sh configflow-install.sh`)
    await button(wrapper, '复制安装命令').trigger('click'); await flushPromises()
    expect(writeText).toHaveBeenCalledWith(buildInstallCommand(url, 'wget'))
  })
  it('hides the downloader choice when only the server fallback command is available', async () => {
    const wrapper = await render(); await prepare(wrapper)
    expect(wrapper.findComponent(InstallerDownloaderPicker).exists()).toBe(false)
    expect(wrapper.get('#onboarding-command').element).toHaveProperty('value', session().command)
  })
  it('offers to adopt the install address when it differs from the system server address', async () => {
    const message = '系统设置中的服务地址是 http://172.17.0.2:5001，与这里填写的 http://192.168.1.3:8000 不一致。'
    vi.mocked(gatewayOnboardingApi.create).mockRejectedValueOnce({ response: { status: 409, data: { success: false, code: 'server_url_mismatch', message } } })
    const wrapper = await render(); await prepare(wrapper)
    expect(wrapper.text()).toContain(message)
    const adopt = button(wrapper, '将 http://192.168.1.3:8000 设为系统服务地址并继续')
    expect(adopt.exists()).toBe(true)
    await adopt.trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.create).toHaveBeenLastCalledWith({ profile_id: 'office', name: 'home-gateway', server_url: 'http://192.168.1.3:8000', network: spec, config_revision: 8, use_as_server_domain: true })
    expect(wrapper.get('#onboarding-command').element).toHaveProperty('value', session().command)
  })
  it('allows correcting network information immediately after creating a session', async () => {
    const wrapper = await render(); await prepare(wrapper)
    await button(wrapper, '重新准备安装').trigger('click'); await flushPromises()
    expect(wrapper.get('#onboarding-address').element).toHaveProperty('value', spec.address)
    expect(wrapper.get('#onboarding-interface').element).toHaveProperty('value', '')
    expect(gatewayOnboardingApi.create).toHaveBeenCalledTimes(1)
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })
  it('allows clearing a selected ruleset that was removed from the library', async () => {
    vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response({ ...config(), settings: { ...settings, rule_set_ids: ['removed'] } }))
    const wrapper = await render(); await fillEnvironment(wrapper)
    expect(wrapper.text()).toContain('已移除的规则集（removed）')
    const boxes = wrapper.findAllComponents(Checkbox)
    expect(boxes[1].props('disabled')).toBe(false)
    boxes[1].vm.$emit('update:modelValue', false); await flushPromises()
    await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises()
    expect(vi.mocked(gatewayOnboardingApi.preview).mock.lastCall?.[1].rule_set_ids).toEqual([])
  })
  it('preserves searches and selected rules when switching categories without saving', async () => {
    const wrapper = await render(); await fillEnvironment(wrapper)
    const picker = wrapper.get('[data-gateway-rule-picker]')
    expect(picker.findAllComponents(Checkbox)).toHaveLength(1)
    expect(picker.text()).toContain('Telegram IP')
    expect(picker.text()).not.toContain('国内域名')
    expect(picker.text()).not.toContain('国外域名')
    const search = 'no-such-gateway-ruleset'
    await wrapper.get('#onboarding-search').setValue(search)
    for (const category of ['IP 接管', '直连域名', '代理域名', 'IP 接管']) {
      await button(wrapper, `${category}，已选 1 个规则集`).trigger('click'); await flushPromises()
      expect(wrapper.get('#onboarding-search').element).toHaveProperty('value', search)
      expect(picker.findAll('p').filter(item => item.text().includes('没有匹配的规则集'))).toHaveLength(1)
      expect(picker.findAllComponents(Checkbox)).toHaveLength(0)
    }
    expect(wrapper.text()).not.toContain('Telegram IP')
    expect(wrapper.text()).not.toContain('当前没有已引用的规则集')
    expect(gatewayOnboardingApi.preview).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.save).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
    await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises()
    expect(vi.mocked(gatewayOnboardingApi.preview).mock.lastCall?.[1]).toMatchObject(settings)
    await wrapper.get('#onboarding-search').setValue('')
    expect(picker.text()).toContain('Telegram IP')
    expect(picker.getComponent(Checkbox).props('modelValue')).toBe(true)
    expect(wrapper.text()).not.toContain('没有匹配的规则集')
  })
  it('guides empty profiles through the rule library and profile rule references', async () => {
    vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response({ ...config(), ip_candidates: [], domain_candidates: [],
      settings: { ...settings, rule_set_ids: [], direct_rule_set_ids: [], proxy_rule_set_ids: [] } }))
    const wrapper = await render(); await fillEnvironment(wrapper)
    const picker = wrapper.get('[data-gateway-rule-picker]')
    for (const category of ['IP 接管', '直连域名', '代理域名']) {
      await button(wrapper, `${category}，已选 0 个规则集`).trigger('click'); await flushPromises()
      expect(picker.findAll('p').filter(item => item.text().includes('「规则库」添加'))).toHaveLength(1)
      expect(picker.text()).toContain('「策略规则」引用并启用')
      expect(picker.findAllComponents(Checkbox)).toHaveLength(0)
      expect(picker.text()).not.toContain('没有匹配的规则集')
    }
    expect(gatewayOnboardingApi.save).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
  })
  it('clears the initial DNS warning after editing and validates the new DNS value', async () => {
    const warning = '当前 DNS 使用明文 53 端口，请改用加密上游。'
    vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response({ ...config(),
      settings: { ...settings, local_dns: '223.5.5.5' }, field_warnings: { local_dns: warning } }))
    const wrapper = await render(); await fillEnvironment(wrapper)
    expect(wrapper.get('#onboarding-dns-warning').text()).toBe(warning)
    const newDns = 'https://1.1.1.1/dns-query'
    await wrapper.get('#onboarding-dns').setValue(newDns); await flushPromises()
    expect(wrapper.find('#onboarding-dns-warning').exists()).toBe(false)
    expect(wrapper.text()).not.toContain(warning)
    await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.preview).toHaveBeenCalledExactlyOnceWith('office', {
      ...settings, local_dns: newDns, query_recorder: false, confirm_template_conversion: false, expected_config_revision: 7,
    })
    expect(gatewayOnboardingApi.save).not.toHaveBeenCalled()
  })
  it('keeps query recording off by default and sends the explicit choice', async () => {
    const wrapper = await render(); await fillEnvironment(wrapper)
    const toggle = wrapper.findComponent(Switch)
    expect(toggle.props('modelValue')).toBe(false)
    expect(wrapper.text()).toContain('关闭（默认）')
    const field = wrapper.findAllComponents(FormField).find(item => item.props('label') === 'OxiDNS 查询记录')!
    expect(field.props('hint')).toContain('会持续写入磁盘')
    expect(field.props('help')).toContain('默认关闭')
    expect(field.props('persistentHint')).toBe(true)
    toggle.vm.$emit('update:modelValue', true); await flushPromises()
    expect(wrapper.text()).toContain('开启，记录每条 DNS 查询')
    await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.preview).toHaveBeenCalledExactlyOnceWith('office', expect.objectContaining({ query_recorder: true }))
  })
  it('shows a profile that already records queries as enabled', async () => {
    vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response({ ...config(), settings: { ...settings, query_recorder: true } }))
    const wrapper = await render(); await fillEnvironment(wrapper)
    expect(wrapper.findComponent(Switch).props('modelValue')).toBe(true)
  })
  it('collapses general gateway notes while keeping current and preview problems directly visible', async () => {
    const currentWarning = '有一个已引用的 IP 规则集暂不可用。'
    const currentBlocker = '当前配置缺少可用的代理出口。'
    const previewWarning = '所选规则集包含重复网段。'
    const previewBlocker = '检查未找到可用的代理节点。'
    vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response({ ...config(),
      warnings: [currentWarning], blockers: [currentBlocker], can_prepare: false }))
    vi.mocked(gatewayOnboardingApi.preview).mockResolvedValue(response({ success: true, ready: false,
      warnings: [previewWarning], blockers: [previewBlocker], changes: [] }))
    const wrapper = await render(); await fillEnvironment(wrapper)
    const notes = wrapper.findAll('details').find(item => item.find('summary').text() === '网关说明')!
    expect(notes.element).toHaveProperty('open', false)
    const expectVisibleProblem = (message: string) => {
      const item = wrapper.findAll('p, li').find(candidate => candidate.text() === message)!
      expect(item.exists()).toBe(true)
      expect(item.element.closest('details')).toBeNull()
      expect(item.isVisible()).toBe(true)
    }
    for (const message of [currentWarning, currentBlocker]) expectVisibleProblem(message)
    await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises()
    for (const message of [currentWarning, currentBlocker, previewWarning, previewBlocker]) expectVisibleProblem(message)
    expect(notes.element).toHaveProperty('open', false)
    expect(button(wrapper, '保存并生成安装命令').attributes('disabled')).toBeDefined()
    expect(gatewayOnboardingApi.save).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
  })
  it('shows installation expiry in local time instead of raw server timestamps', async () => {
    const expiresAt = '2026-10-20T06:45:38.242868+00:00'
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('waiting', { expires_at: expiresAt }) }))
    const wrapper = await render()
    expect(wrapper.text()).toContain(new Date(expiresAt).toLocaleString())
    expect(wrapper.text()).toContain('本地时间')
    expect(wrapper.text()).not.toContain(expiresAt)
  })
  it.each([undefined, 'not-a-date'])('keeps missing or invalid installation expiry %s understandable', async (expiresAt) => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('waiting', { expires_at: expiresAt }) }))
    const wrapper = await render()
    expect(wrapper.text()).toContain('命令有效期暂时无法确认')
    expect(wrapper.text()).not.toContain('Invalid Date')
    expect(wrapper.text()).not.toContain('undefined')
  })
  it('moves overlapping domain choices to one destination and invalidates the preview', async () => {
    const wrapper = await render(); await fillEnvironment(wrapper)
    await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises()
    await button(wrapper, '代理域名，已选 1 个规则集').trigger('click'); await flushPromises()
    const picker = wrapper.get('[data-gateway-rule-picker]')
    expect(button(wrapper, '保存并生成安装命令').attributes('disabled')).toBeUndefined()
    expect(picker.findAllComponents(Checkbox).map(item => item.props('modelValue'))).toEqual([false, true])
    picker.findAllComponents(Checkbox)[0].vm.$emit('update:modelValue', true)
    await flushPromises()
    expect(button(wrapper, '保存并生成安装命令').attributes('disabled')).toBeDefined()
    await button(wrapper, '直连域名，已选 0 个规则集').trigger('click'); await flushPromises()
    expect(picker.findAllComponents(Checkbox).map(item => item.props('modelValue'))).toEqual([false, false])
    await button(wrapper, '代理域名，已选 2 个规则集').trigger('click'); await flushPromises()
    expect(picker.findAllComponents(Checkbox).map(item => item.props('modelValue'))).toEqual([true, true])
    expect(gatewayOnboardingApi.save).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
    await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises()
    expect(vi.mocked(gatewayOnboardingApi.preview).mock.lastCall?.[1]).toMatchObject({ direct_rule_set_ids: [], proxy_rule_set_ids: ['global', 'cn'] })
  })
  it('publishes exact enrolled pair, then requires network preview and explicit apply', async () => {
    const wrapper = await render(); await prepare(wrapper)
    await button(wrapper, '检查安装进度').trigger('click'); await flushPromises()
    await button(wrapper, '按顺序发布两项配置').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.publish).toHaveBeenCalledExactlyOnceWith('installation-1', 'office', false)
    expect(linuxGatewayApi.preview).not.toHaveBeenCalled()
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session('published')))
    await button(wrapper, '刷新进度').trigger('click'); await flushPromises()
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeDefined()
    await button(wrapper, '检查并预览转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.preview).toHaveBeenCalledWith('new-mihomo', 'office', spec)
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
    await button(wrapper, '应用网关转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.apply).toHaveBeenCalledExactlyOnceWith('new-mihomo', 'office', spec)
    expect(wrapper.findAllComponents(AgentStatusRow).find(item => item.props('label') === '网关转发')?.props()).toMatchObject({ status: '已启用', tone: 'success' })
  })
  it.each([2, 3])('can return directly to verification after reviewing completed step %s', async (reviewStep) => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('published') }))
    const wrapper = await render()
    expect(stepButton(wrapper, 4).attributes('aria-current')).toBe('step')
    await stepButton(wrapper, reviewStep).trigger('click'); await flushPromises()
    expect(stepButton(wrapper, reviewStep).attributes('aria-current')).toBe('step')
    // Reviewing completed work must not require a status refresh to unlock it again.
    expect(stepButton(wrapper, 4).attributes('disabled')).toBeUndefined()
    await stepButton(wrapper, 4).trigger('click'); await flushPromises()
    expect(wrapper.find('section[aria-label="启用与验证"]').exists()).toBe(true)
    expect(gatewayOnboardingApi.session).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.publish).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })
  it.each(['waiting', 'enrolled', 'publishing', 'publish_failed'])('keeps verification unavailable while the installation is %s', async (phase) => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session(phase) }))
    const wrapper = await render()
    expect(stepButton(wrapper, 4).attributes('disabled')).toBeDefined()
    await stepButton(wrapper, 4).trigger('click'); await flushPromises()
    expect(wrapper.find('section[aria-label="启用与验证"]').exists()).toBe(false)
    expect(linuxGatewayApi.preview).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })
  it('keeps manual review open through unchanged publishing polls, then advances on completion', async () => {
    const phase = 'publishing'
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session(phase) }))
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session(phase)))
    const wrapper = await render()
    await stepButton(wrapper, 2).trigger('click'); await flushPromises()
    for (let count = 1; count <= 2; count++) {
      await vi.advanceTimersByTimeAsync(4000); await flushPromises()
      expect(gatewayOnboardingApi.session).toHaveBeenCalledTimes(count)
      expect(wrapper.find('section[aria-label="安装核心"]').exists()).toBe(true)
      expect(stepButton(wrapper, 2).attributes('aria-current')).toBe('step')
    }
    // A real phase transition still brings the user to the newly available next action.
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session('published')))
    await vi.advanceTimersByTimeAsync(4000); await flushPromises()
    expect(wrapper.find('section[aria-label="启用与验证"]').exists()).toBe(true)
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })
  it('keeps restoration guidance after removal and stops instructing clients to use the Linux gateway', async () => {
    const published = session('published')
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: published }))
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(published))
    vi.mocked(linuxGatewayApi.status)
      .mockResolvedValue(response({ success: true, managed: { active: false } }))
      .mockResolvedValueOnce(response({ success: true, managed: { active: true, healthy: true } }))
    const wrapper = await render()
    expect(wrapper.text()).toContain('客户端接入检查')
    await button(wrapper, '撤销网关转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.remove).toHaveBeenCalledExactlyOnceWith('new-mihomo', 'office')
    const showsRestoration = () => /(?:网关转发已撤销|已撤销.*网关转发)/.test(wrapper.text())
    expect.soft(showsRestoration(), 'Restoration must remain visible in the workspace after the toast disappears').toBe(true)
    expect.soft(wrapper.text().includes('都填为'), 'Removed networking must not instruct clients to point gateway and DNS at Linux').toBe(false)
    expect.soft(wrapper.text().includes('客户端接入检查')).toBe(false)
    // A read-only refresh must keep the successful removal guidance visible.
    await button(wrapper, '重新读取网络状态').trigger('click'); await flushPromises()
    expect(showsRestoration()).toBe(true)
    expect(wrapper.text()).toMatch(/恢复.{0,16}(?:原|之前)|(?:原|之前).{0,16}(?:网关|DNS)/)
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
    await button(wrapper, '检查并预览转发').trigger('click'); await flushPromises()
    await button(wrapper, '应用网关转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.apply).toHaveBeenCalledExactlyOnceWith('new-mihomo', 'office', spec)
    expect(showsRestoration()).toBe(false)
    expect(wrapper.text()).toContain('客户端接入检查')
  })
  it('recovers an unfinished session without reinstalling, publishing, or applying', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('publishing') }))
    const wrapper = await render()
    expect(wrapper.text()).toContain('已恢复')
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.publish).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
    expect(button(wrapper, '继续确认发布').attributes('disabled')).toBeUndefined()
  })
  it('stops on failure and lets explicit retry continue same session', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('publish_failed', { error: 'Mihomo 健康检查失败' }) }))
    const wrapper = await render()
    expect(wrapper.text()).toContain('Mihomo 健康检查失败')
    expect(button(wrapper, '应用网关转发')).toBeUndefined()
    await button(wrapper, '处理后继续发布').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.publish).toHaveBeenCalledWith('installation-1', 'office', true)
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })
  it.each(['', '。', '。。', '！', '？', '!?', '...', '； \n'])('joins the publish failure with one sentence ending (suffix=%j)', async suffix => {
    const message = '安装设备离线或已禁用，请检查连接后继续'
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('enrolled') }))
    vi.mocked(gatewayOnboardingApi.publish).mockRejectedValue({ response: { status: 409, data: { message: message + suffix } } })
    const wrapper = await render()
    await button(wrapper, '按顺序发布两项配置').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain(`发布状态需要确认：${message}。请刷新进度，不要重复安装。`)
    expect(wrapper.text()).not.toContain('。。')
  })
  it('offers the download remedy only for rule download failures', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('publish_failed', { message: '无法下载「Telegram」规则集', remedy: { kind: 'rule_download', github: true } }) }))
    const wrapper = await render()
    const remedy = wrapper.findComponent({ name: 'RuleDownloadRemedy' })
    expect(remedy.exists()).toBe(true)
    expect(remedy.props('github')).toBe(true)
    remedy.vm.$emit('retry'); await flushPromises()
    expect(gatewayOnboardingApi.publish).toHaveBeenCalledWith('installation-1', 'office', true)
  })
  it('hides the download remedy for other publish failures', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('publish_failed', { error: 'Mihomo 健康检查失败' }) }))
    const wrapper = await render()
    expect(wrapper.findComponent({ name: 'RuleDownloadRemedy' }).exists()).toBe(false)
  })
  it('rejects changed target identity before applying', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('published') }))
    const wrapper = await render()
    await button(wrapper, '检查并预览转发').trigger('click'); await flushPromises()
    const wrong = session('published'); wrong.agents.mihomo.id = 'different-device'
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(wrong))
    await button(wrapper, '应用网关转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('设备绑定已改变')
  })
  it('reuses a saved revision when installation creation response is lost', async () => {
    vi.mocked(gatewayOnboardingApi.create).mockRejectedValueOnce(new Error('连接中断'))
    const wrapper = await render(); await prepare(wrapper)
    expect(wrapper.text()).toContain('连接中断')
    await button(wrapper, '保存并生成安装命令').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.save).toHaveBeenCalledTimes(1)
    expect(gatewayOnboardingApi.create).toHaveBeenCalledTimes(2)
    expect(vi.mocked(gatewayOnboardingApi.create).mock.calls.map(item => item[0].config_revision)).toEqual([8, 8])
  })
  it.each(['config_changed', 'target_changed'])('blocks changed installation %s before forwarding', async (flag) => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('published', { [flag]: true }) }))
    const wrapper = await render()
    expect(wrapper.text()).toContain('已停止发布与转发操作')
    expect(button(wrapper, '检查并预览转发').attributes('disabled')).toBeDefined()
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeDefined()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })
  it('allows registered installations to recover after only the command expires', async () => {
    const expired = session('published', { expired: true })
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: expired }))
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(expired))
    const wrapper = await render()
    expect(linuxGatewayApi.status).toHaveBeenCalledWith('new-mihomo', 'office')
    expect(button(wrapper, '检查并预览转发').attributes('disabled')).toBeUndefined()
    await button(wrapper, '检查并预览转发').trigger('click'); await flushPromises()
    await button(wrapper, '应用网关转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.apply).toHaveBeenCalledWith('new-mihomo', 'office', spec)
  })
  it('can inspect and restore owned networking when the configuration changed', async () => {
    const stale = session('published', { config_changed: true, target_changed: true })
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: stale }))
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(stale))
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ success: true, managed: { active: true, healthy: true } }))
    const wrapper = await render()
    expect(linuxGatewayApi.status).toHaveBeenCalledWith('new-mihomo', 'office')
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeDefined()
    await button(wrapper, '撤销网关转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.remove).toHaveBeenCalledWith('new-mihomo', 'office')
  })
  it('requires a new preparation when the command expired before enrollment', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('waiting', { expired: true }) }))
    const wrapper = await render()
    expect(wrapper.text()).toContain('安装凭据已过期')
    expect(button(wrapper, '复制安装命令').attributes('disabled')).toBeDefined()
  })
  it('can resume a publishing session with the same server job after reconnect', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('publishing') }))
    const wrapper = await render()
    await button(wrapper, '继续确认发布').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.publish).toHaveBeenCalledWith('installation-1', 'office', false)
  })
  it('ignores a delayed response after leaving fixed profile', async () => {
    const pending = deferred<any>()
    vi.mocked(gatewayOnboardingApi.preview).mockReturnValue(pending.promise)
    const wrapper = await render(); await fillEnvironment(wrapper)
    await button(wrapper, '检查配置与代理出口').trigger('click')
    await wrapper.setProps({ open: false })
    pending.resolve(response({ success: true, ready: true, blockers: [], warnings: [], changes: [] })); await flushPromises()
    expect(gatewayOnboardingApi.save).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
  })
  it('shows blockers and refuses commands after failed preview', async () => {
    vi.mocked(gatewayOnboardingApi.preview).mockResolvedValue(response({ success: true, ready: false, blockers: ['没有可用的代理节点'], warnings: [], changes: [] }))
    const wrapper = await render(); await fillEnvironment(wrapper)
    await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('没有可用的代理节点')
    expect(button(wrapper, '保存并生成安装命令').attributes('disabled')).toBeDefined()
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
  })
})
describe('gateway installation boundaries', () => {
  it('validates server origin and unambiguous target identity', () => {
    expect(gatewayEnvironmentErrors('home', 'http://192.168.1.3:8000', spec)).toEqual([])
    for (const server of ['http://localhost:8000', 'file:///etc/passwd', 'https://name:secret@example.com', 'https://example.com/path']) expect(gatewayEnvironmentErrors('home', server, spec).length).toBeGreaterThan(0)
    expect(installationMatches(session('published'), 'office')).toBe(true)
    expect(installationMatches(session('published'), 'other')).toBe(false)
    const wrong = session('published'); wrong.agents.mihomo.host = '192.168.1.99'
    expect(installationMatches(wrong, 'office')).toBe(false)
  })
})

it('tells an embedded user to publish once both Agents have enrolled', async () => {
  vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('enrolled') }))
  const wrapper = await render(false, true)
  expect(wrapper.text()).toContain('按顺序发布并检查两项配置后，再启用转发。')
  expect(wrapper.text()).not.toContain('请在目标 Linux 执行命令，完成登记后继续发布与检查。')
})

it('shows the enrolled Agents as online, offline or disabled, as publishing judges them', async () => {
  const rows = async (change: (agents: any) => void) => {
    const enrolled = session('enrolled'); change(enrolled.agents)
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: enrolled }))
    const wrapper = await render()
    await stepButton(wrapper, 2).trigger('click'); await flushPromises()
    const row = (label: string) => wrapper.findAllComponents(AgentStatusRow).find(item => item.props('label') === label)?.props()
    return { mihomo: row('Mihomo Agent'), oxidns: row('OxiDNS Agent') }
  }
  expect(await rows(agents => { agents.mihomo.enabled = false; agents.oxidns.status = 'offline' })).toMatchObject({
    mihomo: { status: '已禁用', tone: 'warning' }, oxidns: { status: '离线', tone: 'warning' } })
  expect(await rows(agents => { agents.mihomo.enabled = true })).toMatchObject({
    mihomo: { status: '在线', tone: 'success' }, oxidns: { status: '在线', tone: 'success' } })
})

it('does not offer to continue before a reinstalled host publishes, even if forwarding is still active', async () => {
  vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('enrolled') }))
  vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ success: true, enabled_in_config: true, supported: true, managed: { active: true, healthy: true, spec } }))
  const wrapper = await render(false, true)
  await button(wrapper, '重新自动检测')?.trigger('click'); await flushPromises()
  expect(button(wrapper, '继续设置测试客户端')).toBeUndefined()
})

describe.each([false, true])('shared automatic gateway flow (embedded=%s)', embedded => {
  it('keeps the client step blocked when application succeeds but forwarding is unhealthy', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('published') }))
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session('published')))
    vi.mocked(linuxGatewayApi.apply).mockResolvedValue(response({ success: true, managed: { active: true, healthy: false } }))
    const wrapper = await render(false, embedded)
    await button(wrapper, '检查并预览转发').trigger('click'); await flushPromises()
    await button(wrapper, '应用网关转发').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('暂不切换客户端')
    expect(button(wrapper, '继续设置测试客户端')).toBeUndefined()
    expect(wrapper.text()).not.toContain('客户端接入检查')
    expect(button(wrapper, '撤销网关转发')).toBeTruthy()
    expect(wrapper.emitted('completed')).toBeUndefined()
  })
  it('defaults to automatic detection and reuses prepared rules without a pasted probe', async () => {
    const wrapper = await render(false, embedded)
    expect(wrapper.find('#onboarding-network-result').exists()).toBe(false)
    expect(wrapper.get(embedded ? '#setup-agent-ip' : '#onboarding-address').element).toHaveProperty('value', '')
    expect(wrapper.get('#onboarding-interface').element.closest('details')).toHaveProperty('open', false)
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
    await wrapper.get('#onboarding-server').setValue('http://192.168.1.3:8000')
    if (!embedded) { await button(wrapper, '下一步：选择规则').trigger('click'); await flushPromises(); await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises() }
    await button(wrapper, embedded ? '生成安装命令' : '保存并生成安装命令').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.preview).toHaveBeenCalledWith('office', expect.objectContaining(settings))
    expect(gatewayOnboardingApi.create).toHaveBeenCalledWith(expect.objectContaining({ auto_network: true, agent_ip: '', config_revision: 8 }))
    expect(gatewayOnboardingApi.create.mock.calls[0][0]).not.toHaveProperty('network')
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })

  it('validates the reachable server before saving or creating an installation', async () => {
    const wrapper = await render(false, embedded)
    await wrapper.get('#onboarding-server').setValue('http://127.0.0.1:15881')
    await button(wrapper, embedded ? '生成安装命令' : '下一步：选择规则').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('局域网')
    expect(gatewayOnboardingApi.save).not.toHaveBeenCalled()
    expect(gatewayOnboardingApi.create).not.toHaveBeenCalled()
  })

  it('allows complete manual network parameters only in advanced options', async () => {
    const wrapper = await render(false, embedded)
    await wrapper.get('#onboarding-server').setValue('http://192.168.1.3:8000')
    await wrapper.get(embedded ? '#setup-agent-ip' : '#onboarding-address').setValue(spec.address)
    for (const [id, value] of Object.entries({ upstream: spec.upstream_gateway, interface: spec.interface, cidr: spec.lan_cidr })) await wrapper.get(`#onboarding-${id}`).setValue(value)
    if (!embedded) { await button(wrapper, '下一步：选择规则').trigger('click'); await flushPromises(); await button(wrapper, '检查配置与代理出口').trigger('click'); await flushPromises() }
    await button(wrapper, embedded ? '生成安装命令' : '保存并生成安装命令').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.create).toHaveBeenCalledWith(expect.objectContaining({ network: spec }))
    expect(gatewayOnboardingApi.create.mock.calls[0][0]).not.toHaveProperty('auto_network')
  })

  it('automatically detects the enrolled host once and exposes all four network values', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('enrolled', { auto_network: true, network: { ...spec, interface: '', lan_cidr: '', upstream_gateway: '' } }) }))
    const wrapper = await render(false, embedded)
    expect(gatewayOnboardingApi.network).toHaveBeenCalledExactlyOnceWith('installation-1', 'office', { auto_network: true })
    for (const value of Object.values(spec)) expect(wrapper.text()).toContain(value)
    expect(gatewayOnboardingApi.publish).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })

  it('allows manual repair after automatic detection fails without applying host changes', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('enrolled', { auto_network: true, network: { ...spec, interface: '', lan_cidr: '', upstream_gateway: '' } }) }))
    vi.mocked(gatewayOnboardingApi.network).mockRejectedValueOnce(new Error('无法自动确认网卡'))
    const wrapper = await render(false, embedded)
    expect(wrapper.text()).toContain('无法自动确认网卡')
    for (const [id, field] of Object.entries(networkInputs)) await wrapper.get(`#onboarding-${id}`).setValue(spec[field])
    expect(button(wrapper, '按顺序发布两项配置').attributes('disabled')).toBeDefined()
    await button(wrapper, '按顺序发布两项配置').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.publish).not.toHaveBeenCalled()
    await button(wrapper, '核对并保存网络参数').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.network).toHaveBeenLastCalledWith('installation-1', 'office', { network: spec })
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })

  it('does not overwrite advanced edits with enrolled polling', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('enrolled') }))
    const wrapper = await render(false, embedded)
    await wrapper.get('#onboarding-interface').setValue('eth1')
    await vi.advanceTimersByTimeAsync(12000); await flushPromises()
    expect(wrapper.get('#onboarding-interface').element).toHaveProperty('value', 'eth1')
    expect(gatewayOnboardingApi.session).not.toHaveBeenCalled()
    expect(button(wrapper, '按顺序发布两项配置').attributes('disabled')).toBeDefined()
  })

  it('shows operation preview, invalidates it after edits and rechecks the pair before applying', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('published') }))
    const wrapper = await render(false, embedded)
    await button(wrapper, '检查并预览转发').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('查看网络操作')
    expect(wrapper.text()).toContain('创建本向导专用 nftables 表')
    await wrapper.get('#onboarding-interface').setValue('eth1')
    expect(button(wrapper, '应用网关转发').attributes('disabled')).toBeDefined()
    await wrapper.get('#onboarding-interface').setValue('eth0')
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session('published', { target_changed: true })))
    await button(wrapper, '应用网关转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
    expect(wrapper.emitted('completed')).toBeUndefined()
  })

  it('requires healthy forwarding before continuing and keeps revocation on the original host', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('published') }))
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session('published')))
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ success: true, supported: true, managed: { active: true, healthy: true } }))
    const wrapper = await render(false, embedded)
    expect(wrapper.text().includes('客户端接入检查')).toBe(!embedded)
    if (embedded) expect(button(wrapper, '继续设置测试客户端')).toBeTruthy()
    await button(wrapper, '撤销网关转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.remove).toHaveBeenCalledExactlyOnceWith('new-mihomo', 'office')
    expect(button(wrapper, '继续设置测试客户端')).toBeUndefined()
    expect(wrapper.text()).toContain('网关转发已撤销')
    expect(wrapper.emitted('completed')).toBeUndefined()
  })
})

describe('router split with full TUN', () => {
  const ready = (changes: Record<string, unknown> = {}) => ({ success: true, supported: true, enabled_in_config: false, managed: { active: false, preserves_forwarding: true },
    ipv4_forwarding: { protocol: 1, current: '1', config: 'managed', hook_script: 'managed', startup_hook: 'registered', persisted: true, supported: true },
    tun: { configured: true, device: 'Meta', present: true, gateway_marker: false }, ...changes })
  const row = (wrapper: ReturnType<typeof shallowMount>, label: string) => wrapper.findAllComponents(AgentStatusRow).find(item => item.props('label') === label)?.props()
  async function renderPublished(status: unknown, embedded = true, takeover: 'full_tun' | 'legacy' = 'full_tun', extraStubs: Record<string, boolean> = {}, sessionChanges: Record<string, unknown> = {}) {
    vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response({ ...config(), takeover }))
    const published = session('published', { ...(takeover === 'full_tun' ? { takeover: 'full_tun' } : {}), ...sessionChanges })
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: published }))
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(published))
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response(status))
    return render(false, embedded, 'router', extraStubs)
  }

  it.each([
    ['mihomo', 'disabled', 'Mihomo Agent 已在管理中停用，启用后才能继续。'],
    ['oxidns', 'disabled', 'OxiDNS Agent 已在管理中停用，启用后才能继续。'],
    ['mihomo', 'offline', 'Mihomo Agent 离线，请恢复连接后重新检查。'],
    ['oxidns', 'offline', 'OxiDNS Agent 离线，请恢复连接后重新检查。'],
  ])('keeps physical checks visible but blocks continuing for %s %s, then refreshes recovery', async (service, state, warning) => {
    const agents = session('published').agents
    if (state === 'disabled') agents[service].enabled = false
    else agents[service].status = 'offline'
    const wrapper = await renderPublished(ready(), true, 'full_tun', {}, { agents })
    expect(row(wrapper, 'IPv4 转发')).toMatchObject({ status: '已开启并持久化', tone: 'success' })
    expect(row(wrapper, 'Mihomo TUN')).toMatchObject({ status: '运行中 · Meta', tone: 'success' })
    expect(wrapper.text()).toContain(warning)
    expect(button(wrapper, '继续接入主路由')).toBeUndefined()
    expect(wrapper.emitted('completed')).toBeUndefined()

    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session('published', { takeover: 'full_tun' })))
    await button(wrapper, '重新检查').trigger('click'); await flushPromises()
    expect(gatewayOnboardingApi.session).toHaveBeenCalledExactlyOnceWith('installation-1', 'office')
    expect(wrapper.text()).not.toContain(warning)
    await button(wrapper, '继续接入主路由').trigger('click')
    expect(wrapper.emitted('completed')).toEqual([[]])
    expect(linuxGatewayApi.status).toHaveBeenCalledTimes(2)
  })

  it('refreshes Agent availability even when the host network remains healthy', async () => {
    const wrapper = await renderPublished(ready())
    const fresh = session('published', { takeover: 'full_tun' })
    fresh.agents.oxidns.enabled = false
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(fresh))
    await button(wrapper, '重新检查').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('OxiDNS Agent 已在管理中停用')
    expect(button(wrapper, '继续接入主路由')).toBeUndefined()
    expect(row(wrapper, 'Mihomo TUN')?.tone).toBe('success')
  })

  it('does not treat a failed Agent refresh as permission to continue', async () => {
    const wrapper = await renderPublished(ready())
    vi.mocked(gatewayOnboardingApi.session).mockRejectedValue(new Error('刷新失败'))
    await button(wrapper, '重新检查').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('状态暂时无法确认')
    expect(button(wrapper, '继续接入主路由')).toBeUndefined()
    expect(row(wrapper, 'Mihomo TUN')?.tone).toBe('success')
  })

  it('keeps recovered Agents blocked until the physical recheck also succeeds', async () => {
    const agents = session('published').agents
    agents.oxidns.status = 'offline'
    const wrapper = await renderPublished(ready(), true, 'full_tun', {}, { agents })
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session('published', { takeover: 'full_tun' })))
    vi.mocked(linuxGatewayApi.status).mockRejectedValueOnce(new Error('网络超时'))
    await button(wrapper, '重新检查').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('网络超时')
    expect(row(wrapper, 'Mihomo TUN')?.tone).toBe('success')
    expect(button(wrapper, '继续接入主路由')).toBeUndefined()
    await button(wrapper, '重新检查').trigger('click'); await flushPromises()
    await button(wrapper, '继续接入主路由').trigger('click')
    expect(wrapper.emitted('completed')).toEqual([[]])
  })

  it.each(['selective', 'legacy'])('offers a read-only Agent recovery check while %s forwarding is active', async takeover => {
    const published = session('published', { takeover })
    published.agents.mihomo.enabled = false
    vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response({ ...config(), takeover }))
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: published }))
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ success: true, managed: { active: true, healthy: true } }))
    const wrapper = await render(false, true, 'gateway')
    expect(button(wrapper, '继续设置测试客户端')).toBeUndefined()
    expect(button(wrapper, '重新读取网络状态')).toBeTruthy()
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session('published', { takeover })))
    await button(wrapper, '重新读取网络状态').trigger('click'); await flushPromises()
    await button(wrapper, '继续设置测试客户端').trigger('click')
    expect(wrapper.emitted('completed')).toEqual([[]])
    expect(linuxGatewayApi.remove).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })

  it.each([false, true])('checks forwarding and TUN instead of offering gateway forwarding (embedded=%s)', async embedded => {
    const wrapper = await renderPublished(ready(), embedded)
    expect(wrapper.find('section[aria-label="转发与 TUN"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('检查转发与 TUN')
    for (const label of ['检查并预览转发', '应用网关转发', '撤销网关转发', '开启并持久化 IPv4 转发']) expect(button(wrapper, label)).toBeUndefined()
    expect(wrapper.text()).not.toContain('高级选项：手动修改网络')
    expect(wrapper.findComponent({ name: 'GatewayClientInstructions' }).exists()).toBe(false)
    expect(row(wrapper, 'IPv4 转发')).toMatchObject({ status: '已开启并持久化', tone: 'success' })
    expect(row(wrapper, 'Mihomo TUN')).toMatchObject({ status: '运行中 · Meta', tone: 'success' })
    expect(row(wrapper, '旧网关记录')).toBeUndefined()
    expect(wrapper.text()).toContain('主路由转发的网段在「上游路由」管理')
    if (embedded) {
      await button(wrapper, '继续接入主路由').trigger('click')
      expect(wrapper.emitted('completed')).toEqual([[]])
    }
    expect(linuxGatewayApi.preview).not.toHaveBeenCalled()
    expect(linuxGatewayApi.apply).not.toHaveBeenCalled()
  })

  it.each([
    ['current', { current: '0', persisted: false }, '未开启', 'danger'],
    ['persisted', { current: '1', persisted: false, startup_hook: 'absent' }, '已开启，未持久化', 'warning'],
  ] as const)('blocks continuing until forwarding is on and persisted (%s)', async (_case, forwarding, label, tone) => {
    const status = ready({ ipv4_forwarding: { ...ready().ipv4_forwarding, ...forwarding } })
    const wrapper = await renderPublished(status)
    expect(row(wrapper, 'IPv4 转发')).toMatchObject({ status: label, tone })
    expect(button(wrapper, '继续接入主路由')).toBeUndefined()
    expect(wrapper.text()).toContain('转发与 TUN 检查通过后，再接入主路由。')
    vi.mocked(confirm).mockResolvedValueOnce(true)
    vi.mocked(linuxGatewayApi.persistForwarding).mockResolvedValue(response({ success: true, message: 'IPv4 转发已开启并持久化', ipv4_forwarding: ready().ipv4_forwarding, tun_prepare: { level: 'pass', message: '' } }))
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response(ready()))
    await button(wrapper, '开启并持久化 IPv4 转发').trigger('click'); await flushPromises()
    expect(vi.mocked(confirm).mock.calls[0][0]).toContain('/etc/sysctl.d/99-configflow-aio-forward.conf')
    expect(vi.mocked(confirm).mock.calls[0][1]).toMatchObject({ title: '开启 IPv4 转发？', confirmText: '开启并持久化' })
    expect(linuxGatewayApi.persistForwarding).toHaveBeenCalledExactlyOnceWith('new-mihomo', 'office')
    expect(notify.success).toHaveBeenCalledWith('IPv4 转发已开启并持久化。')
    // The real host state is read again instead of assuming success.
    expect(linuxGatewayApi.status).toHaveBeenCalledTimes(2)
    expect(button(wrapper, '继续接入主路由')).toBeTruthy()
  })

  it('does not persist forwarding when the confirmation is cancelled', async () => {
    const wrapper = await renderPublished(ready({ ipv4_forwarding: { ...ready().ipv4_forwarding, persisted: false } }))
    vi.mocked(confirm).mockResolvedValueOnce(false)
    await button(wrapper, '开启并持久化 IPv4 转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.persistForwarding).not.toHaveBeenCalled()
  })

  it.each([
    ['TUN not running', { tun: { configured: true, device: 'Meta', present: false, gateway_marker: false } }, 'Mihomo TUN', '未运行'],
    ['TUN not configured', { tun: { configured: false, device: 'Meta', present: false, gateway_marker: false } }, 'Mihomo TUN', '未启用完整 TUN'],
    ['gateway config still deployed', { enabled_in_config: true }, 'IPv4 转发', '已开启并持久化'],
  ])('keeps the router step blocked when %s', async (_case, changes, label, status) => {
    const wrapper = await renderPublished(ready(changes))
    expect(row(wrapper, label)?.status).toBe(status)
    expect(button(wrapper, '继续接入主路由')).toBeUndefined()
  })

  it('shows a leftover gateway record and keeps the step blocked', async () => {
    const wrapper = await renderPublished(ready({ managed: { active: true, healthy: true, error: '旧规则仍存在' } }), true, 'full_tun', { AgentStatusRow: false })
    expect(row(wrapper, '旧网关记录')).toMatchObject({ status: '仍存在', tone: 'warning' })
    expect(wrapper.text()).toContain('请在配置生成 → Mihomo → Linux 网关中清理。')
    expect(wrapper.text()).toContain('旧规则仍存在')
    expect(button(wrapper, '撤销网关转发')).toBeUndefined()
    expect(button(wrapper, '继续接入主路由')).toBeUndefined()
  })

  it('shows an unreadable gateway record instead of reporting the router step ready', async () => {
    // The setup check refuses managed.error too; the step must not offer to continue.
    const wrapper = await renderPublished(ready({ managed: { active: false, error: '网关状态文件无法读取' } }), true, 'full_tun', { AgentStatusRow: false })
    expect(row(wrapper, '旧网关记录')).toMatchObject({ status: '无法确认', tone: 'warning' })
    expect(wrapper.text()).toContain('网关状态文件无法读取')
    expect(wrapper.text()).not.toContain('转发与 TUN 正常')
    expect(button(wrapper, '继续接入主路由')).toBeUndefined()
  })

  it('asks for an Agent update when the status has no forwarding or TUN fields', async () => {
    const wrapper = await renderPublished({ success: true, supported: true, enabled_in_config: false, managed: { active: false } })
    expect(row(wrapper, 'IPv4 转发')?.status).toBe('需要 Agent 1.8.40-go')
    expect(row(wrapper, 'Mihomo TUN')?.status).toBe('需要 Agent 1.8.40-go')
    expect(button(wrapper, '开启并持久化 IPv4 转发')).toBeUndefined()
    expect(button(wrapper, '继续接入主路由')).toBeUndefined()
  })

  it('names the IP list for router forwarding and keeps a single IP list', async () => {
    vi.mocked(gatewayOnboardingApi.get).mockResolvedValue(response({ ...config(), takeover: 'full_tun' }))
    const wrapper = await render(false, false, 'router')
    await fillEnvironment(wrapper)
    const picker = wrapper.get('[data-gateway-rule-picker]')
    expect(picker.text()).toContain('主路由转发')
    expect(picker.text()).not.toContain('IP 接管')
    expect(picker.text()).toContain('勾选的规则集 IPv4 网段会加入主路由静态路由')
    expect(picker.findAll('[role="tab"]')).toHaveLength(3)
  })

  it('warns about a legacy router split without offering a migration and confirms a breaking revocation', async () => {
    const wrapper = await renderPublished({ success: true, supported: true, enabled_in_config: true, managed: { active: true, healthy: true } }, false, 'legacy')
    expect(wrapper.get('[data-legacy-router-split]').text()).toContain('旧版主路由分流')
    expect(wrapper.get('[data-legacy-router-split]').text()).not.toContain('迁移')
    expect(button(wrapper, '前往上游路由迁移')).toBeUndefined()
    vi.mocked(confirmDanger).mockResolvedValueOnce(false)
    await button(wrapper, '撤销网关转发').trigger('click'); await flushPromises()
    expect(confirmDanger).toHaveBeenCalledWith(expect.stringContaining('这些连接会中断'), expect.objectContaining({ confirmText: '仍然撤销' }))
    expect(linuxGatewayApi.remove).not.toHaveBeenCalled()
    vi.mocked(confirmDanger).mockResolvedValueOnce(true)
    expect(wrapper.text()).toContain('撤销前，先删除主路由上指向这台 Linux 的静态路由。')
    await button(wrapper, '撤销网关转发').trigger('click'); await flushPromises()
    expect(linuxGatewayApi.remove).toHaveBeenCalledExactlyOnceWith('new-mihomo', 'office', true)
    // The router split's clients never changed their gateway; the static routes need handling instead.
    expect(notify.success).toHaveBeenCalledWith('网关转发已撤销，主路由转来的连接已中断。请删除主路由上指向这台 Linux 的静态路由，或重新应用网关转发。')
    expect(wrapper.get('[data-legacy-clients]').text()).toContain('请删除主路由上指向这台 Linux 的静态路由')
    expect(wrapper.text()).not.toContain('恢复客户端')
    expect(wrapper.text()).not.toContain('恢复原网关')
  })

  it('uses the server text after revoking a legacy router split and keeps clients on the main router', async () => {
    const wrapper = await renderPublished({ success: true, supported: true, enabled_in_config: true, managed: { active: true, healthy: true } }, false, 'legacy')
    expect(wrapper.findComponent({ name: 'GatewayClientInstructions' }).exists()).toBe(false)
    expect(wrapper.get('[data-legacy-clients]').text()).toBe('主路由转来的连接由这台 Linux 接管。客户端网关保持主路由，DNS 保持接入主路由时的设置，无需修改。')
    expect(wrapper.text()).not.toContain('可将两项都设为')
    vi.mocked(confirmDanger).mockResolvedValueOnce(true)
    vi.mocked(linuxGatewayApi.remove).mockResolvedValueOnce(response({ success: true, message: '服务端原文', managed: { active: false } }))
    await button(wrapper, '撤销网关转发').trigger('click'); await flushPromises()
    expect(notify.success).toHaveBeenCalledWith('服务端原文')
  })

  it('warns about forward filtering on a full-TUN host without changing the completion check', async () => {
    const wrapper = await renderPublished(ready({ forward_filter: mayDrop() }), true, 'full_tun', { AgentStatusRow: false, ForwardFilterNotice: false })
    expect(row(wrapper, '转发过滤')).toMatchObject({ status: '可能拦截 UDP', tone: 'warning' })
    expect(row(wrapper, 'IPv4 转发')).toMatchObject({ status: '已开启并持久化', tone: 'success' })
    expect(row(wrapper, 'Mihomo TUN')).toMatchObject({ status: '运行中 · Meta', tone: 'success' })
    const labels = wrapper.findAllComponents(AgentStatusRow).map(item => item.props('label'))
    expect(labels.indexOf('转发过滤')).toBe(labels.indexOf('Mihomo TUN') + 1)
    expect(wrapper.text()).toContain(DOCKER_HINT)
    expect(wrapper.get('[data-full-tun-checks] pre').text()).toBe(DOCKER_COMMANDS.join('\n'))
    // Still complete: the warning never blocks the router step.
    expect(wrapper.text()).toContain('转发与 TUN 正常')
    await button(wrapper, '继续接入主路由').trigger('click')
    expect(wrapper.emitted('completed')).toEqual([[]])
    expect(button(wrapper, '开启并持久化 IPv4 转发')).toBeUndefined()
  })

  it.each([
    ['an older Agent', undefined],
    ['ok', quiet('ok')],
    ['unknown', quiet('unknown', 'timeout')],
  ])('adds no forward-filter row for %s', async (_case, forward_filter) => {
    const wrapper = await renderPublished(ready(forward_filter ? { forward_filter } : {}), true, 'full_tun', { AgentStatusRow: false })
    expect(row(wrapper, '转发过滤')).toBeUndefined()
    expect(wrapper.text()).toContain('转发与 TUN 正常')
  })

  it('never shows the forward-filter row on a selective gateway', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('published') }))
    vi.mocked(gatewayOnboardingApi.session).mockResolvedValue(response(session('published')))
    vi.mocked(linuxGatewayApi.status).mockResolvedValue(response({ success: true, enabled_in_config: true, supported: true, managed: { active: true, healthy: true }, forward_filter: mayDrop() }))
    const wrapper = await render(false, false, 'gateway', { AgentStatusRow: false, ForwardFilterNotice: false })
    expect(linuxGatewayApi.status).toHaveBeenCalled()
    expect(row(wrapper, '网关转发')).toBeDefined()
    expect(row(wrapper, '转发过滤')).toBeUndefined()
    expect(wrapper.text()).not.toContain(DOCKER_HINT)
  })

  it('does not show the legacy banner for a selective gateway', async () => {
    vi.mocked(gatewayOnboardingApi.sessions).mockResolvedValue(response({ success: true, session: session('published') }))
    const wrapper = await render()
    expect(wrapper.find('[data-legacy-router-split]').exists()).toBe(false)
    expect(button(wrapper, '检查并预览转发')).toBeTruthy()
  })
})
