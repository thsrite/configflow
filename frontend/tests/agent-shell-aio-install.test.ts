import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import Agents from '@/views/Agents.vue'
import AgentDNSHA from '@/components/agents/AgentDNSHA.vue'
import OxiDNSHAPairing from '@/components/agents/OxiDNSHAPairing.vue'
import api, { agentApi, profileApi } from '@/api'
import { notify } from '@/lib/feedback'
import { setActiveProfileId } from '@/profileContext'
import { AGENT_CORE_INSTALL_INTERFACE, DNS_HA_PREPARE_INTERFACE, buildInstallCommand } from '@/lib/coreInstallCommand'

vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  agentApi: { getAll: vi.fn(), getDeployment: vi.fn(), getUpgrade: vi.fn(), generateInstallCommand: vi.fn(), generateCoreInstallCommand: vi.fn() },
  profileApi: { list: vi.fn() }
}))
vi.mock('@/lib/feedback', () => ({
  notify: { warning: vi.fn(), loading: vi.fn(), success: vi.fn(), error: vi.fn(), dismiss: vi.fn() },
  confirm: vi.fn(), confirmDanger: vi.fn()
}))

const downloadPath = '/api/agents/i/abcdefghijklmnopqrstuv'
const commandResponse = (script: string, overrides: Record<string, unknown> = {}) => ({ data: {
  script, download_path: downloadPath, expires_at: Math.floor(Date.now() / 1000) + 86400, ...overrides
} })
let wrapper: ReturnType<typeof mount>
const mediaDescriptor = Object.getOwnPropertyDescriptor(window, 'matchMedia')
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: (query: string) => ({
    matches: query.includes('prefers-reduced-motion'), media: query,
    addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn()
  }) })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
})
afterAll(() => {
  if (mediaDescriptor) Object.defineProperty(window, 'matchMedia', mediaDescriptor)
  else Reflect.deleteProperty(window, 'matchMedia')
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})

beforeEach(async () => {
  vi.clearAllMocks()
  localStorage.setItem('serverDomain', 'https://config.example.test')
  setActiveProfileId('default')
  vi.mocked(agentApi.getAll).mockResolvedValue({ data: [] } as any)
  vi.mocked(profileApi.list).mockResolvedValue({ data: [{ id: 'default', name: 'Default' }] } as any)
  vi.mocked(agentApi.generateInstallCommand).mockResolvedValue(commandResponse('#!/bin/sh\necho installed') as any)
  vi.mocked(agentApi.generateCoreInstallCommand).mockResolvedValue(commandResponse('#!/bin/sh\n# ConfigFlow core installer interface: 1\necho cores-installed') as any)
  vi.mocked(api.get).mockResolvedValue({ data: 'docker output' } as any)
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/agents', component: { render: () => null } }] })
  await router.push('/agents')
  await router.isReady()
  wrapper = mount(Agents, { attachTo: document.body, global: { plugins: [router], stubs: { FlowMap: true } } })
  await flushPromises()
  await click('生成安装脚本')
  await input('agent-name', 'home-proxy')
})
afterEach(() => {
  wrapper.unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  sessionStorage.clear()
})

