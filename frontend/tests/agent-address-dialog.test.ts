import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const getAddress = vi.fn()
const setAddress = vi.fn()
const success = vi.fn()
vi.mock('@/api', () => ({ agentApi: { getAddress: (...args: unknown[]) => getAddress(...args), setAddress: (...args: unknown[]) => setAddress(...args) } }))
vi.mock('@/lib/feedback', () => ({ notify: { success: (...args: unknown[]) => success(...args), error: vi.fn() } }))

import AgentAddressDialog from '@/components/agents/AgentAddressDialog.vue'

const agent = { id: 'a1', name: 'dns', host: '192.0.2.2', port: 8082 } as any
const info = (extra = {}) => ({ data: { success: true, data: { host: '192.0.2.2', port: 8082, mode: 'agent', message: '', addresses: ['192.0.2.2', '192.0.2.11'], configured: '192.0.2.2', reachable: true, ...extra } } })
const stubs = {
  Dialog: { template: '<div><slot /></div>' }, DialogContent: { template: '<div><slot /></div>' }, DialogHeader: { template: '<div><slot /></div>' },
  DialogTitle: { template: '<h2><slot /></h2>' }, DialogDescription: { template: '<p><slot /></p>' }, DialogFooter: { template: '<div><slot /></div>' },
}
const open = async () => {
  const wrapper = mount(AgentAddressDialog, { props: { agent }, global: { stubs } })
  await flushPromises()
  return wrapper
}
const saveButton = (wrapper: any) => wrapper.findAll('button').find((button: any) => button.text().includes('核实并保存'))

describe('AgentAddressDialog', () => {
  beforeEach(() => { getAddress.mockReset(); setAddress.mockReset(); success.mockReset() })

  it('offers the addresses of the device and saves the chosen one', async () => {
    getAddress.mockResolvedValue(info())
    setAddress.mockResolvedValue({ data: { success: true, host: '192.0.2.11', written: true, changed: true, message: '地址已改为 192.0.2.11' } })
    const wrapper = await open()
    expect(wrapper.text()).toContain('同时写入面板记录和设备上的 Agent 配置文件')
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()      // nothing changed yet
    await wrapper.findAll('button').find(button => button.text() === '192.0.2.11')!.trigger('click')
    await saveButton(wrapper).trigger('click')
    await flushPromises()
    expect(setAddress).toHaveBeenCalledWith('a1', '192.0.2.11')
    expect(success).toHaveBeenCalledWith('Agent 地址已修改', '地址已改为 192.0.2.11')
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('keeps the dialog open and shows why when the new address is refused', async () => {
    getAddress.mockResolvedValue(info())
    setAddress.mockRejectedValue({ response: { data: { message: '面板连不上 192.0.2.99:8082，没有修改。' } } })
    const wrapper = await open()
    await wrapper.find('input').setValue('192.0.2.99')
    await saveButton(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-address-error]').text()).toContain('连不上')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('lets a device that pins nothing be pinned to the address the panel already has', async () => {
    getAddress.mockResolvedValue(info({ configured: '' }))
    const wrapper = await open()
    expect(saveButton(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('refuses text that is not an IPv4 address', async () => {
    getAddress.mockResolvedValue(info())
    const wrapper = await open()
    await wrapper.find('input').setValue('dns.example')
    expect(wrapper.text()).toContain('请输入 IPv4 地址')
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
  })

  it.each([
    ['ha', '这台设备是 DNS 高可用成员，不能在这里修改地址'],
    ['env', '这个容器的地址由环境变量 AGENT_IP 指定'],
  ])('only explains what to do for mode %s', async (mode, message) => {
    getAddress.mockResolvedValue(info({ mode, message, addresses: [] }))
    const wrapper = await open()
    expect(wrapper.find('[data-address-note]').text()).toContain(message)
    expect(wrapper.find('input').exists()).toBe(false)
    expect(saveButton(wrapper)).toBeUndefined()
  })

  it('says so when only the panel record changes, or when the current address is dead', async () => {
    getAddress.mockResolvedValue(info({ mode: 'record', message: '容器没有指定 AGENT_IP，这里只修改面板记录的地址', addresses: [] }))
    expect((await open()).text()).toContain('只修改面板记录')
    getAddress.mockResolvedValue(info({ mode: 'unknown', reachable: false, message: '面板用当前地址 192.0.2.2:8082 连不上这台 Agent。', addresses: [] }))
    const wrapper = await open()
    expect(wrapper.find('[data-address-note]').text()).toContain('连不上')
    expect(wrapper.find('input').exists()).toBe(true)
  })
})
