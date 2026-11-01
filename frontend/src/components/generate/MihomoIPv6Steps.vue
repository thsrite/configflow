<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, type RouteLocationRaw } from 'vue-router'
import { Copy, RefreshCw, ScanLine } from '@lucide/vue'
import { agentApi, profileApi, routerApi } from '@/api'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import MihomoIPv6Verification from './MihomoIPv6Verification.vue'
import UpstreamRouterPending from '@/components/routers/UpstreamRouterPending.vue'
import { upstreamRouterApi } from '@/api/upstreamRouterPending'
import { upstreamRouterLink } from '@/lib/upstreamRouterPending'
import { buildMihomoIPv6Summary } from '@/lib/mihomoIPv6Summary'
import { copyText } from '@/lib/clipboard'
import { notify } from '@/lib/feedback'
import { checkedAgo } from '@/lib/format'
import {
  firewallStep, hostStep, oxidnsStep, publishStep, stepOrder, upstreamStep, verifyStep,
  type IPv6Step, type IPv6StepContext, type IPv6StepId, type OxiDNSLink, type UpstreamPendingStatus,
} from '@/lib/mihomoIPv6Steps'
import type { IPv6Verification, MihomoIPv6Readiness } from '@/types/mihomoIPv6'

type Agent = { id: string; name: string; profile_id?: string; service_type?: string; status?: string; enabled?: boolean }
type Profile = { id: string; is_default?: boolean }
type Router = { id: string; name?: string; proxy_gateway_agent_id?: string; dns_agent_id?: string }
// refreshKey: a confirmed publish; recheckKey: the device check window closed.
const props = defineProps<{ open: boolean; profileId: string; preferredAgentId?: string; mode: 'nodes' | 'clients'; dirty?: boolean; disabled?: boolean; refreshKey?: number; recheckKey?: number }>()
const emit = defineEmits<{ check: [agentId: string] }>()

const agents = ref<Agent[]>([])
const profiles = ref<Profile[]>([])
const selectedId = ref('')
const targetUnavailable = ref(false)
const phase = ref<'idle' | 'loading' | 'checking'>('idle')
const error = ref('')
const readiness = ref<MihomoIPv6Readiness | null>(null)
const oxidns = ref<OxiDNSLink[]>([])
const upstream = ref<UpstreamPendingStatus | null>(null)
// Where the upstream step links: one waiting router opens its preview directly.
const upstreamLinks = ref<Array<{ key: string; label: string; to: RouteLocationRaw }>>([])
let upstreamSession = 0
const verifier = ref<{ verification: IPv6Verification | null; fresh: boolean } | null>(null)
// Bumped after a save, a publish or a manual refresh: the upstream reminder
// then reads the routers again. Opening the settings and closing the device
// check (which refreshes its own reminder) reuse the server's last read.
const upstreamKey = ref(0)
// 「检查于 N 分钟前」随时间更新。
const now = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | undefined
// Bumped by each new readiness: remounts the embedded verifier to re-read it.
const readings = ref(0)
// While the server still compares Mihomo's files (after a save or a router publication).
let publicationTimer: ReturnType<typeof setTimeout> | undefined
let session = 0
let initialized = false
let disposed = false

const defaultProfile = computed(() => profiles.value.find(item => item.is_default === true)?.id
  || profiles.value.find(item => item.id === 'default' && item.is_default === undefined)?.id)
const candidates = computed(() => agents.value.filter(agent => (agent.service_type || 'mihomo') === 'mihomo'
  && (agent.profile_id || defaultProfile.value) === props.profileId))
const selected = computed(() => candidates.value.find(agent => agent.id === selectedId.value))
const online = computed(() => selected.value?.status === 'online' && selected.value.enabled !== false)
const available = computed(() => props.open && !props.dirty && online.value)
// The last check is shown for an offline device too; the server does not contact it.
const readable = computed(() => props.open && !props.dirty && !!selected.value && selected.value.enabled !== false)
const current = (generation: number, id?: string) => !disposed && props.open && generation === session && (!id || id === selectedId.value)
const messageOf = (failure: any) => failure?.response?.data?.message || failure?.message || '读取失败，请重试。'

