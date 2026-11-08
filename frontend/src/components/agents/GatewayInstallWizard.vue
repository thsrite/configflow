<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { Check, Copy, LoaderCircle, RefreshCw } from '@lucide/vue'
import { gatewayOnboardingApi, type GatewayConfigurationPreview, type GatewayInstallation, type GatewayOnboardingConfig, type GatewayOnboardingSettings } from '@/api/gatewayOnboarding'
import { linuxGatewayApi, type LinuxGatewayCandidate, type LinuxGatewayPreview, type LinuxGatewayStatus } from '@/api/linuxGateway'
import { cleanGatewaySpec, emptyGatewaySpec, gatewayErrorMessage, gatewaySpecErrors, gatewaySpecKey } from '@/lib/linuxGateway'
import { setupServerError } from '@/lib/setupFlow'
import { stripTrailingPunctuation } from '@/lib/text'
import type { SetupGoal } from '@/api/setup'
import { gatewayEnvironmentErrors, installationHostMatches, installationMatches, onboardingSettingsKey } from '@/lib/gatewayOnboarding'
import { copyText } from '@/lib/clipboard'
import { forwardFilterWarning } from '@/lib/forwardFilter'
import { buildInstallCommand, type InstallerDownloader } from '@/lib/coreInstallCommand'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { forwardingLabel, forwardingTone, fullTunReady, gatewayLeftover, routerSplitErrorMessage, tunLabel, tunTone, type StatusTone } from '@/lib/routerSplit'
import SettingsWorkspace from '@/components/generate/SettingsWorkspace.vue'
import AgentDeploymentStatus from '@/components/agents/AgentDeploymentStatus.vue'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import AgentFailureDetails from '@/components/agents/AgentFailureDetails.vue'
import AgentDNSChecks from '@/components/agents/AgentDNSChecks.vue'
import RuleDownloadRemedy from '@/components/agents/RuleDownloadRemedy.vue'
import ForwardFilterNotice from '@/components/agents/ForwardFilterNotice.vue'
import FormField from '@/components/common/FormField.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import GatewayNetworkFields from '@/components/agents/GatewayNetworkFields.vue'
import GatewayClientInstructions from '@/components/agents/GatewayClientInstructions.vue'
import GatewayDetectedNetwork from '@/components/agents/GatewayDetectedNetwork.vue'
import InstallerDownloaderPicker from '@/components/agents/InstallerDownloaderPicker.vue'
import { gatewayCopy } from '@/lib/gatewayCopy'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { queryRecorderCopy } from '@/lib/oxidns'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

const props = defineProps<{ open: boolean; profileId: string; profileName: string; embedded?: boolean; goal?: SetupGoal }>()
const emit = defineEmits<{ 'update:open': [value: boolean]; 'agents-changed': []; completed: []; busy: [value: boolean] }>()
const step = ref(0)
const contentPane = ref<HTMLElement | null>(null)
const environmentErrorPanel = ref<HTMLElement | null>(null)
const config = ref<GatewayOnboardingConfig | null>(null)
const settings = reactive<GatewayOnboardingSettings>({ rule_set_ids: [], direct_rule_set_ids: [], proxy_rule_set_ids: [], local_dns: 'https://223.5.5.5/dns-query', default_forward: 'forward_remote', query_recorder: false })
const network = reactive(emptyGatewaySpec())
const name = ref('home-gateway')
const serverUrl = ref('')
// Address entered for this installation that differs from the system setting.
const serverUrlMismatch = ref('')
const environmentAttempted = ref(false)
const search = ref('')
const ruleCategory = ref('rule_set_ids')
const configurationPreview = ref<GatewayConfigurationPreview | null>(null)
const configurationPreviewKey = ref('')
const savedConfiguration = ref<{ key: string; revision: number } | null>(null)
const installation = ref<GatewayInstallation | null>(null)
const networkStatus = ref<LinuxGatewayStatus | null>(null)
const networkRemoved = ref(false)
const networkPreview = ref<LinuxGatewayPreview | null>(null)
const networkPreviewKey = ref('')
const loading = ref(false)
const action = ref('')
const refreshPending = ref(false)
const error = ref('')
const pollError = ref('')
const resumed = ref(false)
const baseline = ref('')
let generation = 0
let disposed = false
let poll: ReturnType<typeof setTimeout> | undefined
const busy = computed(() => loading.value || !!action.value)
// The server decides the takeover, so an Agent-page wizard follows it too.
const takeover = computed(() => config.value?.takeover || installation.value?.takeover || 'selective')
const fullTun = computed(() => takeover.value === 'full_tun')
const legacy = computed(() => takeover.value === 'legacy')
const steps = computed(() => ['设备与网络', '分流规则', '安装核心', '发布并检查', fullTun.value ? '转发与 TUN' : '启用与验证'])
const tunPrepareNote = ref('')
// Same curl / wget choice as the Agent page; native Alpine ships only BusyBox wget.
const installerDownloader = ref<InstallerDownloader>('curl')
const installCommand = computed(() => installation.value?.expired ? '' : installation.value?.download_url
  ? buildInstallCommand(installation.value.download_url, installerDownloader.value) : installation.value?.command || '')
const locked = computed(() => !!installation.value)
const availableStep = computed(() => !installation.value ? step.value : installation.value.phase === 'published' ? 4 : installation.value.phase === 'waiting' ? 2 : 3)
const commandExpiry = computed(() => {
  const value = installation.value?.expires_at
  const date = value ? new Date(value) : null
  return date && !Number.isNaN(date.getTime())
    ? `命令有效期至 ${date.toLocaleString()}（本地时间）。`
    : '命令有效期暂时无法确认，请刷新安装进度。'
})
const settingsKey = computed(() => onboardingSettingsKey(settings))
const allDraftKey = computed(() => JSON.stringify([name.value, serverUrl.value, gatewaySpecKey(network), settingsKey.value]))
const dirty = computed(() => !locked.value && !!baseline.value && allDraftKey.value !== baseline.value)
const automaticNetwork = computed(() => ![network.interface, network.lan_cidr, network.upstream_gateway].some(value => value.trim()))
const environmentErrors = computed(() => {
  if (!automaticNetwork.value) return gatewayEnvironmentErrors(name.value, serverUrl.value, network)
  const errors = [setupServerError(serverUrl.value)].filter(Boolean)
  if (network.address && (!/^(?:\d{1,3}\.){3}\d{1,3}$/.test(network.address) || network.address.split('.').some(part => Number(part) > 255))) errors.push('请输入有效 IPv4 地址，或留空自动获取。')
  if (!/^[A-Za-z0-9_-]{1,48}$/.test(name.value)) errors.push('设备名称需为 1–48 个字母、数字、短横线或下划线。')
  return errors
})
const configReady = computed(() => configurationPreview.value?.ready === true && configurationPreviewKey.value === settingsKey.value)
const localDnsWarning = computed(() => settings.local_dns === config.value?.settings.local_dns
  ? config.value?.field_warnings?.local_dns || '' : '')
const configLists = computed(() => [
  { field: 'rule_set_ids' as const, ...(fullTun.value ? { title: '主路由转发', ...gatewayCopy.routerRoutes } : { title: 'IP 接管', ...gatewayCopy.applications }), rows: config.value?.ip_candidates || [] },
  { field: 'direct_rule_set_ids' as const, title: '直连域名', ...gatewayCopy.directDomains, rows: config.value?.domain_candidates || [] },
  { field: 'proxy_rule_set_ids' as const, title: '代理域名', ...gatewayCopy.proxyDomains, rows: config.value?.domain_candidates || [] },
].map(list => {
  const rows: LinuxGatewayCandidate[] = [...list.rows, ...settings[list.field].filter(id => !list.rows.some(row => row.id === id)).map(id => ({
    id, name: `已移除的规则集（${id}）`, behavior: '', available: false, enabled: false, reason: '此关联已失效，请取消勾选后重新检查。',
  }))]
  return { ...list, rows, visibleRows: rows.filter(row => `${row.name} ${row.policy || ''}`.toLowerCase().includes(search.value.trim().toLowerCase())) }
}))
const hasRuleCandidates = computed(() => configLists.value.some(list => list.rows.length > 0))
const hostAgent = computed(() => installation.value?.agents.mihomo)
const installationBlocked = computed(() => !!(installation.value?.config_changed || installation.value?.target_changed || (installation.value?.expired && installation.value?.phase === 'waiting')))
const networkDirty = computed(() => !!installation.value && gatewaySpecKey(network) !== gatewaySpecKey(installation.value.network))
const canNetwork = computed(() => !installationBlocked.value && !networkDirty.value && installation.value?.phase === 'published' && !!hostAgent.value && installationMatches(installation.value, props.profileId))
const canInspectNetwork = computed(() => installationHostMatches(installation.value, props.profileId))
const hostKey = computed(() => JSON.stringify([installation.value?.id, props.profileId, hostAgent.value?.id, gatewaySpecKey(network)]))
const canApply = computed(() => canNetwork.value && !busy.value && networkPreview.value?.ready === true && networkPreviewKey.value === hostKey.value)
const managedNetwork = computed(() => !!(networkStatus.value?.managed?.active || networkStatus.value?.managed?.pending))
const activeNetwork = computed(() => fullTun.value ? fullTunReady(networkStatus.value)
  : networkStatus.value?.success === true && networkStatus.value.managed?.active === true && networkStatus.value.managed.healthy === true && !networkStatus.value.managed.pending)
