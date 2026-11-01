import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { agentApi, clientDeviceApi, oxidnsMonitorApi, type ClientDevice, type ClientDevicesState, type ClientDiscovery, type ClientPresenceInfo } from '@/api'
import { choose, confirm, confirmDanger, notify } from '@/lib/feedback'
import ClientDevices from '@/views/ClientDevices.vue'
import OxiDNSMonitor from '@/views/OxiDNSMonitor.vue'
import { Select } from '@/components/ui/select'
import FieldHelp from '@/components/common/FieldHelp.vue'
import { forgetClientDevicesSnapshot } from '@/lib/clientDevicesSnapshot'
import { setActiveProfileId } from '@/profileContext'
import { invalidateAuthSession } from '@/authSession'

vi.mock('@/api', () => ({
  clientDeviceApi: { get: vi.fn(), save: vi.fn(), discover: vi.fn(), previewFixedIP: vi.fn(), applyFixedIP: vi.fn() },
  oxidnsMonitorApi: { overview: vi.fn(), clients: vi.fn(), queries: vi.fn() },
  agentApi: { getAll: vi.fn(), pushConfig: vi.fn(), getDeployment: vi.fn(), activateDeployment: vi.fn() },
  profileApi: { list: vi.fn() }
}))
vi.mock('@/lib/feedback', () => ({ choose: vi.fn(), confirm: vi.fn(), confirmDanger: vi.fn(), notify: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() } }))

const response = (data: any) => ({ data } as any)
const agents = [{ id: 'one', name: 'DNS 1', host: '192.0.2.2', status: 'online', deployment: null },
  { id: 'two', name: 'DNS 2', host: '192.0.2.3', status: 'online', deployment: null },
  { id: 'off', name: 'DNS 3', host: '192.0.2.4', status: 'offline', deployment: null }]
const state = (devices: ClientDevice[] = [{ ip: '192.0.2.5', name: '电视', mode: 'direct' }, { ip: '192.0.2.9', name: '手机', mode: '' }],
  extra: Partial<ClientDevicesState> = {}): ClientDevicesState =>
  ({ success: true, profile_id: 'office', profile_name: '办公室', oxidns_revision: 'dns-1', devices, editable: true, reason: '', agents, ...extra })
const seen = (ip: string, extra: Partial<ClientPresenceInfo> = {}): ClientPresenceInfo =>
  ({ ip, mac: '', hostname: '', sources: [], presence: 'unknown', basis: '', last_seen: null, router_id: 'ros1', ...extra })
const discovery = (extra: Partial<ClientDiscovery> = {}): ClientDiscovery => ({
  success: true, profile_id: 'office', state: 'ok', message: '', cached: false, stale: false, refresh_error: '', checked_at: '2026-10-25T10:46:34+08:00',
  sources: [{ id: 'ros1', name: '主路由', type: 'routeros', ok: true, error: '', notes: [], policy_routing: false }],
  devices: [
    { name: '电视', mode: 'direct', ...seen('192.0.2.5', { mac: 'E0:BA:AD:94:F2:48', hostname: 'TV', sources: ['arp', 'lease'], presence: 'online', basis: 'lease', last_seen: 523 }), discoverable: true },
    { name: '手机', mode: '', ...seen('192.0.2.9', { basis: 'outside', router_id: '' }), discoverable: false }
  ],
  discovered: [
    { ...seen('192.0.2.147', { mac: '16:F7:C5:8A:16:A4', hostname: 'iPhone', sources: ['arp', 'lease'], presence: 'online', basis: 'arp', last_seen: 286 }), network: null },
    { ...seen('192.0.2.150', { mac: 'AA:BB:CC:00:00:01', sources: ['arp'], presence: 'recent', basis: 'stale' }), network: null }
  ],
  ...extra
})
const wrappers: VueWrapper[] = []
let stored: ClientDevice[]

async function render(path = '/client-devices') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/client-devices', component: ClientDevices },
    { path: '/agents/:agentId/oxidns', component: OxiDNSMonitor },
    { path: '/other', component: { template: '<div>other</div>' } },
    { path: '/setup', component: { template: '<div>setup</div>' } }
  ] })
  await router.push(path)
  const wrapper = mount({ template: '<RouterView />' }, { global: { plugins: [router] }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}
const nameInput = (wrapper: VueWrapper, ip: string) => wrapper.get<HTMLInputElement>(`input[aria-label="${ip} 名称"]`)
const addButton = (wrapper: VueWrapper) => wrapper.findAll('button').find(item => item.text().trim() === '添加')!
const button = (wrapper: VueWrapper, text: string) => wrapper.findAll('button').find(item => item.text().includes(text))!

beforeEach(() => {
  forgetClientDevicesSnapshot()
  setActiveProfileId('office')
  vi.clearAllMocks()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })))
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
  stored = state().devices
  vi.mocked(clientDeviceApi.get).mockImplementation(async () => response(state(stored)))
  // Like the server: one row changes, and a row without name and policy leaves the list.
  vi.mocked(clientDeviceApi.save).mockImplementation(async (_profile, device) => {
    const others = stored.filter(row => row.ip !== device.ip)
    const index = stored.findIndex(row => row.ip === device.ip)
    stored = device.name || device.mode ? (index < 0 ? [...stored, { ...device }] : stored.map(row => row.ip === device.ip ? { ...device } : row)) : others
    return response(state(stored, { oxidns_revision: 'dns-2' }))
  })
  vi.mocked(oxidnsMonitorApi.overview).mockResolvedValue(response({ recorders: ['queries'] }))
  vi.mocked(clientDeviceApi.discover).mockImplementation(async () => response(discovery()))
  vi.mocked(agentApi.pushConfig).mockImplementation(async (_id, deploymentId) => response({ deployment_id: deploymentId, status: 'succeeded' }))
  vi.mocked(confirm).mockResolvedValue(true)
  vi.mocked(confirmDanger).mockResolvedValue(true)
  vi.mocked(choose).mockResolvedValue('confirm')
  vi.mocked(clientDeviceApi.previewFixedIP).mockImplementation(async (_profile, devices, operation) => response({
    token: 'confirmed-change', actions: devices.map(device => ({ ip: device.ip, router_id: device.router_id, router_name: device.router_id === 'ik1' ? '爱快实验路由' : '主路由',
      router_type: device.router_id === 'ik1' ? 'ikuai' : 'routeros', description: `${device.router_id === 'ik1' ? 'iKuai' : 'RouterOS'}：${operation === 'delete' ? '删除' : '固定'} ${device.ip}（MAC ${device.mac}）` }))
  }))
  vi.mocked(clientDeviceApi.applyFixedIP).mockImplementation(async () => response(state(stored)))
})
afterEach(async () => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})

