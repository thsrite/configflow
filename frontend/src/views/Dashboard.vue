<template>
  <div>
    <header class="relative mb-5 flex flex-wrap items-end gap-4 max-md:mb-4 max-sm:mb-3 max-sm:grid max-sm:grid-cols-[minmax(0,1fr)_auto] max-sm:gap-2">
      <div class="min-w-0 flex-[1_1_260px]">
        <div class="mb-2 flex min-w-0 items-center gap-2 font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">
          <span class="live-dot" aria-hidden="true" />
          <span class="shrink-0">实时流向 ·</span> <span class="truncate normal-case" :title="profileName">{{ profileName }}</span>
        </div>
        <h1 class="font-display m-0 text-[34px] leading-[1.12] text-foreground max-md:text-[26px]">数据统计</h1>
      </div>
      <!-- 实时吞吐：来自已绑定且在线的 Agent 心跳上报的网卡速率；没有上报时不显示 -->
      <div v-if="throughputItems.length" class="flex items-end gap-7 max-md:w-full max-md:gap-5 max-sm:col-span-2 max-sm:row-start-2">
        <div v-for="item in throughputItems" :key="item.label" class="text-right max-md:text-left">
          <div class="font-mono text-[10.5px] tracking-[0.12em] text-muted-foreground uppercase">{{ item.label }}</div>
          <div :class="cn('font-display text-[40px] leading-none max-md:text-[28px]', item.accent && 'text-primary-accent')">
            {{ item.value }}<span class="ml-1 font-mono text-[12px] tracking-normal text-muted-foreground">{{ item.unit }}</span>
          </div>
        </div>
      </div>
      <Button
        variant="outline"
        size="sm"
        class="border-border/70 bg-card/60 max-md:absolute max-md:top-6 max-md:right-0 max-md:size-11 max-md:px-0 max-sm:static max-sm:col-start-2 max-sm:row-start-1 max-sm:self-end"
        :disabled="loading"
        aria-label="刷新"
        @click="loadAllData"
      >
        <RefreshCw class="size-3.5" :class="loading && 'animate-spin'" />
        <span class="max-md:hidden">刷新</span>
      </Button>
    </header>

    <SetupEntry @abandoned="loadAllData" />

    <p
      v-if="loadFailed"
      role="alert"
      class="mb-5 rounded-lg border border-warning-accent/30 bg-warning-soft px-4 py-3 text-sm text-warning-accent"
    >
      部分数据未能加载，显示结果可能不完整。点击「刷新」重试。
    </p>

    <!-- 流向图：配置流向（订阅 → 节点池 → 策略组 → 规则 → 输出 → Agent）与 Mihomo 实时流量两个视图 -->
    <SectionCard :padded="false" class="mb-5 max-sm:mb-3" role="region" :aria-label="flowView === 'live' ? '实时流量' : '配置流向'">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border/60 px-5 py-2.5 max-md:px-4 max-sm:px-3">
        <Segmented v-model="flowView" :options="FLOW_VIEWS" label="流向视图" />
        <span class="text-[12px] text-muted-foreground max-md:hidden">
          {{ flowView === 'live' ? 'Mihomo 当前连接的实时速率' : '配置之间的引用关系' }}
        </span>
      </div>
      <LiveFlow v-if="flowView === 'live'" :agents="liveAgents" />
      <FlowMap
        v-else
        label="配置流向"
        :columns="flowColumns"
        :edges="flowEdges"
        :height="flowHeight"
        :min-width="980"
        :density="flowDensity"
      >
        <template #node="{ node }">
          <span v-if="node.kind === 'pool'" class="block p-3.5">
            <span class="font-display block text-[44px] leading-none text-foreground">
              <AnimatedNumber :value="poolNames.length" />
            </span>
            <span class="mt-1.5 block font-mono text-[10.5px] text-muted-foreground">
              个节点<template v-if="regions.length"> · {{ regions.length }} 个地区</template>
            </span>
            <span v-if="regions.length" class="mt-3 grid gap-1.5">
              <span
                v-for="region in regions"
                :key="region.code"
                class="grid grid-cols-[30px_1fr_30px] items-center gap-1.5 font-mono text-[10.5px] text-muted-foreground"
              >
                <span>{{ region.code }}</span>
                <span class="relative h-1 overflow-hidden rounded-full bg-accent">
                  <span
                    class="region-bar absolute inset-y-0 left-0 rounded-full bg-linear-to-r from-primary to-accent-2"
                    :style="{ width: `${region.ratio * 100}%` }"
                  />
                </span>
                <span class="text-right">{{ region.count }}</span>
              </span>
            </span>
          </span>

          <span v-else-if="node.kind === 'engine'" class="block p-3.5">
            <span class="font-display block text-[30px] leading-none text-foreground">
              <AnimatedNumber :value="counts.rules" />
              <span class="font-sans text-[13px] text-muted-foreground"> 条</span>
            </span>
            <span class="mt-1.5 block truncate font-mono text-[10.5px] whitespace-nowrap text-muted-foreground">
              {{ ruleSetCount ? `${ruleSetCount} 规则集 · ` : '' }}首条命中
            </span>
            <span class="engine-scan mt-3 block h-0.5 overflow-hidden rounded-full bg-accent" aria-hidden="true" />
          </span>
        </template>
      </FlowMap>
      <footer v-if="flowView === 'live'" class="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-border/60 px-5 py-3 text-[12px] text-muted-foreground max-md:px-4 max-sm:gap-x-3 max-sm:px-3 max-sm:py-2">
        <span class="flex items-center gap-1.5"><i class="h-0.5 w-4 rounded-full bg-primary-accent" />设备 → 规则</span>
        <span class="flex items-center gap-1.5"><i class="h-0.5 w-4 rounded-full bg-accent-2" />规则 → 策略</span>
        <span class="flex items-center gap-1.5"><i class="h-0.5 w-4 rounded-full bg-success-accent" />策略 → 出口</span>
        <span class="ml-auto max-md:hidden">粒子越密流量越大 · 每 3 秒采样 · 点击进入连接列表</span>
      </footer>
      <footer v-else class="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-border/60 px-5 py-3 text-[12px] text-muted-foreground max-md:px-4 max-sm:gap-x-3 max-sm:px-3 max-sm:py-2">
        <span class="flex items-center gap-1.5"><i class="h-0.5 w-4 rounded-full bg-primary-accent" />订阅 → 节点</span>
        <span class="flex items-center gap-1.5"><i class="h-0.5 w-4 rounded-full bg-accent-2" />策略 → 规则</span>
        <span class="flex items-center gap-1.5"><i class="h-0.5 w-4 rounded-full bg-success-accent" />下发 → Agent</span>
        <span class="flex items-center gap-1.5"><i class="h-0.5 w-4 rounded-full bg-muted-foreground/50" />离线</span>
        <span class="ml-auto max-md:hidden">悬停任意节点追踪完整链路 · 点击进入</span>
      </footer>
    </SectionCard>

    <!-- KPI：数字 + 说明 + 真实趋势（计数快照 / 会话内吞吐采样） -->
    <div class="mb-5 grid grid-cols-4 gap-3.5 max-[1180px]:grid-cols-2 max-md:gap-2.5 max-sm:mb-3 max-sm:gap-2" role="region" aria-label="关键指标">
      <button
        v-for="kpi in kpis"
        :key="kpi.label"
        type="button"
        class="group relative overflow-hidden rounded-[18px] border border-border bg-card/90 px-[18px] pt-4 pb-3 text-left shadow-surface transition-[transform,border-color] duration-350 ease-(--ease-flow) hover:-translate-y-[3px] hover:border-border-strong focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none max-sm:p-3"
        @click="router.push(kpi.route)"
      >
        <span class="flex items-center gap-2 text-[12.5px] text-muted-foreground">
          <component :is="kpi.icon" class="size-[15px] opacity-70" :stroke-width="2" aria-hidden="true" />
          {{ kpi.label }}
          <span
            :class="cn(
              'ml-auto rounded-full border px-1.5 py-px font-mono text-[9.5px] tracking-[0.06em]',
              kpi.scope === 'profile'
                ? 'border-primary-accent/40 bg-primary-soft text-primary-accent'
                : kpi.scope === 'resource'
                  ? 'border-info-accent/40 text-info-accent'
                  : 'border-border-strong text-muted-foreground'
            )"
          >
            {{ SCOPE_LABEL[kpi.scope] }}
          </span>
        </span>
        <span class="mt-2.5 flex items-baseline gap-2">
          <b class="font-display text-[42px] leading-none font-medium tracking-[-0.03em] text-foreground max-md:text-[32px]">
            <AnimatedNumber :value="kpi.value" />
          </b>
          <span class="truncate font-mono text-[11.5px] text-muted-foreground">{{ kpi.delta }}</span>
        </span>
        <Sparkline :data="kpi.series" :color="kpi.color" class="mt-2 h-[38px] w-full" :title="kpi.seriesLabel" />
      </button>
    </div>

    <div class="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)] items-stretch gap-3.5 max-[1180px]:grid-cols-[minmax(0,1fr)] max-sm:gap-3">
      <!-- 事件流：系统日志实时尾随，新事件从顶部进入 -->
      <SectionCard :padded="false" class="flex min-w-0 flex-col" role="region" aria-label="事件流">
        <header class="flex items-center gap-2.5 px-[18px] pt-4 max-sm:px-3 max-sm:pt-3">
          <span class="live-dot" aria-hidden="true" />
          <h2 class="m-0 flex min-h-7 items-center text-[13.5px] font-semibold">事件流</h2>
          <span class="ml-auto font-mono text-[12px] text-muted-foreground">{{ events.length }} 条</span>
        </header>
        <div
          :class="cn('event-stream mt-2.5 h-[360px] grow overflow-x-hidden overflow-y-auto focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring/50', !events.length && 'max-sm:h-auto max-sm:grow-0')"
          role="region"
          aria-label="最近日志事件"
          :tabindex="events.length ? 0 : undefined"
        >
          <p v-if="!events.length" class="px-[18px] py-10 text-center text-[13px] max-sm:px-3 max-sm:py-6 text-muted-foreground">
            {{ eventsLoaded ? '还没有日志事件' : '正在读取日志…' }}
          </p>
          <TransitionGroup name="event" tag="div">
            <div
              v-for="event in events"
              :key="event.key"
              class="grid grid-cols-[62px_22px_minmax(0,1fr)_auto] items-center gap-2.5 border-t border-border/70 px-[18px] py-2.5 first:border-t-0 max-sm:gap-2 max-sm:px-3"
            >
              <time class="font-mono text-[11px] text-muted-foreground">{{ event.time }}</time>
              <span
                :class="cn(
                  'grid size-[22px] place-items-center rounded-[7px] bg-secondary text-muted-foreground',
                  event.tone === 'warn' && 'text-warning-accent',
                  event.tone === 'err' && 'text-destructive-accent'
                )"
              >
                <component :is="event.icon" class="size-[13px]" :stroke-width="2" aria-hidden="true" />
              </span>
              <p class="m-0 truncate text-[13px] text-muted-foreground" :title="event.message">
                <b class="font-medium text-foreground">{{ event.task }}</b> {{ event.message }}
              </p>
              <span class="font-mono text-[11px] text-muted-foreground">{{ event.tag }}</span>
            </div>
          </TransitionGroup>
        </div>
      </SectionCard>

      <!-- 订阅健康：每个订阅最近 24 次拉取，柱高为节点数，颜色为结果 -->
      <SectionCard :padded="false" class="flex min-w-0 flex-col" role="region" aria-label="订阅健康">
        <header class="flex items-center gap-2.5 px-[18px] pt-4 max-sm:px-3 max-sm:pt-3">
          <h2 class="m-0 text-[13.5px] font-semibold">订阅健康 · 近 {{ HEALTH_SIZE }} 次拉取</h2>
          <Button
            variant="ghost"
            size="icon-sm"
            class="ml-auto size-7"
            aria-label="刷新订阅健康"
            :disabled="healthLoading"
            @click="loadHealth"
          >
            <RefreshCw class="size-3.5" :class="healthLoading && 'animate-spin'" />
          </Button>
        </header>
        <div class="flex flex-1 flex-col px-[18px] pt-3.5 pb-[18px] max-sm:p-3">
          <p v-if="!healthRows.length" class="py-6 text-center text-[13px] text-muted-foreground">还没有订阅</p>
          <div
            v-for="row in healthRows"
            :key="row.id"
            :class="cn('grid grid-cols-[minmax(0,1fr)_auto] gap-x-2.5 gap-y-1.5 border-t border-border/70 py-2.5 first:border-t-0 first:pt-0', !row.enabled && 'opacity-55')"
          >
            <b class="truncate text-[13px] font-medium">{{ row.name }}</b>
            <span class="font-mono text-[12px] text-muted-foreground">{{ row.summary }}</span>
            <div class="col-span-2 flex h-[18px] items-end gap-0.5" :aria-label="`${row.name} 最近拉取记录`">
              <i
                v-for="(bar, i) in row.bars"
                :key="i"
                :title="bar.title"
                :class="cn(
                  'flex-1 rounded-[2px] transition-[height] duration-600',
                  bar.status === 'ok' && 'bg-success-accent/80',
                  bar.status === 'cache' && 'bg-warning-accent/85',
                  bar.status === 'fail' && 'bg-destructive-accent/85',
                  bar.status === 'empty' && 'bg-accent'
                )"
                :style="{ height: `${bar.height}%` }"
              />
            </div>
          </div>

          <div class="mt-auto grid grid-cols-2 gap-2 pt-3.5">
            <button
              v-for="action in QUICK_ACTIONS"
              :key="action.id"
              type="button"
              class="flex flex-col items-start gap-2 rounded-xl border border-border bg-background/60 p-3 text-left text-[12.5px] text-muted-foreground transition-[transform,border-color,color] duration-250 ease-(--ease-flow) hover:-translate-y-0.5 hover:border-primary-accent/45 hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none max-sm:min-h-11 max-sm:flex-row max-sm:items-center max-sm:gap-2 max-sm:p-2"
              @click="runAction(router, action.id)"
            >
              <component :is="action.icon" class="size-[18px] text-primary-accent" :stroke-width="1.75" aria-hidden="true" />
              {{ ACTION_LABEL[action.id] }}
            </button>
          </div>
        </div>
      </SectionCard>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import api, { nodeApi, proxyGroupApi, ruleSetApi, statsApi, subscriptionApi } from '@/api'
