<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { LoaderCircle, RefreshCw } from '@lucide/vue'
import { agentApi, profileApi } from '@/api'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import MihomoIPv6Verification from './MihomoIPv6Verification.vue'
import MihomoIPv6VerificationSettings from './MihomoIPv6VerificationSettings.vue'
import UpstreamRouterPending from '@/components/routers/UpstreamRouterPending.vue'
import type { MihomoIPv6Readiness } from '@/types/mihomoIPv6'
import { checkedAgo } from '@/lib/format'

type Agent = { id: string; name: string; profile_id?: string; service_type?: string; status?: string; enabled?: boolean }
type Profile = { id: string; is_default?: boolean }
const props = defineProps<{ open: boolean; profileId: string; preferredAgentId?: string; disabled?: boolean; dirty?: boolean }>()
const emit = defineEmits<{ busy: [value: boolean] }>()
const agents = ref<Agent[]>([])
const profiles = ref<Profile[]>([])
const selectedId = ref('')
const targetUnavailable = ref(false)
const result = ref<MihomoIPv6Readiness | null>(null)
const error = ref('')
const phase = ref<'idle' | 'loading' | 'checking' | 'preparing'>('idle')
const verificationVersion = ref(0)
const actionMessage = ref('')
// 每次取得检查结果加一：首次显示上游路由提醒，之后重新检查时一并刷新。
const routerCheck = ref(0)
let session = 0
let initialized = false
let disposed = false
const defaultProfile = computed(() => profiles.value.find(item => item.is_default === true)?.id
  || profiles.value.find(item => item.id === 'default' && item.is_default === undefined)?.id)
const candidates = computed(() => agents.value.filter(agent => (agent.service_type || 'mihomo') === 'mihomo'
  && (agent.profile_id || defaultProfile.value) === props.profileId))
const selected = computed(() => candidates.value.find(agent => agent.id === selectedId.value))
const busy = computed(() => phase.value !== 'idle')
const available = computed(() => props.open && !props.disabled && !props.dirty && !busy.value
  && selected.value?.status === 'online' && selected.value.enabled !== false)
// The last check is shown for an offline device too; the server does not contact it.
const readable = computed(() => props.open && !props.disabled && !props.dirty && !busy.value
  && !!selected.value && selected.value.enabled !== false)
const now = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | undefined
const checked = computed(() => {
  void now.value
  return result.value?.checked_at ? `${checkedAgo(result.value.checked_at)}。路由器或主机上的手动修改不会自动发现，请点「重新检查」。` : ''
})
const recheckFailed = computed(() => {
  const failure = result.value?.refresh_error
  return failure ? `这次未能重新检查：${failure.message.replace(/[。.]$/, '')}。以下是${result.value?.stale ? '设备或配置变化前' : '上次'}的结果。` : ''
})
const canPrepare = computed(() => available.value && result.value?.can_prepare === true && !!result.value.check_token)
const current = (generation: number, id?: string) => !disposed && props.open && generation === session
  && (!id || id === selectedId.value)
const messageOf = (failure: any) => failure?.response?.data?.message || failure?.message || '读取失败，请重试。'

async function readTargets() {
  const [agentResponse, profileResponse] = await Promise.all([agentApi.summaries(), profileApi.list()])
  if (!Array.isArray(agentResponse.data) || !Array.isArray(profileResponse.data)) throw new Error('设备列表响应无效。')
  return { agents: agentResponse.data as Agent[], profiles: profileResponse.data as Profile[] }
}

function updateTargets(data: Awaited<ReturnType<typeof readTargets>>) {
  agents.value = data.agents
  profiles.value = data.profiles
  if (!initialized) {
    selectedId.value = props.preferredAgentId
      ? candidates.value.find(agent => agent.id === props.preferredAgentId)?.id || ''
      : candidates.value.length === 1 ? candidates.value[0].id : ''
    targetUnavailable.value = !!props.preferredAgentId && !selectedId.value
    initialized = true
  } else if (selectedId.value && !selected.value) {
    selectedId.value = ''
    result.value = null
    targetUnavailable.value = true
  }
}

