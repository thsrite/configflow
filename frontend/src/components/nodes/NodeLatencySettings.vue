<template>
  <Button variant="outline" class="border-border/60 bg-background/40 max-sm:size-11 max-sm:px-0" aria-label="测速设置" title="测速设置" :disabled="disabled" @click="openSettings">
    <Settings2 class="size-4" /><span class="max-sm:hidden">测速设置</span>
  </Button>
  <Dialog :open="open" @update:open="value => { if (!busy) open = value }">
    <DialogContent class="flex max-w-xl flex-col gap-0 overflow-hidden p-0 max-sm:p-0" :show-close-button="!busy" @escape-key-down="event => { if (busy) event.preventDefault() }" @interact-outside="event => { if (busy) event.preventDefault() }">
      <div class="shrink-0 space-y-3 border-b border-border px-6 pb-4 pt-6 max-sm:px-4 max-sm:pt-4">
        <DialogHeader>
          <DialogTitle>测速设置</DialogTitle>
          <DialogDescription>适用于全部配置空间的节点测速。</DialogDescription>
        </DialogHeader>
        <Segmented v-if="loaded" v-model="mode" :options="modes" label="测速 DNS 模式" block :disabled="busy || disabled" class="[&_[data-segment]]:px-2" />
        <p v-if="error" role="alert" class="m-0 break-words text-sm text-destructive-accent">{{ error }}</p>
      </div>
      <p v-if="loading" role="status" class="m-0 p-6 text-sm text-muted-foreground max-sm:p-4">正在读取测速设置…</p>
      <form v-else-if="loaded" ref="settingsForm" class="flex min-h-0 min-w-0 flex-col" @submit.prevent="save">
        <div class="flex min-h-0 flex-col gap-4 overflow-y-auto overscroll-contain px-6 py-4 max-sm:px-4">
        <p v-if="mode === 'system'" class="m-0 text-sm leading-relaxed text-muted-foreground">使用 ConfigFlow 所在服务器或容器的 DNS。</p>
        <template v-else-if="mode === 'custom'">
          <FormField label="自定义 DNS" html-for="node-latency-dns" hint="每行一个地址，需返回真实 IP。支持 IPv4[:端口]、DoH 和 DoT。" :help="optionHelp('已有可用 DNS，或希望自行管理加密 DNS 时使用。', 'DoH 使用 https://主机/路径，DoT 使用 tls://主机[:端口]。不支持账号、查询参数和片段。配置仅供节点测速使用。')" persistent-hint>
            <Textarea id="node-latency-dns" v-model="servers" class="min-h-24 font-mono text-sm" :disabled="busy || disabled" :rows="3" placeholder="192.0.2.53:15353&#10;https://dns.example/dns-query&#10;tls://dns.example" spellcheck="false" />
          </FormField>
          <FormField v-if="bootstrapRequired || bootstrap.trim()" label="引导 DNS" html-for="node-latency-bootstrap" class="[&_p]:break-words" :hint="bootstrapHint(customDnsHostnames, false)" persistent-hint>
            <Textarea id="node-latency-bootstrap" v-model="bootstrap" class="min-h-20 font-mono text-sm" :disabled="busy || disabled" :rows="2" placeholder="IPv4 或 IPv4:端口，每行一个" spellcheck="false" />
          </FormField>
        </template>
        <div v-else class="flex min-w-0 flex-col gap-4">
          <p class="m-0 text-sm leading-relaxed text-muted-foreground">专用入口把节点域名查询交给所选上游。</p>
          <p v-if="managedBindingError" role="alert" class="m-0 break-words text-sm text-warning-accent">{{ managedBindingError }}</p>
          <p v-if="managedLoading" role="status" class="m-0 text-sm text-muted-foreground">正在读取 OxiDNS 设备…</p>
          <div v-if="managedError" class="space-y-2">
            <p role="alert" class="m-0 break-words text-sm text-destructive-accent">{{ managedError }}</p>
            <Button type="button" variant="outline" class="min-h-11" :disabled="managedLoading || busy" @click="loadManaged">重新读取设备</Button>
          </div>
          <template v-if="!managedLoading && !managedError">
            <FormField label="OxiDNS 设备" html-for="node-latency-agent" :hint="selectedAgent ? `配置空间：${selectedAgent.profile_name || selectedAgent.profile_id}` : undefined" persistent-hint>
              <Select v-model="agentId" :disabled="busy || disabled">
                <SelectTrigger id="node-latency-agent" class="min-h-11 w-full min-w-0"><SelectValue placeholder="选择运行 OxiDNS 的设备" class="truncate" /></SelectTrigger>
                <SelectContent><SelectItem v-for="agent in agents" :key="agent.id" :value="agent.id">{{ agent.name }}</SelectItem></SelectContent>
              </Select>
            </FormField>
            <p v-if="!agents.length" class="m-0 text-sm text-muted-foreground">暂无 OxiDNS 设备，请先在 Agent 中添加并发布 OxiDNS。</p>
            <template v-if="selectedAgent">
              <p v-if="!agentAvailable" role="alert" class="m-0 text-sm text-warning-accent">{{ selectedAgent.reason || '该设备当前不可创建专用 DNS。' }}</p>
              <FormField v-if="agentEndpoints.length" label="专用 DNS 入口" html-for="node-latency-endpoint">
                <Select v-model="endpointId" :disabled="busy || disabled">
                  <SelectTrigger id="node-latency-endpoint" class="min-h-11 w-full min-w-0"><SelectValue placeholder="选择入口" class="truncate" /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__new__">创建新入口</SelectItem>
                    <SelectItem v-for="endpoint in agentEndpoints" :key="endpoint.endpoint_id" :value="endpoint.endpoint_id">{{ endpoint.endpoint || endpoint.endpoint_id }} · {{ statusLabel(endpoint.status) }}</SelectItem>
                  </SelectContent>
                </Select>
              </FormField>
              <div v-if="selectedEndpoint" class="space-y-2 border-t border-border pt-3">
                <p role="status" class="m-0 break-words text-sm" :class="statusClass(selectedEndpoint.status)">{{ statusLabel(selectedEndpoint.status) }}<span v-if="selectedEndpoint.endpoint" class="font-mono"> · {{ selectedEndpoint.endpoint }}</span></p>
                <p v-if="selectedEndpoint.message" class="m-0 break-words text-xs leading-relaxed text-muted-foreground">{{ selectedEndpoint.message }}</p>
                <p v-if="selectedEndpoint.upstream" class="m-0 break-all text-xs text-muted-foreground">上游：{{ selectedEndpoint.upstream }}</p>
                <div class="flex flex-wrap gap-2">
                  <Button type="button" variant="outline" class="min-h-11" :disabled="busy || statusLoading" @click="refreshEndpoint()"><Loader2 v-if="statusLoading" class="size-4 animate-spin" />刷新状态</Button>
                  <Button type="button" variant="ghost" class="min-h-11 text-destructive-accent" :disabled="busy || disabled" @click="removeEndpoint">移除入口</Button>
                </div>
              </div>
              <template v-else>
                <div role="radiogroup" aria-label="上游来源" class="flex min-w-0 gap-4 border-b border-border">
                  <button v-for="source in upstreamModes" :key="source.value" type="button" role="radio" :aria-checked="upstreamMode === source.value" :tabindex="upstreamMode === source.value ? 0 : -1" class="min-h-11 border-b-2 px-1 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50" :class="upstreamMode === source.value ? 'border-primary text-primary-accent' : 'border-transparent text-muted-foreground hover:text-foreground'" :disabled="busy || disabled" @click="upstreamMode = source.value" @keydown="changeUpstreamSource($event, source.value)">{{ source.label }}</button>
                </div>
                <template v-if="upstreamMode === 'existing'">
                  <FormField label="现有上游" html-for="node-latency-existing-upstream" :help="optionHelp('选择此设备已发布配置中的上游，免于手动填写。', '创建时复制上游参数并独立保存；原上游后续修改不会自动同步。列表中的上游尚未验证，创建后通过专用入口查询，验证返回真实 IP 才启用。')">
                    <div class="flex min-w-0 items-start gap-2">
                      <Select v-if="availableUpstreams.length" v-model="upstreamId" :disabled="busy || disabled">
                        <SelectTrigger id="node-latency-existing-upstream" class="min-h-11 min-w-0 flex-1" :title="selectedUpstream?.label"><SelectValue placeholder="选择要使用的上游" class="truncate"><span v-if="selectedUpstream">{{ selectedUpstream.address || selectedUpstream.label }}</span></SelectValue></SelectTrigger>
                        <SelectContent><SelectItem v-for="candidate in availableUpstreams" :key="candidate.id" :value="candidate.id" :disabled="!candidate.eligible" class="max-w-[calc(100vw-4rem)] whitespace-normal break-all"><span class="min-w-0"><span class="block">{{ candidate.label }}</span><span v-if="!candidate.eligible && candidate.reason" class="block text-xs text-muted-foreground">{{ candidate.reason }}</span></span></SelectItem></SelectContent>
                      </Select>
                      <p v-else class="m-0 min-w-0 flex-1 py-3 text-sm text-muted-foreground">暂无可选择的上游</p>
                      <Button type="button" variant="outline" class="size-11 shrink-0 p-0" aria-label="刷新上游" title="刷新上游" :disabled="busy || managedLoading || disabled" @click="loadManaged"><RefreshCw class="size-4" /><span class="sr-only">刷新上游</span></Button>
                    </div>
                    <p v-if="!eligibleUpstreams.length" role="status" class="m-0 text-sm text-warning-accent">{{ unavailableUpstreamsReason }}</p>
                  </FormField>
                </template>
                <template v-else>
                  <FormField label="上游 DNS" html-for="node-latency-upstream" hint="支持 IPv4[:端口]、https://主机/路径 或 tls://主机[:端口]。" persistent-hint><Input id="node-latency-upstream" v-model="upstream" class="min-h-11 font-mono" :disabled="busy || disabled" placeholder="填写 DNS 地址" /></FormField>
                  <FormField v-if="managedBootstrapRequired || managedBootstrap.trim()" label="引导 DNS" html-for="node-latency-managed-bootstrap" class="[&_p]:break-words" :hint="bootstrapHint(managedDnsHostnames, true)" persistent-hint><Input id="node-latency-managed-bootstrap" v-model="managedBootstrap" class="min-h-11 font-mono" :disabled="busy || disabled" placeholder="IPv4 或 IPv4:端口" /></FormField>
                </template>
                <details :open="listenerExpanded" class="border-t border-border">
                  <summary class="flex min-h-11 cursor-pointer flex-wrap items-center gap-x-2 gap-y-1 py-2 text-sm marker:content-none" @click.prevent="listenerExpanded = !listenerExpanded"><ChevronRight class="size-4 shrink-0 transition-transform" :class="{ 'rotate-90': listenerExpanded }" /><span>入口地址与端口</span><span class="min-w-0 break-all font-mono text-xs text-muted-foreground">{{ listenAddress.trim() || '未填写地址' }}:{{ port || '未填写端口' }}</span></summary>
                  <div class="space-y-1 text-xs leading-relaxed text-muted-foreground">
                    <p class="m-0">测速时，ConfigFlow 通过这个地址，让 OxiDNS 查询节点的真实 IP。</p>
                    <p class="m-0">IP 地址：运行 OxiDNS 的那台设备的 IP。</p>
                    <p class="m-0">端口：一般保持默认 <span class="font-mono">15353</span>，被占用时再换。</p>
                  </div>
                  <div class="grid min-w-0 grid-cols-[minmax(0,1fr)_6rem] gap-3 pb-1 pt-2">
                    <FormField label="监听 IPv4" html-for="node-latency-listen"><Input id="node-latency-listen" v-model="listenAddress" class="min-h-11 font-mono" :disabled="busy || disabled" placeholder="设备可访问的 IPv4" /></FormField>
                    <FormField label="端口" html-for="node-latency-port"><Input id="node-latency-port" v-model="port" class="min-h-11 font-mono" :disabled="busy || disabled" inputmode="numeric" /></FormField>
                  </div>
                </details>
              </template>
            </template>
            <p v-else-if="agentId" role="alert" class="m-0 text-sm text-warning-accent">已保存的设备不在可用列表中，请重新选择设备或切换模式。</p>
          </template>
        </div>
        <div v-if="mode !== 'managed' || selectedEndpoint?.status === 'ready'" class="flex flex-col gap-3 border-t border-border pt-3">
          <div class="flex min-w-0 items-center justify-between gap-3">
            <span class="text-sm font-medium">DNS 检测</span>
            <Button type="button" variant="outline" class="min-h-11 shrink-0" :aria-label="checking ? '正在检测 DNS…' : '检测 DNS'" :disabled="busy || checking || disabled" @click="checkDns"><Loader2 v-if="checking" class="size-4 animate-spin" />{{ checking ? '检测中…' : '检测' }}</Button>
          </div>
          <div v-if="checkResult" role="status" aria-label="DNS 检测结果" class="flex min-w-0 items-start gap-2">
            <CircleCheck v-if="checkResult.success" class="mt-0.5 size-4 shrink-0 text-success-accent" aria-hidden="true" />
            <CircleAlert v-else class="mt-0.5 size-4 shrink-0" :class="checkReturnsFakeIp ? 'text-warning-accent' : 'text-destructive-accent'" aria-hidden="true" />
            <div class="flex min-w-0 flex-col gap-1">
              <p class="m-0 text-sm font-medium" :class="checkResult.success ? 'text-success-accent' : checkReturnsFakeIp ? 'text-warning-accent' : 'text-destructive-accent'">{{ checkResult.success ? '解析成功' : checkReturnsFakeIp ? '返回 Fake-IP' : '检测失败' }}<span v-if="checkResult.success && checkResult.latency_ms != null" class="ml-2 text-xs font-normal text-muted-foreground">{{ checkResult.latency_ms }} ms</span></p>
              <p v-if="checkResult.success" class="m-0 break-all font-mono text-xs leading-relaxed text-muted-foreground">{{ checkResult.addresses.join('、') }}</p>
              <p v-else-if="checkReturnsFakeIp" class="m-0 break-words text-xs leading-relaxed text-muted-foreground">{{ mode === 'system' ? '请切换为 OxiDNS 托管或自定义 DNS，以获取真实 IP。' : mode === 'custom' ? '请检查自定义 DNS 是否返回真实 IP，或切换为 OxiDNS 托管。' : '请检查托管入口的上游是否返回真实 IP，或切换为自定义 DNS。' }}</p>
              <p v-else class="m-0 break-words text-xs leading-relaxed text-muted-foreground">{{ checkResult.message || '未能完成 DNS 查询，请重试。' }}</p>
            </div>
          </div>
        </div>
        </div>
        <div class="flex shrink-0 flex-col gap-4 border-t border-border px-6 py-4 max-sm:px-4">
          <p v-if="mode === 'managed' && selectedAgent && !selectedEndpoint && !managedLoading && !managedError" class="m-0 text-xs leading-relaxed text-warning-accent">将发布到「{{ selectedAgent.name }}」（{{ selectedAgent.profile_name || selectedAgent.profile_id }}）。验证返回真实 IP 后启用；失败保留原设置。</p>
        <DialogFooter class="flex-row gap-2">
          <Button type="button" variant="outline" class="min-h-11" :disabled="busy" @click="open = false">取消</Button>
          <Button v-if="mode === 'managed' && !selectedEndpoint" type="button" class="min-h-11 max-sm:flex-1" :disabled="busy || disabled || !agentAvailable || managedLoading || !!managedError || (upstreamMode === 'existing' && !selectedUpstream)" @click="createAndEnable"><Loader2 v-if="provisioning" class="size-4 animate-spin" />{{ provisioning ? '正在创建并验证…' : '创建并启用' }}</Button>
          <Button v-else type="submit" class="min-h-11 max-sm:flex-1" :disabled="busy || disabled || (mode === 'managed' && selectedEndpoint?.status !== 'ready')"><Loader2 v-if="saving || provisioning" class="size-4 animate-spin" />{{ saving ? '正在保存…' : provisioning ? '正在创建并验证…' : '保存' }}</Button>
        </DialogFooter>
        </div>
      </form>
      <div v-else class="p-6 max-sm:p-4"><Button variant="outline" class="min-h-11" @click="load">重新读取</Button></div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { ChevronRight, CircleAlert, CircleCheck, Loader2, RefreshCw, Settings2 } from '@lucide/vue'
