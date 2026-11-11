<template>
  <div class="min-w-0">
    <!-- 警告和错误：按内容合并，最新的在前 -->
    <section v-if="alerts.length" class="mb-3 grid gap-2" aria-label="警告和错误">
      <article
        v-for="group in shownAlerts" :key="group.key" data-log-alert
        :class="cn('rounded-xl px-3 py-2.5', group.last.entry.level === 'ERROR' ? 'bg-destructive-soft' : 'bg-warning-soft')"
      >
        <div class="flex min-w-0 items-baseline gap-2">
          <span :class="cn('shrink-0 text-[13px] font-semibold', group.last.entry.level === 'ERROR' ? 'text-destructive-accent' : 'text-warning-accent')">
            {{ alertTitle(group.last) }}
          </span>
          <span v-if="group.count > 1" class="num shrink-0 text-xs text-muted-foreground">×{{ group.count }}</span>
          <span class="num ml-auto shrink-0 font-mono text-xs text-muted-foreground">{{ timeLabel(group.last.entry) }}</span>
        </div>
        <p v-if="group.last.failure" class="m-0 mt-0.5 min-w-0 text-xs text-muted-foreground [overflow-wrap:anywhere]">
          {{ deviceLabel(notes, group.last.failure.sourceIP, group.last.failure.inner) }} → <span class="font-mono">{{ group.last.failure.target }}</span>
          · {{ group.last.failure.policy }}
        </p>
        <!-- 有的警告是整段结构体，默认只显示两行，点开看全文 -->
        <button
          type="button" data-log-alert-text :aria-expanded="openAlerts.has(group.key)"
          :class="cn('mt-0.5 block w-full cursor-pointer text-left font-mono text-xs text-muted-foreground [overflow-wrap:anywhere]', !openAlerts.has(group.key) && 'line-clamp-2')"
          @click="toggleAlert(group.key)"
        >
          {{ group.last.failure ? group.last.failure.error : group.last.entry.message }}
        </button>
      </article>
      <Button v-if="alerts.length > ALERT_LIMIT" variant="ghost" size="sm" class="min-h-11 justify-self-start" @click="allAlerts = !allAlerts">
        {{ allAlerts ? '收起' : `显示全部 ${alerts.length} 组警告和错误` }}
      </Button>
    </section>

    <div ref="scroller" :class="cn('log-view min-w-0 overflow-y-auto rounded-xl border border-border bg-card', heightClass)">
      <p v-if="notice || !items.length" class="m-0 px-4 py-10 text-center text-[13px] whitespace-pre-wrap text-muted-foreground [overflow-wrap:anywhere]">
        {{ notice || emptyText }}
      </p>
      <ol v-else :class="cn('m-0 list-none p-0', merged && 'is-merged', hasOtherDays && 'has-dates')">
        <li class="log-head" aria-hidden="true">
          <span class="c-time">{{ merged ? '最近' : '时间' }}</span><span class="c-device">设备</span><span class="c-target">目标</span>
          <span class="c-rule">规则</span><span class="c-policy">策略 → 出口</span><span v-if="merged" class="c-count">次数</span>
        </li>
        <li v-for="item in items" :key="item.key" :class="cn('log-item', item.row.entry.level === 'WARN' && 'is-warn', item.row.entry.level === 'ERROR' && 'is-error')">
          <button type="button" class="log-line" :aria-expanded="expanded.has(item.key)" :title="item.row.entry.raw" @click="toggle(item.key)">
            <span class="c-time num font-mono">{{ timeLabel(item.row.entry) }}</span>
            <template v-if="item.row.connection">
              <span class="c-device truncate">{{ deviceLabel(notes, item.row.connection.sourceIP, item.row.connection.inner) }}<span v-if="item.row.connection.process" class="ml-1 text-muted-foreground">{{ item.row.connection.process }}</span></span>
              <span class="c-target min-w-0">
                <span class="truncate font-mono"><span class="text-foreground">{{ item.row.connection.host }}</span><span v-if="item.row.connection.port" class="text-muted-foreground">:{{ item.row.connection.port }}</span></span>
                <span class="proto">{{ item.row.connection.network }}</span>
              </span>
              <span class="c-rule truncate text-muted-foreground">{{ item.row.connection.ruleLabel }}</span>
              <span class="c-policy min-w-0"><span :class="cn('policy', `policy-${item.row.connection.kind}`)">{{ policyLabel(item.row.connection) }}</span></span>
            </template>
            <span v-else class="c-message min-w-0">
              <span v-if="item.row.entry.level" :class="cn('level', `level-${item.row.entry.level}`)">{{ item.row.entry.level }}</span>
              <span :class="cn('min-w-0 font-mono [overflow-wrap:anywhere]', !expanded.has(item.key) && 'line-clamp-2')">{{ item.row.entry.message }}</span>
            </span>
            <span v-if="merged" class="c-count"><span :class="cn('count num', item.count >= 10 && 'is-hot')">×{{ item.count }}</span></span>
          </button>
          <div v-if="expanded.has(item.key)" class="log-raw font-mono">
            <p v-if="item.count > item.recent.length" class="m-0 text-muted-foreground">共 {{ item.count }} 次，下面是最近 {{ item.recent.length }} 次</p>
            <p v-for="row in item.recent" :key="row.entry.id" class="m-0 [overflow-wrap:anywhere]">
              <span class="num text-muted-foreground">{{ timeLabel(row.entry) }}</span> {{ row.entry.raw }}
            </p>
          </div>
        </li>
      </ol>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { AgentLogEntry } from '@/lib/agentLogs'