function acceptResult(data: MihomoIPv6Readiness, id: string) {
  if (data?.success !== true || data.agent_id !== id || data.profile_id !== props.profileId
    || !Array.isArray(data.checks) || !Array.isArray(data.routers) || !data.connectivity) {
    throw new Error(data?.message || '检查结果与当前设备或配置空间不一致，请重新检查。')
  }
  result.value = data
  routerCheck.value++
}

async function loadTargets() {
  const generation = session
  phase.value = 'loading'
  error.value = ''
  try {
    const data = await readTargets()
    if (!current(generation)) return
    updateTargets(data)
  } catch (failure) {
    if (current(generation)) error.value = messageOf(failure)
  } finally {
    if (current(generation)) phase.value = 'idle'
  }
  if (current(generation) && !error.value && readable.value) await inspect()
}

// Without refresh the server returns the last check while nothing it reads has changed.
async function inspect(refresh = false) {
  if (!readable.value) return
  const generation = session
  const id = selectedId.value
  phase.value = 'checking'
  error.value = ''
  actionMessage.value = ''
  result.value = null
  try {
    const { data } = await agentApi.ipv6Readiness(id, props.profileId, { refresh })
    if (current(generation, id)) acceptResult(data, id)
  } catch (failure) {
    if (current(generation, id)) error.value = messageOf(failure)
  } finally {
    if (current(generation, id)) phase.value = 'idle'
  }
}

async function prepare() {
  if (!canPrepare.value) return
  const previous = result.value!
  const generation = session
  const id = selectedId.value
  const profileId = props.profileId
  phase.value = 'preparing'
  error.value = ''
  actionMessage.value = ''
  try {
    // Bindings can change while the settings window stays open. Never fall back
    // to another device, even if it becomes the only remaining candidate.
    const targets = await readTargets()
    if (!current(generation, id)) return
    updateTargets(targets)
    if (selectedId.value !== id || selected.value?.status !== 'online' || selected.value.enabled === false) {
      throw new Error('原目标设备已改绑、离线或被禁用，请重新选择并检查。')
    }
    if (props.dirty || props.disabled) throw new Error('草稿已变化，请保存后重新检查。')
    result.value = null
    const { data } = await agentApi.prepareIPv6(id, { profile_id: profileId, interface: previous.prepare_interface, check_token: previous.check_token })
    if (!current(generation, id) || props.dirty) return
    acceptResult(data, id)
    actionMessage.value = '补齐操作已完成，请查看检查结果。'
  } catch (failure) {
    if (current(generation)) {
      error.value = messageOf(failure)
      result.value = null
    }
  } finally {
    if (current(generation)) phase.value = 'idle'
  }
}

function selectTarget(value: unknown) {
  if (busy.value) return
  selectedId.value = String(value || '')
  targetUnavailable.value = false
  result.value = null
  actionMessage.value = ''
  error.value = ''
  if (readable.value) void inspect()
}

watch(() => [props.open, props.profileId, props.preferredAgentId] as const, () => {
  session++
  initialized = false
  agents.value = []
  profiles.value = []
  selectedId.value = ''
  result.value = null
  targetUnavailable.value = false
  actionMessage.value = ''
  error.value = ''
  phase.value = 'idle'
  routerCheck.value = 0
  if (props.open) void loadTargets()
}, { immediate: true, flush: 'sync' })
watch(() => props.dirty, dirty => {
  if (dirty) {
    if (phase.value !== 'preparing') {
      session++
      phase.value = 'idle'
    }
    result.value = null
    actionMessage.value = ''
  }
}, { flush: 'sync' })
watch(busy, value => emit('busy', value), { immediate: true })
onMounted(() => { ticker = setInterval(() => { now.value = Date.now() }, 30000) })
onUnmounted(() => { clearInterval(ticker); disposed = true; session++; emit('busy', false) })
</script>

