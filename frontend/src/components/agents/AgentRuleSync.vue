<template>
  <section class="mb-3 min-w-0" aria-label="规则同步">
    <div class="flex min-h-8 min-w-0 items-center justify-between gap-2">
      <span class="text-[13px] font-semibold">规则同步</span>
      <slot name="actions" />
    </div>
    <div class="flex min-w-0 flex-wrap items-center gap-x-4">
      <div v-for="option in options" :key="option.key" class="flex min-w-0 items-center gap-1">
        <label :for="`${controlId}-${option.key}`"
          class="flex min-h-11 min-w-0 cursor-pointer items-center gap-2 text-xs" :class="saving || !loaded ? 'cursor-wait text-muted-foreground' : 'text-foreground'">
          <Switch :id="`${controlId}-${option.key}`" :aria-label="option.label" :model-value="settings[option.key]"
            :disabled="saving || !loaded" @update:model-value="value => save(option.key, Boolean(value))" />
          {{ option.label }}
        </label>
        <FieldHelp :label="option.label" :text="option.help" />
      </div>
      <span v-if="saving || (!loaded && reading)" role="status" class="flex min-h-11 items-center gap-1.5 text-xs text-muted-foreground">
        <Loader2 class="size-3.5 animate-spin" />{{ saving ? '正在保存…' : '读取设置…' }}
      </span>
    </div>
    <p v-if="highAvailability" class="mt-1 mb-0 text-xs leading-5 text-foreground">高可用组共用这些开关，修改后同时作用于主备两台；规则按先备机后主机的顺序发布。</p>
    <p v-if="loaded && !baselineReady" class="mt-1 mb-0 text-xs leading-5 text-warning-accent">{{ baselineHint }}</p>
    <p v-if="loaded" role="status" aria-live="polite" class="mt-1 mb-0 flex min-w-0 items-start gap-1.5 text-xs leading-5" :class="stateTone">
      <Loader2 v-if="state.status === 'running'" class="mt-0.5 size-3.5 shrink-0 animate-spin" />
      <span class="min-w-0 break-words">{{ stateLabel }}<template v-if="state.message && (baselineReady || state.message !== baselineHint)"> · {{ state.message }}</template></span>
    </p>
    <div v-if="error" role="alert" class="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 text-xs leading-5" :class="errorKind === 'save' ? 'text-destructive-accent' : 'text-warning-accent'">
      <span class="min-w-0 break-words">{{ error }}</span>
      <Button v-if="errorKind === 'load'" type="button" variant="ghost" size="sm" class="min-h-11 shrink-0" :disabled="reading || saving" @click="load">重新读取</Button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Loader2 } from '@lucide/vue'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import FieldHelp from '@/components/common/FieldHelp.vue'
import { notify } from '@/lib/feedback'
import { agentRuleSyncApi, type AgentRuleSyncResponse, type AgentRuleSyncSettings, type AgentRuleSyncState } from '@/api/agentRuleSync'

const props = defineProps<{ agentId: string; highAvailability?: boolean }>()
const baselineHint = '先成功发布一次配置，建立自动同步基线'
const options: { key: keyof AgentRuleSyncSettings; label: string; help: string }[] = [
  { key: 'library', label: '规则库自动同步', help: '同步已启用引用的规则集内容及新增引用，只下载有变化的规则文件。仅新增未引用的规则库条目不会触发同步。' },
  { key: 'rules', label: '单条规则自动同步', help: '同步已选内嵌规则。' },
]
const controlId = computed(() => `agent-rule-sync-${encodeURIComponent(props.agentId)}`)
const settings = ref<AgentRuleSyncSettings>({ library: false, rules: false })
const state = ref<AgentRuleSyncState>({ status: 'disabled', message: '' })
const baselineReady = ref(false)
const loaded = ref(false)
const reading = ref(false)
const saving = ref(false)
const error = ref('')
const errorKind = ref<'load' | 'save'>('load')
const stateLabels: Record<AgentRuleSyncState['status'], string> = {
  disabled: '自动同步未启用', idle: '等待规则变更', pending: '等待同步', running: '正在同步规则',
  succeeded: '规则同步完成', failed: '规则同步失败', blocked: '自动同步等待处理',
}
const stateLabel = computed(() => stateLabels[state.value.status])
const stateTone = computed(() => ({ succeeded: 'text-success-accent', failed: 'text-destructive-accent',
  blocked: 'text-warning-accent', pending: 'text-warning-accent', running: 'text-primary-accent',
  disabled: 'text-muted-foreground', idle: 'text-muted-foreground' }[state.value.status]))
