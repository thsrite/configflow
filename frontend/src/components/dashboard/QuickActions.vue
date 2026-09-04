<template>
  <section class="cf-panel" aria-label="快速操作">
    <h2 class="cf-panel__title">快速操作</h2>
    <div class="cf-quick">
      <button
        v-for="action in actions"
        :key="action.label"
        type="button"
        class="cf-quick__item"
        :class="{ 'is-primary': action.primary }"
        :disabled="action.disabled"
        @click="$emit('run', action)"
      >
        <el-icon class="cf-quick__icon"><component :is="action.icon" /></el-icon>
        <span class="cf-quick__label">{{ action.label }}</span>
        <el-icon class="cf-quick__chev"><ArrowRight /></el-icon>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
export interface QuickAction {
  label: string
  icon: string
  /** 一个视图只有一个主操作 */
  primary?: boolean
  disabled?: boolean
  route?: string
}

defineProps<{ actions: QuickAction[] }>()
defineEmits<{ (e: 'run', action: QuickAction): void }>()
</script>

<style scoped>
.cf-panel {
  background: var(--cf-s1);
  border: 1px solid var(--cf-bd);
  border-radius: var(--cf-r-xl);
  box-shadow: var(--cf-shadow);
  padding: var(--cf-sp-4);
}

.cf-panel__title {
  font-size: 15px;
  font-weight: 650;
  margin: 0 0 var(--cf-sp-3);
  color: var(--cf-fg);
}

.cf-quick {
  display: flex;
  flex-direction: column;
  gap: var(--cf-sp-2);
}

.cf-quick__item {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  min-height: 40px;
  padding: 0 12px;
  border-radius: var(--cf-r-md);
  border: 1px solid var(--cf-bd);
  background: var(--cf-s2);
  color: var(--cf-fg);
  font-family: inherit;
  font-size: 13.5px;
  font-weight: 600;
  cursor: pointer;
  text-align: left;
}

.cf-quick__item:hover:not(:disabled) {
  border-color: var(--cf-bd-strong);
}

.cf-quick__item:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.cf-quick__item.is-primary {
  background: var(--cf-primary-fill);
  border-color: var(--cf-primary-fill);
  color: var(--cf-primary-fg);
}

.cf-quick__label {
  flex: 1 1 auto;
  min-width: 0;
}

.cf-quick__icon {
  font-size: 15px;
  flex: 0 0 auto;
}

.cf-quick__chev {
  flex: 0 0 auto;
  opacity: 0.6;
}
</style>
