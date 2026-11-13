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
        <template v-else>
          <FormField label="自定义 DNS" html-for="node-latency-dns" hint="每行一个地址，需返回真实 IP。支持 IPv4[:端口]、DoH 和 DoT。经过代理网关时用 IP 形式的 DoH，例如 https://223.5.5.5/dns-query。" :help="optionHelp('已有可用 DNS，或希望自行管理加密 DNS 时使用。', 'DoH 使用 https://主机/路径，DoT 使用 tls://主机[:端口]。不支持账号、查询参数和片段。配置仅供节点测速使用。')" persistent-hint>
            <Textarea id="node-latency-dns" v-model="servers" class="min-h-24 font-mono text-sm" :disabled="busy || disabled" :rows="3" placeholder="https://223.5.5.5/dns-query&#10;192.0.2.53:15353&#10;tls://dns.example" spellcheck="false" />
          </FormField>
          <FormField v-if="bootstrapRequired || bootstrap.trim()" label="引导 DNS" html-for="node-latency-bootstrap" class="[&_p]:break-words" :hint="bootstrapHint(customDnsHostnames)" persistent-hint>
            <Textarea id="node-latency-bootstrap" v-model="bootstrap" class="min-h-20 font-mono text-sm" :disabled="busy || disabled" :rows="2" placeholder="IPv4 或 IPv4:端口，每行一个" spellcheck="false" />
          </FormField>
        </template>
        <div class="flex flex-col gap-3 border-t border-border pt-3">
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
              <template v-else-if="checkReturnsFakeIp">
                <p class="m-0 break-words text-xs leading-relaxed text-muted-foreground">{{ mode === 'system' ? '这台机器的 DNS 被网关接管，普通 DNS 查询拿不到真实 IP。' : '53 端口的普通 DNS 和域名形式的 DoH 会被网关接管。' }}改用 IP 形式的 DoH 可以绕开。</p>
                <Button type="button" variant="outline" class="min-h-11 self-start" :disabled="busy || disabled" @click="useDirectDoh">改用 DoH（223.5.5.5 / 1.12.12.12）</Button>
              </template>
              <p v-else class="m-0 break-words text-xs leading-relaxed text-muted-foreground">{{ checkResult.message || '未能完成 DNS 查询，请重试。' }}</p>
            </div>
          </div>
        </div>
        </div>
        <div class="flex shrink-0 flex-col gap-4 border-t border-border px-6 py-4 max-sm:px-4">
        <DialogFooter class="flex-row gap-2">
          <Button type="button" variant="outline" class="min-h-11" :disabled="busy" @click="open = false">取消</Button>
          <Button type="submit" class="min-h-11 max-sm:flex-1" :disabled="busy || disabled"><Loader2 v-if="saving" class="size-4 animate-spin" />{{ saving ? '正在保存…' : '保存' }}</Button>
        </DialogFooter>
        </div>
      </form>
      <div v-else class="p-6 max-sm:p-4"><Button variant="outline" class="min-h-11" @click="load">重新读取</Button></div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { CircleAlert, CircleCheck, Loader2, Settings2 } from '@lucide/vue'
import { nodeApi, type NodeLatencyDnsCheck, type NodeLatencyDnsConfig, type NodeLatencyDnsMode } from '@/api'
import FormField from '@/components/common/FormField.vue'
import Segmented from '@/components/common/Segmented.vue'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { notify } from '@/lib/feedback'
import { optionHelp } from '@/lib/optionCopy'

