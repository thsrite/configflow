<template>
  <!-- Joins the panel's action row; the row owns alignment and wrapping. -->
  <div class="contents">
    <div class="flex min-w-0 items-center gap-1 text-xs text-muted-foreground max-sm:mr-auto">
      <span class="hidden sm:inline">{{ statisticsLabel }}</span>
      <span class="sm:hidden">{{ statistics ? '本轮统计' : '统计重置不可用' }}</span>
      <FieldHelp label="统计周期" :text="statisticsLabel + '\n' + statisticsNote" />
    </div>
    <Button variant="outline" class="min-h-11 shrink-0" :disabled="!statistics || resetting" @click="$emit('reset')">
      <Loader2 v-if="resetting" class="size-4 animate-spin" /><RotateCcw v-else class="size-4" />{{ resetting ? '正在重置…' : '重置统计' }}
    </Button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Loader2, RotateCcw } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import FieldHelp from '@/components/common/FieldHelp.vue'
import { formatQueryTime, type OxiDNSOverview } from '@/types/oxidnsMonitor'

const props = defineProps<{ statistics?: OxiDNSOverview['statistics']; resetting?: boolean }>()
defineEmits<{ reset: [] }>()

const statisticsLabel = computed(() => props.statistics
  ? '本轮统计开始于 ' + formatQueryTime(props.statistics.started_at_ms)
  : '当前 Agent 不支持面板统计重置')
const resetReasons: Record<string, string> = {
  initial: '首次查看时开始统计', manual: '已手动重置统计', core_changed: '核心重启或重载后已重新统计',
  configuration_changed: '配置变化后已重新统计', target_changed: '监控目标变化后已重新统计', counters_changed: '插件重新计数后已重新统计',
  publication: '配置发布后继续累计，沿用原统计起点'
}
const statisticsNote = computed(() => props.statistics
  ? (resetReasons[props.statistics.reason] || '本轮统计') + '。重置仅影响此设备的面板统计；缓存条目、处理中数量及查询记录保持原样。'
  : '现有指标仍按核心启动以来展示。重置统计需要 Agent 1.8.14-go 或更新版本。')
</script>