const context = computed<IPv6StepContext>(() => ({ dirty: !!props.dirty, readiness: readiness.value, checking: phase.value !== 'idle' }))
const groups = computed(() => readiness.value && verifier.value ? buildMihomoIPv6Summary({ readiness: readiness.value,
  verification: verifier.value.verification?.status === 'running' ? undefined : verifier.value.verification || undefined,
  preferVerificationSnapshot: verifier.value.fresh }) : null)
const steps = computed(() => {
  const all: Record<IPv6StepId, IPv6Step | null> = {
    host: hostStep(context.value),
    publish: publishStep(context.value),
    upstream: upstreamStep(upstream.value, !!props.dirty, props.mode),
    firewall: firewallStep(context.value),
    oxidns: oxidnsStep(oxidns.value),
    verify: verifyStep(context.value, groups.value, verifier.value?.verification?.status === 'running'),
  }
  return stepOrder(props.mode, oxidns.value.length > 0).map(id => all[id]).filter((item): item is IPv6Step => !!item)
})
// Shown once above the list instead of repeating the same reason on every step.
const notice = computed(() => {
  if (phase.value === 'loading') return { text: '正在读取设备…', tone: 'muted' }
  if (error.value) return { text: error.value, tone: 'warning' }
  if (targetUnavailable.value) return { text: '原目标设备已不属于此配置空间，请重新选择设备后检查。', tone: 'warning' }
  if (!candidates.value.length) return { text: '此配置空间尚未绑定 Mihomo 设备，绑定后才能检查。', tone: 'muted' }
  if (!selected.value) return { text: '选择要检查的 Mihomo 设备。', tone: 'muted' }
  if (props.dirty) return { text: '有未保存的修改。检查读取已保存的配置，保存草稿后自动重新检查。', tone: 'muted' }
  // With a last result the line below says it could not be checked again.
  if (!online.value && !readiness.value?.refresh_error) return { text: `设备「${selected.value.name}」${selected.value.enabled === false ? '已禁用' : '离线'}，上线后重新检查。`, tone: 'warning' }
  return null
})
// When the shown result was taken, and that edits made on a router or the host itself are not seen.
const checked = computed(() => {
  void now.value
  const at = readiness.value?.checked_at
  if (!at || props.dirty || phase.value !== 'idle') return ''
  const routers = upstream.value?.checkedAt ? `，上游路由${checkedAgo(upstream.value.checkedAt, '读取')}` : ''
  return `设备${checkedAgo(at)}${routers}。路由器或主机上的手动修改不会自动发现，请点「重新检查」。`
})
const recheckFailed = computed(() => {
  const failure = readiness.value?.refresh_error
  if (!failure || props.dirty) return ''
  return `这次未能重新检查：${failure.message.replace(/[。.]$/, '')}。以下是${readiness.value?.stale ? '设备或配置变化前' : '上次'}的结果。`
})

async function readTargets() {
  const [agentResponse, profileResponse] = await Promise.all([agentApi.summaries(), profileApi.list()])
  if (!Array.isArray(agentResponse.data) || !Array.isArray(profileResponse.data)) throw new Error('设备列表响应无效。')
  return { agents: agentResponse.data as Agent[], profiles: profileResponse.data as Profile[] }
}

function clearResults() {
  clearTimeout(publicationTimer)
  readiness.value = null
  oxidns.value = []
  upstream.value = null
  upstreamLinks.value = []
  upstreamSession++
  verifier.value = null
}

async function loadTargets() {
  const generation = ++session
  phase.value = 'loading'
  error.value = ''
  try {
    const data = await readTargets()
    if (!current(generation)) return
    agents.value = data.agents
    profiles.value = data.profiles
    if (!initialized) {
      // Same rule as the device check: a fixed entry device is never replaced.
      selectedId.value = props.preferredAgentId
        ? candidates.value.find(agent => agent.id === props.preferredAgentId)?.id || ''
        : candidates.value.length === 1 ? candidates.value[0].id : ''
      targetUnavailable.value = !!props.preferredAgentId && !selectedId.value
      initialized = true
    } else if (selectedId.value && !selected.value) {
      selectedId.value = ''
      targetUnavailable.value = true
      clearResults()
    }
  } catch (failure) {
    if (current(generation)) error.value = messageOf(failure)
  } finally {
    if (current(generation)) phase.value = 'idle'
  }
  if (current(generation) && !error.value) await inspect()
}

