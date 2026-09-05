<template>
  <header class="relative mb-6 flex flex-wrap items-end gap-4">
    <div class="min-w-0 flex-[1_1_280px]">
      <div
        v-if="eyebrow"
        class="mb-2 flex items-center gap-2 text-[11px] font-semibold tracking-[0.14em] text-muted-foreground uppercase"
      >
        <span class="h-px w-6 bg-linear-to-r from-primary-accent to-transparent" aria-hidden="true" />
        {{ eyebrow }}
      </div>
      <h1
        class="m-0 text-[26px] leading-[1.15] font-semibold tracking-[-0.025em] text-foreground max-md:text-[21px]"
      >
        {{ title }}
      </h1>
      <p v-if="description" class="mt-2 mb-0 max-w-[68ch] text-[13px] leading-relaxed text-muted-foreground">
        {{ description }}
      </p>
    </div>

    <div v-if="$slots.actions" class="flex shrink-0 items-center gap-2 max-md:w-full">
      <slot name="actions" />
    </div>
  </header>
</template>

<script setup lang="ts">
defineProps<{ title: string; description?: string; eyebrow?: string }>()
</script>