describe('client devices page', () => {
  it('lists named and policy devices of the active profile and targets online OxiDNS by default', async () => {
    const { wrapper } = await render()
    expect(clientDeviceApi.get).toHaveBeenCalledWith({ agentId: undefined })
    expect(nameInput(wrapper, '192.0.2.5').element.value).toBe('电视')
    expect(nameInput(wrapper, '192.0.2.9').element.value).toBe('手机')
    const boxes = wrapper.findAll('[role="checkbox"]')
    expect(boxes.map(box => box.attributes('aria-checked'))).toEqual(['true', 'true', 'false'])
    expect(button(wrapper, '发布到 2 台 OxiDNS').exists()).toBe(true)
    expect(clientDeviceApi.discover).toHaveBeenCalledWith('office')
    expect(oxidnsMonitorApi.overview).not.toHaveBeenCalled()
  })

  it('saves a rename on its own with the values the editor showed', async () => {
    const { wrapper } = await render()
    await nameInput(wrapper, '192.0.2.5').setValue('客厅电视 ')
    await nameInput(wrapper, '192.0.2.5').trigger('blur')
    await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenCalledTimes(1)
    expect(clientDeviceApi.save).toHaveBeenCalledWith('office', { ip: '192.0.2.5', name: '客厅电视', mode: 'direct' }, { name: '电视', mode: 'direct' })
    expect(wrapper.text()).toContain('已保存')
    await nameInput(wrapper, '192.0.2.5').trigger('blur')
    await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenCalledTimes(1)
  })

  it('maps the default DNS policy to no policy and saves policy changes immediately', async () => {
    const { wrapper } = await render()
    wrapper.findAllComponents(Select)[1].vm.$emit('update:modelValue', 'smart')
    await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenLastCalledWith('office', { ip: '192.0.2.9', name: '手机', mode: 'smart' }, { name: '手机', mode: '' })
    wrapper.findAllComponents(Select)[0].vm.$emit('update:modelValue', 'default')
    await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenLastCalledWith('office', { ip: '192.0.2.5', name: '电视', mode: '' }, { name: '电视', mode: 'direct' })
  })

  it('keeps a manually added address in place until it is named, then saves it', async () => {
    const { wrapper } = await render()
    await wrapper.get('#client-device-ip').setValue('192.0.2.77/24')
    await addButton(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tbody tr')[0].text()).toContain('192.0.2.0/24')
    expect(wrapper.text()).toContain('未保存')
    await nameInput(wrapper, '192.0.2.0/24').trigger('blur')
    await flushPromises()
    expect(clientDeviceApi.save).not.toHaveBeenCalled()
    await nameInput(wrapper, '192.0.2.0/24').setValue('访客网段')
    await nameInput(wrapper, '192.0.2.0/24').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenCalledWith('office', { ip: '192.0.2.0/24', name: '访客网段', mode: '' }, { name: '', mode: '' })
    // The saved row stays where the user added it.
    expect(wrapper.findAll('tbody tr')[0].text()).toContain('192.0.2.0/24')
    expect(wrapper.text()).not.toContain('未保存')
  })

  it('removes a device after confirmation by clearing its name and policy', async () => {
    const { wrapper } = await render()
    await wrapper.get('[aria-label="移除 192.0.2.5"]').trigger('click')
    await flushPromises()
    expect(confirmDanger).toHaveBeenCalled()
    expect(clientDeviceApi.save).toHaveBeenCalledWith('office', { ip: '192.0.2.5', name: '', mode: '' }, { name: '电视', mode: 'direct' })
    expect(wrapper.find('input[aria-label="192.0.2.5 名称"]').exists()).toBe(false)
  })

  it('reloads instead of overwriting a device changed elsewhere', async () => {
    const { wrapper } = await render()
    vi.mocked(clientDeviceApi.save).mockRejectedValueOnce({ response: { status: 409, data: { message: '此设备已在其他页面修改，请重新载入后再保存。' } } })
    stored = [{ ip: '192.0.2.5', name: '电视机', mode: 'direct' }]
    await nameInput(wrapper, '192.0.2.5').setValue('客厅')
    await nameInput(wrapper, '192.0.2.5').trigger('blur')
    await flushPromises()
    expect(notify.error).toHaveBeenCalledWith('设备已在其他页面修改', '此设备已在其他页面修改，请重新载入后再保存。')
    expect(nameInput(wrapper, '192.0.2.5').element.value).toBe('电视机')
  })

  it('never sends back a DNS policy that changed elsewhere while the name was being edited', async () => {
    const { wrapper } = await render()
    await nameInput(wrapper, '192.0.2.9').setValue('手机2')
    // Another page sets a policy for .9, then another row is saved here.
    stored = stored.map(row => row.ip === '192.0.2.9' ? { ...row, mode: 'direct' } : row)
    await nameInput(wrapper, '192.0.2.5').setValue('客厅')
    await nameInput(wrapper, '192.0.2.5').trigger('blur')
    await flushPromises()
    expect(nameInput(wrapper, '192.0.2.9').element.value).toBe('手机2')
    await nameInput(wrapper, '192.0.2.9').trigger('blur')
    await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenLastCalledWith('office', { ip: '192.0.2.9', name: '手机2', mode: 'direct' }, { name: '手机', mode: 'direct' })
  })

  it('keeps other rows being edited when one row conflicts', async () => {
    const { wrapper } = await render()
    await wrapper.get('#client-device-ip').setValue('192.0.2.30')
    await addButton(wrapper).trigger('click')
    await nameInput(wrapper, '192.0.2.9').setValue('typing')
    vi.mocked(clientDeviceApi.save).mockRejectedValueOnce({ response: { status: 409, data: { message: '此设备已在其他页面修改，请重新载入后再保存。' } } })
    stored = stored.map(row => row.ip === '192.0.2.5' ? { ...row, name: '电视机' } : row)
    await nameInput(wrapper, '192.0.2.5').setValue('客厅')
    await nameInput(wrapper, '192.0.2.5').trigger('blur')
    await flushPromises()
    expect(nameInput(wrapper, '192.0.2.5').element.value).toBe('电视机')
    expect(nameInput(wrapper, '192.0.2.9').element.value).toBe('typing')
    expect(wrapper.find('input[aria-label="192.0.2.30 名称"]').exists()).toBe(true)
    expect(confirm).not.toHaveBeenCalled()
  })

  it('clears a save failure when the edit is undone, reloaded or the unsaved row is removed', async () => {
    const { wrapper } = await render()
    const fail = () => vi.mocked(clientDeviceApi.save).mockRejectedValueOnce({ response: { status: 500, data: { message: '网络中断' } } })
    fail()
    await nameInput(wrapper, '192.0.2.5').setValue('新名')
    await nameInput(wrapper, '192.0.2.5').trigger('blur')
    await flushPromises()
    await nameInput(wrapper, '192.0.2.5').setValue('电视')
    await button(wrapper, '重试').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('网络中断')

    fail()
    await nameInput(wrapper, '192.0.2.5').setValue('新名')
    await nameInput(wrapper, '192.0.2.5').trigger('blur')
    await flushPromises()
    await wrapper.get('[aria-label="重新载入"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('网络中断')

    fail()
    await wrapper.get('#client-device-ip').setValue('192.0.2.30')
    await addButton(wrapper).trigger('click')
    await nameInput(wrapper, '192.0.2.30').setValue('新设备')
    await nameInput(wrapper, '192.0.2.30').trigger('blur')
    await flushPromises()
    await wrapper.get('[aria-label="移除 192.0.2.30"]').trigger('click')
    await button(wrapper, '发布到 2 台 OxiDNS').trigger('click')
    await flushPromises()
    expect(agentApi.pushConfig).toHaveBeenCalled()
  })

  it('keeps other failures on the row with a retry', async () => {
    const { wrapper } = await render()
    vi.mocked(clientDeviceApi.save).mockRejectedValueOnce({ response: { status: 400, data: { message: '设备备注最多 80 个字符' } } })
    await nameInput(wrapper, '192.0.2.5').setValue('新名')
    await nameInput(wrapper, '192.0.2.5').trigger('blur')
    await flushPromises()
    expect(wrapper.text()).toContain('设备备注最多 80 个字符')
    await button(wrapper, '重试').trigger('click')
    await flushPromises()
    expect(stored[0].name).toBe('新名')
  })

  it('asks for a publish target when the entry OxiDNS is not selected', async () => {
    vi.mocked(clientDeviceApi.get).mockResolvedValue(response(state(undefined, { agents: agents.map(agent => ({ ...agent, status: agent.id === 'two' ? 'online' : 'offline' })) })))
    const { wrapper } = await render('/client-devices?agent=one')
    expect(clientDeviceApi.discover).toHaveBeenCalledWith('office')
    await wrapper.get('[aria-label="发布到 DNS 1"]').trigger('click')
    await flushPromises()
    expect(button(wrapper, '选择要发布的 OxiDNS').attributes('disabled')).toBeDefined()
  })

  it('opens the bound profile from an OxiDNS Agent and targets only that device', async () => {
    const { wrapper, router } = await render('/client-devices?agent=two')
    expect(clientDeviceApi.get).toHaveBeenCalledWith({ agentId: 'two' })
    expect(wrapper.text()).toContain('配置：办公室')
    expect(wrapper.findAll('[role="checkbox"]').map(box => box.attributes('aria-checked'))).toEqual(['false', 'true', 'false'])
    expect(clientDeviceApi.discover).toHaveBeenCalledWith('office')
    await button(wrapper, '返回 OxiDNS 面板').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/agents/two/oxidns')
  })

  it('opens another profile from the router panel without switching the global profile', async () => {
    const { wrapper } = await render('/client-devices?profile=home')
    expect(clientDeviceApi.get).toHaveBeenCalledWith({ agentId: undefined, profileId: 'home' })
    expect(wrapper.text()).toContain('配置：办公室')
    expect(wrapper.text()).not.toContain('返回 OxiDNS 面板')
  })

  it('returns to the setup wizard or router page it was opened from', async () => {
    const { wrapper, router } = await render('/client-devices?profile=home&from=%2Fother%3Fsession%3Ds1')
    await button(wrapper, '返回上游路由').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/other?session=s1')
    const external = await render('/client-devices?from=%2F%2Fevil.example')
    expect(external.wrapper.text()).not.toContain('返回上游路由')
  })

  it('only names devices while the setup wizard is open, since a DNS policy would change its published OxiDNS', async () => {
    stored = [{ ip: '192.0.2.9', name: '', mode: '' }, { ip: '192.0.2.5', name: '电视', mode: 'direct' }]
    const { wrapper, router } = await render('/client-devices?profile=setup_x&from=%2Fsetup%3Fsession%3Ds1')
    const selects = wrapper.findAllComponents(Select)
    expect(selects.length).toBe(2)
    expect(selects.every(select => select.props('disabled'))).toBe(true)
    expect(wrapper.get('button[aria-label="移除 192.0.2.5"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('完成向导后再设置 DNS 策略')
    await nameInput(wrapper, '192.0.2.9').setValue('测试手机')
    await nameInput(wrapper, '192.0.2.9').trigger('blur'); await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenCalledWith('office', { ip: '192.0.2.9', name: '测试手机', mode: '' }, { name: '', mode: '' })
    await button(wrapper, '返回配置向导').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/setup?session=s1')
  })

  it('switches to the active profile when the navigation drops the Agent', async () => {
    const { wrapper, router } = await render('/client-devices?agent=two')
    await router.push('/client-devices')
    await flushPromises()
    expect(clientDeviceApi.get).toHaveBeenLastCalledWith({ agentId: undefined })
    expect(wrapper.text()).not.toContain('返回 OxiDNS 面板')
    expect(button(wrapper, '发布到 2 台 OxiDNS').exists()).toBe(true)
  })

  it('publishes the saved draft to each selected OxiDNS with the profile guard', async () => {
    const { wrapper } = await render()
    await button(wrapper, '发布到 2 台 OxiDNS').trigger('click')
    await flushPromises()
    expect(agentApi.pushConfig).toHaveBeenCalledTimes(2)
    expect(agentApi.pushConfig).toHaveBeenNthCalledWith(1, 'one', expect.any(String), { expected_profile_id: 'office', expected_oxidns_revision: 'dns-1' })
    expect(agentApi.pushConfig).toHaveBeenNthCalledWith(2, 'two', expect.any(String), { expected_profile_id: 'office', expected_oxidns_revision: 'dns-1' })
  })

  it('stops publishing when the draft changed after loading', async () => {
    const { wrapper } = await render()
    vi.mocked(agentApi.pushConfig).mockRejectedValueOnce({ response: { status: 409, data: { code: 'draft_changed', message: '配置或设备绑定已改变，请重新载入后发布。' } } })
    await button(wrapper, '发布到 2 台 OxiDNS').trigger('click')
    await flushPromises()
    expect(agentApi.pushConfig).toHaveBeenCalledTimes(1)
    expect(notify.error).toHaveBeenCalledWith('发布已停止', '配置或设备绑定已改变，请重新载入后发布。')
    expect(wrapper.text()).toContain('配置或设备绑定已改变')
  })

  it('continues with the next OxiDNS when one device is busy', async () => {
    const { wrapper } = await render()
    vi.mocked(agentApi.pushConfig).mockRejectedValueOnce({ response: { status: 409, data: { message: 'Agent update is in progress' } } })
    await button(wrapper, '发布到 2 台 OxiDNS').trigger('click')
    await flushPromises()
    expect(vi.mocked(agentApi.pushConfig).mock.calls.map(call => call[0])).toEqual(['one', 'two'])
    expect(notify.error).not.toHaveBeenCalledWith('发布已停止', expect.anything())
    expect(wrapper.text()).toContain('Agent update is in progress')
  })

  it('shows DNS checks and explains what an uploaded snapshot contains', async () => {
    vi.mocked(clientDeviceApi.get).mockResolvedValue(response(state(undefined, { agents: [{ ...agents[0], deployment: {
      deployment_id: 'd1', status: 'ready', dns_checks: [{ name: 'DNS 上游', level: 'warn', message: '上游解析有超时', fix: '检查上游连接' }] } as any }] })))
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('上游解析有超时')
    expect(wrapper.text()).toContain('待激活的是已上传的配置快照，不包含之后保存的草稿修改。')
    expect(button(wrapper, '应用并启动').exists()).toBe(true)
  })

  it('toggles a publish target from its name', async () => {
    const { wrapper } = await render()
    await wrapper.findAll('label').find(label => label.text() === 'DNS 1')!.trigger('click')
    await flushPromises()
    expect(button(wrapper, '发布到 1 台 OxiDNS').exists()).toBe(true)
  })

  it('publishes with the revision returned by the latest save', async () => {
    const { wrapper } = await render()
    wrapper.findAllComponents(Select)[1].vm.$emit('update:modelValue', 'direct')
    await button(wrapper, '发布到 2 台 OxiDNS').trigger('click')
    await flushPromises()
    expect(agentApi.pushConfig).toHaveBeenCalledWith('one', expect.any(String), { expected_profile_id: 'office', expected_oxidns_revision: 'dns-2' })
  })

  it('makes DNS policies read-only when the native configuration was edited by hand', async () => {
    vi.mocked(clientDeviceApi.get).mockResolvedValue(response(state(undefined, { editable: false, reason: '处理链已手动修改。' })))
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('处理链已手动修改。 名称仍可修改。')
    expect(wrapper.get('[aria-label="192.0.2.5 DNS 策略"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[aria-label="移除 192.0.2.5"]').attributes('disabled')).toBeDefined()
    expect(nameInput(wrapper, '192.0.2.5').attributes('disabled')).toBeUndefined()
  })

  it('lists side DNS entries that bypass the main chain while policies stay editable', async () => {
    const side = [{ tags: ['debug_dns_udp', 'debug_dns_tcp'], listen: '198.51.100.3:5300', entry: 'debug_dns_sequence' }]
    vi.mocked(clientDeviceApi.get).mockResolvedValue(response(state(undefined, { side_entries: side })))
    const { wrapper } = await render()
    const note = wrapper.get('[data-side-entries]')
    expect(note.attributes('role')).toBe('note')
    expect(note.text()).toContain('以下入口没有经过主处理链，设备策略对它们不生效：')
    expect(note.text()).toContain('debug_dns_udp、debug_dns_tcp（198.51.100.3:5300）')
    expect(note.text()).toContain('处理链 debug_dns_sequence')
    expect(wrapper.get('[aria-label="192.0.2.5 DNS 策略"]').attributes('disabled')).toBeUndefined()
  })

  it('shows no side entry note without entries, while read-only or during the setup wizard', async () => {
    const side = [{ tags: ['debug_dns_udp'], listen: '198.51.100.3:5300', entry: 'debug_dns_sequence' }]
    vi.mocked(clientDeviceApi.get).mockResolvedValue(response(state()))
    expect((await render()).wrapper.find('[data-side-entries]').exists()).toBe(false)
    vi.mocked(clientDeviceApi.get).mockResolvedValue(response(state(undefined, { editable: false, reason: '处理链已手动修改。', side_entries: side })))
    expect((await render()).wrapper.find('[data-side-entries]').exists()).toBe(false)
    vi.mocked(clientDeviceApi.get).mockResolvedValue(response(state(undefined, { side_entries: side })))
    expect((await render('/client-devices?from=%2Fsetup%3Fsession%3Dabc')).wrapper.find('[data-side-entries]').exists()).toBe(false)
  })
})

describe('client device fixed IP', () => {
  const dynamic = { eligible: true, reason: '', status: 'dynamic' as const, owned: false, router_id: 'ros1' }
  const owned = { router_id: 'ros1', mac: 'E0:BA:AD:94:F2:48', id: '*1', comment: 'cf-client:电视', vendor: 'routeros' }
  const setDiscovery = (alter?: (result: ClientDiscovery) => void) => {
    const result = discovery()
    result.devices[0].fixed_ip = { ...dynamic }
    result.discovered[0].fixed_ip = { ...dynamic }
    result.discovered[1].fixed_ip = { ...dynamic, eligible: false, reason: 'IP 不在已启用的 DHCP 网段内。' }
    alter?.(result)
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(result))
  }
  const selectFixed = async (wrapper: VueWrapper, ip = '192.0.2.147') => {
    await wrapper.get(`[aria-label="同时固定 IP ${ip}"]`).trigger('click')
    await flushPromises()
  }
  const setOwned = () => {
    stored[0] = { ...stored[0], fixed_ip: owned }
    setDiscovery(result => { result.devices[0].fixed_ip = { ...dynamic, eligible: false, status: 'static', owned: true } })
  }

  it('leaves fixed IP unchecked and adds a device without changing the router by default', async () => {
    setDiscovery()
    const { wrapper } = await render('/client-devices?tab=new')
    expect(wrapper.get('[aria-label="同时固定 IP 192.0.2.147"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('[aria-label="添加 iPhone"]').trigger('click'); await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenCalledTimes(1)
    expect(clientDeviceApi.previewFixedIP).not.toHaveBeenCalled()
    expect(clientDeviceApi.applyFixedIP).not.toHaveBeenCalled()
  })

  it('explains disabled options for missing MAC, unsupported routers and addresses outside enabled DHCP networks', async () => {
    setDiscovery(result => {
      result.discovered[0].mac = ''
      result.discovered.push({ ...seen('192.0.2.151', { mac: 'AA:BB:CC:00:00:02', fixed_ip: { ...dynamic, eligible: false, reason: '来源路由器不支持 DHCP 写入。' } }), network: null })
    })
    const { wrapper } = await render('/client-devices?tab=new')
    for (const ip of ['192.0.2.147', '192.0.2.150', '192.0.2.151']) expect(wrapper.get(`[aria-label="同时固定 IP ${ip}"]`).attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('路由器未提供 MAC')
    expect(wrapper.text()).toContain('IP 不在已启用的 DHCP 网段内')
    expect(wrapper.text()).toContain('来源路由器不支持 DHCP 写入')
  })

  it('waits for the router preview confirmation before saving and writing, and uses the discovered router', async () => {
    setDiscovery(result => { result.discovered[0].router_id = 'ik1'; result.discovered[0].fixed_ip!.router_id = 'ik1' })
    let answer!: (result: boolean) => void
    vi.mocked(confirm).mockImplementationOnce(() => new Promise(resolve => { answer = resolve }))
    const { wrapper } = await render('/client-devices?tab=new')
    await selectFixed(wrapper)
    await wrapper.get('[aria-label="添加 iPhone"]').trigger('click'); await flushPromises()
    expect(clientDeviceApi.previewFixedIP).toHaveBeenCalledWith('office', [{ ip: '192.0.2.147', mac: '16:F7:C5:8A:16:A4', router_id: 'ik1', name: 'iPhone' }], 'add')
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('「爱快实验路由」iKuai：固定 192.0.2.147（MAC 16:F7:C5:8A:16:A4）'), expect.objectContaining({ confirmText: '确认固定 IP' }))
    expect(clientDeviceApi.save).not.toHaveBeenCalled()
    expect(clientDeviceApi.applyFixedIP).not.toHaveBeenCalled()
    answer(true); await flushPromises()
    expect(clientDeviceApi.applyFixedIP).toHaveBeenCalledWith('office', 'confirmed-change')
    expect(vi.mocked(clientDeviceApi.save).mock.invocationCallOrder[0]).toBeLessThan(vi.mocked(clientDeviceApi.applyFixedIP).mock.invocationCallOrder[0])
    expect(clientDeviceApi.discover).toHaveBeenCalledTimes(2)
  })

  it('cancels without saving or writing to a router', async () => {
    setDiscovery()
    vi.mocked(confirm).mockResolvedValueOnce(false)
    const { wrapper } = await render('/client-devices?tab=new')
    await selectFixed(wrapper)
    await wrapper.get('[aria-label="添加 iPhone"]').trigger('click'); await flushPromises()
    expect(clientDeviceApi.save).not.toHaveBeenCalled()
    expect(clientDeviceApi.applyFixedIP).not.toHaveBeenCalled()
    expect(wrapper.find('[data-found-ip="192.0.2.147"]').exists()).toBe(true)
  })

  it('previews only eligible selected devices in a batch and preserves the default add-only behavior for others', async () => {
    setDiscovery()
    const { wrapper } = await render('/client-devices?tab=new')
    await wrapper.get('[aria-label="选择全部设备"]').trigger('click'); await flushPromises()
    await wrapper.get('[aria-label="同时固定所选设备 IP"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[aria-label="同时固定 IP 192.0.2.147"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[aria-label="同时固定 IP 192.0.2.150"]').attributes('aria-checked')).toBe('false')
    await button(wrapper, '添加所选（2）').trigger('click'); await flushPromises()
    expect(clientDeviceApi.previewFixedIP).toHaveBeenCalledWith('office', [expect.objectContaining({ ip: '192.0.2.147' })], 'add')
    expect(clientDeviceApi.save).toHaveBeenCalledTimes(2)
    expect(clientDeviceApi.applyFixedIP).toHaveBeenCalledTimes(1)
  })

  it('does not apply a batch if any device record could not be saved', async () => {
    setDiscovery()
    vi.mocked(clientDeviceApi.save).mockRejectedValueOnce(new Error('保存失败'))
    const { wrapper } = await render('/client-devices?tab=new')
    await wrapper.get('[aria-label="选择全部设备"]').trigger('click'); await flushPromises()
    await wrapper.get('[aria-label="同时固定所选设备 IP"]').trigger('click'); await flushPromises()
    await button(wrapper, '添加所选（2）').trigger('click'); await flushPromises()
    expect(clientDeviceApi.applyFixedIP).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('部分设备记录未保存，本次未执行固定 IP')
  })

  it('does not mistake a record concurrently added elsewhere for a successful save in this batch', async () => {
    setDiscovery()
    vi.mocked(clientDeviceApi.save).mockImplementationOnce(async (_profile, device) => {
      stored.push({ ...device, name: '其他页面添加的设备' })
      throw { response: { status: 409, data: { message: '设备已在其他页面修改' } } }
    })
    const { wrapper } = await render('/client-devices?tab=new')
    await selectFixed(wrapper)
    await wrapper.get('[aria-label="添加 iPhone"]').trigger('click'); await flushPromises()
    expect(clientDeviceApi.applyFixedIP).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('部分设备记录未保存，本次未执行固定 IP')
  })

  it('blocks profile switching while a confirmed DHCP apply is pending', async () => {
    setDiscovery()
    let finish!: (value: any) => void
    vi.mocked(clientDeviceApi.applyFixedIP).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const { wrapper, router } = await render('/client-devices?profile=office')
    await wrapper.get('[aria-label="固定 IP 192.0.2.5"]').trigger('click'); await flushPromises()
    await router.push('/client-devices?profile=another'); await flushPromises()
    expect(router.currentRoute.value.query.profile).toBe('office')
    finish(response(state(stored))); await flushPromises()
    expect(clientDeviceApi.applyFixedIP).toHaveBeenCalledWith('office', 'confirmed-change')
  })

  it('does not clear another profile when navigation waits for a canceled removal choice', async () => {
    setOwned()
    let answer!: (value: 'cancel') => void
    vi.mocked(choose).mockImplementationOnce(() => new Promise(resolve => { answer = resolve }))
    const { wrapper, router } = await render('/client-devices?profile=office')
    await wrapper.get('[aria-label="移除 192.0.2.5"]').trigger('click'); await flushPromises()
    const navigation = router.push('/client-devices?profile=another')
    await flushPromises()
    expect(router.currentRoute.value.query.profile).toBe('office')
    answer('cancel'); await navigation; await flushPromises()
    expect(clientDeviceApi.save).not.toHaveBeenCalled()
    expect(clientDeviceApi.applyFixedIP).not.toHaveBeenCalled()
  })

  it('reports an apply failure separately from saved device records and refreshes router state', async () => {
    setDiscovery()
    vi.mocked(clientDeviceApi.applyFixedIP).mockRejectedValueOnce({ response: { data: { message: '账号需要 DHCP 写入权限' } } })
    const { wrapper } = await render('/client-devices?tab=new')
    await selectFixed(wrapper)
    await wrapper.get('[aria-label="添加 iPhone"]').trigger('click'); await flushPromises()
    expect(stored.some(row => row.ip === '192.0.2.147')).toBe(true)
    expect(wrapper.text()).toContain('设备记录已保存，固定 IP 失败：账号需要 DHCP 写入权限')
    expect(clientDeviceApi.discover).toHaveBeenCalledTimes(2)
  })

  it('offers fixed IP for saved dynamic leases and marks existing static leases without an overwrite action', async () => {
    setDiscovery(result => { result.devices[1].fixed_ip = { ...dynamic, eligible: false, status: 'static' } })
    const { wrapper } = await render()
    expect(wrapper.get('[data-client-ip="192.0.2.9"]').text()).toContain('已固定')
    expect(wrapper.find('[aria-label="固定 IP 192.0.2.9"]').exists()).toBe(false)
    await wrapper.get('[aria-label="固定 IP 192.0.2.5"]').trigger('click'); await flushPromises()
    expect(clientDeviceApi.applyFixedIP).toHaveBeenCalledTimes(1)
    expect(clientDeviceApi.save).not.toHaveBeenCalled()
  })

  it('keeps a ConfigFlow DHCP binding by default when its device record is removed', async () => {
    setOwned()
    const { wrapper } = await render()
    await wrapper.get('[aria-label="移除 192.0.2.5"]').trigger('click'); await flushPromises()
    expect(choose).toHaveBeenCalledWith(expect.stringContaining('默认保留'), expect.objectContaining({ confirmText: '仅移除设备，保留固定 IP', altText: '一并删除固定 IP' }))
    expect(clientDeviceApi.save).toHaveBeenCalledWith('office', { ip: '192.0.2.5', name: '', mode: '' }, { name: '电视', mode: 'direct' })
    expect(clientDeviceApi.previewFixedIP).not.toHaveBeenCalled()
    expect(clientDeviceApi.applyFixedIP).not.toHaveBeenCalled()
  })

  it('previews and deletes the owned lease before clearing its device record', async () => {
    setOwned()
    vi.mocked(choose).mockResolvedValueOnce('alt')
    const { wrapper } = await render()
    await wrapper.get('[aria-label="移除 192.0.2.5"]').trigger('click'); await flushPromises()
    expect(clientDeviceApi.previewFixedIP).toHaveBeenCalledWith('office', [{ ip: '192.0.2.5', mac: owned.mac, router_id: 'ros1', name: '电视' }], 'delete')
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('删除 192.0.2.5'), expect.objectContaining({ confirmText: '确认删除固定 IP', danger: true }))
    expect(vi.mocked(clientDeviceApi.applyFixedIP).mock.invocationCallOrder[0]).toBeLessThan(vi.mocked(clientDeviceApi.save).mock.invocationCallOrder[0])
  })

  it('can remove a retained ConfigFlow binding after the device was added again without saved binding metadata', async () => {
    setDiscovery(result => {
      result.devices[0].router_id = ''
      result.devices[0].mac = ''
      result.devices[0].fixed_ip = { ...dynamic, eligible: false, status: 'static', owned: true, binding: owned }
    })
    vi.mocked(choose).mockResolvedValueOnce('alt')
    const { wrapper } = await render()
    await wrapper.get('[aria-label="移除 192.0.2.5"]').trigger('click'); await flushPromises()
    expect(clientDeviceApi.previewFixedIP).toHaveBeenCalledWith('office', [{ ip: '192.0.2.5', mac: owned.mac, router_id: owned.router_id, name: '电视' }], 'delete')
    expect(clientDeviceApi.applyFixedIP).toHaveBeenCalledTimes(1)
  })

  it('retains the record and restores its edited fields when DHCP deletion fails', async () => {
    setOwned()
    vi.mocked(choose).mockResolvedValueOnce('alt')
    vi.mocked(clientDeviceApi.applyFixedIP).mockRejectedValueOnce(new Error('路由器连接失败'))
    const { wrapper } = await render()
    await wrapper.get('[aria-label="移除 192.0.2.5"]').trigger('click'); await flushPromises()
    expect(clientDeviceApi.save).not.toHaveBeenCalled()
    expect(nameInput(wrapper, '192.0.2.5').element.value).toBe('电视')
    expect(wrapper.text()).toContain('固定 IP 未能删除，设备记录已保留：路由器连接失败')
  })

  it('asks the same removal question when name and policy are cleared, and restores them on cancel', async () => {
    setOwned()
    stored[0].mode = ''
    vi.mocked(choose).mockResolvedValueOnce('cancel')
    const { wrapper } = await render()
    await nameInput(wrapper, '192.0.2.5').setValue('')
    await nameInput(wrapper, '192.0.2.5').trigger('blur'); await flushPromises()
    expect(choose).toHaveBeenCalledTimes(1)
    expect(clientDeviceApi.save).not.toHaveBeenCalled()
    expect(nameInput(wrapper, '192.0.2.5').element.value).toBe('电视')
  })

  it('never offers DHCP deletion for a static lease owned by someone else', async () => {
    setDiscovery(result => { result.devices[0].fixed_ip = { ...dynamic, status: 'static', eligible: false, owned: false } })
    const { wrapper } = await render()
    await wrapper.get('[aria-label="移除 192.0.2.5"]').trigger('click'); await flushPromises()
    expect(confirmDanger).toHaveBeenCalledTimes(1)
    expect(choose).not.toHaveBeenCalled()
    expect(clientDeviceApi.applyFixedIP).not.toHaveBeenCalled()
  })

  it('hides fixed IP and lease deletion actions throughout the naming-only setup flow', async () => {
    setOwned()
    stored[0].mode = ''
    const { wrapper, router } = await render('/client-devices?from=%2Fsetup')
    expect(wrapper.find('[aria-label^="固定 IP"]').exists()).toBe(false)
    await wrapper.get('[aria-label="移除 192.0.2.5"]').trigger('click'); await flushPromises()
    expect(choose).not.toHaveBeenCalled()
    await router.push('/client-devices?from=%2Fsetup&tab=new'); await flushPromises()
    expect(wrapper.find('[aria-label^="同时固定"]').exists()).toBe(false)
    expect(clientDeviceApi.previewFixedIP).not.toHaveBeenCalled()
    expect(clientDeviceApi.applyFixedIP).not.toHaveBeenCalled()
  })
})

describe('client device discovery tabs', () => {
  const tabButton = (wrapper: VueWrapper, label: string) => wrapper.findAll('[role="tab"]').find(item => item.text().startsWith(label))!
  const openTab = async (wrapper: VueWrapper, label: string) => {
    await tabButton(wrapper, label).trigger('mousedown', { button: 0, ctrlKey: false })
    await flushPromises()
  }
  const listedIps = (wrapper: VueWrapper) => wrapper.findAll('tbody tr').map(row => row.attributes('data-client-ip') || row.attributes('data-found-ip'))
  const offline = (ip: string) => ({ ...seen(ip, { presence: 'offline', basis: 'absent', router_id: '' }) })

  it('splits saved rows into added and offline, and lists unsaved router devices with counts', async () => {
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(discovery({
      devices: [{ name: '电视', mode: 'direct', ...seen('192.0.2.5', { presence: 'online', basis: 'arp' }), discoverable: false }, { name: '手机', mode: '', ...offline('192.0.2.9'), discoverable: false }]
    })))
    const { wrapper, router } = await render()
    expect(wrapper.findAll('[role="tab"]').map(item => item.text().replace(/\s+/g, ''))).toEqual(['已添加1', '未添加2', '已离线1'])
    expect(listedIps(wrapper)).toEqual(['192.0.2.5'])
    expect(wrapper.text()).toContain('ARP 可达')
    await openTab(wrapper, '已离线')
    expect(router.currentRoute.value.query.tab).toBe('offline')
    expect(listedIps(wrapper)).toEqual(['192.0.2.9'])
    expect(wrapper.text()).toContain('路由器没有看到这个地址')
    await openTab(wrapper, '未添加')
    expect(router.currentRoute.value.query.tab).toBe('new')
    expect(listedIps(wrapper)).toEqual(['192.0.2.147', '192.0.2.150'])
    expect(wrapper.text()).toContain('iPhone')
    expect(wrapper.text()).toContain('近期见过')
    expect(wrapper.text()).toContain('未命名设备')
    await openTab(wrapper, '已添加')
    expect(router.currentRoute.value.query.tab).toBeUndefined()
  })

  it('opens the tab named in the address and searches hostnames and MAC addresses', async () => {
    const { wrapper } = await render('/client-devices?tab=new')
    expect(tabButton(wrapper, '未添加').attributes('aria-selected')).toBe('true')
    await wrapper.get('[aria-label="搜索设备"]').setValue('aa:bb')
    expect(listedIps(wrapper)).toEqual(['192.0.2.150'])
    await wrapper.get('[aria-label="搜索设备"]').setValue('iphone')
    expect(listedIps(wrapper)).toEqual(['192.0.2.147'])
  })

  it('adds a found device with its hostname as smart and reminds to publish', async () => {
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(discovery({
      sources: [{ id: 'ros1', name: '主路由', type: 'routeros', ok: true, error: '', notes: [], policy_routing: true }]
    })))
    const { wrapper } = await render('/client-devices?tab=new')
    await wrapper.get('[aria-label="添加 iPhone"]').trigger('click')
    await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenCalledWith('office', { ip: '192.0.2.147', name: 'iPhone', mode: 'smart' }, { name: '', mode: '' })
    expect(listedIps(wrapper)).toEqual(['192.0.2.150'])
    expect(wrapper.text()).toContain('已添加 1 台设备')
    expect(wrapper.text()).toContain('智能分流设备还需在上游路由重新发布')
    expect(notify.success).toHaveBeenCalledWith('已添加 1 台设备', expect.any(String))
    await openTab(wrapper, '已添加')
    expect(listedIps(wrapper)).toContain('192.0.2.147')
    const row = wrapper.get('[data-client-ip="192.0.2.147"]')
    expect(row.text()).toContain('参与策略路由')
    expect(row.text()).toContain('16:F7:C5:8A:16:A4')
    expect(wrapper.get('[data-client-ip="192.0.2.5"]').text()).not.toContain('参与策略路由')
  })

  it('adds the selected devices one after another through the save queue', async () => {
    const { wrapper } = await render('/client-devices?tab=new')
    expect(wrapper.text()).not.toContain('添加所选')
    await wrapper.get('[aria-label="选择全部设备"]').trigger('click')
    await flushPromises()
    let resolveFirst!: () => void
    const original = vi.mocked(clientDeviceApi.save).getMockImplementation()!
    vi.mocked(clientDeviceApi.save).mockImplementationOnce(async (...args) => {
      await new Promise<void>(resolve => { resolveFirst = resolve })
      return original(...args)
    })
    await button(wrapper, '添加所选（2）').trigger('click')
    await flushPromises()
    // The second row waits for the first save.
    expect(clientDeviceApi.save).toHaveBeenCalledTimes(1)
    resolveFirst()
    await flushPromises()
    expect(vi.mocked(clientDeviceApi.save).mock.calls.map(call => call[1])).toEqual([
      { ip: '192.0.2.147', name: 'iPhone', mode: 'smart' },
      { ip: '192.0.2.150', name: '', mode: 'smart' }
    ])
    expect(wrapper.text()).toContain('已添加 2 台设备')
    expect(wrapper.text()).toContain('没有未添加的设备')
    expect(wrapper.text()).not.toContain('添加所选')
  })

  it('keeps a removed device findable under 未添加', async () => {
    const { wrapper } = await render()
    await wrapper.get('[aria-label="移除 192.0.2.5"]').trigger('click')
    await flushPromises()
    await openTab(wrapper, '未添加')
    expect(listedIps(wrapper)).toEqual(['192.0.2.5', '192.0.2.147', '192.0.2.150'])
    expect(wrapper.get('[data-found-ip="192.0.2.5"]').text()).toContain('TV')
  })

  it('shows the saved network policy that covers a found device', async () => {
    stored = [...stored, { ip: '192.0.2.128/25', name: '访客', mode: 'direct' }, { ip: '192.0.2.0/24', name: '家里', mode: 'smart' }, { ip: '192.0.2.144/28', name: '只命名', mode: '' }]
    const { wrapper } = await render('/client-devices?tab=new')
    expect(wrapper.get('[data-found-ip="192.0.2.147"]').text()).toContain('随网段 192.0.2.128/25 · 直连')
  })

  it('names devices but leaves policies alone when DNS policies are read-only', async () => {
    vi.mocked(clientDeviceApi.get).mockImplementation(async () => response(state(stored, { editable: false, reason: '处理链已手动修改。' })))
    const { wrapper } = await render('/client-devices?tab=new')
    expect(wrapper.get('[aria-label="添加 192.0.2.150"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[aria-label="添加 iPhone"]').trigger('click')
    await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenCalledWith('office', { ip: '192.0.2.147', name: 'iPhone', mode: '' }, { name: '', mode: '' })
    expect(wrapper.text()).not.toContain('已添加 1 台设备')
  })

  it('adds found devices by name only while the setup wizard is open', async () => {
    const { wrapper } = await render('/client-devices?profile=setup_x&from=%2Fsetup%3Fsession%3Ds1&tab=new')
    expect(wrapper.get('[aria-label="添加 192.0.2.150"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[aria-label="添加 iPhone"]').trigger('click')
    await flushPromises()
    expect(clientDeviceApi.save).toHaveBeenCalledWith('office', { ip: '192.0.2.147', name: 'iPhone', mode: '' }, { name: '', mode: '' })
    expect(wrapper.text()).not.toContain('已添加 1 台设备')
    expect(wrapper.text()).toContain('完成向导后再设置 DNS 策略')
    const help = wrapper.findAllComponents(FieldHelp).map(item => String(item.props('text')))
    expect(help.some(text => text.includes('只写入名称'))).toBe(true)
    expect(help.some(text => text.includes('默认设为智能分流'))).toBe(false)
  })

  it('explains that discovery needs RouterOS or iKuai on the unadded and offline tabs', async () => {
    const message = '需要在当前配置接入 RouterOS 或 iKuai 才能发现设备'
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(discovery({ state: 'unsupported', message, sources: [], discovered: [],
      devices: [{ name: '电视', mode: 'direct', ...seen('192.0.2.5', { router_id: '' }), discoverable: false }, { name: '手机', mode: '', ...seen('192.0.2.9', { router_id: '' }), discoverable: false }] })))
    const { wrapper } = await render()
    expect(wrapper.findAll('[role="tab"]').map(item => item.text().replace(/\s+/g, ''))).toEqual(['已添加2', '未添加', '已离线'])
    expect(wrapper.text()).toContain('未接入 RouterOS / iKuai')
    await openTab(wrapper, '未添加')
    expect(wrapper.text()).toContain(message)
    expect(wrapper.find('a[href="/routers"]').exists()).toBe(true)
    await openTab(wrapper, '已离线')
    expect(wrapper.text()).toContain(message)
  })

  it('lists router failures and source notes', async () => {
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(discovery({ state: 'partial', message: '部分路由器读取失败，未出现的设备显示为未知',
      sources: [
        { id: 'ros1', name: '主路由', type: 'routeros', ok: true, error: '', notes: ['账号缺少 test 权限，无法确认静态 IP 设备是否在线'], policy_routing: false },
        { id: 'ik1', name: '爱快', type: 'ikuai', ok: false, error: '登录失败：账号或密码错误', notes: [] }
      ] })))
    const { wrapper } = await render()
    expect(wrapper.get('[role="alert"]').text()).toContain('「爱快」读取失败：登录失败：账号或密码错误')
    expect(wrapper.text()).toContain('「主路由」账号缺少 test 权限')
    expect(wrapper.text()).toContain('主路由（RouterOS）')
    expect(wrapper.text()).toContain('爱快（iKuai）')
  })

  it('does not call devices offline-free when the discovery request failed', async () => {
    vi.mocked(clientDeviceApi.discover).mockRejectedValueOnce({ response: { data: { message: '服务暂时不可用' } } })
    const { wrapper } = await render('/client-devices?tab=offline')
    expect(wrapper.text()).toContain('无法判断设备是否离线')
    expect(wrapper.text()).not.toContain('没有离线的设备')
    await button(wrapper, '重试').trigger('click')
    await flushPromises()
    expect(clientDeviceApi.discover).toHaveBeenCalledTimes(2)
  })

  it('shows a discovery request failure and retries it', async () => {
    vi.mocked(clientDeviceApi.discover).mockRejectedValueOnce({ response: { data: { message: '服务暂时不可用' } } })
    const { wrapper } = await render('/client-devices?tab=new')
    expect(wrapper.text()).toContain('暂时无法读取路由器：服务暂时不可用')
    await button(wrapper, '重试').trigger('click')
    await flushPromises()
    expect(clientDeviceApi.discover).toHaveBeenCalledTimes(2)
    expect(listedIps(wrapper)).toEqual(['192.0.2.147', '192.0.2.150'])
  })

  it('ignores a discovery result that arrives after a reload', async () => {
    let finish!: (value: any) => void
    vi.mocked(clientDeviceApi.discover).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const { wrapper } = await render('/client-devices?tab=new')
    expect(wrapper.text()).toContain('正在读取路由器')
    await wrapper.get('[aria-label="重新载入"]').trigger('click')
    await flushPromises()
    finish(response(discovery({ discovered: [] })))
    await flushPromises()
    expect(listedIps(wrapper)).toEqual(['192.0.2.147', '192.0.2.150'])
  })

  it('lays out both lists as mobile records without an empty batch bar', async () => {
    const { wrapper } = await render()
    expect(wrapper.get('table').classes()).toEqual(expect.arrayContaining(['cf-mobile-records', 'client-device-records']))
    expect(wrapper.find('#client-device-ip').exists()).toBe(true)
    await openTab(wrapper, '未添加')
    expect(wrapper.get('table').classes()).toEqual(expect.arrayContaining(['cf-mobile-records', 'client-discovery-records']))
    expect(wrapper.find('#client-device-ip').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('添加所选')
    expect(wrapper.text()).not.toContain('取消选择')
    // A device without a covering network hides its empty policy cell on phones.
    expect(wrapper.get('[data-found-ip="192.0.2.147"] td.client-found-default').exists()).toBe(true)
    await wrapper.get('[aria-label="选择 192.0.2.147"]').trigger('click')
    await flushPromises()
    expect(button(wrapper, '添加所选（1）').exists()).toBe(true)
  })

  it('moves a manually added offline address to the offline tab and focuses its name', async () => {
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(discovery({ discovered: [{ ...offline('192.0.2.77'), sources: ['lease'], mac: 'AA:00:00:00:00:77', network: null }] })))
    const { wrapper, router } = await render()
    await wrapper.get('#client-device-ip').setValue('192.0.2.77')
    await addButton(wrapper).trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBe('offline')
    expect(listedIps(wrapper)).toEqual(['192.0.2.77'])
    expect(document.activeElement).toBe(nameInput(wrapper, '192.0.2.77').element)
  })

  it('does not offer a removed Agent host or router address again', async () => {
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(discovery({ devices: [
      ...discovery().devices.slice(0, 1),
      { name: '手机', mode: '', ...seen('192.0.2.9', { mac: 'AA:00:00:00:00:09', sources: ['arp'], presence: 'online', basis: 'arp' }), discoverable: false }
    ] })))
    const { wrapper } = await render()
    await wrapper.get('[aria-label="移除 192.0.2.9"]').trigger('click')
    await flushPromises()
    await openTab(wrapper, '未添加')
    expect(listedIps(wrapper)).toEqual(['192.0.2.147', '192.0.2.150'])
  })

  it('leaves the proxy gateway out of policy routing like the router page', async () => {
    stored = [{ ip: '192.0.2.5', name: '网关', mode: 'smart' }, { ip: '192.0.2.9', name: '手机', mode: 'smart' }]
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(discovery({
      sources: [{ id: 'ros1', name: '主路由', type: 'routeros', ok: true, error: '', notes: [], policy_routing: true, gateway: '192.0.2.5' }]
    })))
    const { wrapper } = await render()
    expect(wrapper.get('[data-client-ip="192.0.2.5"]').text()).not.toContain('参与策略路由')
    expect(wrapper.get('[data-client-ip="192.0.2.9"]').text()).toContain('参与策略路由')
  })

  it('marks the header checkbox only from the devices the search shows', async () => {
    const { wrapper } = await render('/client-devices?tab=new')
    await wrapper.get('[aria-label="选择 192.0.2.147"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[aria-label="选择全部设备"]').attributes('data-state')).toBe('indeterminate')
    await wrapper.get('[aria-label="搜索设备"]').setValue('aa:bb')
    expect(wrapper.get('[aria-label="选择全部搜索结果"]').attributes('data-state')).toBe('unchecked')
    expect(button(wrapper, '添加所选（1）').exists()).toBe(true)
  })

  it('shows no counts and explains the offline tab when every router failed', async () => {
    const message = '路由器读取失败，暂时无法发现设备'
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(discovery({ state: 'failed', message, discovered: [],
      sources: [{ id: 'ros1', name: '主路由', type: 'routeros', ok: false, error: '连接超时', notes: [] }],
      devices: [{ name: '电视', mode: 'direct', ...seen('192.0.2.5'), discoverable: false }, { name: '手机', mode: '', ...seen('192.0.2.9'), discoverable: false }] })))
    const { wrapper } = await render('/client-devices?tab=offline')
    expect(wrapper.findAll('[role="tab"]').map(item => item.text().replace(/\s+/g, ''))).toEqual(['已添加2', '未添加', '已离线'])
    expect(wrapper.text()).toContain('无法判断设备是否离线')
    expect(wrapper.text()).toContain(message)
  })

  it('keeps a row being edited in its tab when the routers answer later', async () => {
    let finish!: (value: any) => void
    vi.mocked(clientDeviceApi.discover).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const { wrapper } = await render()
    await nameInput(wrapper, '192.0.2.9').trigger('focusin')
    await nameInput(wrapper, '192.0.2.9').setValue('手机（改名中')
    finish(response(discovery({ devices: [discovery().devices[0], { name: '手机', mode: '', ...offline('192.0.2.9'), discoverable: false }] })))
    await flushPromises()
    expect(listedIps(wrapper)).toEqual(['192.0.2.5', '192.0.2.9'])
    expect(wrapper.get('[data-client-ip="192.0.2.9"]').text()).toContain('离线')
    await openTab(wrapper, '已离线')
    expect(listedIps(wrapper)).toEqual(['192.0.2.9'])
    await openTab(wrapper, '已添加')
    expect(listedIps(wrapper)).toEqual(['192.0.2.5'])
  })

  it('says where policies take effect when the profile has no OxiDNS', async () => {
    vi.mocked(clientDeviceApi.get).mockImplementation(async () => response(state(stored, { agents: [] })))
    const original = vi.mocked(clientDeviceApi.save).getMockImplementation()!
    vi.mocked(clientDeviceApi.save).mockImplementation(async (...args) => response({ ...(await original(...args)).data, agents: [] }))
    const { wrapper } = await render('/client-devices?tab=new')
    await wrapper.get('[aria-label="添加 iPhone"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('此配置空间没有 OxiDNS Agent，DNS 策略会在接入 OxiDNS 并发布后生效。')
    expect(wrapper.text()).not.toContain('前往发布')
  })
})