const PAIR_SAVED = 'configflow:rule-sync-pair-saved'
const onPairSaved = (event: Event) => {
  if (props.highAvailability && (event as CustomEvent<string>).detail !== props.agentId) void load()
}
window.addEventListener(PAIR_SAVED, onPairSaved)
let request = 0
let disposed = false
let controller: AbortController | undefined
let timer: ReturnType<typeof setTimeout> | undefined

function clearTimer() { if (timer) clearTimeout(timer); timer = undefined }
function schedule() {
  clearTimer()
  if (!disposed && (settings.value.library || settings.value.rules || ['pending', 'running'].includes(state.value.status))) {
    timer = setTimeout(() => { timer = undefined; void load() }, 10000)
  }
}
function apply(data: AgentRuleSyncResponse) {
  if (!data?.success || typeof data.settings?.library !== 'boolean' || typeof data.settings?.rules !== 'boolean'
    || !Object.prototype.hasOwnProperty.call(stateLabels, data.state?.status) || typeof data.baseline_ready !== 'boolean') {
    throw new Error(data?.message || '自动同步设置格式异常，请更新主服务和 Agent 后重试。')
  }
  settings.value = { ...data.settings }
  state.value = { ...data.state, message: typeof data.state.message === 'string' ? data.state.message : '' }
  baselineReady.value = data.baseline_ready
  loaded.value = true
}
function failureMessage(failure: any, fallback: string) {
  if (!failure?.response?.data?.message && failure?.response?.data?.code === 'UPGRADE_REQUIRED') {
    return '请将此 Agent 升级到 1.8.30-go 或更新版本后使用规则自动同步。'
  }
  return failure?.response?.data?.message || failure?.message || fallback
}
async function load() {
  if (disposed || !props.agentId || saving.value || reading.value) return
  clearTimer()
  const current = ++request
  controller = new AbortController()
  reading.value = true
  try {
    const { data } = await agentRuleSyncApi.get(props.agentId, controller.signal)
    if (disposed || current !== request) return
    apply(data)
    // A background status refresh must not erase a failed save explanation.
    if (errorKind.value === 'load') error.value = ''
  } catch (failure: any) {
    if (disposed || current !== request) return
    if (errorKind.value !== 'save' || !error.value) {
      errorKind.value = 'load'
      error.value = failureMessage(failure, '暂时无法读取自动同步状态，请重试。')
    }
  } finally {
    if (!disposed && current === request) { reading.value = false; schedule() }
  }
}
async function save(key: keyof AgentRuleSyncSettings, enabled: boolean) {
  if (disposed || !loaded.value || saving.value || enabled === settings.value[key]) return
  clearTimer()
  controller?.abort()
  const current = ++request
  controller = new AbortController()
  reading.value = false
  saving.value = true
  error.value = ''
  const candidate = { ...settings.value, [key]: enabled }
  try {
    const { data } = await agentRuleSyncApi.save(props.agentId, candidate, controller.signal)
    if (disposed || current !== request) return
    apply(data)
    errorKind.value = 'load'
    notify.success('自动同步设置已保存')
    // The switches of a high-availability pair are saved to both members; the other card must show them too.
    if (props.highAvailability) window.dispatchEvent(new CustomEvent(PAIR_SAVED, { detail: props.agentId }))
  } catch (failure: any) {
    if (disposed || current !== request) return
    errorKind.value = 'save'
    error.value = failureMessage(failure, '自动同步设置未保存，请重试。')
    notify.error('自动同步设置未保存', error.value)
  } finally {
    if (!disposed && current === request) { saving.value = false; schedule() }
  }
}
watch(() => props.agentId, () => {
  request++
  clearTimer()
  controller?.abort()
  settings.value = { library: false, rules: false }
  state.value = { status: 'disabled', message: '' }
  baselineReady.value = false
  loaded.value = false
  reading.value = false
  saving.value = false
  error.value = ''
  errorKind.value = 'load'
  void load()
}, { immediate: true })
onBeforeUnmount(() => { disposed = true; request++; clearTimer(); controller?.abort(); window.removeEventListener(PAIR_SAVED, onPairSaved) })
</script>