import { nodeApi, type NodeLatencyDnsCheck, type NodeLatencyDnsConfig, type NodeLatencyDnsMode, type NodeLatencyManagedAgent, type NodeLatencyManagedEndpoint } from '@/api'
import FormField from '@/components/common/FormField.vue'
import Segmented from '@/components/common/Segmented.vue'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { confirmDanger, notify } from '@/lib/feedback'
import { optionHelp } from '@/lib/optionCopy'

const props = defineProps<{ disabled?: boolean }>()
const emit = defineEmits<{ saved: [] }>()
const modes: Array<{ value: NodeLatencyDnsMode; label: string }> = [{ value: 'system', label: '系统 DNS' }, { value: 'managed', label: 'OxiDNS 托管' }, { value: 'custom', label: '自定义 DNS' }]
const open = ref(false)
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const provisioning = ref(false)
const removing = ref(false)
const busy = computed(() => saving.value || provisioning.value || removing.value)
const mode = ref<NodeLatencyDnsMode>('system')
const servers = ref('')
const bootstrap = ref('')
const error = ref('')
const checking = ref(false)
const checkResult = ref<NodeLatencyDnsCheck | null>(null)
const checkReturnsFakeIp = computed(() => !checkResult.value?.success && checkResult.value?.error_code === 'dns_fake_ip')
const managedLoading = ref(false)
const managedError = ref('')
const managedBindingError = ref('')
const managedLoaded = ref(false)
const agents = ref<NodeLatencyManagedAgent[]>([])
const endpoints = ref<NodeLatencyManagedEndpoint[]>([])
const agentId = ref('')
const endpointId = ref('__new__')
const listenAddress = ref('')
const port = ref('15353')
const listenerExpanded = ref(false)
const settingsForm = ref<HTMLFormElement | null>(null)
const upstream = ref('')
const upstreamModes = [{ value: 'existing', label: '选择现有上游' }, { value: 'manual', label: '手动指定' }]
const upstreamMode = ref('existing')
const upstreamId = ref('')
const managedBootstrap = ref('')
const statusLoading = ref(false)
const selectedAgent = computed(() => agents.value.find(agent => agent.id === agentId.value))
const agentAvailable = computed(() => !!selectedAgent.value?.eligible)
const agentEndpoints = computed(() => endpoints.value.filter(endpoint => endpoint.agent_id === agentId.value && endpoint.status !== 'removed'))
const selectedEndpoint = computed(() => agentEndpoints.value.find(endpoint => endpoint.endpoint_id === endpointId.value))
const availableUpstreams = computed(() => selectedAgent.value?.upstreams || [])
const eligibleUpstreams = computed(() => availableUpstreams.value.filter(candidate => candidate.eligible))
const selectedUpstream = computed(() => eligibleUpstreams.value.find(candidate => candidate.id === upstreamId.value))
const unavailableUpstreamsReason = computed(() => {
  if (selectedAgent.value?.upstreams_reason) return `${selectedAgent.value.upstreams_reason} 可切换为手动指定。`
  const reasons = [...new Set(availableUpstreams.value.map(candidate => candidate.reason).filter(Boolean))]
  return reasons.length ? `${reasons.join('；')}。可切换为手动指定。` : '已发布配置中没有支持复用的上游，可手动指定或先发布上游配置。'
})
const splitLines = (value: string) => value.split(/[,\n]/).map(line => line.trim()).filter(Boolean)
const ipv4 = /^(?:\d{1,3}\.){3}\d{1,3}$/
function isListenerIPv4(value: string) {
  if (!ipv4.test(value)) return false
  const parts = value.split('.').map(Number)
  return parts.every(part => part <= 255) && parts.join('.') === value && parts[0] !== 127 && !(parts[0]! >= 224 && parts[0]! <= 239) && !['0.0.0.0', '255.255.255.255'].includes(value)
}
function dnsHostname(value: string) {
  try { const url = new URL(value); return ['https:', 'tls:'].includes(url.protocol) && !ipv4.test(url.hostname) ? url.hostname : '' } catch { return '' }
}
const customDnsHostnames = computed(() => [...new Set(splitLines(servers.value).map(dnsHostname).filter(Boolean))])
const managedDnsHostnames = computed(() => [dnsHostname(upstream.value.trim())].filter(Boolean))
const bootstrapRequired = computed(() => customDnsHostnames.value.length > 0)
const managedBootstrapRequired = computed(() => managedDnsHostnames.value.length > 0)
function bootstrapHint(hostnames: string[], managed: boolean) {
  const purpose = hostnames.length
    ? `先用这里的 DNS 查询 ${hostnames.join('、')} 的 IP，再连接它查询节点域名。`
    : '当前 DNS 地址不含服务商域名，无需引导 DNS；可清空已有值。'
  return `${purpose}${managed ? '仅填一个 IPv4[:端口]。' : '每行一个 IPv4[:端口]，最多 4 个。'}`
}
function changeUpstreamSource(event: KeyboardEvent, value: string) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  upstreamMode.value = event.key === 'Home' ? 'existing' : event.key === 'End' ? 'manual' : value === 'existing' ? 'manual' : 'existing'
  const group = (event.currentTarget as HTMLElement).parentElement
  void nextTick(() => group?.querySelector<HTMLButtonElement>('[aria-checked="true"]')?.focus())
}
let disposed = false
let loadVersion = 0
let managedVersion = 0
let checkVersion = 0
let statusVersion = 0
let pollTimer: ReturnType<typeof setTimeout> | undefined
let pollCount = 0