<template>
  <div class="min-w-0 space-y-3" data-mihomo-ipv6-readiness>
    <p class="m-0 text-xs leading-relaxed text-muted-foreground">检查已保存的配置；待验证的项目会提示下一步操作。</p>
    <p v-if="dirty" class="m-0 text-xs text-warning-accent" role="status">有未保存的修改，请先保存草稿，再检查设备 IPv6。</p>
    <div class="flex min-w-0 flex-wrap items-center gap-2">
      <Select v-if="candidates.length > 1 || (candidates.length && !selected)" :model-value="selectedId" :disabled="busy || disabled" @update:model-value="selectTarget">
        <SelectTrigger class="min-h-11 w-full min-w-0 sm:max-w-xs" aria-label="IPv6 检查设备"><SelectValue placeholder="选择检查设备" /></SelectTrigger>
        <SelectContent><SelectItem v-for="agent in candidates" :key="agent.id" :value="agent.id" :disabled="agent.status !== 'online' || agent.enabled === false" class="min-h-11 break-all whitespace-normal">{{ agent.name }}{{ agent.status !== 'online' ? '（离线）' : agent.enabled === false ? '（已禁用）' : '' }}</SelectItem></SelectContent>
      </Select>
      <p v-else-if="selected" class="m-0 min-w-0 flex-1 break-words text-xs">检查设备：{{ selected.name }}{{ selected.status !== 'online' ? '（离线）' : selected.enabled === false ? '（已禁用）' : '' }}</p>
      <p v-else-if="phase !== 'loading' && !targetUnavailable && !error" class="m-0 text-xs text-muted-foreground">此配置空间尚未绑定 Mihomo 设备，可先设置并保存草稿。</p>
      <Button v-if="!selected || selected.status !== 'online' || selected.enabled === false" type="button" variant="outline" class="min-h-11" :disabled="busy || disabled" @click="loadTargets"><RefreshCw class="size-3.5" />重新读取设备</Button>
      <Button v-else type="button" variant="outline" class="min-h-11" :disabled="!available" @click="inspect(true)"><RefreshCw class="size-3.5" />重新检查</Button>
    </div>
    <p v-if="targetUnavailable" class="m-0 text-xs text-warning-accent" role="status">原目标设备已不属于此配置空间，请明确选择另一台设备后再检查。</p>
    <p v-if="busy" class="m-0 flex items-center gap-2 text-xs" role="status"><LoaderCircle class="size-4 animate-spin motion-reduce:animate-none" />{{ phase === 'loading' ? '正在读取设备…' : phase === 'checking' ? '正在检查 IPv6 配置…' : '正在补齐缺少的配置并复查…' }}</p>
    <p v-if="error" class="m-0 break-words text-xs text-destructive-accent" role="alert">{{ error }}</p>
    <p v-if="actionMessage" class="m-0 break-words text-xs" role="status">{{ actionMessage }}</p>
    <p v-if="result && recheckFailed" class="m-0 break-words text-xs text-warning-accent" role="status" data-ipv6-readiness-recheck-failed>{{ recheckFailed }}</p>
    <p v-if="result && checked" class="m-0 break-words text-xs text-muted-foreground" data-ipv6-readiness-checked>{{ checked }}</p>
    <template v-if="result">
      <MihomoIPv6Verification :key="`${selectedId}-${verificationVersion}`" :open="open" :agent-id="selectedId" :profile-id="profileId" :config-revision="result.config_revision" :readiness="result" :disabled="!available">
        <template #actions>
          <Button v-if="result.can_prepare" type="button" variant="outline" class="min-h-11" :disabled="!canPrepare" @click="prepare">补齐缺少的配置</Button>
        </template>
      </MihomoIPv6Verification>
      <p v-if="result.can_prepare" class="m-0 text-xs leading-relaxed text-muted-foreground">补齐仅处理 Mihomo 主机配置，必要时重新加载 Mihomo。</p>
    </template>
    <UpstreamRouterPending v-if="selectedId && routerCheck && !dirty" :agent-id="selectedId" :profile-id="profileId" :refresh-key="routerCheck" />
    <MihomoIPv6VerificationSettings v-if="selected" :open="open" :agent-id="selectedId" :profile-id="profileId" :disabled="busy || disabled" @saved="verificationVersion++" />
  </div>
</template>
