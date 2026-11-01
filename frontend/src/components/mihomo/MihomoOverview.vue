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

    <MihomoConnectionStats :connections="snapshot?.connections || []" :speeds="speeds" :notes="notes" :loading="!snapshot && !error" :agent-id="agentId" :active="active" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import SectionCard from '@/components/common/SectionCard.vue'
import Sparkline from '@/components/common/Sparkline.vue'
import LiveFlow from '@/components/dashboard/LiveFlow.vue'
import MihomoLatencyCard from './MihomoLatencyCard.vue'
import MihomoRuleHitsCard from './MihomoRuleHitsCard.vue'
import MihomoConnectionStats from './MihomoConnectionStats.vue'
import { useTabActive } from '@/components/ui/tabs'
import { clientToolsApi, mihomoPanelApi } from '@/api'
import { formatBytes } from '@/lib/format'
import { panelError, type MihomoConnectionsSnapshot } from '@/types/mihomo'

const POLL_MS = 2000
/** 趋势图保留的采样数：2 秒一次，约 2 分钟 */
const TREND_POINTS = 60

const props = withDefaults(defineProps<{ agentId: string; agentName?: string; reloadToken?: number; active?: boolean }>(), { active: true })
// 隐藏的标签保持挂载，但不再轮询
const active = useTabActive(() => props.active)
const flowAgents = computed(() => [{ id: props.agentId, name: props.agentName }])

const snapshot = ref<MihomoConnectionsSnapshot | null>(null)
const error = ref('')
const notes = ref<Record<string, string>>({})
const speeds = ref(new Map<string, { up: number; down: number }>())
const totalSpeed = ref({ up: 0, down: 0 })
const trend = ref<{ down: number[]; up: number[]; memory: number[] }>({ down: [], up: [], memory: [] })
let previous: { at: number; totals: { up: number; down: number }; byId: Map<string, { up: number; down: number }> } | null = null

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

/** 返回本次是否算出了速率（需要有上一个样本） */
function updateSpeeds(data: MihomoConnectionsSnapshot): boolean {
  const measured = previous !== null
  const at = Date.now()
  const byId = new Map((data.connections || []).map(c => [c.id, { up: c.upload, down: c.download }]))
  const totals = { up: data.uploadTotal, down: data.downloadTotal }
  const next = new Map<string, { up: number; down: number }>()
  if (previous) {
    const seconds = Math.max((at - previous.at) / 1000, 0.5)
    for (const [id, current] of byId) {
      const before = previous.byId.get(id)
      if (before) next.set(id, { up: Math.max(0, (current.up - before.up) / seconds), down: Math.max(0, (current.down - before.down) / seconds) })
    }
    totalSpeed.value = { up: Math.max(0, (totals.up - previous.totals.up) / seconds), down: Math.max(0, (totals.down - previous.totals.down) / seconds) }
  }
  speeds.value = next
  previous = { at, totals, byId }
  return measured
}

function recordTrend(data: MihomoConnectionsSnapshot, hasSpeed: boolean) {
  const push = (values: number[], value: number) => [...values, value].slice(-TREND_POINTS)
  trend.value = {
    // 第一个样本没有前值，速率无从计算，不补 0
    down: hasSpeed ? push(trend.value.down, totalSpeed.value.down) : trend.value.down,
    up: hasSpeed ? push(trend.value.up, totalSpeed.value.up) : trend.value.up,
    memory: typeof data.memory === 'number' ? push(trend.value.memory, data.memory) : trend.value.memory
  }
}

let timer: ReturnType<typeof setTimeout> | null = null
let controller: AbortController | null = null
let disposed = false

async function load() {
  controller?.abort()
  controller = new AbortController()
  try {
    const { data } = await mihomoPanelApi.connections(props.agentId, { signal: controller.signal })
    const result: MihomoConnectionsSnapshot = data.data || { connections: [], uploadTotal: 0, downloadTotal: 0 }
    recordTrend(result, updateSpeeds(result))
    snapshot.value = result
    error.value = ''
  } catch (failure: any) {
    if (failure?.code === 'ERR_CANCELED') return
    error.value = panelError(failure, '暂时无法读取连接。')
  }
}

function schedule() {
  if (timer) clearTimeout(timer)
  timer = null
  if (disposed || !active.value) return
  timer = setTimeout(async () => {
    if (document.visibilityState === 'visible') await load()
    schedule()
  }, POLL_MS)
}

let notesEpoch = 0
async function loadNotes() {
  const generation = ++notesEpoch
  try {
    const { data } = await clientToolsApi.notes(props.agentId)
    if (generation !== notesEpoch || disposed || !data?.success) return
    if (JSON.stringify(data.notes) !== JSON.stringify(notes.value)) notes.value = data.notes
  } catch {
    // 备注读不到时显示原始 IP
  }
}

watch(active, shown => {
  // 切回时旧样本已过时，不能拿来算速率
  if (!shown) {
    previous = null
    controller?.abort()
  } else {
    void load()
    void loadNotes()
  }
  schedule()
})
watch(() => props.reloadToken, () => { if (active.value) void load() })
onMounted(async () => {
  if (active.value) {
    void loadNotes()
    await load()
  }
  schedule()
})
onUnmounted(() => {
  disposed = true
  notesEpoch++
  if (timer) clearTimeout(timer)
  controller?.abort()
})
</script>
