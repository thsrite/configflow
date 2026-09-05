<template>
  <div
    class="hairline edge-light group relative overflow-hidden rounded-xl border border-border/35 bg-card/55 p-4 backdrop-blur-xl transition-all duration-300 hover:-translate-y-0.5 hover:shadow-glow-soft"
  >
    <!-- 悬停时的定向辉光，强调当前关注的指标 -->
    <span
      class="pointer-events-none absolute -top-16 -right-10 size-32 rounded-full opacity-0 blur-3xl transition-opacity duration-500 group-hover:opacity-100"
      :class="glowClass"
      aria-hidden="true"
    />

    <div class="relative flex items-start justify-between gap-3">
      <div class="min-w-0">
        <p class="m-0 truncate text-[12px] font-medium tracking-[0.02em] text-muted-foreground">
          {{ label }}
        </p>
        <p class="mt-2 mb-0 flex items-baseline gap-1 text-[28px] leading-none font-semibold tracking-[-0.03em] text-foreground">
          <AnimatedNumber :value="value" :precision="precision" />
          <span v-if="unit" class="text-[13px] font-medium text-muted-foreground">{{ unit }}</span>
        </p>
      </div>

      <div
        v-if="icon"
        class="flex size-9 shrink-0 items-center justify-center rounded-lg border border-border/60 bg-background/50"
        :class="iconClass"
      >
        <component :is="icon" class="size-4.5" :stroke-width="2" aria-hidden="true" />
      </div>
    </div>

    <div v-if="hint || $slots.default" class="relative mt-3 flex items-center gap-2 text-[11.5px] text-muted-foreground">
      <slot>{{ hint }}</slot>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, type Component } from 'vue'
import AnimatedNumber from './AnimatedNumber.vue'

type Tone = 'primary' | 'success' | 'warning' | 'danger' | 'info'

const props = withDefaults(
  defineProps<{
    label: string
    value: number
    unit?: string
    hint?: string
    icon?: Component
    tone?: Tone
    precision?: number
  }>(),
  { tone: 'primary', precision: 0 }
)

const glowClass = computed(
  () =>
    ({
      primary: 'bg-primary/25',
      success: 'bg-success/25',
      warning: 'bg-warning/25',
      danger: 'bg-destructive/25',
      info: 'bg-info/25'
    })[props.tone]
)

const iconClass = computed(
  () =>
    ({
      primary: 'text-primary-accent',
      success: 'text-success-accent',
      warning: 'text-warning-accent',
      danger: 'text-destructive-accent',
      info: 'text-info-accent'
    })[props.tone]
)
</script>
