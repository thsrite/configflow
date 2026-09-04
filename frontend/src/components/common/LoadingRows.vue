<template>
  <div class="space-y-2 p-4">
    <Skeleton v-for="i in rows" :key="i" class="h-11 w-full rounded-lg" :style="{ opacity: 1 - i * 0.08 }" />
  </div>
</template>

<script setup lang="ts">
import { Skeleton } from '@/components/ui/skeleton'

withDefaults(defineProps<{ rows?: number }>(), { rows: 5 })
</script>
