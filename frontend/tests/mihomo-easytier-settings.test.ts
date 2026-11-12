import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, shallowMount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { customConfigApi } from '@/api'
import { notify } from '@/lib/feedback'
import MihomoSettings from '@/components/generate/MihomoSettings.vue'
import MihomoEasyTierSettings from '@/components/generate/MihomoEasyTierSettings.vue'
import SettingsWorkspace from '@/components/generate/SettingsWorkspace.vue'
import SettingsPublishBar from '@/components/generate/SettingsPublishBar.vue'
import { EASYTIER_GROUPS, LISTEN_FIELD, defaultEasyTier, easyTierIssues, readEasyTier, type EasyTierSettings } from '@/lib/mihomoEasyTier'

const wrappers: VueWrapper[] = []
const response = (data: any) => ({ data } as any)
const guard = { expected_profile_id: 'bound', expected_service_type: 'mihomo', expected_config_revision: 3 }
const WorkspaceStub = defineComponent({
  props: ['open', 'dirty', 'busy', 'saveDisabled'],
  emits: ['save', 'update:open'],
  setup(props, { slots }) {
    return () => h('section', [slots.default?.(), slots.footer?.({ disabled: props.busy || props.saveDisabled })])
  }
})
const enabled = (extra: Partial<EasyTierSettings> = {}): EasyTierSettings => ({
  ...defaultEasyTier(), enabled: true, routes: ['10.144.144.0/24'],
  options: { 'network-name': 'home', 'network-secret': 'pass', peers: ['tcp://203.0.113.10:11010'], udp: true }, ...extra,
})

beforeEach(() => {
  vi.spyOn(customConfigApi, 'getMihomo').mockResolvedValue(response({ config: 'mixed-port: 7890\n', easytier: enabled() }))
  vi.spyOn(customConfigApi, 'saveMihomo').mockResolvedValue(response({ success: true, publish_guard: guard }))
  vi.spyOn(notify, 'success').mockImplementation(() => 0)
  vi.spyOn(notify, 'error').mockImplementation(() => 0)
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
})

describe('EasyTier field list', () => {
  it('covers every documented outbound option exactly once', () => {
    // 「监听端口」 stands for no-listener.
    expect(LISTEN_FIELD.label).toBe('监听端口')
    const keys = [...EASYTIER_GROUPS.flatMap(group => group.fields.map(item => 'secret' in item ? item.secret : String(item.path[1]))), 'no-listener']
    expect(new Set(keys).size).toBe(keys.length)
    expect(keys.sort()).toEqual([
      'accept-dns', 'dhcp', 'dialer-proxy', 'disable-kcp-input', 'disable-p2p', 'disable-quic-input', 'enable-encryption',
      'enable-exit-node', 'enable-kcp-proxy', 'enable-quic-proxy', 'encryption-algorithm', 'exit-nodes', 'hostname', 'instance-name',
      'interface-name', 'ip-version', 'ipv4', 'latency-first', 'listeners', 'local-private-key', 'local-public-key', 'mapped-listeners',
      'mtu', 'network-name', 'network-secret', 'no-listener', 'peers', 'private-mode', 'proxy-networks', 'routing-mark', 'secure-mode',
      'state-dir', 'tld-dns-zone', 'udp',
    ])
  })

  it('reports the same problems the server refuses', () => {
    expect(easyTierIssues(enabled())).toEqual({})
    expect(easyTierIssues({ ...defaultEasyTier() })).toEqual({})
    const issues = easyTierIssues(enabled({
      routes: ['192.168.0.0/24->10.200.0.0/24'],
      keepalive: { enabled: true, url: '', interval: 5 },
      options: { 'network-name': '', listeners: ['tcp://0.0.0.0:11010'], 'no-listener': true, 'local-public-key': 'abc', mtu: 100 },
    }))
    expect(Object.keys(issues).sort()).toEqual(['keepalive.interval', 'keepalive.url', 'local-private-key', 'mtu', 'network-name', 'no-listener', 'routes'])
    expect(issues.routes).toContain('->')
    expect(easyTierIssues(enabled({ options: { 'network-name': 'home' } })).peers).toContain('入网节点')
  })

  it('reads missing or partial stored values as defaults', () => {
    expect(readEasyTier(undefined)).toEqual(defaultEasyTier())
    expect(readEasyTier({ enabled: true, apply_to: 'bogus', options: { udp: false } })).toMatchObject({ enabled: true, apply_to: 'subscription', options: { udp: false } })
  })
})

