<template>
  <div class="flex shrink-0 items-center gap-1">
    <div class="flex gap-1">
      <button
        type="button"
        class="grid h-8.5 w-9 place-items-center rounded-sm border border-border bg-secondary text-muted-foreground transition-colors hover:text-foreground disabled:cursor-default disabled:opacity-30 disabled:hover:text-muted-foreground"
        :disabled="index === 0"
        :aria-label="`${label} 上移`"
        @click.stop="$emit('up')"
      >
        <ArrowUp class="size-3.5" :stroke-width="2.5" />
      </button>
      <button
        type="button"
        class="grid h-8.5 w-9 place-items-center rounded-sm border border-border bg-secondary text-muted-foreground transition-colors hover:text-foreground disabled:cursor-default disabled:opacity-30 disabled:hover:text-muted-foreground"
        :disabled="index === total - 1"
        :aria-label="`${label} 下移`"
        @click.stop="$emit('down')"
      >
        <ArrowDown class="size-3.5" :stroke-width="2.5" />
      </button>
    </div>
    <!-- 手柄自身是 44px 触控热区，满足移动端最小点击目标 -->
    <button
      type="button"
      :class="cn(
        'grid size-(--cf-touch) cursor-grab place-items-center rounded-md border border-transparent bg-transparent text-muted-foreground [touch-action:none] transition-colors hover:bg-secondary hover:text-foreground',
        grabbed && 'cursor-grabbing border-primary-accent bg-primary-soft text-primary-accent'
      )"
      data-reorder-handle
      :aria-label="`拖动 ${label}，${position}${grabbed ? '，已抓取' : ''}`"
      :aria-pressed="grabbed"
      @keydown="$emit('keydown', $event)"
      @click.stop
    >
      <GripVertical class="size-4" :stroke-width="2" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { ArrowDown, ArrowUp, GripVertical } from '@lucide/vue'
import { cn } from '@/lib/utils'

defineProps<{
  label: string
  index: number
  total: number
  position: string
  grabbed?: boolean
}>()

defineEmits<{
  (e: 'up'): void
  (e: 'down'): void
  (e: 'keydown', event: KeyboardEvent): void
}>()
</script>
