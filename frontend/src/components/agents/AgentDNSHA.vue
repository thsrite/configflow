<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { Loader2, RefreshCw } from '@lucide/vue'
import { agentApi } from '@/api'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { oxidnsHaCopy } from '@/lib/oxidnsHaCopy'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import FormField from '@/components/common/FormField.vue'
import AgentStatusRow from './AgentStatusRow.vue'
import AgentHAPublication from './AgentHAPublication.vue'
import { haPublicationFinished } from '@/types/agentHAPublication'
import type { Agent } from '@/types'
import { isDNSHAEnabled, type AgentDNSHAInput, type AgentDNSHAMember, type AgentDNSHARemoveResult, type AgentDNSHAStatus } from '@/types/agentDNSHA'

const props = withDefaults(defineProps<{ agent: Agent; agents: Agent[]; blocked?: boolean; showInstallGuide?: boolean }>(), { showInstallGuide: true })
const emit = defineEmits<{ changed: []; 'install-guide': [] }>()

const enabled = computed(() => isDNSHAEnabled(props.agent.oxidns_ha))
const config = computed(() => status.value?.config || (enabled.value ? props.agent.oxidns_ha! : null))
const haState = computed(() => props.agent.oxidns_ha_state || null)
// 首次开启失败且设备上的回滚未完成：记录里只有失败状态，停用用来清理残留配置。
const leftover = computed(() => !enabled.value && haState.value?.status === 'failed')
const peerAgent = computed(() => config.value ? props.agents.find(item => item.id === config.value!.peer_id) : undefined)

// 状态独立读取：只在开启时读，组配置、两台在线状态或操作状态变化才重读，心跳轮询不触发。
const status = ref<AgentDNSHAStatus | null>(null)
const statusLoading = ref(false)
const statusError = ref('')
const publication = computed(() => status.value?.publication || props.agent.oxidns_ha_publication)
const publishing = computed(() => !!publication.value && !haPublicationFinished(publication.value.status))
let pollTimer: ReturnType<typeof setTimeout> | undefined
let enablePublication = ''
const enableUnknown = ref(false)
const signature = computed(() => {
  const task = props.agent.oxidns_ha_publication
  const value = enabled.value ? props.agent.oxidns_ha! : null
  const auto = task?.auto_complete?.status || ''
  if (!value) return task ? `${task.id}|${task.status}|${auto}` : ''
  return [props.agent.id, value.group_id, value.peer_id, value.vip, value.vrid, value.role,
    props.agent.status, peerAgent.value?.status || '', haState.value?.status || '', haState.value?.updated_at || '', task?.id || '', task?.status || '', auto].join('|')
})
let generation = 0
let disposed = false
onUnmounted(() => { disposed = true; generation++; clearTimeout(pollTimer) })
const errorMessage = (failure: any) => failure?.response?.data?.message || failure?.response?.data?.error || failure?.message || '操作失败，请重试。'

async function readStatus() {
  clearTimeout(pollTimer)
  const current = ++generation
  statusLoading.value = true
  try {
    const { data } = await agentApi.getDNSHA(props.agent.id)
    if (disposed || current !== generation) return
    if (data?.success === false) throw new Error(data.message || '无法读取 DNS 高可用状态。')
    status.value = data
    statusError.value = ''
    if (data.publication && enableUnknown.value) {
      enablePublication = data.publication.id
      enableUnknown.value = false
    }
    if (enableUnknown.value && !data.publication) {
      enableUnknown.value = false
      error.value = '服务端未记录本次同步任务，请核对状态后重试。'
      notify.error('无法确认开启结果', error.value)
    }
    if (data.publication && data.publication.id === enablePublication && haPublicationFinished(data.publication.status)) {
      enablePublication = ''
      emit('changed')
      if (data.publication.status === 'succeeded' && data.config) enabledNotice(data)
      else notify.error('DNS 高可用未开启完成', data.publication.message || '主备同步失败，请查看发布记录。')
    }
  } catch (failure) {
    // 读取失败保留上次结果，只在展开处说明。
    if (!disposed && current === generation) statusError.value = errorMessage(failure)
  } finally {
    if (!disposed && current === generation) statusLoading.value = false
    // 自动补完开始时旧任务仍是 failed，继续读取直到新的主备发布出现。
    const autoStarting = publication.value?.auto_complete?.status === 'running'
    if (!disposed && current === generation && (publishing.value || enableUnknown.value || autoStarting)) {
      pollTimer = setTimeout(() => { void readStatus() }, 2000)
    }
  }
}
watch(signature, next => {
  if (next) { void readStatus(); return }
  if (publishing.value || enableUnknown.value) return
  generation++
  status.value = null
  statusError.value = ''
  statusLoading.value = false
}, { immediate: true })