import {
  deviceLabel, groupLogRows, isAlertLevel,
  type MihomoConnectionLog, type MihomoLogGroup, type MihomoLogRow
} from '@/lib/mihomoLogView'

const props = withDefaults(defineProps<{
  rows: MihomoLogRow[]
  notes: Record<string, string>
  /** 合并重复：连接按设备 + 目标 + 策略合并，警告和错误只在上方汇总 */
  merged: boolean
  notice?: string
  emptyText?: string
  heightClass?: string
}>(), { notice: '', emptyText: '该日志暂无内容', heightClass: 'h-[55dvh] min-h-60 max-h-[640px]' })

const ALERT_LIMIT = 3
const pad = (value: number) => String(value).padStart(2, '0')
const now = new Date()
const today = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
/** 不是今天的日志在时间前加上月-日，避免把前一天的日志看成今天的 */
const timeLabel = (entry: AgentLogEntry) => entry.date && entry.date !== today ? `${entry.date.slice(5)} ${entry.time}` : entry.time
const hasOtherDays = computed(() => props.rows.some(row => row.entry.date && row.entry.date !== today))
const allAlerts = ref(false)
const expanded = ref(new Set<string>())
const openAlerts = ref(new Set<string>())
const scroller = ref<HTMLElement | null>(null)

interface ViewItem { key: string; row: MihomoLogRow; count: number; recent: MihomoLogRow[] }

const alerts = computed(() => groupLogRows(props.rows.filter(row => isAlertLevel(row.entry.level))).reverse())
const shownAlerts = computed<MihomoLogGroup[]>(() => allAlerts.value ? alerts.value : alerts.value.slice(0, ALERT_LIMIT))

const items = computed<ViewItem[]>(() => {
  if (!props.merged) return props.rows.map(row => ({ key: `r${row.entry.id}`, row, count: 1, recent: [row] }))
  // 合并时警告和错误已在上方汇总，列表里不再重复
  return groupLogRows(props.rows.filter(row => !isAlertLevel(row.entry.level)))
    .map(group => ({ key: group.key, row: group.last, count: group.count, recent: group.recent }))
})

// 切换合并方式后，展开状态对应的键已不同
watch(() => props.merged, () => { expanded.value = new Set() })

function toggle(key: string) {
  const next = new Set(expanded.value)
  if (!next.delete(key)) next.add(key)
  expanded.value = next
}

function toggleAlert(key: string) {
  const next = new Set(openAlerts.value)
  if (!next.delete(key)) next.add(key)
  openAlerts.value = next
}

function policyLabel(connection: MihomoConnectionLog) {
  if (connection.group === connection.node) return connection.node
  return `${connection.group} · ${connection.node}`
}

function alertTitle(row: MihomoLogRow) {
  if (row.failure) return row.entry.level === 'ERROR' ? '连接失败' : '连接失败（警告）'
  if (/^\[DNS\]/i.test(row.entry.message)) return 'DNS'
  return row.entry.level === 'ERROR' ? '错误' : '警告'
}

const scrollToBottom = () => {
  if (scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight
}

defineExpose({ scrollToBottom })
</script>

<style scoped>
/* 时间列：出现非今天的日志时带上月-日，列宽随之加宽 */
ol { --time-col: 4.75rem; }
ol.has-dates { --time-col: 6.75rem; }

/* 宽屏为表格：时间 · 设备 · 目标 · 规则 · 策略（· 次数）；窄屏为两行卡片 */
.log-head,
.log-line {
  display: grid;
  grid-template-columns: var(--time-col) minmax(6rem, 10rem) minmax(0, 1fr) minmax(4rem, 8rem) minmax(7rem, auto);
  grid-template-areas: 'time device target rule policy';
  column-gap: 0.75rem;
  align-items: center;
  width: 100%;
  padding: 0.4375rem 0.875rem;
  text-align: left;
  font-size: 13px;
}

.is-merged .log-head,
.is-merged .log-line {
  grid-template-columns: var(--time-col) minmax(6rem, 10rem) minmax(0, 1fr) minmax(4rem, 8rem) minmax(7rem, auto) 3.5rem;
  grid-template-areas: 'time device target rule policy count';
}

.log-head {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--card);
  border-bottom: 1px solid var(--border);
  color: var(--muted-foreground);
  font-size: 11.5px;
  font-weight: 500;
}

