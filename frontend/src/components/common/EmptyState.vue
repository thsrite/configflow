<template>
  <div class="flex flex-col items-center justify-center gap-3 px-6 py-14 text-center">
    <div
      class="relative flex size-14 items-center justify-center rounded-2xl border border-border/60 bg-card/60 text-muted-foreground"
    >
      <span
        class="absolute inset-0 rounded-2xl bg-linear-to-br from-primary/16 to-accent-2/10"
        aria-hidden="true"
      />
      <component :is="icon ?? Inbox" class="relative size-6" :stroke-width="1.6" aria-hidden="true" />
    </div>
    <p class="m-0 text-[14px] font-medium text-foreground">{{ title }}</p>
    <p v-if="description" class="m-0 max-w-[46ch] text-[12.5px] leading-relaxed text-muted-foreground">
      {{ description }}
    </p>
    <div v-if="$slots.default" class="mt-2 flex items-center gap-2">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Component } from 'vue'
import { Inbox } from '@lucide/vue'

defineProps<{ title: string; description?: string; icon?: Component }>()
</script>
