import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AgentDNSHA from '@/components/agents/AgentDNSHA.vue'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import { Select } from '@/components/ui/select'
import { agentApi } from '@/api'
import { confirm, confirmDanger, notify } from '@/lib/feedback'

vi.mock('@/api', () => ({ agentApi: { getDNSHA: vi.fn(), getDockerHAEnvironment: vi.fn(), setDNSHA: vi.fn(), removeDNSHA: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({
  notify: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
  confirm: vi.fn(),
  confirmDanger: vi.fn(),
}))

const wrappers: ReturnType<typeof mount>[] = []
const ha = { enabled: true, group_id: 'g1', peer_id: 'oxi-2', vip: '192.0.2.53', vrid: 2, role: 'primary' }
function agent(id: string, overrides: Record<string, unknown> = {}) {
  return { id, name: id === 'oxi-1' ? '主 OxiDNS' : `OxiDNS ${id}`, host: `192.0.2.${id === 'oxi-1' ? 20 : 21}`, service_type: 'oxidns', status: 'online', profile_id: 'default', ...overrides } as any
}
const others = [
  agent('oxi-2'),
  agent('oxi-3', { host: '192.0.2.22', profile_id: 'other' }),
  agent('oxi-4', { host: '192.0.2.23', oxidns_ha: { ...ha, group_id: 'g2', peer_id: 'oxi-5' } }),
  agent('mihomo-1', { service_type: 'mihomo', host: '192.0.2.3' }),
]
function member(id: string, runtime: Record<string, unknown> | null, overrides: Record<string, unknown> = {}) {
  return { agent_id: id, name: id === 'oxi-1' ? '主 OxiDNS' : `OxiDNS ${id}`, host: id === 'oxi-1' ? '192.0.2.20' : '192.0.2.21', online: true,
    role_config: id === 'oxi-1' ? 'primary' : 'secondary',
    runtime: runtime && { keepalived_running: true, vip_present: false, role: 'backup', healthy: true, ...runtime }, error: '', ...overrides }
}
function status(health: string, members: unknown[]) {
  return { config: ha, state: { status: 'succeeded', error: '', updated_at: 1 }, members, health }
}
function render(overrides: Record<string, unknown> = {}, list = others) {
  const self = agent('oxi-1', overrides)
  const wrapper = mount(AgentDNSHA, { props: { agent: self, agents: [self, ...list] }, attachTo: document.body })
  wrappers.push(wrapper)
  return wrapper
}
function button(text: string) {
  const found = Array.from(document.querySelectorAll('button')).find(item => item.textContent?.trim() === text || item.getAttribute('aria-label') === text)
  expect(found, `button ${text}`).toBeTruthy()
  return found!
}
async function click(text: string) {
  button(text).click()
  await flushPromises()
}
async function type(id: string, value: string) {
  const field = document.getElementById(id) as HTMLInputElement
  field.value = value
  field.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function submit() {
  document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(confirm).mockResolvedValue(true)
  vi.mocked(confirmDanger).mockResolvedValue(true)
  vi.mocked(agentApi.getDNSHA).mockResolvedValue({ data: status('ok', [member('oxi-1', { vip_present: true, role: 'master' }), member('oxi-2', {})]) } as any)
  vi.mocked(agentApi.setDNSHA).mockResolvedValue({ data: status('ok', []) } as any)
  vi.mocked(agentApi.removeDNSHA).mockResolvedValue({ data: { removed: true } } as any)
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.useRealTimers()
})

const publication = (status = 'applying') => ({
  id: 'publication-1', status, phase: status === 'succeeded' ? 'done' : 'standby',
  message: status === 'succeeded' ? '主备配置与规则均已发布成功' : '正在发布备机',
  owner_agent_id: 'oxi-1', owner_deployment_id: 'deploy-1', active_agent_id: 'oxi-2',
  members: [{ agent_id: 'oxi-2', name: '备用 OxiDNS', deployment_id: 'deploy-2', status: status === 'succeeded' ? 'succeeded' : 'checking' },
    { agent_id: 'oxi-1', name: '主 OxiDNS', deployment_id: 'deploy-1', status: status === 'succeeded' ? 'succeeded' : 'waiting' }],
})

describe('ordered configuration synchronization', () => {
  it('shows Docker preparation instead of a host package command for a Docker pair', async () => {
    render({ deployment_method: 'docker' }, [agent('oxi-2', { deployment_method: 'docker' })])
    await click('设置 DNS 高可用')
    expect(document.body.textContent).toContain('Host 配合独立高可用容器')
    expect(document.body.textContent).toContain('普通容器不能只点开关启用')
    expect(document.body.textContent).not.toContain('apt install keepalived')
  })

  it('keeps native installation advice when pairing a Docker member with a Shell member', async () => {
    const wrapper = render({ deployment_method: 'docker' }, [agent('oxi-2', { deployment_method: 'shell' })])
    await click('设置 DNS 高可用')
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'oxi-2')
    await flushPromises()
    expect(document.body.textContent).toContain('Docker 成员需使用专用高可用安装')
    expect(document.body.textContent).toContain('Shell 成员的高可用安装命令会自动准备 keepalived')
  })

  it('does not announce enable success until both members and keepalived finish', async () => {
    vi.useFakeTimers()
    vi.mocked(agentApi.setDNSHA).mockResolvedValueOnce({ data: { success: true, config: null, publication: publication() } } as any)
    vi.mocked(agentApi.getDNSHA).mockResolvedValueOnce({ data: { success: true, config: null, publication: publication() } } as any)
    const wrapper = render()
    await click('设置 DNS 高可用')
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'oxi-2')
    await type('dns-ha-vip-oxi-1', '192.0.2.53')
    await submit()
    expect(notify.success).not.toHaveBeenCalled()
    expect(wrapper.get('[data-ha-publication]').text()).toContain('备用 OxiDNS')
    expect(wrapper.get('[data-ha-publication]').text()).toContain('等待前一台完成')
    vi.mocked(agentApi.getDNSHA).mockResolvedValueOnce({ data: { ...status('ok', []), publication: publication('succeeded') } } as any)
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(notify.success).toHaveBeenCalledExactlyOnceWith('已开启 DNS 高可用', expect.any(String))
    expect(wrapper.text()).toContain('主备正常')
  })

  it('resumes an unfinished first enable after reopening the page and retains its failure', async () => {
    vi.useFakeTimers()
    const pending = publication()
    vi.mocked(agentApi.getDNSHA).mockResolvedValueOnce({ data: { success: true, config: null, publication: pending } } as any)
    const wrapper = render({ oxidns_ha_publication: pending })
    await flushPromises()
    expect(agentApi.getDNSHA).toHaveBeenCalledWith('oxi-1')
    expect(wrapper.text()).toContain('正在同步主备')
    vi.mocked(agentApi.getDNSHA).mockResolvedValueOnce({ data: { success: true, config: null,
      publication: { ...publication('failed'), message: '备机健康检查失败，主机未更新' } } } as any)
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.text()).toContain('备机健康检查失败，主机未更新')
    expect(wrapper.text()).toContain('主备同步失败')
    expect(notify.success).not.toHaveBeenCalled()
    const calls = vi.mocked(agentApi.getDNSHA).mock.calls.length
    await vi.advanceTimersByTimeAsync(6000)
    expect(agentApi.getDNSHA).toHaveBeenCalledTimes(calls)
  })

  it('distinguishes an unknown first-enable result from active synchronization', async () => {
    const pending = publication('unknown')
    vi.mocked(agentApi.getDNSHA).mockResolvedValueOnce({ data: { success: true, config: null, publication: pending } } as any)
    const wrapper = render({ oxidns_ha_publication: pending })
    await flushPromises()
    expect(wrapper.findComponent(AgentStatusRow).props()).toMatchObject({ status: '同步结果待确认', tone: 'warning' })
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('shows the VIP handoff before updating the original holder and follows actual roles', async () => {
    vi.useFakeTimers()
    const pending = { ...publication(), phase: 'handoff',
      message: '正在将虚拟 IP 迁移到备用 OxiDNS，并确认解析正常',
      members: [{ agent_id: 'oxi-2', name: '备用 OxiDNS', deployment_id: 'deploy-2', status: 'succeeded' },
        { agent_id: 'oxi-1', name: '主 OxiDNS', deployment_id: 'deploy-1', status: 'waiting' }] }
    vi.mocked(agentApi.getDNSHA).mockResolvedValueOnce({ data: {
      ...status('ok', [member('oxi-1', { vip_present: true }), member('oxi-2', {})]), publication: pending,
    } } as any)
    const wrapper = render({ oxidns_ha: ha, oxidns_ha_publication: pending })
    await flushPromises()
    expect(wrapper.findComponent(AgentStatusRow).props()).toMatchObject({ status: '正在迁移虚拟 IP', tone: 'progress' })
    expect(wrapper.get('[data-ha-publication]').text()).toContain('确认解析正常')
    expect(wrapper.get('[data-ha-publication]').text()).toContain('等待前一台完成')
    expect(notify.success).not.toHaveBeenCalled()

    vi.mocked(agentApi.getDNSHA).mockResolvedValueOnce({ data: {
      ...status('ok', [member('oxi-1', {}), member('oxi-2', { vip_present: true, role: 'master' })]),
      publication: { ...pending, phase: 'primary', active_agent_id: 'oxi-1',
        message: '虚拟 IP 解析已确认，正在更新原持有设备 主 OxiDNS' },
    } } as any)
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.get('[data-ha-publication]').text()).toContain('正在更新原持有设备')
    expect(wrapper.find('[data-dns-ha-holder]').exists()).toBe(false)
    expect(wrapper.findComponent(AgentStatusRow).props('tone')).toBe('progress')
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('keeps an unconfirmed handoff visible as a warning and continues checking', async () => {
    vi.useFakeTimers()
    const pending = { ...publication('unknown'), phase: 'handoff',
      message: '虚拟 IP 迁移结果尚未确认，原持有设备未更新' }
    vi.mocked(agentApi.getDNSHA).mockResolvedValue({ data: { ...status('ok', []), publication: pending } } as any)
    const wrapper = render({ oxidns_ha: ha, oxidns_ha_publication: pending })
    await flushPromises()
    expect(wrapper.findComponent(AgentStatusRow).props()).toMatchObject({ status: '虚拟 IP 迁移待确认', tone: 'warning' })
    expect(wrapper.get('[data-status-notice]').text()).toContain('原持有设备未更新')
    await vi.advanceTimersByTimeAsync(2000)
    expect(agentApi.getDNSHA).toHaveBeenCalledTimes(2)
    expect(notify.success).not.toHaveBeenCalled()
  })
})

describe('DNS high availability when not enabled', () => {
  it('shows a neutral row and sends no request, including after opening the dialog', async () => {
    const wrapper = render()
    await flushPromises()
    const row = wrapper.findComponent(AgentStatusRow)
    expect(row.props()).toMatchObject({ label: 'DNS 高可用', status: '未开启', tone: 'neutral' })
    expect(wrapper.find('[data-status-notice]').exists()).toBe(false)
    await wrapper.setProps({ agent: agent('oxi-1', { status: 'offline' }) })
    await click('设置 DNS 高可用')
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    expect(agentApi.getDNSHA).not.toHaveBeenCalled()
    expect(agentApi.setDNSHA).not.toHaveBeenCalled()
    expect(agentApi.removeDNSHA).not.toHaveBeenCalled()
  })

  it('offers only same-profile OxiDNS peers that are not in another group', async () => {
    render()
    await click('设置 DNS 高可用')
    const wrapper = wrappers[0]
    const items = wrapper.findComponent(Select).findAllComponents({ name: 'SelectItem' })
    const values = [...new Set(items.map(item => item.props('value')))]
    expect(values).toEqual(['oxi-2'])
  })

  it('validates the peer, VIP and VRID before enabling, keeps VRID collapsed and omits it when left empty', async () => {
    const wrapper = render()
    await click('设置 DNS 高可用')
    expect((document.querySelector('[data-dns-ha-advanced]') as HTMLDetailsElement).open).toBe(false)
    expect(button('开启').disabled).toBe(true)
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'oxi-2')
    await flushPromises()
    expect(button('开启').disabled).toBe(true)
    await type('dns-ha-vip-oxi-1', '192.0.2.300')
    expect(document.body.textContent).toContain('请填写 IPv4 地址')
    expect(button('开启').disabled).toBe(true)
    await type('dns-ha-vip-oxi-1', '192.0.2.21')
    expect(document.body.textContent).toContain('不能与两台 OxiDNS 的地址相同')
    expect(button('开启').disabled).toBe(true)
    await type('dns-ha-vip-oxi-1', '224.0.0.18')
    expect(document.body.textContent).toContain('单播地址')
    await type('dns-ha-vip-oxi-1', ' 192.0.2.53 ')
    expect(button('开启').disabled).toBe(false)
    await type('dns-ha-vrid-oxi-1', '256')
    expect(document.body.textContent).toContain('VRID 范围为 1–255')
    expect(button('开启').disabled).toBe(true)
    await type('dns-ha-vrid-oxi-1', '')
    await submit()
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(confirmDanger).not.toHaveBeenCalled()
    expect(agentApi.setDNSHA).toHaveBeenCalledExactlyOnceWith('oxi-1', { peer_id: 'oxi-2', vip: '192.0.2.53' })
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(notify.success).toHaveBeenCalled()
  })

  it('sends an explicit VRID and does nothing when the confirmation is cancelled', async () => {
    const wrapper = render()
    await click('设置 DNS 高可用')
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'oxi-2')
    await type('dns-ha-vip-oxi-1', '192.0.2.53')
    await type('dns-ha-vrid-oxi-1', '77')
    vi.mocked(confirm).mockResolvedValueOnce(false)
    await submit()
    expect(agentApi.setDNSHA).not.toHaveBeenCalled()
    await submit()
    expect(agentApi.setDNSHA).toHaveBeenCalledWith('oxi-1', { peer_id: 'oxi-2', vip: '192.0.2.53', vrid: 77 })
  })

  it('shows the backend warnings instead of a plain success after enabling', async () => {
    vi.mocked(agentApi.setDNSHA).mockResolvedValue({ data: { ...status('ok', []), warnings: ['无法解析 OxiDNS 配置，未能确认两台 OxiDNS 转发到同一个 mihomo'] } } as any)
    const wrapper = render()
    await click('设置 DNS 高可用')
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'oxi-2')
    await type('dns-ha-vip-oxi-1', '192.0.2.53')
    await submit()
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(notify.success).not.toHaveBeenCalled()
    expect(notify.warning).toHaveBeenCalledWith(expect.stringContaining('已开启 DNS 高可用'), expect.stringContaining('未能确认两台 OxiDNS 转发到同一个 mihomo'))
  })

  it('offers to clean up what a failed first enable left on the devices', async () => {
    const failed = { status: 'failed', error: '主 OxiDNS 无法连接；回滚未完成', updated_at: 2, members: ['oxi-2', 'oxi-1'] }
    vi.mocked(agentApi.removeDNSHA).mockRejectedValueOnce({ response: { data: { success: false, message: 'OxiDNS oxi-2 离线' } } })
    const wrapper = render({ oxidns_ha_state: failed })
    await flushPromises()
    expect(wrapper.findComponent(AgentStatusRow).props()).toMatchObject({ status: '未开启', tone: 'neutral' })
    expect(document.querySelector('[data-dns-ha-leftover]')?.textContent).toContain('回滚未完成')
    expect(agentApi.getDNSHA).not.toHaveBeenCalled()
    await click('设置 DNS 高可用')
    expect(document.querySelector('[data-dns-ha-leftover-hint]')).not.toBeNull()
    await click('清除残留配置')
    expect(confirmDanger).toHaveBeenCalledWith(expect.stringContaining('仍保留 keepalived 配置'), expect.objectContaining({ title: '清除残留配置' }))
    expect(agentApi.removeDNSHA).toHaveBeenCalledExactlyOnceWith('oxi-1')
    expect(notify.error).toHaveBeenCalledWith('无法清除残留配置', expect.stringContaining('离线'))
    await click('强制清除记录')
    expect(agentApi.removeDNSHA).toHaveBeenLastCalledWith('oxi-1', true)
    expect(notify.success).toHaveBeenCalledWith('已清除残留配置')
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('keeps the dialog open with the backend blockers when enabling fails', async () => {
    vi.mocked(agentApi.setDNSHA).mockRejectedValue({ response: { data: { success: false, message: '主 OxiDNS：缺少 keepalived，请运行 apt install keepalived' } } })
    const wrapper = render()
    await click('设置 DNS 高可用')
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'oxi-2')
    await type('dns-ha-vip-oxi-1', '192.0.2.53')
    await submit()
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('apt install keepalived')
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(notify.success).not.toHaveBeenCalled()
  })
})

describe('DNS high availability status', () => {
  it('reads status once when enabled, marks the VIP holder and keeps heartbeat polling from re-reading', async () => {
    const wrapper = render({ oxidns_ha: ha, oxidns_ha_state: { status: 'succeeded', updated_at: 1 } })
    await flushPromises()
    expect(agentApi.getDNSHA).toHaveBeenCalledExactlyOnceWith('oxi-1')
    expect(wrapper.findComponent(AgentStatusRow).props()).toMatchObject({ status: '主备正常', tone: 'success' })
    expect(wrapper.get('[data-dns-ha-holder]').text()).toBe('主 · VIP 192.0.2.53')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await wrapper.setProps({ agent: agent('oxi-1', { oxidns_ha: ha, oxidns_ha_state: { status: 'succeeded', updated_at: 1 }, last_heartbeat: 'later' }) })
    await flushPromises()
    expect(agentApi.getDNSHA).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ agents: [agent('oxi-1', { oxidns_ha: ha }), agent('oxi-2', { status: 'offline' })] })
    await flushPromises()
    expect(agentApi.getDNSHA).toHaveBeenCalledTimes(2)
  })

  it('uses the warning colour for a degraded group and explains which member is affected', async () => {
    vi.mocked(agentApi.getDNSHA).mockResolvedValue({ data: status('degraded', [
      member('oxi-1', { vip_present: true, role: 'master' }), member('oxi-2', null, { online: false }),
    ]) } as any)
    const wrapper = render({ oxidns_ha: ha })
    await flushPromises()
    expect(wrapper.findComponent(AgentStatusRow).props()).toMatchObject({ status: '部分异常', tone: 'warning' })
    expect(wrapper.get('[data-dns-ha-problems]').text()).toContain('OxiDNS oxi-2离线')
    expect(wrapper.get('[data-dns-ha-problems]').attributes('role')).toBeUndefined()
  })

  it('uses the danger colour with role="alert" for split and down groups', async () => {
    vi.mocked(agentApi.getDNSHA).mockResolvedValueOnce({ data: status('split', [
      member('oxi-1', { vip_present: true, role: 'master' }), member('oxi-2', { vip_present: true, role: 'master' }),
    ]) } as any)
    const split = render({ oxidns_ha: ha })
    await flushPromises()
    expect(split.findComponent(AgentStatusRow).props()).toMatchObject({ status: '虚拟 IP 冲突', tone: 'danger' })
    expect(split.get('[role="alert"]').text()).toContain('两台都持有虚拟 IP')

    vi.mocked(agentApi.getDNSHA).mockResolvedValueOnce({ data: status('down', [
      member('oxi-1', { keepalived_running: false, healthy: false }), member('oxi-2', { healthy: false }),
    ]) } as any)
    const down = render({ oxidns_ha: { ...ha, role: 'secondary' } })
    await flushPromises()
    expect(down.findComponent(AgentStatusRow).props()).toMatchObject({ status: '不可用', tone: 'danger' })
    expect(down.get('[role="alert"]').text()).toContain('本机的 keepalived 未运行')
    expect(down.find('[data-dns-ha-holder]').exists()).toBe(false)
  })

  it('keeps the last result when a refresh fails', async () => {
    const wrapper = render({ oxidns_ha: ha })
    await flushPromises()
    vi.mocked(agentApi.getDNSHA).mockRejectedValueOnce({ response: { data: { message: 'Agent 超时' } } })
    await wrapper.get('[aria-expanded]').trigger('click')
    await click('刷新状态')
    expect(wrapper.findComponent(AgentStatusRow).props()).toMatchObject({ status: '主备正常', tone: 'success' })
    expect(wrapper.text()).toContain('刷新失败，显示上次结果：Agent 超时')
    expect(notify.error).not.toHaveBeenCalled()
  })
})