function findButton(text: string) {
  const buttons = [...document.querySelectorAll('button')]
  const label = (button: HTMLButtonElement) => button.textContent?.trim().replace(/\s+/g, ' ')
  return buttons.find(button => label(button) === text) ?? buttons.find(button => label(button)?.startsWith(`${text} `))
}
async function click(text: string) {
  const button = findButton(text)
  expect(button, text).toBeTruthy()
  button!.click()
  await flushPromises()
}
async function expand(text: string) {
  const button = findButton(text)
  expect(button, text).toBeTruthy()
  if (button!.getAttribute('aria-expanded') !== 'true') await click(text)
}
async function input(id: string, value: string) {
  const element = document.getElementById(id) as HTMLInputElement
  expect(element, id).toBeTruthy()
  element.value = value
  element.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function radio(value: string) {
  const element = document.querySelector(`[role="radio"][value="${value}"]`) as HTMLElement
  expect(element, value).toBeTruthy()
  element.click()
  await flushPromises()
}
function generatedCommandUrl() {
  const command = (document.querySelector('[aria-label="一键安装命令"] textarea') as HTMLTextAreaElement).value
  return commandUrl(command)
}

describe('OxiDNS Docker high availability entry', () => {
  const guide = () => document.querySelector('[aria-label="OxiDNS Docker HA 安装引导"]')
  const deploymentModes = () => document.querySelector('[role="radiogroup"][aria-label="OxiDNS 部署方式"]')
  const checked = (value: string) => document.querySelector(`[role="radio"][value="${value}"]`)?.getAttribute('aria-checked')

  async function openGuide() {
    await radio('docker')
    await radio('oxidns')
    await radio('ha')
  }

  it('groups high availability under Docker OxiDNS and preserves ordinary inputs when returning', async () => {
    const usageModes = document.querySelector('[role="radiogroup"][aria-label="使用模式"]')!
    expect([...usageModes.querySelectorAll('[role="radio"]')].map(element => element.getAttribute('value'))).toEqual(['ordinary', 'gateway'])
    expect(deploymentModes()).toBeNull()
    await radio('docker')
    expect(deploymentModes()).toBeNull()
    await radio('oxidns')
    expect(deploymentModes()?.textContent).toContain('常规安装')
    expect(deploymentModes()?.textContent).toContain('高可用安装')
    expect(checked('standard')).toBe('true')
    await input('agent-name', 'existing-dns')
    await input('agent-ip', '192.168.0.105')
    await input('docker-container', 'existing-oxidns')

    await radio('ha')
    expect(guide()).not.toBeNull()
    expect(document.getElementById('docker-ha-image')).not.toBeNull()
    expect(checked('ordinary')).toBe('true')
    expect(checked('docker')).toBe('true')
    expect(checked('oxidns')).toBe('true')
    expect(document.getElementById('agent-name')).toBeNull()
    expect(findButton('生成 Docker 部署命令')).toBeUndefined()

    await radio('standard')
    expect(guide()).toBeNull()
    expect((document.getElementById('agent-name') as HTMLInputElement).value).toBe('existing-dns')
    expect((document.getElementById('agent-ip') as HTMLInputElement).value).toBe('192.168.0.105')
    expect((document.getElementById('docker-container') as HTMLInputElement).value).toBe('existing-oxidns')
    expect(findButton('生成 Docker 部署命令')).toBeTruthy()
  })

  it.each(['success', 'error'])('ignores an ordinary Docker generation %s arriving after switching to HA', async outcome => {
    await radio('docker')
    await radio('oxidns')
    let finish!: (response: any) => void
    let fail!: (reason: Error) => void
    vi.mocked(api.get).mockReturnValueOnce(new Promise((resolve, reject) => { finish = resolve; fail = reject }))
    await click('生成 Docker 部署命令')
    await radio('ha')
    if (outcome === 'success') finish({ data: 'stale ordinary compose' })
    else fail(new Error('stale ordinary failure'))
    await flushPromises()
    expect(guide()).not.toBeNull()
    expect(notify.success).not.toHaveBeenCalled()
    expect(notify.error).not.toHaveBeenCalled()
    await radio('standard')
    expect([...document.querySelectorAll('textarea')].map(element => element.value).join('\n')).not.toContain('stale ordinary compose')
    expect(findButton('生成 Docker 部署命令')?.disabled).toBe(false)
  })

  it.each(['mihomo', 'mosdns', 'aio'])('returns to normal installation when switching from HA to Docker %s', async mode => {
    await openGuide()
    await radio(mode)
    expect(guide()).toBeNull()
    expect(deploymentModes()).toBeNull()
    expect(document.getElementById('agent-name')).not.toBeNull()
    expect(findButton('生成 Docker 部署命令')).toBeTruthy()
    await radio('oxidns')
    expect(checked('standard')).toBe('true')
    expect(guide()).toBeNull()
  })

  it('resets high availability when changing installation type and keeps gateway independent', async () => {
    await openGuide()
    await radio('shell')
    expect(guide()).toBeNull()
    expect(deploymentModes()).not.toBeNull()
    expect(checked('standard')).toBe('true')
    expect(findButton('生成安装命令')).toBeTruthy()
    await radio('docker')
    expect(checked('standard')).toBe('true')
    await radio('gateway')
    expect(deploymentModes()).toBeNull()
    expect(guide()).toBeNull()
    expect(findButton('开始或继续网关安装')).toBeTruthy()
  })

  it('starts a newly opened installation dialog without a previous HA selection', async () => {
    await openGuide()
    await click('关闭')
    await click('生成安装脚本')
    expect(checked('ordinary')).toBe('true')
    expect(checked('shell')).toBe('true')
    expect(guide()).toBeNull()
    await radio('docker')
    await radio('oxidns')
    expect(checked('standard')).toBe('true')
  })

  it('opens the OxiDNS HA guide directly from an Agent card', async () => {
    await click('关闭')
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [{
      id: 'docker-oxidns', name: 'Existing Docker DNS', service_type: 'oxidns',
      host: '192.168.0.105', port: 8082, status: 'offline', deployment_method: 'docker', profile_id: 'default',
    }] } as any)
    await click('刷新')
    const card = wrapper.findComponent(AgentDNSHA)
    expect(card.exists()).toBe(true)
    card.vm.$emit('install-guide')
    await flushPromises()
    expect(guide()).not.toBeNull()
    for (const value of ['ordinary', 'docker', 'oxidns', 'ha']) expect(checked(value)).toBe('true')
    expect(document.getElementById('agent-name')).toBeNull()
    await radio('standard')
    expect(document.getElementById('docker-dns-ha')).not.toBeNull()
    await radio('shell')
    expect(checked('oxidns')).toBe('true')
  })
})
describe('OxiDNS Shell high availability entry', () => {
  const haScript = `#!/bin/sh\n${AGENT_CORE_INSTALL_INTERFACE}\n${DNS_HA_PREPARE_INTERFACE}\necho prepared`
  const guide = () => document.querySelector('[aria-label="OxiDNS Shell HA 安装引导"]')
  const deploymentModes = () => document.querySelector('[role="radiogroup"][aria-label="OxiDNS 部署方式"]')
  const checked = (value: string) => document.querySelector(`[role="radio"][value="${value}"]`)?.getAttribute('aria-checked')

  async function openGuide() {
    await radio('oxidns')
    await radio('ha')
  }

  it('offers HA only under single-service Shell OxiDNS and reuses the Shell installation form', async () => {
    expect(deploymentModes()).toBeNull()
    await radio('oxidns')
    expect(checked('standard')).toBe('true')
    await input('agent-name', 'shell-dns-a')
    await input('agent-ip', '192.168.0.201')
    await input('agent-port', '8082')
    await radio('ha')
    expect(guide()).not.toBeNull()
    expect(checked('shell')).toBe('true')
    expect(checked('single')).toBe('true')
    expect(document.querySelector('[aria-label="OxiDNS Docker HA 安装引导"]')).toBeNull()
    expect(document.getElementById('agent-name')).not.toBeNull()
    expect(document.body.textContent).toContain('keepalived')
    expect(document.body.textContent).toContain('支持两台 Shell')
    expect(document.body.textContent).toContain('Shell 与 Docker 混合')
    expect(document.getElementById('agent-install-core')?.getAttribute('aria-checked')).toBe('true')
    expect(document.body.textContent).not.toContain('apt-get install')
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(haScript) as any)
    await click('生成安装命令')
    expect(agentApi.generateInstallCommand).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({
      name: 'shell-dns-a', type: 'oxidns', port: 8082, agent_ip: '192.168.0.201',
      config_path: '/etc/oxidns/config.yaml', install_core: true, prepare_dns_ha: true,
    }))
    const generated = generatedCommandUrl()
    await click('2. 验证与配对')
    expect(wrapper.findComponent(OxiDNSHAPairing).exists()).toBe(true)
    expect(wrapper.findComponent(OxiDNSHAPairing).props('active')).toBe(true)
    expect(document.getElementById('agent-name')).toBeNull()
    expect(findButton('生成安装命令')).toBeUndefined()
    expect(document.querySelector('[aria-label="一键安装命令"]')).toBeNull()
    await click('1. 安装与准备')
    expect(wrapper.findComponent(OxiDNSHAPairing).props('active')).toBe(false)
    expect(wrapper.findComponent(OxiDNSHAPairing).isVisible()).toBe(false)
    expect(generatedCommandUrl()).toBe(generated)
    expect((document.getElementById('agent-name') as HTMLInputElement).value).toBe('shell-dns-a')
    expect((document.getElementById('agent-ip') as HTMLInputElement).value).toBe('192.168.0.201')
    await radio('standard')
    expect(guide()).toBeNull()
    expect((document.getElementById('agent-name') as HTMLInputElement).value).toBe('shell-dns-a')
  })

  it('defaults to installing the core and preparing HA in one unchanged short command', async () => {
    await openGuide()
    expect(document.getElementById('agent-install-core')?.getAttribute('aria-checked')).toBe('true')
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(haScript) as any)
    await click('生成安装命令')
    expect(agentApi.generateInstallCommand).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ type: 'oxidns', install_core: true, prepare_dns_ha: true }))
    expect(generatedCommandUrl()).toBe(`https://config.example.test${downloadPath}`)
    expect((document.querySelector('[aria-label="一键安装命令"] textarea') as HTMLTextAreaElement).value).toBe(buildInstallCommand(`https://config.example.test${downloadPath}`))
    expect(agentApi.generateCoreInstallCommand).not.toHaveBeenCalled()
  })

  it.each([false, true])('restores ordinary core choice %s without leaking HA preparation', async ordinaryCore => {
    await radio('oxidns')
    if (ordinaryCore) { document.getElementById('agent-install-core')!.click(); await flushPromises() }
    await radio('ha')
    expect(document.getElementById('agent-install-core')?.getAttribute('aria-checked')).toBe('true')
    document.getElementById('agent-install-core')!.click()
    await flushPromises()
    await radio('standard')
    expect(document.getElementById('agent-install-core')?.getAttribute('aria-checked')).toBe(String(ordinaryCore))
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(ordinaryCore ? `#!/bin/sh\n${AGENT_CORE_INSTALL_INTERFACE}\necho core` : '#!/bin/sh\necho agent') as any)
    await click('生成安装命令')
    const params = vi.mocked(agentApi.generateInstallCommand).mock.calls[0]![0]
    expect(params.prepare_dns_ha).toBeUndefined()
    expect(params.install_core).toBe(ordinaryCore ? true : undefined)
  })

  it('prepares HA when taking over an existing custom OxiDNS path without installing its core', async () => {
    await openGuide()
    document.getElementById('agent-install-core')!.click()
    await flushPromises()
    await expand('服务管理高级配置（可选）')
    await input('agent-config-path', '/opt/oxidns/config.yaml')
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(`#!/bin/sh\n${DNS_HA_PREPARE_INTERFACE}\necho prepared`) as any)
    await click('生成安装命令')
    const params = vi.mocked(agentApi.generateInstallCommand).mock.calls[0]![0]
    expect(params).toMatchObject({ type: 'oxidns', prepare_dns_ha: true, config_path: '/opt/oxidns/config.yaml' })
    expect(params.install_core).toBeUndefined()
    expect(generatedCommandUrl()).toBe(`https://config.example.test${downloadPath}`)
  })

  it.each([
    `#!/bin/sh\n${AGENT_CORE_INSTALL_INTERFACE}\necho old-server`,
    `#!/bin/sh\n${AGENT_CORE_INSTALL_INTERFACE}\necho old-server\n# unrelated header\n${DNS_HA_PREPARE_INTERFACE}`,
  ])('refuses a response that does not declare HA preparation in its header', async script => {
    await openGuide()
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(script) as any)
    await click('生成安装命令')
    expect(document.querySelector('[aria-label="一键安装命令"]')).toBeNull()
    expect(notify.error).toHaveBeenCalledWith(expect.stringContaining('不支持自动准备高可用环境'))
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('discards a pending HA response after changing its core installation choice', async () => {
    await openGuide()
    let finish!: (response: any) => void
    vi.mocked(agentApi.generateInstallCommand).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await click('生成安装命令')
    document.getElementById('agent-install-core')!.click()
    await flushPromises()
    finish(commandResponse(haScript))
    await flushPromises()
    expect(document.querySelector('[aria-label="一键安装命令"]')).toBeNull()
    expect(notify.success).not.toHaveBeenCalled()
  })

  it.each(['success', 'error'])('ignores a pending Shell installation %s after entering verification', async outcome => {
    await openGuide()
    let finish!: (response: any) => void
    let fail!: (reason: Error) => void
    vi.mocked(agentApi.generateInstallCommand).mockReturnValueOnce(new Promise((resolve, reject) => { finish = resolve; fail = reject }))
    await click('生成安装命令')
    await click('2. 验证与配对')
    if (outcome === 'success') finish(commandResponse('#!/bin/sh\necho stale-shell-ha'))
    else fail(new Error('stale Shell HA installation'))
    await flushPromises()
    expect(notify.success).not.toHaveBeenCalled()
    expect(notify.error).not.toHaveBeenCalled()
    await click('1. 安装与准备')
    expect(document.querySelector('[aria-label="一键安装命令"]')).toBeNull()
    expect(findButton('生成安装命令')?.disabled).toBe(false)
  })

  it.each(['mihomo', 'mosdns', 'aio'])('does not carry Shell HA into %s', async mode => {
    await openGuide()
    await radio(mode)
    expect(guide()).toBeNull()
    expect(deploymentModes()).toBeNull()
    expect(findButton('生成安装命令')).toBeTruthy()
    if (mode === 'aio') await radio('single')
    await radio('oxidns')
    expect(checked('standard')).toBe('true')
  })

  it('resets the Shell HA step when changing installation type or reopening the dialog', async () => {
    await openGuide()
    await click('2. 验证与配对')
    await radio('docker')
    expect(guide()).toBeNull()
    await radio('shell')
    await radio('oxidns')
    expect(checked('standard')).toBe('true')
    await radio('ha')
    expect(findButton('1. 安装与准备')?.getAttribute('aria-current')).toBe('step')
    await click('2. 验证与配对')
    await click('关闭')
    await click('生成安装脚本')
    expect(guide()).toBeNull()
    await openGuide()
    expect(findButton('1. 安装与准备')?.getAttribute('aria-current')).toBe('step')
  })

  it('opens an existing Shell Agent directly in shared pairing with that Agent selected', async () => {
    await click('关闭')
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [{
      id: 'shell-oxidns', name: 'Existing Shell DNS', service_type: 'oxidns',
      host: '192.168.0.201', port: 8082, status: 'offline', deployment_method: 'shell', profile_id: 'default',
    }] } as any)
    await click('刷新')
    wrapper.findComponent(AgentDNSHA).vm.$emit('install-guide')
    await flushPromises()
    expect(guide()).not.toBeNull()
    for (const value of ['ordinary', 'shell', 'single', 'oxidns', 'ha']) expect(checked(value)).toBe('true')
    expect(findButton('2. 验证与配对')?.getAttribute('aria-current')).toBe('step')
    const pairing = wrapper.findComponent(OxiDNSHAPairing)
    expect(pairing.exists()).toBe(true)
    expect(pairing.props('initialAgentId')).toBe('shell-oxidns')
    expect(pairing.props('profileId')).toBe('default')
    expect(document.getElementById('agent-name')).toBeNull()
    expect(findButton('生成安装命令')).toBeUndefined()
    await click('1. 安装与准备')
    expect(document.getElementById('agent-install-core')?.getAttribute('aria-checked')).toBe('false')
    expect((document.getElementById('agent-name') as HTMLInputElement).value).toBe('Existing Shell DNS')
    expect((document.getElementById('agent-port') as HTMLInputElement).value).toBe('8082')
    expect((document.getElementById('agent-ip') as HTMLInputElement).value).toBe('192.168.0.201')
    expect(document.getElementById('docker-dns-ha')).toBeNull()
  })
})
function commandUrl(command: string) {
  return command.match(/'(https?:\/\/[^']+)'/)![1]!
}