import {
  Activity,
  Download,
  FileCode2,
  LayoutGrid,
  Link2,
  MoreHorizontal,
  Network,
  RefreshCw,
  Send,
  Server,
  TriangleAlert,
  Waypoints,
  Zap
} from '@lucide/vue'
import { Button } from '@/components/ui/button'
import AnimatedNumber from '@/components/common/AnimatedNumber.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import Sparkline from '@/components/common/Sparkline.vue'
import Segmented from '@/components/common/Segmented.vue'
import FlowMap from '@/components/dashboard/FlowMapViewer.vue'
import type { FlowColumn, FlowEdge, FlowNode } from '@/components/dashboard/FlowMap.vue'
import LiveFlow from '@/components/dashboard/LiveFlow.vue'
import { ACTION_LABEL, runAction, type AppAction } from '@/lib/actions'
import { cn } from '@/lib/utils'
import { regionOf } from '@/lib/regions'
import { formatRate, useLive } from '@/stores/live'
import { useProfileStore } from '@/stores/profile'
import SetupEntry from '@/components/setup/SetupEntry.vue'

const router = useRouter()
const profileStore = useProfileStore()
const live = useLive()
const { boundAgents, speedOf, downHistory } = live

const loading = ref(false)
let disposed = false
const canRefresh = () => !disposed && !document.hidden
const loadFailed = ref(false)
const counts = ref({ subscriptions: 0, nodes: 0, proxyGroups: 0, rules: 0 })
const history = ref<Record<string, number[]>>({})
const subscriptions = ref<any[]>([])
const proxyGroups = ref<any[]>([])
const nodeNames = ref<string[]>([])
/** 订阅缓存里的节点名（已启用订阅），与节点库合并成节点池 */
const subscriptionNodeNames = ref<string[]>([])
const poolNames = computed(() => [...new Set([...nodeNames.value, ...subscriptionNodeNames.value])])
const ruleSetCount = ref(0)
const latency = ref<Record<string, { latency: number | null }>>({})