const members = computed(() => status.value?.config ? status.value.members || [] : [])
const selfMember = computed(() => members.value.find(item => item.agent_id === props.agent.id))
const peerMember = computed(() => members.value.find(item => item.agent_id !== props.agent.id))
const peerName = computed(() => peerAgent.value?.name || peerMember.value?.name || config.value?.peer_id || '对端')
const holdsVip = computed(() => selfMember.value?.runtime?.vip_present === true)
const standby = computed(() => !holdsVip.value && selfMember.value?.runtime?.keepalived_running === true)
const bothHold = computed(() => members.value.length === 2 && members.value.every(item => item.runtime?.vip_present === true))
const health = computed(() => status.value?.config ? status.value.health : undefined)

const row = computed<{ status: string; tone: 'success' | 'warning' | 'danger' | 'progress' | 'neutral' }>(() => {
  if (publication.value?.status === 'ready') return { status: '主备配置待激活', tone: 'warning' }
  if (publication.value?.status === 'unknown') return { status: publication.value.phase === 'handoff' ? '虚拟 IP 迁移待确认' : '同步结果待确认', tone: 'warning' }
  if (publishing.value && publication.value?.phase === 'handoff') return { status: '正在迁移虚拟 IP', tone: 'progress' }
  if (publishing.value) return { status: '正在同步主备', tone: 'progress' }
  if (publication.value?.status === 'failed' && publication.value.auto_complete?.status === 'waiting') return { status: '等待设备恢复后自动完成发布', tone: 'warning' }
  if (publication.value?.status === 'failed' && publication.value.auto_complete?.status === 'running') return { status: '正在自动完成主备发布', tone: 'progress' }
  if (publication.value && publication.value.status !== 'succeeded') return { status: '主备同步失败', tone: 'danger' }
  if (haState.value?.status === 'applying') return { status: '正在开启', tone: 'progress' }
  if (haState.value?.status === 'failed') return { status: '操作失败', tone: 'danger' }
  if (!status.value) {
    if (statusError.value) return { status: '状态未知', tone: 'warning' }
    return { status: '正在读取', tone: 'progress' }
  }
  if (health.value === 'ok') return { status: '主备正常', tone: 'success' }
  if (health.value === 'degraded') return { status: '部分异常', tone: 'warning' }
  if (health.value === 'split') return { status: bothHold.value ? '虚拟 IP 冲突' : '虚拟 IP 未生效', tone: 'danger' }
  if (health.value === 'down') return { status: '不可用', tone: 'danger' }
  return { status: '已开启', tone: 'neutral' }
})

const memberLabel = (member: AgentDNSHAMember) => member.agent_id === props.agent.id ? '本机' : (member.name || '对端')
const problems = computed(() => {
  if (!status.value || health.value === 'ok') return []
  const lines: string[] = []
  if (health.value === 'split') {
    lines.push(bothHold.value
      ? '两台都持有虚拟 IP，请检查两台之间的 VRRP 通信是否被防火墙拦截。'
      : '两台都未持有虚拟 IP，设备无法通过虚拟 IP 解析。')
  }
  for (const member of members.value) {
    const label = memberLabel(member)
    if (!member.online) lines.push(`${label}离线`)
    else if (member.error) lines.push(`${label}：${member.error}`)
    else if (member.runtime && !member.runtime.keepalived_running) lines.push(`${label}的 keepalived 未运行`)
    else if (member.runtime && !member.runtime.healthy) lines.push(`${label}的 OxiDNS 无应答`)
  }
  return lines
})
const alerting = computed(() => row.value.tone === 'danger')