describe('Shell Mihomo + OxiDNS installation', () => {
  it('generates one script snapshot with independent Agent ports and a short download URL', async () => {
    await radio('aio')
    expect((document.getElementById('mihomo-agent-port') as HTMLInputElement).value).toBe('8080')
    expect((document.getElementById('oxidns-agent-port') as HTMLInputElement).value).toBe('8082')
    expect(document.getElementById('mosdns-agent-port')).toBeNull()
    expect(document.getElementById('agent-config-path')).toBeNull()
    expect(document.getElementById('aio-install-cores')?.getAttribute('aria-checked')).toBe('false')
    expect(findButton('安装说明')?.getAttribute('aria-expanded')).toBe('false')
    expect(document.body.textContent).not.toContain('IPv4 转发')
    await expand('安装说明')
    expect(document.body.textContent).toContain('127.0.0.1:1053')
    expect(document.body.textContent).toContain('发布时保留自定义监听地址、端口及 dns-hijack')
    expect(document.body.textContent).not.toContain('自动将 DNS 监听')
    expect(document.body.textContent).toContain('IPv4 转发')

    await click('生成安装命令')
    const params = {
      name: 'home-proxy', type: 'aio', agent_ip: '', server_url: 'https://config.example.test',
      mihomo_agent_port: 8080, oxidns_agent_port: 8082,
      mihomo_config_path: '/etc/mihomo/config.yaml', oxidns_config_path: '/etc/oxidns/config.yaml',
      mihomo_service_unit: 'mihomo', oxidns_service_unit: 'oxidns',
      mihomo_service_binary: 'mihomo', oxidns_service_binary: 'oxidns'
    }
    expect(agentApi.generateInstallCommand).toHaveBeenCalledExactlyOnceWith(params)
    expect(generatedCommandUrl()).toBe(`https://config.example.test${downloadPath}`)
    expect(document.body.textContent).toContain('链接有效至')
    vi.mocked(Element.prototype.scrollIntoView).mockClear()
    await click('查看完整安装脚本')
    expect([...document.querySelectorAll('[aria-label="一键安装命令"] textarea')].map(element => (element as HTMLTextAreaElement).value)).toContain('#!/bin/sh\necho installed')
    // It opens below the dialog's visible area: scroll it into view.
    const scrolled = vi.mocked(Element.prototype.scrollIntoView).mock.contexts as Element[]
    expect(scrolled.map(element => element.id)).toContain('install-script-preview')
  })

  it('uses each core’s custom paths and excludes stale single-service lifecycle settings', async () => {
    await expand('服务管理高级配置（可选）')
    await input('agent-restart', '/opt/single/control restart')
    await radio('aio')
    await expand('核心服务高级配置')
    await input('aio-mihomo-config-path', '/opt/mihomo/config.yaml')
    await input('aio-oxidns-config-path', '/opt/oxidns/config.yaml')
    await input('aio-mihomo-service-unit', 'mihomo-native')
    await input('aio-oxidns-service-unit', 'oxidns-native')
    await input('aio-mihomo-service-binary', '/opt/mihomo/mihomo')
    await input('aio-oxidns-service-binary', '/opt/oxidns/oxidns')
    await input('oxidns-agent-port', '18082')
    await click('生成安装命令')
    const params = vi.mocked(agentApi.generateInstallCommand).mock.calls[0]![0]
    expect(params).toMatchObject({
      type: 'aio', oxidns_agent_port: 18082,
      mihomo_config_path: '/opt/mihomo/config.yaml', oxidns_config_path: '/opt/oxidns/config.yaml',
      mihomo_service_unit: 'mihomo-native', oxidns_service_unit: 'oxidns-native',
      mihomo_service_binary: '/opt/mihomo/mihomo', oxidns_service_binary: '/opt/oxidns/oxidns'
    })
    expect(params).not.toHaveProperty('restart_command')
    expect(params).not.toHaveProperty('service_manager')
    expect(params).not.toHaveProperty('config_path')
    expect(new URL(generatedCommandUrl()).search).toBe('')
  })

  it('rejects conflicting ports before requesting or presenting an installation script', async () => {
    await radio('aio')
    await input('oxidns-agent-port', '8080')
    await click('生成安装命令')
    expect(notify.warning).toHaveBeenCalledWith('两个 Agent 必须使用不同的端口')
    expect(agentApi.generateInstallCommand).not.toHaveBeenCalled()
    expect(document.querySelector('textarea')).toBeNull()
  })

  it.each([
    { mode: 'single', section: '服务管理高级配置（可选）', field: 'agent-config-path' },
    { mode: 'aio', section: '核心服务高级配置', field: 'aio-mihomo-config-path' }
  ])('reveals an invalid $mode configuration path when advanced settings are collapsed', async ({ mode, section, field }) => {
    if (mode === 'aio') await radio('aio')
    await expand(section)
    await input(field, '/opt/custom/')
    await click(section)
    expect(document.getElementById(field)).toBeNull()
    await click('生成安装命令')
    expect(agentApi.generateInstallCommand).not.toHaveBeenCalled()
    expect(notify.warning).toHaveBeenCalledWith('配置文件路径应指向一个文件（如 config.yaml），而不是文件夹')
    expect((document.getElementById(field) as HTMLInputElement)?.value).toBe('/opt/custom/')
  })

  it('keeps the existing single-service request and its explicit lifecycle auto-detection', async () => {
    await input('agent-port', '18080')
    await click('生成安装命令')
    expect(agentApi.generateInstallCommand).toHaveBeenCalledExactlyOnceWith({
      name: 'home-proxy', type: 'mihomo', port: 18080, agent_ip: '',
      config_path: '/etc/mihomo/config.yaml', restart_command: '', server_url: 'https://config.example.test'
    })
    expect(generatedCommandUrl()).toBe(`https://config.example.test${downloadPath}`)
  })

  it('keeps Docker AIO as Mihomo + OxiDNS when switching installation types', async () => {
    await radio('aio')
    await radio('docker')
    await radio('aio')
    expect(document.getElementById('mosdns-agent-port')).toBeNull()
    expect(document.getElementById('oxidns-agent-port')).not.toBeNull()
    await click('生成 Docker 部署命令')
    expect(api.get).toHaveBeenCalledWith('/agents/docker-agent-compose', { params: expect.objectContaining({
      enable_mihomo: true, enable_mosdns: false, enable_oxidns: true, mihomo_port: 8080, oxidns_port: 8082
    }) })
    expect(agentApi.generateInstallCommand).not.toHaveBeenCalled()
  })
})

