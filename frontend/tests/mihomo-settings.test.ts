import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, reactive } from 'vue'
import { agentApi, customConfigApi, mihomoPanelApi, profileApi } from '@/api'
import { invalidateAgentSummaries } from '@/lib/agentSummaries'
import { notify } from '@/lib/feedback'
import { setActiveProfileId } from '@/profileContext'
import MihomoPanel from '@/views/MihomoPanel.vue'
import MihomoSettings from '@/components/generate/MihomoSettings.vue'
import SettingsWorkspace from '@/components/generate/SettingsWorkspace.vue'
import SettingsPublishBar from '@/components/generate/SettingsPublishBar.vue'
import YamlEditor from '@/components/YamlEditor.vue'

const routing = vi.hoisted(() => ({ route: null as any }))
vi.mock('@/router', () => ({ default: { push: vi.fn() } }))
vi.mock('vue-router', async importOriginal => ({
  ...await importOriginal<typeof import('vue-router')>(),
  useRoute: () => routing.route,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() })
}))

const wrappers: VueWrapper[] = []
const device = { id: 'gw', name: 'Gateway', service_type: 'mihomo', status: 'online', profile_id: 'bound' }
const profiles = [{ id: 'sidebar', name: '侧栏配置' }, { id: 'bound', name: '设备配置' }, { id: 'default', name: '默认配置', is_default: true }]
const initialConfig = '# keep this comment\nmixed-port: 7890\n'
const guard = { expected_profile_id: 'bound', expected_service_type: 'mihomo', expected_config_revision: 12 }
const response = (data: any) => ({ data } as any)
function deferred() {
  let resolve!: (value: any) => void
  const promise = new Promise<any>(done => { resolve = done })
  return { promise, resolve }
}
const WorkspaceStub = defineComponent({
  props: ['open', 'dirty', 'busy', 'saveDisabled', 'backLabel'],
  emits: ['save', 'update:open'],
  setup(props, { slots }) {
    return () => h('section', [slots.default?.(), slots.footer?.({ disabled: props.busy || props.saveDisabled })])
  }
})

beforeEach(() => {
  routing.route = reactive({ query: { agent: 'gw' } })
  setActiveProfileId('sidebar')
  vi.spyOn(agentApi, 'getAll').mockResolvedValue(response([device]))
  vi.spyOn(agentApi, 'summaries').mockResolvedValue(response([device]))
  invalidateAgentSummaries()
  vi.spyOn(profileApi, 'list').mockResolvedValue(response(profiles))
  vi.spyOn(mihomoPanelApi, 'version').mockResolvedValue(response({ data: { version: 'v1.19.0' } }))
  vi.spyOn(mihomoPanelApi, 'configs').mockResolvedValue(response({ data: { mode: 'rule' } }))
  vi.spyOn(customConfigApi, 'getMihomo').mockResolvedValue(response({ config: initialConfig }))
  vi.spyOn(customConfigApi, 'saveMihomo').mockResolvedValue(response({ success: true, publish_guard: guard }))
  vi.spyOn(notify, 'success').mockImplementation(() => 0)
  vi.spyOn(notify, 'error').mockImplementation(() => 0)
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
  localStorage.clear()
})

