<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { Loader2 } from '@lucide/vue'
import { agentApi } from '@/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import FormField from '@/components/common/FormField.vue'
import OxiDNSHAPairing from './OxiDNSHAPairing.vue'
import DockerHAHostForm from './DockerHAHostForm.vue'
import DockerHAGuideCommand from './DockerHAGuideCommand.vue'
import DockerHAComposeDeployment from './DockerHAComposeDeployment.vue'
import DockerHAPreflightFeedback from './DockerHAPreflightFeedback.vue'
import type { DockerHAGuideResult, DockerHAHostForm as HostForm, DockerHANetwork } from '@/types/dockerHAGuide'

const props = defineProps<{ profileId: string; profileName: string; serverUrl: string }>()
const emit = defineEmits<{ changed: [] }>()
// This installation keeps its chosen profile even if the global space changes elsewhere.
const profile = { id: props.profileId, name: props.profileName }
const server = ref(props.serverUrl)
// Empty = the Agent image paired with this panel build (chosen by the server).
const image = ref('')
const network = ref<DockerHANetwork>('host')
const step = ref(0)
const pairingVisited = ref(false)
watch(step, value => { if (value === 4) pairingVisited.value = true }, { flush: 'sync' })
const commandHost = ref('0')
const installMethods = ref<('compose_compact' | 'script')[]>(['compose_compact', 'compose_compact'])
const steps = ['准备环境', '主机 1', '主机 2', '执行安装', '验证与配对']
const hosts = reactive<HostForm[]>([0, 1].map(index => ({
  mode: 'new', agent_name: index ? 'dns-b' : 'dns-a', agent_ip: '', oxidns_port: 8082,
  data_dir: '', container_name: '', existing_agent_name: '', agent_mount: { type: 'bind', source: '' },
  core_mount: { type: 'bind', source: '' }, ha_data_dir: '', backup_dir: '',
  ha_host_id_path: '/etc/machine-id', create_host_identity: false, ha_parent: '', ha_subnet: '', ha_gateway: '',
})))
const results = ref<(DockerHAGuideResult | null)[]>([null, null])
const inspecting = ref([false, false])
const inspection = ref(['', ''])
const inspectionError = ref(['', ''])
const generating = ref([false, false])
const errors = ref(['', ''])
const requestIds = [0, 0]
const inspectIds = [0, 0]
let disposed = false
const failureText = (error: any) => error?.response?.data?.message || error?.response?.data?.error || error?.message || '请求失败，请重试。'
const currentHost = computed(() => Number(commandHost.value))
const currentResult = computed(() => results.value[currentHost.value])
const useCompose = computed(() => hosts[currentHost.value]!.mode === 'new' && installMethods.value[currentHost.value] === 'compose_compact')
const currentLogs = computed(() => currentResult.value?.command_blocks?.find(block => block.id === 'logs'))
hosts.forEach((host, index) => {
  watch([() => JSON.stringify(host), () => installMethods.value[index], network, image, server], () => {
    requestIds[index]++
    generating.value[index] = false
    results.value[index] = null
    errors.value[index] = ''
  }, { flush: 'sync' })
  watch(() => [host.container_name, host.mode], () => {
    inspectIds[index]++
    inspecting.value[index] = false
    inspection.value[index] = ''
    inspectionError.value[index] = ''
  }, { flush: 'sync' })
})
const preflight = ref('')
const preflightBusy = ref(false)
const preflightError = ref('')
async function inspectHost(index?: number) {
  if (index === undefined) { preflightBusy.value = true; preflightError.value = '' }
  else { inspecting.value[index] = true; inspectionError.value[index] = '' }
  const token = index === undefined ? 0 : ++inspectIds[index]
  try {
    const { data } = await agentApi.generateDockerHAGuide({ action: 'inspect', ...(index !== undefined && hosts[index]!.mode === 'migrate' ? { container_name: hosts[index]!.container_name } : {}) }, profile.id)
    if (disposed || (index !== undefined && token !== inspectIds[index])) return
    if (!data.success) throw new Error(data.message || '无法生成检查命令。')
    if (index === undefined) preflight.value = data.inspection_command
    else inspection.value[index] = data.inspection_command
  } catch (error) {
    if (disposed || (index !== undefined && token !== inspectIds[index])) return
    if (index === undefined) preflightError.value = failureText(error)
    else inspectionError.value[index] = failureText(error)
  } finally {
    if (index === undefined) preflightBusy.value = false
    else if (token === inspectIds[index]) inspecting.value[index] = false
  }
}
async function generate(index: number) {
  const token = ++requestIds[index]
  generating.value[index] = true
  errors.value[index] = ''
  results.value[index] = null
  try {
    const host = JSON.parse(JSON.stringify(hosts[index]))
    const method = host.mode === 'new' ? installMethods.value[index] : 'script'
    if (method === 'compose_compact') {
      delete host.ha_host_id_path
      delete host.create_host_identity
    }
    const { data } = await agentApi.generateDockerHAGuide({
      ...host, action: 'generate', server_url: server.value,
      network_mode: network.value, ...(image.value.trim() ? { image: image.value.trim() } : {}),
      install_method: method,
      enable_dns_ha: true, enable_oxidns: true, enable_mihomo: false,
    }, profile.id)
    if (disposed || token !== requestIds[index]) return
    if (!data.success) throw new Error(data.message || '无法生成安装命令。')
    if (hosts[index]!.mode === 'new' && installMethods.value[index] === 'compose_compact' && (!data.compose_deployment || !data.compose?.trim())) throw new Error('Compose 未生成，请重试或切换为 Shell 脚本。')
    results.value[index] = data
  } catch (error) {
    if (!disposed && token === requestIds[index]) errors.value[index] = failureText(error)
  } finally {
    if (token === requestIds[index]) generating.value[index] = false
  }
}