describe('Docker OxiDNS HA installation', () => {
  async function prepare() {
    await radio('docker')
    expect(document.getElementById('docker-dns-ha')).toBeNull()
    await radio('oxidns')
    expect(document.getElementById('docker-dns-ha')?.getAttribute('aria-checked')).toBe('false')
    document.getElementById('docker-dns-ha')!.click()
    await flushPromises()
  }

  it('defaults HA to host without asking for a macvlan interface or subnet', async () => {
    await prepare()
    expect(document.body.textContent).toContain('宿主机固定 IPv4（必填）')
    expect(document.getElementById('docker-ha-parent')).toBeNull()
    expect(document.body.textContent).toContain('独立的高可用容器')
    await input('agent-ip', '192.168.50.20')
    await click('生成 Docker 部署命令')
    for (const endpoint of ['compose', 'run']) {
      expect(api.get).toHaveBeenCalledWith(`/agents/docker-agent-${endpoint}`, { params: expect.objectContaining({
        network_mode: 'host', agent_ip: '192.168.50.20', enable_dns_ha: true,
        enable_mihomo: false, enable_mosdns: false, enable_oxidns: true,
        ha_host_id_path: '/etc/machine-id',
      }) })
    }
    const params = vi.mocked(api.get).mock.calls.at(-1)![1]!.params
    expect(params).not.toHaveProperty('ha_parent')
    expect(params).not.toHaveProperty('ha_subnet')
  })

  it('requires a fixed host address and accepts an existing custom host identity file', async () => {
    await prepare()
    await click('生成 Docker 部署命令')
    expect(notify.warning).toHaveBeenCalledWith('请填写宿主机固定 IPv4')
    expect(api.get).not.toHaveBeenCalled()
    await input('agent-ip', '192.168.50.20')
    await expand('Docker 高级配置')
    await input('docker-ha-host-id', '/volume1/configflow/host-id')
    await click('生成 Docker 部署命令')
    expect(api.get).toHaveBeenCalledWith('/agents/docker-agent-compose', { params: expect.objectContaining({
      ha_host_id_path: '/volume1/configflow/host-id',
    }) })
  })

  it('requires macvlan addressing only after choosing macvlan', async () => {
    await prepare()
    await radio('macvlan')
    expect(document.body.textContent).toContain('容器固定 IPv4（必填）')
    expect(document.body.textContent).toContain('macvlan 默认不能与本宿主机通信')
    await input('agent-ip', '192.168.50.21')
    await click('生成 Docker 部署命令')
    expect(api.get).not.toHaveBeenCalled()
    await input('docker-ha-parent', 'enp1s0')
    await input('docker-ha-subnet', '192.168.50.0/24')
    await input('docker-ha-gateway', '192.168.50.1')
    await click('生成 Docker 部署命令')
    expect(api.get).toHaveBeenCalledWith('/agents/docker-agent-run', { params: expect.objectContaining({
      network_mode: 'macvlan', ha_parent: 'enp1s0', ha_subnet: '192.168.50.0/24', ha_gateway: '192.168.50.1',
    }) })
  })

  it('does not carry the opt-in into AIO or normal OxiDNS Docker installation', async () => {
    await prepare()
    await radio('aio')
    expect(document.getElementById('docker-dns-ha')).toBeNull()
    await radio('oxidns')
    expect(document.getElementById('docker-dns-ha')?.getAttribute('aria-checked')).toBe('false')
    await click('生成 Docker 部署命令')
    const params = vi.mocked(api.get).mock.calls.at(-1)![1]!.params
    expect(params.network_mode).toBe('bridge')
    expect(params).not.toHaveProperty('enable_dns_ha')
    expect(params).not.toHaveProperty('ha_host_id_path')
  })
})

