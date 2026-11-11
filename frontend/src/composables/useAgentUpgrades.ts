import { onUnmounted, reactive } from 'vue'
import { agentApi } from '@/api'
import type { AgentOperationEvent } from '@/types/agentOperation'

export interface AgentUpgrade {
  update_id: string
  target_version: string
  previous_version: string
  status: string
  message?: string
  events?: AgentOperationEvent[]
  query_error?: boolean
  request_pending?: boolean
  error?: string
  updated_at?: number
}

export const upgradeLabels: Record<string, string> = {
  submitting: '提交更新请求',
  queued: '等待更新', downloading: '下载更新', verifying: '校验更新',
  backing_up: '备份旧版本', migrating: '迁移配置和恢复服务', replacing: '替换程序',
  restarting: '重启 Agent', checking: '等待新版确认', rolling_back: '恢复旧版本',
  succeeded: '更新成功', failed: '更新失败', rolled_back: '更新失败，已回滚',
  rollback_failed: '回滚失败，需要处理', unknown: '结果待确认，继续查询',
}
export function upgradeStepMessage(status: string, version?: string) {
  const agent = version ? `Agent ${version}` : 'Agent 新版本'
  const messages: Record<string, string> = {
    submitting: '正在提交更新请求，等待 Agent 确认任务。',
    queued: `Agent 已接收更新任务，等待开始安装${version ? ` ${version}` : ''}。`,
    downloading: `正在下载 ${agent} 的程序文件。`,
    verifying: `正在校验 ${agent} 的程序文件完整性与可执行性。`,
    backing_up: '正在备份当前 Agent 程序，供更新失败时恢复。',
    migrating: '正在迁移 Agent 配置并准备服务恢复。',
    replacing: `正在替换为 ${agent} 的程序文件。`,
    restarting: '正在重启 Agent；短暂失联不代表更新成功或失败。',
    checking: `等待 ${agent} 重新上线并确认运行状态。`,
    rolling_back: '正在恢复更新前的 Agent 程序与服务。',
    unknown: '更新执行结果尚未确认，正在查询 Agent；请勿重复提交。',
  }
  return messages[status] || ''
}
const terminal = new Set(['succeeded', 'failed', 'rolled_back', 'rollback_failed'])

