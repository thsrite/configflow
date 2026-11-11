<template>
  <div ref="scroller" class="agent-log-terminal min-w-0 overflow-x-hidden overflow-y-auto rounded-xl border" :class="heightClass">
    <p v-if="notice || !entries.length" class="m-0 px-4 py-10 text-center text-[13px] whitespace-pre-wrap [overflow-wrap:anywhere] opacity-70">
      {{ notice || emptyText }}
    </p>
    <ol v-else :class="cn('m-0 list-none p-0 py-2 font-mono text-[12px] leading-[1.65]', !hasLevels && 'no-levels')">
      <template v-for="(entry, index) in entries" :key="entry.id">
        <li v-if="dateChanged(index)" class="log-date" aria-hidden="true">{{ entry.date }}</li>
        <li :class="cn('log-row', entry.continuation && 'is-continuation', entry.level && `lv-row-${entry.level}`)">
          <span class="log-time" :title="entry.date ? `${entry.date} ${entry.time}` : undefined">{{ entry.time }}</span>
          <span v-if="hasLevels" :class="cn('log-level', entry.level && `lv-${entry.level}`)">{{ entry.level }}</span>
          <div class="log-body">
            <span v-if="entry.sourceShort" class="log-source" :title="entry.source">{{ entry.sourceShort }}</span>
            <span v-if="entry.message" class="log-message"><template v-for="(token, tokenIndex) in messageTokens(entry)" :key="tokenIndex"><mark v-if="token.hit" :class="token.kind && `tk-${token.kind}`">{{ token.text }}</mark><span v-else-if="token.kind" :class="`tk-${token.kind}`">{{ token.text }}</span><template v-else>{{ token.text }}</template></template></span>
            <span v-if="entry.fields.length" class="log-fields">
              <span v-for="(field, fieldIndex) in entry.fields" :key="fieldIndex" class="log-field">
                <span class="log-key">{{ field.key }}=</span><span class="log-value"><template v-for="(token, tokenIndex) in marked(field.value || EMPTY_VALUE)" :key="tokenIndex"><mark v-if="token.hit">{{ token.text }}</mark><template v-else>{{ token.text }}</template></template></span>
              </span>
            </span>
          </div>
        </li>
      </template>
    </ol>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  filterLogLevel, markLogTokens, parseAgentLogs, tokenizeLogMessage,
  type AgentLogEntry, type AgentLogLevel, type LogToken
} from '@/lib/agentLogs'
import { cn } from '@/lib/utils'

const props = withDefaults(defineProps<{
  text?: string
  /** 已解析好的条目（如实时日志），提供时不再解析 text */
  items?: AgentLogEntry[]
  /** 只显示不低于该级别的条目 */
  minLevel?: AgentLogLevel | ''
  /** 加载中、读取失败或不可读时的提示，优先于日志内容 */
  notice?: string
  /** 没有可显示的条目时的提示 */
  emptyText?: string
  heightClass?: string
  /** 搜索词：在正文与字段值中标出命中的文字 */
  highlight?: string
}>(), {
  text: '',
  items: undefined,
  minLevel: '',
  notice: '',
  emptyText: '该日志暂无内容',
  heightClass: 'max-h-[56dvh]',
  highlight: ''
})

const scroller = ref<HTMLElement | null>(null)
const entries = computed(() => filterLogLevel(props.items ?? parseAgentLogs(props.text), props.minLevel))
const hasLevels = computed(() => entries.value.some(entry => entry.level))

const EMPTY_VALUE = '""'
// 着色片段按条目缓存：实时日志每秒追加，旧条目不必重新切分
const tokenCache = new WeakMap<AgentLogEntry, LogToken[]>()
const tokensOf = (entry: AgentLogEntry) => {
  let tokens = tokenCache.get(entry)
  if (!tokens) {
    tokens = tokenizeLogMessage(entry.message)
    tokenCache.set(entry, tokens)
  }
  return tokens
}
const messageTokens = (entry: AgentLogEntry) => markLogTokens(tokensOf(entry), props.highlight)
const marked = (text: string) => markLogTokens([{ text, kind: '' }], props.highlight)

