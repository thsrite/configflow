<script setup lang="ts">
import FieldHelp from '@/components/common/FieldHelp.vue'

/** One row of the base settings sheet: the group name, centred beside its cells, which stay in view. */
defineProps<{ title: string; section: string; help?: string; blocking?: number; warnings?: number; readonly?: boolean }>()
</script>

<template>
  <section class="grid min-w-0 gap-1 border-t border-border px-2 py-2.5 first:border-t-0 sm:grid-cols-[8.5rem_minmax(0,1fr)] sm:gap-3 sm:px-3"
    :class="readonly && 'bg-muted/40'" :aria-label="title" :data-base-section="section">
    <div class="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 px-1.5 sm:flex-col sm:items-start sm:self-center sm:px-2">
      <h3 class="m-0 flex items-center gap-0.5 text-[13px] font-semibold" :class="readonly && 'text-muted-foreground'">
        {{ title }}<FieldHelp v-if="help" :label="title" :text="help" />
      </h3>
      <span v-if="blocking" class="chip chip-bad" data-base-blocking-count>{{ blocking }} 项需修正</span>
      <span v-else-if="warnings" class="chip chip-warn" data-base-warning-count>{{ warnings }} 项提醒</span>
      <span v-if="readonly" class="chip">只读</span>
    </div>
    <div class="grid min-w-0 grid-cols-[repeat(auto-fill,minmax(10.5rem,1fr))] gap-x-1 gap-y-0.5" data-base-cells>
      <slot />
    </div>
  </section>
</template>
