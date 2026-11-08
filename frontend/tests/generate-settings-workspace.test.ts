import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import api, { customConfigApi, proxyGroupApi, ruleApi, ruleSetApi } from '@/api'
import Generate from '@/views/Generate.vue'
import GenerateStudio from '@/components/generate/GenerateStudio.vue'
import SettingsWorkspace from '@/components/generate/SettingsWorkspace.vue'
import SettingsPublishBar from '@/components/generate/SettingsPublishBar.vue'
import OxidnsSettings from '@/components/generate/OxidnsSettings.vue'
import type { AgentSettingsContext } from '@/lib/agentSettings'
import { notify } from '@/lib/feedback'
import { activeProfileId, scopedRequests, setActiveProfileId } from '@/profileContext'

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))

const originalAdapter = api.defaults.adapter
const originalClipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
const originalCopyCommand = Object.getOwnPropertyDescriptor(document, 'execCommand')
const wrappers: ReturnType<typeof shallowMount>[] = []
const initialConfig = '# Keep this comment\nmixed-port: 7890\n'
const guard = (service: string, revision = 12) => ({
  expected_profile_id: 'workspace-profile', expected_service_type: service, expected_config_revision: revision
})
const WorkspaceStub = defineComponent({
  name: 'SettingsWorkspace',
  props: ['open', 'title', 'description', 'dirty', 'busy', 'saveDisabled', 'size', 'saveLabel', 'backLabel'],
  emits: ['save', 'update:open'],
  setup(props, { slots }) {
    return () => h('section', [slots.default?.(), slots.footer?.({ disabled: !!(props.busy || props.saveDisabled) })])
  }
})