const profileName = computed(
  () => profileStore.activeProfile.value?.name || profileStore.activeProfileId.value
)

/* ---------- 实时吞吐 ---------- */
const throughputItems = computed(() => {
  const t = live.throughput.value
  if (!t) return []
  const [down, downUnit] = formatRate(t.down)
  const [up, upUnit] = formatRate(t.up)
  return [
    { label: '下行', value: down, unit: downUnit, accent: true },
    { label: '上行', value: up, unit: upUnit, accent: false },
    { label: '上报 Agent', value: String(t.agents), unit: '台', accent: false }
  ]
})

/** 粒子密度跟随真实吞吐：无上报时保持基础流速，10 MB/s 左右约为 2 倍 */
const flowDensity = computed(() => {
  const t = live.throughput.value
  if (!t) return 1
  const total = t.up + t.down
  return Math.min(2.6, Math.max(0.5, 0.6 + Math.log10(total / 1024 + 1) * 0.35))
})

/* ---------- 流向视图切换：选择记在本机浏览器 ---------- */
type FlowView = 'config' | 'live'
const FLOW_VIEWS: Array<{ value: FlowView; label: string }> = [
  { value: 'config', label: '配置流向' },
  { value: 'live', label: '实时流量' }
]
const FLOW_VIEW_KEY = 'configflow:dashboard:flow-view'
const readFlowView = (): FlowView => {
  try { return localStorage.getItem(FLOW_VIEW_KEY) === 'live' ? 'live' : 'config' } catch { return 'config' }
}
const flowView = ref<FlowView>(readFlowView())
watch(flowView, value => {
  try { localStorage.setItem(FLOW_VIEW_KEY, value) } catch { /* 存储不可用时只在本次会话内生效 */ }
})

