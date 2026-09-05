<template>
  <div
    class="flex items-start gap-2.5 rounded-lg border border-info-accent/25 bg-info-soft/40 px-3.5 py-3 text-[12.5px] leading-relaxed text-muted-foreground"
    role="note"
  >
    <Info class="mt-px size-3.5 shrink-0 text-info-accent" aria-hidden="true" />
    <div class="min-w-0 [&>p]:m-0 [&>p+p]:mt-1.5">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import { Info } from '@lucide/vue'
</script>