// Full TUN only checks the host; the selective gateway's apply/revoke stays hidden.
const fullTunRows = computed(() => {
  const status = networkStatus.value
  const checking = action.value === 'inspect'
  const waiting: { status: string; tone: StatusTone } = { status: checking ? '正在检测' : '等待检测', tone: checking ? 'progress' : 'neutral' }
  return {
    forwarding: status ? { status: forwardingLabel(status), tone: forwardingTone(status) } : waiting,
    forwardingError: status?.ipv4_forwarding?.error || (status?.ipv4_forwarding?.supported === false ? status.ipv4_forwarding.unsupported_reason || '' : ''),
    tun: status ? { status: tunLabel(status), tone: tunTone(status) } : waiting,
    tunError: status?.tun?.error || status?.config_error || '',
  }
})
// A warning only: never part of activeNetwork, fullTunReady or the forwarding action.
const forwardFilter = computed(() => fullTun.value ? forwardFilterWarning(networkStatus.value?.forward_filter) : null)
const canPersistForwarding = computed(() => !!networkStatus.value?.ipv4_forwarding && networkStatus.value.ipv4_forwarding.persisted !== true && networkStatus.value.ipv4_forwarding.supported !== false)
const visibleNetworkWarnings = computed(() => (networkPreview.value?.warnings || []).filter(value => (props.goal !== 'router' && !legacy.value) || !value.includes('切换一台客户端的网关')))
const LEGACY_REMOVED = '网关转发已撤销，主路由转来的连接已中断。请删除主路由上指向这台 Linux 的静态路由，或重新应用网关转发。'
const token = () => ({ generation, profileId: props.profileId })
const current = (value: ReturnType<typeof token>) => !disposed && props.open && value.generation === generation && value.profileId === props.profileId
const storageKey = () => `configflow.gateway-installation.${props.profileId}`
const settingsBody = (): GatewayOnboardingSettings => ({ ...settings, rule_set_ids: [...settings.rule_set_ids], direct_rule_set_ids: [...settings.direct_rule_set_ids], proxy_rule_set_ids: [...settings.proxy_rule_set_ids], expected_config_revision: config.value?.config_revision })