/** 实时流量只取绑定当前配置空间、在线的 Mihomo Agent */
const liveAgents = computed(() =>
  boundAgents.value.filter(a => (a.service_type || 'mihomo') === 'mihomo' && live.isOnline(a))
)

/** 每列最多展示的节点数，超出折叠为「+N」 */
const COLUMN_LIMIT = { subs: 4, groups: 6, agents: 5 }

const regions = computed(() => {
  const counts = new Map<string, number>()
  for (const name of poolNames.value) {
    const code = regionOf(name).code
    counts.set(code, (counts.get(code) || 0) + 1)
  }
  const rows = [...counts.entries()]
    .filter(([code]) => code !== '其他')
    .sort((a, b) => b[1] - a[1])
    .slice(0, 5)
  const other = poolNames.value.length - rows.reduce((sum, [, n]) => sum + n, 0)
  if (other > 0 && rows.length) rows.push(['其他', other])
  const max = Math.max(1, ...rows.map(([, n]) => n))
  return rows.map(([code, count]) => ({ code, count, ratio: count / max }))
})

/** 权重按数量取对数，避免大订阅把其他链路的粒子淹没 */
const weightOf = (n: number) => Math.min(1.8, Math.max(0.3, Math.log10(n + 1) * 0.8))

const moreNode = (id: string, n: number, to: string): FlowNode => ({
  id,
  title: `其余 ${n} 项`,
  meta: '查看全部',
  icon: MoreHorizontal,
  to,
  muted: true
})