describe('Agent settings use the bound space without switching the global space', () => {
  const targets = { agents: [{ id: 'bedroom-agent', name: '卧室设备', profile_id: 'bedroom' }], profiles: [{ id: 'bedroom', name: '卧室空间' }] }
  const context = (serviceType: AgentSettingsContext['serviceType']): AgentSettingsContext => ({
    agentId: 'bedroom-agent', agentName: '卧室设备', profileId: 'bedroom', profileName: '卧室空间', serviceType, targets,
  })
  const renderAgentSettings = (service: AgentSettingsContext['serviceType']) => {
    const wrapper = shallowMount(Generate, {
      props: { agentSettings: context(service) }, global: { stubs: { SettingsWorkspace: WorkspaceStub } },
    })
    wrappers.push(wrapper)
    return wrapper
  }

  it.each([
    ['mihomo', 'getMihomo', 'saveMihomo'], ['surge', 'getSurge', 'saveSurge'], ['mosdns', 'getMosdns', 'saveMosdns'],
  ] as const)('loads and saves %s to its captured space even if the global space changes', async (service, read, save) => {
    const get = vi.spyOn(api, 'get')
    const post = vi.spyOn(api, 'post')
    const wrapper = renderAgentSettings(service)
    await flushPromises()
    expect(wrapper.findComponent(GenerateStudio).exists()).toBe(false)
    expect(customConfigApi[read]).toHaveBeenCalledExactlyOnceWith('bedroom')
    expect(activeProfileId.value).toBe('workspace-profile')
    expect(wrapper.emitted('ready')).toHaveLength(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    const workspace = wrapper.findAllComponents(SettingsWorkspace).find(item => item.props('open'))!
    expect(workspace.props('backLabel')).toBe('返回 Agent 列表')
    expect(workspace.text()).toContain('卧室空间')
    expect(workspace.text()).toContain('共用此配置')
    expect(workspace.getComponent(SettingsPublishBar).props()).toMatchObject({ profileId: 'bedroom', preferredAgentId: 'bedroom-agent', initialTargets: targets, serviceType: service })
    setActiveProfileId('another-space')
    workspace.vm.$emit('save')
    await flushPromises()
    if (service === 'mosdns') {
      expect(ruleApi.getAll).toHaveBeenCalledWith('bedroom')
      expect(ruleSetApi.getAll).toHaveBeenCalledWith('bedroom')
      for (const call of get.mock.calls.filter(([url]) => url.startsWith('/mosdns/'))) {
        expect(call[1]).toEqual({ headers: { 'X-ConfigFlow-Profile': 'bedroom' } })
      }
      expect(customConfigApi.saveMosdns).not.toHaveBeenCalled()
      expect(post).toHaveBeenCalledExactlyOnceWith('/mosdns/settings', expect.objectContaining({
        custom_config: initialConfig,
      }), { headers: { 'X-ConfigFlow-Profile': 'bedroom' } })
    } else {
      expect(customConfigApi[save]).toHaveBeenCalledExactlyOnceWith({
        config: initialConfig, ...(service === 'mihomo' ? { provider_update_interval: 3600 } : {}),
      }, 'bedroom')
    }
    expect(activeProfileId.value).toBe('another-space')
    if (service === 'mihomo') {
      expect(wrapper.emitted('close')).toBeUndefined()
      expect(workspace.props()).toMatchObject({ open: true, dirty: false, busy: false })
      workspace.vm.$emit('update:open', false)
      await flushPromises()
    }
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('passes the same fixed context and target into the OxiDNS editor', async () => {
    const wrapper = renderAgentSettings('oxidns')
    await flushPromises()
    const editor = wrapper.getComponent(OxidnsSettings)
    expect(editor.props()).toMatchObject({ open: true, profileId: 'bedroom', preferredAgentId: 'bedroom-agent', initialTargets: targets, backLabel: '返回 Agent 列表' })
    expect(editor.props('contextLabel')).toContain('卧室空间')
    setActiveProfileId('another-space')
    await nextTick()
    expect(editor.props('profileId')).toBe('bedroom')
    editor.vm.$emit('update:open', false)
    await nextTick()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('requests every MosDNS settings part at once and opens after all of them arrive', async () => {
    const pending = deferred<any>()
    vi.mocked(customConfigApi.getMosdns).mockReturnValueOnce(pending.promise)
    const get = vi.spyOn(api, 'get')
    const wrapper = renderAgentSettings('mosdns')
    await flushPromises()
    expect(ruleApi.getAll).toHaveBeenCalledWith('bedroom')
    expect(ruleSetApi.getAll).toHaveBeenCalledWith('bedroom')
    expect(get.mock.calls.map(([url]) => url).filter(url => url.startsWith('/mosdns/')).sort()).toEqual([
      '/mosdns/api-settings', '/mosdns/cache-settings', '/mosdns/custom-matches',
      '/mosdns/dns-servers', '/mosdns/log-settings', '/mosdns/rulesets',
    ])
    expect(wrapper.emitted('ready')).toBeUndefined()
    expect(wrapper.findAllComponents(SettingsWorkspace).some(item => item.props('open'))).toBe(false)
    pending.resolve({ data: { config: initialConfig } })
    await flushPromises()
    expect(wrapper.emitted('ready')).toHaveLength(1)
    expect((wrapper.vm as any).mosdnsCustomConfig).toBe(initialConfig)
  })

  it('does not open MosDNS settings or keep partial values when one part fails', async () => {
    const adapter = api.defaults.adapter as (config: any) => Promise<any>
    api.defaults.adapter = async config => {
      if (config.url === '/mosdns/dns-servers') throw new Error('连接失败')
      if (config.url === '/mosdns/log-settings') return { ...(await adapter(config)), data: { log_level: 'debug' } }
      return adapter(config)
    }
    const wrapper = renderAgentSettings('mosdns')
    await flushPromises()
    expect(wrapper.emitted('ready')).toBeUndefined()
    expect(wrapper.findAllComponents(SettingsWorkspace).some(item => item.props('open'))).toBe(false)
    expect(notify.error).toHaveBeenCalledWith('加载 MosDNS 设置失败')
    expect((wrapper.vm as any).mosdnsCustomConfig).toBe('')
    expect((wrapper.vm as any).mosdnsLogLevel).toBe('info')
  })

  it('ends a failed initial load without opening an empty editable draft', async () => {
    vi.mocked(customConfigApi.getMihomo).mockRejectedValueOnce(new Error('missing profile'))
    const wrapper = renderAgentSettings('mihomo')
    await flushPromises()
    expect(wrapper.emitted('ready')).toBeUndefined()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.findAllComponents(SettingsWorkspace).some(item => item.props('open'))).toBe(false)
    expect(notify.error).toHaveBeenCalledWith('加载自定义配置失败')
  })

  it('does not reopen or report ready when loading finishes after leaving Agents', async () => {
    const pending = deferred<any>()
    vi.mocked(customConfigApi.getMihomo).mockReturnValueOnce(pending.promise)
    const wrapper = renderAgentSettings('mihomo')
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    pending.resolve({ data: { config: initialConfig } })
    await flushPromises()
    expect(wrapper.emitted('ready')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
  })
})
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

beforeEach(() => {
  setActiveProfileId('workspace-profile')
  scopedRequests.value = 0
  api.defaults.adapter = async config => ({ config, data: { publish_guard: guard('mosdns') }, status: 200, statusText: 'OK', headers: {} })
  for (const method of ['getMihomo', 'getSurge', 'getLoon', 'getMosdns'] as const) {
    vi.spyOn(customConfigApi, method).mockResolvedValue({ data: { config: initialConfig, smart_groups: [{ group_id: 'group-1', policy_priority: 'HK:0' }] } } as any)
  }
  for (const method of ['saveMihomo', 'saveSurge', 'saveLoon', 'saveMosdns'] as const) {
    vi.spyOn(customConfigApi, method).mockResolvedValue({ data: { success: true, publish_guard: guard(method.slice(4).toLowerCase()) } } as any)
  }
  vi.spyOn(ruleApi, 'getAll').mockResolvedValue({ data: [] } as any)
  vi.spyOn(ruleSetApi, 'getAll').mockResolvedValue({ data: [] } as any)
  vi.spyOn(proxyGroupApi, 'getAll').mockResolvedValue({ data: [{ id: 'group-1', name: 'Group 1', type: 'url-test' }] } as any)
  vi.spyOn(notify, 'error').mockImplementation(() => 0)
  vi.spyOn(notify, 'success').mockImplementation(() => 0)
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  api.defaults.adapter = originalAdapter
  scopedRequests.value = 0
  sessionStorage.clear()
  localStorage.clear()
  vi.restoreAllMocks()
  if (originalClipboard) Object.defineProperty(navigator, 'clipboard', originalClipboard)
  else delete (navigator as any).clipboard
  if (originalCopyCommand) Object.defineProperty(document, 'execCommand', originalCopyCommand)
  else delete (document as any).execCommand
})

it.each([true, false])('reports subscription and preview copy accurately on HTTP (accepted=%s)', async accepted => {
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
  const fallback = vi.fn(() => accepted)
  Object.defineProperty(document, 'execCommand', { configurable: true, value: fallback })
  const wrapper = shallowMount(Generate)
  wrappers.push(wrapper)
  await flushPromises()
  const url = 'http://example.test/api/config/workspace-profile/mihomo'
  wrapper.getComponent(GenerateStudio).vm.$emit('copy', url, 'Mihomo')
  await flushPromises()
  expect(fallback).toHaveBeenCalledOnce()
  expect(vi.mocked(notify.success).mock.calls.some(([message]) => String(message).includes('订阅链接已复制'))).toBe(accepted)
  expect(vi.mocked(notify.error).mock.calls.some(([message]) => String(message).includes('复制失败'))).toBe(!accepted)
  vi.mocked(notify.success).mockClear()
  vi.mocked(notify.error).mockClear()
  const draft = wrapper.vm as any
  draft.previewContent = initialConfig
  await draft.copyPreviewContent()
  expect(fallback).toHaveBeenCalledTimes(2)
  expect(notify.success).toHaveBeenCalledTimes(accepted ? 1 : 0)
  expect(notify.error).toHaveBeenCalledTimes(accepted ? 0 : 1)
  expect(draft.previewContent).toBe(initialConfig)
})

async function openSettings(target: string, label: string) {
  const wrapper = shallowMount(Generate, { global: { stubs: { SettingsWorkspace: WorkspaceStub } } })
  wrappers.push(wrapper)
  await flushPromises()
  const action = wrapper.getComponent(GenerateStudio).props('targets')
    .find((item: any) => item.key === target)!.actions.find((item: any) => item.label === label)!
  await action.run()
  await flushPromises()
  const workspace = wrapper.findAllComponents(SettingsWorkspace).find(item => item.props('open'))!
  expect(workspace).toBeDefined()
  return { wrapper, workspace, draft: wrapper.vm as any }
}

describe('generation settings workspace integration', () => {
  it('loads and saves the independent Mihomo provider sync interval including zero', async () => {
    vi.mocked(customConfigApi.getMihomo).mockResolvedValueOnce({ data: { config: initialConfig, provider_update_interval: 7200 } } as any)
    const { workspace, draft } = await openSettings('mihomo', '基础配置')
    expect(draft.mihomoSyncInterval).toBe(7200)
    draft.mihomoSyncInterval = 0
    await nextTick()
    expect(workspace.props('dirty')).toBe(true)
    await workspace.getComponent(SettingsPublishBar).props('save')()
    expect(customConfigApi.saveMihomo).toHaveBeenCalledExactlyOnceWith({ config: initialConfig, provider_update_interval: 0 }, 'workspace-profile')
    expect(workspace.props('dirty')).toBe(false)
  })
  it('submits the whole MosDNS draft in one request and returns that commit receipt', async () => {
    const { workspace, draft } = await openSettings('mosdns', '设置')
    draft.mosdnsCustomHosts = 'example.test 192.0.2.1'
    draft.mosdnsLogFile = 'new.log'
    const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { success: true, publish_guard: guard('mosdns', 27) } } as any)
    expect(await workspace.getComponent(SettingsPublishBar).props('save')()).toEqual(guard('mosdns', 27))
    expect(post).toHaveBeenCalledExactlyOnceWith('/mosdns/settings', expect.objectContaining({
      custom_config: initialConfig, custom_hosts: 'example.test 192.0.2.1', log_file: 'new.log'
    }), { headers: { 'X-ConfigFlow-Profile': 'workspace-profile' } })
    expect(customConfigApi.saveMosdns).not.toHaveBeenCalled()
  })

  it.each([
    ['mihomo', 'saveMihomo'], ['surge', 'saveSurge'], ['loon', 'saveLoon']
  ] as const)('%s keeps configuration text intact and restores clean state after reverting', async (target, saveMethod) => {
    const { workspace, draft } = await openSettings(target, '基础配置')
    expect(workspace.props()).toMatchObject({ size: 'wide', dirty: false, busy: false })
    draft.customConfigContent = `${initialConfig}\n# Edited comment\n`
    await nextTick()
    expect(workspace.props('dirty')).toBe(true)
    draft.customConfigContent = initialConfig
    await nextTick()
    expect(workspace.props('dirty')).toBe(false)
    const edited = '  # Keep whitespace\n\ncustom: value\n'
    draft.customConfigContent = edited
    await nextTick()
    workspace.vm.$emit('save')
    await flushPromises()
    expect(customConfigApi[saveMethod]).toHaveBeenCalledExactlyOnceWith({ config: edited, ...(target === 'mihomo' ? { provider_update_interval: 3600 } : {}) }, 'workspace-profile')
    expect(workspace.props('open')).toBe(target === 'mihomo')
    expect(workspace.props('dirty')).toBe(false)
  })

  it('keeps the basic draft open and dirty after a save failure, then permits retry', async () => {
    const { workspace, draft } = await openSettings('mihomo', '基础配置')
    vi.mocked(customConfigApi.saveMihomo).mockRejectedValueOnce(new Error('network unavailable'))
    draft.customConfigContent = ''
    await nextTick()
    workspace.vm.$emit('save')
    await flushPromises()
    expect(workspace.props()).toMatchObject({ open: true, dirty: true, busy: false })
    expect(draft.customConfigContent).toBe('')
    // The server's reason (e.g. a port conflict) is shown with the failure.
    expect(notify.error).toHaveBeenCalledWith('保存基础配置失败', 'network unavailable')
    workspace.vm.$emit('save')
    await flushPromises()
    expect(customConfigApi.saveMihomo).toHaveBeenLastCalledWith({ config: '', provider_update_interval: 3600 }, 'workspace-profile')
    expect(workspace.props()).toMatchObject({ open: true, dirty: false, busy: false })
    expect(notify.success).toHaveBeenCalledWith('基础配置草稿已保存')
    draft.customConfigContent = '# continue editing\n'
    await nextTick()
    expect(workspace.props('dirty')).toBe(true)
    workspace.getComponent(SettingsPublishBar).vm.$emit('save-draft')
    await flushPromises()
    expect(customConfigApi.saveMihomo).toHaveBeenLastCalledWith({ config: '# continue editing\n', provider_update_interval: 3600 }, 'workspace-profile')
    expect(workspace.props()).toMatchObject({ open: true, dirty: false, busy: false })
    expect(customConfigApi.getMihomo).toHaveBeenCalledTimes(1)
    workspace.vm.$emit('update:open', false)
    await nextTick()
    expect(workspace.props('open')).toBe(false)
  })

  it('tracks every MosDNS draft section without treating tab navigation as an edit', async () => {
    const { workspace, draft } = await openSettings('mosdns', '设置')
    expect(workspace.props()).toMatchObject({ size: 'form', dirty: false })
    draft.mosdnsActiveTab = 'cache'
    await nextTick()
    expect(workspace.props('dirty')).toBe(false)
    const changes: Record<string, unknown> = {
      mosdnsDirectRulesets: ['direct-set'], mosdnsProxyRulesets: ['proxy-set'],
      mosdnsDirectRules: ['direct-rule'], mosdnsProxyRules: ['proxy-rule'],
      mosdnsCustomMatches: [{ id: 'match', enabled: true, matchesText: 'qname example.org', exec: 'reject 0' }],
      mosdnsCustomMatchPosition: 'head',
      mosdnsLocalDnsEntries: [{ id: 'local', mode: 'simple', addr: '223.5.5.5', bootstrap: '', enable_pipeline: false }],
      mosdnsRemoteDnsEntries: [{ id: 'remote', mode: 'yaml', yaml_config: '- addr: [' }],
      mosdnsFallbackDnsEntries: [{ id: 'fallback', mode: 'simple', addr: '1.1.1.1', bootstrap: '', enable_pipeline: true }],
      mosdnsDefaultForward: 'forward_local', mosdnsCustomHosts: 'example.org 127.0.0.1',
      mosdnsLogEnabled: false, mosdnsLogLevel: 'error', mosdnsLogFile: './edited.log',
      mosdnsApiEnabled: false, mosdnsApiAddress: '127.0.0.1:8339',
      mosdnsCacheEnabled: false, mosdnsCacheSize: 20480, mosdnsCacheLazyTtl: 1800,
      mosdnsCacheDumpEnabled: false, mosdnsCacheDumpFile: './edited.dump', mosdnsCacheDumpInterval: 600
    }
    for (const [field, value] of Object.entries(changes)) {
      const original = draft[field]
      draft[field] = value
      await nextTick()
      expect(workspace.props('dirty'), field).toBe(true)
      draft[field] = original
      await nextTick()
      expect(workspace.props('dirty'), `${field} reverted`).toBe(false)
    }
    expect(customConfigApi.saveMosdns).not.toHaveBeenCalled()
  })

  it('retains the whole MosDNS draft on save failure and closes only after a successful retry', async () => {
    const { workspace, draft } = await openSettings('mosdns', '设置')
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const post = vi.spyOn(api, 'post').mockImplementation(async (url: string) => {
      if (url === '/mosdns/settings') throw new Error('save failed')
      return { data: { success: true } } as any
    })
    draft.mosdnsCustomHosts = 'example.org 127.0.0.1'
    await nextTick()
    workspace.vm.$emit('save')
    await flushPromises()
    expect(workspace.props()).toMatchObject({ open: true, dirty: true, busy: false })
    expect(draft.mosdnsCustomHosts).toBe('example.org 127.0.0.1')
    post.mockResolvedValue({ data: { success: true } } as any)
    workspace.vm.$emit('save')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/mosdns/settings', expect.objectContaining({ custom_hosts: 'example.org 127.0.0.1' }), { headers: { 'X-ConfigFlow-Profile': 'workspace-profile' } })
    expect(workspace.props('open')).toBe(false)
  })

  it('keeps Smart edits and the existing filtered save payload', async () => {
    const { workspace, draft } = await openSettings('surge', 'Smart')
    expect(workspace.props()).toMatchObject({ size: 'compact', dirty: false })
    draft.surgeSmartGroups[0].policy_priority = 'US:1'
    await nextTick()
    expect(workspace.props('dirty')).toBe(true)
    draft.surgeSmartGroups[0].policy_priority = 'HK:0'
    await nextTick()
    expect(workspace.props('dirty')).toBe(false)
    draft.surgeSmartGroups.push({ group_id: '', policy_priority: '' })
    await nextTick()
    expect(workspace.props('dirty')).toBe(true)
    vi.mocked(customConfigApi.saveSurge).mockRejectedValueOnce(new Error('save failed'))
    workspace.vm.$emit('save')
    await flushPromises()
    expect(workspace.props()).toMatchObject({ open: true, dirty: true, busy: false })
    workspace.vm.$emit('save')
    await flushPromises()
    expect(customConfigApi.saveSurge).toHaveBeenLastCalledWith({ smart_groups: [{ group_id: 'group-1', policy_priority: 'HK:0' }] }, 'workspace-profile')
    expect(workspace.props('open')).toBe(false)
  })

  it.each([
    ['mihomo', '基础配置', 'saveMihomo'],
    ['surge', '基础配置', 'saveSurge'],
    ['surge', 'Smart', 'saveSurge']
  ] as const)('%s %s saves the draft before handing its receipt to publishing', async (target, label, saveMethod) => {
    const { workspace, draft } = await openSettings(target, label)
    const publish = workspace.getComponent(SettingsPublishBar)
    expect(publish.props()).toMatchObject({ open: true, profileId: 'workspace-profile', serviceType: target, disabled: false })
    if (label === 'Smart') draft.surgeSmartGroups[0].policy_priority = 'US:1'
    else draft.customConfigContent = 'edited: true\n'
    await nextTick()
    expect(workspace.props('dirty')).toBe(true)
    const receipt = await publish.props('save')()
    await nextTick()
    expect(receipt).toEqual(guard(target))
    expect(workspace.props()).toMatchObject({ open: true, dirty: false, busy: false })
    expect(customConfigApi[saveMethod]).toHaveBeenCalledTimes(1)
    publish.vm.$emit('save-draft')
    await flushPromises()
    expect(workspace.props('open')).toBe(target === 'mihomo')
  })

  it('blocks a locally invalid IPv6 range in the generate editor before any save or publish', async () => {
    const { workspace } = await openSettings('mihomo', '基础配置')
    const control = workspace.getComponent({ name: 'MihomoIPv6Settings' })
    control.vm.$emit('validation', { invalid: true, dirty: true })
    await nextTick()
    expect(workspace.props()).toMatchObject({ dirty: true, saveDisabled: true, busy: false })
    expect(workspace.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
    expect(await workspace.getComponent(SettingsPublishBar).props('save')()).toBeNull()
    expect(customConfigApi.saveMihomo).not.toHaveBeenCalled()
    control.vm.$emit('validation', { invalid: false, dirty: false })
    await nextTick()
    expect(workspace.getComponent(SettingsPublishBar).props('disabled')).toBe(false)
  })

  it('shows the base settings form and blocks saving while it reports a problem', async () => {
    const { workspace } = await openSettings('mihomo', '基础配置')
    const form = workspace.getComponent({ name: 'MihomoBaseSettings' })
    expect(form.props()).toMatchObject({ open: true })
    form.vm.$emit('validation', { invalid: true, dirty: false })
    await nextTick()
    expect(workspace.props('saveDisabled')).toBe(true)
    expect(await workspace.getComponent(SettingsPublishBar).props('save')()).toBeNull()
    expect(customConfigApi.saveMihomo).not.toHaveBeenCalled()
    form.vm.$emit('validation', { invalid: false, dirty: false })
    await nextTick()
    expect(workspace.props('saveDisabled')).toBe(false)
  })

  it('keeps Loon as draft-only because it has no publish target', async () => {
    const { workspace } = await openSettings('loon', '基础配置')
    expect(workspace.props('saveLabel')).toBe('保存草稿')
    expect(workspace.findComponent(SettingsPublishBar).exists()).toBe(false)
  })

  it('waits for the complete MosDNS save before returning a publish receipt', async () => {
    const { workspace } = await openSettings('mosdns', '设置')
    const pending = deferred<any>()
    const post = vi.spyOn(api, 'post').mockReturnValueOnce(pending.promise)
    const saving = workspace.getComponent(SettingsPublishBar).props('save')()
    await nextTick()
    expect(workspace.props('busy')).toBe(true)
    pending.resolve({ data: { success: true, publish_guard: guard('mosdns', 27) } })
    const receipt = await saving
    await nextTick()
    expect(receipt).toEqual(guard('mosdns', 27))
    expect(customConfigApi.saveMosdns).not.toHaveBeenCalled()
    expect(post.mock.calls.map(([url]) => url)).toEqual(['/mosdns/settings'])
    expect(workspace.props()).toMatchObject({ open: true, dirty: false, busy: false })
  })

  it('does not permit publishing after the complete MosDNS save fails', async () => {
    const { workspace, draft } = await openSettings('mosdns', '设置')
    vi.spyOn(console, 'error').mockImplementation(() => {})
    draft.mosdnsCustomHosts = 'example.org 127.0.0.1'
    const post = vi.spyOn(api, 'post').mockRejectedValueOnce(new Error('save failed'))
    expect(await workspace.getComponent(SettingsPublishBar).props('save')()).toBeNull()
    await nextTick()
    expect(workspace.props()).toMatchObject({ open: true, dirty: true, busy: false })
    expect(post).toHaveBeenCalledTimes(1)
    expect(customConfigApi.saveMosdns).not.toHaveBeenCalled()
  })

  it('rejects duplicate saves and tracks only the submitted draft as saved', async () => {
    const { workspace, draft } = await openSettings('mihomo', '基础配置')
    const pending = deferred<any>()
    vi.mocked(customConfigApi.saveMihomo).mockReturnValueOnce(pending.promise)
    const save = workspace.getComponent(SettingsPublishBar).props('save')
    draft.customConfigContent = 'submitted: true\n'
    const first = save()
    await nextTick()
    expect(workspace.props('busy')).toBe(true)
    expect(workspace.getComponent(SettingsPublishBar).props('disabled')).toBe(true)
    expect(await save()).toBeNull()
    expect(customConfigApi.saveMihomo).toHaveBeenCalledTimes(1)
    draft.customConfigContent = 'later: true\n'
    pending.resolve({ data: { publish_guard: guard('mihomo') } })
    expect(await first).toEqual(guard('mihomo'))
    await nextTick()
    expect(workspace.props()).toMatchObject({ open: true, dirty: true, busy: false })
    expect(customConfigApi.saveMihomo).toHaveBeenCalledWith({ config: 'submitted: true\n', provider_update_interval: 3600 }, 'workspace-profile')
  })

  it('does not publish a late save response into a reopened editor session', async () => {
    const { workspace, draft } = await openSettings('mihomo', '基础配置')
    const pending = deferred<any>()
    vi.mocked(customConfigApi.saveMihomo).mockReturnValueOnce(pending.promise)
    draft.customConfigContent = 'first: true\n'
    const savedContent = draft.customConfigSavedContent
    const saving = workspace.getComponent(SettingsPublishBar).props('save')()
    draft.customConfigDialogVisible = false
    draft.customConfigDialogVisible = true
    draft.customConfigContent = 'second: true\n'
    pending.resolve({ data: { publish_guard: guard('mihomo') } })
    expect(await saving).toBeNull()
    await nextTick()
    expect(draft.customConfigSavedContent).toBe(savedContent)
    expect(draft.customConfigContent).toBe('second: true\n')
    expect(workspace.props()).toMatchObject({ open: true, dirty: true, busy: false })
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('does not publish a completed MosDNS save after the page unmounts', async () => {
    const { wrapper, workspace } = await openSettings('mosdns', '设置')
    const pending = deferred<any>()
    const post = vi.spyOn(api, 'post').mockReturnValueOnce(pending.promise)
    const saving = workspace.getComponent(SettingsPublishBar).props('save')()
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    pending.resolve({ data: { publish_guard: guard('mosdns') } })
    expect(await saving).toBeNull()
    expect(post).toHaveBeenCalledTimes(1)
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('keeps draft saving available with an older server but never manufactures a publish receipt', async () => {
    const { workspace } = await openSettings('mihomo', '基础配置')
    vi.mocked(customConfigApi.saveMihomo).mockResolvedValue({ data: { success: true } } as any)
    expect(await workspace.getComponent(SettingsPublishBar).props('save')()).toBeNull()
    await nextTick()
    expect(workspace.props()).toMatchObject({ open: true, dirty: false, busy: false })
    workspace.vm.$emit('save')
    await flushPromises()
    expect(workspace.props()).toMatchObject({ open: true, dirty: false, busy: false })
  })

})