describe('disabling DNS high availability', () => {
  it('asks with confirmDanger, then tells which routers need republishing', async () => {
    vi.mocked(agentApi.removeDNSHA).mockResolvedValue({ data: { success: true, routers: [{ id: 'r1', name: '主路由' }] } } as any)
    const wrapper = render({ oxidns_ha: ha })
    await flushPromises()
    await click('设置 DNS 高可用')
    expect(document.querySelector('form')).toBeNull()
    vi.mocked(confirmDanger).mockResolvedValueOnce(false)
    await click('停用')
    expect(agentApi.removeDNSHA).not.toHaveBeenCalled()
    await click('停用')
    expect(confirmDanger).toHaveBeenCalledTimes(2)
    expect(confirm).not.toHaveBeenCalled()
    expect(agentApi.removeDNSHA).toHaveBeenCalledExactlyOnceWith('oxi-1')
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(notify.warning).toHaveBeenCalledWith('已停用 DNS 高可用', expect.stringContaining('主路由'))
  })

  it('shows a refused disable as a warning while the pair keeps working', async () => {
    render({ oxidns_ha: ha, oxidns_ha_state: { status: 'succeeded', error: '停用未完成：OxiDNS oxi-2 无法连接', updated_at: 3 } })
    await flushPromises()
    const notice = document.querySelector('[data-dns-ha-last-error]')
    expect(notice?.textContent).toContain('停用未完成')
    expect(notice?.getAttribute('role')).toBeNull()
  })

  it('offers an explicit force clear only after a normal disable fails', async () => {
    vi.mocked(agentApi.removeDNSHA).mockRejectedValueOnce({ response: { data: { success: false, message: 'OxiDNS oxi-2 离线' } } })
    render({ oxidns_ha: ha })
    await flushPromises()
    await click('设置 DNS 高可用')
    expect(Array.from(document.querySelectorAll('button')).some(item => item.textContent?.includes('强制清除记录'))).toBe(false)
    await click('停用')
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('离线')
    await click('强制清除记录')
    expect(confirmDanger).toHaveBeenCalledTimes(2)
    expect(agentApi.removeDNSHA).toHaveBeenLastCalledWith('oxi-1', true)
  })
})


