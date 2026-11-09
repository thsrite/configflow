<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { Switch } from '@/components/ui/switch'
import { SNIFF_PROTOCOL_COPY, sniffOverrideField } from '@/lib/mihomoBaseCopy'
import {
  SNIFF_DEFAULT_PORTS, SNIFF_PROTOCOLS, parseSniffPorts, readSniff, sniffPortsText,
  type BaseEdit, type BaseIssue, type MihomoBaseView, type SniffProtocol,
} from '@/lib/mihomoBaseSettings'
import MihomoBaseCell from './MihomoBaseCell.vue'
import MihomoBaseField from './MihomoBaseField.vue'
import { CELL_INPUT, cellHelp, shortLabel } from './mihomoBaseCell'

/** The protocols of sniffer.sniff as cells: ports typed in place, a switch adds or removes the protocol. */
const props = defineProps<{ view: MihomoBaseView; issues: (field: string) => BaseIssue[]; disabled?: boolean }>()
const emit = defineEmits<{ edit: [edits: BaseEdit[]]; draft: [field: string, error: string] }>()

const state = computed(() => readSniff(props.view))
const locked = computed(() => props.disabled || !state.value.editable)
const texts = reactive<Record<string, string>>({})
const errors = reactive<Record<string, string>>({})
const portsOf = (name: SniffProtocol) => state.value.protocols[name].ports.value
// A value changed elsewhere (YAML editor, a switch) replaces what is being typed.
watch(() => SNIFF_PROTOCOLS.map(name => JSON.stringify(portsOf(name) ?? null)), () => {
  for (const name of SNIFF_PROTOCOLS) {
    const text = sniffPortsText(portsOf(name))
    if (texts[name] === undefined || errors[name] || JSON.stringify(parseSniffPorts(texts[name]).ports) !== JSON.stringify(portsOf(name) ?? [])) {
      texts[name] = text
      setError(name, '')
    }
  }
}, { immediate: true })

function setError(name: SniffProtocol, error: string) {
  if ((errors[name] || '') === error) return
  errors[name] = error
  emit('draft', `sniffer.sniff.${name}`, error)
}
function type(name: SniffProtocol, text: string) {
  texts[name] = text
  const parsed = parseSniffPorts(text)
  setError(name, parsed.error)
  if (!parsed.error) emit('edit', [{ path: ['sniffer', 'sniff', state.value.protocols[name].key, 'ports'], value: parsed.ports, kind: 'list' }])
}
function toggle(name: SniffProtocol, on: boolean) {
  const protocol = state.value.protocols[name]
  setError(name, '')
  emit('edit', [{ path: ['sniffer', 'sniff', protocol.key], value: on ? { ports: [...SNIFF_DEFAULT_PORTS[name]] } : undefined, kind: 'any' }])
}
// Changes apply as typed; Esc puts back the ports the cell had when it was focused.
const original: Record<string, unknown> = {}
function remember(name: SniffProtocol) { original[name] = portsOf(name) }
// The first Esc only leaves the cell; it must not also close the settings workspace.
function revert(name: SniffProtocol, event: KeyboardEvent) {
  if (JSON.stringify(original[name]) !== JSON.stringify(portsOf(name)) || errors[name]) {
    setError(name, '')
    texts[name] = sniffPortsText(original[name])
    if (Array.isArray(original[name])) emit('edit', [{ path: ['sniffer', 'sniff', state.value.protocols[name].key, 'ports'], value: original[name], kind: 'list' }])
  }
  ;(event.target as HTMLElement).blur()
}
const messages = (name: SniffProtocol) => [
  ...(errors[name] ? [{ message: errors[name], blocking: true }] : []),
  ...(name === 'HTTP' ? props.issues('sniffer.sniff') : []),
  ...props.issues(`sniffer.sniff.${name}`),
]

// HTTP's override-destination, written under the key HTTP already uses; missing, it follows the global switch.
const http = computed(() => state.value.protocols.HTTP)
const overrideField = computed(() => ({
  ...sniffOverrideField,
  path: ['sniffer', 'sniff', http.value.key, 'override-destination'],
  coreDefault: props.view.value(['sniffer', 'override-destination']) ?? true,
}))
</script>

<template>
  <MihomoBaseCell v-for="name in SNIFF_PROTOCOLS" :key="name" :label="shortLabel(SNIFF_PROTOCOL_COPY[name].label)"
    :help="cellHelp(SNIFF_PROTOCOL_COPY[name].label, SNIFF_PROTOCOL_COPY[name].help)" :html-for="`mihomo-base-sniff-${name}`"
    :status="name === 'HTTP' && !disabled ? state.reason : ''" warn :messages="messages(name)" :data-base-field="`sniffer.sniff.${name}`">
    <div class="flex min-w-0 items-center gap-1">
      <input :id="`mihomo-base-sniff-${name}`" :value="state.protocols[name].present ? texts[name] : ''" :placeholder="state.protocols[name].present ? '80, 8080-8880' : '未启用'"
        autocomplete="off" autocapitalize="off" :spellcheck="false" inputmode="numeric" :disabled="locked || !state.protocols[name].present || !state.protocols[name].ports.editable"
        :aria-invalid="messages(name).some(item => item.blocking)" :class="CELL_INPUT"
        @input="type(name, ($event.target as HTMLInputElement).value)" @focus="remember(name)"
        @keydown.enter.prevent="($event.target as HTMLElement).blur()" @keydown.esc.stop="revert(name, $event)" />
      <label class="flex min-h-11 min-w-11 shrink-0 cursor-pointer items-center justify-end pr-1.5 sm:min-h-8 sm:min-w-0">
        <Switch :aria-label="`嗅探 ${name}`" :model-value="state.protocols[name].present" :disabled="locked" @update:model-value="toggle(name, $event)" />
      </label>
    </div>
  </MihomoBaseCell>
  <MihomoBaseField :field="overrideField" :state="view.field(overrideField.path)" :disabled="locked || !http.present"
    :note="http.present ? '' : '先开启 HTTP 嗅探'" data-base-sniff-override @change="emit('edit', [{ path: overrideField.path, value: $event }])" />
</template>
