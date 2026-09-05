<template>
  <div class="flex flex-col gap-1.5">
    <Label v-if="label" :for="htmlFor">{{ label }}</Label>
    <slot />
    <p v-if="hint" class="m-0 text-[12px] leading-relaxed whitespace-pre-line text-muted-foreground">
      {{ hint }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { Label } from '@/components/ui/label'

defineProps<{ label?: string; hint?: string; htmlFor?: string }>()
</script>
