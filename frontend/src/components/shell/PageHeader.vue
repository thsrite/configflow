<template>
  <header class="mb-5 flex flex-wrap items-start gap-4">
    <div class="min-w-0 flex-[1_1_260px]">
      <h1 class="m-0 text-[22px] leading-tight font-semibold tracking-[-0.02em] text-foreground">
        {{ title }}
      </h1>
      <p v-if="description" class="mt-1.5 mb-0 text-[13px] leading-relaxed text-muted-foreground">
        {{ description }}
      </p>
    </div>
    <!-- 一个视图只有一个主操作，其余降级为次要/图标操作 -->
    <div v-if="$slots.actions" class="flex shrink-0 items-center gap-2 max-md:w-full">
      <slot name="actions" />
    </div>
  </header>
</template>

<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>
