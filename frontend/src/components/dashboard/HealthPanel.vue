<template>
  <section class="cf-panel" aria-label="配置健康">
    <header class="cf-panel__head">
      <h2 class="cf-panel__title">配置健康</h2>
      <span class="cf-panel__badge" :class="`is-${overall.level}`">{{ overall.text }}</span>
    </header>

    <ul class="cf-health">
      <li v-for="row in rows" :key="row.label" class="cf-health__row">
        <span class="cf-health__dot" :class="`is-${row.level}`" aria-hidden="true"></span>
        <span class="cf-health__label">{{ row.label }}</span>
        <span class="cf-health__value" :class="`is-${row.level}`">{{ row.value }}</span>
      </li>
    </ul>

    <footer class="cf-panel__foot">
      <span class="cf-panel__meta">上次检测 {{ checkedAt || '—' }}</span>
      <el-button text size="small" :loading="loading" @click="$emit('refresh')">
        <el-icon><Refresh /></el-icon>
        立即检测
      </el-button>
    </footer>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'

export type HealthLevel = 'ok' | 'warn' | 'err'

export interface HealthRow {
  label: string
  value: string
  level: HealthLevel
}

const props = defineProps<{
  rows: HealthRow[]
  checkedAt?: string
  loading?: boolean
}>()

defineEmits<{ (e: 'refresh'): void }>()

// 总体状态取最差的一项，避免出现「有告警但总体良好」的自相矛盾
const overall = computed(() => {
  if (props.rows.some(r => r.level === 'err')) return { level: 'err', text: '异常' }
  if (props.rows.some(r => r.level === 'warn')) return { level: 'warn', text: '需关注' }
  return { level: 'ok', text: '良好' }
})
</script>

<style scoped>
.cf-panel {
  background: var(--cf-s1);
  border: 1px solid var(--cf-bd);
  border-radius: var(--cf-r-xl);
  box-shadow: var(--cf-shadow);
  padding: var(--cf-sp-4);
  margin-bottom: var(--cf-sp-3);
}

.cf-panel__head {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  margin-bottom: var(--cf-sp-3);
}

.cf-panel__title {
  font-size: 15px;
  font-weight: 650;
  margin: 0;
  color: var(--cf-fg);
}

.cf-panel__badge {
  margin-left: auto;
  font-size: 11.5px;
  font-weight: 650;
  padding: 3px 9px;
  border-radius: 7px;
}
.cf-panel__badge.is-ok {
  background: var(--cf-success-soft);
  color: var(--cf-success);
}
.cf-panel__badge.is-warn {
  background: var(--cf-warning-soft);
  color: var(--cf-warning);
}
.cf-panel__badge.is-err {
  background: var(--cf-danger-soft);
  color: var(--cf-danger);
}

.cf-health {
  list-style: none;
  margin: 0;
  padding: 0;
}

.cf-health__row {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  padding: 9px 0;
  border-bottom: 1px solid var(--cf-bd);
  font-size: 13px;
}
.cf-health__row:last-child {
  border-bottom: none;
}

.cf-health__dot {
  width: 7px;
  height: 7px;
  border-radius: var(--cf-r-pill);
  flex: 0 0 auto;
}
.cf-health__dot.is-ok {
  background: var(--cf-success);
}
.cf-health__dot.is-warn {
  background: var(--cf-warning);
}
.cf-health__dot.is-err {
  background: var(--cf-danger);
}

.cf-health__label {
  color: var(--cf-fg-2);
}

.cf-health__value {
  margin-left: auto;
  font-weight: 600;
  color: var(--cf-fg);
  font-variant-numeric: tabular-nums;
}
.cf-health__value.is-warn {
  color: var(--cf-warning);
}
.cf-health__value.is-err {
  color: var(--cf-danger);
}

.cf-panel__foot {
  display: flex;
  align-items: center;
  margin-top: var(--cf-sp-3);
  padding-top: var(--cf-sp-3);
  border-top: 1px solid var(--cf-bd);
}

.cf-panel__meta {
  font-size: 11.5px;
  color: var(--cf-fg-3);
}

.cf-panel__foot .el-button {
  margin-left: auto;
}
</style>
