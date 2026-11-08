<script setup lang="ts">
import { computed } from 'vue'
import { Circle, CircleCheck, CircleX, Loader2, TriangleAlert } from '@lucide/vue'
import type { IPv6StepTone } from '@/lib/mihomoIPv6Steps'

/**
 * One line of the IPv6 checklist: icon, name and a status pill side by side, the
 * reason in the next column (under the name on a narrow window). Columns are fixed
 * so the rows of a group line up; sizes follow the `@container/steps` around them.
 */
const props = defineProps<{ label: string; status: string; tone: IPv6StepTone }>()
const look = computed(() => ({
  success: { icon: CircleCheck, iconClass: 'text-success-accent', pill: 'bg-success-soft text-success-accent' },
  warning: { icon: TriangleAlert, iconClass: 'text-warning-accent', pill: 'bg-warning-soft text-warning-accent' },
  danger: { icon: CircleX, iconClass: 'text-destructive-accent', pill: 'bg-destructive-soft text-destructive-accent' },
  progress: { icon: Loader2, iconClass: 'text-primary-accent animate-spin motion-reduce:animate-none', pill: 'bg-primary-soft text-primary-accent' },
  neutral: { icon: Circle, iconClass: 'text-muted-foreground', pill: 'bg-muted text-muted-foreground' },
})[props.tone])
</script>

<template>
  <section class="grid min-w-0 grid-cols-[1rem_minmax(0,1fr)_auto] items-start gap-x-2.5 gap-y-1 py-2.5 @3xl/steps:grid-cols-[1rem_minmax(0,12rem)_7.5rem_minmax(0,1fr)]"
    :aria-label="label" :data-tone="tone">
    <component :is="look.icon" class="mt-0.5 size-4 shrink-0" :class="look.iconClass" aria-hidden="true" />
    <span class="min-w-0 text-[13px] leading-5 [overflow-wrap:anywhere]" :class="tone === 'success' ? 'text-foreground/80' : 'font-semibold text-foreground'">{{ label }}</span>
    <span class="justify-self-end whitespace-nowrap rounded-full px-2 py-0.5 text-xs leading-4 @3xl/steps:justify-self-start" :class="look.pill"
      aria-live="polite" aria-atomic="true" :aria-label="`${label}：${status}`" data-status>{{ status }}</span>
    <div v-if="$slots.notice" class="col-span-2 col-start-2 min-w-0 space-y-1.5 text-xs leading-5 text-foreground [overflow-wrap:anywhere] @3xl/steps:col-span-1 @3xl/steps:col-start-4 @3xl/steps:row-start-1"
      aria-live="polite" data-status-notice>
      <slot name="notice" />
    </div>
  </section>
</template>