describe('service-specific high availability installation entry', () => {
  it('checks the installed Shell host, then offers backup installation when ready', async () => {
    vi.mocked(agentApi.getDockerHAEnvironment)
      .mockResolvedValueOnce({ data: { oxidns_ha_environment: { missing_tools: ['keepalived', 'jq'] } } } as any)
      .mockResolvedValueOnce({ data: { oxidns_ha_environment: { missing_tools: [] } } } as any)
    const wrapper = render({ deployment_method: 'shell' }, [])
    await click('配置 DNS 主备')
    expect(document.body.textContent).toContain('需要准备 keepalived、jq')
    expect(document.getElementById('dns-ha-vip-oxi-1')).toBeNull()
    await click('重新检查')
    await click('安装到另一台设备')
    expect(wrapper.emitted('install-backup')).toHaveLength(1)
  })
  it.each(['docker'])('offers the installation guide for a %s member without enabling HA', async deployment => {
    const wrapper = render({ deployment_method: deployment })
    await click('设置 DNS 高可用')
    await click('打开高可用安装引导')
    expect(wrapper.emitted('install-guide')).toHaveLength(1)
    expect(agentApi.setDNSHA).not.toHaveBeenCalled()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
})

describe('automatic completion of a pair stopped by a lost member', () => {
  const stopped = (state: 'waiting' | 'running' | 'blocked', message: string) => ({ ...publication('failed'),
    message: '主备设备连接中断超过 10 分钟，本次主备发布已停止；设备恢复后会自动完成本次主备发布。设备无法恢复时可以强制停用高可用',
    auto_complete: { status: state, message } })

  it('waits for the member without polling and shows the plain failure otherwise', async () => {
    vi.useFakeTimers()
    const waiting = stopped('waiting', '等待设备恢复后自动完成发布')
    vi.mocked(agentApi.getDNSHA).mockResolvedValue({ data: { ...status('ok', []), publication: waiting } } as any)
    const wrapper = render({ oxidns_ha: ha, oxidns_ha_publication: waiting })
    await flushPromises()
    expect(wrapper.findComponent(AgentStatusRow).props()).toMatchObject({ status: '等待设备恢复后自动完成发布', tone: 'warning' })
    expect(wrapper.text().match(/等待设备恢复后自动完成发布/g)).toHaveLength(1)
    expect(wrapper.find('[data-ha-auto-complete]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(6000)
    expect(agentApi.getDNSHA).toHaveBeenCalledTimes(1)
    const blocked = stopped('blocked', '无法自动完成本次主备发布：主备关系已变化；请手动完整发布一次主备配置')
    vi.mocked(agentApi.getDNSHA).mockResolvedValue({ data: { ...status('ok', []), publication: blocked } } as any)
    await wrapper.setProps({ agent: agent('oxi-1', { oxidns_ha: ha, oxidns_ha_publication: blocked }) })
    await flushPromises()
    expect(wrapper.findComponent(AgentStatusRow).props()).toMatchObject({ status: '主备同步失败', tone: 'danger' })
    expect(wrapper.get('[data-ha-auto-complete]').text()).toContain('请手动完整发布')
  })

  it('follows a starting completion until the new publication finishes and keeps its mark', async () => {
    vi.useFakeTimers()
    const running = stopped('running', '设备已恢复，正在自动完成本次主备发布')
    const attempt = { ...publication(), id: 'publication-2', auto_complete: { status: '', message: '自动补完', attempt: true } }
    vi.mocked(agentApi.getDNSHA)
      .mockResolvedValueOnce({ data: { ...status('ok', []), publication: running } } as any)
      .mockResolvedValueOnce({ data: { ...status('ok', []), publication: attempt } } as any)
      .mockResolvedValue({ data: { ...status('ok', []), publication: { ...publication('succeeded'), id: 'publication-2', auto_complete: attempt.auto_complete } } } as any)
    const wrapper = render({ oxidns_ha: ha, oxidns_ha_publication: running })
    await flushPromises()
    expect(wrapper.findComponent(AgentStatusRow).props()).toMatchObject({ status: '正在自动完成主备发布', tone: 'progress' })
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.findComponent(AgentStatusRow).props('status')).toBe('正在同步主备')
    expect(wrapper.get('[data-ha-auto-attempt]').text()).toBe('自动补完')
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.get('[data-ha-auto-attempt]').text()).toBe('自动补完')
    expect(wrapper.get('[data-ha-publication]').text()).toContain('主备配置与规则均已发布成功')
  })
})
