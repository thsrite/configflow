<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ChevronDown } from '@lucide/vue'
import RadioChips from '@/components/common/RadioChips.vue'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import type { BaseFieldDef } from '@/lib/mihomoBaseCopy'
import { parseLines, pathId, type BaseFieldState, type BaseIssue } from '@/lib/mihomoBaseSettings'
import MihomoBaseCell from './MihomoBaseCell.vue'
import { CELL_INPUT, CELL_POPOVER, CELL_SWITCH, CELL_TRIGGER, cellHelp, shortLabel } from './mihomoBaseCell'

/** One base setting as a sheet cell, read from and written to the YAML through its parent. */
// `toggle`: an optional key shown with an on / off switch; switching on writes this value.
const props = defineProps<{ field: BaseFieldDef; state: BaseFieldState; issues?: BaseIssue[]; disabled?: boolean; note?: string; toggle?: number }>()
const emit = defineEmits<{ change: [value: unknown]; draft: [error: string] }>()
const UNSET = '__unset__'
const id = computed(() => `mihomo-base-${pathId(props.field.path).replace(/[^a-z0-9]+/gi, '-')}`)
const label = computed(() => shortLabel(props.field.label))
const help = computed(() => cellHelp(props.field.label, props.field.help))
const locked = computed(() => props.disabled || !props.state.editable)
const shown = (value: unknown) => typeof value === 'boolean' ? (value ? '开' : '关') : String(value)
const fallback = computed(() => props.field.coreDefault === undefined ? '未填写' : `默认 ${shown(props.field.coreDefault)}`)
const status = computed(() => !props.state.editable ? props.state.reason : props.note || '')
const draft = ref('')
const draftError = ref('')
const asText = (value: unknown) => props.field.type === 'list'
  ? (Array.isArray(value) ? value.map(String).join('\n') : '')
  : value === undefined || value === null ? '' : String(value)
// A value changed elsewhere (YAML editor, a preset) replaces what is being typed.
watch(() => props.state.value, value => {
  const current = props.field.type === 'list' ? JSON.stringify(parseLines(draft.value)) : draft.value
  const next = props.field.type === 'list' ? JSON.stringify(Array.isArray(value) ? value.map(String) : []) : asText(value)
  if (current !== next || draftError.value) {
    draft.value = asText(value)
    draftError.value = ''
    emit('draft', '')
  }
}, { immediate: true })

function updateInt(text: string) {
  draft.value = text
  const value = text.trim()
  draftError.value = value && !/^\d+$/.test(value) ? `${props.field.label}需为整数` : ''
  emit('draft', draftError.value)
  if (!draftError.value) emit('change', value ? Number(value) : undefined)
}
function updateText(text: string) {
  draft.value = text
  emit('change', text === '' ? undefined : text)
}
function updateList(text: string) {
  draft.value = text
  const values = parseLines(text)
  emit('change', values.length ? values : undefined)
}
// Changes apply as typed; Esc puts back the value the field had when it was focused.
let original: unknown
function remember() { original = props.state.value }
// The first Esc only leaves the field; it must not also close the settings workspace.
function revert(event: KeyboardEvent) {
  if (original !== props.state.value || draft.value !== asText(original) || draftError.value) {
    emit('change', original)
    draft.value = asText(original)
    draftError.value = ''
    emit('draft', '')
  }
  ;(event.target as HTMLElement).blur()
}

const enumValue = computed(() => typeof props.state.value === 'string' || typeof props.state.value === 'number'
  ? String(props.state.value).toLowerCase() : typeof props.state.value === 'boolean' && pathId(props.field.path) === 'find-process-mode'
    ? (props.state.value ? 'always' : 'off') : '')
const unknownEnum = computed(() => props.state.present && !props.field.options?.some(option => option.value === enumValue.value))
const enumOptions = computed(() => [...(props.field.unset ? [{ value: UNSET, label: props.field.unset }] : []), ...(props.field.options || [])])
const enumShown = computed(() => {
  if (!props.state.present) return props.field.unset || fallback.value
  const option = props.field.showOptionLabel ? props.field.options?.find(item => item.value === String(props.state.value)) : undefined
  return option ? option.label : String(props.state.value)
})
const menuOpen = ref(false)
function updateEnum(value: string) {
  emit('change', value === UNSET ? undefined : value)
  menuOpen.value = false
}
const listCount = computed(() => Array.isArray(props.state.value) ? props.state.value.length : 0)
const boolOn = computed(() => props.state.value === undefined ? Boolean(props.field.coreDefault) : props.state.value === true)
const messages = computed(() => [...(draftError.value ? [{ message: draftError.value, blocking: true }] : []), ...(props.issues || [])])
const invalid = computed(() => messages.value.some(item => item.blocking))
</script>