/** 日期变化时插入分隔行；续行与无时间的行不打断 */
const dateChanged = (index: number) => {
  const date = entries.value[index].date
  if (!date) return false
  for (let previous = index - 1; previous >= 0; previous--) {
    const earlier = entries.value[previous].date
    if (earlier) return earlier !== date
  }
  return true
}

const scrollToBottom = () => {
  if (scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight
}

defineExpose({ scrollToBottom })
</script>

<style scoped>
/* 与「日志」页的终端保持同一组暖炭色，两套主题下都为深色 */
.agent-log-terminal {
  background: #141312;
  color: #d9d5ca;
  border-color: rgb(255 255 255 / 8%);
}

:root[data-theme='light'] .agent-log-terminal {
  background: #1c1b19;
}

.log-date {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.5rem 1rem 0.25rem;
  color: #7c786e;
  font-size: 11px;
}

.log-date::after {
  content: '';
  flex: 1;
  border-top: 1px solid rgb(255 255 255 / 8%);
}

/* 窄屏：时间与级别一行，正文另起一行 */
.log-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  grid-template-areas: 'time level' 'body body';
  gap: 0 0.625rem;
  padding: 0.3125rem 1rem;
  border-left: 2px solid transparent;
}

.no-levels .log-row {
  grid-template-areas: 'time time' 'body body';
}

.log-row:hover {
  background: rgb(255 255 255 / 3%);
}

/* 条与条之间的细分隔线；续行属于上一条，不画线 */
.log-row + .log-row:not(.is-continuation) {
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 5.5%);
}

.log-row.is-continuation {
  padding-top: 0;
}

.log-time {
  grid-area: time;
  color: #7c786e;
  font-variant-numeric: tabular-nums;
}

.log-level {
  grid-area: level;
  font-weight: 600;
}

.log-body {
  grid-area: body;
  min-width: 0;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.log-source {
  margin-right: 0.5rem;
  color: #9a968b;
}

.log-fields {
  display: flex;
  flex-wrap: wrap;
  gap: 0.125rem 0.875rem;
  margin-top: 0.125rem;
}

.log-field {
  min-width: 0;
  max-width: 100%;
}

.log-key {
  color: #8a867b;
}

.log-value {
  color: #c4c0b4;
}

/* 正文按含义着色：目标最亮，来源与连接词压暗，规则淡紫，策略按直连 / 拦截 / 代理区分，错误原因红色 */
.tk-tag {
  color: #9a968b;
}

.tk-src,
.tk-kw,
.tk-arrow {
  color: #7c786e;
}

.tk-dst {
  color: #f1ede3;
}

.tk-rule {
  color: #b9a9d9;
}

.tk-proxy {
  color: #e2bd7a;
}

.tk-direct {
  color: #8fb996;
}

.tk-reject {
  color: #e0705e;
}

.tk-str {
  color: #94c3bd;
}

.tk-err {
  color: #e8806f;
}

/* 搜索命中 */
mark {
  border-radius: 2px;
  background: rgb(226 189 122 / 28%);
  box-shadow: 0 0 0 1px rgb(226 189 122 / 35%);
  color: inherit;
}

mark:not([class]) {
  color: #f1ede3;
}

/* 桌面：时间、级别固定列宽，正文换行后与首行对齐 */
@media (min-width: 640px) {
  .log-row {
    grid-template-columns: 64px 44px minmax(0, 1fr);
    grid-template-areas: 'time level body';
  }

  .no-levels .log-row {
    grid-template-columns: 64px minmax(0, 1fr);
    grid-template-areas: 'time body';
  }
}

.lv-TRACE {
  color: #7c786e;
}

.lv-DEBUG {
  color: #7aa7d4;
}

.lv-INFO {
  color: #8fb996;
}

.lv-WARN {
  color: #d9a984;
}

.lv-ERROR {
  color: #e0705e;
}

/* 警告与错误整行加色条，便于在长日志中定位 */
.lv-row-WARN {
  border-left-color: #d9a984;
  background: rgb(217 169 132 / 6%);
}

.lv-row-ERROR {
  border-left-color: #e0705e;
  background: rgb(224 112 94 / 8%);
}
</style>
