import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import NodeLatencySettings from '@/components/nodes/NodeLatencySettings.vue'
import { nodeApi, type NodeLatencyDnsConfig } from '@/api'
import { notify } from '@/lib/feedback'

vi.mock('@/api', () => ({ nodeApi: { latencySettings: vi.fn(), saveLatencySettings: vi.fn(), checkLatencyDns: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn() } }))
let wrapper: VueWrapper | undefined
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
const system: NodeLatencyDnsConfig = { mode: 'system', servers: [], bootstrap: [] }
const custom = (servers = ['192.0.2.53:15353'], bootstrap: string[] = []): NodeLatencyDnsConfig => ({ mode: 'custom', servers, bootstrap })
function response(dns: NodeLatencyDnsConfig = system) { return { data: { success: true, dns_servers: dns.servers, dns } } as any }
function button(label: string) {
  const found = [...document.querySelectorAll('button')].find(item => (item.getAttribute('aria-label') || item.textContent?.trim()) === label)
  expect(found, label).toBeDefined()
  return found!
}
async function click(label: string) { button(label).click(); await flushPromises() }
function input(id = 'node-latency-dns') { return document.getElementById(id) as HTMLInputElement }
async function enter(value: string, id = 'node-latency-dns') {
  input(id).value = value
  input(id).dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function choose(id: string, label: string) {
  document.getElementById(id)!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  await flushPromises()
  const option = [...document.querySelectorAll<HTMLElement>('[role="option"]')].find(item => item.textContent?.includes(label))
  expect(option, label).toBeDefined()
  option!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await flushPromises()
}
async function submit() {
  document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await flushPromises()
}
async function render(disabled = false) {
  wrapper = mount(NodeLatencySettings, { props: { disabled }, attachTo: document.body })
  await flushPromises()
  return wrapper
}
beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  vi.mocked(nodeApi.latencySettings).mockResolvedValue(response())
  vi.mocked(nodeApi.saveLatencySettings).mockResolvedValue(response())
  vi.mocked(nodeApi.checkLatencyDns).mockResolvedValue({ data: { success: true, addresses: ['93.184.215.14'], latency_ms: 8 } } as any)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.unstubAllGlobals()
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})