// The server answers with the last check while nothing it reads has changed;
// `refresh` (「重新检查」, a confirmed publish) checks the device again and
// `upstream` (also after a save) reads the upstream routers again.
async function inspect(options: { refresh?: boolean; upstream?: boolean } = {}) {
  if (!readable.value || phase.value === 'loading') return
  const generation = ++session
  const id = selectedId.value
  const profileId = props.profileId
  phase.value = 'checking'
  error.value = ''
  if (options.upstream) upstreamKey.value++
  try {
    const { data } = await agentApi.ipv6Readiness(id, profileId, { refresh: !!options.refresh })
    if (!current(generation, id)) return
    if (data?.success !== true || data.agent_id !== id || data.profile_id !== profileId
      || !Array.isArray(data.checks) || !Array.isArray(data.routers) || !data.connectivity) {
      throw new Error(data?.message || '检查结果与当前设备或配置空间不一致，请重新检查。')
    }
    readiness.value = data
    readings.value++
    followPublication(generation, id)
  } catch (failure) {
    if (current(generation, id)) {
      error.value = messageOf(failure)
      readiness.value = null
    }
  } finally {
    if (current(generation, id)) phase.value = 'idle'
  }
  if (current(generation, id) && readiness.value && props.mode === 'clients') await readOxiDNS(generation, id)
}

// 「保存并发布 Mihomo」 waits for the server's comparison of Mihomo's files, which
// runs in the background. Re-read the reused device check (no device is
// contacted while it is unchanged) until the comparison answers.
function followPublication(generation: number, id: string, attempt = 0) {
  clearTimeout(publicationTimer)
  if (readiness.value?.publication !== 'checking' || attempt >= 40) return
  publicationTimer = setTimeout(async () => {
    if (!current(generation, id) || phase.value !== 'idle') return
    try {
      const { data } = await agentApi.ipv6Readiness(id, props.profileId, { refresh: false })
      if (!current(generation, id) || phase.value !== 'idle' || !readiness.value) return
      if (data?.success !== true || data.agent_id !== id || data.profile_id !== props.profileId
        || !Array.isArray(data.checks) || !Array.isArray(data.routers) || !data.connectivity) return
      if (data.checked_at === readiness.value.checked_at) readiness.value = { ...readiness.value, publication: data.publication }
      else {
        readiness.value = data
        readings.value++
      }
      followPublication(generation, id, attempt + 1)
    } catch {
      // The next check reads it again.
    }
  }, 3000)
}

// The saved router settings only (no router is contacted): which linked
// routers hand their clients an OxiDNS server.
async function readOxiDNS(generation: number, id: string) {
  let routers: Router[] = []
  try {
    const { data } = await routerApi.list(props.profileId)
    routers = Array.isArray(data?.routers) ? data.routers : []
  } catch {
    // Without the list the OxiDNS hint is simply not shown.
  }
  if (!current(generation, id)) return
  oxidns.value = routers.filter(router => router.proxy_gateway_agent_id === id && router.dns_agent_id).flatMap(router => {
    const agent = agents.value.find(item => item.id === router.dns_agent_id && item.service_type === 'oxidns')
    return agent ? [{ router: router.name || router.id, agent: agent.name }] : []
  })
}

// The reminder itself stays hidden here: the publish bar and the device check
// show its lines, so this step gives only its status and the way to act. The
// waiting routers come from the reminder's own read, reused by the server.
async function followUpstream(status: UpstreamPendingStatus) {
  upstream.value = status
  upstreamLinks.value = []
  const generation = ++upstreamSession
  const id = selectedId.value
  const profileId = props.profileId
  if (status.state !== 'pending' && status.state !== 'unknown') return
  try {
    const { data } = await upstreamRouterApi.pending(id, { profileId })
    if (generation !== upstreamSession || disposed || id !== selectedId.value || profileId !== props.profileId || data?.success !== true || !Array.isArray(data.routers)) return
    upstreamLinks.value = data.routers.filter(router => router.profile_id === profileId && router.state !== 'synced').slice(0, 3).map(router => ({
      key: `${router.profile_id}:${router.id}`,
      label: `「${router.name || router.id}」${router.state === 'pending' ? '查看并发布' : router.state === 'blocked' ? '查看原因' : '查看差异报告'}`,
      to: upstreamRouterLink(router, router.state !== 'unknown'),
    }))
  } catch {
    // The generic link to 上游路由 stays.
  }
}

