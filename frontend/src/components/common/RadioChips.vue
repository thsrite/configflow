<script setup lang="ts" generic="T extends string">
import type { HTMLAttributes } from 'vue'
import type { AcceptableValue } from 'reka-ui'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'

/**
 * 单选胶囊：表单字段的单选，每项是带圆点的胶囊（Agent 安装弹窗同款）。
 * 方向键在组内切换（reka-ui RadioGroup）。aria-label 等属性透传到 radiogroup。
 * modelValue 不在选项中（如 ''）时不选中任何项。
 */
const props = defineProps<{
  modelValue?: T | ''
  options: ReadonlyArray<{ value: T; label: string; disabled?: boolean }>
  disabled?: boolean
  /** 外层布局类，整体替换默认的 `flex flex-wrap gap-2` */
  class?: HTMLAttributes['class']
  /** 追加到每个胶囊上的类 */
  itemClass?: HTMLAttributes['class']
}>()
const emit = defineEmits<{ 'update:modelValue': [value: T] }>()

const BASE = 'flex min-h-11 items-center gap-2 rounded-lg border px-3 py-2 text-[13px] transition-colors'
const ACTIVE = 'border-primary-accent bg-primary-soft text-primary-accent dark:border-primary-accent/40 dark:bg-primary-soft/50 dark:text-foreground'
const IDLE = 'border-input bg-card text-muted-foreground dark:border-border/50 dark:bg-background/40'

const chipClass = (active: boolean, disabled: boolean): HTMLAttributes['class'] => [
  BASE,
  disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer',
  active ? ACTIVE : IDLE,
  !active && !disabled && 'hover:border-border-strong',
  props.itemClass,
]
const select = (value: AcceptableValue) => emit('update:modelValue', value as T)
</script>

<template>
  <RadioGroup :model-value="modelValue" :disabled="disabled" :class="props.class ?? 'flex flex-wrap gap-2'" @update:model-value="select">
    <label v-for="option in options" :key="option.value" :class="chipClass(modelValue === option.value, !!(disabled || option.disabled))">
      <RadioGroupItem :value="option.value" :disabled="option.disabled" :class="(disabled || option.disabled) && 'disabled:opacity-100'" />{{ option.label }}
    </label>
  </RadioGroup>
</template>
