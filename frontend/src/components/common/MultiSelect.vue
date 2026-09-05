<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button
        type="button"
        role="combobox"
        :aria-expanded="open"
        class="flex min-h-9 w-full cursor-pointer items-center gap-2 rounded-md border border-input bg-background/50 px-3 py-1.5 text-left text-[13px] transition-colors hover:border-border-strong focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <span v-if="!selected.length" class="flex-1 text-muted-foreground">{{ placeholder }}</span>
        <span v-else class="flex min-w-0 flex-1 flex-wrap gap-1">
          <!-- 选中项超过 maxVisible 时折叠成计数，避免触发器高度失控 -->
          <Badge
            v-for="option in visibleSelected"
            :key="option.value"
            variant="secondary"
            class="max-w-[180px] gap-1 truncate"
          >
            {{ option.label }}
          </Badge>
          <Badge v-if="hiddenCount" variant="outline">+{{ hiddenCount }}</Badge>
        </span>
        <ChevronsUpDown class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
      </button>
    </PopoverTrigger>

    <PopoverContent class="glass-strong w-(--reka-popper-anchor-width) p-0" align="start">
      <Command :filter-function="filterOptions">
        <CommandInput :placeholder="searchPlaceholder" />
        <CommandList>
          <CommandEmpty>没有匹配项</CommandEmpty>
          <CommandGroup>
            <CommandItem
              v-for="option in options"
              :key="option.value"
              :value="option"
              @select="toggle(option.value)"
            >
              <Check
                class="size-4"
                :class="modelValue.includes(option.value) ? 'opacity-100 text-primary-accent' : 'opacity-0'"
                aria-hidden="true"
              />
              <span class="min-w-0 truncate">{{ option.label }}</span>
            </CommandItem>
          </CommandGroup>
        </CommandList>
      </Command>
    </PopoverContent>
  </Popover>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { Check, ChevronsUpDown } from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList
} from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

export interface MultiSelectOption {
  value: string
  label: string
}

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    options: MultiSelectOption[]
    placeholder?: string
    searchPlaceholder?: string
    maxVisible?: number
  }>(),
  { placeholder: '请选择', searchPlaceholder: '搜索…', maxVisible: 6 }
)

const emit = defineEmits<{ (e: 'update:modelValue', value: string[]): void }>()

const open = ref(false)

const selected = computed(() =>
  props.modelValue
    .map(value => props.options.find(option => option.value === value))
    .filter((option): option is MultiSelectOption => Boolean(option))
)

const visibleSelected = computed(() => selected.value.slice(0, props.maxVisible))
const hiddenCount = computed(() => Math.max(0, selected.value.length - props.maxVisible))

const toggle = (value: string): void => {
  const next = props.modelValue.includes(value)
    ? props.modelValue.filter(item => item !== value)
    : [...props.modelValue, value]
  emit('update:modelValue', next)
}

/* Command 默认按 value 的字符串形式过滤；这里传的是对象，改为按 label 匹配 */
const filterOptions = (list: unknown[], term: string) => {
  const q = term.trim().toLowerCase()
  if (!q) return list
  return (list as MultiSelectOption[]).filter(option =>
    option.label.toLowerCase().includes(q)
  )
}
</script>