describe('节点库测速 DNS 设置', () => {
  it('打开才读取，系统模式明确实际解析位置，不预填公共 DNS', async () => {
    await render()
    expect(nodeApi.latencySettings).not.toHaveBeenCalled()
    await click('测速设置')
    expect(nodeApi.latencySettings).toHaveBeenCalledOnce()
    expect(input()).toBeNull()
    expect(document.body.textContent).toContain('ConfigFlow 所在服务器或容器的 DNS')
    expect(document.body.textContent).toContain('全部配置空间')
    await click('自定义 DNS')
    expect(input().value).toBe('')
  })

  it('旧非空 DNS 作为自定义读取；仅显式保存', async () => {
    vi.mocked(nodeApi.latencySettings).mockResolvedValue({ data: { success: true, dns_servers: ['192.0.2.1:15353'] } } as any)
    await render()
    await click('测速设置')
    expect(input().value).toBe('192.0.2.1:15353')
    await enter(' 192.0.2.3:5353, 192.0.2.4 \n192.0.2.5\n')
    await submit()
    expect(nodeApi.saveLatencySettings).toHaveBeenCalledExactlyOnceWith(custom(['192.0.2.3:5353', '192.0.2.4', '192.0.2.5']))
    expect(wrapper!.emitted('saved')).toHaveLength(1)
    expect(notify.success).toHaveBeenCalledWith('测速设置已保存')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })

  it('切换模式只编辑草稿，不保存；保留自定义输入', async () => {
    await render()
    await click('测速设置')
    await click('自定义 DNS')
    await enter('192.0.2.53:15353')
    await click('系统 DNS')
    await click('自定义 DNS')
    expect(input().value).toBe('192.0.2.53:15353')
    expect(nodeApi.saveLatencySettings).not.toHaveBeenCalled()
  })

  it('IP DNS 不显示空引导字段；域名改成 IP 后已有引导值仍可查看和清除', async () => {
    await render()
    await click('测速设置')
    await click('自定义 DNS')
    await enter('https://192.0.2.53/dns-query')
    expect(input('node-latency-bootstrap')).toBeNull()
    await enter('https://provider.example/dns-query')
    expect(input('node-latency-bootstrap')).not.toBeNull()
    expect(document.body.textContent).toContain('先用这里的 DNS 查询 provider.example 的 IP，再连接它查询节点域名')
    await enter('192.0.2.54', 'node-latency-bootstrap')
    await enter('192.0.2.53:15353')
    expect(input('node-latency-bootstrap').value).toBe('192.0.2.54')
    expect(document.body.textContent).toContain('无需引导 DNS；可清空已有值')
    await submit()
    expect(nodeApi.saveLatencySettings).toHaveBeenCalledExactlyOnceWith(custom(['192.0.2.53:15353'], ['192.0.2.54']))
    vi.mocked(nodeApi.latencySettings).mockResolvedValue(response(custom(['192.0.2.53:15353'], ['192.0.2.54'])))
    await click('测速设置')
    await enter('', 'node-latency-bootstrap')
    expect(input('node-latency-bootstrap')).toBeNull()
    await submit()
    expect(nodeApi.saveLatencySettings).toHaveBeenLastCalledWith(custom())
  })

  it('域名 DoH / DoT 需要引导 DNS，校验失败聚焦字段；保存加密 DNS 不发布设备配置', async () => {
    await render()
    await click('测速设置')
    await click('自定义 DNS')
    await enter('https://dns.example/dns-query\ntls://dns.example:853')
    await submit()
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('引导 DNS')
    expect(document.activeElement).toBe(input('node-latency-bootstrap'))
    expect(nodeApi.saveLatencySettings).not.toHaveBeenCalled()
    await enter('192.0.2.1\n192.0.2.2\n192.0.2.3\n192.0.2.4\n192.0.2.5', 'node-latency-bootstrap')
    await click('检测 DNS')
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('最多填写 4 个')
    expect(document.activeElement).toBe(input('node-latency-bootstrap'))
    expect(nodeApi.checkLatencyDns).not.toHaveBeenCalled()
    await enter('192.0.2.53:15353', 'node-latency-bootstrap')
    await submit()
    expect(nodeApi.saveLatencySettings).toHaveBeenCalledExactlyOnceWith(custom(['https://dns.example/dns-query', 'tls://dns.example:853'], ['192.0.2.53:15353']))
  })

  it('检测当前草稿只查询 DNS，编辑后丢弃旧检测响应', async () => {
    let finish!: (value: any) => void
    vi.mocked(nodeApi.checkLatencyDns).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    vi.mocked(nodeApi.latencySettings).mockResolvedValue(response(custom()))
    await render()
    await click('测速设置')
    await click('检测 DNS')
    expect(nodeApi.checkLatencyDns).toHaveBeenCalledExactlyOnceWith(custom())
    await enter('192.0.2.54')
    finish({ data: { success: true, addresses: ['93.184.215.14'] } })
    await flushPromises()
    expect(document.body.textContent).not.toContain('解析成功')
    expect(nodeApi.saveLatencySettings).not.toHaveBeenCalled()
    await click('检测 DNS')
    const result = document.querySelector('[role="status"]')!
    expect(result.textContent).toContain('解析成功')
    expect(result.textContent).toContain('93.184.215.14')
    expect(result.textContent).toContain('8 ms')
  })

  it('检测返回 Fake-IP 时可一键改用 IP 形式的 DoH；只改草稿，检测通过后再保存', async () => {
    const doh = ['https://223.5.5.5/dns-query', 'https://1.12.12.12/dns-query']
    vi.mocked(nodeApi.checkLatencyDns).mockResolvedValue({ data: { success: false, addresses: [], error_code: 'dns_fake_ip', message: 'DNS 返回了 Fake-IP' } } as any)
    await render()
    await click('测速设置')
    await click('检测 DNS')
    expect(document.body.textContent).toContain('返回 Fake-IP')
    expect(document.body.textContent).toContain('这台机器的 DNS 被网关接管')
    await click('改用 DoH（223.5.5.5 / 1.12.12.12）')
    expect(input().value).toBe(doh.join('\n'))
    expect(input('node-latency-bootstrap')).toBeNull()
    expect(document.body.textContent).not.toContain('返回 Fake-IP')
    expect(nodeApi.saveLatencySettings).not.toHaveBeenCalled()
    vi.mocked(nodeApi.checkLatencyDns).mockResolvedValue({ data: { success: true, addresses: ['93.184.215.14'], latency_ms: 40 } } as any)
    await click('检测 DNS')
    expect(nodeApi.checkLatencyDns).toHaveBeenLastCalledWith(custom(doh))
    expect(document.body.textContent).toContain('解析成功')
    await submit()
    expect(nodeApi.saveLatencySettings).toHaveBeenCalledExactlyOnceWith(custom(doh))
  })

  it('自定义 DNS 返回 Fake-IP 时同样提示改用 IP 形式的 DoH', async () => {
    vi.mocked(nodeApi.latencySettings).mockResolvedValue(response(custom(['192.0.2.53'])))
    vi.mocked(nodeApi.checkLatencyDns).mockResolvedValue({ data: { success: false, addresses: [], error_code: 'dns_fake_ip' } } as any)
    await render()
    await click('测速设置')
    await click('检测 DNS')
    expect(document.body.textContent).toContain('53 端口的普通 DNS 和域名形式的 DoH 会被网关接管')
    await click('改用 DoH（223.5.5.5 / 1.12.12.12）')
    expect(input().value).toBe('https://223.5.5.5/dns-query\nhttps://1.12.12.12/dns-query')
  })

  it('保存返回 success:false 不算成功，保留弹窗和输入', async () => {
    vi.mocked(nodeApi.saveLatencySettings).mockResolvedValue({ data: { success: false, message: '系统设置已更新，请刷新后重试' } } as any)
    await render()
    await click('测速设置')
    await click('自定义 DNS')
    await enter('192.0.2.53:15353')
    await submit()
    expect(document.body.textContent).toContain('系统设置已更新，请刷新后重试')
    expect(notify.success).not.toHaveBeenCalled()
    expect(input().value).toBe('192.0.2.53:15353')
  })

  it('一般检测失败保留后端原因，重试成功后替换失败状态', async () => {
    vi.mocked(nodeApi.checkLatencyDns).mockResolvedValueOnce({ data: { success: false, addresses: [], error_code: 'timeout', message: 'DNS 查询超时，请检查网络和服务状态' } } as any)
    await render()
    await click('测速设置')
    await click('检测 DNS')
    expect(document.querySelector('[role="status"]')?.textContent).toContain('DNS 查询超时，请检查网络和服务状态')
    expect(document.querySelector('[role="status"]')?.textContent).not.toContain('Fake-IP')
    await click('检测 DNS')
    expect(document.querySelector('[role="status"]')?.textContent).toContain('93.184.215.14')
    expect(document.querySelector('[role="status"]')?.textContent).not.toContain('DNS 查询超时')
    expect(nodeApi.saveLatencySettings).not.toHaveBeenCalled()
  })

  it('取消不提交，重新打开重新读取；明确选择系统模式才恢复系统 DNS', async () => {
    vi.mocked(nodeApi.latencySettings).mockResolvedValue(response(custom()))
    await render()
    await click('测速设置')
    await enter('192.0.2.2')
    await click('取消')
    expect(nodeApi.saveLatencySettings).not.toHaveBeenCalled()
    await click('测速设置')
    expect(input().value).toBe('192.0.2.53:15353')
    await click('系统 DNS')
    await submit()
    expect(nodeApi.saveLatencySettings).toHaveBeenCalledExactlyOnceWith(system)
  })

  it('保存失败保留输入与弹窗，并允许重试', async () => {
    vi.mocked(nodeApi.latencySettings).mockResolvedValue(response(custom()))
    vi.mocked(nodeApi.saveLatencySettings).mockRejectedValueOnce({ response: { data: { message: 'DNS 地址无效' } } })
    await render()
    await click('测速设置')
    await enter('invalid-dns')
    await submit()
    expect(input().value).toBe('invalid-dns')
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('DNS 地址无效')
    expect(wrapper!.emitted('saved')).toBeUndefined()
    await enter('192.0.2.1')
    await submit()
    expect(wrapper!.emitted('saved')).toHaveLength(1)
  })

  it('设置读取失败不提供空表单覆盖，可以重新读取', async () => {
    vi.mocked(nodeApi.latencySettings).mockRejectedValueOnce(new Error('network'))
    await render()
    await click('测速设置')
    expect(document.querySelector('form')).toBeNull()
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('读取测速设置失败')
    await click('重新读取')
    expect(document.querySelector('form')).not.toBeNull()
  })

  it('测速禁用设置入口和已打开保存；保存中禁止重复提交和关闭', async () => {
    await render(true)
    expect(button('测速设置').disabled).toBe(true)
    await wrapper!.setProps({ disabled: false })
    await click('测速设置')
    await wrapper!.setProps({ disabled: true })
    expect(button('保存').disabled).toBe(true)
    await submit()
    expect(nodeApi.saveLatencySettings).not.toHaveBeenCalled()
    await wrapper!.setProps({ disabled: false })
    let finish!: (value: any) => void
    vi.mocked(nodeApi.saveLatencySettings).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await submit()
    expect(button('正在保存…').disabled).toBe(true)
    expect(button('取消').disabled).toBe(true)
    expect(document.querySelector('[data-slot="dialog-close"]')).toBeNull()
    await submit()
    expect(nodeApi.saveLatencySettings).toHaveBeenCalledOnce()
    finish(response())
    await flushPromises()
    expect(wrapper!.emitted('saved')).toHaveLength(1)
  })

})
