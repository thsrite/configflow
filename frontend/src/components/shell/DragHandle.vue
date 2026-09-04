<template>
  <div class="cf-handle-group">
    <div class="cf-handle-arrows">
      <button
        type="button"
        class="cf-handle-arrow"
        :disabled="index === 0"
        :aria-label="`${label} 上移`"
        @click.stop="$emit('up')"
      >
        <el-icon><ArrowUp /></el-icon>
      </button>
      <button
        type="button"
        class="cf-handle-arrow"
        :disabled="index === total - 1"
        :aria-label="`${label} 下移`"
        @click.stop="$emit('down')"
      >
        <el-icon><ArrowDown /></el-icon>
      </button>
    </div>
    <button
      type="button"
      class="cf-handle"
      :class="{ 'is-grabbed': grabbed }"
      data-reorder-handle
      :aria-label="`拖动 ${label}，${position}${grabbed ? '，已抓取' : ''}`"
      :aria-pressed="grabbed"
      @keydown="$emit('keydown', $event)"
      @click.stop
    >
      <el-icon><Rank /></el-icon>
    </button>
  </div>
</template>

<script setup lang="ts">
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

<style scoped>
.cf-handle-group {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-1);
  flex: 0 0 auto;
}

.cf-handle-arrows {
  display: flex;
  gap: 4px;
}

.cf-handle-arrow {
  width: 36px;
  height: 34px;
  border-radius: var(--cf-r-sm);
  border: 1px solid var(--cf-bd);
  background: var(--cf-s2);
  color: var(--cf-fg-2);
  font-size: 11px;
  display: grid;
  place-items: center;
  cursor: pointer;
}

.cf-handle-arrow:disabled {
  opacity: 0.3;
  cursor: default;
}

/* 手柄自身是 44px 触控热区，满足移动端最小点击目标 */
.cf-handle {
  width: var(--cf-touch);
  height: var(--cf-touch);
  border-radius: var(--cf-r-md);
  border: 1px solid transparent;
  background: none;
  color: var(--cf-fg-3);
  display: grid;
  place-items: center;
  font-size: 15px;
  cursor: grab;
  touch-action: none;
}

.cf-handle:hover {
  color: var(--cf-fg);
  background: var(--cf-s2);
}

.cf-handle.is-grabbed {
  color: var(--cf-primary);
  border-color: var(--cf-primary);
  background: var(--cf-primary-soft);
  cursor: grabbing;
}
</style>