describe('curl and wget installation commands', () => {
  it.each([
    { mode: 'single', type: 'mihomo', includeCore: false },
    { mode: 'single', type: 'mosdns', includeCore: false },
    { mode: 'single', type: 'oxidns', includeCore: true },
    { mode: 'aio', type: 'aio', includeCore: false },
    { mode: 'aio', type: 'aio', includeCore: true }
  ])('switches $mode $type includeCore=$includeCore without requesting another script', async ({ mode, type, includeCore }) => {
    if (mode === 'aio') await radio('aio')
    else if (type !== 'mihomo') await radio(type)
    if (includeCore) {
      document.getElementById(mode === 'aio' ? 'aio-install-cores' : 'agent-install-core')!.click()
      await flushPromises()
      vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(`#!/bin/sh\n${AGENT_CORE_INSTALL_INTERFACE}\necho installed`) as any)
    }
    await click('生成安装命令')
    const output = () => document.querySelector('[aria-label="一键安装命令"] textarea') as HTMLTextAreaElement
    const curlCommand = output().value
    const url = commandUrl(curlCommand)
    const builder = buildInstallCommand
    expect(curlCommand).toBe(builder(url, 'curl'))
    expect(document.querySelector('[aria-label="安装命令下载方式"] [value="curl"]')?.getAttribute('aria-checked')).toBe('true')
    expect(document.body.textContent).toContain('以 root 执行')
    expect(document.querySelectorAll('[aria-label="一键安装命令"] textarea')).toHaveLength(1)
    vi.mocked(Element.prototype.scrollIntoView).mockClear()

    await radio('wget')
    expect(output().value).toBe(builder(url, 'wget'))
    expect(output().value).toContain(' && sh ')
    expect(output().value).not.toMatch(/\|\s*(?:sudo\s+)?(?:ba)?sh/)
    expect(agentApi.generateInstallCommand).toHaveBeenCalledTimes(1)
    await radio('curl')
    expect(output().value).toBe(curlCommand)
    expect(agentApi.generateInstallCommand).toHaveBeenCalledTimes(1)
    expect(Element.prototype.scrollIntoView).not.toHaveBeenCalled()
    await input('agent-name', 'another-agent')
    expect(document.querySelector('[aria-label="一键安装命令"]')).toBeNull()
  })

  it('switches the independent core downloader while retaining its selected core and preview', async () => {
    await radio('aio')
    await expand('仅安装核心')
    await radio('oxidns')
    await click('生成OxiDNS的安装命令')
    const output = () => document.getElementById('core-install-command') as HTMLTextAreaElement
    const curlCommand = output().value
    const url = commandUrl(curlCommand)
    expect(curlCommand).toBe(buildInstallCommand(url, 'curl'))
    await radio('wget')
    expect(output().value).toBe(buildInstallCommand(url, 'wget'))
    expect(agentApi.generateCoreInstallCommand).toHaveBeenCalledTimes(1)
    await click('查看核心安装脚本')
    expect((document.querySelector('[aria-label="OxiDNS的独立安装脚本"]') as HTMLTextAreaElement).value).toContain('cores-installed')
    await click('关闭')
    await click('生成安装脚本')
    await radio('aio')
    await expand('仅安装核心')
    await click('生成OxiDNS的安装命令')
    expect(output().value).toBe(curlCommand)
    expect(document.querySelector('[aria-label="核心安装命令下载方式"] [value="curl"]')?.getAttribute('aria-checked')).toBe('true')
  })

  it.each(['javascript:alert(1)', 'https://config.example.test/?token=abc', 'https://user:pass@config.example.test'])('rejects an invalid Agent-only installation URL: %s', async url => {
    localStorage.setItem('serverDomain', url)
    await click('生成安装命令')
    expect(agentApi.generateInstallCommand).not.toHaveBeenCalled()
    expect(notify.error).toHaveBeenCalledWith(expect.stringContaining('HTTP / HTTPS'))
    expect(document.querySelector('textarea')).toBeNull()
  })
})

describe('short installation links', () => {
  it.each(['agent', 'core'])('keeps the configured reverse-proxy prefix for %s downloads', async kind => {
    localStorage.setItem('serverDomain', 'https://config.example.test/panel/configflow/')
    if (kind === 'core') { await radio('aio'); await expand('仅安装核心') }
    await click(kind === 'core' ? '生成两个核心的安装命令' : '生成安装命令')
    const command = kind === 'core' ? (document.getElementById('core-install-command') as HTMLTextAreaElement).value
      : (document.querySelector('[aria-label="一键安装命令"] textarea') as HTMLTextAreaElement).value
    expect(commandUrl(command)).toBe(`https://config.example.test/panel/configflow${downloadPath}`)
    expect(new URL(commandUrl(command)).search).toBe('')
  })

  it.each([
    'https://elsewhere.example/api/agents/i/abcdefghijklmnopqrstuv',
    '//elsewhere.example/api/agents/i/abcdefghijklmnopqrstuv',
    '/api/agents/i/abcdefghijklmnopqrstuv?name=home-proxy',
    '/api/agents/i/abcdefghijklmnopqrstuv#fragment',
    '/api/agents/i/../abcdefghijklmnopqrstuv',
    '/api/agents/i/short',
    '/api/agents/i/abcdefghijklmnopqrstuv/other'
  ])('rejects an untrusted download path %s', async download_path => {
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse('#!/bin/sh\necho installed', { download_path }) as any)
    await click('生成安装命令')
    expect(document.querySelector('textarea')).toBeNull()
    expect(notify.error).toHaveBeenCalledWith('服务返回的安装链接无效，请更新 ConfigFlow 后重试')
  })

  it.each(['agent', 'core'])('asks for a panel update if the %s shortlink endpoint is unavailable', async kind => {
    const method = kind === 'core' ? agentApi.generateCoreInstallCommand : agentApi.generateInstallCommand
    vi.mocked(method).mockRejectedValueOnce({ response: { status: 404, data: '<html>not found</html>' } })
    if (kind === 'core') { await radio('aio'); await expand('仅安装核心') }
    await click(kind === 'core' ? '生成两个核心的安装命令' : '生成安装命令')
    expect(document.querySelector('textarea')).toBeNull()
    expect(notify.error).toHaveBeenCalledWith('当前服务不支持短安装链接，请更新 ConfigFlow 后重试')
  })

  it.each([
    [0, 0],
    ['expired', Math.floor(Date.now() / 1000) - 1],
    ['tomorrow', 'tomorrow'],
    [undefined, undefined],
  ])('rejects expired or missing expiry %s', async (_label, expires_at) => {
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse('#!/bin/sh\necho installed', { expires_at }) as any)
    await click('生成安装命令')
    expect(document.querySelector('textarea')).toBeNull()
    expect(notify.error).toHaveBeenCalledWith('安装链接已失效，请重新生成')
  })
})