/** 旧数据里的订阅可能缺少 id，回落到名称和序号，保证节点 id 唯一 */
const subKey = (s: any, index: number) => `sub:${s.id || `${s.name}#${index}`}`


const flowColumns = computed<FlowColumn[]>(() => {
  const subs = [...subscriptions.value].sort((a, b) => Number(b.enabled) - Number(a.enabled))
  const subNodes: FlowNode[] = subs.slice(0, COLUMN_LIMIT.subs).map(s => ({
    id: subKey(s, subscriptions.value.indexOf(s)),
    title: s.name,
    meta: s.cached_node_count != null ? `${s.cached_node_count} 节点` : s.type || '订阅',
    icon: Link2,
    to: '/subscriptions',
    muted: !s.enabled
  }))
  if (subs.length > COLUMN_LIMIT.subs) subNodes.push(moreNode('sub:more', subs.length - COLUMN_LIMIT.subs, '/subscriptions'))
  if (!subNodes.length) subNodes.push({ id: 'sub:empty', title: '还没有订阅', meta: '去添加', icon: Link2, to: '/subscriptions', muted: true })

  const groups = proxyGroups.value
  const groupNodes: FlowNode[] = groups.slice(0, COLUMN_LIMIT.groups).map((g, i) => ({
    id: `group:${g.id || g.name || i}`,
    title: g.name,
    meta: g.type,
    icon: LayoutGrid,
    to: '/proxy-groups',
    muted: g.enabled === false
  }))
  if (groups.length > COLUMN_LIMIT.groups) groupNodes.push(moreNode('group:more', groups.length - COLUMN_LIMIT.groups, '/proxy-groups'))
  if (!groupNodes.length) groupNodes.push({ id: 'group:empty', title: '还没有策略组', meta: '去创建', icon: LayoutGrid, to: '/proxy-groups', muted: true })

  const bound = boundAgents.value
  const agentNodes: FlowNode[] = bound.slice(0, COLUMN_LIMIT.agents).map(a => ({
    id: `agent:${a.id}`,
    title: a.name,
    meta: a.status === 'online' ? a.service_type : '离线',
    icon: Server,
    to: '/agents',
    status: a.status === 'online' && a.enabled !== false ? 'online' : 'offline'
  }))
  if (bound.length > COLUMN_LIMIT.agents) agentNodes.push(moreNode('agent:more', bound.length - COLUMN_LIMIT.agents, '/agents'))
  if (!agentNodes.length) agentNodes.push({ id: 'agent:empty', title: '未绑定 Agent', meta: '去绑定', icon: Server, to: '/agents', muted: true })

  return [
    { key: 'subs', title: '订阅来源', nodes: subNodes },
    { key: 'pool', title: '节点池', nodes: [{ id: 'pool', title: '节点池', kind: 'pool', to: '/nodes' }] },
    { key: 'groups', title: '策略组', nodes: groupNodes },
    { key: 'rules', title: '规则引擎', nodes: [{ id: 'rules', title: '策略规则', kind: 'engine', to: '/rules' }] },
    {
      key: 'outputs',
      title: '输出',
      nodes: [
        { id: 'out:mihomo', title: 'Mihomo', meta: 'config.yaml', icon: FileCode2, to: '/generate' },
        { id: 'out:surge', title: 'Surge', meta: 'surge.conf', icon: FileCode2, to: '/generate' },
        { id: 'out:loon', title: 'Loon', meta: 'loon.conf', icon: FileCode2, to: '/generate' },
        { id: 'out:mosdns', title: 'MosDNS', meta: 'config.yaml', icon: Waypoints, to: '/generate' },
        { id: 'out:oxidns', title: 'OxiDNS', meta: 'config.yaml', icon: Waypoints, to: '/generate' }
      ]
    },
    { key: 'agents', title: 'Agent', nodes: agentNodes }
  ]
})

