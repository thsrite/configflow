<script setup lang="ts">
/**
 * A group of the IPv6 checklist, laid out like the base settings sheet: the name,
 * its progress and the group's own action centred on the left, the rows on the
 * right; on a narrow window they sit in one line above the rows.
 */
defineProps<{ title: string; progress?: string }>()
</script>

<template>
  <section class="grid min-w-0 gap-x-3 border-t border-border px-3 pb-1 first:border-t-0 @xl/steps:grid-cols-[8.5rem_minmax(0,1fr)] @xl/steps:px-4" :aria-label="title">
    <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 pt-2.5 @xl/steps:flex-col @xl/steps:items-start @xl/steps:self-center @xl/steps:py-2.5">
      <h4 class="m-0 text-[13px] font-semibold">{{ title }}</h4>
      <span v-if="progress" class="text-xs text-muted-foreground" data-ipv6-group-progress>{{ progress }}</span>
      <div v-if="$slots.actions" class="ml-auto @xl/steps:ml-0 @xl/steps:mt-1"><slot name="actions" /></div>
    </div>
    <div class="min-w-0 divide-y divide-border/60"><slot /></div>
  </section>
</template>