describe('independent Mihomo + OxiDNS core installation', () => {
  it.each([{ target: 'mihomo', name: 'Mihomo' }, { target: 'oxidns', name: 'OxiDNS' }])('generates an independent command for only $name', async ({ target, name }) => {
    await input('agent-name', '')
    await radio('aio')
    await expand('仅安装核心')
    await radio(target)
    await click(`生成${name}的安装命令`)
    const command = (document.getElementById('core-install-command') as HTMLTextAreaElement).value
    expect(command).toBe(buildInstallCommand(`https://config.example.test${downloadPath}`))
    expect(command).not.toContain('--core')
    expect(agentApi.generateCoreInstallCommand).toHaveBeenCalledExactlyOnceWith(target)
    expect(agentApi.generateInstallCommand).not.toHaveBeenCalled()
  })

  it('clears commands and ignores pending results after changing the core selection', async () => {
    await radio('aio')
    await expand('仅安装核心')
    await click('生成两个核心的安装命令')
    await radio('mihomo')
    expect(document.getElementById('core-install-command')).toBeNull()
    let finish!: (value: any) => void
    vi.mocked(agentApi.generateCoreInstallCommand).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await click('生成Mihomo的安装命令')
    await radio('oxidns')
    finish({ data: '#!/bin/sh\necho stale' })
    await flushPromises()
    expect(document.getElementById('core-install-command')).toBeNull()
    await click('生成OxiDNS的安装命令')
    expect(agentApi.generateCoreInstallCommand).toHaveBeenLastCalledWith('oxidns')
    expect((document.getElementById('core-install-command') as HTMLTextAreaElement).value).toBe(buildInstallCommand(`https://config.example.test${downloadPath}`))
  })

  it('generates the core command with an empty Agent name and default layout, independently of Agent settings', async () => {
    await input('agent-name', '')
    await radio('aio')
    expect(document.querySelector('[aria-label="要安装的核心"]')).toBeNull()
    await expand('仅安装核心')
    await click('核心服务高级配置')
    await input('aio-mihomo-config-path', '/opt/custom/mihomo.yaml')
    await input('oxidns-agent-port', '8080')
    await click('生成两个核心的安装命令')
    expect(agentApi.generateCoreInstallCommand).toHaveBeenCalledExactlyOnceWith('both')
    expect(agentApi.generateInstallCommand).not.toHaveBeenCalled()
    expect(notify.warning).not.toHaveBeenCalled()
    const command = (document.getElementById('core-install-command') as HTMLTextAreaElement).value
    expect(command).toContain(`'https://config.example.test${downloadPath}'`)
    expect(command).not.toMatch(/[\r\n]/)
    expect(command).toContain('curl -fsSL ')
    expect(command).toContain(' && sh ')
    expect(command).not.toContain('sh -c ')
    expect(command).not.toContain('/opt/custom')
    expect(command).not.toContain('agent_name')
    expect(document.body.textContent).toContain('生成安装命令')
  })

  it('preserves Agent output while generating a separate core command and script preview', async () => {
    await radio('aio')
    await expand('仅安装核心')
    await click('生成安装命令')
    const agentUrl = generatedCommandUrl()
    await click('生成两个核心的安装命令')
    expect(generatedCommandUrl()).toEqual(agentUrl)
    expect(agentApi.generateInstallCommand).toHaveBeenCalledTimes(1)
    await click('查看核心安装脚本')
    expect((document.querySelector('[aria-label="两个核心的独立安装脚本"]') as HTMLTextAreaElement).value).toContain('cores-installed')
    expect(document.getElementById('core-install-command')).not.toBeNull()
  })

  it('clears core output when switching to single-service or Docker modes', async () => {
    await radio('aio')
    await expand('仅安装核心')
    await click('生成两个核心的安装命令')
    await radio('single')
    expect(document.getElementById('core-install-command')).toBeNull()
    await radio('aio')
    await expand('仅安装核心')
    expect(document.getElementById('core-install-command')).toBeNull()
    await click('生成两个核心的安装命令')
    await radio('docker')
    await radio('shell')
    expect(document.getElementById('core-install-command')).toBeNull()
  })

  it('ignores a completed request after the user switches modes and returns', async () => {
    let finish!: (value: any) => void
    vi.mocked(agentApi.generateCoreInstallCommand).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await radio('aio')
    await expand('仅安装核心')
    await click('生成两个核心的安装命令')
    expect(document.body.textContent).toContain('正在生成核心安装命令…')
    await radio('single')
    await radio('aio')
    await expand('仅安装核心')
    finish({ data: '#!/bin/sh\necho stale' })
    await flushPromises()
    expect(document.getElementById('core-install-command')).toBeNull()
    expect(notify.success).not.toHaveBeenCalled()
    await click('生成两个核心的安装命令')
    expect(document.getElementById('core-install-command')).not.toBeNull()
  })

  it('discards a pending error after closing the dialog', async () => {
    let fail!: (reason: any) => void
    vi.mocked(agentApi.generateCoreInstallCommand).mockReturnValueOnce(new Promise((_, reject) => { fail = reject }))
    await radio('aio')
    await expand('仅安装核心')
    await click('生成两个核心的安装命令')
    await click('关闭')
    fail(new Error('stale error'))
    await flushPromises()
    expect(notify.error).not.toHaveBeenCalled()
    await click('生成安装脚本')
    await radio('aio')
    await expand('仅安装核心')
    expect(document.getElementById('core-install-command')).toBeNull()
  })

  it('shows request failure and permits retry without affecting Agent generation', async () => {
    vi.mocked(agentApi.generateCoreInstallCommand).mockRejectedValueOnce({ response: { data: { message: '核心安装文件尚未就绪' } } })
    await radio('aio')
    await expand('仅安装核心')
    await click('生成两个核心的安装命令')
    expect(notify.error).toHaveBeenCalledWith('核心安装文件尚未就绪')
    expect(document.getElementById('core-install-command')).toBeNull()
    expect(agentApi.generateInstallCommand).not.toHaveBeenCalled()
    await click('生成两个核心的安装命令')
    expect(document.getElementById('core-install-command')).not.toBeNull()
  })

  it.each(['javascript:alert(1)', 'https://config.example.test/?token=abc', 'https://user:pass@config.example.test'])('rejects an invalid installation service URL: %s', async baseUrl => {
    localStorage.setItem('serverDomain', baseUrl)
    await radio('aio')
    await expand('仅安装核心')
    await click('生成两个核心的安装命令')
    expect(agentApi.generateCoreInstallCommand).not.toHaveBeenCalled()
    expect(notify.error).toHaveBeenCalledWith(expect.stringContaining('HTTP / HTTPS'))
    expect(document.getElementById('core-install-command')).toBeNull()
  })

  it('does not present commands if an older server returns HTML instead of the script', async () => {
    vi.mocked(agentApi.generateCoreInstallCommand).mockResolvedValueOnce({ data: '<!doctype html><title>ConfigFlow</title>' } as any)
    await radio('aio')
    await expand('仅安装核心')
    await click('生成两个核心的安装命令')
    expect(notify.error).toHaveBeenCalledWith('当前服务不支持短安装链接，请更新 ConfigFlow 后重试')
    expect(document.getElementById('core-install-command')).toBeNull()
  })

  it.each(['#!/bin/sh\necho old-installer', '#!/bin/sh\n'])('rejects an older or incomplete installer before showing a selected-core command', async script => {
    vi.mocked(agentApi.generateCoreInstallCommand).mockResolvedValueOnce(commandResponse(script) as any)
    await radio('aio')
    await expand('仅安装核心')
    await radio('oxidns')
    await click('生成OxiDNS的安装命令')
    expect(notify.error).toHaveBeenCalledWith('安装脚本版本不支持单核心选择，请更新 ConfigFlow 后重试')
    expect(document.getElementById('core-install-command')).toBeNull()
  })

  it('quotes special URL characters in the generated one-line command', async () => {
    localStorage.setItem('serverDomain', "https://config.example.test/base'$(printf_injected)/")
    await radio('aio')
    await expand('仅安装核心')
    await click('生成两个核心的安装命令')
    const command = (document.getElementById('core-install-command') as HTMLTextAreaElement).value
    expect(command).not.toMatch(/[\r\n]/)
    expect(command).toContain(`base'"'"'$(printf_injected)${downloadPath}`)
  })
})


