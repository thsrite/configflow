<template>
  <!-- 概念图：四项 KPI 同处一个面板，以细分隔线区隔，而非四张独立卡片 -->
  <section class="cf-kpi" aria-label="关键指标">
    <component
      :is="item.route ? 'button' : 'div'"
      v-for="item in items"
      :key="item.label"
      class="cf-kpi__cell"
      :class="{ 'is-link': !!item.route }"
      :type="item.route ? 'button' : undefined"
      @click="item.route && $router.push(item.route)"
    >
      <div class="cf-kpi__text">
        <div class="cf-kpi__value cf-num">{{ item.value }}</div>
        <div class="cf-kpi__label">{{ item.label }}</div>
      </div>
      <span class="cf-kpi__icon" :class="`is-${item.scope}`" aria-hidden="true">
        <el-icon><component :is="item.icon" /></el-icon>
      </span>
    </component>
  </section>
</template>

<script setup lang="ts">
export interface KpiItem {
  label: string
  value: number | string
  icon: string
  scope: 'resource' | 'profile' | 'system'
  route?: string
}

defineProps<{ items: KpiItem[] }>()
</script>

<style scoped>
.cf-kpi {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  background: var(--cf-s1);
  border: 1px solid var(--cf-bd);
  border-radius: var(--cf-r-xl);
  box-shadow: var(--cf-shadow);
  overflow: hidden;
  margin-bottom: var(--cf-sp-4);
}

.cf-kpi__cell {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-3);
  padding: var(--cf-sp-4) var(--cf-sp-4);
  background: none;
  border: none;
  border-left: 1px solid var(--cf-bd);
  font-family: inherit;
  color: inherit;
  text-align: left;
  min-width: 0;
}

.cf-kpi__cell:first-child {
  border-left: none;
}

.cf-kpi__cell.is-link {
  cursor: pointer;
}

.cf-kpi__cell.is-link:hover {
  background: var(--cf-s2);
}

.cf-kpi__text {
  min-width: 0;
  flex: 1 1 auto;
}

.cf-kpi__value {
  font-size: 27px;
  font-weight: 700;
  letter-spacing: -0.03em;
  line-height: 1.1;
  color: var(--cf-fg);
}

.cf-kpi__label {
  font-size: 12.5px;
  color: var(--cf-fg-2);
  margin-top: 2px;
}

.cf-kpi__icon {
  width: 38px;
  height: 38px;
  flex: 0 0 auto;
  border-radius: var(--cf-r-lg);
  display: grid;
  place-items: center;
  font-size: 17px;
}

/* 作用域用图标底色区分，与分组标题共同表意，不单靠颜色 */
.cf-kpi__icon.is-resource {
  background: var(--cf-shared-soft);
  color: var(--cf-shared);
}
.cf-kpi__icon.is-profile {
  background: var(--cf-primary-soft);
  color: var(--cf-primary);
}
.cf-kpi__icon.is-system {
  background: var(--cf-success-soft);
  color: var(--cf-success);
}

@media (max-width: 900px) {
  .cf-kpi {
    grid-template-columns: 1fr 1fr;
  }
  .cf-kpi__cell {
    padding: var(--cf-sp-3);
  }
  /* 两列布局下，右列与第三行起都需要分隔线 */
  .cf-kpi__cell:nth-child(odd) {
    border-left: none;
  }
  .cf-kpi__cell:nth-child(n + 3) {
    border-top: 1px solid var(--cf-bd);
  }
  .cf-kpi__value {
    font-size: 23px;
  }
}
</style>
