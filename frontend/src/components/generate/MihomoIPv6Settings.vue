<script setup lang="ts">
import { computed, onUnmounted, ref, useId, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import FieldHelp from '@/components/common/FieldHelp.vue'
import FormField from '@/components/common/FormField.vue'
import RadioChips from '@/components/common/RadioChips.vue'
import { optionHelp } from '@/lib/optionCopy'
import { readMihomoIPv6Mode, setMihomoIPv6Mode, readMihomoFakeIPv6Range, setMihomoFakeIPv6Range, type MihomoIPv6Mode } from '@/lib/mihomoIPv6'
import MihomoIPv6Readiness from './MihomoIPv6Readiness.vue'
import MihomoIPv6Steps from './MihomoIPv6Steps.vue'

const content = defineModel<string>({ required: true })
const props = defineProps<{ open: boolean; profileId: string; preferredAgentId?: string; defaultConfig?: string; disabled?: boolean; dirty?: boolean; refreshKey?: number }>()
const emit = defineEmits<{ busy: [value: boolean]; validation: [value: { invalid: boolean; dirty: boolean }] }>()
const checksOpen = ref(false)
const checkTarget = ref('')
// Each closed device check may have prepared the host or run a verification.
const checksClosed = ref(0)
let checksOpener: HTMLElement | null = null
const busy = ref(false)
const error = ref('')
const changeNotice = ref('')
// The YAML a notice describes; any later edit (e.g. typed in the editor) retires it.
let noticeContent = ''
const id = useId()
const state = computed(() => readMihomoIPv6Mode(content.value, props.defaultConfig))
const fakeRange = computed(() => readMihomoFakeIPv6Range(content.value, props.defaultConfig))
const stepsMode = computed(() => state.value.mode === 'nodes' || state.value.mode === 'clients' ? state.value.mode : null)
const rangeInput = ref('')
const rangeError = ref('')
const rangeDirty = ref(false)
const rangeHelp = optionHelp('客户端收到的 IPv6 Fake-IP 取自此网段，通常保留默认值即可。',
  '填写 IPv6 内网网段，例如 fc00::1/64，并避开局域网正在使用的网段。两个 IPv6 模式都把此网段发布到上游路由，切换模式不用再改路由器；选择「关闭」会保留此值。')
// The chip uses the short name; the description gives the full one.
const modeLabels: Record<MihomoIPv6Mode, string> = { off: '关闭', nodes: 'Mihomo 用 IPv6，客户端走 IPv4', clients: '客户端也走 IPv6' }
const modeDescriptions: Record<MihomoIPv6Mode, string> = {
  off: 'Mihomo 不使用 IPv6，客户端也只拿到 IPv4 地址。',
  nodes: 'Mihomo 用 IPv6，客户端走 IPv4 分流：Mihomo 经 IPv6 连接节点，DNS 只给客户端返回 IPv4 地址。IPv6 Fake-IP 网段和规则集里的 IPv6 网段照常发布到上游路由，切换模式不用再改路由器。',
  clients: '手机、电脑也拿到 IPv6 地址并经 Mihomo 分流，需要上游路由配合。',
}
const modeHelp = optionHelp(
  '节点只有 IPv6 地址或想走 IPv6 线路时，选「Mihomo 用 IPv6，客户端走 IPv4」；希望手机、电脑也通过 IPv6 分流时，选「客户端也走 IPv6」。',
  '关闭写入 ipv6: false；「Mihomo 用 IPv6，客户端走 IPv4」写入 ipv6: true、dns.ipv6: false；「客户端也走 IPv6」写入两项 true。两个 IPv6 模式在 Fake-IP 模式下都补齐 fake-ip-range6。与下方 YAML 双向同步，保存并发布后生效。')
const modeOptions = computed(() => (Object.keys(modeLabels) as MihomoIPv6Mode[])
  .map(value => ({ value, label: modeLabels[value], disabled: !state.value.options[value].available })))
watch(() => [content.value, props.defaultConfig, props.open, props.profileId] as const, () => {
  rangeInput.value = fakeRange.value.value
  rangeError.value = fakeRange.value.error
  rangeDirty.value = false
}, { immediate: true })
watch(() => [props.open, fakeRange.value.visible, rangeError.value, rangeDirty.value] as const, () => {
  emit('validation', { invalid: props.open && fakeRange.value.visible && !!rangeError.value,
    dirty: props.open && fakeRange.value.visible && rangeDirty.value })
}, { immediate: true })
onUnmounted(() => emit('validation', { invalid: false, dirty: false }))
function changeRange(value: string | number) {
  if (props.disabled || busy.value || !fakeRange.value.editable) return
  rangeInput.value = String(value)
  rangeDirty.value = rangeInput.value !== fakeRange.value.value
  try {
    content.value = setMihomoFakeIPv6Range(content.value, rangeInput.value, props.defaultConfig)
    rangeError.value = ''
    rangeDirty.value = false
    changeNotice.value = ''
  } catch (failure) {
    rangeError.value = failure instanceof Error ? failure.message : '请填写有效的 IPv6 网段。'
    changeNotice.value = ''
  }
}
function changeMode(mode: string) {
  if (props.disabled || busy.value || mode === state.value.mode || !(mode in modeLabels)) return
  try {
    const changed = setMihomoIPv6Mode(content.value, mode as MihomoIPv6Mode, props.defaultConfig)
    content.value = changed.content
    changeNotice.value = changed.notice
    noticeContent = changed.content
    error.value = ''
  } catch (failure) {
    error.value = failure instanceof Error ? failure.message : '无法修改，请在 YAML 中调整。'
  }
}
function openChecks(agentId = '') {
  if (busy.value || props.disabled || !props.open) return
  checksOpener = document.activeElement instanceof HTMLElement ? document.activeElement : null
  checkTarget.value = agentId
  checksOpen.value = true
}
function setChecksOpen(value: boolean) {
  if (busy.value || value) return
  checksOpen.value = false
  checksClosed.value++
}
function guardDismiss(event: Event) {
  if (busy.value) event.preventDefault()
}
function restoreFocus(event: Event) {
  // Opened from a step button rather than a DialogTrigger: return focus there.
  if (checksOpener?.isConnected) {
    event.preventDefault()
    checksOpener.focus()
  }
  checksOpener = null
}
watch(() => [props.open, props.profileId, props.preferredAgentId], () => { checksOpen.value = false; error.value = ''; changeNotice.value = '' })
watch(content, value => { if (value !== noticeContent) changeNotice.value = '' })
watch(busy, value => emit('busy', value))
</script>

<template>
  <Dialog :open="checksOpen" @update:open="setChecksOpen">
    <div class="min-w-0 shrink-0 border-b border-border px-4 py-3 md:px-5" data-mihomo-ipv6-settings>
      <FormField label="IPv6 模式" :help="modeHelp">
        <RadioChips :model-value="state.mode === 'custom' ? '' : state.mode || ''" :options="modeOptions" aria-label="IPv6 模式"
          :aria-describedby="`${id}-mode-description`" :disabled="disabled || busy || state.mode === null" @update:model-value="changeMode" />
        <div :id="`${id}-mode-description`" class="min-w-0 space-y-1 text-xs leading-relaxed" data-ipv6-mode-description>
          <p v-if="state.mode && state.mode !== 'custom'" class="m-0 break-words text-muted-foreground">{{ modeDescriptions[state.mode] }}</p>
          <p v-if="state.mode === 'custom'" class="m-0 break-words text-warning-accent" data-ipv6-mode-custom><span class="font-medium">自定义：</span>{{ state.customReason }}</p>
          <p v-if="state.mode === null" class="m-0 break-words text-warning-accent" role="alert">{{ state.lockedReason }}</p>
          <p v-else-if="state.lockedReason" class="m-0 break-words text-warning-accent">{{ state.lockedReason }}</p>
          <p v-if="state.defaultNote" class="m-0 break-words text-muted-foreground">{{ state.defaultNote }}</p>
        </div>
      </FormField>
      <div v-if="fakeRange.visible" class="min-w-0 max-w-sm space-y-1 pt-2" data-ipv6-fakeip-range>
        <div class="flex min-h-8 items-center gap-1">
          <label :for="`${id}-fakeip-range`" class="text-[13px] font-medium">IPv6 Fake-IP 网段</label>
          <FieldHelp label="IPv6 Fake-IP 网段" :text="rangeHelp" />
        </div>
        <Input :id="`${id}-fakeip-range`" :model-value="rangeInput" placeholder="fc00::1/64" class="min-h-11 font-mono"
          :disabled="disabled || busy || !fakeRange.editable" :aria-invalid="!!rangeError" :aria-describedby="`${id}-fakeip-range-message`"
          autocomplete="off" autocapitalize="off" :spellcheck="false" @update:model-value="changeRange" />
        <p :id="`${id}-fakeip-range-message`" class="m-0 break-words text-xs leading-relaxed"
          :class="rangeError ? 'text-destructive-accent' : 'text-muted-foreground'" :role="rangeError ? 'alert' : undefined">{{ rangeError || fakeRange.message }}</p>
      </div>
      <p v-if="changeNotice" class="mb-0 mt-2 break-words text-xs text-muted-foreground" role="status">{{ changeNotice }}</p>
      <p v-if="error" class="mb-0 mt-2 break-words text-xs text-warning-accent" role="alert">{{ error }}</p>
      <MihomoIPv6Steps v-if="stepsMode" :open="open" :profile-id="profileId" :preferred-agent-id="preferredAgentId" :mode="stepsMode"
        :dirty="dirty" :disabled="disabled || busy" :refresh-key="refreshKey" :recheck-key="checksClosed" @check="openChecks" />
    </div>
    <DialogContent v-if="checksOpen" class="flex min-h-0 max-w-2xl flex-col gap-0 overflow-hidden p-0 max-sm:p-0" :show-close-button="!busy" :aria-describedby="undefined" data-mihomo-ipv6-dialog
      @escape-key-down="guardDismiss" @interact-outside="guardDismiss" @close-auto-focus="restoreFocus">
      <DialogHeader class="shrink-0 border-b border-border px-4 py-4 pr-14 sm:px-5 sm:pr-14"><DialogTitle>设备 IPv6 检查</DialogTitle></DialogHeader>
      <div class="min-h-0 overflow-y-auto overscroll-contain px-4 py-4 sm:px-5">
        <MihomoIPv6Readiness :open="open && checksOpen" :profile-id="profileId" :preferred-agent-id="preferredAgentId || checkTarget || undefined" :disabled="disabled" :dirty="dirty" @busy="busy = $event" />
      </div>
      <DialogFooter class="shrink-0 border-t border-border px-4 py-3 sm:px-5"><Button type="button" variant="outline" class="min-h-11" :disabled="busy" @click="setChecksOpen(false)">返回设置</Button></DialogFooter>
    </DialogContent>
  </Dialog>
</template>
