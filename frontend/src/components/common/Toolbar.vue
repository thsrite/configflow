<template>
  <div
    class="mb-4 flex flex-wrap items-center gap-2 rounded-xl border border-border/50 bg-card/45 p-2 backdrop-blur-xl"
  >
    <div v-if="searchable" class="relative min-w-[200px] flex-[1_1_220px]">
      <Search
        class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
        aria-hidden="true"
      />
      <Input
        :model-value="search"
        :placeholder="placeholder"
        class="h-9 border-transparent bg-background/50 pl-8 text-[13px] focus-visible:border-ring/60"
        @update:model-value="value => emit('update:search', String(value))"
      />
    </div>

    <slot name="filters" />

    <div class="ml-auto flex items-center gap-2">
      <slot name="actions" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { Search } from '@lucide/vue'
import { Input } from '@/components/ui/input'

withDefaults(
  defineProps<{ search?: string; placeholder?: string; searchable?: boolean }>(),
  { search: '', placeholder: '搜索…', searchable: true }
)

const emit = defineEmits<{ 'update:search': [value: string] }>()
</script>
