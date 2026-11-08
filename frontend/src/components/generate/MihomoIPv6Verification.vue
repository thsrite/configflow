<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { LoaderCircle, RefreshCw } from '@lucide/vue'
import { agentApi } from '@/api'
import { buildMihomoIPv6Summary } from '@/lib/mihomoIPv6Summary'
import { Button } from '@/components/ui/button'
import MihomoIPv6VerificationResult from './MihomoIPv6VerificationResult.vue'
import MihomoIPv6CheckSummary from './MihomoIPv6CheckSummary.vue'
import MihomoIPv6StepGroup from './MihomoIPv6StepGroup.vue'
import type { IPv6Verification, IPv6VerificationResult, MihomoIPv6Readiness } from '@/types/mihomoIPv6'

// group: shown as that group of the IPv6 step list, with its pass count and the verify button beside the rows.
const props = defineProps<{ open: boolean; agentId: string; profileId: string; configRevision?: number; deploymentId?: string; auto?: boolean; disabled?: boolean; readiness?: MihomoIPv6Readiness; group?: string }>()
// Lets an embedding view (the IPv6 step list) follow the result shown here.
const emit = defineEmits<{ result: [value: { verification: IPv6Verification | null; fresh: boolean }] }>()
const verification = ref<IPv6Verification | null>(null)
const pending = ref(false)
const starting = ref(false)
// A run started or observed in progress here reads a newer static snapshot.
const freshVerification = ref(false)
// The verified configuration has IPv6 off: nothing to verify, which is not a failure.
const notApplicable = ref(false)
const message = ref('')
const error = ref('')
let generation = 0
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false
let started = 0
const validRevision = computed(() => Number.isInteger(props.configRevision) && props.configRevision! >= 0)
const running = computed(() => verification.value?.status === 'running')
const canRetryAutomatic = computed(() => verification.value && !running.value
  && buildMihomoIPv6Summary({ verification: verification.value }).some(group => group.tone !== 'success'))
const shown = computed(() => verification.value && !running.value && !error.value ? verification.value : undefined)
const progress = computed(() => {
  if (notApplicable.value || !props.readiness) return ''
  const groups = buildMihomoIPv6Summary({ readiness: props.readiness, verification: shown.value, preferVerificationSnapshot: freshVerification.value })
  return `${groups.filter(group => group.tone === 'success').length} / ${groups.length} 通过`
})
const current = (session: number) => !disposed && props.open && session === generation
const failureMessage = (failure: any) => failure?.response?.data?.message || failure?.message || '验收结果暂时无法读取，请重试。'

function accept(data: IPv6VerificationResult, session: number, required = false) {
  if (!current(session)) return false
  if (!data?.success || data.agent_id !== props.agentId || data.profile_id !== props.profileId) throw new Error('验收结果与当前设备或配置空间不一致。')
  notApplicable.value = data.applicable === false
  if (notApplicable.value) {
    verification.value = null
    return false
  }
  const value = data.verification
  if (!value) {
    if (required) throw new Error('服务端尚未确认验收任务，请重新读取结果。')
    verification.value = null
    return false
  }
  // A saved result of an older draft is only stale: with the current readiness
  // the summary asks for a new verification. A run started here, or the result
  // of one publication, must match exactly.
  if ((value.config_revision !== props.configRevision || (props.deploymentId && value.deployment_id !== props.deploymentId))
    && (required || props.deploymentId || !props.readiness)) {
    throw new Error('验收结果对应其他配置版本，请重新检查当前配置后再验证。')
  }
  if (!['running', 'ok', 'warning', 'error'].includes(value.status) || !['mihomo_runtime', 'client_path'].includes(value.scope)
    || !Array.isArray(value.checks) || value.checks.some(check => !check || typeof check.id !== 'string'
      || typeof check.label !== 'string' || typeof check.message !== 'string'
      || !['ok', 'warning', 'error', 'unknown'].includes(check.status))) throw new Error('验收结果格式无效，请重新读取。')
  verification.value = value
  if (value.status === 'running') freshVerification.value = true
  return value.status === 'running'
}

function schedule(session: number) {
  if (!current(session)) return
  if (Date.now() - started >= 150000) {
    message.value = '验收仍在后台执行。可以离开此窗口，稍后重新打开查看结果。'
    return
  }
  timer = setTimeout(() => { timer = undefined; void read(session) }, 2000)
}

async function read(session: number) {
  if (!current(session)) return
  pending.value = true
  try {
    const { data } = await agentApi.getIPv6Verification(props.agentId, props.profileId, props.deploymentId)
    if (accept(data, session, running.value)) schedule(session)
  } catch (failure) {
    if (current(session)) error.value = failureMessage(failure)
  } finally { if (current(session)) pending.value = false }
}

async function start() {
  if (!props.open || props.disabled || !validRevision.value || pending.value || running.value) return
  const session = ++generation
  clearTimeout(timer)
  verification.value = null
  notApplicable.value = false
  error.value = ''
  message.value = ''
  started = Date.now()
  pending.value = true
  starting.value = true
  freshVerification.value = true
  try {
    const { data } = await agentApi.verifyIPv6(props.agentId, { profile_id: props.profileId,
      expected_config_revision: props.configRevision, ...(props.deploymentId ? { deployment_id: props.deploymentId } : {}) })
    if (accept(data, session, true)) schedule(session)
  } catch (failure) {
    if (current(session)) error.value = failureMessage(failure)
  } finally { if (current(session)) { pending.value = false; starting.value = false } }
}

