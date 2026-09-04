<template>
  <section
    :class="cn(
      'hairline edge-light relative overflow-hidden rounded-xl border border-border/35 bg-card/55 backdrop-blur-xl transition-shadow duration-300',
      interactive && 'hover:shadow-glow-soft hover:border-border-strong/70',
      padded && 'p-5 max-md:p-4'
    )"
  >
    <header v-if="title || $slots.actions" :class="cn('flex flex-wrap items-center gap-3', padded ? 'mb-4' : 'px-5 pt-5 pb-3 max-md:px-4')">
      <div class="min-w-0 flex-1">
        <h2 v-if="title" class="m-0 flex items-center gap-2 text-[14px] font-semibold tracking-[-0.01em] text-foreground">
          <component v-if="icon" :is="icon" class="size-4 text-primary-accent" :stroke-width="2.2" aria-hidden="true" />
          {{ title }}
        </h2>
        <p v-if="description" class="mt-1 mb-0 text-[12px] leading-relaxed text-muted-foreground">
          {{ description }}
        </p>
      </div>
      <div v-if="$slots.actions" class="flex shrink-0 items-center gap-2">
        <slot name="actions" />
      </div>
    </header>

    <slot />
  </section>
</template>

<script setup lang="ts">
import type { Component } from 'vue'
import { cn } from '@/lib/utils'

withDefaults(
  defineProps<{
    title?: string
    description?: string
    icon?: Component
    /** 内容自带内边距时（例如整块是表格）传 false */
    padded?: boolean
    interactive?: boolean
  }>(),
  { padded: true, interactive: false }
)
</script>