async function panel() {
  const wrapper = shallowMount(MihomoPanel, { global: { stubs: {
    PageHeader: { template: '<header><slot name="title-extra" /><slot name="actions" /></header>' },
    Button: false,
    Primitive: false,
    MihomoSettings: { name: 'MihomoSettings', props: ['open', 'profileId', 'contextLabel', 'preferredAgentId'], template: '<div />' }
  } } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
function settingsButton(wrapper: VueWrapper) {
  return wrapper.findAll('button').find(button => ['设置', '正在打开…'].includes(button.text()))!
}

describe('Mihomo settings entry', () => {
  it('opens from a fresh lightweight lookup without waiting for content comparison', async () => {
    const wrapper = await panel()
    const slowContentComparison = deferred()
    vi.mocked(agentApi.getAll).mockReturnValue(slowContentComparison.promise)
    // The picker cache still contains the old binding at this point.
    vi.mocked(agentApi.summaries).mockResolvedValue(response([{ ...device, profile_id: 'default' }]))
    await settingsButton(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.getComponent({ name: 'MihomoSettings' }).props('profileId')).toBe('default')
    expect(agentApi.getAll).not.toHaveBeenCalled()
    expect(agentApi.summaries).toHaveBeenCalledTimes(2)
  })

  it('re-reads the device binding and opens its profile instead of the sidebar profile', async () => {
    const wrapper = await panel()
    vi.mocked(agentApi.summaries).mockResolvedValue(response([{ ...device, profile_id: 'default' }]))
    await settingsButton(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.getComponent({ name: 'MihomoSettings' }).props()).toMatchObject({
      open: true, profileId: 'default', contextLabel: '默认配置 · Gateway', preferredAgentId: 'gw'
    })
    expect(customConfigApi.saveMihomo).not.toHaveBeenCalled()
  })

  it('supports legacy Mihomo devices and uses the declared default profile', async () => {
    vi.mocked(agentApi.summaries).mockResolvedValue(response([{ ...device, service_type: undefined, profile_id: undefined }]))
    vi.mocked(profileApi.list).mockResolvedValue(response([{ id: 'default', name: '旧默认', is_default: false }, { id: 'bound', name: '新默认', is_default: true }]))
    const wrapper = await panel()
    await settingsButton(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.getComponent({ name: 'MihomoSettings' }).props('profileId')).toBe('bound')
  })

  it('keeps settings available while the Mihomo core is unreachable', async () => {
    vi.mocked(mihomoPanelApi.version).mockRejectedValue(new Error('offline'))
    const wrapper = await panel()
    expect(settingsButton(wrapper).attributes('disabled')).toBeUndefined()
    await settingsButton(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.getComponent({ name: 'MihomoSettings' }).props('profileId')).toBe('bound')
  })

  it('shows a missing binding error without opening another profile, and permits retry', async () => {
    const wrapper = await panel()
    vi.mocked(profileApi.list).mockResolvedValueOnce(response([]))
    await settingsButton(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('找不到此 Agent 绑定的配置空间')
    expect(wrapper.findComponent({ name: 'MihomoSettings' }).exists()).toBe(false)
    await settingsButton(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.getComponent({ name: 'MihomoSettings' }).props('profileId')).toBe('bound')
  })

  it.each(['device change', 'unmount'])('ignores a pending settings lookup after %s', async action => {
    const wrapper = await panel()
    const pending = deferred()
    vi.mocked(profileApi.list).mockReturnValueOnce(pending.promise)
    await settingsButton(wrapper).trigger('click')
    expect(settingsButton(wrapper).attributes('disabled')).toBeDefined()
    if (action === 'device change') routing.route.query.agent = 'other'
    else wrapper.unmount()
    await flushPromises()
    pending.resolve(response(profiles))
    await flushPromises()
    expect(wrapper.findComponent({ name: 'MihomoSettings' }).exists()).toBe(false)
  })
})

async function editor() {
  const wrapper = shallowMount(MihomoSettings, {
    props: { open: true, profileId: 'bound', contextLabel: '设备配置 · Gateway' },
    global: { stubs: { SettingsWorkspace: WorkspaceStub, Button: false, Primitive: false } }
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

describe('Mihomo settings editor', () => {
  it('blocks saving an invalid range while keeping the unsaved input protected', async () => {
    const wrapper = await editor()
    const control = wrapper.getComponent({ name: 'MihomoIPv6Settings' })
    control.vm.$emit('validation', { invalid: true, dirty: true })
    await flushPromises()
    expect(wrapper.getComponent(SettingsWorkspace).props()).toMatchObject({ dirty: true, saveDisabled: true, busy: false })
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
    expect(await wrapper.getComponent(SettingsPublishBar).props('save')()).toBeNull()
    expect(customConfigApi.saveMihomo).not.toHaveBeenCalled()
    control.vm.$emit('validation', { invalid: false, dirty: false })
    await flushPromises()
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(false)
  })

  it('passes the saved draft and server context to the base settings form, which can block saving', async () => {
    const context = { takeover: 'full_tun', reserved_ports: [{ port: 8080, owner: 'Mihomo Agent「gw」的端口' }], local_addresses: ['10.0.0.5'] }
    vi.mocked(customConfigApi.getMihomo).mockResolvedValueOnce(response({ config: initialConfig, base_settings: context }))
    const wrapper = await editor()
    const form = wrapper.getComponent({ name: 'MihomoBaseSettings' })
    expect(form.props()).toMatchObject({ modelValue: initialConfig, savedContent: initialConfig, context, open: true })
    form.vm.$emit('validation', { invalid: true, dirty: false })
    await flushPromises()
    expect(wrapper.getComponent(SettingsWorkspace).props('saveDisabled')).toBe(true)
    expect(await wrapper.getComponent(SettingsPublishBar).props('save')()).toBeNull()
    expect(customConfigApi.saveMihomo).not.toHaveBeenCalled()
  })

  it('saves a zero sync interval independently of the YAML and marks only that edit dirty', async () => {
    const wrapper = await editor()
    const input = wrapper.getComponent({ name: 'MihomoSyncInterval' })
    input.vm.$emit('update:modelValue', 0)
    await flushPromises()
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(true)
    await wrapper.getComponent(SettingsPublishBar).props('save')()
    expect(customConfigApi.saveMihomo).toHaveBeenCalledExactlyOnceWith({ config: initialConfig, provider_update_interval: 0 }, 'bound')
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(false)
  })
  it('preserves YAML text and saves to the bound profile before returning the publish receipt', async () => {
    const wrapper = await editor()
    expect(customConfigApi.getMihomo).toHaveBeenCalledExactlyOnceWith('bound')
    expect(wrapper.getComponent(YamlEditor).props('modelValue')).toBe(initialConfig)
    expect(wrapper.getComponent(SettingsWorkspace).props('backLabel')).toBe('返回 Mihomo 面板')
    const text = `${initialConfig}\n# preserve spacing\n  \n`
    wrapper.getComponent(YamlEditor).vm.$emit('update:modelValue', text)
    await flushPromises()
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(true)
    const receipt = await wrapper.getComponent(SettingsPublishBar).props('save')()
    expect(receipt).toEqual(guard)
    expect(customConfigApi.saveMihomo).toHaveBeenCalledExactlyOnceWith({ config: text, provider_update_interval: 3600 }, 'bound')
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(false)
    expect(wrapper.emitted('update:open')).toBeUndefined()
  })

  it('retains an edited draft on save failure, then keeps the saved editor open for further edits', async () => {
    const wrapper = await editor()
    wrapper.getComponent(YamlEditor).vm.$emit('update:modelValue', '')
    await flushPromises()
    vi.mocked(customConfigApi.saveMihomo).mockRejectedValueOnce(new Error('invalid YAML'))
    wrapper.getComponent(SettingsPublishBar).vm.$emit('save-draft')
    await flushPromises()
    expect(wrapper.getComponent(SettingsWorkspace).props()).toMatchObject({ dirty: true, busy: false })
    expect(wrapper.getComponent(YamlEditor).props('modelValue')).toBe('')
    expect(wrapper.emitted('update:open')).toBeUndefined()
    wrapper.getComponent(SettingsPublishBar).vm.$emit('save-draft')
    await flushPromises()
    expect(customConfigApi.saveMihomo).toHaveBeenLastCalledWith({ config: '', provider_update_interval: 3600 }, 'bound')
    expect(wrapper.emitted('update:open')).toBeUndefined()
    expect(wrapper.getComponent(SettingsWorkspace).props()).toMatchObject({ open: true, dirty: false, busy: false })
    expect(notify.success).toHaveBeenCalledWith('基础配置草稿已保存')
    wrapper.getComponent(YamlEditor).vm.$emit('update:modelValue', '# next edit\n')
    await flushPromises()
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(true)
    wrapper.getComponent(SettingsPublishBar).vm.$emit('save-draft')
    await flushPromises()
    expect(customConfigApi.saveMihomo).toHaveBeenLastCalledWith({ config: '# next edit\n', provider_update_interval: 3600 }, 'bound')
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(false)
    expect(customConfigApi.getMihomo).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('update:open')).toBeUndefined()
    wrapper.getComponent(SettingsWorkspace).vm.$emit('update:open', false)
    expect(wrapper.emitted('update:open')).toEqual([[false]])
  })

  it('blocks saving after a failed read and offers a retry', async () => {
    vi.mocked(customConfigApi.getMihomo).mockRejectedValueOnce(new Error('read failed'))
    const wrapper = await editor()
    expect(wrapper.get('[role="alert"]').text()).toContain('read failed')
    expect(wrapper.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
    expect(await wrapper.getComponent(SettingsPublishBar).props('save')()).toBeNull()
    expect(customConfigApi.saveMihomo).not.toHaveBeenCalled()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(YamlEditor).props('modelValue')).toBe(initialConfig)
  })

  it('does not return a publish receipt after the editor unmounts, or start duplicate saves', async () => {
    const wrapper = await editor()
    const pending = deferred()
    vi.mocked(customConfigApi.saveMihomo).mockReturnValueOnce(pending.promise)
    const save = wrapper.getComponent(SettingsPublishBar).props('save')
    const first = save()
    expect(await save()).toBeNull()
    wrapper.unmount()
    pending.resolve(response({ publish_guard: guard }))
    expect(await first).toBeNull()
    expect(customConfigApi.saveMihomo).toHaveBeenCalledTimes(1)
    expect(notify.success).not.toHaveBeenCalled()
  })
})