const flowEdges = computed<FlowEdge[]>(() => {
  const edges: FlowEdge[] = []
  const [subCol, , groupCol, , , agentCol] = flowColumns.value
  for (const node of subCol.nodes) {
    if (node.id === 'sub:empty') continue
    const sub = subscriptions.value.find((s, i) => subKey(s, i) === node.id)
    edges.push({ from: node.id, to: 'pool', weight: sub ? weightOf(sub.cached_node_count || 0) : 0.3, dead: node.muted && !!sub })
  }
  for (const node of groupCol.nodes) {
    if (node.id === 'group:empty') continue
    const dead = node.muted && node.id !== 'group:more'
    edges.push({ from: 'pool', to: node.id, weight: 0.7, dead })
    edges.push({ from: node.id, to: 'rules', weight: 0.6, tone: 'kraft', dead })
  }
  edges.push(
    { from: 'rules', to: 'out:mihomo', weight: 1.2, tone: 'kraft' },
    { from: 'rules', to: 'out:surge', weight: 0.5, tone: 'kraft' },
    { from: 'rules', to: 'out:loon', weight: 0.4, tone: 'kraft' },
    { from: 'rules', to: 'out:mosdns', weight: 0.6, tone: 'kraft' },
    { from: 'rules', to: 'out:oxidns', weight: 0.6, tone: 'kraft' }
  )
  for (const node of agentCol.nodes) {
    if (node.id === 'agent:empty') continue
    const agent = boundAgents.value.find(a => `agent:${a.id}` === node.id)
    const from = agent?.service_type === 'oxidns' ? 'out:oxidns' : agent?.service_type === 'mosdns' ? 'out:mosdns' : agent?.service_type === 'surge' ? 'out:surge' : 'out:mihomo'
    const sp = agent ? speedOf(agent) : null
    const weight = sp?.reported ? weightOf((sp.up + sp.down) / 1024 / 64) : 0.8
    edges.push({ from, to: node.id, weight, tone: 'success', dead: !!agent && node.status !== 'online' })
  }
  return edges
})

/** 列中节点越多，画布越高 */
const flowHeight = computed(() => {
  const most = Math.max(...flowColumns.value.map(c => (c.key === 'pool' ? 4 : c.nodes.length)))
  return Math.max(340, most * 66)
})


/* ---------- KPI ---------- */
type Scope = 'resource' | 'profile' | 'system'
const SCOPE_LABEL: Record<Scope, string> = { resource: '资源', profile: '当前配置', system: '系统' }

const nodeAvailability = computed(() => {
  const tested = Object.values(latency.value).filter(r => r && 'latency' in r)
  if (!tested.length) return null
  return Math.round((tested.filter(r => r.latency != null).length / tested.length) * 100)
})

const kpis = computed(() => {
  const withTraffic = subscriptions.value.filter(s => s.traffic && s.traffic.total).length
  const online = live.onlineCount.value
  return [
    {
      label: '订阅来源',
      value: counts.value.subscriptions,
      delta: withTraffic ? `${withTraffic} 个有流量信息` : '无流量信息',
      icon: Link2,
      scope: 'resource' as Scope,
      route: '/subscriptions',
      series: history.value.subscriptions || [],
      seriesLabel: '订阅数量变化',
      color: '--primary-accent'
    },
    {
      label: '节点',
      value: poolNames.value.length,
      delta: nodeAvailability.value == null ? '尚未测速' : `可用 ${nodeAvailability.value}%`,
      icon: Network,
      scope: 'resource' as Scope,
      route: '/nodes',
      series: history.value.nodes || [],
      seriesLabel: '节点数量变化',
      color: '--info-accent'
    },
    {
      label: '策略组',
      value: counts.value.proxyGroups,
      delta: `${counts.value.rules} 条规则`,
      icon: LayoutGrid,
      scope: 'profile' as Scope,
      route: '/proxy-groups',
      series: history.value.proxyGroups || [],
      seriesLabel: '策略组数量变化',
      color: '--accent-2'
    },
    {
      label: '在线 Agent',
      value: online,
      delta: `/ ${boundAgents.value.length} 台`,
      icon: Server,
      scope: 'system' as Scope,
      route: '/agents',
      series: downHistory.value,
      seriesLabel: '下行吞吐（本次会话）',
      color: '--success-accent'
    }
  ]
})

