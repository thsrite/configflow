<script setup lang="ts">
import { computed, inject, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { routerKey } from 'vue-router'
import { Check, CircleCheck, Copy, FileDiff, LoaderCircle, Network, RefreshCw, RotateCcw } from '@lucide/vue'
import { agentApi, profileApi, type PublishGuard } from '@/api'
import { linuxGatewayApi, type LinuxGatewayConfig, type LinuxGatewayDraftPreview, type LinuxGatewayRestorePoint, type LinuxGatewayCandidate, type LinuxGatewayPreview, type LinuxGatewayRouterSplit, type LinuxGatewaySettings, type LinuxGatewayStatus } from '@/api/linuxGateway'
import { FORWARDING_AGENT_VERSION, ROUTER_SPLIT_MESSAGES, gatewayLeftover, routerSplitErrorMessage } from '@/lib/routerSplit'
import { cleanGatewaySpec, emptyGatewaySpec, gatewayErrorMessage, gatewaySettingsKey, gatewaySpecErrors, gatewaySpecKey } from '@/lib/linuxGateway'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { copyText } from '@/lib/clipboard'
import { forwardFilterWarning } from '@/lib/forwardFilter'
import SettingsWorkspace from './SettingsWorkspace.vue'
import SettingsPublishBar from './SettingsPublishBar.vue'
import FormField from '@/components/common/FormField.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import { gatewayCopy } from '@/lib/gatewayCopy'
import { optionHelp } from '@/lib/optionCopy'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import ForwardFilterNotice from '@/components/agents/ForwardFilterNotice.vue'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

type Agent = { id: string; name: string; profile_id?: string; service_type: string; status?: string; enabled?: boolean; deployment_method?: string }
type Profile = { id: string; name: string; is_default?: boolean }
const props = defineProps<{ open: boolean; profileId: string }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()
// Optional: the editor is also mounted outside a routed page.
const router = inject(routerKey, null)
const settings = reactive<LinuxGatewaySettings>({ enabled: false, rule_set_ids: [] })
const savedSettings = ref<LinuxGatewaySettings>({ enabled: false, rule_set_ids: [] })
const candidates = ref<LinuxGatewayCandidate[]>([])
const fakeIPRange = ref('')
const warnings = ref<string[]>([])
const refreshInterval = ref(300)
const search = ref('')
const agents = ref<Agent[]>([])
const profiles = ref<Profile[]>([])
const selectedAgentId = ref('')
const network = reactive(emptyGatewaySpec())
const networkBaseline = ref(gatewaySpecKey(network))
const status = ref<LinuxGatewayStatus | null>(null)
const preview = ref<LinuxGatewayPreview | null>(null)
const previewKey = ref('')
const loading = ref(false)
const saving = ref(false)
const configRevision = ref(0)
const restorePoint = ref<LinuxGatewayRestorePoint | null>(null)
const draftReview = ref<LinuxGatewayDraftPreview | null>(null)
const reviewKey = ref('')
const draftAction = ref<'' | 'preview' | 'restore'>('')
const draftError = ref('')
const networkAction = ref<'' | 'status' | 'preview' | 'apply' | 'remove'>('')
const loadError = ref('')
const targetError = ref('')
const networkError = ref('')
const networkResult = ref('')
const routerSplit = ref<LinuxGatewayRouterSplit | null>(null)
let session = 0
let hostSession = 0
let disposed = false
const profileName = computed(() => profiles.value.find(item => item.id === props.profileId)?.name || props.profileId)
const defaultProfileId = computed(() => profiles.value.find(item => item.is_default)?.id || 'default')
const targets = computed(() => agents.value.filter(item => item.service_type === 'mihomo'
  && (item.profile_id || defaultProfileId.value) === props.profileId))
const selectedAgent = computed(() => targets.value.find(item => item.id === selectedAgentId.value))
const online = computed(() => selectedAgent.value?.status === 'online' && selectedAgent.value?.enabled !== false)
const shell = computed(() => selectedAgent.value?.deployment_method !== 'docker')
const managedNetwork = computed(() => !!(status.value?.managed?.active || status.value?.managed?.pending))
const networkState = computed(() => {
  if (networkAction.value === 'status') return { label: '正在检测', tone: 'progress' as const }
  if (status.value?.managed?.pending) return { label: '待恢复', tone: 'warning' as const }
  if (status.value?.managed?.active) return status.value.managed.healthy === false
    ? { label: '需要修复', tone: 'warning' as const } : { label: '已应用', tone: 'success' as const }
  return { label: status.value ? '未应用' : '等待检测', tone: 'neutral' as const }
})
const configDirty = computed(() => gatewaySettingsKey(settings) !== gatewaySettingsKey(savedSettings.value))
const networkDirty = computed(() => !!selectedAgentId.value && gatewaySpecKey(network) !== networkBaseline.value)
const busy = computed(() => loading.value || saving.value || !!networkAction.value || !!draftAction.value)
// A router split keeps its takeover outside this editor: full TUN never uses the
// selective gateway, and the legacy split must not be switched off here.
const splitMode = computed(() => routerSplit.value?.mode ?? null)
const invalidMode = computed(() => splitMode.value === 'invalid')
const fullTunMode = computed(() => splitMode.value === 'full_tun' || invalidMode.value)
const legacyMode = computed(() => splitMode.value === 'legacy')
const tunMissing = computed(() => splitMode.value === 'full_tun' && !!routerSplit.value?.issues?.includes('tun_missing'))
const tunFiltered = computed(() => splitMode.value === 'full_tun' && !!routerSplit.value?.issues?.includes('tun_filtered'))
const forwardedRuleNames = computed(() => (routerSplit.value?.route_rule_set_ids || []).map(id => candidates.value.find(item => item.id === id)?.name || id))
// Full TUN only, and a warning only: it never changes the publish bar or any action here.
const filterWarning = computed(() => fullTunMode.value && !invalidMode.value && selectedAgentId.value ? forwardFilterWarning(status.value?.forward_filter) : null)
const leftoverGateway = computed(() => fullTunMode.value && !invalidMode.value ? gatewayLeftover(status.value) : null)
const configured = computed(() => savedSettings.value.enabled && settings.enabled && !configDirty.value)
const errors = computed(() => gatewaySpecErrors(network))
const currentPreviewKey = computed(() => JSON.stringify([props.profileId, selectedAgentId.value, gatewaySpecKey(network), gatewaySettingsKey(settings)]))
const canPreview = computed(() => configured.value && online.value && shell.value && !!status.value && status.value.supported !== false && !busy.value && errors.value.length === 0)
const canApply = computed(() => canPreview.value && status.value?.enabled_in_config === true && preview.value?.ready === true && previewKey.value === currentPreviewKey.value)
const rows = computed(() => {
  const all = [...candidates.value]
  for (const id of settings.rule_set_ids) {
    if (!all.some(item => item.id === id)) all.push({ id, name: `已移除的规则集（${id}）`, behavior: '', available: false, enabled: false, description: '接管范围已排除此项，可取消勾选清理关联。' })
  }
  return all.filter(item => `${item.name} ${item.policy || ''}`.toLowerCase().includes(search.value.trim().toLowerCase()))
})
const selectedAvailable = computed(() => candidates.value.filter(item => item.available && settings.rule_set_ids.includes(item.id)).length)
const capture = () => ({ session, hostSession, profileId: props.profileId, agentId: selectedAgentId.value })
const current = (token: ReturnType<typeof capture>, host = false) => !disposed && props.open && token.session === session
  && token.profileId === props.profileId && (!host || (token.hostSession === hostSession && token.agentId === selectedAgentId.value))

function acceptConfig(data: LinuxGatewayConfig) {
  Object.assign(settings, { enabled: data.settings.enabled, rule_set_ids: [...data.settings.rule_set_ids] })
  savedSettings.value = { enabled: settings.enabled, rule_set_ids: [...settings.rule_set_ids] }
  configRevision.value = data.config_revision
  restorePoint.value = data.restore_point || null
  if (data.candidates) candidates.value = data.candidates
  if (data.warnings) warnings.value = data.warnings
  if (data.fake_ip_range) fakeIPRange.value = data.fake_ip_range
  refreshInterval.value = data.refresh_interval || 300
  if (data.router_split !== undefined) routerSplit.value = data.router_split || null
  draftReview.value = null
  preview.value = null
  networkResult.value = ''
}

function failureMessage(error: any, hostOperation = false) {
  const code = error?.response?.data?.code
  return code ? routerSplitErrorMessage(code, error.response.data.message) : gatewayErrorMessage(error, hostOperation)
}

async function reviewDraft(action: 'save' | 'restore', token: ReturnType<typeof capture>) {
  const key = gatewaySettingsKey(settings)
  const { data } = action === 'restore'
    ? await linuxGatewayApi.previewRestore(token.profileId, configRevision.value)
    : await linuxGatewayApi.previewDraft(token.profileId, { enabled: settings.enabled, rule_set_ids: [...settings.rule_set_ids] }, configRevision.value)
  if (!current(token) || key !== gatewaySettingsKey(settings)) throw new Error('设置页面或试用内容已改变，请重新预览。')
  if (data.success === false) throw new Error((data as any).message || '配置预览失败')
  draftReview.value = data
  reviewKey.value = key
  return data
}

async function previewDraft() {
  if (busy.value) return
  const token = capture()
  draftAction.value = 'preview'
  draftReview.value = null
  draftError.value = ''
  try { await reviewDraft('save', token) }
  catch (error) { if (current(token)) draftError.value = failureMessage(error) }
  finally { if (current(token)) draftAction.value = '' }
}

async function reloadDraft() {
  if (busy.value) return
  const token = capture()
  if (configDirty.value && !await confirm('重新读取会放弃本页未保存的试用设置。', { title: '重新读取草稿？', confirmText: '放弃试用并读取' })) return
  if (current(token) && !busy.value) await load()
}

async function load() {
  const token = capture()
  loading.value = true
  loadError.value = ''
  targetError.value = ''
  const [configResult, agentsResult, profilesResult] = await Promise.allSettled([
    linuxGatewayApi.get(token.profileId), agentApi.getAll(), profileApi.list(),
  ])
  if (!current(token)) return
  if (configResult.status === 'fulfilled') {
    acceptConfig(configResult.value.data)
  } else loadError.value = failureMessage(configResult.reason)
  if (profilesResult.status === 'fulfilled') profiles.value = profilesResult.value.data
  if (agentsResult.status === 'fulfilled' && profilesResult.status === 'fulfilled') {
    agents.value = agentsResult.value.data
    if (targets.value.length === 1) selectedAgentId.value = targets.value[0].id
  } else targetError.value = '暂时无法读取设备或配置空间列表，仍可保存网关配置。请重试读取。'
  loading.value = false
}

async function save(): Promise<PublishGuard | null> {
  if (busy.value || loadError.value) throw new Error('配置尚未就绪，请重试读取后再保存。')
  if (fullTunMode.value) throw new Error(ROUTER_SPLIT_MESSAGES[invalidMode.value ? 'ROUTER_SPLIT_INVALID' : 'ROUTER_SPLIT_FULL_TUN'])
  const token = capture()
  const draft = { enabled: settings.enabled, rule_set_ids: [...settings.rule_set_ids] }
  saving.value = true
  draftError.value = ''
  try {
    const review = draftReview.value?.action === 'save' && reviewKey.value === gatewaySettingsKey(draft)
      ? draftReview.value : await reviewDraft('save', token)
    const approved = await confirm(`将保存当前网关设置，生成配置有 ${review.changes.length} 项变更。基础 YAML 保持原样；首次更改会保留试用前的 Mihomo、OxiDNS 草稿供恢复。`, {
      title: '保存网关草稿？', confirmText: '确认保存',
    })
    if (!approved || !current(token) || gatewaySettingsKey(draft) !== gatewaySettingsKey(settings)) return null
    const { data } = await linuxGatewayApi.save(token.profileId, draft, {
      expected_config_revision: review.config_revision, preview_token: review.preview_token,
    })
    if (!current(token)) throw new Error('设置页面已关闭或配置空间已改变。')
    if (data.success === false) throw new Error((data as any).message || '保存失败')
    acceptConfig(data)
    return data.publish_guard || null
  } catch (error) {
    if (current(token)) { draftReview.value = null; draftError.value = failureMessage(error); notify.error(`保存网关配置失败：${draftError.value}`) }
    throw error
  } finally { if (current(token)) saving.value = false }
}

async function restoreDraft() {
  // Legacy: the server refuses only a snapshot that turns the gateway off.
  if (busy.value || !restorePoint.value || fullTunMode.value) return
  const token = capture()
  const key = gatewaySettingsKey(settings)
  draftAction.value = 'restore'
  draftError.value = ''
  try {
    if (draftReview.value?.action !== 'restore' || reviewKey.value !== key) {
      await reviewDraft('restore', token)
      return
    }
    const review = draftReview.value
    const approved = await confirm(`将恢复快照中的 Mihomo 和 OxiDNS 草稿，撤销快照之后对这两份草稿的修改${configDirty.value ? '，并放弃本页未保存的试用设置' : ''}。请核对页面中的 ${review.changes.length} 项变更。恢复后需要另行发布才能在设备生效。`, {
      title: '恢复试用前草稿？', confirmText: '确认恢复草稿', danger: true,
    })
    if (!approved || !current(token) || key !== gatewaySettingsKey(settings)) return
    const { data } = await linuxGatewayApi.restoreDraft(token.profileId, {
      expected_config_revision: review.config_revision, preview_token: review.preview_token,
    })
    if (!current(token)) return
    if (data.success === false) throw new Error((data as any).message || '恢复失败')
    acceptConfig(data)
    notify.success('Mihomo、OxiDNS 草稿已恢复。请按需另行发布到设备。')
  } catch (error) {
    if (current(token)) { draftReview.value = null; draftError.value = failureMessage(error); notify.error(`恢复草稿失败：${draftError.value}`) }
  } finally { if (current(token)) draftAction.value = '' }
}

async function reloadTargets() {
  const token = capture()
  loading.value = true
  targetError.value = ''
  try {
    const [agentResponse, profileResponse] = await Promise.all([agentApi.getAll(), profileApi.list()])
    if (!current(token)) return
    profiles.value = profileResponse.data
    agents.value = agentResponse.data
    if (selectedAgentId.value && !targets.value.some(item => item.id === selectedAgentId.value)) selectedAgentId.value = ''
  } catch (error) {
    if (current(token)) targetError.value = `暂时无法读取目标设备：${gatewayErrorMessage(error)}`
  } finally { if (current(token)) loading.value = false }
}

async function saveDraft() {
  const token = capture()
  try { const guard = await save(); if (guard && current(token)) notify.success('网关草稿已保存。请发布 Mihomo，再继续配置设备网络。') }
  catch { /* The save callback also supplies feedback for SettingsPublishBar. */ }
}

function toggleRule(id: string, checked: boolean | 'indeterminate') {
  settings.rule_set_ids = checked === true ? [...new Set([...settings.rule_set_ids, id])] : settings.rule_set_ids.filter(item => item !== id)
}

async function loadStatus(fill = false) {
  if (!selectedAgentId.value || !online.value || !shell.value) return
  const token = capture()
  networkAction.value = 'status'
  preview.value = null
  networkError.value = ''
  try {
    const { data } = await linuxGatewayApi.status(token.agentId, token.profileId)
    if (!current(token, true)) return
    if (data.success === false) throw new Error(data.message || '设备检测失败')
    status.value = data
    networkError.value = [data.unsupported_reason, data.detection_error, data.config_error, data.managed?.error].filter(Boolean).join('；')
    if (fill) {
      Object.assign(network, emptyGatewaySpec(), data.managed?.spec || data.detection?.suggested || {})
      networkBaseline.value = gatewaySpecKey(network)
    }
  } catch (error) {
    if (current(token, true)) { status.value = null; networkError.value = gatewayErrorMessage(error, true) }
  } finally { if (current(token, true)) networkAction.value = '' }
}

async function previewNetwork() {
  if (!canPreview.value) return
  const token = capture()
  const key = currentPreviewKey.value
  preview.value = null
  networkAction.value = 'preview'
  networkError.value = ''
  networkResult.value = ''
  try {
    const { data } = await linuxGatewayApi.preview(token.agentId, token.profileId, cleanGatewaySpec(network))
    if (!current(token, true) || key !== currentPreviewKey.value) return
    if (data.success === false) throw new Error(data.message || '网络预览失败')
    preview.value = data
    previewKey.value = key
    if (data.managed) status.value = { ...status.value!, managed: data.managed }
  } catch (error) {
    if (current(token, true)) networkError.value = gatewayErrorMessage(error, true)
  } finally { if (current(token, true)) networkAction.value = '' }
}

async function applyNetwork() {
  if (!canApply.value) return
  const token = capture()
  const key = currentPreviewKey.value
  networkAction.value = 'apply'
  networkError.value = ''
  preview.value = null
  try {
    // Only this explicit action can mutate the host; saves and mode switches cannot.
    const { data } = await linuxGatewayApi.apply(token.agentId, token.profileId, cleanGatewaySpec(network))
    if (!current(token, true) || key !== currentPreviewKey.value) return
    if (!data.success) throw new Error(data.message || '应用网关转发失败')
    status.value = { ...status.value!, ...data }
    networkBaseline.value = gatewaySpecKey(network)
    preview.value = null
    networkResult.value = data.message || '网关转发已应用。请先测试一台客户端。'
    notify.success('网关转发已应用')
  } catch (error) {
    if (current(token, true)) {
      const failure = error as any
      if (failure?.response?.data?.managed) status.value = { ...status.value!, managed: failure.response.data.managed }
      networkError.value = `${failureMessage(error, true)} 请刷新检测，确认设备实际状态后再操作。`
    }
  } finally { if (current(token, true)) networkAction.value = '' }
}

async function removeNetwork() {
  if (busy.value || !selectedAgentId.value || !online.value || invalidMode.value) return
  const token = capture()
  // Legacy split: the router still forwards here, so revoking interrupts it.
  const teardown = legacyMode.value
  const leftover = fullTunMode.value
  const accepted = leftover
    ? await confirm('将删除旧的 configflow_gateway 规则和网关记录；IPv4 转发已持久化，会保持开启。', { title: `清理「${selectedAgent.value?.name}」的旧网关记录？`, confirmText: '清理旧记录' })
    : teardown
      ? await confirmDanger('主路由仍把代理网段转给这台 Linux。撤销后 TUN 接管与 IPv4 转发会还原，这些连接会中断，直到重新应用转发。', { title: `撤销「${selectedAgent.value?.name}」的网关转发？`, confirmText: '仍然撤销' })
      : await confirm('请先恢复客户端原来的网关和 DNS；原先自动获取的设备恢复自动获取。此操作会移除 ConfigFlow 管理的转发规则，并恢复它接管的系统参数。', {
        title: `撤销「${selectedAgent.value?.name}」的网关转发？`, confirmText: '撤销转发', cancelText: '取消',
      })
  if (!accepted || !current(token, true) || busy.value) return
  networkAction.value = 'remove'
  networkError.value = ''
  try {
    const { data } = teardown ? await linuxGatewayApi.remove(token.agentId, token.profileId, true) : await linuxGatewayApi.remove(token.agentId, token.profileId)
    if (!current(token, true)) return
    if (!data.success) throw new Error(data.message || '撤销网关转发失败')
    status.value = { ...status.value!, ...data }
    preview.value = null
    networkBaseline.value = gatewaySpecKey(network)
    // For a router split the server replaces the Agent's text (restore client
    // gateways), which its clients never changed; it also says whether a record existed.
    networkResult.value = data.message || (leftover ? '旧网关记录已清理，IPv4 转发保持开启。'
      : teardown ? '网关转发已撤销，主路由转来的连接已中断。请删除主路由上指向这台 Linux 的静态路由，或重新应用网关转发。'
        : '网关转发已撤销。接下来关闭 Linux 网关，再保存并发布 Mihomo。')
    notify.success(leftover ? '旧网关记录已清理' : '网关转发已撤销')
  } catch (error) {
    if (current(token, true)) networkError.value = `${failureMessage(error, true)} 请刷新检测确认设备状态。`
  } finally { if (current(token, true)) networkAction.value = '' }
}

function openRouters() {
  emit('update:open', false)
  void router?.push('/routers')
}

async function copyCommands() {
  try { await copyText((preview.value?.commands || []).join('\n')); notify.success('已复制操作说明') }
  catch { notify.error('复制失败，请手动选择操作说明复制') }
}

watch(() => [props.open, props.profileId], () => {
  session++
  hostSession++
  loading.value = false
  saving.value = false
  draftAction.value = ''
  draftReview.value = null
  draftError.value = ''
  restorePoint.value = null
  routerSplit.value = null
  configRevision.value = 0
  networkAction.value = ''
  selectedAgentId.value = ''
  status.value = null
  preview.value = null
  networkError.value = ''
  networkResult.value = ''
  search.value = ''
  Object.assign(network, emptyGatewaySpec())
  networkBaseline.value = gatewaySpecKey(network)
  if (props.open) void load()
}, { immediate: true, flush: 'sync' })

watch(selectedAgentId, () => {
  hostSession++
  status.value = null
  preview.value = null
  networkAction.value = ''
  networkError.value = ''
  networkResult.value = ''
  Object.assign(network, emptyGatewaySpec())
  networkBaseline.value = gatewaySpecKey(network)
  if (props.open && selectedAgentId.value) void loadStatus(true)
}, { flush: 'sync' })
watch(currentPreviewKey, () => { preview.value = null; previewKey.value = '' }, { flush: 'sync' })
watch(() => gatewaySettingsKey(settings), () => { draftReview.value = null; reviewKey.value = ''; draftError.value = '' }, { flush: 'sync' })
onBeforeUnmount(() => { disposed = true; session++; hostSession++ })
</script>


<template>
  <SettingsWorkspace :open="open" title="Linux 网关" description="试用并预览配置变更，保存后再发布或应用网关转发。" :dirty="configDirty || networkDirty" :busy="busy" :save-disabled="!!loadError || loading || fullTunMode || (configDirty && draftReview?.action !== 'save')" size="form" @update:open="emit('update:open', $event)" @save="saveDraft">
    <div class="min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain [overflow-wrap:anywhere]">
      <div class="mx-auto w-full min-w-0 max-w-3xl space-y-4 p-3 sm:p-5">
        <div v-if="loading" role="status" class="flex items-center gap-2 py-4 text-sm text-muted-foreground"><LoaderCircle class="size-4 animate-spin" />正在读取网关配置…</div>
        <div v-else-if="loadError" role="alert" class="space-y-3 text-sm"><p class="m-0 text-destructive-accent">{{ loadError }}</p><Button variant="outline" class="min-h-11" @click="load">重试读取</Button></div>
        <template v-else>
          <div class="space-y-1 border-b border-border pb-3 text-xs text-muted-foreground">
            <p class="m-0">配置空间 · <span class="font-medium text-foreground">{{ profileName }}</span></p>
            <p class="m-0 leading-relaxed">规则由此空间的 Mihomo 设备共用，网络设置仅应用到所选 Linux。</p>
          </div>

          <div v-if="invalidMode" role="alert" class="rounded-lg border border-destructive-accent/30 bg-destructive-soft px-3 py-2.5 text-xs leading-relaxed" data-router-split="invalid">
            <p class="m-0 text-sm font-semibold text-destructive-accent">主路由分流标记无效</p>
            <p class="mt-1 mb-0 text-foreground">{{ ROUTER_SPLIT_MESSAGES.ROUTER_SPLIT_INVALID }}</p>
          </div>
          <div v-else-if="fullTunMode" role="status" class="space-y-2 rounded-lg border border-primary-accent/30 bg-primary-soft px-3 py-2.5 text-xs leading-relaxed" data-router-split="full_tun">
            <p class="m-0 text-sm font-semibold text-primary-accent">主路由分流（完整 TUN）</p>
            <p class="m-0 text-foreground">Mihomo 以完整 TUN 接管主路由转来的连接，不使用这里的选择性接管。启用会把完整 TUN 改成选择性接管，未应用转发时真实 IP 网段会回环，因此不可用。</p>
            <Button variant="link" class="h-auto min-h-11 p-0 text-xs text-primary-accent" @click="openRouters()">到上游路由管理转发网段</Button>
          </div>
          <div v-else-if="legacyMode" role="status" class="space-y-2 rounded-lg border border-warning-accent/30 bg-warning-soft px-3 py-2.5 text-xs leading-relaxed" data-router-split="legacy">
            <p class="m-0 text-sm font-semibold text-warning-accent">旧版主路由分流</p>
            <p class="m-0 text-foreground">关闭开关、恢复到网关关闭的快照或撤销转发，会同时去掉 TUN 并还原 IPv4 转发，主路由转来的连接会中断。</p>
          </div>
          <p v-if="tunMissing" role="alert" class="m-0 text-xs leading-relaxed" data-tun-missing><span class="font-medium text-warning-accent">基础 YAML 缺少完整 TUN，发布已暂停。</span>请在 Mihomo 基础 YAML 中恢复 tun 后再发布，否则主路由转来的连接会回环。</p>
          <p v-if="tunFiltered" role="alert" class="m-0 text-xs leading-relaxed" data-tun-filtered><span class="font-medium text-warning-accent">基础 YAML 的 tun 带有过滤，发布已暂停。</span>exclude-interface、include-uid 等过滤会让未进入 TUN 的连接在主路由与 Linux 之间回环，请在 Mihomo 基础 YAML 中移除后再发布。</p>

          <section aria-labelledby="gateway-rules" class="space-y-3">
            <div class="flex items-center gap-1"><h2 id="gateway-rules" class="m-0 text-lg font-semibold">接管规则</h2><FieldHelp label="Linux 网关" :text="splitMode ? optionHelp('主路由分流：主路由把 fake-IP 和所选规则集的网段转给这台 Linux。', '客户端网关保持主路由，只有转来的连接交给 Mihomo，其余由主路由直接转发。转发网段在「上游路由」管理。') : optionHelp('Linux + nftables 的 IPv4 网络，用 Shell Agent。', '设备以 Linux 为网关和 DNS，选中连接交给 Mihomo，其余直接转发。保存并发布配置后，还需单独应用网关转发；IPv6 需另行处理。')" /></div>
            <label for="linux-gateway-enabled" class="flex min-h-11 cursor-pointer items-center justify-between gap-4 border-b border-border pb-3">
              <span><span class="block text-sm font-medium">启用 Linux 网关</span><span class="mt-1 block text-xs text-muted-foreground">切换只在本页试用。预览并保存后才更新草稿，网关转发需单独应用。</span></span>
              <span class="flex shrink-0 items-center gap-1">
                <span v-if="fullTunMode" class="text-xs text-muted-foreground">不适用于主路由分流</span>
                <FieldHelp v-else-if="legacyMode && savedSettings.enabled" label="Linux 网关开关" text="主路由分流依赖此接管，关闭会让主路由转来的连接中断。" />
                <Switch id="linux-gateway-enabled" v-model="settings.enabled" :disabled="busy || fullTunMode || (legacyMode && savedSettings.enabled)" />
              </span>
            </label>
            <p v-if="!settings.enabled && savedSettings.enabled" class="m-0 text-xs leading-relaxed" role="status"><span class="font-medium text-warning-accent">准备停用：</span>先恢复客户端并撤销网关转发，再保存并发布已关闭模式的 Mihomo 配置。</p>
            <div v-if="fullTunMode && !invalidMode" class="space-y-1 text-xs leading-relaxed" data-router-routes>
              <p class="m-0 text-sm font-medium">安装时选择的转发网段：fake-IP + <span class="num">{{ forwardedRuleNames.length }}</span> 个规则集</p>
              <p v-if="forwardedRuleNames.length" class="m-0 text-muted-foreground [overflow-wrap:anywhere]">{{ forwardedRuleNames.join('、') }}</p>
              <p class="m-0 text-muted-foreground">主路由实际转发哪些网段，以「上游路由」中的设置为准。</p>
            </div>
            <div v-else-if="!fullTunMode" class="space-y-2">
              <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1"><div class="flex min-h-8 items-center gap-1"><h3 class="m-0 text-sm font-semibold">IP 接管（可选）</h3><span class="cf-compact-help"><FieldHelp label="IP 接管" :text="gatewayCopy.applications.help" /></span></div><span class="text-xs text-muted-foreground">已选 <span class="num">{{ selectedAvailable }}</span> 个可用规则集</span></div>
              <p class="m-0 text-xs leading-relaxed text-muted-foreground">{{ gatewayCopy.applications.hint }}</p>
              <p v-if="fakeIPRange" class="m-0 text-xs text-muted-foreground">自动接管的临时地址范围（fake-IP）：<span class="font-mono">{{ fakeIPRange }}</span></p>
              <Input v-model="search" aria-label="搜索可接管的 IP 规则集" placeholder="搜索规则集名称或策略" class="min-h-11" :disabled="busy" />
              <div class="max-h-64 overflow-y-auto rounded-md border border-border">
                <label v-for="item in rows" :key="item.id" :for="`gateway-rule-${item.id}`" class="flex min-h-11 cursor-pointer items-start gap-3 border-b border-border px-3 py-2.5 last:border-b-0 hover:bg-accent/40">
                  <Checkbox :id="`gateway-rule-${item.id}`" class="mt-0.5 shrink-0" :model-value="settings.rule_set_ids.includes(item.id)" :disabled="busy || !settings.enabled || (!item.available && !settings.rule_set_ids.includes(item.id))" @update:model-value="toggleRule(item.id, $event)" />
                  <span class="min-w-0 flex-1 space-y-0.5 text-xs leading-relaxed"><span class="block text-sm font-medium">{{ item.name }}</span><span class="block text-muted-foreground">{{ item.behavior === 'classical' ? '仅提取目的 IPv4' : '目的 IP 规则集' }}<template v-if="item.policy"> · 出口策略：{{ item.policy }}</template></span><span v-if="!item.available" class="block text-warning-accent">{{ item.reason || item.description || '已停用或不可用于接管；当前不会进入接管范围。' }}</span><span v-else-if="item.policy?.toUpperCase() === 'DIRECT'" class="block text-warning-accent">当前策略为 DIRECT，接管后仍由 Mihomo 直连。</span></span>
                </label>
                <p v-if="!rows.length" class="m-0 px-3 py-4 text-xs leading-relaxed text-muted-foreground">{{ search ? '没有匹配的规则集。' : '当前空间没有可用 IP 规则集，可只接管 fake-IP。需要时在「规则库」添加，再到「策略规则」关联并启用。' }}</p>
              </div>
              <p class="m-0 text-xs text-muted-foreground">修改选择后，请在底部保存并发布 Mihomo。<template v-if="legacyMode">新增网段也需加入主路由的静态路由。</template></p>
            </div>
            <div v-if="warnings.length" class="text-xs leading-relaxed" role="status"><p class="m-0 font-medium text-warning-accent">配置提醒</p><ul class="mt-1 mb-0 list-disc space-y-1 pl-5 text-foreground"><li v-for="warning in warnings" :key="warning">{{ warning }}</li></ul></div>
          </section>

          <section v-if="!fullTunMode" aria-labelledby="gateway-draft" class="space-y-3 border-t border-border pt-4">
            <div><div class="flex items-center gap-1"><h2 id="gateway-draft" class="m-0 text-lg font-semibold">配置试用与恢复</h2><FieldHelp label="配置试用与恢复" :text="optionHelp('修改了网关开关或接管规则，想先看清会改变什么再保存时使用。', '预览只对比差异，不改动草稿。首次保存时自动备份 Mihomo、OxiDNS 草稿，之后可恢复；基础 YAML 不变，关闭网关即按原配置生成。')" /></div><p class="mt-1 mb-0 text-xs leading-relaxed text-muted-foreground">先预览会改变什么，确认后再保存。</p></div>
            <div class="flex flex-wrap gap-2">
              <Button variant="outline" class="min-h-11" :disabled="busy" @click="previewDraft"><LoaderCircle v-if="draftAction === 'preview'" class="size-4 animate-spin" />预览配置变更</Button>
              <Button v-if="restorePoint" variant="outline" class="min-h-11" :disabled="busy" @click="restoreDraft"><LoaderCircle v-if="draftAction === 'restore'" class="size-4 animate-spin" /><RotateCcw v-else class="size-4" />{{ draftReview?.action === 'restore' ? '恢复这份草稿' : '预览恢复变更' }}</Button>
            </div>
            <p v-if="restorePoint" class="m-0 text-xs leading-relaxed text-muted-foreground">可恢复到 <time :datetime="restorePoint.created_at">{{ new Date(restorePoint.created_at).toLocaleString() }}</time> 的草稿，之后的修改会被撤销。</p>
            <p v-else class="m-0 text-xs leading-relaxed text-muted-foreground">第一次保存时会自动备份当前草稿，之后可以恢复。</p>
            <p v-if="draftError" role="alert" class="m-0 text-xs leading-relaxed text-destructive-accent">{{ draftError }}<Button variant="ghost" class="ml-1 min-h-11" :disabled="busy" @click="reloadDraft">重新读取已保存草稿</Button></p>
            <div v-if="draftReview" class="space-y-2" aria-live="polite">
              <div class="flex items-start gap-2.5 rounded-md border px-3 py-2.5" :class="draftReview.changes.length ? 'border-primary-accent/40 bg-primary-soft' : 'border-success-accent/40 bg-success-soft'">
                <component :is="draftReview.changes.length ? FileDiff : CircleCheck" class="mt-0.5 size-4 shrink-0" :class="draftReview.changes.length ? 'text-primary-accent' : 'text-success-accent'" aria-hidden="true" />
                <div class="min-w-0">
                  <p class="m-0 text-sm font-semibold"><template v-if="draftReview.changes.length">{{ draftReview.action === 'restore' ? '恢复后会改变' : '保存后会改变' }} <span class="num">{{ draftReview.changes.length }}</span> 项配置</template><template v-else>配置没有变化</template><span class="ml-2 text-xs font-normal text-muted-foreground">{{ draftReview.action === 'restore' ? '恢复预览 · 尚未恢复' : '试用预览 · 尚未保存' }}</span></p>
                  <p class="mt-0.5 mb-0 text-xs leading-relaxed text-muted-foreground">{{ draftReview.changes.length ? (draftReview.action === 'restore' ? '核对下方差异，确认后点「恢复这份草稿」，再重新发布。' : '核对下方差异，确认后点底部「保存草稿」或「保存并发布」。') : (draftReview.action === 'restore' ? '恢复后生成的配置与现在相同。' : '当前设置不会改变生成的配置，无需保存。') }}</p>
                </div>
              </div>
              <p v-for="warning in draftReview.warnings" :key="warning" class="m-0 text-xs text-warning-accent">{{ warning }}</p>
              <div v-if="draftReview.changes.length" class="max-h-80 min-w-0 overflow-auto rounded-md border border-border">
                <dl class="m-0 divide-y divide-border">
                  <div v-for="change in draftReview.changes" :key="change.path" class="space-y-2 p-3 text-xs">
                    <dt class="break-all font-mono font-semibold">{{ change.path }}</dt>
                    <dd class="m-0 grid min-w-0 gap-2 sm:grid-cols-2"><div class="min-w-0"><span class="text-muted-foreground">当前</span><pre class="mt-1 mb-0 whitespace-pre-wrap break-all font-mono leading-relaxed">{{ change.before }}</pre></div><div class="min-w-0"><span class="text-primary-accent">{{ draftReview.action === 'restore' ? '恢复后' : '保存后' }}</span><pre class="mt-1 mb-0 whitespace-pre-wrap break-all font-mono leading-relaxed">{{ change.after }}</pre></div></dd>
                  </div>
                </dl>
              </div>
            </div>
            <p v-else class="m-0 text-xs text-muted-foreground" role="status">{{ configDirty ? '有未保存的修改，点「预览配置变更」查看会改变什么。' : '调整上方开关或规则后，点「预览配置变更」查看会改变什么。' }}关闭页面即放弃试用。</p>
          </section>

          <section v-if="fullTunMode && !invalidMode && targets.length" aria-labelledby="gateway-leftover" class="space-y-3 border-t border-border pt-4">
            <div><h2 id="gateway-leftover" class="m-0 text-lg font-semibold">旧网关记录</h2><p class="mt-1 mb-0 text-xs leading-relaxed text-muted-foreground">只在设备上仍有旧的 Linux 网关转发记录时需要清理。</p></div>
            <FormField v-if="targets.length > 1" label="Mihomo 设备" html-for="gateway-agent">
              <Select v-model="selectedAgentId" :disabled="busy"><SelectTrigger id="gateway-agent" class="min-h-11 w-full"><SelectValue placeholder="选择当前空间的 Mihomo 设备" /></SelectTrigger><SelectContent><SelectItem v-for="agent in targets" :key="agent.id" :value="agent.id">{{ agent.name }}{{ agent.enabled === false ? '（已禁用）' : agent.status !== 'online' ? '（离线）' : '' }}</SelectItem></SelectContent></Select>
            </FormField>
            <div v-if="selectedAgentId" class="border-y border-border">
              <AgentStatusRow label="旧网关记录" :status="networkAction === 'status' ? '正在检测' : !status ? '等待检测' : leftoverGateway === 'present' ? '仍存在' : leftoverGateway ? '无法确认' : '无'" :tone="networkAction === 'status' ? 'progress' : leftoverGateway ? 'warning' : status ? 'success' : 'neutral'">
                <template #actions><Button variant="ghost" size="sm" class="min-h-11 text-xs" :disabled="busy || !online || !shell" @click="loadStatus()"><RefreshCw class="size-3.5" />刷新检测</Button></template>
                <template v-if="leftoverGateway" #notice>
                  <p v-if="status?.managed?.preserves_forwarding !== true" class="m-0 text-warning-accent">这台设备尚未持久化 IPv4 转发，清理会关闭转发；请先在安装向导中开启并持久化 IPv4 转发。</p>
                  <Button v-else variant="outline" size="sm" class="min-h-11" :disabled="busy || !online || !shell" @click="removeNetwork"><LoaderCircle v-if="networkAction === 'remove'" class="size-3.5 animate-spin" />清理旧网关记录</Button>
                </template>
              </AgentStatusRow>
            </div>
            <div v-if="networkError" role="alert" class="text-xs leading-relaxed"><p class="m-0 font-medium text-destructive-accent">需要处理</p><p class="mt-1 mb-0">{{ networkError }}</p></div>
            <p v-if="networkResult" role="status" class="m-0 flex items-start gap-2 text-xs leading-relaxed"><Check class="mt-0.5 size-3.5 shrink-0 text-success-accent" />{{ networkResult }}</p>
          </section>

          <section v-if="fullTunMode && !invalidMode && filterWarning" aria-labelledby="gateway-forward-filter" class="space-y-3 border-t border-border pt-4" data-forward-filter-section>
            <div><h2 id="gateway-forward-filter" class="m-0 text-lg font-semibold">转发过滤</h2><p class="mt-1 mb-0 text-xs leading-relaxed text-muted-foreground">{{ selectedAgent ? `「${selectedAgent.name}」` : '这台设备' }}的防火墙检查结果，只作提醒，不影响发布。</p></div>
            <div class="border-y border-border">
              <AgentStatusRow label="转发过滤" :status="filterWarning.status" tone="warning">
                <template #notice><ForwardFilterNotice :filter="status?.forward_filter" /></template>
              </AgentStatusRow>
            </div>
          </section>

          <section v-if="!fullTunMode" aria-labelledby="gateway-network" class="space-y-3 border-t border-border pt-4">
            <div><h2 id="gateway-network" class="m-0 text-lg font-semibold">设备网络</h2><p class="mt-1 mb-0 text-sm text-muted-foreground">核对实际地址，预览通过后再应用转发。</p></div>
            <div v-if="targetError" role="alert" class="text-xs text-destructive-accent">{{ targetError }} <Button variant="ghost" class="min-h-11" :disabled="busy" @click="reloadTargets">重试读取</Button></div>
            <FormField label="承担网关的 Linux 设备" html-for="gateway-agent">
              <Select v-model="selectedAgentId" :disabled="busy"><SelectTrigger id="gateway-agent" class="min-h-11 w-full"><SelectValue placeholder="选择当前空间的 Mihomo 设备" /></SelectTrigger><SelectContent><SelectItem v-for="agent in targets" :key="agent.id" :value="agent.id">{{ agent.name }}{{ agent.enabled === false ? '（已禁用）' : agent.status !== 'online' ? '（离线）' : '' }}</SelectItem></SelectContent></Select>
            </FormField>
            <p v-if="!targets.length && !targetError" class="m-0 text-xs leading-relaxed text-muted-foreground">此空间尚未绑定 Mihomo 设备。请先在 Agent 页面完成安装和绑定；规则设置仍可保存草稿。</p>
            <template v-if="selectedAgentId">
              <p v-if="!online" class="m-0 text-xs leading-relaxed" role="status"><span class="font-medium text-warning-accent">设备离线或已禁用。</span>无法检测和修改网络，仍可保存草稿。</p>
              <p v-else-if="!shell" class="m-0 text-xs leading-relaxed" role="status"><span class="font-medium text-warning-accent">Docker 设备不支持。</span>请选择 Linux Shell 安装的设备，才能修改宿主机网络。</p>
              <div class="border-y border-border">
                <AgentStatusRow label="网络状态" :status="networkState.label" :tone="networkState.tone">
                  <template #actions><Button variant="ghost" size="sm" class="min-h-11 text-xs" :disabled="busy || !online || !shell" @click="loadStatus()"><RefreshCw class="size-3.5" />刷新检测</Button></template>
                  <template v-if="status?.managed?.pending || (status?.managed?.active && status?.managed?.healthy === false)" #notice><p class="m-0">{{ status?.managed?.pending ? '应用未完成，需要恢复或重新检查。' : '已管理，检测到网络配置需要修复。' }}</p></template>
                </AgentStatusRow>
              </div>
              <div class="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
                <FormField label="LAN 网卡" html-for="gateway-interface" v-bind="gatewayCopy.interface"><Input id="gateway-interface" v-model="network.interface" :disabled="busy || !status" class="min-h-11 font-mono" placeholder="ens18" autocomplete="off" /></FormField>
                <FormField label="Linux 局域网 IPv4" html-for="gateway-address" v-bind="splitMode ? gatewayCopy.routerSplitAddress : gatewayCopy.address"><Input id="gateway-address" v-model="network.address" :disabled="busy || !status" class="min-h-11 font-mono" placeholder="192.0.2.2" inputmode="decimal" /></FormField>
                <FormField label="LAN 网段（CIDR）" html-for="gateway-cidr" v-bind="gatewayCopy.cidr"><Input id="gateway-cidr" v-model="network.lan_cidr" :disabled="busy || !status" class="min-h-11 font-mono" placeholder="192.0.2.0/24" /></FormField>
                <FormField label="上游主路由 IPv4" html-for="gateway-upstream" v-bind="gatewayCopy.upstream"><Input id="gateway-upstream" v-model="network.upstream_gateway" :disabled="busy || !status" class="min-h-11 font-mono" placeholder="192.0.2.1" inputmode="decimal" /></FormField>
              </div>
              <div v-if="status && errors.length" class="text-xs leading-relaxed" role="alert"><p class="m-0 font-medium text-destructive-accent">请修正网络信息</p><ul class="mt-1 mb-0 list-disc space-y-1 pl-5"><li v-for="error in errors" :key="error">{{ error }}</li></ul></div>
              <p v-if="!configured" class="m-0 text-xs leading-relaxed" role="status"><span class="font-medium text-warning-accent">先保存并发布。</span>开启模式并保存规则设置，再发布 Mihomo；有未保存修改时不能预览或应用网络。</p>
              <p v-else-if="status && !status.enabled_in_config" class="m-0 text-xs leading-relaxed" role="status"><span class="font-medium text-warning-accent">设备尚未检测到已部署的 Linux 网关配置。</span>请先发布并激活 Mihomo，再刷新检测。</p>
              <div class="flex flex-wrap gap-2"><Button variant="outline" class="min-h-11" :disabled="!canPreview" @click="previewNetwork"><LoaderCircle v-if="networkAction === 'preview'" class="size-4 animate-spin" /><Network v-else class="size-4" />预览并检查</Button><Button class="min-h-11" :disabled="!canApply" @click="applyNetwork"><LoaderCircle v-if="networkAction === 'apply'" class="size-4 animate-spin" />应用网关转发</Button></div>
            </template>
            <div v-if="networkError" role="alert" class="text-xs leading-relaxed"><p class="m-0 font-medium text-destructive-accent">需要处理</p><p class="mt-1 mb-0">{{ networkError }}</p></div>
            <p v-if="networkResult" role="status" class="m-0 flex items-start gap-2 text-xs leading-relaxed"><Check class="mt-0.5 size-3.5 shrink-0 text-success-accent" />{{ networkResult }}</p>
            <div v-if="preview" class="space-y-3 border-t border-border pt-3" role="status">
              <p class="m-0 text-sm font-semibold" :class="preview.ready ? 'text-success-accent' : 'text-destructive-accent'">{{ preview.ready ? '检查通过，可以应用' : '先解决以下问题，再重新检查' }}</p>
              <div v-if="preview.blockers?.length" class="text-xs leading-relaxed" role="alert"><p class="m-0 font-medium text-destructive-accent">阻断问题</p><ul class="mt-1 mb-0 list-disc space-y-1 pl-5"><li v-for="blocker in preview.blockers" :key="blocker">{{ blocker }}</li></ul></div>
              <div v-if="preview.warnings?.length" class="text-xs leading-relaxed"><p class="m-0 font-medium text-warning-accent">检查提醒</p><ul class="mt-1 mb-0 list-disc space-y-1 pl-5"><li v-for="warning in preview.warnings" :key="warning">{{ warning }}</li></ul></div>
              <details v-if="preview.commands?.length" class="text-xs">
                <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">查看网络操作预览</summary>
                <div class="space-y-1.5 pb-2">
                  <div class="flex min-h-8 items-center justify-between gap-2 max-md:min-h-11"><span class="font-medium">操作说明</span><Button variant="ghost" size="sm" class="px-2 text-xs" aria-label="复制操作说明" @click="copyCommands"><Copy class="size-3.5" aria-hidden="true" />复制</Button></div>
                  <pre class="m-0 max-h-64 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 font-mono text-xs leading-relaxed">{{ preview.commands.join('\n') }}</pre>
                  <p class="m-0 text-muted-foreground">Agent 将执行的操作说明，不是可粘贴到终端的 Shell 脚本。</p>
                </div>
              </details>
            </div>
            <p v-if="!legacyMode" class="m-0 text-xs leading-relaxed">应用成功后再修改客户端网关和 DNS。先记录原设置，以便恢复。</p>
            <p v-else class="m-0 text-xs leading-relaxed">客户端网关保持主路由，DNS 保持接入主路由时的设置，无需修改。</p>
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-border pt-3">
              <Button variant="outline" class="min-h-11" :disabled="busy || !online || !shell || !managedNetwork" @click="removeNetwork"><LoaderCircle v-if="networkAction === 'remove'" class="size-4 animate-spin" /><RotateCcw v-else class="size-3.5" />撤销网关转发</Button>
              <p v-if="managedNetwork" class="m-0 text-xs leading-relaxed text-muted-foreground">{{ legacyMode ? '撤销前，先删除主路由上指向这台 Linux 的静态路由。' : '恢复前，先恢复客户端原来的网关与 DNS。' }}</p>
            </div>
          </section>

          <div class="divide-y divide-border border-y border-border text-xs">
            <details v-if="splitMode" data-router-split-guide>
              <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">分流说明</summary>
              <ul class="mt-1 mb-3 list-disc space-y-2 pl-5 leading-relaxed text-muted-foreground">
                <li>主路由按静态路由把 fake-IP 网段 <code class="break-all font-mono">{{ fakeIPRange || '由 Mihomo 配置决定' }}</code> 和所选规则集的 IPv4 网段转给这台 Linux，其余流量由主路由直接转发。转发网段在「上游路由」管理。</li>
                <template v-if="legacyMode">
                  <li>旧版方案：Mihomo 靠这台 Linux 的网关转发与选择性 TUN 接管转来的连接，实际出口由规则顺序及策略组当前选择决定。</li>
                  <li>不要关闭 Linux 网关或撤销网关转发，否则会同时去掉 TUN 并还原 IPv4 转发，主路由转来的连接会中断。</li>
                </template>
                <template v-else-if="!invalidMode">
                  <li>Mihomo 使用 rule / fake-IP 模式和完整 TUN，接管主路由转来的全部连接；实际出口由规则顺序及策略组当前选择决定。</li>
                  <li>需要 Mihomo Agent {{ FORWARDING_AGENT_VERSION }} 或更新版本、root 和 TUN。IPv4 转发由 Agent 开启并持久化，不使用本页的 Linux 网关开关与网关转发。</li>
                </template>
              </ul>
            </details>
            <details v-else>
              <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">网关说明与安装要求</summary>
              <div class="space-y-3 pt-1 pb-3 leading-relaxed text-muted-foreground">
                <ul class="m-0 list-disc space-y-2 pl-5">
                  <li>仅支持 Linux + nftables 的 IPv4 网关。客户端需停用 IPv6，或另行配置 IPv6 路由；所有流量仍经过 Linux。</li>
                  <li>fake-IP 和所选真实 IP 进入 Mihomo；其余流量由 Linux 直连转发。Mihomo 使用 rule 模式，实际出口仍由规则顺序与策略组当前选择决定。</li>
                  <li>接管网段须为 /8 至 /32，拒绝 /0、/1 等过大范围。不要把全网或普通直连规则一并选入。</li>
                  <li>规则按 ID 关联，改名保留选择；内容和停用状态约每 {{ Math.ceil(refreshInterval / 60) }} 分钟刷新。新增选择需重新保存并发布。</li>
                  <li>在底部保存并发布到目标 Mihomo；显示「待激活」时还需应用并启动。网关配置会自动生成 TUN 设置，保留现有节点与策略。</li>
                </ul>
                <p class="m-0">首次使用可在「首次使用向导」选择「Linux 网关」，会新建配置空间；要沿用当前配置空间，到「Agent → 生成安装脚本」选择「Linux 网关」。两种方式都会安装双核心和 Agent，并完成转发设置；已有标准安装会先校验再接入。普通模式也可接入已有核心，或勾选安装核心。</p>
                <p class="m-0">需要 Mihomo 稳定版 v1.19.0+、Agent 1.8.25-go+、root、TUN 和 nftables，不支持通过 Docker 修改宿主机网络。ConfigFlow 服务地址需能被 Linux 访问；启用登录时需订阅令牌，供规则刷新。</p>
              </div>
            </details>
            <details>
              <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">DNS 配置说明</summary>
              <div class="space-y-3 pt-1 pb-3 leading-relaxed text-muted-foreground">
                <p class="m-0">在「OxiDNS → 设置」核对后，发布到同一台 Linux 的 OxiDNS。本页不会改写 OxiDNS；安装向导已准备的配置可直接核对。</p>
                <dl class="m-0 grid gap-x-4 gap-y-2 sm:grid-cols-[8rem_1fr]">
                  <dt class="font-medium text-foreground">客户端入口</dt><dd class="m-0"><code>0.0.0.0:53</code>，开启 UDP 和 TCP。</dd>
                  <dt class="font-medium text-foreground">直连上游</dt><dd class="m-0">使用带 IP 地址的 DoH / DoT，例如 <code>https://223.5.5.5/dns-query</code>；普通 UDP / TCP 53 可能被接管而回环。需要时填写拨号 IP / 引导 IP。</dd>
                  <dt class="font-medium text-foreground">代理与备用上游</dt><dd class="m-0">都使用 <code>127.0.0.1:1053</code>，避免失败后返回真实 IP 绕过接管。</dd>
                  <dt class="font-medium text-foreground">域名分流</dt><dd class="m-0">直连域名用本地上游；代理域名用远程上游获取 fake-IP。域名规则与上面的真实 IP 集合分别配置。</dd>
                </dl>
                <p class="m-0">Mihomo 的 DNS 上游不能再指回 OxiDNS；代理域名也不能被 fake-IP 过滤规则改为真实 IP。请确认对应策略组已选择可用代理节点。</p>
              </div>
            </details>
            <details v-if="splitMode" data-router-split-clients>
              <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">客户端接入与验证</summary>
              <ol class="mt-1 mb-3 list-decimal space-y-2 pl-5 leading-relaxed text-muted-foreground">
                <li>客户端网关保持主路由，无需改为 Linux；DNS 保持接入主路由时的设置，由主路由 DHCP 下发这台 Linux 上的 OxiDNS 地址。</li>
                <li>关闭客户端代理、浏览器安全 DNS 和系统私有 DNS，刷新 DNS 缓存或重新连接网络。应用自带 DoH / DoT，或主路由未转发的真实 IP，不会经过 Mihomo。</li>
                <li>分别测试直连和代理域名：直连返回真实 IP；代理返回 fake-IP，并在 Mihomo 连接列表确认出口。再测试所选规则集的真实 IP 服务是否命中预期策略。</li>
                <li>单设备通过后再接入其他设备。新增转发网段时，主路由的静态路由也需同步添加。</li>
              </ol>
            </details>
            <details v-else>
              <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">客户端接入与验证</summary>
              <ol class="mt-1 mb-3 list-decimal space-y-2 pl-5 leading-relaxed text-muted-foreground">
                <li>先记录一台测试设备原来的网关、DNS、IPv6 和安全 DNS 设置。将网关和 DNS 都设为 <code class="text-foreground">{{ network.address || 'Linux 的局域网 IP' }}</code>；Linux 自身仍以主路由 <code>{{ network.upstream_gateway || '的 IP' }}</code> 为默认网关。</li>
                <li>关闭客户端代理、IPv6、浏览器安全 DNS 和系统私有 DNS，刷新 DNS 缓存或重新连接网络。应用自带 DoH / DoT、缓存或未选中的真实 IP 不会自动代理。</li>
                <li>分别测试直连和代理域名：直连返回真实 IP；代理返回 fake-IP，并在 Mihomo 连接列表确认出口。再测试勾选的真实 IP 服务是否命中预期策略。</li>
                <li>单设备通过后再接入其他设备。主路由无需静态路由；支持 DHCP 分发网关和 DNS 时，可统一下发 Linux 地址，否则逐台设置。</li>
              </ol>
            </details>
            <details v-if="splitMode" data-router-split-stop>
              <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">停用和恢复步骤</summary>
              <div class="space-y-2 pt-1 pb-3 leading-relaxed text-muted-foreground">
                <p class="m-0">停用分流时，先删除主路由上指向这台 Linux 的静态路由，再把主路由 DHCP 的 DNS 改回原值；客户端重新连接网络后不再经过 Mihomo。</p>
                <p v-if="legacyMode" class="m-0">静态路由删除前，不要撤销网关转发，否则主路由转来的连接会中断。删除后再用上方按钮撤销网关转发；Linux 网关开关保持开启。</p>
                <p v-else-if="invalidMode" class="m-0">标记无效时已停止生成和网关操作，请先按上方提示恢复标记。</p>
                <p v-else class="m-0">完整 TUN 不使用本页的开关与网关转发，无需在这里撤销。Agent 持久化的 IPv4 转发与 Mihomo TUN 保持不变，核心也不会卸载。</p>
              </div>
            </details>
            <details v-else>
              <summary class="min-h-11 cursor-pointer content-center py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">停用和恢复步骤</summary>
              <div class="space-y-2 pt-1 pb-3 leading-relaxed text-muted-foreground"><p class="m-0">先恢复客户端原网关和 DNS（原先自动获取的改回自动获取），以及测试前的 IPv6、浏览器安全 DNS 和系统私有 DNS；再用上方按钮撤销网关转发。</p><p class="m-0">最后关闭 Linux 网关开关，保存并发布 Mihomo。撤销只处理 ConfigFlow 管理的网络参数，不卸载核心；撤销转发不会单独关闭 Mihomo TUN。</p><p v-if="!managedNetwork" class="m-0">当前设备没有已应用或未完成的 ConfigFlow 网关转发，或尚未完成状态检测。</p></div>
            </details>
          </div>
        </template>
      </div>
    </div>
    <template #footer="{ disabled }"><SettingsPublishBar :open="open" :profile-id="profileId" service-type="mihomo" :disabled="disabled || !!loadError || loading || fullTunMode || (configDirty && draftReview?.action !== 'save')" :save="save" @save-draft="saveDraft" /></template>
  </SettingsWorkspace>
</template>
