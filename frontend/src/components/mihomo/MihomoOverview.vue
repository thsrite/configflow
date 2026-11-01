<template>
  <div class="flex min-w-0 flex-col gap-4 max-sm:gap-3">
    <p v-if="error" role="alert" class="m-0 break-words text-sm text-warning-accent">{{ error }}{{ snapshot ? '，以下是上次读取的数据。' : '' }}</p>
    <div class="grid grid-cols-3 gap-3 max-md:gap-2">
      <div v-for="item in summary" :key="item.label" class="flex min-w-0 flex-col rounded-xl border border-border bg-card px-4 py-3 max-md:px-3">
        <div class="truncate text-[12px] text-muted-foreground">{{ item.label }}</div>
        <div class="num mt-1 truncate text-[18px] text-foreground max-md:text-[15px]">{{ item.value }}</div>
        <template v-if="item.trend">
          <Sparkline class="mt-2 h-9 w-full" :data="item.trend" :color="item.color" />
          <div class="num mt-1 truncate text-[11px] text-muted-foreground" :title="`近 2 分钟峰值 ${item.peak}`">近 2 分钟峰值 {{ item.peak }}</div>
        </template>
      </div>
    </div>

    <MihomoLatencyCard :agent-id="agentId" :active="active" :reload-token="reloadToken" />

    <SectionCard title="连接拓扑" :padded="false" help="来源设备 → 命中规则 → 策略组 → 出口节点，按最近一次采样的实时速率绘制，每列最多 6 项。点节点可到「连接」查看明细。">
      <LiveFlow :agents="flowAgents" :paused="!active" />
    </SectionCard>

    <MihomoRuleHitsCard :agent-id="agentId" :active="active" :reload-token="reloadToken" />

  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import SectionCard from '@/components/common/SectionCard.vue'
import Sparkline from '@/components/common/Sparkline.vue'
import LiveFlow from '@/components/dashboard/LiveFlow.vue'
import MihomoLatencyCard from './MihomoLatencyCard.vue'
import MihomoRuleHitsCard from './MihomoRuleHitsCard.vue'
import { useTabActive } from '@/components/ui/tabs'
import { useMihomoSnapshot } from '@/composables/useMihomoSnapshot'
import { formatBytes } from '@/lib/format'

const props = withDefaults(defineProps<{ agentId: string; agentName?: string; reloadToken?: number; active?: boolean }>(), { active: true })
// 隐藏的标签保持挂载，但不再轮询
const active = useTabActive(() => props.active)
const flowAgents = computed(() => [{ id: props.agentId, name: props.agentName }])
const { snapshot, error, totalSpeed, trend } = useMihomoSnapshot(() => props.agentId, active, () => props.reloadToken)

const peak = (values: number[]) => values.length ? Math.max(...values) : 0
const speedText = (bytesPerSecond: number) => bytesPerSecond > 0 ? formatBytes(bytesPerSecond) + '/s' : '—'
// 上排是计数与累计量，下排是带趋势线的实时指标，两排高度各自一致
const summary = computed(() => [
  { label: '连接', value: String(snapshot.value?.connections?.length ?? '—') },
  { label: '累计上传', value: formatBytes(snapshot.value?.uploadTotal) },
  { label: '累计下载', value: formatBytes(snapshot.value?.downloadTotal) },
  {
    label: '内存使用', value: formatBytes(snapshot.value?.memory),
    trend: trend.value.memory, color: '--warning-accent', peak: formatBytes(peak(trend.value.memory))
  },
  {
    label: '上传速度', value: speedText(totalSpeed.value.up),
    trend: trend.value.up, color: '--success-accent', peak: speedText(peak(trend.value.up))
  },
  {
    label: '下载速度', value: speedText(totalSpeed.value.down),
    trend: trend.value.down, color: '--info-accent', peak: speedText(peak(trend.value.down))
  }
])
</script>