/* ---------- 事件流：解析系统日志，不编造记录 ---------- */
interface StreamEvent {
  key: string
  time: string
  task: string
  message: string
  tag: string
  tone: 'warn' | 'err' | ''
  icon: typeof Activity
}

const LOG_LINE = /^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2})[,.]?\d*\s+-\s+([\w.]+)\s+-\s+(\w+)\s+-\s+(.*)$/
const events = ref<StreamEvent[]>([])
const eventsLoaded = ref(false)
let eventsLoading = false

const classify = (logger: string, message: string, level: string): Pick<StreamEvent, 'task' | 'tone' | 'icon'> => {
  const text = `${logger} ${message}`.toLowerCase()
  if (level === 'ERROR' || level === 'CRITICAL') return { task: '错误', tone: 'err', icon: TriangleAlert }
  if (level === 'WARNING') return { task: '警告', tone: 'warn', icon: TriangleAlert }
  if (text.includes('subscription') || text.includes('订阅')) return { task: '订阅', tone: '', icon: Link2 }
  if (text.includes('generate') || text.includes('生成')) return { task: '生成', tone: '', icon: Download }
  if (text.includes('agent')) return { task: 'Agent', tone: '', icon: Server }
  if (text.includes('rule') || text.includes('规则')) return { task: '规则', tone: '', icon: FileCode2 }
  return { task: '系统', tone: '', icon: Activity }
}

const loadEvents = async () => {
  if (!canRefresh() || eventsLoading) return
  eventsLoading = true
  try {
    const { data } = await api.get('/logs/tail', { params: { lines: 60 } })
    const lines: string[] = data?.logs || []
    const parsed: StreamEvent[] = []
    for (const line of lines) {
      const m = LOG_LINE.exec(line)
      if (!m) continue
      const [, date, time, logger, level, message] = m
      parsed.push({
        key: `${date} ${line}`,
        time,
        message,
        tag: level === 'INFO' ? logger.split('.').pop() || logger : level,
        ...classify(logger, message, level)
      })
    }
    events.value = parsed.reverse().slice(0, 12)
  } catch {
    // 日志不可读时保留上次结果
  } finally {
    eventsLoading = false
    eventsLoaded.value = true
  }
}

/* ---------- 订阅健康 ---------- */
const HEALTH_SIZE = 24
const health = ref<any[]>([])
const healthLoading = ref(false)

const healthRows = computed(() =>
  health.value.map(item => {
    const records: any[] = item.history || []
    const maxCount = Math.max(1, ...records.map(r => r.count || 0))
    const bars = [
      ...Array.from({ length: Math.max(0, HEALTH_SIZE - records.length) }, () => ({
        status: 'empty',
        height: 30,
        title: '暂无记录'
      })),
      ...records.map(r => ({
        status: r.status,
        height: r.status === 'fail' ? 30 : Math.max(30, Math.round(((r.count || 0) / maxCount) * 100)),
        title: `${new Date(r.at).toLocaleString('zh-CN', { hour12: false })} · ${
          r.status === 'ok' ? `成功 ${r.count} 节点` : r.status === 'cache' ? `失败，使用缓存 ${r.count} 节点` : '失败'
        }`
      }))
    ]
    const usable = records.filter(r => r.status !== 'fail').length
    return {
      id: item.id,
      name: item.name,
      enabled: item.enabled !== false,
      summary: records.length ? `${Math.round((usable / records.length) * 100)}% 可用` : '暂无记录',
      bars
    }
  })
)

const loadHealth = async () => {
  if (!canRefresh() || healthLoading.value) return
  healthLoading.value = true
  try {
    const { data } = await subscriptionApi.health()
    health.value = data?.items || []
  } catch {
    // 保留上次结果
  } finally {
    healthLoading.value = false
  }
}

