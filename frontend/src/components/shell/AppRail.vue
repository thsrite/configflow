<template>
  <nav class="cf-rail" aria-label="主导航">
    <template v-for="group in groups" :key="group.scope">
      <div v-if="group.title" class="cf-rail__title" :class="`is-${group.scope}`">
        <span class="cf-rail__mark" aria-hidden="true"></span>
        <span class="cf-rail__title-text">{{ groupTitle(group) }}</span>
      </div>
      <router-link
        v-for="item in visibleItems(group)"
        :key="item.path"
        :to="item.path"
        class="cf-rail__item"
        :class="{ 'is-active': activePath === item.path }"
      >
        <el-icon class="cf-rail__icon"><component :is="item.icon" /></el-icon>
        <span class="cf-rail__label">{{ item.label }}</span>
      </router-link>
    </template>
  </nav>
</template>

<script setup lang="ts">
import { NAV_GROUPS, type NavGroup, type NavItem } from '@/navigation'

const props = defineProps<{
  activePath: string
  profileName: string
  subscriptionAggregationEnabled: boolean
}>()

const groups = NAV_GROUPS

const groupTitle = (group: NavGroup): string =>
  group.scope === 'profile' ? `当前配置 · ${props.profileName}` : group.title || ''

const visibleItems = (group: NavGroup): NavItem[] =>
  group.items.filter(
    item => item.flag !== 'subscriptionAggregation' || props.subscriptionAggregationEnabled
  )
</script>

<style scoped>
.cf-rail {
  width: var(--cf-rail-w);
  flex: 0 0 var(--cf-rail-w);
  border-right: 1px solid var(--cf-bd);
  background: var(--cf-bg);
  padding: var(--cf-sp-3) var(--cf-sp-2) var(--cf-sp-5);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.cf-rail__title {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: var(--cf-sp-4) 0 var(--cf-sp-2);
  padding: 0 var(--cf-sp-2);
  font-size: 11px;
  font-weight: 650;
  letter-spacing: 0.06em;
  color: var(--cf-fg-3);
  text-transform: uppercase;
}

/* 作用域同时用图标色标 + 分组标题区分，不只靠颜色 */
.cf-rail__mark {
  width: 6px;
  height: 6px;
  border-radius: 2px;
  flex: 0 0 auto;
  background: var(--cf-fg-3);
}
.cf-rail__title.is-resource .cf-rail__mark {
  background: var(--cf-shared);
}
.cf-rail__title.is-profile .cf-rail__mark {
  background: var(--cf-profile);
}

.cf-rail__title-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cf-rail__item {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  min-height: var(--cf-ctrl-h);
  padding: 0 var(--cf-sp-2);
  border-radius: var(--cf-r-md);
  color: var(--cf-fg-2);
  font-size: 13.5px;
  font-weight: 550;
  text-decoration: none;
  transition: background var(--cf-dur) var(--cf-ease), color var(--cf-dur) var(--cf-ease);
}

.cf-rail__item:hover {
  background: var(--cf-s2);
  color: var(--cf-fg);
}

.cf-rail__item.is-active {
  background: var(--cf-s3);
  color: var(--cf-fg);
  font-weight: 650;
}

.cf-rail__icon {
  font-size: 15px;
  flex: 0 0 auto;
}

.cf-rail__label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
