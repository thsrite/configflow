<template>
  <div class="flex items-center gap-3 pt-1">
    <span class="text-[11px] font-semibold tracking-[0.1em] text-muted-foreground uppercase">
      {{ label }}
    </span>
    <span class="h-px flex-1 bg-border/60" aria-hidden="true" />
  </div>
</template>

<script setup lang="ts">
defineProps<{ label: string }>()
</script>