function runtimeText(member: AgentDNSHAMember) {
  if (!member.online) return '离线'
  if (!member.runtime) return member.error || '状态未知'
  const parts = [
    member.runtime.vip_present ? '持有虚拟 IP' : member.runtime.keepalived_running ? '待命' : '未参与',
    member.runtime.keepalived_running ? 'keepalived 运行中' : 'keepalived 未运行',
    member.runtime.healthy ? 'OxiDNS 应答正常' : 'OxiDNS 无应答',
  ]
  return parts.join(' · ')
}

// 对话框
const open = ref(false)
const action = ref<'enable' | 'disable' | 'force' | ''>('')
const error = ref('')
const canForce = ref(false)
const peer = ref('')
const vip = ref('')
const vrid = ref('')
const busy = computed(() => !!action.value || !!props.blocked || publishing.value || enableUnknown.value)
const candidates = computed(() => props.agents.filter(item => item.id !== props.agent.id
  && item.service_type === 'oxidns'
  && (item.profile_id || '') === (props.agent.profile_id || '')
  && !isDNSHAEnabled(item.oxidns_ha)))
const selectedPeer = computed(() => candidates.value.find(item => item.id === peer.value))
const includesDocker = computed(() => props.agent.deployment_method === 'docker' || selectedPeer.value?.deployment_method === 'docker')
const includesNative = computed(() => props.agent.deployment_method !== 'docker' || (!!selectedPeer.value && selectedPeer.value.deployment_method !== 'docker'))

const IPV4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/
const vipError = computed(() => {
  const value = vip.value.trim()
  if (!value) return ''
  if (!IPV4.test(value)) return '请填写 IPv4 地址，如 192.0.2.53。'
  const first = Number(value.split('.')[0])
  if (first === 0 || first === 127 || first >= 224) return '虚拟 IP 需为局域网单播地址。'
  if (value === props.agent.host || value === selectedPeer.value?.host) return '虚拟 IP 不能与两台 OxiDNS 的地址相同。'
  return ''
})
const vridError = computed(() => {
  const value = vrid.value.trim()
  if (!value) return ''
  return /^\d+$/.test(value) && Number(value) >= 1 && Number(value) <= 255 ? '' : 'VRID 范围为 1–255。'
})
const canEnable = computed(() => !enabled.value && !busy.value && !!selectedPeer.value
  && !!vip.value.trim() && !vipError.value && !vridError.value)

watch(open, value => {
  if (!value) return
  error.value = ''
  canForce.value = false
  if (!enabled.value) {
    if (!candidates.value.some(item => item.id === peer.value)) peer.value = ''
  }
})

async function enable() {
  if (!canEnable.value) return
  const peerAgentValue = selectedPeer.value!
  const address = vip.value.trim()
  const accepted = await confirm(
    `首次开启将把当前配置空间已保存的 OxiDNS 配置与规则同步到 ${peerAgentValue.name}（备机）和 ${props.agent.name}（主机），备机检查通过后更新主机，再启用虚拟 IP ${address}。后续配置发布会先更新未持有虚拟 IP 的设备，将虚拟 IP 迁移到该设备并确认解析正常后，再更新原持有设备。完成后需到上游路由页重新发布 DHCP DNS 与 DNS 劫持。`,
    { title: '开启 DNS 高可用', confirmText: '开启' },
  )
  if (!accepted || disposed) return
  action.value = 'enable'
  error.value = ''
  try {
    const input: AgentDNSHAInput = { peer_id: peerAgentValue.id, vip: address, ...(vrid.value.trim() ? { vrid: Number(vrid.value.trim()) } : {}) }
    const { data } = await agentApi.setDNSHA(props.agent.id, input)
    if (disposed) return
    if (data?.success === false) throw new Error(data.message || '开启未完成。')
    status.value = data
    emit('changed')
    open.value = false
    if (data.publication && !haPublicationFinished(data.publication.status)) {
      enablePublication = data.publication.id
      void readStatus()
    } else if (data.publication && data.publication.status !== 'succeeded') {
      notify.error('DNS 高可用未开启完成', data.publication.message || '主备同步失败，请查看发布记录。')
    } else enabledNotice(data)
  } catch (failure: any) {
    if (disposed) return
    error.value = errorMessage(failure)
    if (!failure?.response || failure.response.status >= 500) {
      enableUnknown.value = true
      error.value = '开启结果待确认，正在读取服务端状态。'
      void readStatus()
    } else notify.error('无法开启 DNS 高可用', error.value)
  } finally {
    if (!disposed) action.value = ''
  }
}

