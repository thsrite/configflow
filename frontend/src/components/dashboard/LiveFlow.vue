<template>
  <div>
    <div
      v-if="agents.length"
      class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border/60 px-5 py-1.5 text-[12px] text-muted-foreground max-md:px-4"
    >
      <!-- 多台 Mihomo Agent 时手动切换；默认看当前流量最大的一台 -->
      <div v-if="agents.length > 1" class="flex flex-wrap items-center gap-1.5" role="group" aria-label="选择 Agent">
        <button
          v-for="agent in agents"
          :key="agent.id"
          type="button"
          :aria-pressed="agent.id === currentId"
          :class="cn('chip cursor-pointer', agent.id === currentId && 'chip-acc')"
          @click="pick(agent.id)"
        >
          {{ agent.name || agent.id }}
          <span class="font-mono text-[10.5px] opacity-80">{{ agentBadge(agent.id) }}</span>
        </button>
      </div>
      <span v-if="current?.snapshot?.interval" class="num ml-auto font-mono max-md:ml-0">
        ↓ {{ rateText(current.snapshot.down) }} · ↑ {{ rateText(current.snapshot.up) }}
        · {{ current.snapshot.active }} / {{ current.snapshot.connections }} 条连接有流量
      </span>
    </div>

    <FlowMap
      v-if="graph"
      label="实时流量"
      :columns="graph.columns"
      :edges="graph.edges"
      :height="height"
      :min-width="980"
      :density="density"
    />
    <EmptyState v-else :icon="empty.icon" :title="empty.title" :description="empty.description" role="status">
      <Button v-if="empty.link" variant="outline" size="sm" @click="router.push(empty.link)">{{ empty.linkLabel }}</Button>
    </EmptyState>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Activity, Hourglass, ServerOff, TriangleAlert } from '@lucide/vue'
import { clientToolsApi, mihomoPanelApi } from '@/api'
import { Button } from '@/components/ui/button'
import EmptyState from '@/components/common/EmptyState.vue'
import FlowMap from '@/components/dashboard/FlowMapViewer.vue'
import { buildLiveFlow, rateText, type LiveFlowSnapshot } from '@/lib/live-flow'
import { deviceNote } from '@/lib/device-notes'
import { cn } from '@/lib/utils'
import { panelError } from '@/types/mihomo'

/**
 * 实时流量视图：轮询在线 Mihomo Agent 的聚合流向，只展示真实速率。
 * 没有可用 Agent、仍在建立基线或当前没有流量时给出空状态，不回落到其他数据。
 */
const props = defineProps<{
  agents: Array<{ id: string; name?: string }>
  /** 所在标签隐藏时暂停轮询，切回后立即补采 */
  paused?: boolean
}>()

const POLL_MS = 3000
const STORAGE_KEY = 'configflow:dashboard:live-agent'

interface AgentFlow {
  snapshot?: LiveFlowSnapshot
  error?: string
}

const router = useRouter()
const flows = ref<Record<string, AgentFlow>>({})
const notes = ref<Record<string, Record<string, string>>>({})
const loaded = ref(false)

const readPick = () => {
  try { return localStorage.getItem(STORAGE_KEY) || '' } catch { return '' }
}
const picked = ref(readPick())
const pick = (id: string) => {
  // 再点一次已选中的 Agent 恢复自动选择
  picked.value = picked.value === id ? '' : id
  try {
    if (picked.value) localStorage.setItem(STORAGE_KEY, picked.value)
    else localStorage.removeItem(STORAGE_KEY)
  } catch {
    // 存储不可用时只在本次会话内生效
  }
}

const rateOf = (id: string) => {
  const s = flows.value[id]?.snapshot
  return s?.interval ? s.up + s.down : 0
}

/** 手动选择优先；否则取当前流量最大的一台，都没有流量时不选 */
const currentId = computed(() => {
  if (picked.value && props.agents.some(a => a.id === picked.value)) return picked.value
  let best = ''
  for (const agent of props.agents) if (rateOf(agent.id) > (best ? rateOf(best) : 0)) best = agent.id
  return best
})
const current = computed(() => (currentId.value ? flows.value[currentId.value] : undefined))

const agentBadge = (id: string) => {
  const flow = flows.value[id]
  if (flow?.error) return '不可用'
  if (!flow?.snapshot?.interval) return '采样中'
  const rate = rateOf(id)
  return rate ? rateText(rate) : '无流量'
}