function statusLabel(status: string) {
  return ({ preparing: '准备发布', publishing: '正在发布', verifying: '正在验证 DNS', ready: '可用', failed: '创建或验证失败', removing: '正在移除', removed: '已移除' } as Record<string, string>)[status] || '状态待确认'
}
function statusClass(status: string) { return status === 'ready' ? 'text-success-accent' : status === 'failed' ? 'text-destructive-accent' : 'text-primary-accent' }
function failureMessage(failure: any, fallback: string) { return failure.response?.data?.message || (failure instanceof Error ? failure.message : '') || fallback }
function formConfig(): NodeLatencyDnsConfig {
  return { mode: mode.value, servers: mode.value === 'custom' ? splitLines(servers.value) : [], bootstrap: mode.value === 'custom' ? splitLines(bootstrap.value) : [], managed: mode.value === 'managed' ? { agent_id: agentId.value, endpoint_id: endpointId.value } : {} }
}
function validationError() {
  if (mode.value === 'custom' && !splitLines(servers.value).length) return '请填写自定义 DNS，或选择系统 DNS。'
  if (mode.value === 'custom' && bootstrapRequired.value && !splitLines(bootstrap.value).length) return '请填写引导 DNS，以解析 DNS 服务商的域名。'
  if (mode.value === 'custom' && splitLines(bootstrap.value).length > 4) return '引导 DNS 最多填写 4 个。'
  if (mode.value === 'managed' && selectedEndpoint.value?.status !== 'ready') return '专用 DNS 尚未验证成功，请等待可用后保存。'
  return ''
}
function focusField(id: string) {
  void nextTick(() => settingsForm.value?.querySelector<HTMLElement>(`#${id}`)?.focus())
}
function validateForm() {
  error.value = validationError()
  if (error.value) focusField(mode.value === 'custom' ? (splitLines(servers.value).length ? 'node-latency-bootstrap' : 'node-latency-dns') : 'node-latency-endpoint')
  return !error.value
}
function stopPolling() { clearTimeout(pollTimer); pollTimer = undefined; ++statusVersion }
function upsertEndpoint(endpoint: NodeLatencyManagedEndpoint) {
  const index = endpoints.value.findIndex(item => item.agent_id === endpoint.agent_id && item.endpoint_id === endpoint.endpoint_id)
  if (index >= 0) endpoints.value[index] = { ...endpoints.value[index], ...endpoint }
  else endpoints.value.push(endpoint)
}
async function openSettings() { if (!props.disabled) { open.value = true; await load() } }
async function load() {
  const version = ++loadVersion
  loading.value = true
  loaded.value = false
  error.value = ''
  managedLoaded.value = false
  managedError.value = ''
  managedBindingError.value = ''
  endpoints.value = []
  agents.value = []
  listenAddress.value = ''
  port.value = '15353'
  listenerExpanded.value = false
  upstream.value = ''
  upstreamMode.value = 'existing'
  upstreamId.value = ''
  managedBootstrap.value = ''
  try {
    const { data } = await nodeApi.latencySettings()
    if (disposed || version !== loadVersion || !open.value) return
    if (!data?.success || !Array.isArray(data.dns_servers)) throw new Error('读取测速设置失败，请重试。')
    const dns = data.dns || { mode: data.dns_servers.length ? 'custom' : 'system', servers: data.dns_servers, bootstrap: [], managed: {} }
    servers.value = dns.servers.join('\n')
    bootstrap.value = dns.bootstrap.join('\n')
    agentId.value = dns.managed?.agent_id || ''
    endpointId.value = dns.managed?.endpoint_id || '__new__'
    mode.value = dns.mode
    managedBindingError.value = data.managed_status?.message || ''
    loaded.value = true
    if (mode.value === 'managed') await loadManaged()
  } catch (failure: any) {
    if (!disposed && version === loadVersion) error.value = failure.response?.data?.message || '读取测速设置失败，请重试。'
  } finally { if (!disposed && version === loadVersion) loading.value = false }
}
async function loadManaged() {
  if (managedLoading.value) return
  const version = ++managedVersion
  managedLoading.value = true
  managedError.value = ''
  try {
    const { data } = await nodeApi.latencyManagedDns()
    if (disposed || version !== managedVersion || !open.value) return
    if (!data.success || !Array.isArray(data.agents)) throw new Error(data.message || '读取 OxiDNS 设备失败，请重试。')
    agents.value = data.agents
    for (const endpoint of data.agents.flatMap(agent => agent.endpoints || [])) upsertEndpoint(endpoint)
    managedLoaded.value = true
  } catch (failure: any) {
    if (!disposed && version === managedVersion) managedError.value = failureMessage(failure, '读取 OxiDNS 设备失败，请重试。')
  } finally { if (!disposed && version === managedVersion) managedLoading.value = false }
}
async function save() {
  if (!loaded.value || busy.value || props.disabled) return
  if (validateForm()) await persist(formConfig())
}
async function persist(dns: NodeLatencyDnsConfig) {
  saving.value = true
  error.value = ''
  try {
    const { data } = await nodeApi.saveLatencySettings(dns)
    if (disposed) return
    if (!data?.success) throw new Error(data.message || '保存测速设置失败，请重试。')
    notify.success('测速设置已保存')
    emit('saved')
    open.value = false
  } catch (failure: any) { if (!disposed) error.value = failureMessage(failure, '保存测速设置失败，请重试。') }
  finally { if (!disposed) saving.value = false }
}
async function checkDns() {
  if (busy.value || checking.value || props.disabled) return
  if (!validateForm()) return
  const version = ++checkVersion
  checking.value = true
  checkResult.value = null
  try {
    const { data } = await nodeApi.checkLatencyDns(formConfig())
    if (!disposed && version === checkVersion && open.value) checkResult.value = data
  } catch (failure: any) {
    if (!disposed && version === checkVersion) checkResult.value = { success: false, addresses: [], message: failureMessage(failure, 'DNS 检测失败，请重试。') }
  } finally { if (!disposed && version === checkVersion) checking.value = false }
}
async function createAndEnable() {
  if (busy.value || props.disabled || !agentAvailable.value) return
  error.value = ''
  if (!isListenerIPv4(listenAddress.value.trim()) || !/^\d+$/.test(port.value) || Number(port.value) < 1 || Number(port.value) > 65535) {
    error.value = '请填写监听 IPv4 和 1–65535 范围内的端口。'
    listenerExpanded.value = true
    focusField(!isListenerIPv4(listenAddress.value.trim()) ? 'node-latency-listen' : 'node-latency-port')
    return
  }
  if (upstreamMode.value === 'existing' && !selectedUpstream.value) { error.value = '请选择现有上游，或切换为手动指定。'; focusField('node-latency-existing-upstream'); return }
  if (upstreamMode.value === 'manual' && !upstream.value.trim()) { error.value = '请填写上游 DNS 地址。'; focusField('node-latency-upstream'); return }
  if (upstreamMode.value === 'manual' && managedBootstrapRequired.value && (!managedBootstrap.value.trim() || splitLines(managedBootstrap.value).length !== 1)) { error.value = '请填写一个引导 DNS（IPv4[:端口]），以解析 DNS 服务商的域名。'; focusField('node-latency-managed-bootstrap'); return }
  provisioning.value = true
  pollCount = 0
  try {
    const target = { agent_id: agentId.value, listen_address: listenAddress.value.trim(), port: Number(port.value) }
    const payload = upstreamMode.value === 'existing'
      ? { ...target, upstream_id: upstreamId.value }
      : { ...target, upstream: upstream.value.trim(), ...(managedBootstrapRequired.value ? { bootstrap: splitLines(managedBootstrap.value) } : {}) }
    const { data } = await nodeApi.createLatencyManagedDns(payload)
    if (disposed) return
    if (!data.success || !data.endpoint_id) throw new Error(data.message || '创建专用 DNS 失败，请重试。')
    upsertEndpoint(data)
    endpointId.value = data.endpoint_id
    await handleStatus(data, true)
  } catch (failure: any) { if (!disposed) { error.value = failureMessage(failure, '创建专用 DNS 失败，请重试。'); provisioning.value = false } }
}
async function handleStatus(endpoint: NodeLatencyManagedEndpoint, enableWhenReady: boolean) {
  if (endpoint.status === 'ready') {
    provisioning.value = false
    if (enableWhenReady && !props.disabled) await persist({ mode: 'managed', servers: [], bootstrap: [], managed: { agent_id: endpoint.agent_id, endpoint_id: endpoint.endpoint_id } })
  } else if (['failed', 'removed'].includes(endpoint.status)) {
    provisioning.value = false
    if (enableWhenReady) error.value = endpoint.message || '专用 DNS 未通过验证，原测速设置保持不变。'
  } else if (enableWhenReady) {
    if (++pollCount >= 60) { provisioning.value = false; error.value = '设备仍在处理，测速设置尚未更改。稍后刷新状态，可用后再保存。'; return }
    pollTimer = setTimeout(() => { void refreshEndpoint(true) }, 2000)
  }
}
async function refreshEndpoint(enableWhenReady = false) {
  const endpoint = selectedEndpoint.value
  if (!endpoint || statusLoading.value) return
  const version = ++statusVersion
  statusLoading.value = true
  try {
    const { data } = await nodeApi.latencyManagedDnsStatus(endpoint.agent_id, endpoint.endpoint_id)
    if (disposed || version !== statusVersion || !open.value) return
    if (!data.success) throw new Error(data.message || '读取专用 DNS 状态失败。')
    upsertEndpoint(data)
    await handleStatus(data, enableWhenReady)
  } catch (failure: any) { if (!disposed && version === statusVersion) { error.value = failureMessage(failure, '读取专用 DNS 状态失败，请重试。'); provisioning.value = false } }
  finally { if (!disposed && version === statusVersion) statusLoading.value = false }
}
async function removeEndpoint() {
  const endpoint = selectedEndpoint.value
  if (!endpoint || busy.value || props.disabled) return
  const accepted = await confirmDanger(`将从设备「${selectedAgent.value?.name || endpoint.agent_id}」的 OxiDNS 配置中移除 ${endpoint.endpoint || '该专用入口'} 并发布。正在用于测速的入口须先切换模式并保存。`, { title: '移除专用 DNS 入口', confirmText: '移除并发布' })
  if (!accepted || disposed || !open.value || busy.value || props.disabled) return
  removing.value = true
  error.value = ''
  try {
    const { data } = await nodeApi.removeLatencyManagedDns(endpoint.agent_id, endpoint.endpoint_id)
    if (disposed) return
    if (!data.success) throw new Error(data.message || '移除专用 DNS 失败，请重试。')
    upsertEndpoint(data)
    // The removal is published to the device in the background; follow it to its
    // result instead of leaving「正在移除」until someone presses 刷新状态.
    let current = data
    for (let attempt = 0; current.status === 'removing' && attempt < 60; attempt++) {
      await new Promise(resolve => setTimeout(resolve, 2000))
      if (disposed || !open.value) return
      const status = await nodeApi.latencyManagedDnsStatus(endpoint.agent_id, endpoint.endpoint_id)
      if (disposed || !open.value) return
      if (!status.data.success) throw new Error(status.data.message || '读取专用 DNS 状态失败。')
      current = status.data
      upsertEndpoint(current)
    }
    if (current.status === 'removed') { endpointId.value = '__new__'; notify.success('专用 DNS 入口已移除') }
    else if (current.status === 'removing') error.value = '设备仍在处理移除，稍后点「刷新状态」查看结果。'
    else error.value = current.message || '移除专用 DNS 失败，请重试。'
  } catch (failure: any) { if (!disposed) error.value = failureMessage(failure, '移除专用 DNS 失败，请重试。') }
  finally { if (!disposed) removing.value = false }
}
watch(mode, value => { error.value = ''; if (value === 'managed' && loaded.value && !managedLoaded.value) void loadManaged() })
watch(agentId, () => { if (!loading.value) { endpointId.value = '__new__'; listenAddress.value = selectedAgent.value?.address || '' } })
watch(endpointId, value => { if (value === '__new__' && !listenAddress.value.trim()) listenAddress.value = selectedAgent.value?.address || '' })
watch([agentId, eligibleUpstreams], ([currentAgent, candidates], [previousAgent]) => {
  if (currentAgent !== previousAgent) { upstreamMode.value = 'existing'; upstreamId.value = '' }
  if (!candidates.some(candidate => candidate.id === upstreamId.value)) upstreamId.value = candidates.length === 1 ? candidates[0]!.id : ''
})
watch([agentId, endpointId], () => { if (!loading.value) managedBindingError.value = ''; if (!provisioning.value) { stopPolling(); statusLoading.value = false } })
watch([mode, servers, bootstrap, agentId, endpointId], () => { ++checkVersion; checking.value = false; checkResult.value = null })
watch(open, value => { if (!value) { ++loadVersion; ++managedVersion; ++checkVersion; stopPolling(); managedLoading.value = false; statusLoading.value = false; checking.value = false } })
onUnmounted(() => { disposed = true; ++loadVersion; ++managedVersion; ++checkVersion; stopPolling() })
</script>