function enabledNotice(data: AgentDNSHAStatus) {
  const republish = '请到上游路由页重新发布，让 DHCP DNS 与 DNS 劫持改用虚拟 IP。'
  const warnings = (data.warnings || []).filter(item => typeof item === 'string' && item)
  if (warnings.length) notify.warning('已开启 DNS 高可用，请确认以下事项', [...warnings, republish].join('\n'))
  else notify.success('已开启 DNS 高可用', republish)
}

function routerNames(result: AgentDNSHARemoveResult) {
  return (result?.routers || []).map(item => typeof item === 'string' ? item : item?.name || item?.id || '').filter(Boolean)
}

const manualCleanup = '需在该主机上手动移除 /etc/keepalived/configflow-oxidns.conf 及主配置中的 CONFIGFLOW OXIDNS-HA 标记块。'
function disablePrompt(force: boolean) {
  if (!config.value) {
    return force
      ? confirmDanger(`只清除 ConfigFlow 中的失败记录。无法连接的 Agent 上可能仍保留 keepalived 配置，${manualCleanup}`,
        { title: '强制清除失败记录', confirmText: '强制清除' })
      : confirmDanger('上次开启失败时，部分 Agent 上可能仍保留 keepalived 配置。将移除这些由 ConfigFlow 写入的配置，并恢复 keepalived 原来的运行状态。',
        { title: '清除残留配置', confirmText: '清除' })
  }
  return force
    ? confirmDanger(`只清除 ConfigFlow 中的高可用记录。无法连接的 Agent 上可能仍保留 keepalived 配置和虚拟 IP ${config.value.vip}，${manualCleanup}`,
      { title: '强制清除高可用记录', confirmText: '强制清除' })
    : confirmDanger(`将移除 ${props.agent.name} 和 ${peerName.value} 上由 ConfigFlow 写入的 keepalived 配置，虚拟 IP ${config.value.vip} 随之下线。仍在下发虚拟 IP 的路由器需重新发布，否则设备将无法解析。`,
      { title: '停用 DNS 高可用', confirmText: '停用' })
}

async function disable(force: boolean) {
  if (busy.value || (!config.value && !leftover.value)) return
  const cleanup = !config.value
  const accepted = await disablePrompt(force)
  if (!accepted || disposed) return
  action.value = force ? 'force' : 'disable'
  error.value = ''
  try {
    const { data } = force ? await agentApi.removeDNSHA(props.agent.id, true) : await agentApi.removeDNSHA(props.agent.id)
    if (disposed) return
    if (data?.success === false) throw new Error(data.message || '停用未完成。')
    generation++
    status.value = null
    statusError.value = ''
    canForce.value = false
    emit('changed')
    open.value = false
    const names = routerNames(data)
    if (cleanup) notify.success('已清除残留配置')
    else if (names.length) notify.warning('已停用 DNS 高可用', `请到上游路由页重新发布：${names.join('、')}`)
    else notify.success('已停用 DNS 高可用')
  } catch (failure) {
    if (disposed) return
    error.value = errorMessage(failure)
    canForce.value = true
    notify.error(cleanup ? '无法清除残留配置' : '无法停用 DNS 高可用', error.value)
  } finally {
    if (!disposed) action.value = ''
  }
}
</script>