function selectTarget(value: unknown) {
  if (phase.value !== 'idle') return
  selectedId.value = String(value || '')
  targetUnavailable.value = false
  clearResults()
  void inspect()
}

// 「重新检查」: re-read the devices too when the target is missing, offline or could not be read.
function refresh() {
  if (phase.value !== 'idle') return
  if (!agents.value.length || !selected.value || !online.value) void loadTargets()
  else void inspect({ refresh: true, upstream: true })
}

async function copyCommand(command: string) {
  try {
    await copyText(command)
    notify.success('命令已复制。')
  } catch {
    notify.error('复制失败，请手动选择命令。')
  }
}

watch(() => [props.open, props.profileId, props.preferredAgentId] as const, () => {
  session++
  initialized = false
  agents.value = []
  profiles.value = []
  selectedId.value = ''
  targetUnavailable.value = false
  error.value = ''
  phase.value = 'idle'
  clearResults()
  if (props.open) void loadTargets()
}, { immediate: true, flush: 'sync' })
// A save changes the saved configuration (the server checks again if its
// content did); a publish changes the device, so it is checked again; a closed
// device check may have checked again or prepared the host, which the server kept.
watch(() => props.dirty, (dirty, before) => { if (before && !dirty) void inspect({ upstream: true }) })
watch(() => props.refreshKey, () => void inspect({ refresh: true, upstream: true }))
watch(() => props.recheckKey, () => void inspect())
onMounted(() => { ticker = setInterval(() => { now.value = Date.now() }, 30000) })
onUnmounted(() => { clearInterval(ticker); clearTimeout(publicationTimer); disposed = true; session++ })
</script>