.log-item {
  border-bottom: 1px solid var(--border);
}

.log-item:last-child {
  border-bottom: 0;
}

.log-line {
  cursor: pointer;
  min-height: 2.25rem;
}

.log-line:hover {
  background: var(--accent);
}

.log-line:focus-visible {
  outline: none;
  box-shadow: inset 0 0 0 2px color-mix(in oklab, var(--ring) 50%, transparent);
}

.is-warn .log-line {
  box-shadow: inset 3px 0 var(--warning);
}

.is-error .log-line {
  box-shadow: inset 3px 0 var(--destructive);
}

.c-time { grid-area: time; color: var(--muted-foreground); font-size: 12px; }
.c-device { grid-area: device; min-width: 0; }
.c-target { grid-area: target; display: flex; align-items: center; gap: 0.375rem; }
.c-rule { grid-area: rule; min-width: 0; font-size: 12px; }
.c-policy { grid-area: policy; display: flex; justify-content: flex-start; }
.c-count { grid-area: count; text-align: right; }
.c-message { grid-column: 2 / -1; display: flex; align-items: baseline; gap: 0.5rem; font-size: 12px; }
/* 合并时最后一列留给次数 */
.is-merged .c-message { grid-column: 2 / -2; }

.proto {
  flex-shrink: 0;
  border: 1px solid var(--border-strong);
  border-radius: 5px;
  padding: 0 0.3125rem;
  color: var(--muted-foreground);
  font-size: 10.5px;
  line-height: 1.5;
  letter-spacing: 0.04em;
}

.policy {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  border-radius: 999px;
  padding: 0.0625rem 0.5625rem;
  font-size: 12px;
}

.policy-proxy { background: var(--primary-soft); color: var(--primary-accent); }
.policy-direct { background: var(--success-soft); color: var(--success-accent); }
.policy-reject { background: var(--destructive-soft); color: var(--destructive-accent); }

.level {
  flex-shrink: 0;
  border-radius: 5px;
  padding: 0 0.375rem;
  font-size: 10.5px;
  font-weight: 600;
  background: var(--secondary);
  color: var(--muted-foreground);
}

.level-WARN { background: var(--warning-soft); color: var(--warning-accent); }
.level-ERROR { background: var(--destructive-soft); color: var(--destructive-accent); }
.level-DEBUG,
.level-TRACE { background: var(--info-soft); color: var(--info-accent); }

.count {
  display: inline-block;
  min-width: 2.25rem;
  border-radius: 999px;
  background: var(--secondary);
  padding: 0 0.5rem;
  text-align: center;
  font-size: 12px;
}

.count.is-hot { background: var(--primary-soft); color: var(--primary-accent); }

.log-raw {
  display: grid;
  gap: 0.125rem;
  padding: 0.25rem 0.875rem 0.625rem calc(var(--time-col) + 1.625rem);
  font-size: 11.5px;
  color: var(--foreground);
}

@media (max-width: 639px) {
  .log-head { display: none; }

  .log-line,
  .is-merged .log-line {
    grid-template-columns: minmax(0, 1fr) auto;
    grid-template-areas: 'target meta' 'device policy';
    row-gap: 0.25rem;
    padding: 0.625rem 0.75rem;
  }

  .c-time { grid-area: meta; text-align: right; }
  .c-rule { display: none; }
  .c-device { font-size: 12px; color: var(--muted-foreground); }
  .c-policy { justify-content: flex-end; }
  /* 非连接日志：内容占第一行左侧，右侧是时间（合并时为次数） */
  .c-message,
  .is-merged .c-message { grid-column: 1; grid-row: 1; }

  /* 合并时右上角显示次数，最近时间单独一行 */
  .is-merged .log-line { grid-template-areas: 'target meta' 'device policy' 'time time'; }
  .is-merged .c-count { grid-area: meta; }
  .is-merged .c-time { grid-area: time; text-align: left; font-size: 11.5px; }

  .log-raw { padding-left: 0.75rem; }
}
</style>