<template>
  <section class="min-w-0" data-agent-dns-ha>
    <AgentStatusRow v-if="config" label="DNS 高可用" :status="row.status" :tone="row.tone">
      <template #actions>
        <Button variant="ghost" size="sm" class="min-h-11 text-xs" :disabled="!!action" aria-label="设置 DNS 高可用" @click="open = true">设置</Button>
      </template>
      <template #notice>
        <AgentHAPublication v-if="publication && (publishing || publication.status !== 'succeeded' || publication.auto_complete?.attempt)" :publication="publication" />
        <div class="flex min-w-0 flex-wrap items-center gap-1.5 text-muted-foreground">
          <span v-if="holdsVip" class="chip chip-ok shrink-0" data-dns-ha-holder>主 · VIP <span class="font-mono">{{ config.vip }}</span></span>
          <span v-else-if="standby" class="chip shrink-0">备</span>
          <span class="min-w-0 [overflow-wrap:anywhere]">与 {{ peerName }} 共用虚拟 IP<span v-if="!holdsVip" class="ml-1 font-mono">{{ config.vip }}</span></span>
        </div>
        <p v-if="haState?.status === 'failed' && haState.error" role="alert" class="m-0 whitespace-pre-wrap text-destructive-accent">{{ haState.error }}</p>
        <p v-else-if="haState?.error" class="m-0 whitespace-pre-wrap text-warning-accent" data-dns-ha-last-error>{{ haState.error }}</p>
        <ul v-if="problems.length" :role="alerting ? 'alert' : undefined" class="m-0 list-none space-y-1 p-0" :class="alerting ? 'text-destructive-accent' : 'text-warning-accent'" data-dns-ha-problems>
          <li v-for="line in problems" :key="line">{{ line }}</li>
        </ul>
        <p v-if="!status && statusError" class="m-0 text-warning-accent">无法读取高可用状态：{{ statusError }}</p>
      </template>
      <div class="min-w-0 space-y-2">
        <dl class="m-0 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1">
          <dt>虚拟 IP</dt><dd class="m-0 font-mono">{{ config.vip }}</dd>
          <dt>VRID</dt><dd class="m-0 font-mono">{{ config.vrid }}</dd>
          <dt>配置角色</dt><dd class="m-0">{{ config.role === 'primary' ? '主机（优先持有虚拟 IP）' : '备机' }}</dd>
        </dl>
        <ul v-if="members.length" class="m-0 list-none space-y-1 p-0">
          <li v-for="member in members" :key="member.agent_id" class="[overflow-wrap:anywhere]">
            <span class="text-foreground">{{ member.name || member.agent_id }}</span>
            <span class="font-mono"> {{ member.host }}</span>
            · {{ member.role_config === 'primary' ? '配置主机' : '配置备机' }} · {{ runtimeText(member) }}
          </li>
        </ul>
        <p v-if="status && statusError" class="m-0 text-warning-accent">刷新失败，显示上次结果：{{ statusError }}</p>
        <Button variant="outline" size="sm" class="min-h-11" :disabled="statusLoading" @click="readStatus">
          <Loader2 v-if="statusLoading" class="size-3.5 animate-spin" /><RefreshCw v-else class="size-3.5" />{{ statusLoading ? '正在读取…' : '刷新状态' }}
        </Button>
      </div>
    </AgentStatusRow>
    <AgentStatusRow v-else-if="publication || enableUnknown" label="DNS 高可用"
      :status="enableUnknown ? '开启结果待确认' : row.status"
      :tone="enableUnknown ? 'warning' : row.tone">
      <template #actions><Button variant="ghost" size="sm" class="min-h-11 text-xs" :disabled="busy" aria-label="设置 DNS 高可用" @click="open = true">设置</Button></template>
      <template #notice>
        <AgentHAPublication v-if="publication" :publication="publication" />
        <p v-if="statusError" class="m-0 text-warning-accent">刷新失败，正在重试：{{ statusError }}</p>
      </template>
    </AgentStatusRow>
    <AgentStatusRow v-else label="DNS 高可用" status="未开启" tone="neutral">
      <template #actions>
        <Button variant="ghost" size="sm" class="min-h-11 text-xs" aria-label="设置 DNS 高可用" @click="open = true">设置</Button>
      </template>
      <template v-if="leftover" #notice>
        <p class="m-0 whitespace-pre-wrap text-warning-accent" data-dns-ha-leftover>上次开启失败<template v-if="haState?.error">：{{ haState.error }}</template>。设备上可能仍有残留配置，可在「设置」中清除。</p>
      </template>
    </AgentStatusRow>

    <Dialog :open="open" @update:open="value => { if (!action) open = value }">
      <DialogContent class="max-w-[560px]">
        <DialogHeader>
          <DialogTitle>DNS 高可用</DialogTitle>
          <DialogDescription v-if="config">{{ agent.name }} 与 {{ peerName }} 共用虚拟 IP {{ config.vip }}，一台故障时由另一台继续应答。</DialogDescription>
          <DialogDescription v-else>{{ agent.name }} 与另一台 OxiDNS 共用一个虚拟 IP，一台故障时由另一台继续应答。</DialogDescription>
        </DialogHeader>
        <dl v-if="config" class="m-0 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-sm">
          <dt class="text-muted-foreground">配置主机</dt><dd class="m-0 [overflow-wrap:anywhere]">{{ config.role === 'primary' ? agent.name : peerName }}</dd>
          <dt class="text-muted-foreground">配置备机</dt><dd class="m-0 [overflow-wrap:anywhere]">{{ config.role === 'primary' ? peerName : agent.name }}</dd>
          <dt class="text-muted-foreground">虚拟 IP</dt><dd class="m-0 font-mono">{{ config.vip }}</dd>
          <dt class="text-muted-foreground">VRID</dt><dd class="m-0 font-mono">{{ config.vrid }}</dd>
        </dl>
        <p v-if="config" class="m-0 text-xs text-muted-foreground">当前主备以实际虚拟 IP 持有情况为准。如需更换对端或虚拟 IP，请先停用后重新开启。</p>
        <form v-else :id="`dns-ha-form-${agent.id}`" class="min-w-0 space-y-4" @submit.prevent="enable">
          <fieldset :disabled="busy" class="m-0 min-w-0 space-y-4 border-0 p-0">
            <FormField label="备机" :html-for="`dns-ha-peer-${agent.id}`" v-bind="oxidnsHaCopy.peer">
              <Select v-model="peer" :disabled="busy">
                <SelectTrigger :id="`dns-ha-peer-${agent.id}`" class="min-h-11 w-full" aria-label="备机"><SelectValue placeholder="选择另一台 OxiDNS" /></SelectTrigger>
                <SelectContent class="max-w-(--reka-select-content-available-width)">
                  <SelectItem v-for="item in candidates" :key="item.id" :value="item.id" class="min-h-11 whitespace-normal [overflow-wrap:anywhere]">{{ item.name }}（{{ item.host }}）</SelectItem>
                </SelectContent>
              </Select>
              <p v-if="!candidates.length" class="m-0 text-xs text-warning-accent">同一配置空间内没有可用的另一台 OxiDNS（需尚未加入其他高可用组）。</p>
            </FormField>
            <FormField label="虚拟 IP" :html-for="`dns-ha-vip-${agent.id}`" v-bind="oxidnsHaCopy.vip">
              <Input :id="`dns-ha-vip-${agent.id}`" v-model="vip" class="min-h-11 font-mono" placeholder="192.0.2.53" autocomplete="off" spellcheck="false" inputmode="decimal" :aria-invalid="!!vipError || undefined" />
              <p v-if="vipError" class="m-0 text-xs text-destructive-accent">{{ vipError }}</p>
            </FormField>
            <details class="rounded-md border border-border text-xs text-muted-foreground" data-dns-ha-advanced>
              <summary class="min-h-11 cursor-pointer px-3 py-3 font-medium text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">高级选项</summary>
              <div class="border-t border-border p-3">
                <FormField label="VRID" :html-for="`dns-ha-vrid-${agent.id}`" v-bind="oxidnsHaCopy.vrid">
                  <Input :id="`dns-ha-vrid-${agent.id}`" v-model="vrid" class="min-h-11 font-mono" placeholder="自动" autocomplete="off" inputmode="numeric" :aria-invalid="!!vridError || undefined" />
                  <p v-if="vridError" class="m-0 text-xs text-destructive-accent">{{ vridError }}</p>
                </FormField>
              </div>
            </details>
          </fieldset>
        </form>
        <p v-if="leftover" class="m-0 rounded-md border border-border p-3 text-xs leading-relaxed text-warning-accent" data-dns-ha-leftover-hint>上次开启失败，部分 Agent 上可能仍保留 keepalived 配置。请先清除残留配置，再重新开启。</p>
        <p v-if="config" class="m-0 text-xs leading-relaxed text-muted-foreground">停用后虚拟 IP 下线，请到上游路由页重新发布 DHCP DNS 与 DNS 劫持。</p>
        <div v-else class="rounded-md border border-border p-3 text-xs leading-relaxed text-muted-foreground">
          <p class="m-0 font-medium text-foreground">开启前请确认</p>
          <ul class="m-0 mt-1.5 list-disc space-y-1 pl-4">
            <li>开启时会把本配置空间已保存的配置与规则发布到两台；备机检查成功后才更新主机，任一步失败都会停止后续步骤。</li>
            <li>后续配置发布先更新未持有虚拟 IP 的设备，迁移虚拟 IP 并确认解析正常后，再更新原持有设备。</li>
            <li>规则自动同步开关以本次主机设置为准；开启后修改任一台的开关都会同时作用于两台。</li>
            <li>两台位于不同宿主机、同一局域网（同一网段），虚拟 IP 未被其他设备或 DHCP 地址池占用，Agent 已更新到支持高可用的版本。</li>
            <li v-if="includesNative">Shell 成员的高可用安装命令会自动准备 keepalived 等依赖；已有设备缺少依赖时，可从安装引导补齐。</li>
            <li v-if="includesDocker">Docker 成员需使用专用高可用安装：Host 配合独立高可用容器，或使用 Macvlan，并通过网络、权限和持久化检查。普通容器不能只点开关启用。</li>
            <li>两台 OxiDNS 转发到同一个 Mihomo：代理 DNS 上游填 Mihomo 的局域网地址，不能用 127.0.0.1。</li>
            <li>开启或停用后，到上游路由页重新发布 DHCP DNS 与 DNS 劫持。</li>
          </ul>
          <Button v-if="showInstallGuide" variant="outline" class="mt-3 min-h-11" :disabled="busy" @click="open = false; emit('install-guide')">打开高可用安装引导</Button>
        </div>
        <p v-if="error" role="alert" class="m-0 whitespace-pre-wrap text-sm text-destructive-accent [overflow-wrap:anywhere]">{{ error }}</p>
        <DialogFooter class="flex-wrap gap-2">
          <Button v-if="config" variant="outline" class="min-h-11 sm:mr-auto" :disabled="busy" @click="disable(false)"><Loader2 v-if="action === 'disable'" class="size-3.5 animate-spin" />停用</Button>
          <Button v-else-if="leftover" variant="outline" class="min-h-11 sm:mr-auto" :disabled="busy" @click="disable(false)"><Loader2 v-if="action === 'disable'" class="size-3.5 animate-spin" />清除残留配置</Button>
          <Button v-if="(config || leftover) && canForce" variant="outline" class="min-h-11" :disabled="busy" @click="disable(true)"><Loader2 v-if="action === 'force'" class="size-3.5 animate-spin" />强制清除记录</Button>
          <Button variant="outline" class="min-h-11" :disabled="!!action" @click="open = false">关闭</Button>
          <Button v-if="!config" :form="`dns-ha-form-${agent.id}`" type="submit" class="min-h-11" :disabled="!canEnable"><Loader2 v-if="action === 'enable'" class="size-3.5 animate-spin" />{{ action === 'enable' ? '正在开启…' : '开启' }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </section>
</template>
