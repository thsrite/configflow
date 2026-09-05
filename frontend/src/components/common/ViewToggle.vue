<template>
  <div
    class="relative flex h-9 shrink-0 items-center gap-0.5 rounded-lg border border-border/50 bg-background/40 p-0.5"
    role="group"
    aria-label="视图切换"
  >
    <button
      v-for="option in OPTIONS"
      :key="option.value"
      type="button"
      :aria-pressed="modelValue === option.value"
      :aria-label="option.label"
      :title="option.label"
      :class="[
        'relative grid size-8 cursor-pointer place-items-center rounded-md border-0 bg-transparent p-0 transition-colors',
        modelValue === option.value ? 'text-foreground' : 'text-muted-foreground hover:text-foreground'
      ]"
      @click="$emit('update:modelValue', option.value)"
    >
      <Motion
        v-if="modelValue === option.value"
        layout-id="view-toggle"
        class="absolute inset-0 rounded-md border border-border/60 bg-accent/70"
        :transition="SPRING"
        aria-hidden="true"
      />
      <component :is="option.icon" class="relative size-4" :stroke-width="2" aria-hidden="true" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { Motion } from 'motion-v'
import { LayoutGrid, List } from '@lucide/vue'
import { SPRING } from '@/lib/motion'

export type ViewMode = 'list' | 'card'

defineProps<{ modelValue: ViewMode }>()
defineEmits<{ (e: 'update:modelValue', value: ViewMode): void }>()

const OPTIONS = [
  { value: 'list' as const, label: '列表视图', icon: List },
  { value: 'card' as const, label: '卡片视图', icon: LayoutGrid }
]
</script>