/* ---------- 快捷操作 ---------- */
const QUICK_ACTIONS: Array<{ id: AppAction; icon: typeof Zap }> = [
  { id: 'speedtest', icon: Zap },
  { id: 'generate-mihomo', icon: Download },
  { id: 'pull-all', icon: RefreshCw },
  { id: 'push-all', icon: Send }
]

/* ---------- 数据加载 ---------- */
const loadAllData = async () => {
  if (!canRefresh() || loading.value) return
  loading.value = true
  try {
    const [stats, subRes, groupRes, ruleSetRes, nodeRes, latencyRes] = await Promise.allSettled([
      statsApi.getOverview(),
      subscriptionApi.getAll(),
      proxyGroupApi.getAll(),
      ruleSetApi.getAll(),
      nodeApi.getAll(),
      nodeApi.latency()
    ])

    if (!canRefresh()) return

    loadFailed.value = [stats, subRes, groupRes, ruleSetRes, nodeRes].some(result => result.status === 'rejected')
      || (stats.status === 'fulfilled' && !stats.value.data?.success)

    if (stats.status === 'fulfilled' && stats.value.data?.success) {
      const d = stats.value.data.data
      counts.value = {
        subscriptions: d.subscriptions?.total ?? 0,
        nodes: d.nodes?.total ?? 0,
        proxyGroups: d.proxyGroups?.total ?? 0,
        rules: d.rules?.total ?? 0
      }
      history.value = {
        subscriptions: d.subscriptions?.history || [],
        nodes: d.nodes?.history || [],
        proxyGroups: d.proxyGroups?.history || [],
        rules: d.rules?.history || []
      }
    }
    if (subRes.status === 'fulfilled') subscriptions.value = subRes.value.data || []
    if (groupRes.status === 'fulfilled') proxyGroups.value = groupRes.value.data || []
    if (ruleSetRes.status === 'fulfilled') ruleSetCount.value = (ruleSetRes.value.data || []).length
    if (nodeRes.status === 'fulfilled') {
      nodeNames.value = (nodeRes.value.data || []).map((n: any) => String(n.name || ''))
    }
    if (latencyRes.status === 'fulfilled') latency.value = latencyRes.value.data?.results || {}

    // 订阅节点：用策略组的正则预览接口取全部候选（.* 匹配所有），口径与生成配置一致
    const enabledSubs = subscriptions.value.filter(s => s.enabled !== false && s.id).map(s => s.id)
    if (enabledSubs.length) {
      try {
        const { data } = await proxyGroupApi.previewRegex({ source: 'subscription', regex: '.*', subscriptions: enabledSubs })
        subscriptionNodeNames.value = (data.nodes || []).map((n: any) => String(n.name || ''))
      } catch {
        subscriptionNodeNames.value = []
      }
    } else {
      subscriptionNodeNames.value = []
    }
    // Keep this refresh in flight through its follow-up reads as well.
    if (!canRefresh()) return
    await Promise.all([loadHealth(), loadEvents(), live.refresh()])
  } finally {
    loading.value = false
  }
}

let timer: number | undefined
let eventTimer: number | undefined

const onVisibilityChange = () => {
  if (canRefresh()) void loadAllData()
}

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibilityChange)
  live.start()
  loadAllData()
  timer = window.setInterval(loadAllData, 30000)
  // 事件流比其他数据刷新得更勤，页面不可见时跳过
  eventTimer = window.setInterval(loadEvents, 5000)
})

onUnmounted(() => {
  disposed = true
  document.removeEventListener('visibilitychange', onVisibilityChange)
  live.stop()
  clearInterval(timer)
  clearInterval(eventTimer)
})
</script>

<style scoped>
.region-bar {
  transform-origin: left;
  animation: region-grow 1.4s var(--ease-flow) both;
}

@keyframes region-grow {
  from {
    transform: scaleX(0);
  }
}

.engine-scan {
  position: relative;
}

.engine-scan::after {
  content: '';
  position: absolute;
  inset: 0 auto 0 -40%;
  width: 40%;
  background: linear-gradient(90deg, transparent, var(--accent-2), transparent);
  animation: engine-scan 1.8s linear infinite;
}

@keyframes engine-scan {
  to {
    left: 100%;
  }
}

.event-enter-active {
  transition:
    opacity 0.7s var(--ease-flow),
    transform 0.7s var(--ease-flow),
    background-color 1.2s var(--ease-flow);
}

.event-enter-from {
  opacity: 0;
  transform: translateY(-14px);
  background-color: var(--primary-soft);
}

.event-move {
  transition: transform 0.6s var(--ease-flow);
}

@media (prefers-reduced-motion: reduce) {
  .region-bar,
  .engine-scan::after {
    animation: none;
  }
}
</style>
