<template>
  <!-- 当前分组内的页面切换：横向分段控件，就地切换不弹层 -->
  <nav v-if="items.length > 1" class="cf-subnav" aria-label="分组内页面">
    <router-link
      v-for="item in items"
      :key="item.path"
      :to="item.path"
      class="cf-subnav__item"
      :class="{ 'is-active': activePath === item.path }"
    >
      <el-icon class="cf-subnav__icon"><component :is="item.icon" /></el-icon>
      <span>{{ item.label }}</span>
    </router-link>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NAV_GROUPS, scopeOfPath, type NavItem } from '@/navigation'

const props = defineProps<{
  activePath: string
  subscriptionAggregationEnabled: boolean
}>()

const items = computed<NavItem[]>(() => {
  const scope = scopeOfPath(props.activePath)
  const group = NAV_GROUPS.find(g => g.scope === scope)
  if (!group) return []
  return group.items.filter(
    item =>
      item.flag !== 'subscriptionAggregation' || props.subscriptionAggregationEnabled
  )
})
</script>

<style scoped>
.cf-subnav {
  /* 仅移动端出现；桌面由左侧 rail 承担分组导航 */
  display: none;
  gap: var(--cf-sp-2);
  overflow-x: auto;
  scrollbar-width: none;
  /* 贴边滚动，两侧留出与内容一致的呼吸 */
  margin: 0 calc(var(--cf-sp-4) * -1) var(--cf-sp-4);
  padding: 0 var(--cf-sp-4);
  scroll-snap-type: x proximity;
}

.cf-subnav::-webkit-scrollbar {
  display: none;
}

.cf-subnav__item {
  flex: 0 0 auto;
  scroll-snap-align: start;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 36px;
  padding: 0 14px;
  border-radius: var(--cf-r-md);
  border: 1px solid var(--cf-bd);
  background: var(--cf-s1);
  color: var(--cf-fg-2);
  font-size: 13.5px;
  font-weight: 600;
  text-decoration: none;
  white-space: nowrap;
}

.cf-subnav__item.is-active {
  background: var(--cf-primary-fill);
  border-color: var(--cf-primary-fill);
  color: var(--cf-primary-fg);
}

.cf-subnav__icon {
  font-size: 14px;
}

@media (max-width: 900px) {
  .cf-subnav {
    display: flex;
  }
}
</style>