function hostError(failure: any) {
  const code = failure?.response?.data?.code
  return code ? routerSplitErrorMessage(code, failure.response.data.message) : gatewayErrorMessage(failure, true)
}
// 发布与安装检查都拒绝离线或已禁用的设备，这里按同样的判断显示。
function agentRow(agent?: GatewayInstallation['agents']['mihomo']): { status: string; tone: StatusTone } {
  if (!agent) return { status: '等待登记', tone: 'neutral' }
  if (agent.enabled === false) return { status: '已禁用', tone: 'warning' }
  if (agent.status === 'online') return { status: '在线', tone: 'success' }
  if (agent.status === 'offline') return { status: '离线', tone: 'warning' }
  return { status: agent.status || '已登记', tone: 'neutral' }
}
const agentWarnings = computed(() => (['mihomo', 'oxidns'] as const).flatMap(service => {
  const row = agentRow(installation.value?.agents[service])
  if (row.tone === 'success') return []
  const label = `${service === 'mihomo' ? 'Mihomo' : 'OxiDNS'} Agent`
  if (row.status === '已禁用') return [`${label} 已在管理中停用，启用后才能继续。`]
  if (row.status === '离线') return [`${label} 离线，请恢复连接后重新检查。`]
  return [`${label} 状态尚未确认，请重新检查后继续。`]
}))
function stopPolling() { if (poll) clearTimeout(poll); poll = undefined }
function continueToRules() {
  if (busy.value || !config.value || locked.value) return
  environmentAttempted.value = true
  if (!environmentErrors.value.length) step.value = 1
  else {
    const captured = token()
    void nextTick(() => { if (current(captured) && step.value === 0) environmentErrorPanel.value?.scrollIntoView?.({ block: 'nearest' }) })
  }
}
function selectStep(value: string | number) {
  const index = Number(value)
  if (busy.value || !Number.isInteger(index) || index < 0 || index > availableStep.value || (locked.value && index < 2)) return
  step.value = index
}
function schedulePoll() {
  stopPolling()
  if (!props.open || !installation.value || installation.value.phase === 'published' || installation.value.phase === 'publish_failed' || installation.value.phase === 'enrolled') return
  poll = setTimeout(() => { void refreshInstallation() }, 4000)
}
function acceptInstallation(value: GatewayInstallation, restore = false) {
  if (!value?.id || value.profile_id !== props.profileId || !value.network || !value.agents || !value.deployments || (!installationMatches(value, props.profileId) && !value.target_changed) || (installation.value && installation.value.id !== value.id)) throw new Error('安装记录与当前配置空间或目标设备不一致，已停止操作。')
  const phaseChanged = installation.value?.phase !== value.phase
  installation.value = value
  environmentAttempted.value = false
  Object.assign(network, value.network)
  name.value = value.name
  if (value.server_url) serverUrl.value = value.server_url
  try { localStorage.setItem(storageKey(), value.id) } catch { /* The server record remains resumable. */ }
  if (restore) resumed.value = true
  if (restore || phaseChanged) {
    step.value = availableStep.value
    if (value.phase === 'published') void nextTick(inspectNetwork)
  }
  emit('agents-changed')
  schedulePoll()
}
async function detectNetwork(automatic = true) {
  if (busy.value || !installation.value || installationBlocked.value || !canInspectNetwork.value) return
  if (!automatic && gatewaySpecErrors(network).length) { error.value = gatewaySpecErrors(network).join('；'); return }
  const captured = token(), id = installation.value.id
  action.value = 'detect'; error.value = ''; networkPreview.value = null
  try {
    if (automatic) {
      const { data } = await linuxGatewayApi.status(installation.value.agents.mihomo!.id, captured.profileId)
      if (!current(captured) || installation.value?.id !== id) return
      networkStatus.value = data
      if (!data.success) throw new Error(data.message || '暂时无法读取主机网络。')
    }
    const { data } = await gatewayOnboardingApi.network(id, captured.profileId, automatic ? { auto_network: true } : { network: cleanGatewaySpec(network) })
    if (!current(captured) || installation.value?.id !== id) return
    acceptInstallation(data)
    notify.success(automatic ? '网络检测完成。' : '网络参数已按主机实际状态核对并保存。')
  } catch (failure) { if (current(captured)) error.value = gatewayErrorMessage(failure, true) }
  finally { if (current(captured)) action.value = '' }
}
async function generatePreparedInstallation() {
  if (busy.value || locked.value || !config.value) return
  environmentAttempted.value = true
  if (environmentErrors.value.length) return
  const captured = token()
  await previewConfiguration()
  if (current(captured) && configReady.value) await createInstallation()
  else if (current(captured) && configurationPreview.value?.blockers?.length) error.value = configurationPreview.value.blockers.join('；')
}
async function load() {
  const captured = token()
  loading.value = true
  error.value = ''
  const [configResponse, sessionsResponse] = await Promise.allSettled([
    gatewayOnboardingApi.get(captured.profileId), gatewayOnboardingApi.sessions(captured.profileId),
  ])
  if (!current(captured)) return
  if (configResponse.status === 'fulfilled') {
    config.value = configResponse.value.data
    Object.assign(settings, config.value.settings)
    baseline.value = allDraftKey.value
  } else error.value = gatewayErrorMessage(configResponse.reason)
  if (sessionsResponse.status === 'fulfilled') {
    try {
      const result = sessionsResponse.value.data
      if (result.success === false || !Object.prototype.hasOwnProperty.call(result, 'session')) throw new Error('安装记录响应无效。')
      if (result.session) acceptInstallation(result.session, true)
    } catch (failure) { error.value = gatewayErrorMessage(failure) }
  } else pollError.value = `暂时无法读取先前的安装记录：${gatewayErrorMessage(sessionsResponse.reason)}`
  loading.value = false
}
function toggle(field: 'rule_set_ids' | 'direct_rule_set_ids' | 'proxy_rule_set_ids', id: string, checked: boolean | 'indeterminate') {
  if (locked.value) return
  settings[field] = checked === true ? [...new Set([...settings[field], id])] : settings[field].filter(item => item !== id)
  // A domain can have only one DNS destination; move it instead of leaving overlap.
  if (checked === true && field !== 'rule_set_ids') {
    const other = field === 'direct_rule_set_ids' ? 'proxy_rule_set_ids' : 'direct_rule_set_ids'
    settings[other] = settings[other].filter(item => item !== id)
  }
}
async function previewConfiguration() {
  if (busy.value || locked.value || !config.value) return
  const captured = token()
  const key = settingsKey.value
  action.value = 'preview-config'; error.value = ''; configurationPreview.value = null
  try {
    const { data } = await gatewayOnboardingApi.preview(captured.profileId, settingsBody())
    if (!current(captured) || key !== settingsKey.value) return
    configurationPreview.value = data
    configurationPreviewKey.value = key
    if (data.success === false && !data.blockers?.length) throw new Error(data.message || '配置检查失败。')
  } catch (failure) { if (current(captured)) error.value = gatewayErrorMessage(failure) }
  finally { if (current(captured)) action.value = '' }
}
async function createInstallation(useAsServerDomain = false) {
  if (busy.value || locked.value || !configReady.value || environmentErrors.value.length || !config.value) return
  const captured = token()
  action.value = 'create'; error.value = ''; serverUrlMismatch.value = ''
  try {
    const key = settingsKey.value
    if (savedConfiguration.value?.key !== key) {
      const saved = (await gatewayOnboardingApi.save(captured.profileId, settingsBody())).data
      if (!current(captured)) return
      if (!saved.success || !Number.isSafeInteger(saved.config_revision)) throw new Error(saved.message || saved.blockers?.join('；') || '配置没有保存成功。')
      config.value.config_revision = saved.config_revision!
      savedConfiguration.value = { key, revision: saved.config_revision! }
    }
    if (key !== settingsKey.value) throw new Error('配置已保存，但表单已改变，请重新检查后继续。')
    // Retrying a lost create response must reuse the saved revision and server idempotency.
    const { data } = await gatewayOnboardingApi.create({ profile_id: captured.profileId, name: name.value.trim(), server_url: serverUrl.value.trim().replace(/\/$/, ''), ...(automaticNetwork.value ? { auto_network: true, agent_ip: network.address.trim() } : { network: cleanGatewaySpec(network) }), config_revision: savedConfiguration.value!.revision, ...(useAsServerDomain ? { use_as_server_domain: true } : {}) })
    if (!current(captured)) return
    acceptInstallation(data)
    baseline.value = allDraftKey.value
    notify.success(data.phase === 'waiting' ? '配置已保存，请在目标 Linux 执行安装命令。' : '配置已更新，继续沿用已安装的设备，请重新发布。')
  } catch (failure: any) {
    if (!current(captured)) return
    error.value = gatewayErrorMessage(failure)
    if (failure?.response?.data?.code === 'server_url_mismatch') serverUrlMismatch.value = serverUrl.value.trim().replace(/\/$/, '')
  }
  finally { if (current(captured)) action.value = '' }
}
async function refreshInstallation() {
  if (!installation.value || refreshPending.value) return
  const captured = token(), id = installation.value.id
  refreshPending.value = true
  try {
    const { data } = await gatewayOnboardingApi.session(id, captured.profileId)
    if (!current(captured) || installation.value?.id !== id) return
    acceptInstallation(data)
    pollError.value = ''
  } catch (failure) { if (current(captured)) pollError.value = `状态暂时无法确认，未继续操作：${gatewayErrorMessage(failure)}` }
  finally { if (current(captured)) { refreshPending.value = false; schedulePoll() } }
}
async function publish() {
  if (busy.value || networkDirty.value || gatewaySpecErrors(network).length || installationBlocked.value || !installation.value || !['enrolled', 'publishing', 'publish_failed'].includes(installation.value.phase)) return
  const captured = token(), id = installation.value.id
  action.value = 'publish'; error.value = ''
  try {
    const { data } = await gatewayOnboardingApi.publish(id, captured.profileId, installation.value.phase === 'publish_failed')
    if (current(captured) && installation.value?.id === id) acceptInstallation(data)
  } catch (failure) {
    if (current(captured)) {
      const message = stripTrailingPunctuation(gatewayErrorMessage(failure))
      error.value = `发布状态需要确认：${message}。请刷新进度，不要重复安装。`
      void refreshInstallation()
    }
  } finally { if (current(captured)) action.value = '' }
}
async function inspectNetwork(refreshAgents = false) {
  if (!canInspectNetwork.value || !hostAgent.value || action.value || refreshPending.value) return
  const captured = token(), agentId = hostAgent.value.id
  action.value = 'inspect'; error.value = ''
  try {
    // Keep physical host checks independent of the pair's management/heartbeat state.
    if (refreshAgents) await refreshInstallation()
    if (!current(captured) || !canInspectNetwork.value || hostAgent.value?.id !== agentId) return
    const { data } = await linuxGatewayApi.status(agentId, captured.profileId)
    if (!current(captured) || hostAgent.value?.id !== agentId) return
    networkStatus.value = data
    if (!data.success) throw new Error(data.message || '网络检测失败。')
  } catch (failure) { if (current(captured)) error.value = gatewayErrorMessage(failure, true) }
  finally { if (current(captured)) action.value = '' }
}
async function previewNetwork() {
  if (!canNetwork.value || busy.value || !hostAgent.value) return
  const captured = token(), key = hostKey.value, agentId = hostAgent.value.id
  action.value = 'preview-network'; error.value = ''; networkPreview.value = null
  try {
    const { data } = await linuxGatewayApi.preview(agentId, captured.profileId, cleanGatewaySpec(network))
    if (!current(captured) || key !== hostKey.value) return
    networkPreview.value = data; networkPreviewKey.value = key
    if (data.success === false && !data.blockers?.length) throw new Error(data.message || '转发检查失败。')
  } catch (failure) { if (current(captured)) error.value = gatewayErrorMessage(failure, true) }
  finally { if (current(captured)) action.value = '' }
}
async function applyNetwork() {
  if (!canApply.value || !hostAgent.value || !installation.value) return
  const captured = token(), key = hostKey.value, agentId = hostAgent.value.id, id = installation.value.id
  action.value = 'apply'; error.value = ''; networkPreview.value = null
  try {
    // Recheck the exact registered pair after preview; a stale UI may never pick another host.
    const fresh = (await gatewayOnboardingApi.session(id, captured.profileId)).data
    if (!current(captured) || key !== hostKey.value) return
    if (!installationMatches(fresh, captured.profileId) || fresh.id !== id || fresh.phase !== 'published' || fresh.agents.mihomo?.id !== agentId || gatewaySpecKey(fresh.network) !== gatewaySpecKey(network) || fresh.config_changed || fresh.target_changed) throw new Error('服务发布状态或设备绑定已改变，请刷新安装进度。')
    const { data } = await linuxGatewayApi.apply(agentId, captured.profileId, cleanGatewaySpec(network))
    if (!current(captured) || key !== hostKey.value) return
    networkStatus.value = data
    if (!data.success) throw new Error(data.message || '转发应用失败，请检查设备状态。')
    if (!activeNetwork.value) throw new Error(data.managed?.error || '转发尚未确认健康，请重新读取网络状态，暂不切换客户端。')
    networkRemoved.value = false
    notify.success('网关转发已启用。请先用一台客户端完成连接验证。')
  } catch (failure) { if (current(captured)) error.value = gatewayErrorMessage(failure, true) }
  finally { if (current(captured)) action.value = '' }
}
async function removeNetwork() {
  if (!canInspectNetwork.value || !hostAgent.value || busy.value || !installation.value) return
  const captured = token(), agentId = hostAgent.value.id, id = installation.value.id
  // A legacy router split still depends on this takeover; revoking it cuts the router's traffic.
  const teardown = legacy.value
  if (teardown && !await confirmDanger('主路由仍把代理网段转给这台 Linux。撤销后 TUN 接管与 IPv4 转发会还原，这些连接会中断，直到重新应用转发。', { title: `撤销「${installation.value.name}」的网关转发？`, confirmText: '仍然撤销' })) return
  if (!current(captured) || hostAgent.value?.id !== agentId || busy.value) return
  action.value = 'remove'; error.value = ''; networkPreview.value = null
  try {
    const fresh = (await gatewayOnboardingApi.session(id, captured.profileId)).data
    if (!current(captured) || hostAgent.value?.id !== agentId) return
    if (!installationHostMatches(fresh, captured.profileId) || fresh.id !== id || fresh.agents.mihomo?.id !== agentId) throw new Error('原目标设备的身份或绑定已改变，未撤销任何设备的网络设置。')
    const { data } = teardown ? await linuxGatewayApi.remove(agentId, captured.profileId, true) : await linuxGatewayApi.remove(agentId, captured.profileId)
    if (!current(captured) || hostAgent.value?.id !== agentId) return
    networkStatus.value = data
    if (!data.success) throw new Error(data.message || '恢复未完成，请检查设备状态。')
    if (data.managed?.active || data.managed?.pending) throw new Error(data.managed.error || '撤销结果尚未确认，请重新读取网络状态。')
    networkRemoved.value = true
    // A legacy router split's clients never changed their gateway; the server says what to do instead.
    notify.success(teardown ? data.message || LEGACY_REMOVED : '本向导管理的转发已撤销，请恢复客户端原来的网关与 DNS 设置。')
  } catch (failure) { if (current(captured)) error.value = hostError(failure) }
  finally { if (current(captured)) action.value = '' }
}
async function persistForwarding() {
  if (!canInspectNetwork.value || !hostAgent.value || busy.value || !installation.value) return
  const captured = token(), agentId = hostAgent.value.id
  const approved = await confirm(`将在「${installation.value.name}」写入 /etc/sysctl.d/99-configflow-aio-forward.conf 和 Agent 开机检查，并立即开启 IPv4 转发。`, { title: '开启 IPv4 转发？', confirmText: '开启并持久化' })
  if (!approved || !current(captured) || hostAgent.value?.id !== agentId || busy.value) return
  action.value = 'forwarding'; error.value = ''; tunPrepareNote.value = ''
  let done = false
  try {
    const { data } = await linuxGatewayApi.persistForwarding(agentId, captured.profileId)
    if (!current(captured) || hostAgent.value?.id !== agentId) return
    if (!data.success) throw new Error(data.message || 'IPv4 转发未能持久化，请重新检查设备状态。')
    if (data.tun_prepare && data.tun_prepare.level !== 'pass') tunPrepareNote.value = data.tun_prepare.message
    notify.success('IPv4 转发已开启并持久化。')
    done = true
  } catch (failure) { if (current(captured)) error.value = hostError(failure) }
  finally { if (current(captured)) action.value = '' }
  // Show the host's real state instead of assuming the write took effect.
  if (done && current(captured)) await inspectNetwork()
}
async function startAnotherInstallation() {
  if (busy.value) return
  stopPolling(); installation.value = null; networkStatus.value = null; networkPreview.value = null; networkRemoved.value = false; tunPrepareNote.value = ''
  configurationPreview.value = null; savedConfiguration.value = null; resumed.value = false; step.value = 0; error.value = ''; pollError.value = ''
  environmentAttempted.value = false
  Object.assign(network, emptyGatewaySpec(), { address: network.address })
  try { localStorage.removeItem(storageKey()) } catch { /* The original installation remains on the server. */ }
  const captured = token()
  loading.value = true
  try {
    const { data } = await gatewayOnboardingApi.get(captured.profileId)
    if (!current(captured)) return
    config.value = data; Object.assign(settings, data.settings); baseline.value = allDraftKey.value
  } catch (failure) { if (current(captured)) { config.value = null; error.value = gatewayErrorMessage(failure) } }
  finally { if (current(captured)) loading.value = false }
}
async function copyCommand() {
  if (!installCommand.value) return
  const captured = token()
  try { await copyText(installCommand.value); if (current(captured)) notify.success('安装命令已复制。') }
  catch { if (current(captured)) notify.error('复制失败，请选中安装命令手动复制。') }
}
watch(step, () => { void nextTick(() => {
  contentPane.value?.scrollTo?.({ top: 0 })
  contentPane.value?.querySelector<HTMLElement>('[data-gateway-steps] [aria-current="step"]')?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
}) })
watch(() => networkStatus.value?.managed, managed => { if (managed?.active || managed?.pending) networkRemoved.value = false })
watch(settingsKey, () => { configurationPreview.value = null; savedConfiguration.value = null })
let autoDetectionAttempt = ''
watch(() => [installation.value?.id, installation.value?.phase, busy.value] as const, () => {
  if (busy.value || !installation.value?.auto_network || installation.value.phase !== 'enrolled' || installation.value.network.interface || autoDetectionAttempt === installation.value.id) return
  autoDetectionAttempt = installation.value.id
  void detectNetwork()
})
watch(busy, value => emit('busy', value), { immediate: true })
watch(() => [props.open, props.profileId] as const, () => {
  autoDetectionAttempt = ''
  generation++; stopPolling(); installation.value = null; config.value = null; configurationPreview.value = null; savedConfiguration.value = null
  networkStatus.value = null; networkPreview.value = null; networkRemoved.value = false; error.value = ''; pollError.value = ''; resumed.value = false; search.value = ''; ruleCategory.value = 'rule_set_ids'; tunPrepareNote.value = ''
  loading.value = false; action.value = ''; refreshPending.value = false; baseline.value = ''; step.value = 0
  environmentAttempted.value = false
  Object.assign(network, emptyGatewaySpec()); name.value = 'home-gateway'
  Object.assign(settings, { rule_set_ids: [], direct_rule_set_ids: [], proxy_rule_set_ids: [], local_dns: 'https://223.5.5.5/dns-query', default_forward: 'forward_remote', query_recorder: false, confirm_template_conversion: false })
  serverUrl.value = window.location.origin
  if (props.open) void load()
}, { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { disposed = true; generation++; stopPolling(); emit('busy', false) })
</script>

<template>
  <SettingsWorkspace :open="open" :inline="embedded" :title="fullTun ? '安装主路由分流' : '安装 Linux 网关'" :description="fullTun ? '安装 Mihomo、OxiDNS 与 Agent，开启转发并检查 TUN。' : '安装 Mihomo、OxiDNS 与 Agent，检查后启用转发。'" back-label="返回 Agent 列表" :dirty="dirty" :busy="busy" size="form" @update:open="emit('update:open', $event)">
    <div ref="contentPane" class="min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain" data-gateway-install-wizard>
      <Tabs :model-value="String(step)" activation-mode="manual" class="mx-auto w-full min-w-0 max-w-3xl gap-5" :class="embedded ? '' : 'p-3 sm:p-5'" @update:model-value="selectStep">
        <div v-if="!embedded" class="min-w-0 space-y-2">
          <div class="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <p class="m-0 min-w-0 break-words">配置空间 · <span class="font-medium text-foreground">{{ profileName }}</span></p>
            <span>IPv4 · 单网卡</span>
          </div>
          <TabsList variant="strip" class="h-auto w-full justify-start gap-1 overflow-x-auto rounded-none border-b border-border bg-card p-1" aria-label="安装步骤" data-gateway-steps>
            <TabsTrigger v-for="(label, index) in steps" :key="label" :value="String(index)" class="h-11 min-h-11 flex-none px-3 text-xs" :aria-label="`${index + 1} ${label}`" :aria-current="index === step ? 'step' : undefined" :disabled="busy || index > availableStep || (locked && index < 2)">
              <span class="num">{{ index + 1 }}</span>{{ label }}
            </TabsTrigger>
          </TabsList>
        </div>
        <p v-if="loading" role="status" class="m-0 flex items-center gap-2 text-sm text-muted-foreground"><LoaderCircle class="size-4 animate-spin" />正在读取配置与安装进度…</p>
        <div v-if="error" class="rounded-lg border border-destructive/30 bg-destructive-soft p-3 text-sm text-destructive-accent" role="alert">
          {{ error }}<Button v-if="!config && !installation" variant="ghost" class="min-h-11" :disabled="busy" @click="load">重新读取</Button>
          <div v-if="serverUrlMismatch && !installation" class="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <Button variant="outline" size="sm" class="min-h-11" :disabled="busy || serverUrl.trim().replace(/\/$/, '') !== serverUrlMismatch" @click="createInstallation(true)">将 {{ serverUrlMismatch }} 设为系统服务地址并继续</Button>
            <span>所有配置生成的规则、订阅和安装链接都会改用此地址。</span>
          </div>
        </div>
        <p v-if="pollError" class="m-0 break-words text-sm text-warning-accent" role="alert">{{ pollError }}</p>
        <div v-if="embedded && (configurationPreview?.warnings || config?.warnings)?.length" class="space-y-1 text-xs text-warning-accent" role="status"><p v-for="warning in (configurationPreview?.warnings || config?.warnings)" :key="warning" class="m-0">{{ warning }}</p></div>
        <div v-if="installationBlocked" role="alert" class="space-y-1 border-l-2 border-destructive-accent pl-3 text-xs leading-relaxed">
          <p class="m-0 font-medium text-destructive-accent">安装已暂停</p>
          <p class="m-0">{{ installation?.target_changed ? '设备绑定或身份已改变。已停止发布与转发操作，请重新准备安装。' : installation?.config_changed ? '配置空间在安装后有修改。已停止发布与转发操作；请重新准备安装并预览保存，已安装的设备会继续沿用，无需重新运行安装命令。' : '安装凭据已过期。已停止发布与转发操作，请重新准备安装。' }}</p>
        </div>
        <div v-if="installation" class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 border-b border-border pb-2">
          <p class="m-0 mr-auto min-w-0 break-words text-xs text-muted-foreground">{{ resumed ? '已恢复' : '安装目标' }} · <strong class="font-medium text-foreground">{{ embedded ? (goal === 'router' ? '主路由分流' : 'Linux 网关') : installation.name }}</strong> <span class="font-mono">{{ network.address }}</span></p>
          <div class="flex shrink-0 items-center">
            <Button variant="ghost" size="sm" class="px-2 text-xs" :disabled="busy" @click="startAnotherInstallation">重新准备安装</Button>
            <FieldHelp label="重新准备安装" text="返回设备与网络重新准备；不会撤销已安装的服务或已提交的发布。" />
          </div>
        </div>

        <section v-if="installation && ['enrolled', 'publish_failed'].includes(installation.phase)" class="flex flex-col gap-3 border-b border-border pb-4" aria-label="网络检测">
          <div class="flex flex-wrap items-center justify-between gap-2"><h3 class="m-0 text-sm font-semibold">网络检测</h3><Button variant="outline" size="sm" :disabled="busy || installationBlocked" @click="detectNetwork()">{{ action === 'detect' ? '正在检测…' : '重新自动检测' }}</Button></div>
          <p v-if="!installation.network.interface" class="m-0 text-xs text-muted-foreground">设备已登记，正在确认实际网卡、网段和上游网关；检测失败可在高级选项修正。</p>
          <dl v-else class="m-0 grid grid-cols-2 gap-3 text-xs"><div><dt class="text-muted-foreground">Linux IPv4</dt><dd class="m-0 font-mono">{{ installation.network.address }}</dd></div><div><dt class="text-muted-foreground">主路由</dt><dd class="m-0 font-mono">{{ installation.network.upstream_gateway }}</dd></div><div><dt class="text-muted-foreground">LAN 网卡</dt><dd class="m-0 font-mono">{{ installation.network.interface }}</dd></div><div><dt class="text-muted-foreground">LAN 网段</dt><dd class="m-0 font-mono">{{ installation.network.lan_cidr }}</dd></div></dl>
          <details><summary class="min-h-11 cursor-pointer content-center text-sm font-medium">高级选项：手动修改网络</summary><GatewayNetworkFields :model-value="network" :disabled="busy" @update:model-value="Object.assign(network, $event)" /><Button class="mt-3 min-h-11" :disabled="busy || installationBlocked || !!gatewaySpecErrors(network).length" @click="detectNetwork(false)">核对并保存网络参数</Button></details>
          <GatewayDetectedNetwork v-if="networkStatus?.detection" :detection="networkStatus.detection" />
          <p v-if="networkDirty" class="m-0 text-xs text-warning-accent">网络参数有修改，请核对并保存后再发布。</p>
        </section>
        <TabsContent :value="String(step)" as-child>
        <section v-if="step === 0" :key="`${profileId}:${open}`" class="space-y-4" aria-label="设备与网络">
          <div>
            <h2 v-if="!embedded" class="m-0 text-lg font-semibold">设备与网络</h2>
            <p class="mt-1 mb-0 text-sm text-muted-foreground">{{ embedded ? (goal === 'router' ? `沿用已保存的规则与 DNS。安装登记后自动检测网络，${fullTun ? '转发与 TUN 检查通过' : '启用成功'}后再接入主路由。` : '沿用已保存的规则与 DNS。安装登记后自动检测网络，启用成功后再切换客户端。') : '安装登记后自动检测网络。Linux 需使用固定 IP，并与主路由在同一网段。' }}</p>
          </div>
          <div class="grid min-w-0 gap-3 sm:grid-cols-2">
            <FormField v-if="!embedded" label="设备名称" html-for="onboarding-name" help="用于命名这台 Linux 上的 Mihomo 和 OxiDNS Agent，不会修改 Linux 的主机名。"><Input id="onboarding-name" v-model="name" class="min-h-11" placeholder="home-gateway" :disabled="busy" /></FormField>
            <FormField label="主机 IP（可选）" :html-for="embedded ? 'setup-agent-ip' : 'onboarding-address'" help="留空由 Agent 自动识别 LAN IPv4。安装登记后自动读取网卡、网段和默认网关；需要手动指定时再展开高级选项。"><Input :id="embedded ? 'setup-agent-ip' : 'onboarding-address'" v-model="network.address" :disabled="busy" class="min-h-11 font-mono" placeholder="留空则自动获取" /></FormField>
            <FormField label="ConfigFlow 服务地址" html-for="onboarding-server" help="默认取浏览器当前访问的协议、主机和端口。目标 Linux 必须能访问此地址；从本机回环地址访问时，请改为局域网 IP 或域名。核心持续下载规则使用系统设置中的服务地址，两者需要一致。"><Input id="onboarding-server" v-model="serverUrl" class="min-h-11 font-mono text-xs" placeholder="http://192.168.1.10:8000" :disabled="locked" /></FormField>
          </div>
          <details class="border-b border-border text-sm">
            <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">高级选项：手动指定网络</summary>
            <GatewayNetworkFields :model-value="network" :disabled="busy" hide-address class="pt-1 pb-4" @update:model-value="Object.assign(network, $event)" />
          </details>
          <details class="border-b border-border text-sm">
            <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">安装要求</summary>
            <div class="space-y-3 pt-1 pb-4 text-xs leading-relaxed text-muted-foreground">
              <p class="m-0">支持 x86_64 / ARM64，systemd / OpenRC（含 Alpine）。需要 root 或 sudo、<code>/dev/net/tun</code> 和网络管理权限。LXC 容器可用非特权容器，开启 nesting 并映射 <code>/dev/net/tun</code>（Proxmox：<code class="break-all">pct set &lt;ID&gt; --dev0 /dev/net/tun</code>）；Docker 等无 systemd / OpenRC 的容器不支持。</p>
              <p class="m-0">在主路由保留 Linux 的 IP，或为 Linux 设置静态 IP。向导不会修改设备 IP 或默认路由。{{ goal === 'router' ? '本方案需要在主路由添加静态路由与 DNS，客户端网关保持主路由。' : '网关方案通过修改客户端网关接入，主路由无需添加静态路由。' }}</p>
              <div>
                <p class="mt-0 mb-1 font-medium text-foreground">缺少依赖时</p>
                <p class="m-0">安装命令会用系统包管理器（apk、apt-get、dnf、yum、pacman、zypper）自动补装缺少的工具，如 nftables、iproute2 和时区数据，已有的跳过，补装后再次检查。补装失败时终端会给出可直接复制的手动安装命令。</p>
              </div>
              <div>
                <p class="mt-0 mb-1 font-medium text-foreground">已有 Mihomo / OxiDNS</p>
                <p class="m-0">标准安装可直接接入：核心位于 <code class="break-all">/usr/local/bin/</code>，配置位于 <code class="break-all">/etc/mihomo/config.yaml</code> 和 <code class="break-all">/etc/oxidns/config.yaml</code>，并有同名系统服务。安装时先校验并保留现有核心与配置，第 4 步再发布新配置。</p>
                <p class="mt-2 mb-0">端口冲突或安装不完整时会停止。非标准目录请用普通模式填写现有路径，接入 Agent。</p>
              </div>
            </div>
          </details>
          <div v-if="environmentAttempted && environmentErrors.length" ref="environmentErrorPanel" class="text-xs leading-relaxed text-destructive-accent" role="alert">
            <p class="m-0 font-medium">请补全或修正以下信息</p><ul class="mt-1 mb-0 list-disc space-y-1 pl-5"><li v-for="item in environmentErrors" :key="item">{{ item }}</li></ul>
          </div>
        </section>

        <section v-else-if="step === 1" class="space-y-4" aria-label="分流规则">
          <div><h2 class="m-0 text-lg font-semibold">分流规则</h2><p class="mt-1 mb-0 text-xs leading-relaxed text-muted-foreground">保存后将更新「{{ profileName }}」内共用的 Mihomo 与 OxiDNS 配置。</p></div>
          <Tabs v-model="ruleCategory" class="min-w-0 gap-3" data-gateway-rule-picker>
            <div class="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
              <TabsList class="h-11 w-full shrink-0 sm:w-auto" aria-label="规则类别">
                <TabsTrigger v-for="list in configLists" :key="list.field" :value="list.field" :aria-label="`${list.title}，已选 ${settings[list.field].length} 个规则集`" class="min-h-11 px-2 text-xs sm:px-3">
                  {{ list.title }}<span class="num text-xs">{{ settings[list.field].length }}</span>
                </TabsTrigger>
              </TabsList>
              <Input id="onboarding-search" v-model="search" aria-label="搜索规则集" class="min-h-11 w-full sm:max-w-64" placeholder="搜索名称或策略" />
            </div>
            <TabsContent v-for="list in configLists" :key="list.field" :value="list.field" class="mt-0 min-w-0 space-y-2">
              <template v-if="ruleCategory === list.field">
                <div class="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-1">
                  <div class="flex min-h-8 items-center gap-1 text-xs"><span class="text-muted-foreground">已选 <span class="num">{{ settings[list.field].length }}</span> 个规则集<template v-if="list.field === 'rule_set_ids'"> · 可选</template></span><span class="cf-compact-help"><FieldHelp :label="list.title" :text="list.help" /></span></div>
                </div>
                <p class="m-0 whitespace-pre-line text-xs leading-relaxed text-muted-foreground">{{ list.hint }}</p>
                <div class="max-h-56 overflow-y-auto rounded-md border border-border">
                  <label v-for="item in list.visibleRows" :key="item.id" class="flex min-h-11 cursor-pointer items-start gap-3 border-b border-border px-3 py-2.5 last:border-b-0 hover:bg-muted" :class="{ 'opacity-70': !item.available && !settings[list.field].includes(item.id) }">
                    <Checkbox :model-value="settings[list.field].includes(item.id)" :disabled="busy || (!item.available && !settings[list.field].includes(item.id))" class="mt-0.5 shrink-0" @update:model-value="toggle(list.field, item.id, $event)" />
                    <span class="min-w-0 flex-1 space-y-0.5"><span class="block break-words text-sm font-medium [overflow-wrap:anywhere]">{{ item.name }}</span><span v-if="item.reason || item.policy" class="block break-words text-xs leading-relaxed" :class="item.reason ? 'text-warning-accent' : 'text-muted-foreground'">{{ item.reason || `策略：${item.policy}` }}</span><span v-if="item.behavior === 'classical' && item.description" class="block break-words text-xs leading-relaxed text-muted-foreground">{{ item.description }}</span></span>
                  </label>
                  <p v-if="!hasRuleCandidates" class="m-0 p-3 text-xs leading-relaxed text-muted-foreground">先到「规则库」添加，再在此空间的「策略规则」引用并启用，然后重新打开向导。</p>
                  <p v-else-if="!list.rows.length" class="m-0 p-3 text-xs text-muted-foreground">{{ list.field === 'rule_set_ids' ? '暂无可选的 IP 规则集，可留空继续。' : '暂无可选的域名规则集。' }}</p>
                  <p v-else-if="!list.visibleRows.length" class="m-0 p-3 text-xs text-muted-foreground" role="status">没有匹配的规则集。已选规则会保留。</p>
                </div>
              </template>
            </TabsContent>
          </Tabs>
          <div class="border-t border-border pt-3">
            <FormField label="直连网站使用的 DNS" html-for="onboarding-dns" :hint="gatewayCopy.localDNS.hint" :help="gatewayCopy.localDNS.help" persistent-hint>
              <Textarea id="onboarding-dns" v-model="settings.local_dns" :disabled="busy" :aria-describedby="localDnsWarning ? 'onboarding-dns-warning' : undefined" class="min-h-16 font-mono text-xs" :rows="2" placeholder="https://223.5.5.5/dns-query" />
              <p v-if="localDnsWarning" id="onboarding-dns-warning" class="m-0 text-xs leading-relaxed text-warning-accent" role="status">{{ localDnsWarning }}</p>
            </FormField>
          </div>
          <FormField label="OxiDNS 查询记录" html-for="onboarding-query-recorder" :hint="queryRecorderCopy.hint" :help="queryRecorderCopy.help" persistent-hint>
            <div class="flex min-h-11 items-center gap-3">
              <Switch id="onboarding-query-recorder" v-model="settings.query_recorder" :disabled="busy" />
              <Label for="onboarding-query-recorder" class="text-sm">{{ settings.query_recorder ? '开启，记录每条 DNS 查询' : '关闭（默认）' }}</Label>
            </div>
          </FormField>
          <div v-if="config?.oxidns_mode === 'native'" class="space-y-1">
            <label class="flex min-h-11 cursor-pointer items-center gap-2 text-sm font-medium"><Checkbox v-model="settings.confirm_template_conversion" :disabled="busy" aria-describedby="onboarding-conversion-help" class="shrink-0" /><span>允许转换为 OxiDNS 模板</span></label>
            <p id="onboarding-conversion-help" class="mt-0 mb-0 ml-6 text-xs leading-relaxed text-muted-foreground">仅在原生配置无法保留时替换解析流程，并备份原配置；未勾选则停止。</p>
          </div>
          <details class="border-b border-border text-xs">
            <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">{{ fullTun ? '分流说明' : '网关说明' }}</summary>
            <ul class="mt-1 mb-3 list-disc space-y-2 pl-5 leading-relaxed text-muted-foreground">
              <template v-if="fullTun">
                <li>主路由的静态路由自动包含 fake-IP 网段 <code class="break-all font-mono">{{ config?.fake_ip_range || '由 Mihomo 配置决定' }}</code>，另加所选规则集的 IPv4 网段，须为 /8 至 /32。</li>
                <li>Mihomo 使用 rule / fake-IP 模式和完整 TUN，接管主路由转来的全部连接；实际出口由规则顺序及策略组当前选择决定。</li>
              </template>
              <template v-else>
                <li>仅支持 Linux + nftables 的 IPv4 网关；客户端需停用 IPv6，或另行配置 IPv6 路由。</li>
                <li>接管网段须为 /8 至 /32，拒绝 /0、/1 等过大范围。</li>
                <li>自动包含 fake-IP 网段 <code class="break-all font-mono">{{ config?.fake_ip_range || '由 Mihomo 配置决定' }}</code>。未选中的 IP 走 Linux 直连转发。</li>
                <li>Mihomo 使用 rule / fake-IP 模式和选择性 TUN；实际出口由规则顺序及策略组当前选择决定。</li>
              </template>
              <li>OxiDNS 在 53 端口服务局域网，代理和后备 DNS 交给 Mihomo；未匹配域名使用已保存的默认 DNS，Mihomo 地址为（<code class="break-all font-mono">127.0.0.1:1053</code>）。</li>
              <li>直连 DNS 使用 IP 地址的 DoH / DoT；普通 53 端口可能被自动接管，形成回环。</li>
            </ul>
          </details>
          <div v-if="config?.warnings?.length || config?.blockers?.length" class="space-y-3 text-xs leading-relaxed">
            <div v-if="config?.blockers?.length" role="alert">
              <p class="m-0 font-medium text-destructive-accent">当前配置需处理</p>
              <ul class="mt-1 mb-0 list-disc space-y-1 pl-5 text-foreground"><li v-for="item in config.blockers" :key="item" class="break-words">{{ item }}</li></ul>
            </div>
            <div v-if="config?.warnings?.length" role="status">
              <p class="m-0 font-medium text-warning-accent">当前配置提醒</p>
              <ul class="mt-1 mb-0 list-disc space-y-1 pl-5 text-foreground"><li v-for="item in config.warnings" :key="item" class="break-words">{{ item }}</li></ul>
            </div>
          </div>
          <Button variant="outline" class="min-h-11" :disabled="busy || !config" @click="previewConfiguration"><LoaderCircle v-if="action === 'preview-config'" class="size-4 animate-spin" />检查配置与代理出口</Button>
          <div v-if="configurationPreview" class="space-y-3 border-t border-border pt-3 text-sm" role="status">
            <p class="m-0 font-semibold" :class="configReady ? 'text-success-accent' : 'text-destructive-accent'">{{ configReady ? '配置检查通过，可以生成安装命令' : '请先处理以下问题' }}</p>
            <div v-if="configurationPreview.blockers?.length" class="text-xs leading-relaxed" role="alert">
              <p class="m-0 font-medium text-destructive-accent">阻断问题</p>
              <ul class="mt-1 mb-0 list-disc space-y-1 pl-5 text-foreground"><li v-for="item in configurationPreview.blockers" :key="item" class="break-words">{{ item }}</li></ul>
            </div>
            <div v-if="configurationPreview.warnings?.length" class="text-xs leading-relaxed">
              <p class="m-0 font-medium text-warning-accent">检查提醒</p>
              <ul class="mt-1 mb-0 list-disc space-y-1 pl-5 text-foreground"><li v-for="item in configurationPreview.warnings" :key="item" class="break-words">{{ item }}</li></ul>
            </div>
            <details v-if="configurationPreview.changes?.length" class="text-xs text-muted-foreground"><summary class="min-h-11 cursor-pointer content-center py-2">查看配置变更</summary><ul class="mt-0 mb-2 list-disc space-y-1 pl-5"><li v-for="item in configurationPreview.changes" :key="item">{{ item }}</li></ul></details>
          </div>
        </section>

        <section v-else-if="step === 2" class="space-y-4" aria-label="安装核心">
          <div><h2 class="m-0 text-lg font-semibold">安装核心与 Agent</h2><p class="mt-1 mb-0 text-sm text-muted-foreground">在目标 Linux 执行命令，等待两项 Agent 登记。</p></div>
          <div class="min-w-0 space-y-1.5">
            <div class="flex min-h-8 items-center justify-between gap-2 max-md:min-h-11">
              <label for="onboarding-command" class="text-xs font-medium">安装命令</label>
              <Button variant="ghost" size="sm" class="px-2 text-xs" aria-label="复制安装命令" :disabled="!installCommand" @click="copyCommand"><Copy class="size-3.5" aria-hidden="true" />复制</Button>
            </div>
            <div v-if="installation?.download_url && !installation.expired" class="flex flex-wrap items-center gap-x-3 gap-y-1">
              <InstallerDownloaderPicker v-model="installerDownloader" aria-label="安装命令下载方式" />
              <p class="m-0 text-xs text-muted-foreground">原生 Alpine 未自带 curl，请选择 wget。</p>
            </div>
            <Textarea id="onboarding-command" :model-value="installCommand" readonly :rows="3" class="min-h-24 max-h-48 font-mono text-xs" />
            <div class="flex flex-wrap gap-x-3 gap-y-1 text-xs leading-relaxed text-muted-foreground"><p class="m-0">使用 root，或按提示输入 sudo 密码。</p><p class="m-0 break-words">{{ commandExpiry }}</p></div>
          </div>
          <p class="m-0 text-xs leading-relaxed">安装命令含临时凭据，请勿公开分享。完成检查前，请勿修改客户端网关或 DNS。</p>
          <div class="divide-y divide-border border-y border-border">
            <AgentStatusRow v-for="service in (['mihomo', 'oxidns'] as const)" :key="service" :label="`${service === 'mihomo' ? 'Mihomo' : 'OxiDNS'} Agent`" v-bind="agentRow(installation?.agents[service])">
              <template v-if="installation?.agents[service]?.name" #default><p class="m-0">设备：{{ installation.agents[service]?.name }}</p></template>
            </AgentStatusRow>
          </div>
          <p v-if="installation?.message" class="m-0 break-words text-xs text-muted-foreground" role="status">{{ installation.message }}</p>
          <Button variant="outline" class="min-h-11" :disabled="refreshPending || busy" @click="refreshInstallation"><RefreshCw class="size-4" :class="{ 'animate-spin': refreshPending }" />检查安装进度</Button>
          <details class="border-b border-border text-xs">
            <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">安装说明与排错</summary>
            <div class="space-y-2 pt-1 pb-3 leading-relaxed text-muted-foreground"><p class="m-0">已有标准安装会先校验并保留，缺少的服务会自动安装；登记完成后进入发布步骤。</p><p class="m-0">失败时按终端提示处理，在同一台 Linux 重跑原命令。凭据过期再重新准备；没有 sudo 时先切换 root。已完成登记可继续发布，无需重装。</p></div>
          </details>
        </section>

        <section v-else-if="step === 3" class="space-y-4" aria-label="发布并检查">
          <div><h2 class="m-0 text-lg font-semibold">发布配置</h2><p class="mt-1 mb-0 text-sm text-muted-foreground">先发布并检查 Mihomo，再发布 OxiDNS；失败时会停止。</p></div>
          <div class="divide-y divide-border border-y border-border">
            <div v-for="service in (['mihomo', 'oxidns'] as const)" :key="service" class="min-w-0">
              <AgentDeploymentStatus v-if="installation?.deployments[service]" :task="installation.deployments[service]!" :label="service === 'mihomo' ? 'Mihomo' : 'OxiDNS'" compact>
                <template #notice><AgentFailureDetails :task="installation.deployments[service]!" :show-progress-message="installation.deployments[service]?.status !== 'succeeded'" :showDNSChecks="false" /></template>
                <template #details><p class="m-0">设备：{{ installation.agents[service]?.name }}</p><p v-if="installation.deployments[service]?.status === 'succeeded' && installation.deployments[service]?.message" class="m-0">{{ installation.deployments[service]?.message }}</p></template>
              </AgentDeploymentStatus>
              <AgentStatusRow v-else :label="service === 'mihomo' ? 'Mihomo' : 'OxiDNS'" :status="service === 'oxidns' ? '等待 Mihomo 检查通过' : '等待发布'" tone="neutral" />
              <AgentDNSChecks :checks="installation?.deployments[service]?.dns_checks" :checked-at="installation?.deployments[service]?.dns_checked_at" compact />
            </div>
          </div>
          <p v-if="installation?.error || installation?.message" class="m-0 break-words whitespace-pre-line text-xs leading-relaxed" :class="installation?.phase === 'publish_failed' ? 'text-destructive-accent' : 'text-muted-foreground'" :role="installation?.phase === 'publish_failed' ? 'alert' : 'status'">{{ installation.error || installation.message }}</p>
          <RuleDownloadRemedy v-if="installation?.phase === 'publish_failed' && installation.remedy?.kind === 'rule_download'" :github="installation.remedy.github" :busy="busy || installationBlocked" @retry="publish" />
          <div class="flex flex-wrap gap-2"><Button class="min-h-11" :disabled="busy || networkDirty || !!gatewaySpecErrors(network).length || installationBlocked || !installation || !['enrolled', 'publishing', 'publish_failed'].includes(installation.phase)" @click="publish"><LoaderCircle v-if="action === 'publish'" class="size-4 animate-spin" />{{ installation?.phase === 'publish_failed' ? '处理后继续发布' : installation?.phase === 'publishing' ? '继续确认发布' : '按顺序发布两项配置' }}</Button><Button variant="outline" class="min-h-11" :disabled="refreshPending || busy" @click="refreshInstallation"><RefreshCw class="size-4" />刷新进度</Button></div>
          <details class="border-b border-border text-xs"><summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">发布说明与排错</summary><p class="mt-1 mb-3 leading-relaxed text-muted-foreground">关闭窗口后发布仍会继续，重新打开可查看进度。失败时在 Agent 查看原因，处理后继续发布，无需重装。</p></details>
        </section>

        <section v-else class="space-y-4" :aria-label="steps[4]">
          <div v-if="agentWarnings.length" class="space-y-1 text-sm text-warning-accent" role="status">
            <p v-for="warning in agentWarnings" :key="warning" class="m-0 break-words">{{ warning }}</p>
          </div>
          <div v-if="legacy && !embedded" class="rounded-lg border border-warning-accent/30 bg-warning-soft px-3 py-2.5 text-xs leading-relaxed" role="status" data-legacy-router-split>
            <p class="m-0 text-sm font-semibold text-warning-accent">旧版主路由分流</p>
            <p class="mt-1 mb-0 text-foreground">这台设备使用 Linux 网关的选择性接管，主路由转来的连接依赖它；撤销转发会让这些连接中断。</p>
          </div>
          <template v-if="fullTun">
          <div><h2 class="m-0 text-lg font-semibold">检查转发与 TUN</h2><p class="mt-1 mb-0 text-sm text-muted-foreground">安装时已开启并持久化 IPv4 转发；发布 Mihomo 后由 TUN 接管主路由转来的连接。两项正常后再接入主路由。</p></div>
          <div class="divide-y divide-border border-y border-border" data-full-tun-checks>
            <AgentStatusRow label="IPv4 转发" :status="fullTunRows.forwarding.status" :tone="fullTunRows.forwarding.tone">
              <template v-if="fullTunRows.forwardingError" #notice><p role="alert" class="m-0 text-destructive-accent">{{ fullTunRows.forwardingError }}</p></template>
            </AgentStatusRow>
            <AgentStatusRow label="Mihomo TUN" :status="fullTunRows.tun.status" :tone="fullTunRows.tun.tone">
              <template v-if="fullTunRows.tunError || tunPrepareNote" #notice><p v-if="fullTunRows.tunError" role="alert" class="m-0 text-destructive-accent">{{ fullTunRows.tunError }}</p><p v-if="tunPrepareNote" class="m-0 text-warning-accent">{{ tunPrepareNote }}</p></template>
            </AgentStatusRow>
            <AgentStatusRow v-if="forwardFilter" label="转发过滤" :status="forwardFilter.status" tone="warning">
              <template #notice><ForwardFilterNotice :filter="networkStatus?.forward_filter" /></template>
            </AgentStatusRow>
            <AgentStatusRow v-if="gatewayLeftover(networkStatus)" label="旧网关记录" :status="gatewayLeftover(networkStatus) === 'present' ? '仍存在' : '无法确认'" tone="warning">
              <template #notice><p class="m-0">请在配置生成 → Mihomo → Linux 网关中清理。</p><p v-if="networkStatus?.managed?.error" role="alert" class="m-0 text-destructive-accent">{{ networkStatus.managed.error }}</p></template>
            </AgentStatusRow>
            <dl class="m-0 grid grid-cols-2 gap-3 py-3 text-xs sm:grid-cols-4"><div class="min-w-0"><dt class="text-muted-foreground">Linux IPv4</dt><dd class="mx-0 mt-1 break-all font-mono">{{ network.address }}</dd></div><div class="min-w-0"><dt class="text-muted-foreground">主路由</dt><dd class="mx-0 mt-1 break-all font-mono">{{ network.upstream_gateway }}</dd></div><div class="min-w-0"><dt class="text-muted-foreground">LAN 网卡</dt><dd class="mx-0 mt-1 break-all font-mono">{{ network.interface }}</dd></div><div class="min-w-0"><dt class="text-muted-foreground">LAN 网段</dt><dd class="mx-0 mt-1 break-all font-mono">{{ network.lan_cidr }}</dd></div></dl>
          </div>
          <div class="flex flex-wrap gap-2">
            <Button variant="outline" class="min-h-11" :disabled="busy || refreshPending || !canInspectNetwork" @click="inspectNetwork(true)"><LoaderCircle v-if="action === 'inspect'" class="size-4 animate-spin" /><RefreshCw v-else class="size-4" />重新检查</Button>
            <Button v-if="canPersistForwarding" class="min-h-11" :disabled="busy || !canInspectNetwork" @click="persistForwarding"><LoaderCircle v-if="action === 'forwarding'" class="size-4 animate-spin" />开启并持久化 IPv4 转发</Button>
          </div>
          <p v-if="activeNetwork" class="m-0 text-xs leading-relaxed">转发与 TUN 正常。接入主路由后，先用一台客户端测试国内网站和代理网站。</p>
          <details class="border-b border-border text-xs"><summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">后续管理</summary><p class="mt-1 mb-3 leading-relaxed text-muted-foreground">主路由转发的网段在「上游路由」管理；Linux 网关的选择性接管不适用于此方案。</p></details>
          </template>
          <template v-else>
          <div><h2 class="m-0 text-lg font-semibold">{{ networkRemoved ? '网关转发已撤销' : activeNetwork ? '验证客户端' : goal === 'router' ? '启用 Linux 转发' : '启用网关转发' }}</h2><p class="mt-1 mb-0 text-sm text-muted-foreground">{{ networkRemoved ? (legacy ? '主路由转来的连接已中断。' : '请恢复客户端接入前的设置。') : activeNetwork ? '先测试一台设备，再接入其他客户端。' : '服务检查已通过。先检查实际网络，再应用转发。' }}</p></div>
          <div class="divide-y divide-border border-y border-border">
            <AgentStatusRow :label="goal === 'router' ? 'Linux 转发' : '网关转发'" :status="networkRemoved ? '已撤销' : activeNetwork ? '已启用' : managedNetwork ? '待确认' : '未启用'" :tone="networkRemoved || activeNetwork ? 'success' : managedNetwork ? 'warning' : 'neutral'">
              <template v-if="networkStatus?.unsupported_reason || networkStatus?.config_error || networkStatus?.detection_error || networkStatus?.managed?.error" #notice><p role="alert" class="m-0 text-destructive-accent">{{ networkStatus.unsupported_reason || networkStatus.config_error || networkStatus.detection_error || networkStatus.managed?.error }}</p></template>
            </AgentStatusRow>
            <dl class="m-0 grid grid-cols-2 gap-3 py-3 text-xs sm:grid-cols-4"><div class="min-w-0"><dt class="text-muted-foreground">Linux IPv4</dt><dd class="mx-0 mt-1 break-all font-mono">{{ network.address }}</dd></div><div class="min-w-0"><dt class="text-muted-foreground">主路由</dt><dd class="mx-0 mt-1 break-all font-mono">{{ network.upstream_gateway }}</dd></div><div class="min-w-0"><dt class="text-muted-foreground">LAN 网卡</dt><dd class="mx-0 mt-1 break-all font-mono">{{ network.interface }}</dd></div><div class="min-w-0"><dt class="text-muted-foreground">LAN 网段</dt><dd class="mx-0 mt-1 break-all font-mono">{{ network.lan_cidr }}</dd></div></dl>
          </div>
          <GatewayDetectedNetwork v-if="networkStatus?.detection" :detection="networkStatus.detection" />
          <details class="border-b border-border text-sm"><summary class="min-h-11 cursor-pointer content-center font-medium">高级选项：手动修改网络</summary><p class="m-0 pb-3 text-xs text-muted-foreground">只选择主机实际已有的网络参数；不会修改 Linux 的 IP 或默认路由。转发启用期间需先撤销再修改。</p><GatewayNetworkFields :model-value="network" :disabled="busy || managedNetwork" @update:model-value="Object.assign(network, $event)" /><div class="flex flex-wrap gap-2 py-3"><Button variant="outline" :disabled="busy || managedNetwork || installationBlocked" @click="detectNetwork()">重新自动检测</Button><Button :disabled="busy || managedNetwork || installationBlocked || !!gatewaySpecErrors(network).length" @click="detectNetwork(false)">核对并保存网络参数</Button></div><p v-if="networkDirty" class="m-0 pb-3 text-xs text-warning-accent">网络参数有修改，请保存后重新预览转发。</p></details>
          <div class="flex flex-wrap gap-2"><Button variant="outline" class="min-h-11" :disabled="!canNetwork || busy" @click="previewNetwork"><LoaderCircle v-if="action === 'preview-network'" class="size-4 animate-spin" />检查并预览转发</Button><Button class="min-h-11" :disabled="!canApply" @click="applyNetwork"><LoaderCircle v-if="action === 'apply'" class="size-4 animate-spin" />应用网关转发</Button></div>
          <div v-if="networkPreview" class="space-y-3 border-t border-border pt-3 text-sm" role="status">
            <p class="m-0 font-semibold" :class="networkPreview.ready ? 'text-success-accent' : 'text-destructive-accent'">{{ networkPreview.ready ? '检查通过，等待应用' : '请处理以下检查结果' }}</p>
            <div v-if="networkPreview.blockers?.length" class="text-xs leading-relaxed" role="alert"><p class="m-0 font-medium text-destructive-accent">阻断问题</p><ul class="mt-1 mb-0 list-disc space-y-1 pl-5 text-foreground"><li v-for="item in networkPreview.blockers" :key="item" class="break-words">{{ item }}</li></ul></div>
            <div v-if="visibleNetworkWarnings.length" class="text-xs leading-relaxed"><p class="m-0 font-medium text-warning-accent">检查提醒</p><ul class="mt-1 mb-0 list-disc space-y-1 pl-5 text-foreground"><li v-for="item in visibleNetworkWarnings" :key="item" class="break-words">{{ item }}</li></ul></div>
            <details v-if="networkPreview.commands?.length" class="text-xs text-muted-foreground"><summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">查看网络操作</summary><ul class="mt-0 mb-2 list-disc space-y-1 pl-5"><li v-for="item in networkPreview.commands" :key="item" class="break-words">{{ item }}</li></ul></details>
          </div>
          <p v-if="legacy && activeNetwork" class="m-0 text-xs leading-relaxed" data-legacy-clients>主路由转来的连接由这台 Linux 接管。客户端网关保持主路由，DNS 保持接入主路由时的设置，无需修改。</p>
          <p v-else-if="legacy && networkRemoved" class="m-0 text-xs leading-relaxed" role="status" data-legacy-clients>请删除主路由上指向这台 Linux 的静态路由，或重新应用网关转发恢复分流。客户端设置无需修改。</p>
          <p v-else-if="legacy" class="m-0 text-xs leading-relaxed" data-legacy-clients>应用成功后，主路由转来的连接重新由 Mihomo 接管；客户端设置无需修改。</p>
          <GatewayClientInstructions v-else-if="activeNetwork && !embedded" :address="network.address" />
          <div v-else-if="networkRemoved" class="space-y-2 text-xs" role="status">
            <p class="m-0 leading-relaxed">恢复原网关和 DNS（原来自动获取的改回自动获取），以及测试前的 IPv6、浏览器安全 DNS 和系统私有 DNS。修改过主路由 DHCP 时，也恢复原值。</p>
            <p class="m-0 leading-relaxed text-muted-foreground">核心和已发布配置仍保留。撤销转发不会关闭 Mihomo TUN；要停用，请到「配置生成 → Mihomo → Linux 网关」关闭模式，再保存并发布。</p>
          </div>
          <p v-else-if="!activeNetwork" class="m-0 text-xs leading-relaxed">应用成功后再修改客户端。接入前记下原网关、DNS、IPv6 和安全 DNS 设置，以便恢复。</p>
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-border pt-3 text-xs text-muted-foreground">
            <Button v-if="managedNetwork" variant="outline" class="min-h-11" :disabled="busy || !canInspectNetwork" @click="removeNetwork">撤销网关转发</Button>
            <p v-if="managedNetwork" class="m-0">{{ legacy ? '撤销前，先删除主路由上指向这台 Linux 的静态路由。' : '撤销前，先恢复客户端原来的网关与 DNS。' }}</p>
            <Button variant="ghost" class="min-h-11 px-2" :disabled="busy || refreshPending || !canInspectNetwork" @click="inspectNetwork(true)">重新读取网络状态</Button>
          </div>
          <details class="border-b border-border text-xs"><summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">后续管理</summary><div class="space-y-2 pt-1 pb-3 leading-relaxed text-muted-foreground"><p v-if="legacy" class="m-0">主路由转发的网段在「上游路由」管理。</p><p v-else-if="activeNetwork" class="m-0">测试通过后再接入其他设备。支持 DHCP 分发网关与 DNS 的主路由，可将两项都设为 <code class="font-mono">{{ network.address }}</code>。</p><p class="m-0">配置生成 → Mihomo → Linux 网关。未选 IP、应用自带 DoH 或绕过网关的流量不自动代理。</p></div></details>
          </template>
        </section>
        </TabsContent>
      </Tabs>
    </div>
    <template #footer>
      <div v-if="embedded" class="flex flex-wrap items-center gap-2">
        <Button v-if="!locked" class="min-h-11" :disabled="busy || !config || !!pollError" @click="generatePreparedInstallation"><LoaderCircle v-if="busy" class="size-4 animate-spin" />生成安装命令</Button>
        <Button v-else-if="activeNetwork && installation?.phase === 'published' && !installationBlocked && !agentWarnings.length && !pollError && !error" class="min-h-11" :disabled="busy || refreshPending" @click="emit('completed')">{{ goal === 'router' ? '继续接入主路由' : '继续设置测试客户端' }}</Button>
        <span v-else class="text-xs text-muted-foreground">{{ step === 4 ? (fullTun ? '转发与 TUN 检查通过后，再接入主路由。' : goal === 'router' ? '检查并启用转发后，再接入主路由。' : '检查并启用转发后，才能接入客户端。') : step === 3 ? (fullTun ? '按顺序发布并检查两项配置后，再检查转发与 TUN。' : '按顺序发布并检查两项配置后，再启用转发。') : '请在目标 Linux 执行命令，完成登记后继续发布与检查。' }}</span>
      </div>
      <div v-else class="flex min-w-0 flex-wrap items-center justify-between gap-2">
        <p class="m-0 shrink-0 text-xs text-muted-foreground"><span class="num">{{ step + 1 }} / 5</span><span class="hidden sm:inline"> · {{ steps[step] }}</span></p>
        <div class="ml-auto flex min-w-0 flex-wrap justify-end gap-2">
          <Button v-if="step === 1 && !locked" variant="outline" class="min-h-11" :disabled="busy" @click="step = 0">上一步</Button>
          <Button v-if="step === 0" class="min-h-11" :disabled="busy || !config" @click="continueToRules">下一步：选择规则</Button>
          <Button v-else-if="step === 1" class="min-h-11" :disabled="busy || !configReady || !!environmentErrors.length" @click="createInstallation()"><LoaderCircle v-if="action === 'create'" class="size-4 animate-spin" />保存并生成安装命令</Button>
          <Button v-else variant="outline" class="min-h-11" :disabled="busy" @click="emit('update:open', false)">{{ activeNetwork || networkRemoved ? '完成，返回 Agent' : '稍后继续' }}</Button>
        </div>
      </div>
    </template>
  </SettingsWorkspace>
</template>
