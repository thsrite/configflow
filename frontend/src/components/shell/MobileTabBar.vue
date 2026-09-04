<template>
  <nav class="cf-tabs" aria-label="主导航">
    <button
      v-for="group in groups"
      :key="group.scope"
      type="button"
      class="cf-tabs__item"
      :aria-current="group.scope === activeScope ? 'page' : undefined"
      @click="$emit('select', group)"
    >
      <el-icon class="cf-tabs__icon"><component :is="group.tabIcon" /></el-icon>
      <span>{{ group.tabLabel }}</span>
    </button>
  </nav>
</template>

<script setup lang="ts">
import { NAV_GROUPS, type NavGroup, type NavScope } from '@/navigation'

defineProps<{ activeScope?: NavScope }>()
defineEmits<{ (e: 'select', group: NavGroup): void }>()

const groups = NAV_GROUPS
</script>

<style scoped>
.cf-tabs {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 900;
  /* 仅移动端出现 */
  display: none;
  grid-template-columns: repeat(4, 1fr);
  background: color-mix(in srgb, var(--cf-bg) 92%, transparent);
  backdrop-filter: saturate(1.4) blur(16px);
  border-top: 1px solid var(--cf-bd);
  padding-bottom: env(safe-area-inset-bottom);
}

.cf-tabs__item {
  background: none;
  border: none;
  padding: 7px 0 6px;
  min-height: var(--cf-tabbar-h);
  color: var(--cf-fg-3);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
  font-size: 10.5px;
  font-weight: 600;
  font-family: inherit;
  cursor: pointer;
}

.cf-tabs__item[aria-current='page'] {
  color: var(--cf-primary);
}

.cf-tabs__icon {
  font-size: 19px;
}

@media (max-width: 900px) {
  .cf-tabs {
    display: grid;
  }
}
</style>