describe('single Agent with its corresponding core', () => {
  const combinedScript = `#!/bin/sh\n${AGENT_CORE_INSTALL_INTERFACE}\necho installed`
  async function selectCore() {
    const checkbox = document.getElementById('agent-install-core')!
    expect(checkbox).toBeTruthy()
    checkbox.click()
    await flushPromises()
  }
  function commands() { return [...document.querySelectorAll('textarea')].map(element => element.value) }

  it('defaults to Agent only and hides the option for MosDNS, Docker, and Shell AIO', async () => {
    expect(document.getElementById('agent-install-core')?.getAttribute('aria-checked')).toBe('false')
    await radio('mosdns')
    expect(document.getElementById('agent-install-core')).toBeNull()
    await radio('oxidns')
    expect(document.getElementById('agent-install-core')).not.toBeNull()
    await selectCore()
    await radio('aio')
    expect(document.getElementById('agent-install-core')).toBeNull()
    await radio('single')
    expect(document.getElementById('agent-install-core')?.getAttribute('aria-checked')).toBe('false')
    await radio('docker')
    expect(document.getElementById('agent-install-core')).toBeNull()
  })

  it.each(['mihomo', 'oxidns'])('generates one matching combined script and command for %s', async type => {
    if (type !== 'mihomo') await radio(type)
    await input('agent-port', '18080')
    await selectCore()
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(combinedScript) as any)
    await click('生成安装命令')
    const expected = {
      name: 'home-proxy', type, port: 18080, agent_ip: '', config_path: `/etc/${type}/config.yaml`,
      restart_command: '', server_url: 'https://config.example.test',
      service_unit: type, service_binary: `/usr/local/bin/${type}`, install_core: true
    }
    expect(agentApi.generateInstallCommand).toHaveBeenCalledExactlyOnceWith(expected)
    const command = commands()[0]!
    const actualUrl = commandUrl(command)
    expect(actualUrl).toBe(`https://config.example.test${downloadPath}`)
    expect(command).toBe(buildInstallCommand(actualUrl))
    expect(commands()).toHaveLength(1)
    expect(document.body.textContent).toContain('Linux（systemd / OpenRC）')
    expect(document.body.textContent).not.toContain('Ubuntu / Debian / CentOS')
    await click('查看完整安装脚本')
    expect(commands()).toContain(combinedScript)
    expect(agentApi.generateCoreInstallCommand).not.toHaveBeenCalled()
  })

  it.each(['53', '1053', '18080.5', '65536'])('rejects an invalid or conflicting combined Agent port %s before requesting a script', async port => {
    await selectCore()
    await input('agent-port', port)
    await click('生成安装命令')
    expect(agentApi.generateInstallCommand).not.toHaveBeenCalled()
    expect(notify.warning).toHaveBeenCalledWith(port === '1053' ? 'Mihomo DNS 使用 1053，请为 Agent 选择其他端口' : 'Agent 端口应为 1024 到 65535 之间的整数')
    expect(commands()).toEqual([])
  })

  it('shows locked defaults while preserving and restoring custom service settings', async () => {
    await expand('服务管理高级配置（可选）')
    await input('agent-config-path', '/opt/custom/main.yaml')
    await input('agent-restart', '/opt/custom/control restart')
    await input('agent-service-unit', 'custom-core')
    await input('agent-service-binary', '/opt/custom/mihomo')
    await input('agent-service-stop', '/opt/custom/control stop')
    await input('agent-service-start', '/opt/custom/control start')
    await input('agent-service-status', '/opt/custom/control status')
    await selectCore()
    for (const [id, value] of Object.entries({ 'agent-config-path': '/etc/mihomo/config.yaml', 'agent-restart': '', 'agent-service-unit': 'mihomo', 'agent-service-binary': '/usr/local/bin/mihomo' })) {
      const element = document.getElementById(id) as HTMLInputElement
      expect(element.value).toBe(value)
      expect(element.disabled).toBe(true)
    }
    expect(document.getElementById('agent-service-stop')).toBeNull()
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(combinedScript) as any)
    await click('生成安装命令')
    expect(vi.mocked(agentApi.generateInstallCommand).mock.calls[0]![0]).not.toHaveProperty('stop_command')
    await selectCore()
    expect(commands()).toEqual([])
    expect((document.getElementById('agent-config-path') as HTMLInputElement).value).toBe('/opt/custom/main.yaml')
    expect((document.getElementById('agent-config-path') as HTMLInputElement).disabled).toBe(false)
    await click('生成安装命令')
    expect(vi.mocked(agentApi.generateInstallCommand).mock.calls[1]![0]).toMatchObject({
      config_path: '/opt/custom/main.yaml', restart_command: '/opt/custom/control restart',
      service_unit: 'custom-core', service_binary: '/opt/custom/mihomo', stop_command: '/opt/custom/control stop',
      start_command: '/opt/custom/control start', status_command: '/opt/custom/control status'
    })
    expect(vi.mocked(agentApi.generateInstallCommand).mock.calls[1]![0]).not.toHaveProperty('install_core')
  })

  it.each(['#!/bin/sh\necho old-agent', '<html>old server</html>', '#!/bin/sh\n# ConfigFlow core installer interface: 1\n'])('rejects unsupported combined preview %s', async data => {
    await selectCore()
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(data) as any)
    await click('生成安装命令')
    expect(commands()).toEqual([])
    expect(notify.error).toHaveBeenCalledWith(data.startsWith('#!')
      ? '安装脚本版本不支持同时安装核心，请更新 ConfigFlow 后重试'
      : '当前服务不支持短安装链接，请更新 ConfigFlow 后重试')
  })

  it.each(['toggle', 'type', 'mode', 'name', 'close'])('discards a pending combined preview after changing %s', async change => {
    await selectCore()
    let finish!: (response: any) => void
    vi.mocked(agentApi.generateInstallCommand).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await click('生成安装命令')
    if (change === 'toggle') await selectCore()
    if (change === 'type') await radio('oxidns')
    if (change === 'mode') await radio('aio')
    if (change === 'name') await input('agent-name', 'another-agent')
    if (change === 'close') await click('关闭')
    finish(commandResponse(combinedScript))
    await flushPromises()
    expect(commands()).toEqual([])
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('discards stale errors and clears successful output when the Agent port changes', async () => {
    await selectCore()
    let reject!: (reason: any) => void
    vi.mocked(agentApi.generateInstallCommand).mockReturnValueOnce(new Promise((_, fail) => { reject = fail }))
    await click('生成安装命令')
    await input('agent-port', '18081')
    reject(new Error('stale error'))
    await flushPromises()
    expect(notify.error).not.toHaveBeenCalled()
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(combinedScript) as any)
    await click('生成安装命令')
    expect(commands()).toHaveLength(1)
    await input('agent-port', '18082')
    expect(commands()).toEqual([])
  })
})