watch(() => [props.open, props.agentId, props.profileId, props.configRevision, props.deploymentId] as const, () => {
  const session = ++generation
  clearTimeout(timer)
  verification.value = null
  notApplicable.value = false
  pending.value = false
  starting.value = false
  freshVerification.value = false
  error.value = ''
  message.value = ''
  started = Date.now()
  if (!props.open || !props.agentId || !validRevision.value) return
  if (props.auto) void start()
  else void read(session)
}, { immediate: true })
// Report only settled reads, so a re-read does not flash an empty result.
watch([verification, freshVerification, pending], ([value, fresh, reading]) => {
  if (!reading) emit('result', { verification: value, fresh })
}, { immediate: true })
onUnmounted(() => { disposed = true; generation++; clearTimeout(timer) })
</script>

<template>
  <MihomoIPv6StepGroup v-if="group" :title="group" :progress="progress" data-ipv6-verifier>
    <template v-if="!notApplicable" #actions>
      <Button type="button" variant="outline" size="sm" class="min-h-11 border-border-strong px-2.5 text-xs @xl/steps:min-h-8" aria-label="验证连接与分流"
        :disabled="disabled || pending || running || !validRevision" @click="start"><RefreshCw class="size-3.5" aria-hidden="true" />验证</Button>
    </template>
    <div v-if="pending || running || message || error || !validRevision || notApplicable" class="space-y-1 py-2.5 text-xs">
      <p v-if="pending || running" class="m-0 flex items-center gap-2" role="status"><LoaderCircle class="size-4 shrink-0 animate-spin motion-reduce:animate-none" aria-hidden="true" />{{ running || starting ? '正在验证连接与分流…' : '正在读取验收结果…' }}</p>
      <p v-if="starting || running" class="m-0 text-muted-foreground">可以离开此窗口，验收会在后台继续；重新打开设备检查可查看结果。</p>
      <p v-if="message" class="m-0 break-words text-warning-accent" role="status">{{ message }}</p>
      <p v-if="error" class="m-0 break-words text-destructive-accent" role="alert">{{ error }}</p>
      <p v-if="!validRevision" class="m-0 text-warning-accent">请重新检查设备，取得当前配置版本后再验证。</p>
      <p v-if="notApplicable" class="m-0 text-muted-foreground" role="status" data-ipv6-not-applicable>未启用 IPv6，无需验收。</p>
      <Button v-if="message || (error && running)" type="button" variant="ghost" class="min-h-11" :disabled="pending" @click="error = ''; message = ''; started = Date.now(); read(generation)">重新读取结果</Button>
    </div>
    <MihomoIPv6CheckSummary v-if="!notApplicable && readiness" checklist :readiness="readiness" :prefer-verification-snapshot="freshVerification" :verification="shown" data-mihomo-ipv6-verification />
  </MihomoIPv6StepGroup>
  <section v-else class="min-w-0 space-y-2" :aria-label="auto ? '发布后验收' : '连接与分流验收'" data-ipv6-verifier>
    <p v-if="pending || running" class="m-0 flex items-center gap-2 text-xs" role="status"><LoaderCircle class="size-4 shrink-0 animate-spin motion-reduce:animate-none" aria-hidden="true" />{{ running || starting ? auto ? '配置已发布，正在验证连接与分流…' : '正在验证连接与分流…' : '正在读取验收结果…' }}</p>
    <p v-if="starting || running" class="m-0 text-xs text-muted-foreground">可以离开此窗口，验收会在后台继续；重新打开设备检查可查看结果。</p>
    <p v-if="message" class="m-0 break-words text-xs text-warning-accent" role="status">{{ message }}</p>
    <p v-if="error" class="m-0 break-words text-xs text-destructive-accent" role="alert">{{ error }}</p>
    <p v-if="!validRevision" class="m-0 text-xs text-warning-accent">请重新检查设备，取得当前配置版本后再验证。</p>
    <p v-if="notApplicable" class="m-0 text-xs text-muted-foreground" role="status" data-ipv6-not-applicable>未启用 IPv6，无需验收。</p>
    <div v-else-if="readiness" data-mihomo-ipv6-verification>
      <MihomoIPv6CheckSummary :readiness="readiness" :prefer-verification-snapshot="freshVerification" :verification="shown" />
    </div>
    <MihomoIPv6VerificationResult v-else-if="verification && verification.status !== 'running'" :verification="verification" :compact="auto" />
    <div class="flex flex-wrap gap-2">
      <slot name="actions" />
      <Button v-if="!notApplicable && (!auto || error || canRetryAutomatic)" type="button" variant="outline" class="min-h-11" :disabled="disabled || pending || running || !validRevision" @click="start"><RefreshCw class="size-3.5" aria-hidden="true" />验证连接与分流</Button>
      <Button v-if="message || (error && running)" type="button" variant="ghost" class="min-h-11" :disabled="pending" @click="error = ''; message = ''; started = Date.now(); read(generation)">重新读取结果</Button>
    </div>
  </section>
</template>