export function useAgentUpgrades(onSuccess: () => void) {
  const upgrades = reactive<Record<string, AgentUpgrade>>({})
  const timers = new Map<string, ReturnType<typeof setTimeout>>()
  const inflight = new Set<string>()
  const submitting = reactive(new Set<string>())
  const querying = reactive(new Set<string>())
  const unconfirmed = new Map<string, { previousId?: string; startedAt: number }>()
  let disposed = false
  const busy = (id: string) => submitting.has(id) || !!upgrades[id] && (!terminal.has(upgrades[id].status) || upgrades[id].status === 'rollback_failed')

  function schedule(id: string, immediate = false) {
    if (disposed || timers.has(id) || !busy(id) || upgrades[id].status === 'rollback_failed') return
    // 结果待确认时 Agent 多半已失联，放慢轮询，避免每 1.5 秒向它发两次请求；
    // 首次载入的任务立即查一次，否则按钮要白白锁住一个轮询周期
    const delay = immediate ? 0 : upgrades[id].status === 'unknown' ? 15000 : 1500
    timers.set(id, setTimeout(() => { timers.delete(id); void refresh(id) }, delay))
  }
  function track(id: string, state: AgentUpgrade, confirmedRequest = false) {
    if (disposed) return
    const request = unconfirmed.get(id)
    if (request && !confirmedRequest) {
      // The POST creates its ID server-side. An old latest task is not evidence
      // that this request succeeded, especially while the POST is still pending.
      if (submitting.has(id) || !state.update_id || state.update_id === request.previousId) return
      if (!request.previousId && (!state.updated_at || state.updated_at < request.startedAt)) return
    }
    if (request) unconfirmed.delete(id)
    const previous = upgrades[id]
    if (previous?.update_id === state.update_id && previous.updated_at && state.updated_at && state.updated_at < previous.updated_at) return
    if (previous?.update_id === state.update_id && terminal.has(previous.status) && !terminal.has(state.status)) return
    if (previous?.update_id !== state.update_id && previous?.updated_at && state.updated_at && state.updated_at < previous.updated_at) return
    upgrades[id] = { ...state, events: state.events ?? (previous?.update_id === state.update_id ? previous.events : undefined), query_error: false }
    // 只有在这里看着它变成成功才通知；首次载入时已经成功的任务来自刚读到的列表，
    // 再通知一次只会让页面为每台设备把完整列表重读一遍
    if (state.status === 'succeeded' && previous && previous.status !== 'succeeded') onSuccess()
    schedule(id, !previous || confirmedRequest)
  }
  async function refresh(id: string) {
    if (disposed || inflight.has(id)) return
    inflight.add(id)
    try {
      const { data } = await agentApi.getUpgrade(id)
      if (!disposed) track(id, data)
    } catch {
      // Losing contact during restart is expected. Keep the durable task pending.
      if (!disposed && upgrades[id]) upgrades[id] = { ...upgrades[id], query_error: true }
    } finally {
      inflight.delete(id)
      schedule(id)
    }
  }
  /** 手动查询：不受后台轮询的 inflight 去重影响，返回最新状态，失败时抛出 */
  async function query(id: string): Promise<AgentUpgrade | undefined> {
    if (querying.has(id)) return upgrades[id]
    querying.add(id)
    const timer = timers.get(id)
    if (timer) { clearTimeout(timer); timers.delete(id) }
    try {
      const { data } = await agentApi.getUpgrade(id)
      if (!disposed) track(id, data)
      return upgrades[id]
    } catch (error) {
      if (!disposed && upgrades[id]) upgrades[id] = { ...upgrades[id], query_error: true }
      throw error
    } finally {
      querying.delete(id)
      schedule(id)
    }
  }
  async function start(id: string) {
    if (busy(id)) return
    const previous = upgrades[id]
    unconfirmed.set(id, { previousId: previous?.update_id, startedAt: Math.floor(Date.now() / 1000) })
    submitting.add(id)
    upgrades[id] = { update_id: '', target_version: '', previous_version: '', status: 'submitting', request_pending: true }
    try {
      const { data } = await agentApi.update(id)
      if (!disposed && data?.update_id && typeof data.status === 'string') track(id, data, true)
      else if (!disposed) upgrades[id] = { ...upgrades[id], status: 'unknown', request_pending: false }
    } catch (error: any) {
      if (!disposed) {
        if (error.response && error.response.status >= 400 && error.response.status < 500) {
          unconfirmed.delete(id)
          if (previous) upgrades[id] = previous
          else upgrades[id] = { ...upgrades[id], status: 'failed', request_pending: false, error: error.response.data?.message || '更新请求被拒绝。' }
        } else upgrades[id] = { ...upgrades[id], status: 'unknown', request_pending: false, query_error: true }
      }
      throw error
    } finally {
      submitting.delete(id)
      if (!disposed && upgrades[id]) schedule(id, true)
    }
  }
  async function retryUnknown(id: string, confirmRetry: () => Promise<boolean>) {
    const uncertain = upgrades[id]
    if (!uncertain || uncertain.update_id || uncertain.status !== 'unknown' || querying.has(id) || submitting.has(id)) return
    querying.add(id)
    try {
      let previous: AgentUpgrade | undefined
      try {
        const { data } = await agentApi.getUpgrade(id)
        if (disposed || upgrades[id] !== uncertain) return
        // An active task always wins over a second submission. A different ID
        // is also accepted only when it can belong to this request.
        if (!terminal.has(data.status)) { track(id, data, true); return }
        track(id, data)
        if (upgrades[id]?.update_id) return
        const request = unconfirmed.get(id)
        if (data.update_id !== request?.previousId && !(request && data.updated_at && data.updated_at < request.startedAt)) {
          throw new Error('无法确认这条更新记录是否属于本次请求，请先核对 Agent 的实际版本。')
        }
        previous = data
      } catch (error: any) {
        if (error.response?.status !== 404) throw error
      }
      if (disposed || upgrades[id] !== uncertain || !await confirmRetry()) return
      if (disposed || upgrades[id] !== uncertain) return
      clearTimeout(timers.get(id)); timers.delete(id)
      unconfirmed.delete(id)
      if (previous) upgrades[id] = previous
      else delete upgrades[id]
      await start(id)
    } finally { querying.delete(id); if (!disposed && upgrades[id]) schedule(id) }
  }
  onUnmounted(() => { disposed = true; timers.forEach(clearTimeout); timers.clear(); unconfirmed.clear() })
  return { upgrades, busy, querying, track, refresh, query, start, retryUnknown }
}