describe('Shell AIO with both cores', () => {
  const combinedScript = `#!/bin/sh\n${AGENT_CORE_INSTALL_INTERFACE}\necho installed`
  const defaults = {
    'aio-mihomo-config-path': '/etc/mihomo/config.yaml',
    'aio-oxidns-config-path': '/etc/oxidns/config.yaml',
    'aio-mihomo-service-unit': 'mihomo',
    'aio-oxidns-service-unit': 'oxidns',
    'aio-mihomo-service-binary': '/usr/local/bin/mihomo',
    'aio-oxidns-service-binary': '/usr/local/bin/oxidns'
  }
  async function toggleCores() {
    const checkbox = document.getElementById('aio-install-cores')!
    expect(checkbox).toBeTruthy()
    checkbox.click()
    await flushPromises()
  }
  function commands() { return [...document.querySelectorAll('textarea')].map(element => element.value) }

  it('generates one combined command with both standard layouts and matching preview parameters', async () => {
    await radio('aio')
    await toggleCores()
    await input('mihomo-agent-port', '18080')
    await input('oxidns-agent-port', '18082')
    expect(document.getElementById('core-install-title')).toBeNull()
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(combinedScript) as any)
    await click('生成安装命令')
    const expected = {
      name: 'home-proxy', type: 'aio', agent_ip: '', server_url: 'https://config.example.test',
      mihomo_agent_port: 18080, oxidns_agent_port: 18082,
      mihomo_config_path: '/etc/mihomo/config.yaml', oxidns_config_path: '/etc/oxidns/config.yaml',
      mihomo_service_unit: 'mihomo', oxidns_service_unit: 'oxidns',
      mihomo_service_binary: '/usr/local/bin/mihomo', oxidns_service_binary: '/usr/local/bin/oxidns',
      install_core: true
    }
    expect(agentApi.generateInstallCommand).toHaveBeenCalledExactlyOnceWith(expected)
    expect(commands()).toHaveLength(1)
    const command = commands()[0]!
    const url = commandUrl(command)
    expect(url).toBe(`https://config.example.test${downloadPath}`)
    expect(command).toBe(buildInstallCommand(url))
    expect(command).not.toMatch(/[\r\n]/)
    expect(document.body.textContent).toContain('Linux（systemd / OpenRC）')
    expect(document.body.textContent).not.toContain('Ubuntu / Debian / CentOS')
    expect(agentApi.generateCoreInstallCommand).not.toHaveBeenCalled()
    await click('查看完整安装脚本')
    expect(commands()).toContain(combinedScript)
  })

  it('locks both standard layouts without overwriting custom takeover settings', async () => {
    await radio('aio')
    await click('核心服务高级配置')
    const custom = {
      'aio-mihomo-config-path': '/opt/mihomo/main.yaml', 'aio-oxidns-config-path': '/opt/oxidns/main.yaml',
      'aio-mihomo-service-unit': 'mihomo-native', 'aio-oxidns-service-unit': 'oxidns-native',
      'aio-mihomo-service-binary': '/opt/mihomo/core', 'aio-oxidns-service-binary': '/opt/oxidns/core'
    }
    for (const [id, value] of Object.entries(custom)) await input(id, value)
    await toggleCores()
    for (const [id, value] of Object.entries(defaults)) {
      const element = document.getElementById(id) as HTMLInputElement
      expect(element.value).toBe(value)
      expect(element.disabled).toBe(true)
    }
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(combinedScript) as any)
    await click('生成安装命令')
    expect(vi.mocked(agentApi.generateInstallCommand).mock.calls[0]![0]).toMatchObject({
      install_core: true, mihomo_config_path: '/etc/mihomo/config.yaml', oxidns_config_path: '/etc/oxidns/config.yaml'
    })
    await toggleCores()
    expect(commands()).toEqual([])
    expect(findButton('仅安装核心')).toBeTruthy()
    for (const [id, value] of Object.entries(custom)) {
      const element = document.getElementById(id) as HTMLInputElement
      expect(element.value).toBe(value)
      expect(element.disabled).toBe(false)
    }
    await click('生成安装命令')
    expect(vi.mocked(agentApi.generateInstallCommand).mock.calls[1]![0]).toMatchObject({
      mihomo_config_path: '/opt/mihomo/main.yaml', oxidns_config_path: '/opt/oxidns/main.yaml',
      mihomo_service_unit: 'mihomo-native', oxidns_service_unit: 'oxidns-native',
      mihomo_service_binary: '/opt/mihomo/core', oxidns_service_binary: '/opt/oxidns/core'
    })
    expect(vi.mocked(agentApi.generateInstallCommand).mock.calls[1]![0]).not.toHaveProperty('install_core')
  })

  it.each([
    ['mihomo-agent-port', '1053'], ['oxidns-agent-port', '1053'],
    ['mihomo-agent-port', '53'], ['oxidns-agent-port', '53'],
    ['mihomo-agent-port', '18080.5'], ['oxidns-agent-port', '65536'],
    ['oxidns-agent-port', '8080']
  ])('rejects conflicting or invalid %s=%s before generation', async (id, value) => {
    await radio('aio')
    await toggleCores()
    await input(id!, value!)
    await click('生成安装命令')
    expect(agentApi.generateInstallCommand).not.toHaveBeenCalled()
    const message = value === '1053' ? 'Mihomo DNS 使用 1053，请为 Agent 选择其他端口'
      : value === '8080' ? '两个 Agent 必须使用不同的端口' : 'Agent 端口应为 1024 到 65535 之间的整数'
    expect(notify.warning).toHaveBeenCalledWith(message)
    expect(commands()).toEqual([])
  })

  it.each(['#!/bin/sh\necho old-aio', '#!/bin/sh\n# ConfigFlow core installer interface: 1\n'])('rejects an older non-combined AIO preview %s', async data => {
    await radio('aio')
    await toggleCores()
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(data) as any)
    await click('生成安装命令')
    expect(commands()).toEqual([])
    expect(notify.error).toHaveBeenCalledWith('安装脚本版本不支持同时安装核心，请更新 ConfigFlow 后重试')
  })

  it.each(['toggle', 'mode', 'port', 'close'])('discards pending combined output after changing %s', async change => {
    await radio('aio')
    await toggleCores()
    let finish!: (response: any) => void
    vi.mocked(agentApi.generateInstallCommand).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await click('生成安装命令')
    if (change === 'toggle') await toggleCores()
    if (change === 'mode') { await radio('single'); await radio('aio') }
    if (change === 'port') await input('oxidns-agent-port', '18082')
    if (change === 'close') await click('关闭')
    finish(commandResponse(combinedScript))
    await flushPromises()
    expect(commands()).toEqual([])
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('clears an independent core command and discards in-flight core requests when combined installation is selected', async () => {
    await radio('aio')
    await expand('仅安装核心')
    await click('生成两个核心的安装命令')
    expect(document.getElementById('core-install-command')).not.toBeNull()
    await toggleCores()
    await toggleCores()
    expect(document.getElementById('core-install-command')).toBeNull()
    let finish!: (response: any) => void
    vi.mocked(agentApi.generateCoreInstallCommand).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await expand('仅安装核心')
    await click('生成两个核心的安装命令')
    await toggleCores()
    vi.mocked(notify.success).mockClear()
    finish({ data: '#!/bin/sh\n# ConfigFlow core installer interface: 1\necho stale' })
    await flushPromises()
    expect(notify.success).not.toHaveBeenCalled()
    await toggleCores()
    expect(document.getElementById('core-install-command')).toBeNull()
    expect(document.body.textContent).not.toContain('正在生成核心安装命令…')
  })

  it('resets the opt-in when switching modes and leaves Docker deployment unchanged', async () => {
    await radio('aio')
    await toggleCores()
    await radio('single')
    expect(document.getElementById('agent-install-core')?.getAttribute('aria-checked')).toBe('false')
    await radio('aio')
    expect(document.getElementById('aio-install-cores')?.getAttribute('aria-checked')).toBe('false')
    await toggleCores()
    await radio('docker')
    expect(document.getElementById('aio-install-cores')).toBeNull()
    await radio('aio')
    await click('生成 Docker 部署命令')
    expect(api.get).toHaveBeenCalledWith('/agents/docker-agent-compose', { params: expect.objectContaining({
      enable_mihomo: true, enable_mosdns: false, enable_oxidns: true
    }) })
    expect(vi.mocked(api.get).mock.calls[0]![1]!.params).not.toHaveProperty('install_core')
    expect(agentApi.generateInstallCommand).not.toHaveBeenCalled()
    await radio('shell')
    expect(document.getElementById('aio-install-cores')?.getAttribute('aria-checked')).toBe('false')
  })
})

describe('standalone Mihomo IPv4 forwarding opt-out', () => {
  const skip = () => document.getElementById('agent-skip-forwarding') as HTMLButtonElement | null
  const note = () => document.getElementById('agent-skip-forwarding-note')?.textContent?.trim()
  async function toggleSkip() {
    skip()!.click()
    await flushPromises()
  }
  const lastParams = () => vi.mocked(agentApi.generateInstallCommand).mock.calls.at(-1)![0]

  it('defaults to enabling forwarding and leaves the field out of the request', async () => {
    expect(skip()?.getAttribute('aria-checked')).toBe('false')
    expect(skip()?.getAttribute('aria-describedby')).toBe('agent-skip-forwarding-note')
    expect(document.querySelector('label[for="agent-skip-forwarding"]')?.textContent?.trim()).toBe('不开启 IPv4 转发')
    expect(note()).toBe('默认开启并持久化本机 IPv4 转发（写入 /etc/sysctl.d/99-configflow-aio-forward.conf，Agent 启动前检查），作为主路由的代理网关时需要。')
    await click('生成安装命令')
    expect(lastParams()).not.toHaveProperty('ipv4_forwarding')
  })

  it('sends ipv4_forwarding false once opted out, also with the core', async () => {
    await toggleSkip()
    expect(note()).toBe('不修改本机 IPv4 转发；已有的持久化设置会保留。被路由器用作代理网关后，可在这台设备的卡片上开启并持久化。')
    await click('生成安装命令')
    expect(lastParams()).toMatchObject({ type: 'mihomo', ipv4_forwarding: false })
    document.getElementById('agent-install-core')!.click()
    await flushPromises()
    vi.mocked(agentApi.generateInstallCommand).mockResolvedValueOnce(commandResponse(`#!/bin/sh\n${AGENT_CORE_INSTALL_INTERFACE}\necho installed`) as any)
    await click('生成安装命令')
    expect(lastParams()).toMatchObject({ install_core: true, ipv4_forwarding: false })
  })

  it('resets after switching the service or mode and is hidden outside standalone Mihomo', async () => {
    await toggleSkip()
    await radio('oxidns')
    expect(skip()).toBeNull()
    await click('生成安装命令')
    expect(lastParams()).not.toHaveProperty('ipv4_forwarding')
    await radio('mosdns')
    expect(skip()).toBeNull()
    await radio('mihomo')
    expect(skip()?.getAttribute('aria-checked')).toBe('false')
    await toggleSkip()
    await radio('aio')
    expect(skip()).toBeNull()
    await click('生成安装命令')
    expect(lastParams()).toMatchObject({ type: 'aio' })
    expect(lastParams()).not.toHaveProperty('ipv4_forwarding')
    await radio('single')
    expect(skip()?.getAttribute('aria-checked')).toBe('false')
    await radio('docker')
    expect(skip()).toBeNull()
  })

  it('is disabled and never sent for a custom lifecycle', async () => {
    await toggleSkip()
    await expand('服务管理高级配置（可选）')
    await input('agent-restart', '/opt/custom/control restart')
    expect(skip()!.disabled).toBe(true)
    expect(note()).toBe('自定义启停命令的安装不自动开启 IPv4 转发。')
    await input('agent-service-stop', '/opt/custom/control stop')
    await input('agent-service-start', '/opt/custom/control start')
    await input('agent-service-status', '/opt/custom/control status')
    await click('生成安装命令')
    expect(lastParams()).toMatchObject({ restart_command: '/opt/custom/control restart', stop_command: '/opt/custom/control stop' })
    expect(lastParams()).not.toHaveProperty('ipv4_forwarding')
    // A standard restart command keeps the option available.
    await input('agent-restart', 'systemctl restart mihomo')
    expect(skip()!.disabled).toBe(false)
  })
})
