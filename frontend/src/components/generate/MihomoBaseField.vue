<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import FormField from '@/components/common/FormField.vue'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { BaseFieldDef } from '@/lib/mihomoBaseCopy'
import { parseLines, pathId, type BaseFieldState, type BaseIssue } from '@/lib/mihomoBaseSettings'

/** One base setting, read from and written to the YAML through its parent. */
// `toggle`: an optional key shown with an on / off switch; switching on writes this value.
const props = defineProps<{ field: BaseFieldDef; state: BaseFieldState; issues?: BaseIssue[]; disabled?: boolean; note?: string; toggle?: number }>()
const emit = defineEmits<{ change: [value: unknown]; draft: [error: string] }>()
const UNSET = '__unset__'
const id = computed(() => `mihomo-base-${pathId(props.field.path).replace(/[^a-z0-9]+/gi, '-')}`)
const locked = computed(() => props.disabled || !props.state.editable)
const shown = (value: unknown) => typeof value === 'boolean' ? (value ? '开启' : '关闭') : String(value)
const status = computed(() => {
  if (!props.state.editable) return props.state.reason
  if (!props.state.present && props.toggle !== undefined) return '未启用，YAML 中没有此项。'
  if (!props.state.present) {
    return props.field.coreDefault === undefined ? 'YAML 未填写，使用 Mihomo 默认值。' : `YAML 未填写，Mihomo 默认：${shown(props.field.coreDefault)}。`
  }
  return props.note || ''
})
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
const enumValue = computed(() => typeof props.state.value === 'string' || typeof props.state.value === 'number'
  ? String(props.state.value).toLowerCase() : typeof props.state.value === 'boolean' && pathId(props.field.path) === 'find-process-mode'
    ? (props.state.value ? 'always' : 'off') : '')
const unknownEnum = computed(() => props.state.present && !props.field.options?.some(option => option.value === enumValue.value))
function updateEnum(value: unknown) {
  if (typeof value !== 'string') return
  emit('change', value === UNSET ? undefined : value)
}
const messages = computed(() => [...(draftError.value ? [{ message: draftError.value, blocking: true }] : []), ...(props.issues || [])])
</script>

<template>
  <FormField :label="field.label" :html-for="id" :help="field.help" :class="field.wide && 'sm:col-span-2'" :data-base-field="pathId(field.path)">
    <div v-if="field.type === 'bool'" class="flex min-h-11 min-w-0 items-center gap-3">
      <Switch :id="id" :aria-label="field.label" :model-value="state.value === undefined ? Boolean(field.coreDefault) : state.value === true"
        :disabled="locked" @update:model-value="emit('change', $event)" />
      <label :for="id" class="min-w-0 cursor-pointer text-[13px]" :class="locked && 'cursor-not-allowed opacity-60'">
        {{ (state.value === undefined ? Boolean(field.coreDefault) : state.value === true) ? '开启' : '关闭' }}
      </label>
    </div>
    <Select v-else-if="field.type === 'enum'" :model-value="unknownEnum ? undefined : enumValue || undefined" :disabled="locked" @update:model-value="updateEnum">
      <SelectTrigger :id="id" class="min-h-11 w-full"><SelectValue :placeholder="unknownEnum ? `当前值 ${String(state.value)} 无效` : `未填写（Mihomo 默认 ${field.coreDefault ?? '无'}）`" /></SelectTrigger>
      <SelectContent>
        <SelectItem v-if="field.unset" :value="UNSET">{{ field.unset }}</SelectItem>
        <SelectItem v-for="option in field.options" :key="option.value" :value="option.value">{{ option.label }}</SelectItem>
      </SelectContent>
    </Select>
    <div v-else-if="field.type === 'int'" class="flex min-w-0 items-center gap-2">
      <label v-if="toggle !== undefined" class="flex min-h-11 shrink-0 cursor-pointer items-center gap-2 text-[13px]" :class="locked && 'cursor-not-allowed opacity-60'">
        <Switch :aria-label="`启用${field.label}`" :model-value="state.present" :disabled="locked"
          @update:model-value="emit('change', $event ? toggle : undefined)" />
        <span>{{ state.present ? '启用' : '关闭' }}</span>
      </label>
      <Input :id="id" :model-value="draft" inputmode="numeric" autocomplete="off" :placeholder="field.placeholder || '未填写'" :disabled="locked || (toggle !== undefined && !state.present)"
        :aria-invalid="messages.some(item => item.blocking)" class="min-h-11 w-36 font-mono" @update:model-value="updateInt(String($event))" />
      <span v-if="field.unit" class="text-xs text-muted-foreground">{{ field.unit }}</span>
    </div>
    <Textarea v-else-if="field.type === 'list'" :id="id" :model-value="draft" :rows="4" :placeholder="field.placeholder" :disabled="locked"
      autocapitalize="off" :spellcheck="false" :aria-invalid="messages.some(item => item.blocking)" class="max-h-56 min-h-24 min-w-0 font-mono text-xs"
      @update:model-value="updateList(String($event))" />
    <Input v-else :id="id" :model-value="draft" autocomplete="off" autocapitalize="off" :spellcheck="false" :placeholder="field.placeholder" :disabled="locked"
      :aria-invalid="messages.some(item => item.blocking)" class="min-h-11 min-w-0 font-mono" @update:model-value="updateText(String($event))" />
    <p v-if="status" class="m-0 break-words text-xs leading-relaxed" :class="state.editable ? 'text-muted-foreground' : 'text-warning-accent'">{{ status }}</p>
    <p v-for="item in messages" :key="item.message" class="m-0 break-words text-xs leading-relaxed"
      :class="item.blocking ? 'text-destructive-accent' : 'text-warning-accent'" :role="item.blocking ? 'alert' : undefined" data-base-issue>
      {{ item.blocking ? item.message : `已保存的配置：${item.message}` }}
    </p>
  </FormField>
</template>