<template>
  <section class="min-w-0 space-y-1 pt-3" aria-label="IPv6 设置步骤" data-mihomo-ipv6-steps>
    <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
      <h3 class="m-0 min-w-0 flex-1 text-[13px] font-semibold">需要完成的步骤<span v-if="selected" class="block break-words font-normal text-muted-foreground sm:ml-2 sm:inline">设备：{{ selected.name }}</span></h3>
      <div class="flex shrink-0 items-center gap-1">
        <Button type="button" variant="ghost" class="min-h-11 px-3 text-[13px]" :disabled="!open || dirty || disabled || phase !== 'idle'" data-ipv6-steps-recheck @click="refresh">
          <RefreshCw class="size-4" :class="phase === 'checking' && 'animate-spin motion-reduce:animate-none'" aria-hidden="true" />重新检查
        </Button>
        <Button type="button" variant="outline" class="min-h-11 shrink-0 border-border-strong text-[13px]" aria-label="检查设备 IPv6" :disabled="disabled" @click="emit('check', selectedId)"><ScanLine class="size-4" aria-hidden="true" />检查设备</Button>
      </div>
    </div>
    <Select v-if="!preferredAgentId && candidates.length > 1" :model-value="selectedId" :disabled="phase !== 'idle' || disabled" @update:model-value="selectTarget">
      <SelectTrigger class="min-h-11 w-full min-w-0 sm:max-w-xs" aria-label="IPv6 步骤检查的设备"><SelectValue placeholder="选择检查设备" /></SelectTrigger>
      <SelectContent><SelectItem v-for="agent in candidates" :key="agent.id" :value="agent.id" :disabled="agent.status !== 'online' || agent.enabled === false" class="min-h-11 break-all whitespace-normal">{{ agent.name }}{{ agent.status !== 'online' ? '（离线）' : agent.enabled === false ? '（已禁用）' : '' }}</SelectItem></SelectContent>
    </Select>
    <p v-if="notice" class="m-0 break-words text-xs leading-relaxed" :class="notice.tone === 'warning' ? 'text-warning-accent' : 'text-muted-foreground'" role="status">{{ notice.text }}</p>
    <p v-if="recheckFailed" class="m-0 break-words text-xs leading-relaxed text-warning-accent" role="status" data-ipv6-steps-recheck-failed>{{ recheckFailed }}</p>
    <p v-if="checked" class="m-0 break-words text-xs leading-relaxed text-muted-foreground" data-ipv6-steps-checked>{{ checked }}</p>
    <ol class="m-0 min-w-0 list-none divide-y divide-border/70 p-0">
      <li v-for="item in steps" :key="item.id" class="min-w-0" :data-ipv6-step="item.id">
        <AgentStatusRow :label="item.label" :status="item.status" :tone="item.tone">
          <template v-if="item.issues.length || item.note || item.commands?.length" #notice>
            <p v-for="issue in item.issues" :key="issue" class="m-0 [overflow-wrap:anywhere]" :role="item.tone === 'danger' ? 'alert' : undefined">{{ issue }}</p>
            <p v-if="item.note" class="m-0 text-muted-foreground [overflow-wrap:anywhere]">{{ item.note }}</p>
            <div v-for="entry in item.commands || []" :key="entry.command" class="min-w-0 space-y-1.5" data-ipv6-step-command>
              <div class="flex min-h-8 items-center justify-between gap-2 max-md:min-h-11">
                <p class="m-0 min-w-0 font-medium">{{ entry.label }}</p>
                <Button type="button" variant="ghost" size="sm" class="shrink-0 px-2 text-xs max-md:min-h-11" aria-label="复制 IPv6 同口转发放行命令" @click="copyCommand(entry.command)"><Copy class="size-3.5" aria-hidden="true" />复制</Button>
              </div>
              <pre class="m-0 min-w-0 overflow-x-auto whitespace-pre rounded-md border border-border bg-muted px-3 py-2 font-mono text-xs leading-relaxed text-foreground">{{ entry.command }}</pre>
            </div>
            <Button v-if="item.id === 'host' && item.issues.length" type="button" variant="outline" class="min-h-11" :disabled="disabled" @click="emit('check', selectedId)"><ScanLine class="size-4" aria-hidden="true" />检查设备</Button>
            <RouterLink v-if="item.id === 'upstream' && upstream?.state === 'none'" class="inline-flex min-h-11 items-center text-primary-accent underline underline-offset-4" to="/routers">前往上游路由</RouterLink>
            <RouterLink v-if="item.id === 'oxidns'" class="inline-flex min-h-11 items-center text-primary-accent underline underline-offset-4" to="/agents">到 Agent 页打开 OxiDNS 设置</RouterLink>
          </template>
        </AgentStatusRow>
        <template v-if="item.id === 'upstream' && selectedId">
          <!-- Mounted but hidden: this row shows only its status; its own lines appear in the
               publish bar and the device check, not twice on one screen. -->
          <div class="hidden" data-ipv6-step-upstream-reminder>
            <UpstreamRouterPending :agent-id="selectedId" :profile-id="profileId" :refresh-key="upstreamKey" compact @status="followUpstream" />
          </div>
          <div v-if="!dirty && (upstream?.state === 'pending' || upstream?.state === 'unknown')" class="-mt-1 mb-2 flex min-w-0 flex-wrap gap-x-4 text-xs" data-ipv6-step-upstream>
            <RouterLink v-for="link in upstreamLinks" :key="link.key" class="inline-flex min-h-11 items-center text-primary-accent underline underline-offset-4" :to="link.to">{{ link.label }}</RouterLink>
            <RouterLink v-if="!upstreamLinks.length" class="inline-flex min-h-11 items-center text-primary-accent underline underline-offset-4" to="/routers">前往上游路由查看</RouterLink>
          </div>
        </template>
        <div v-if="item.id === 'verify' && readiness && !dirty" v-show="item.tone !== 'success'" class="mb-2 ml-2 min-w-0 border-l border-border pl-4 text-xs" data-ipv6-step-verify>
          <MihomoIPv6Verification :key="`${selectedId}-${readings}`" :open="open" :agent-id="selectedId" :profile-id="profileId"
            :config-revision="readiness.config_revision" :readiness="readiness" :disabled="!available || phase !== 'idle'" @result="verifier = $event" />
        </div>
      </li>
    </ol>
  </section>
</template>
