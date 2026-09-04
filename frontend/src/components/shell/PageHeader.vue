<template>
  <header class="cf-page-header">
    <div class="cf-page-header__text">
      <h1 class="cf-page-header__title">{{ title }}</h1>
      <p v-if="description" class="cf-page-header__desc">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="cf-page-header__actions">
      <slot name="actions" />
    </div>
  </header>
</template>

<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>

<style scoped>
.cf-page-header {
  display: flex;
  align-items: flex-start;
  gap: var(--cf-sp-4);
  margin-bottom: var(--cf-sp-4);
  flex-wrap: wrap;
}

.cf-page-header__text {
  flex: 1 1 260px;
  min-width: 0;
}

.cf-page-header__title {
  font-size: 23px;
  font-weight: 700;
  letter-spacing: -0.02em;
  margin: 0 0 3px;
  color: var(--cf-fg);
}

.cf-page-header__desc {
  margin: 0;
  font-size: 13px;
  color: var(--cf-fg-2);
}

/* 一个视图只有一个主操作，其余降级为次要/图标操作 */
.cf-page-header__actions {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  flex: 0 0 auto;
}

@media (max-width: 768px) {
  .cf-page-header__title {
    font-size: 21px;
  }
  .cf-page-header__actions {
    width: 100%;
  }
}
</style>
