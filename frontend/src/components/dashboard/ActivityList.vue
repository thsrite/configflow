<template>
  <section class="cf-activity" aria-label="运行状态">
    <header class="cf-activity__head">
      <h2 class="cf-activity__title">运行状态</h2>
      <div class="cf-activity__filters" role="tablist">
        <button
          v-for="tab in tabs"
          :key="tab"
          type="button"
          role="tab"
          class="cf-activity__tab"
          :aria-selected="tab === active"
          @click="$emit('update:active', tab)"
        >
          {{ tab }}
        </button>
      </div>
    </header>

    <el-empty v-if="!rows.length" description="暂无运行记录" :image-size="80" />

    <div v-else class="cf-activity__scroll">
      <table class="cf-activity__table">
        <thead>
          <tr>
            <th scope="col">任务</th>
            <th scope="col">详情</th>
            <th scope="col">状态</th>
            <th scope="col">时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in rows" :key="i">
            <td class="cf-activity__task">
              <span class="cf-activity__dot" :class="`is-${row.level}`" aria-hidden="true"></span>
              {{ row.task }}
            </td>
            <td class="cf-activity__detail">{{ row.detail }}</td>
            <td>
              <span class="cf-activity__status" :class="`is-${row.level}`">{{ row.status }}</span>
            </td>
            <td class="cf-activity__time cf-mono">{{ row.time }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <footer v-if="rows.length" class="cf-activity__foot">共 {{ rows.length }} 条</footer>
  </section>
</template>

<script setup lang="ts">
export interface ActivityRow {
  task: string
  detail: string
  status: string
  level: 'ok' | 'warn' | 'err'
  time: string
}

defineProps<{ rows: ActivityRow[]; tabs: string[]; active: string }>()
defineEmits<{ (e: 'update:active', tab: string): void }>()
</script>

<style scoped>
.cf-activity {
  background: var(--cf-s1);
  border: 1px solid var(--cf-bd);
  border-radius: var(--cf-r-xl);
  box-shadow: var(--cf-shadow);
  padding: var(--cf-sp-4);
}

.cf-activity__head {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-3);
  flex-wrap: wrap;
  margin-bottom: var(--cf-sp-3);
}

.cf-activity__title {
  font-size: 15px;
  font-weight: 650;
  margin: 0;
  color: var(--cf-fg);
}

.cf-activity__filters {
  display: flex;
  gap: var(--cf-sp-1);
  overflow-x: auto;
  scrollbar-width: none;
}
.cf-activity__filters::-webkit-scrollbar {
  display: none;
}

.cf-activity__tab {
  flex: 0 0 auto;
  /* 触屏最小点击目标 */
  height: 32px;
  padding: 0 11px;
  border: none;
  background: none;
  border-radius: var(--cf-r-sm);
  color: var(--cf-fg-2);
  font-family: inherit;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
}

.cf-activity__tab[aria-selected='true'] {
  background: var(--cf-s3);
  color: var(--cf-fg);
}

/* 宽表在自身容器内滚动，页面不产生横向滚动 */
.cf-activity__scroll {
  overflow-x: auto;
}

.cf-activity__table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.cf-activity__table th {
  text-align: left;
  font-size: 11.5px;
  font-weight: 650;
  color: var(--cf-fg-3);
  padding: 0 var(--cf-sp-3) var(--cf-sp-2) 0;
  border-bottom: 1px solid var(--cf-bd);
  white-space: nowrap;
}

.cf-activity__table td {
  padding: 11px var(--cf-sp-3) 11px 0;
  border-bottom: 1px solid var(--cf-bd);
  vertical-align: top;
}

.cf-activity__table tr:last-child td {
  border-bottom: none;
}

.cf-activity__task {
  font-weight: 600;
  color: var(--cf-fg);
  white-space: nowrap;
}

.cf-activity__dot {
  display: inline-block;
  width: 7px;
  height: 7px;
  border-radius: var(--cf-r-pill);
  margin-right: 7px;
}
.cf-activity__dot.is-ok {
  background: var(--cf-success);
}
.cf-activity__dot.is-warn {
  background: var(--cf-warning);
}
.cf-activity__dot.is-err {
  background: var(--cf-danger);
}

.cf-activity__detail {
  color: var(--cf-fg-2);
  min-width: 220px;
}

.cf-activity__status {
  font-size: 12px;
  font-weight: 650;
  white-space: nowrap;
}
.cf-activity__status.is-ok {
  color: var(--cf-success);
}
.cf-activity__status.is-warn {
  color: var(--cf-warning);
}
.cf-activity__status.is-err {
  color: var(--cf-danger);
}

.cf-activity__time {
  color: var(--cf-fg-3);
  font-size: 12px;
  white-space: nowrap;
}

.cf-activity__foot {
  margin-top: var(--cf-sp-3);
  padding-top: var(--cf-sp-3);
  border-top: 1px solid var(--cf-bd);
  font-size: 12px;
  color: var(--cf-fg-3);
}
</style>