describe('client device router snapshots', () => {
  it('shows a fresh cached list without starting a router refresh', async () => {
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(discovery({ cached: true, checked_at: '2000-01-02T10:46:34+08:00' })))
    const { wrapper } = await render('/client-devices?tab=new')
    expect(wrapper.find('[data-found-ip="192.0.2.147"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('上次读取')
    // Cross-day snapshots must show the date as well as the time.
    expect(wrapper.text()).toContain('2000/01/02')
    expect(clientDeviceApi.discover).toHaveBeenCalledTimes(1)
  })

  it('keeps stale devices visible and editable while refreshing in the background', async () => {
    let finish!: (value: any) => void
    vi.mocked(clientDeviceApi.discover)
      .mockResolvedValueOnce(response(discovery({ cached: true, stale: true })))
      .mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const { wrapper } = await render()
    expect(wrapper.text()).toContain('正在后台更新路由器上的设备')
    expect(wrapper.text()).toContain('上次读取')
    expect(clientDeviceApi.discover).toHaveBeenLastCalledWith('office', { refresh: true })
    await nameInput(wrapper, '192.0.2.5').setValue('正在输入的新名称')
    finish(response(discovery({ discovered: [] })))
    await flushPromises()
    expect(nameInput(wrapper, '192.0.2.5').element.value).toBe('正在输入的新名称')
    expect(wrapper.text()).not.toContain('正在后台更新路由器上的设备')
  })

  it('retains the cached list and explains a background request failure', async () => {
    vi.mocked(clientDeviceApi.discover)
      .mockResolvedValueOnce(response(discovery({ cached: true, stale: true })))
      .mockRejectedValueOnce(new Error('连接中断'))
    const { wrapper } = await render('/client-devices?tab=new')
    expect(wrapper.find('[data-found-ip="192.0.2.147"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('连接中断；仍显示上次结果')
  })

  it('shows router refresh failures retained alongside the server snapshot', async () => {
    vi.mocked(clientDeviceApi.discover).mockResolvedValue(response(discovery({ cached: true, refresh_error: '认证失败' })))
    const { wrapper } = await render('/client-devices?tab=new')
    expect(wrapper.find('[data-found-ip="192.0.2.147"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('认证失败；仍显示上次结果')
  })

  it('forces a manual refresh and keeps the previous devices visible until it finishes', async () => {
    const { wrapper } = await render('/client-devices?tab=new')
    let finish!: (value: any) => void
    vi.mocked(clientDeviceApi.discover).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await wrapper.get('[aria-label="重新载入"]').trigger('click')
    await flushPromises()
    expect(clientDeviceApi.discover).toHaveBeenLastCalledWith('office', { refresh: true })
    expect(wrapper.find('[data-found-ip="192.0.2.147"]').exists()).toBe(true)
    finish(response(discovery({ discovered: [] })))
    await flushPromises()
    expect(wrapper.text()).toContain('没有未添加的设备')
  })

  it('discards a background refresh after switching profiles', async () => {
    let finish!: (value: any) => void
    vi.mocked(clientDeviceApi.discover)
      .mockResolvedValueOnce(response(discovery({ cached: true, stale: true })))
      .mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
      .mockResolvedValueOnce(response(discovery({ profile_id: 'home', devices: [], discovered: [] })))
    const { wrapper, router } = await render('/client-devices?tab=new')
    vi.mocked(clientDeviceApi.get).mockResolvedValue(response(state([], { profile_id: 'home', profile_name: '家里' })))
    await router.push('/client-devices?profile=home&tab=new')
    await flushPromises()
    finish(response(discovery()))
    await flushPromises()
    expect(wrapper.find('[data-found-ip="192.0.2.147"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('没有未添加的设备')
  })
})


describe('client device page reopening', () => {
  it('shows the previous saved list immediately, then validates records without a skeleton', async () => {
    const { wrapper, router } = await render()
    await router.push('/other')
    let finish!: (value: any) => void
    vi.mocked(clientDeviceApi.get).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await router.push('/client-devices')
    await flushPromises()
    expect(nameInput(wrapper, '192.0.2.5').element.value).toBe('电视')
    expect(nameInput(wrapper, '192.0.2.5').element.readOnly).toBe(true)
    expect(wrapper.find('[aria-label="正在加载"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('正在核对最新记录')
    finish(response(state([{ ip: '192.0.2.5', name: '别处改过的名称', mode: 'direct' }])))
    await flushPromises()
    expect(nameInput(wrapper, '192.0.2.5').element.value).toBe('别处改过的名称')
    expect(nameInput(wrapper, '192.0.2.5').element.readOnly).toBe(false)
  })

  it('starts router discovery while the record request is still pending', async () => {
    let finish!: (value: any) => void
    vi.mocked(clientDeviceApi.get).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const { wrapper } = await render()
    expect(clientDeviceApi.discover).toHaveBeenCalledWith('office')
    expect(wrapper.find('[aria-label="正在加载"]').exists()).toBe(true)
    finish(response(state()))
    await flushPromises()
    expect(clientDeviceApi.discover).toHaveBeenCalledTimes(1)
    expect(nameInput(wrapper, '192.0.2.5').exists()).toBe(true)
  })

  it('never stores an unsaved input as the next page preview', async () => {
    const { wrapper, router } = await render()
    await nameInput(wrapper, '192.0.2.5').setValue('未保存的输入')
    await router.push('/other')
    let finish!: (value: any) => void
    vi.mocked(clientDeviceApi.get).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await router.push('/client-devices')
    await flushPromises()
    expect(nameInput(wrapper, '192.0.2.5').element.value).toBe('电视')
    finish(response(state()))
    await flushPromises()
  })

  it('keeps a failed preview read-only and disables publishing until records are verified', async () => {
    const { wrapper, router } = await render()
    await router.push('/other')
    vi.mocked(clientDeviceApi.get).mockRejectedValueOnce(new Error('网络中断'))
    await router.push('/client-devices')
    await flushPromises()
    expect(nameInput(wrapper, '192.0.2.5').element.readOnly).toBe(true)
    expect(wrapper.text()).toContain('网络中断')
    expect(wrapper.text()).toContain('上次记录 · 暂不可编辑')
    expect(button(wrapper, '发布到 2 台 OxiDNS').attributes('disabled')).toBeDefined()
    await nameInput(wrapper, '192.0.2.5').trigger('blur')
    expect(clientDeviceApi.save).not.toHaveBeenCalled()
    await wrapper.get('[aria-label="重新载入"]').trigger('click')
    await flushPromises()
    expect(nameInput(wrapper, '192.0.2.5').element.readOnly).toBe(false)
  })

  it('discards previews when the authenticated session changes', async () => {
    const { wrapper, router } = await render()
    await router.push('/other')
    invalidateAuthSession()
    let finish!: (value: any) => void
    vi.mocked(clientDeviceApi.get).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await router.push('/client-devices')
    await flushPromises()
    expect(wrapper.find('[aria-label="192.0.2.5 名称"]').exists()).toBe(false)
    finish(response(state()))
    await flushPromises()
  })

  it('does not preview another profile or adopt its late record response', async () => {
    const { wrapper, router } = await render()
    let finishHome!: (value: any) => void
    vi.mocked(clientDeviceApi.get).mockImplementationOnce(() => new Promise(resolve => { finishHome = resolve }))
    await router.push('/client-devices?profile=home')
    await flushPromises()
    expect(wrapper.find('[aria-label="192.0.2.5 名称"]').exists()).toBe(false)
    vi.mocked(clientDeviceApi.get).mockResolvedValueOnce(response(state([], { profile_id: 'work', profile_name: '工作' })))
    await router.push('/client-devices?profile=work')
    await flushPromises()
    finishHome(response(state([{ ip: '192.0.2.77', name: '家里', mode: '' }], { profile_id: 'home' })))
    await flushPromises()
    expect(wrapper.find('[aria-label="192.0.2.77 名称"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('配置：工作')
  })
})


it('drops a preview when its profile no longer exists', async () => {
  const { wrapper, router } = await render()
  await router.push('/other')
  vi.mocked(clientDeviceApi.get).mockRejectedValueOnce({ response: { status: 404, data: { message: '配置空间已删除' } } })
  await router.push('/client-devices')
  await flushPromises()
  expect(wrapper.find('[aria-label="192.0.2.5 名称"]').exists()).toBe(false)
  expect(wrapper.text()).toContain('配置空间已删除')
})

it('does not start discovery in an Agents previous profile before checking its binding', async () => {
  const { wrapper, router } = await render('/client-devices?agent=one')
  await router.push('/other')
  vi.mocked(clientDeviceApi.discover).mockClear()
  let finish!: (value: any) => void
  vi.mocked(clientDeviceApi.get).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  await router.push('/client-devices?agent=one')
  await flushPromises()
  expect(clientDeviceApi.discover).not.toHaveBeenCalled()
  expect(nameInput(wrapper, '192.0.2.5').element.readOnly).toBe(true)
  finish(response(state([], { profile_id: 'home' })))
  await flushPromises()
  expect(clientDeviceApi.discover).toHaveBeenCalledWith('home')
  expect(wrapper.find('[aria-label="192.0.2.5 名称"]').exists()).toBe(false)
})
