<template>
  <span class="inline-flex items-center gap-1.5 text-[12px] font-medium" :class="textClass">
    <span class="relative flex size-2 shrink-0">
      <span
        v-if="pulse"
        class="absolute inline-flex size-full animate-ping rounded-full opacity-70"
        :class="dotClass"
        aria-hidden="true"
      />
      <span class="relative inline-flex size-2 rounded-full" :class="dotClass" aria-hidden="true" />
    </span>
    <slot>{{ label }}</slot>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

type Tone = 'success' | 'warning' | 'danger' | 'info' | 'muted'

const props = withDefaults(defineProps<{ tone?: Tone; label?: string; pulse?: boolean }>(), {
  tone: 'muted',
  pulse: false
})

const dotClass = computed(
  () =>
    ({
      success: 'bg-success-accent',
      warning: 'bg-warning-accent',
      danger: 'bg-destructive-accent',
      info: 'bg-info-accent',
      muted: 'bg-muted-foreground'
    })[props.tone]
)

const textClass = computed(
  () =>
    ({
      success: 'text-success-accent',
      warning: 'text-warning-accent',
      danger: 'text-destructive-accent',
      info: 'text-info-accent',
      muted: 'text-muted-foreground'
    })[props.tone]
)
</script>