const graph = computed(() => {
  const id = currentId.value
  const s = current.value?.snapshot
  if (!id || !s?.interval || !s.paths.some(p => p.up + p.down > 0)) return null
  const deviceNotes = notes.value[id] || {}
  return buildLiveFlow(s.paths, {
    labelOf: ip => deviceNote(deviceNotes, ip) || ip,
    to: `/mihomo?agent=${encodeURIComponent(id)}&tab=connections`
  })
})

const height = computed(() => {
  const most = Math.max(0, ...(graph.value?.columns.map(c => c.nodes.length) || []))
  return Math.max(340, most * 66)
})

/** 粒子密度跟随当前 Agent 的真实速率，10 MB/s 左右约为 2 倍 */
const density = computed(() => {
  const s = current.value?.snapshot
  const total = s ? s.up + s.down : 0
  return Math.min(2.6, Math.max(0.5, 0.6 + Math.log10(total / 1024 + 1) * 0.35))
})

const empty = computed(() => {
  if (!props.agents.length) {
    return { icon: ServerOff, title: '没有在线的 Mihomo Agent', description: '实时流量来自 Mihomo 内核的连接数据，绑定当前配置空间的 Mihomo Agent 上线后显示。', link: '/agents', linkLabel: '查看 Agent' }
  }
  const scoped = currentId.value ? [currentId.value] : props.agents.map(a => a.id)
  const states = scoped.map(id => flows.value[id])
  if (!loaded.value || states.some(f => !f || (!f.error && !f.snapshot?.interval))) {
    return { icon: Hourglass, title: '正在采样', description: '两次采样之间的字节差即实时速率，几秒后显示。' }
  }
  const errors = states.filter(f => f?.error)
  if (errors.length === states.length) {
    return { icon: TriangleAlert, title: '暂时读不到实时流量', description: errors[0]!.error, link: '/agents', linkLabel: '查看 Agent' }
  }
  const s = current.value?.snapshot
  return {
    icon: Activity,
    title: '当前没有流量',
    description: s?.connections ? `${s.connections} 条连接在最近一次采样内都没有收发数据。` : '没有活跃连接，有流量经过 Mihomo 时自动显示。'
  }
})

/* ---------- 轮询 ---------- */
let timer: number | undefined
let disposed = false
const inflight = new Set<string>()
const controllers = new Set<AbortController>()

const pollAgent = async (id: string) => {
  if (inflight.has(id)) return
  inflight.add(id)
  const controller = new AbortController()
  controllers.add(controller)
  try {
    const { data } = await mihomoPanelApi.flow(id, { signal: controller.signal })
    if (disposed) return
    const snapshot = data?.data as LiveFlowSnapshot | undefined
    flows.value = { ...flows.value, [id]: snapshot && Array.isArray(snapshot.paths) ? { snapshot } : { error: 'Agent 返回的数据格式异常。' } }
  } catch (failure: any) {
    if (disposed || controller.signal.aborted) return
    flows.value = { ...flows.value, [id]: { error: panelError(failure, '暂时无法连接 Agent。') } }
  } finally {
    inflight.delete(id)
    controllers.delete(controller)
  }
}

const poll = async () => {
  if (disposed || document.hidden || props.paused) return
  await Promise.all(props.agents.map(a => pollAgent(a.id)))
  if (!disposed) loaded.value = true
}

const loadNotes = async (id: string) => {
  if (!id || notes.value[id]) return
  notes.value = { ...notes.value, [id]: {} }
  try {
    const { data } = await clientToolsApi.notes(id)
    if (!disposed) notes.value = { ...notes.value, [id]: data?.notes || {} }
  } catch {
    // 备注读取失败时显示原始 IP
  }
}

const onVisibilityChange = () => {
  if (!document.hidden) void poll()
}

watch(currentId, id => void loadNotes(id), { immediate: true })
watch(() => props.paused, paused => { if (!paused) void poll() })
// Agent 上下线后立即补采新出现的 Agent，并丢掉已离开的结果
watch(
  () => props.agents.map(a => a.id).join('\n'),
  () => {
    const ids = new Set(props.agents.map(a => a.id))
    flows.value = Object.fromEntries(Object.entries(flows.value).filter(([id]) => ids.has(id)))
    void poll()
  }
)

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibilityChange)
  void poll()
  timer = window.setInterval(poll, POLL_MS)
})

onUnmounted(() => {
  disposed = true
  document.removeEventListener('visibilitychange', onVisibilityChange)
  clearInterval(timer)
  controllers.forEach(c => c.abort())
})
</script>