<template>
  <MihomoBaseCell :label="label" :help="help" :html-for="field.type === 'bool' || field.type === 'int' || field.type === 'text' ? id : undefined"
    :status="status" :warn="!state.editable" :messages="messages" :data-base-field="pathId(field.path)">
    <div v-if="field.type === 'bool'" :class="CELL_SWITCH">
      <Switch :id="id" :aria-label="label" :model-value="boolOn" :disabled="locked" @update:model-value="emit('change', $event)" />
      <span :class="locked && 'opacity-60'">{{ boolOn ? '开' : '关' }}</span>
      <span v-if="!state.present" class="text-[11.5px] text-muted-foreground">默认</span>
    </div>

    <Popover v-else-if="field.type === 'enum'" v-model:open="menuOpen">
      <PopoverTrigger as-child>
        <button type="button" :class="CELL_TRIGGER" :disabled="locked" :aria-label="`${label}：${enumShown}`">
          <span class="min-w-0 flex-1 truncate" :class="!state.present ? 'text-muted-foreground' : field.showOptionLabel ? '' : 'font-mono'">{{ enumShown }}</span>
          <ChevronDown class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" :class="CELL_POPOVER">
        <p class="m-0 text-[13px] font-medium">{{ field.label }}</p>
        <RadioChips :model-value="unknownEnum ? '' : enumValue || (field.unset ? UNSET : '')" :options="enumOptions" :aria-label="label" @update:model-value="updateEnum" />
        <p v-if="unknownEnum" class="m-0 text-xs text-warning-accent">当前值 {{ String(state.value) }} 无效，请选择一项。</p>
        <p v-else-if="!state.present && !field.unset" class="m-0 text-xs text-muted-foreground">YAML 未填写，{{ field.coreDefault === undefined ? '使用 Mihomo 默认值' : `Mihomo 默认 ${shown(field.coreDefault)}` }}。</p>
      </PopoverContent>
    </Popover>

    <Popover v-else-if="field.type === 'list'">
      <PopoverTrigger as-child>
        <button type="button" :class="CELL_TRIGGER" :disabled="locked" :aria-label="`${label}：${listCount ? `${listCount} 条` : '未填写'}`" :aria-invalid="invalid">
          <span class="min-w-0 flex-1 truncate" :class="listCount ? 'text-primary-accent' : 'text-muted-foreground'">{{ listCount ? `${listCount} 条` : state.present ? '空列表' : '未填写' }}</span>
          <ChevronDown class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" :class="CELL_POPOVER">
        <label :for="id" class="text-[13px] font-medium">{{ field.label }}</label>
        <Textarea :id="id" :model-value="draft" :rows="8" :placeholder="field.placeholder" autocapitalize="off" :spellcheck="false" :aria-invalid="invalid"
          class="max-h-72 min-h-32 min-w-0 font-mono text-xs" @update:model-value="updateList(String($event))" />
        <p class="m-0 text-xs text-muted-foreground">每行一条，清空即从 YAML 删除这一项。</p>
      </PopoverContent>
    </Popover>

    <div v-else class="flex min-w-0 items-center gap-1">
      <input :id="id" :value="draft" :inputmode="field.type === 'int' ? 'numeric' : undefined" autocomplete="off" autocapitalize="off" :spellcheck="false"
        :placeholder="toggle !== undefined && !state.present ? '未启用' : field.placeholder && field.type === 'text' ? field.placeholder : fallback"
        :disabled="locked || (toggle !== undefined && !state.present)" :aria-invalid="invalid" :class="CELL_INPUT"
        @input="field.type === 'int' ? updateInt(($event.target as HTMLInputElement).value) : updateText(($event.target as HTMLInputElement).value)"
        @focus="remember" @keydown.enter.prevent="($event.target as HTMLElement).blur()" @keydown.esc.stop="revert" />
      <span v-if="field.unit" class="shrink-0 pr-1.5 text-xs text-muted-foreground">{{ field.unit }}</span>
      <label v-if="toggle !== undefined" class="flex min-h-11 min-w-11 shrink-0 cursor-pointer items-center justify-end pr-1.5 sm:min-h-8 sm:min-w-0">
        <Switch :aria-label="`启用${label}`" :model-value="state.present" :disabled="locked" @update:model-value="emit('change', $event ? toggle : undefined)" />
      </label>
    </div>
  </MihomoBaseCell>
</template>
