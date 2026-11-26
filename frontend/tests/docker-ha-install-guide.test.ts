import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DockerHAInstallGuide from '@/components/agents/DockerHAInstallGuide.vue'
import OxiDNSHAPairing from '@/components/agents/OxiDNSHAPairing.vue'
import DockerHAGuideCommand from '@/components/agents/DockerHAGuideCommand.vue'
import { RadioGroup } from '@/components/ui/radio-group'
import { Select } from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'
import { agentApi } from '@/api'
import { confirm } from '@/lib/feedback'
import { copyText } from '@/lib/clipboard'

vi.mock('@/api', () => ({ agentApi: {
  generateDockerHAGuide: vi.fn(), getDockerHAEnvironment: vi.fn(), getAll: vi.fn(), bindProfile: vi.fn(),
} }))
vi.mock('@/lib/feedback', () => ({ confirm: vi.fn(), notify: { success: vi.fn(), error: vi.fn() } }))
vi.mock('@/lib/clipboard', () => ({ copyText: vi.fn() }))
const PairStub = { name: 'AgentDNSHA', props: ['agent', 'agents', 'blocked', 'showInstallGuide'], template: '<div data-testid="pair" :data-blocked="blocked" />' }
const wrappers: ReturnType<typeof mount>[] = []
const result = { success: true, inspection_command: 'docker version', identity_command: '', install_command: 'install-host-command', rollback_command: '', compose: 'services: {}', warnings: [] }
const composeYaml = "name: cf-oxidns-test\nservices:\n  agent:\n    image: example/agent:ha\n    environment:\n      NOTE: '局域网 $value'\n"
const composeDeployment = {
  filename: 'compose.yaml', project_name: 'cf-oxidns-test', command: 'docker compose up -d', identity_mode: 'installation', service_count: 2,
}
const composeResult = { ...result, compose: composeYaml, inspection_command: 'standalone-inspection-script', identity_command: 'standalone-identity-script', compose_deployment: composeDeployment }

const device = (id: string, extra: Record<string, unknown> = {}) => ({
  id, name: `dns-${id}-oxidns`, host: `192.168.1.${id === 'a' ? 21 : 22}`, port: 8082,
  service_type: 'oxidns', deployment_method: 'docker', status: 'online', profile_id: 'space', ...extra,
})
const environment = (id: string, extra: Record<string, unknown> = {}) => ({
  success: true, agent_id: id, service_type: 'oxidns', oxidns_ha_protocols: [1],
  oxidns_ha_container_protocols: [1], oxidns_ha_handoff_protocols: [1],
  oxidns_ha_environment: { kind: 'container', ready: true, network: 'host', host_id: `host-${id}`, blockers: [] }, ...extra,
})
function render() {
  const wrapper = mount(DockerHAInstallGuide, { props: { profileId: 'space', profileName: '家庭 DNS', serverUrl: 'http://192.168.1.10:8000' }, global: { stubs: { AgentDNSHA: PairStub } }, attachTo: document.body })
  wrappers.push(wrapper)
  return wrapper
}
async function click(wrapper: ReturnType<typeof render>, text: string) {
  const button = wrapper.findAll('button').find(item => item.text() === text)
  expect(button, `button: ${text}`).toBeTruthy()
  await button!.trigger('click')
  await flushPromises()
}
async function radio(wrapper: ReturnType<typeof render>, label: string, value: string) {
  const group = wrapper.findAllComponents(RadioGroup).find(item => item.attributes('aria-label') === label)
  expect(group).toBeTruthy()
  group!.vm.$emit('update:modelValue', value)
  await flushPromises()
}
async function hosts(wrapper: ReturnType<typeof render>) {
  for (const index of [1, 2]) {
    await click(wrapper, `${index + 1}. 主机 ${index}`)
    await wrapper.get(`#docker-ha-host-${index}-ip`).setValue(`192.168.1.${index === 1 ? 21 : 22}`)
    await wrapper.get(`#docker-ha-host-${index}-data`).setValue(`/srv/dns-${index}`)
  }
}
async function verify(wrapper: ReturnType<typeof render>) {
  await hosts(wrapper)
  await click(wrapper, '5. 验证与配对')
  await click(wrapper, '刷新并检查两台 Agent')
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValue({ data: result } as any)
  vi.mocked(agentApi.getAll).mockResolvedValue({ data: [device('a'), device('b')] } as any)
  vi.mocked(agentApi.getDockerHAEnvironment).mockImplementation(async id => ({ data: environment(id) }) as any)
  vi.mocked(agentApi.bindProfile).mockResolvedValue({ data: {} } as any)
  vi.mocked(confirm).mockResolvedValue(true)
  vi.mocked(copyText).mockResolvedValue(undefined)
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })

describe('Docker HA installation guide', () => {
  it('defaults to compact Compose and copies or downloads the exact UTF-8 YAML without host scripts', async () => {
    vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValue({ data: composeResult } as any)
    const createDescriptor = Object.getOwnPropertyDescriptor(URL, 'createObjectURL')
    const revokeDescriptor = Object.getOwnPropertyDescriptor(URL, 'revokeObjectURL')
    const createURL = vi.fn((_blob: Blob) => 'blob:compose-download')
    const revokeURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeURL })
    const downloads: { href: string; filename: string }[] = []
    const anchorClick = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      downloads.push({ href: this.href, filename: this.download })
    })
    try {
      const wrapper = render()
      await hosts(wrapper)
      await click(wrapper, '4. 执行安装')
      await click(wrapper, '生成主机 1 的Compose')
      expect(agentApi.generateDockerHAGuide).toHaveBeenLastCalledWith(expect.objectContaining({ mode: 'new', install_method: 'compose_compact', agent_ip: '192.168.1.21', data_dir: '/srv/dns-1' }), 'space')
      // An untouched image field lets the server pick the Agent image paired with this panel build.
      expect(vi.mocked(agentApi.generateDockerHAGuide).mock.calls.at(-1)![0]).not.toHaveProperty('image')
      expect(wrapper.text()).toContain('1. 保存 Compose')
      expect(wrapper.text()).toContain('2. 启动项目')
      expect(wrapper.text()).not.toContain('prepare.sh')
      expect(wrapper.text()).toContain(composeDeployment.command)
      expect(wrapper.text()).not.toContain('standalone-inspection-script')
      expect(wrapper.text()).not.toContain('standalone-identity-script')
      expect(wrapper.text()).not.toContain('install-host-command')
      await click(wrapper, '复制 Compose')
      expect(copyText).toHaveBeenCalledWith(composeYaml)
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      await click(wrapper, '下载 compose.yaml')
      await vi.runOnlyPendingTimersAsync()
      vi.useRealTimers()
      expect(createURL).toHaveBeenCalledTimes(1)
      const blob = createURL.mock.calls[0]![0] as Blob
      expect(blob.type).toBe('text/yaml;charset=utf-8')
      const contents = await new Promise<ArrayBuffer>((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () => resolve(reader.result as ArrayBuffer)
        reader.onerror = () => reject(reader.error)
        reader.readAsArrayBuffer(blob)
      })
      expect(new TextDecoder().decode(contents)).toBe(composeYaml)
      expect(downloads).toEqual([{ href: 'blob:compose-download', filename: composeDeployment.filename }])
      expect(revokeURL).toHaveBeenCalledWith('blob:compose-download')
      await radio(wrapper, '新安装部署方式', 'script')
      expect(wrapper.findAll('button').some(button => button.text() === '下载 compose.yaml')).toBe(false)
      expect(wrapper.text()).not.toContain('install-host-command')
    } finally {
      vi.useRealTimers()
      anchorClick.mockRestore()
      if (createDescriptor) Object.defineProperty(URL, 'createObjectURL', createDescriptor)
      else Reflect.deleteProperty(URL, 'createObjectURL')
      if (revokeDescriptor) Object.defineProperty(URL, 'revokeObjectURL', revokeDescriptor)
      else Reflect.deleteProperty(URL, 'revokeObjectURL')
    }
  })

  it('rejects a successful Compose response without deployment metadata instead of showing a download or script fallback', async () => {
    const wrapper = render()
    await click(wrapper, '4. 执行安装')
    await click(wrapper, '生成主机 1 的Compose')
    expect(wrapper.findAll('[role="alert"]').some(item => /部署包|Compose/.test(item.text()))).toBe(true)
    expect(wrapper.findAll('button').some(button => button.text() === '下载 compose.yaml')).toBe(false)
    expect(wrapper.text()).not.toContain('install-host-command')
  })

  it('discards an in-flight Compose response after switching method without replacing a newer script result', async () => {
    const wrapper = render()
    let resolve!: (value: any) => void
    vi.mocked(agentApi.generateDockerHAGuide).mockImplementationOnce(() => new Promise(done => { resolve = done }) as any)
    await click(wrapper, '4. 执行安装')
    await click(wrapper, '生成主机 1 的Compose')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '生成主机 1 的安装命令')
    expect(agentApi.generateDockerHAGuide).toHaveBeenLastCalledWith(expect.objectContaining({ install_method: 'script' }), 'space')
    resolve({ data: composeResult })
    await flushPromises()
    expect(wrapper.text()).toContain('install-host-command')
    expect(wrapper.findAll('button').some(button => button.text() === '下载 compose.yaml')).toBe(false)
  })

  it('keeps deployment method and results independent for both hosts and invalidates only an edited host', async () => {
    const wrapper = render()
    await hosts(wrapper)
    vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValueOnce({ data: composeResult } as any)
    await click(wrapper, '4. 执行安装')
    await click(wrapper, '生成主机 1 的Compose')
    await radio(wrapper, '执行命令的主机', '1')
    expect(wrapper.findAll('button').some(button => button.text() === '生成主机 2 的Compose')).toBe(true)
    expect(wrapper.findAll('button').some(button => button.text() === '下载 compose.yaml')).toBe(false)
    await radio(wrapper, '新安装部署方式', 'script')
    vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValueOnce({ data: { ...result, install_command: 'second-host-script' } } as any)
    await click(wrapper, '生成主机 2 的安装命令')
    expect(agentApi.generateDockerHAGuide).toHaveBeenLastCalledWith(expect.objectContaining({ install_method: 'script', agent_ip: '192.168.1.22' }), 'space')
    await radio(wrapper, '执行命令的主机', '0')
    expect(wrapper.findAll('button').some(button => button.text() === '生成主机 1 的Compose')).toBe(true)
    expect(wrapper.findAll('button').some(button => button.text() === '下载 compose.yaml')).toBe(true)
    expect(wrapper.text()).not.toContain('second-host-script')
    await click(wrapper, '2. 主机 1')
    await wrapper.get('#docker-ha-host-1-data').setValue('/srv/changed-host-a')
    await click(wrapper, '4. 执行安装')
    expect(wrapper.findAll('button').some(button => button.text() === '下载 compose.yaml')).toBe(false)
    await radio(wrapper, '执行命令的主机', '1')
    expect(wrapper.findAll('button').some(button => button.text() === '生成主机 2 的安装命令')).toBe(true)
    expect(wrapper.text()).toContain('second-host-script')
  })

  it('starts with host prerequisites and does not imply generated commands were executed', async () => {
    const wrapper = render()
    expect(wrapper.text()).toContain('Host + 独立 HA 容器（推荐）')
    expect(wrapper.text()).toContain('Docker Engine')
    expect(wrapper.text()).toContain('强杀 HA 容器或整个 Docker 可能留下宿主 VIP')
    expect(agentApi.generateDockerHAGuide).not.toHaveBeenCalled()
    await click(wrapper, '生成环境检查命令')
    expect(agentApi.generateDockerHAGuide).toHaveBeenCalledWith({ action: 'inspect' }, 'space')
    expect(wrapper.text()).toContain('docker version')
    expect(wrapper.text()).not.toContain('安装完成')
  })

  it('limits host port-conflict guidance to Host mode and explains macvlan isolation', async () => {
    const wrapper = render()
    expect(wrapper.get('[data-docker-ha-port-note]').text()).toContain('Host 使用宿主机网络')
    expect(wrapper.get('[data-docker-ha-port-note]').text()).toContain('默认配置需要 TCP / UDP 53')
    await radio(wrapper, 'Docker HA 网络模式', 'macvlan')
    expect(wrapper.get('[data-docker-ha-port-note]').text()).toContain('宿主机 TCP / UDP 53 的占用不直接阻断')
    expect(wrapper.get('[data-docker-ha-port-note]').text()).toContain('容器地址与 VIP 未被其他设备使用')
    expect(wrapper.get('[data-docker-ha-port-note]').text()).not.toContain('已有监听需先核对并处理')
    await radio(wrapper, 'Docker HA 网络模式', 'host')
    expect(wrapper.get('[data-docker-ha-port-note]').text()).not.toContain('占用不直接阻断')
  })

  it('keeps optional preflight inputs and results when leaving and returning to the preparation step', async () => {
    const wrapper = render()
    vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValueOnce({ data: { success: true, preflight_report: { status: 'fail', summary: 'Docker 尚未启动', checks: [
      { id: 'docker', label: 'Docker 引擎', status: 'fail', detail: '无法连接', remedy: '启动 Docker 后重试。' },
    ] } } } as any)
    await wrapper.get('#docker-ha-preflight-output-1').setValue('saved host output')
    await click(wrapper, '检测主机 1')
    expect(wrapper.get('[data-preflight-report="1"]').text()).toContain('Docker 尚未启动')
    await click(wrapper, '下一步：主机 1')
    expect(wrapper.get('#docker-ha-host-1-name').isVisible()).toBe(true)
    expect(wrapper.get('#docker-ha-preflight-output-1').isVisible()).toBe(false)
    await click(wrapper, '上一步')
    expect((wrapper.get('#docker-ha-preflight-output-1').element as HTMLTextAreaElement).value).toBe('saved host output')
    expect(wrapper.get('[data-preflight-report="1"]').isVisible()).toBe(true)
    // Optional pasted checks never gate the existing install workflow.
    await click(wrapper, '4. 执行安装')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '生成主机 1 的安装命令')
    expect(wrapper.text()).toContain('install-host-command')
  })

  it('preserves both drafts through step navigation and sends explicit image and new-install settings', async () => {
    const wrapper = render()
    await wrapper.get('#docker-ha-image').setValue('registry.example/agent:ha')
    await hosts(wrapper)
    await click(wrapper, '2. 主机 1')
    expect((wrapper.get('#docker-ha-host-1-ip').element as HTMLInputElement).value).toBe('192.168.1.21')
    await click(wrapper, '4. 执行安装')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '生成主机 1 的安装命令')
    expect(agentApi.generateDockerHAGuide).toHaveBeenLastCalledWith(expect.objectContaining({ action: 'generate', mode: 'new', agent_name: 'dns-a', agent_ip: '192.168.1.21', data_dir: '/srv/dns-1', image: 'registry.example/agent:ha', network_mode: 'host', enable_dns_ha: true, enable_mihomo: false }), 'space')
    expect(wrapper.text()).toContain('命令已生成，尚未验证安装')
    await radio(wrapper, '执行命令的主机', '1')
    expect(wrapper.text()).not.toContain('install-host-command')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '生成主机 2 的安装命令')
    expect(agentApi.generateDockerHAGuide).toHaveBeenLastCalledWith(expect.objectContaining({ agent_ip: '192.168.1.22', data_dir: '/srv/dns-2' }), 'space')
  })

  it('shows macvlan isolation requirements and submits per-host LAN parameters', async () => {
    const wrapper = render()
    await radio(wrapper, 'Docker HA 网络模式', 'macvlan')
    expect(wrapper.text()).toContain('macvlan 默认不能访问本宿主机')
    await click(wrapper, '2. 主机 1')
    await wrapper.get('#docker-ha-host-1-parent').setValue('enp1s0')
    await wrapper.get('#docker-ha-host-1-subnet').setValue('192.168.1.0/24')
    await wrapper.get('#docker-ha-host-1-gateway').setValue('192.168.1.1')
    await click(wrapper, '4. 执行安装')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '生成主机 1 的安装命令')
    expect(agentApi.generateDockerHAGuide).toHaveBeenLastCalledWith(expect.objectContaining({ network_mode: 'macvlan', ha_parent: 'enp1s0', ha_subnet: '192.168.1.0/24', ha_gateway: '192.168.1.1' }), 'space')
  })

  it('keeps exact original identity and bind/volume sources for migration, with ordered backup and rollback commands', async () => {
    const wrapper = render()
    await click(wrapper, '2. 主机 1')
    await radio(wrapper, '主机 1 安装方式', 'migrate')
    await wrapper.get('#docker-ha-host-1-container').setValue('old-oxidns')
    await wrapper.get('#docker-ha-host-1-existing-name').setValue('original-full-agent-name')
    await wrapper.get('#docker-ha-host-1-agent_mount').setValue('/srv/original/agent')
    await radio(wrapper, 'docker-ha-host-1-core_mount 类型', 'volume')
    await wrapper.get('#docker-ha-host-1-core_mount').setValue('original-core-volume')
    await wrapper.get('#docker-ha-host-1-ha-data').setValue('/srv/ha')
    await wrapper.get('#docker-ha-host-1-backup').setValue('/srv/backups')
    await click(wrapper, '生成这台主机的只读检查命令')
    expect(agentApi.generateDockerHAGuide).toHaveBeenLastCalledWith({ action: 'inspect', container_name: 'old-oxidns' }, 'space')
    vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValue({ data: { ...result, rollback_command: 'safe-rollback' } } as any)
    await click(wrapper, '4. 执行安装')
    expect(wrapper.findAllComponents(RadioGroup).some(group => group.attributes('aria-label') === '新安装部署方式')).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text() === '生成主机 1 的Compose')).toBe(false)
    await click(wrapper, '生成主机 1 的安装命令')
    expect(agentApi.generateDockerHAGuide).toHaveBeenLastCalledWith(expect.objectContaining({ mode: 'migrate', install_method: 'script', container_name: 'old-oxidns', existing_agent_name: 'original-full-agent-name', agent_mount: { type: 'bind', source: '/srv/original/agent' }, core_mount: { type: 'volume', source: 'original-core-volume' }, ha_data_dir: '/srv/ha', backup_dir: '/srv/backups' }), 'space')
    expect(wrapper.text()).toContain('safe-rollback')
    expect(wrapper.text()).toContain('不要跳过脚本直接运行 Compose')
    expect(wrapper.text()).toContain('已启用 HA 的旧容器会被迁移脚本拒绝')
  })

  it.each([['host', 2], ['macvlan', 1]] as const)('installs one %s node with automatic identity and leaves host 2 empty', async (network, serviceCount) => {
    vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValue({ data: { ...composeResult,
      compose_deployment: { ...composeDeployment, service_count: serviceCount },
    } } as any)
    const wrapper = render()
    await radio(wrapper, 'Docker HA 网络模式', network)
    await click(wrapper, '2. 主机 1')
    await wrapper.get('#docker-ha-host-1-ip').setValue('192.168.1.21')
    await wrapper.get('#docker-ha-host-1-data').setValue('/srv/dns-1')
    expect(wrapper.find('#docker-ha-host-1-identity').exists()).toBe(false)
    expect(wrapper.find('#docker-ha-host-1-create-id').exists()).toBe(false)
    expect(wrapper.text()).toContain('无需填写宿主身份文件')
    await click(wrapper, '只安装这一台，前往部署')
    await click(wrapper, '生成主机 1 的Compose')
    const payload = vi.mocked(agentApi.generateDockerHAGuide).mock.calls.at(-1)![0]
    expect(payload).toEqual(expect.objectContaining({ install_method: 'compose_compact', agent_ip: '192.168.1.21', data_dir: '/srv/dns-1' }))
    expect(payload).not.toHaveProperty('create_host_identity')
    expect(payload).not.toHaveProperty('ha_host_id_path')
    expect(wrapper.text()).toContain(serviceCount === 2 ? 'Host 运行 OxiDNS 和独立 VIP 服务' : 'macvlan 容器内包含 OxiDNS、Agent 和 keepalived')
    expect(wrapper.text()).not.toContain('configflow-ha-host-identity')
    expect(wrapper.text()).not.toContain('standalone-identity-script')
  })

  it('does not let hidden script identity settings change compact enrollment inputs', async () => {
    vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValue({ data: composeResult } as any)
    const wrapper = render()
    await hosts(wrapper)
    await click(wrapper, '4. 执行安装')
    await click(wrapper, '生成主机 1 的Compose')
    const original = vi.mocked(agentApi.generateDockerHAGuide).mock.calls.at(-1)![0]
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '2. 主机 1')
    wrapper.getComponent(Checkbox).vm.$emit('update:modelValue', true)
    await flushPromises()
    await wrapper.get('#docker-ha-host-1-identity').setValue('/srv/legacy-id')
    await click(wrapper, '4. 执行安装')
    await radio(wrapper, '新安装部署方式', 'compose_compact')
    await click(wrapper, '生成主机 1 的Compose')
    expect(vi.mocked(agentApi.generateDockerHAGuide).mock.calls.at(-1)![0]).toEqual(original)
  })

  it('offers one-time stable host identity creation without reusing machine-id as a new output path', async () => {
    const wrapper = render()
    await click(wrapper, '4. 执行安装')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '2. 主机 1')
    wrapper.getComponent(Checkbox).vm.$emit('update:modelValue', true)
    await flushPromises()
    expect((wrapper.get('#docker-ha-host-1-identity').element as HTMLInputElement).value).toBe('')
    expect(wrapper.text()).toContain('两台主机分别创建')
    await wrapper.get('#docker-ha-host-1-identity').setValue('/volume1/docker/host-id')
    vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValue({ data: { ...result, identity_command: 'create-id-once' } } as any)
    await click(wrapper, '4. 执行安装')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '生成主机 1 的安装命令')
    expect(agentApi.generateDockerHAGuide).toHaveBeenLastCalledWith(expect.objectContaining({ create_host_identity: true, ha_host_id_path: '/volume1/docker/host-id' }), 'space')
    const titles = wrapper.findAllComponents(DockerHAGuideCommand).map(item => item.props('title'))
    expect(titles.slice(0, 3)).toEqual(['1. 检查宿主机', '2. 创建这台宿主机的身份文件', '3. 安装或迁移容器'])
  })

  it('invalidates generated commands after edits and ignores a stale in-flight result', async () => {
    const wrapper = render()
    let resolve!: (value: any) => void
    vi.mocked(agentApi.generateDockerHAGuide).mockImplementationOnce(() => new Promise(done => { resolve = done }) as any)
    await click(wrapper, '4. 执行安装')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '生成主机 1 的安装命令')
    await click(wrapper, '2. 主机 1')
    await wrapper.get('#docker-ha-host-1-data').setValue('/new/path')
    resolve({ data: result })
    await flushPromises()
    await click(wrapper, '4. 执行安装')
    expect(wrapper.text()).not.toContain('install-host-command')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '生成主机 1 的安装命令')
    expect(wrapper.text()).toContain('install-host-command')
    await click(wrapper, '1. 准备环境')
    await wrapper.get('#docker-ha-image').setValue('another:version')
    await click(wrapper, '4. 执行安装')
    expect(wrapper.text()).not.toContain('install-host-command')
  })

  it('retains drafts after generation failure and shows the backend reason', async () => {
    const wrapper = render()
    await hosts(wrapper)
    vi.mocked(agentApi.generateDockerHAGuide).mockRejectedValueOnce({ response: { data: { message: '旧容器挂载不匹配，请核对 inspect 输出' } } })
    await click(wrapper, '4. 执行安装')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '生成主机 1 的安装命令')
    expect(wrapper.get('[role="alert"]').text()).toContain('旧容器挂载不匹配')
    await click(wrapper, '2. 主机 1')
    expect((wrapper.get('#docker-ha-host-1-data').element as HTMLInputElement).value).toBe('/srv/dns-1')
  })

  it('clears inspection commands when switching installation mode and ignores an old in-flight response', async () => {
    const wrapper = render()
    await click(wrapper, '2. 主机 1')
    await click(wrapper, '生成这台主机的只读检查命令')
    expect(wrapper.text()).toContain('docker version')
    await radio(wrapper, '主机 1 安装方式', 'migrate')
    expect(wrapper.text()).not.toContain('docker version')
    await wrapper.get('#docker-ha-host-1-container').setValue('old-oxidns')
    let resolve!: (value: any) => void
    vi.mocked(agentApi.generateDockerHAGuide).mockImplementationOnce(() => new Promise(done => { resolve = done }) as any)
    await click(wrapper, '生成这台主机的只读检查命令')
    await radio(wrapper, '主机 1 安装方式', 'new')
    resolve({ data: { success: true, inspection_command: 'docker inspect old-oxidns' } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('docker inspect old-oxidns')
  })

  it('shows and copies the exact backend startup-log command for the current host', async () => {
    vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValue({ data: { ...result,
      command_blocks: [{ id: 'logs', title: '查看启动日志', command: 'docker logs --tail 80 exact-old-container\n' }],
    } } as any)
    const wrapper = render()
    await click(wrapper, '4. 执行安装')
    await radio(wrapper, '新安装部署方式', 'script')
    await click(wrapper, '生成主机 1 的安装命令')
    const logs = wrapper.findAllComponents(DockerHAGuideCommand).find(item => item.props('title') === '查看启动日志')!
    expect(logs.props('command')).toBe('docker logs --tail 80 exact-old-container\n')
    await logs.get('button').trigger('click')
    await flushPromises()
    expect(copyText).toHaveBeenCalledWith('docker logs --tail 80 exact-old-container\n')
    await radio(wrapper, '执行命令的主机', '1')
    expect(wrapper.text()).not.toContain('exact-old-container')
  })

  it('enables pairing only after live checks and hides a recursive install-guide entry', async () => {
    const wrapper = render()
    await verify(wrapper)
    expect(agentApi.getDockerHAEnvironment).toHaveBeenCalledWith('a')
    expect(agentApi.getDockerHAEnvironment).toHaveBeenCalledWith('b')
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(false)
    expect(wrapper.getComponent(PairStub).props('showInstallGuide')).toBe(false)
    expect(wrapper.text()).toContain('开启 HA 时服务端仍会重新校验')
    expect(wrapper.text()).toContain('在线心跳不能代替安装检查')
  })

  it.each(['file', 'docker-volume', 'installation'])('pairs an existing Shell node with a checked Docker %s identity', async source => {
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [device('a'), device('b', { deployment_method: 'shell' })] } as any)
    vi.mocked(agentApi.getDockerHAEnvironment).mockImplementation(async id => ({ data: environment(id, {
      oxidns_ha_container_protocols: id === 'b' ? [] : [1],
      oxidns_ha_environment: { kind: id === 'b' ? 'native' : 'container', ready: true,
        host_id: (id === 'a' ? 'a' : 'b').repeat(64), host_id_source: id === 'a' ? source : 'file',
        host_boot_id: (id === 'a' ? 'c' : 'd').repeat(64) },
    }) } as any))
    const wrapper = render()
    await verify(wrapper)
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(false)
    expect(wrapper.text()).toContain('Shell 原生')
    expect(wrapper.getComponent(PairStub).props('agents')).toHaveLength(2)
  })

  it.each([['docker-volume', 'same-boot'], ['docker-volume', 'missing-boot'], ['installation', 'same-boot'], ['installation', 'missing-boot']])('blocks mixed %s pairing with %s evidence', async (source, failure) => {
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [device('a'), device('b', { deployment_method: 'shell' })] } as any)
    vi.mocked(agentApi.getDockerHAEnvironment).mockImplementation(async id => ({ data: environment(id, {
      oxidns_ha_environment: { kind: id === 'b' ? 'native' : 'container', ready: true,
        host_id: (id === 'a' ? 'a' : 'b').repeat(64), host_id_source: id === 'a' ? source : 'file',
        host_boot_id: failure === 'missing-boot' && id === 'b' ? '' : 'c'.repeat(64) },
    }) } as any))
    const wrapper = render()
    await verify(wrapper)
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(true)
    expect(wrapper.text()).toContain(failure === 'same-boot' ? '来自同一宿主' : '宿主校验证据')
  })

  it('does not treat online heartbeat as readiness when live capability calls fail', async () => {
    vi.mocked(agentApi.getDockerHAEnvironment).mockRejectedValue(new Error('实时能力请求超时'))
    const wrapper = render()
    await verify(wrapper)
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(true)
    expect(wrapper.text()).toContain('实时能力请求超时')
  })

  it.each([
    ['environment blockers', { oxidns_ha_environment: { kind: 'container', ready: false, host_id: 'host-b', blockers: ['缺少独立 HA companion'] } }, '缺少独立 HA companion'],
    ['protocol missing', { oxidns_ha_handoff_protocols: [] }, '缺少所需的 HA 或计划迁移能力'],
    ['same host', { oxidns_ha_environment: { kind: 'container', ready: true, host_id: 'host-a' } }, '宿主身份相同'],
  ])('blocks pairing for %s', async (_name, override, text) => {
    vi.mocked(agentApi.getDockerHAEnvironment).mockImplementation(async id => ({ data: environment(id, id === 'b' ? override : {}) }) as any)
    const wrapper = render()
    await verify(wrapper)
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(true)
    expect(wrapper.text()).toContain(text)
  })

  it('invalidates live checks on selection changes and refuses a duplicate Agent', async () => {
    const wrapper = render()
    await verify(wrapper)
    wrapper.findAllComponents(Select)[1]!.vm.$emit('update:modelValue', 'a')
    await flushPromises()
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(true)
    expect(wrapper.text()).toContain('两台不同的 Agent')
    expect(wrapper.text()).toContain('尚未实时检查')
  })

  it('keeps pairing blocked until binding to the chosen profile actually succeeds', async () => {
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [device('a', { profile_id: 'other' }), device('b')] } as any)
    const wrapper = render()
    await verify(wrapper)
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(true)
    vi.mocked(agentApi.bindProfile).mockRejectedValueOnce(new Error('当前事务运行中，不能改绑'))
    await click(wrapper, '绑定到「家庭 DNS」')
    expect(agentApi.bindProfile).toHaveBeenCalledWith('a', 'space')
    expect(wrapper.text()).toContain('当前事务运行中')
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(true)
  })

  it('keeps selected Docker members when returning to verification but requires fresh checks', async () => {
    const wrapper = render()
    await verify(wrapper)
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(false)
    await click(wrapper, '4. 执行安装')
    expect(wrapper.findComponent(PairStub).exists()).toBe(false)
    await click(wrapper, '5. 验证与配对')
    expect(wrapper.findAllComponents(Select).map(select => select.props('modelValue'))).toEqual(['a', 'b'])
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(true)
    await click(wrapper, '刷新并检查两台 Agent')
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(false)
  })

  it('checks two native Shell members without requiring Docker or claiming native dependencies are ready', async () => {
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [device('a', { deployment_method: 'shell' }), device('b', { deployment_method: 'shell' })] } as any)
    vi.mocked(agentApi.getDockerHAEnvironment).mockImplementation(async id => ({ data: environment(id, {
      oxidns_ha_container_protocols: [], oxidns_ha_environment: { kind: 'native', ready: true, host_id: `host-${id}` },
    }) } as any))
    const wrapper = mount(OxiDNSHAPairing, { props: { profileId: 'space', profileName: '家庭 DNS', initialAgentId: 'a' }, global: { stubs: { AgentDNSHA: PairStub } }, attachTo: document.body })
    wrappers.push(wrapper)
    await click(wrapper as any, '刷新并检查两台 Agent')
    expect(wrapper.findAllComponents(Select)[0]!.props('modelValue')).toBe('a')
    wrapper.findAllComponents(Select)[1]!.vm.$emit('update:modelValue', 'b')
    await flushPromises()
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(true)
    await click(wrapper as any, '刷新并检查两台 Agent')
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(false)
    expect(wrapper.text()).toContain('身份已读取，依赖将在开启时检查')
    expect(wrapper.text()).toContain('正式开启时还会检查权限、服务管理和 DNS 配置')
    expect(wrapper.text()).not.toContain('Docker 端查看容器日志')
    expect(wrapper.text()).not.toContain('Docker 镜像升级')
  })

  it('ignores native environment responses after hiding the pairing step', async () => {
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [device('a', { deployment_method: 'shell' })] } as any)
    let finish!: (value: any) => void
    vi.mocked(agentApi.getDockerHAEnvironment).mockReturnValueOnce(new Promise(resolve => { finish = resolve }) as any)
    const wrapper = mount(OxiDNSHAPairing, { props: { profileId: 'space', profileName: '家庭 DNS', initialAgentId: 'a' }, global: { stubs: { AgentDNSHA: PairStub } }, attachTo: document.body })
    wrappers.push(wrapper)
    await click(wrapper as any, '刷新并检查两台 Agent')
    await wrapper.setProps({ active: false })
    finish({ data: environment('a', { oxidns_ha_environment: { kind: 'native', ready: true, host_id: 'host-a' } }) })
    await flushPromises()
    expect(wrapper.findComponent(PairStub).exists()).toBe(false)
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.text()).not.toContain('实时检查时间')
    await wrapper.setProps({ active: true })
    expect(wrapper.findAllComponents(Select)[0]!.props('modelValue')).toBe('a')
    expect(wrapper.getComponent(PairStub).props('blocked')).toBe(true)
    expect(wrapper.text()).toContain('尚未实时检查')
  })

  it('does not assume generated verification commands passed, and uses the actual paired VIP', async () => {
    vi.mocked(agentApi.getAll).mockResolvedValue({ data: [device('a', { oxidns_ha: { enabled: true, vip: '192.168.1.99', peer_id: 'b' } }), device('b')] } as any)
    vi.mocked(agentApi.generateDockerHAGuide).mockResolvedValue({ data: { success: true, verification_command: 'nslookup example.com 192.168.1.99' } } as any)
    const wrapper = render()
    await verify(wrapper)
    await click(wrapper, '生成 VIP DNS 验证命令')
    expect(agentApi.generateDockerHAGuide).toHaveBeenCalledWith({ action: 'verify', vip: '192.168.1.99' }, 'space')
    expect(wrapper.text()).toContain('nslookup example.com 192.168.1.99')
    expect(wrapper.text()).toContain('Docker 镜像升级仍需要手动重建容器')
  })

  it('copies the exact command through the shared clipboard helper', async () => {
    const wrapper = mount(DockerHAGuideCommand, { props: { title: '检查', command: "printf '%s\\n' safe" } })
    wrappers.push(wrapper)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(copyText).toHaveBeenCalledWith("printf '%s\\n' safe")
  })
})