onUnmounted(() => { disposed = true; requestIds.forEach((_, index) => requestIds[index]++); inspectIds.forEach((_, index) => inspectIds[index]++) })
</script>

<template>
  <section class="flex min-w-0 flex-col gap-5" aria-label="OxiDNS Docker HA 安装引导">
    <div>
      <h3 class="m-0 font-display text-xl">两台主机，共用一个 DNS 地址</h3>
      <p class="mb-0 mt-2 text-[13px] leading-relaxed text-muted-foreground">已有 Shell 节点无需重装，只部署 Docker 端，再到“验证与配对”中选择两台。本次使用配置空间「{{ profile.name }}」。</p>
    </div>
    <nav class="grid grid-cols-3 gap-1.5 sm:grid-cols-5" aria-label="安装步骤">
      <Button v-for="(label, index) in steps" :key="label" variant="outline" class="min-h-11 h-auto whitespace-normal px-2 py-2 text-xs" :class="step === index ? 'border-primary-accent bg-primary-soft text-primary-accent' : ''" :aria-current="step === index ? 'step' : undefined" @click="step = index">{{ index + 1 }}. {{ label }}</Button>
    </nav>

    <div v-show="step === 0" class="flex flex-col gap-4">
      <div class="rounded-md border border-border bg-muted p-3 text-[13px] leading-relaxed">
        <p class="mb-2 mt-0 font-semibold">开始前准备</p>
        <ul class="m-0 list-disc space-y-1 pl-5">
          <li>两台不同的 Linux 宿主机，位于同一局域网，使用固定 IPv4。两个容器放在同一主机不算双机 HA。</li>
          <li>Docker 端先安装 Docker Engine；新安装使用单份 Compose，需要 Docker Compose 2.20 或更新版本；也可选择不依赖 Compose 的 Shell 脚本。NAS 在套件中心安装 Docker / Container Manager。Compose 可直接导入项目启动；手动检查或使用 Shell 脚本时再通过 SSH 操作。已有 Shell 节点无需安装 Docker。</li>
          <li>需要 rootful Docker；Docker Desktop、rootless 和普通 bridge 网络不满足此引导。</li>
          <li data-docker-ha-port-note><template v-if="network === 'host'">Host 使用宿主机网络，默认配置需要 TCP / UDP 53；已有监听需先核对并处理，不要直接停止正在服务的 DNS。</template><template v-else>macvlan 使用容器独立地址，宿主机 TCP / UDP 53 的占用不直接阻断；仍需确认容器地址与 VIP 未被其他设备使用。</template></li>
          <li>准备同一份 OxiDNS 配置：监听所有地址的 UDP 53，上游 DNS 从两台设备都可达。开启 HA 时会同步这个配置空间的配置和规则。</li>
          <li>为 VIP 预留一个同网段、未使用且在 DHCP 地址池之外的 IPv4；不要填写主机、容器或网关现有地址。</li>
        </ul>
      </div>
      <FormField label="Docker HA 网络模式">
        <RadioGroup v-model="network" class="flex flex-wrap gap-3" aria-label="Docker HA 网络模式">
          <label class="flex min-h-11 items-center gap-2"><RadioGroupItem value="host" />Host + 独立 HA 容器（推荐）</label>
          <label class="flex min-h-11 items-center gap-2"><RadioGroupItem value="macvlan" />macvlan 独立地址</label>
        </RadioGroup>
      </FormField>
      <p v-if="network === 'host'" class="m-0 text-[13px] leading-relaxed">使用宿主机地址；OxiDNS 和 HA companion 分开运行，NET_ADMIN / NET_RAW 只授予 HA 容器。核心容器停止后 HA 仍可探测并让出 VIP；强杀 HA 容器或整个 Docker 可能留下宿主 VIP，需要恢复 HA 或人工清理。</p>
      <p v-else class="m-0 rounded-md bg-warning-soft p-3 text-[13px] leading-relaxed">单个容器内仍包含 OxiDNS、Agent 和 keepalived。需要宿主机和网络支持 macvlan，为每台容器分配独立 LAN 地址。macvlan 默认不能访问本宿主机；ConfigFlow 应部署在可达的其他 LAN 主机或同 macvlan 网络。NAS 不支持时使用 Host。</p>
      <FormField label="ConfigFlow 服务器地址" html-for="docker-ha-server" persistent-hint hint="两端都必须能访问，不能填写 localhost 或 127.0.0.1。"><Input id="docker-ha-server" v-model="server" class="font-mono" placeholder="http://192.168.1.10:8000" /></FormField>
      <FormField label="Agent Docker 镜像" html-for="docker-ha-image" hint="留空时使用与当前面板版本配套的镜像。自定义镜像须与面板版本配套，精简 Compose 需要 Agent 1.8.49-go 或更新镜像。"><Input id="docker-ha-image" v-model="image" class="font-mono" placeholder="与面板版本配套（默认）" /></FormField>
      <Button variant="outline" class="min-h-11 self-start" :disabled="preflightBusy" @click="inspectHost()"><Loader2 v-if="preflightBusy" class="size-4 animate-spin" />生成环境检查命令</Button>
      <p v-if="preflightError" role="alert" class="m-0 text-sm text-destructive-accent">{{ preflightError }}</p>
      <DockerHAGuideCommand v-if="preflight" title="两台宿主机分别检查" :command="preflight" collapsed note="复制完整命令到两台宿主机分别执行；将完整输出粘贴到下方检测，逐项查看需要处理的问题。" />
      <DockerHAPreflightFeedback :profile-id="profile.id" :network="network" />
    </div>

    <div v-if="step === 1 || step === 2" class="flex min-w-0 flex-col gap-4">
      <h4 class="m-0 text-sm font-semibold">填写主机 {{ step }} · {{ network === 'host' ? '宿主机地址' : 'macvlan 容器地址' }}</h4>
      <DockerHAHostForm :host="hosts[step - 1]!" :index="step - 1" :network="network" :compact-compose="installMethods[step - 1] === 'compose_compact' && hosts[step - 1]!.mode === 'new'" />
      <Button v-if="step === 1" variant="outline" class="min-h-11 self-start" @click="commandHost = '0'; step = 3">只安装这一台，前往部署</Button>
      <details v-if="hosts[step - 1]!.mode === 'migrate'" class="text-[13px] leading-relaxed">
        <summary class="min-h-11 cursor-pointer py-3">旧容器已有 HA、额外挂载或自定义启动命令？</summary>
        <p class="mt-0">自动迁移仅支持官方单 OxiDNS 容器、默认入口和启动命令，以及原 Agent / OxiDNS 两个数据挂载。已有 Docker HA 容器停用后仍保留 HA 环境变量和额外挂载，不能使用自动迁移脚本。请按下面步骤保留原 Compose 手动重建，不要把新安装的 Compose 直接覆盖上去。</p>
        <ol class="mb-0 list-decimal space-y-2 pl-5">
          <li>保存原 Compose 项目、环境变量、全部挂载和自定义启动参数；核对 Agent 完整名称和身份目录。备份原 Agent / OxiDNS 数据，保留旧镜像和容器作为回滚来源。</li>
          <li>以原 Compose 为基础，对照生成的 HA 定义补充专用 HA 持久目录、宿主身份挂载和网络设置。Host 还需独立 companion、共享 HA 目录与宿主网络证明挂载，网络权限只给 companion；macvlan 则给核心容器所需权限。保留原名称、数据、环境和其他业务挂载。</li>
          <li>已有 HA 先将路由器 DHCP / 客户端 DNS 临时改为仍在服务的健康 OxiDNS 固定地址，发布路由器配置并确认客户端解析正常。记录当前 VIP 持有者，然后在 Agent 卡片的「DNS 高可用 → 设置」中停用原组并等待清理完成；停用会让 VIP 下线。</li>
          <li>一次只重建一台，先处理原备用；确认原 Agent 身份仍在、DNS 健康和新环境检查通过，再把临时 DNS 切到这台已恢复的固定地址，确认解析后处理另一台。两台恢复后重新配对，并从第三台 LAN 设备验证 VIP，最后才把客户端 DNS 切回 VIP。</li>
          <li>失败时停止新容器，使用原 Compose / 旧容器和备份恢复，检查原 DNS；不要删除原数据卷。无法确认自定义参数兼容时停止迁移，继续使用旧服务。</li>
        </ol>
      </details>
      <Button variant="outline" class="min-h-11 self-start" :disabled="inspecting[step - 1] || (hosts[step - 1]!.mode === 'migrate' && !hosts[step - 1]!.container_name.trim())" @click="inspectHost(step - 1)"><Loader2 v-if="inspecting[step - 1]" class="size-4 animate-spin" />生成这台主机的只读检查命令</Button>
      <p v-if="inspectionError[step - 1]" role="alert" class="m-0 text-sm text-destructive-accent">{{ inspectionError[step - 1] }}</p>
      <DockerHAGuideCommand v-if="inspection[step - 1]" :title="`主机 ${step} 安装前检查`" :command="inspection[step - 1]!" note="在这台宿主机运行；迁移时从输出核对完整 Agent 名称、原 Agent 身份挂载和 OxiDNS 数据挂载。" />
    </div>

    <div v-else-if="step === 3" class="flex min-w-0 flex-col gap-4">
      <RadioGroup v-model="commandHost" class="flex gap-4" aria-label="执行命令的主机">
        <label v-for="index in [0, 1]" :key="index" class="flex min-h-11 items-center gap-2"><RadioGroupItem :value="String(index)" />主机 {{ index + 1 }} · {{ hosts[index]!.agent_ip || '未填写地址' }}</label>
      </RadioGroup>
      <p v-if="hosts[currentHost]!.mode === 'migrate'" class="m-0 text-[13px] leading-relaxed">迁移会核对原挂载并备份，请在对应宿主机执行脚本。已启用 HA 的旧容器会被迁移脚本拒绝；请返回主机步骤，展开手动重建说明。</p>
      <template v-else>
        <RadioGroup v-model="installMethods[currentHost]" class="flex flex-wrap gap-4" aria-label="新安装部署方式">
          <label class="flex min-h-11 cursor-pointer items-center gap-2 text-[13px]"><RadioGroupItem value="compose_compact" />精简 Compose</label>
          <label class="flex min-h-11 cursor-pointer items-center gap-2 text-[13px]"><RadioGroupItem value="script" />Shell 脚本</label>
        </RadioGroup>
        <p class="m-0 text-[12px] leading-relaxed text-muted-foreground">为当前 Docker 主机生成配置，已有 Shell 节点无需再次安装。</p>
      </template>
      <Button class="min-h-11 self-start" :disabled="generating[currentHost]" @click="generate(currentHost)"><Loader2 v-if="generating[currentHost]" class="size-4 animate-spin" />生成主机 {{ currentHost + 1 }} 的{{ useCompose ? 'Compose' : '安装命令' }}</Button>
      <p v-if="errors[currentHost]" role="alert" class="m-0 text-[13px] text-destructive-accent">{{ errors[currentHost] }}</p>
      <template v-if="currentResult">
        <p class="m-0 text-[13px] text-muted-foreground">{{ useCompose ? 'Compose 已生成，尚未安装。' : '命令已生成，尚未验证安装。复制命令不会自动执行，也不代表设备已就绪。' }}</p>
        <DockerHAComposeDeployment v-if="useCompose && currentResult.compose_deployment" :deployment="currentResult.compose_deployment" :compose="currentResult.compose" />
        <template v-else>
          <ul v-if="currentResult.warnings?.length" class="m-0 list-disc space-y-1 pl-5 text-[13px]"><li v-for="warning in currentResult.warnings" :key="warning">{{ warning }}</li></ul>
          <DockerHAGuideCommand title="1. 检查宿主机" :command="currentResult.inspection_command" collapsed />
          <DockerHAGuideCommand v-if="currentResult.identity_command" title="2. 创建这台宿主机的身份文件" :command="currentResult.identity_command" note="仅在该宿主机执行一次；不要把文件复制给另一台主机。" collapsed />
          <DockerHAGuideCommand :title="currentResult.identity_command ? '3. 安装或迁移容器' : '2. 安装或迁移容器'" :command="currentResult.install_command" :note="hosts[currentHost]!.mode === 'migrate' ? '此脚本会核对原挂载、停止并备份旧容器、保留旧容器再重建；不要跳过脚本直接运行 Compose。迁移期间这台 DNS 会暂停。' : '复制到对应宿主机执行。脚本创建持久目录并启动容器，结束后前往下一步刷新 Agent。'" collapsed />
        </template>
        <details v-if="hosts[currentHost]!.mode === 'new'" class="min-w-0 text-[13px] leading-relaxed">
          <summary class="min-h-11 cursor-pointer py-3">安装失败或凭据过期，如何继续？</summary>
          <p class="mt-0">保留全部设置和持久数据，修正报错后重试。首次安装凭据过期时，用相同设置重新生成{{ useCompose ? 'Compose，替换原文件后再次启动项目' : '安装命令' }}。已成功登记的 Compose 容器重启时复用本机身份，不需要重新注册。</p>
          <p class="mb-0">不要删除 Agent 身份和安装记录。改变名称、地址或目录前，先核对已有安装并选择迁移。</p>
        </details>
        <DockerHAGuideCommand v-if="currentLogs" :title="currentLogs.title" :command="currentLogs.command" :note="useCompose ? '在 compose.yaml 所在目录执行，或在容器管理器查看 OxiDNS 和 HA 服务日志。' : '在同一台宿主机执行，查看实际容器的启动错误；修正原因后再刷新 Agent。'" />
        <details v-if="currentResult.compose" class="min-w-0"><summary class="min-h-11 cursor-pointer py-3 text-[13px]">查看 Compose 定义</summary><DockerHAGuideCommand :title="useCompose ? 'compose.yaml' : 'Compose 定义（供核对）'" :command="currentResult.compose" :note="useCompose ? '此文件包含启动所需配置，可直接部署。' : '迁移必须使用上方安装脚本，以保留检查、备份和回滚路径。'" /></details>
        <DockerHAGuideCommand v-if="currentResult.rollback_command" title="迁移失败时回滚" :command="currentResult.rollback_command" note="只在对应主机运行。保留旧容器、备份和原数据，不删除卷；恢复后检查原 Agent 与 DNS 服务。" />
      </template>
    </div>

    <OxiDNSHAPairing v-if="pairingVisited" v-show="step === 4" :active="step === 4" :profile-id="profile.id" :profile-name="profile.name" :hosts="hosts" :network="network" @changed="emit('changed')" />
    <div class="flex justify-between gap-2 border-t border-border pt-4">
      <Button variant="outline" class="min-h-11" :disabled="step === 0" @click="step--">上一步</Button>
      <Button v-if="step < steps.length - 1" class="min-h-11" @click="step++">下一步：{{ steps[step + 1] }}</Button>
    </div>
  </section>
</template>