describe('EasyTier card', () => {
  function card(value = enabled()) {
    const model = ref(value)
    const wrapper = mount(MihomoEasyTierSettings, {
      props: { modelValue: model.value, 'onUpdate:modelValue': (next: EasyTierSettings) => { model.value = next; void wrapper.setProps({ modelValue: next }) } },
      attachTo: document.body,
    })
    wrappers.push(wrapper)
    return { wrapper, model }
  }

  it('shows the basic groups, hides the rest until asked, and opens a group with a problem', async () => {
    const { wrapper } = card()
    expect(wrapper.find('[data-base-section="easytier-network"]').exists()).toBe(true)
    expect(wrapper.find('[data-base-section="easytier-security"]').exists()).toBe(false)
    await wrapper.get('[data-easytier-advanced]').trigger('click')
    expect(wrapper.find('[data-base-section="easytier-security"]').exists()).toBe(true)
    const broken = card(enabled({ options: { 'network-name': 'home', peers: ['tcp://a:1'], 'local-public-key': 'abc' } })).wrapper
    expect(broken.find('[data-base-section="easytier-security"]').exists()).toBe(true)
    expect(broken.emitted('validation')!.at(-1)).toEqual([true])
  })

  it('writes typed secrets under their Mihomo keys and removes cleared ones', async () => {
    const { wrapper, model } = card()
    const input = wrapper.get('[data-base-field="easytier.network-secret"] input')
    expect(input.attributes('type')).toBe('password')
    await input.setValue('new-secret')
    expect(model.value.options['network-secret']).toBe('new-secret')
    await wrapper.get('[data-base-field="easytier.network-secret"] input').setValue('')
    expect('network-secret' in model.value.options).toBe(false)
  })

  it('shows the listen addresses only while listening and writes Mihomo keys for each state', async () => {
    const { wrapper, model } = card()
    const listen = () => wrapper.get('[data-easytier-listen] [role="switch"]')
    expect(wrapper.find('[data-base-field="easytier.listeners"]').exists()).toBe(false)
    expect(wrapper.find('[data-base-field="easytier.mapped-listeners"]').exists()).toBe(false)
    await listen().trigger('click')
    expect(model.value.options['no-listener']).toBe(false)
    expect(wrapper.find('[data-base-field="easytier.listeners"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('启动即连接')
    // Filled listeners drop no-listener; turning listening off clears every listen key.
    const listenersField = wrapper.findAllComponents({ name: 'MihomoBaseField' }).find(item => item.props('field').path[1] === 'listeners')!
    listenersField.vm.$emit('change', ['tcp://0.0.0.0:11010'])
    await flushPromises()
    expect(model.value.options).toMatchObject({ listeners: ['tcp://0.0.0.0:11010'] })
    expect('no-listener' in model.value.options).toBe(false)
    await listen().trigger('click')
    expect(['listeners', 'mapped-listeners', 'no-listener'].some(key => key in model.value.options)).toBe(false)
    expect(wrapper.find('[data-base-field="easytier.listeners"]').exists()).toBe(false)
  })

  it('turns the whole setting on and off with one switch', async () => {
    const { wrapper, model } = card({ ...defaultEasyTier() })
    expect(wrapper.find('[data-easytier-form]').exists()).toBe(false)
    await wrapper.get('[data-easytier-enabled]').trigger('click')
    expect(model.value.enabled).toBe(true)
  })
})

describe('Mihomo settings workspace with EasyTier', () => {
  async function editor() {
    const wrapper = shallowMount(MihomoSettings, {
      props: { open: true, profileId: 'bound', contextLabel: '设备配置' },
      global: { stubs: { SettingsWorkspace: WorkspaceStub, Button: false, Primitive: false } },
    })
    wrappers.push(wrapper)
    await flushPromises()
    return wrapper
  }

  it('loads the stored setting, sends it only when edited, and blocks invalid edits', async () => {
    const wrapper = await editor()
    const control = wrapper.getComponent(MihomoEasyTierSettings)
    expect(control.props('modelValue')).toEqual(enabled())
    await wrapper.getComponent(SettingsPublishBar).props('save')()
    expect(vi.mocked(customConfigApi.saveMihomo).mock.calls[0][0]).not.toHaveProperty('easytier')

    const edited = enabled({ apply_to: 'all' })
    control.vm.$emit('update:modelValue', edited)
    await flushPromises()
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(true)
    await wrapper.getComponent(SettingsPublishBar).props('save')()
    expect(vi.mocked(customConfigApi.saveMihomo).mock.calls[1][0]).toMatchObject({ easytier: edited })
    expect(wrapper.getComponent(SettingsWorkspace).props('dirty')).toBe(false)

    control.vm.$emit('validation', true)
    await flushPromises()
    expect(wrapper.getComponent(SettingsWorkspace).props('saveDisabled')).toBe(true)
    expect(await wrapper.getComponent(SettingsPublishBar).props('save')()).toBeNull()
    expect(customConfigApi.saveMihomo).toHaveBeenCalledTimes(2)
  })
})
