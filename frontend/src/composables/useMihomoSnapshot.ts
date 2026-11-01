import { onMounted, onUnmounted, ref, watch, type Ref } from 'vue'
import { clientToolsApi, mihomoPanelApi } from '@/api'
import { panelError, type MihomoConnectionsSnapshot } from '@/types/mihomo'

const POLL_MS = 2000
/** 趋势图保留的采样数：2 秒一次，约 2 分钟 */
const TREND_POINTS = 60

/**
 * Mihomo 面板「概览」「统计」共用：所在标签可见时每 2 秒读一次连接列表，算出每条连接与总的速率，
 * 并读取客户端设备备注。标签隐藏、页面切到后台时暂停；切回时旧样本已过时，重新建立基线。
 */
export function useMihomoSnapshot(agentId: () => string, active: Ref<boolean>, reloadToken: () => number | undefined) {
  const snapshot = ref<MihomoConnectionsSnapshot | null>(null)
  const error = ref('')
  const notes = ref<Record<string, string>>({})
  const speeds = ref(new Map<string, { up: number; down: number }>())
  const totalSpeed = ref({ up: 0, down: 0 })
  const trend = ref<{ down: number[]; up: number[]; memory: number[] }>({ down: [], up: [], memory: [] })
  let previous: { at: number; totals: { up: number; down: number }; byId: Map<string, { up: number; down: number }> } | null = null

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
      const { data } = await mihomoPanelApi.connections(agentId(), { signal: controller.signal })
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
      const { data } = await clientToolsApi.notes(agentId())
      if (generation !== notesEpoch || disposed || !data?.success) return
      if (JSON.stringify(data.notes) !== JSON.stringify(notes.value)) notes.value = data.notes
    } catch {
      // 备注读不到时显示原始 IP
    }
  }

  watch(active, shown => {
    if (!shown) {
      previous = null
      controller?.abort()
    } else {
      void load()
      void loadNotes()
    }
    schedule()
  })
  watch(reloadToken, () => { if (active.value) void load() })
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

  return { snapshot, error, notes, speeds, totalSpeed, trend }
}