const props = defineProps<{ disabled?: boolean }>()
const emit = defineEmits<{ saved: [] }>()
const modes: Array<{ value: NodeLatencyDnsMode; label: string }> = [{ value: 'system', label: '系统 DNS' }, { value: 'custom', label: '自定义 DNS' }]
// Port 443 is not taken over by a gateway's DNS hijack, and an IP needs no bootstrap lookup.
const DIRECT_DOH = ['https://223.5.5.5/dns-query', 'https://1.12.12.12/dns-query']
const open = ref(false)
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const busy = computed(() => saving.value)
const mode = ref<NodeLatencyDnsMode>('system')
const servers = ref('')
const bootstrap = ref('')
const error = ref('')
const checking = ref(false)
const checkResult = ref<NodeLatencyDnsCheck | null>(null)
const checkReturnsFakeIp = computed(() => !checkResult.value?.success && checkResult.value?.error_code === 'dns_fake_ip')
const settingsForm = ref<HTMLFormElement | null>(null)
const splitLines = (value: string) => value.split(/[,\n]/).map(line => line.trim()).filter(Boolean)
const ipv4 = /^(?:\d{1,3}\.){3}\d{1,3}$/
function dnsHostname(value: string) {
  try { const url = new URL(value); return ['https:', 'tls:'].includes(url.protocol) && !ipv4.test(url.hostname) ? url.hostname : '' } catch { return '' }
}
const customDnsHostnames = computed(() => [...new Set(splitLines(servers.value).map(dnsHostname).filter(Boolean))])
const bootstrapRequired = computed(() => customDnsHostnames.value.length > 0)
function bootstrapHint(hostnames: string[]) {
  const purpose = hostnames.length
    ? `先用这里的 DNS 查询 ${hostnames.join('、')} 的 IP，再连接它查询节点域名。`
    : '当前 DNS 地址不含服务商域名，无需引导 DNS；可清空已有值。'
  return `${purpose}每行一个 IPv4[:端口]，最多 4 个。`
}
let disposed = false
let loadVersion = 0
let checkVersion = 0

function failureMessage(failure: any, fallback: string) { return failure.response?.data?.message || (failure instanceof Error ? failure.message : '') || fallback }
function formConfig(): NodeLatencyDnsConfig {
  return { mode: mode.value, servers: mode.value === 'custom' ? splitLines(servers.value) : [], bootstrap: mode.value === 'custom' ? splitLines(bootstrap.value) : [] }
}
function validationError() {
  if (mode.value === 'custom' && !splitLines(servers.value).length) return '请填写自定义 DNS，或选择系统 DNS。'
  if (mode.value === 'custom' && bootstrapRequired.value && !splitLines(bootstrap.value).length) return '请填写引导 DNS，以解析 DNS 服务商的域名。'
  if (mode.value === 'custom' && splitLines(bootstrap.value).length > 4) return '引导 DNS 最多填写 4 个。'
  return ''
}
function focusField(id: string) {
  void nextTick(() => settingsForm.value?.querySelector<HTMLElement>(`#${id}`)?.focus())
}
function validateForm() {
  error.value = validationError()
  if (error.value) focusField(splitLines(servers.value).length ? 'node-latency-bootstrap' : 'node-latency-dns')
  return !error.value
}
function useDirectDoh() {
  mode.value = 'custom'
  servers.value = DIRECT_DOH.join('\n')
  bootstrap.value = ''
  focusField('node-latency-dns')
}
async function openSettings() { if (!props.disabled) { open.value = true; await load() } }
async function load() {
  const version = ++loadVersion
  loading.value = true
  loaded.value = false
  error.value = ''
  try {
    const { data } = await nodeApi.latencySettings()
    if (disposed || version !== loadVersion || !open.value) return
    if (!data?.success || !Array.isArray(data.dns_servers)) throw new Error('读取测速设置失败，请重试。')
    const dns = data.dns || { mode: data.dns_servers.length ? 'custom' : 'system', servers: data.dns_servers, bootstrap: [] }
    servers.value = dns.servers.join('\n')
    bootstrap.value = dns.bootstrap.join('\n')
    mode.value = dns.mode
    loaded.value = true
  } catch (failure: any) {
    if (!disposed && version === loadVersion) error.value = failure.response?.data?.message || '读取测速设置失败，请重试。'
  } finally { if (!disposed && version === loadVersion) loading.value = false }
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
watch(mode, () => { error.value = '' })
watch([mode, servers, bootstrap], () => { ++checkVersion; checking.value = false; checkResult.value = null })
watch(open, value => { if (!value) { ++loadVersion; ++checkVersion; checking.value = false } })
onUnmounted(() => { disposed = true; ++loadVersion; ++checkVersion })
</script>
