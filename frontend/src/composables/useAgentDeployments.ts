import { onUnmounted, reactive } from 'vue'
import { agentApi } from '@/api'
import { notify } from '@/lib/feedback'
import { deploymentErrorSummary } from '@/lib/agentFailure'
import type { AgentOperationEvent } from '@/types/agentOperation'

export interface AgentDNSCheck {
  name: string
  level: 'pass' | 'warn' | 'error'
  message: string
  fix?: string
}

export interface AgentDeployment {
  deployment_id: string
  status: string
  message?: string
  events?: AgentOperationEvent[]
  request_pending?: boolean
  error?: string
  error_details?: string
  failed_stage?: string
  rollback_error?: string
  rollback_error_details?: string
  config_version?: string
  config_revision?: number
  profile_id?: string
  service_type?: string
  created_at?: string
  updated_at?: string
  manual_rollback?: boolean
  rollback_rejected?: boolean
  rollback_message?: string
  query_error?: boolean
  pending?: boolean
  not_recorded?: boolean
  retrying?: boolean
  dns_checks?: AgentDNSCheck[]
  dns_checked_at?: string
  /** Set by the server when this Mihomo publication's own snapshot turns IPv6 on. */
  ipv6_verification_requested?: boolean
  ha_publication?: import('@/types/agentHAPublication').AgentHAPublication
}

export const deploymentLabels: Record<string, string> = {
  applying: '主备发布中', waiting: '等待前一台完成',
  preparing: '准备文件', uploading: '上传中', receiving: '接收中',
  verifying: '校验中', ready: '已校验，待激活', stopping: '停止服务',
  backing_up: '备份中', replacing: '替换中', reloading: '热重载', starting: '启动服务',
  checking: '启动检查', rolling_back: '回滚中', succeeded: '上次配置推送：成功',
  recovery_pending: '旧文件已恢复，等待服务检查',
  failed: '发布失败', rolled_back: '发布失败，已回滚',
  rollback_failed: '回滚失败，需要处理', unknown: '结果待确认'
}

const terminal = new Set(['succeeded', 'failed', 'rolled_back', 'rollback_failed'])

export function deploymentStatusLabel(task: AgentDeployment) {
  if (task.rollback_rejected) return '手动回滚未执行'
  if (task.manual_rollback) {
    if (task.status === 'rolled_back') return '手动回滚成功'
    if (task.status === 'rolling_back') return '正在恢复发布前配置'
    if (task.status === 'unknown') return '手动回滚结果待确认'
  }
  if (task.ha_publication?.phase === 'handoff') {
    if (['applying', 'checking'].includes(task.status)) return '正在迁移虚拟 IP'
    if (task.status === 'unknown') return '虚拟 IP 迁移待确认'
  }
  return deploymentLabels[task.status] || task.status
}

export const deploymentNeedsNotice = (task: AgentDeployment) =>
  !['succeeded', 'rolled_back'].includes(task.status) || (task.status === 'rolled_back' && !task.manual_rollback)
  || !!(task.query_error || task.pending || task.not_recorded || task.retrying || task.rollback_rejected
    || task.error || task.error_details || task.rollback_error || task.rollback_error_details || task.rollback_message)

export function createDeploymentId() {
  // LAN HTTP installations do not expose crypto.randomUUID (secure-context only).
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 15) | 64
  bytes[8] = (bytes[8] & 63) | 128
  const hex = Array.from(bytes, value => value.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

export function useAgentDeployments(onSuccess: () => void, shouldAnnounce: (agentId: string, task: AgentDeployment) => boolean = () => true) {
  const deployments = reactive<Record<string, AgentDeployment>>({})
  const timers = new Map<string, ReturnType<typeof setTimeout>>()
  const inFlight = new Set<string>()
  const submissions = new Map<string, { id: string; observed: boolean }>()
  const actions = reactive(new Map<string, { id: string; kind: 'activate' | 'rollback'; observed: boolean }>())
  const announced = new Set<string>()
  // Deployments whose failed rollback the user asked the Agent to recover.
  const recovering = reactive(new Set<string>())
  let disposed = false

  function busy(agentId: string) {
    const task = deployments[agentId]
    return actions.has(agentId) || !!task && !terminal.has(task.status) && task.status !== 'ready'
  }

  function schedule(agentId: string, delay = 2000) {
    clearTimeout(timers.get(agentId))
    if (!disposed) timers.set(agentId, setTimeout(() => {
      timers.delete(agentId)
      void refresh(agentId)
    }, delay))
  }

  function beginSubmission(agentId: string, deploymentId: string) {
    if (disposed || submissions.has(agentId) || busy(agentId)) return false
    submissions.set(agentId, { id: deploymentId, observed: false })
    track(agentId, { deployment_id: deploymentId, status: 'preparing', request_pending: true, message: '正在提交发布请求，等待服务端报告步骤。' })
    return true
  }

  function endSubmission(agentId: string, deploymentId: string, result: AgentDeployment | 'unknown' | 'rejected') {
    const submission = submissions.get(agentId)
    if (!submission || submission.id !== deploymentId) return false
    submissions.delete(agentId)
    const current = deployments[agentId]
    if (disposed || current?.deployment_id !== deploymentId) return false
    // Polls can reach activation or completion while the original POST is still
    // pending. Its accepted snapshot (or transport error) is then older evidence.
    if (submission.observed && (typeof result === 'string' || !terminal.has(result.status))) return true
    if (result === 'rejected') {
      clearTimeout(timers.get(agentId))
      timers.delete(agentId)
      delete deployments[agentId]
      return false
    }
    track(agentId, result === 'unknown' ? { deployment_id: deploymentId, status: 'unknown' } : result)
    return true
  }

  function track(agentId: string, task: AgentDeployment, announce = true) {
    if (disposed || !task.deployment_id) return
    announce = announce && shouldAnnounce(agentId, task)
    const previous = deployments[agentId]
    if (previous?.deployment_id === task.deployment_id && previous.updated_at && task.updated_at
      && Date.parse(task.updated_at) < Date.parse(previous.updated_at) && !task.pending) return
    const action = actions.get(agentId)
    if (action?.id === task.deployment_id && !task.request_pending) {
      if ((action.kind === 'activate' && task.status === 'ready') || (action.kind === 'rollback' && !task.manual_rollback)) {
        schedule(agentId)
        return
      }
      if (!task.pending && task.status !== 'unknown') action.observed = true
    }
    if (task.rollback_rejected && !(previous?.deployment_id === task.deployment_id && previous.manual_rollback && terminal.has(previous.status))) {
      // The server has resolved an uncertain request as rejected; the original release remains active.
      deployments[agentId] = { ...task, manual_rollback: false, query_error: false }
      clearTimeout(timers.get(agentId))
      const key = `${agentId}:${task.deployment_id}:rollback_rejected:${task.rollback_message || ''}`
      if (announce && !announced.has(key)) {
        notify.error('无法手动回滚', task.rollback_message || 'Agent 拒绝了回滚请求，原配置继续运行。')
        onSuccess()
      }
      announced.add(key)
      return
    }
    if (previous?.deployment_id === task.deployment_id && previous.manual_rollback && !task.manual_rollback) {
      // A cached publication result cannot confirm a rollback request whose transport timed out.
      if (!terminal.has(previous.status)) schedule(agentId, 2000)
      return
    }
    if (previous?.deployment_id === task.deployment_id && terminal.has(previous.status) && task.status !== previous.status) {
      // A failed rollback can recover after an Agent restart; completed releases cannot regress.
      const recovering = previous.status === 'rollback_failed' && ['recovery_pending', 'rolling_back', 'rolled_back'].includes(task.status)
      const manualRollback = previous.status === 'succeeded' && task.manual_rollback && ['rolling_back', 'rolled_back', 'rollback_failed', 'recovery_pending'].includes(task.status)
      if (!recovering && !manualRollback) return
    }
    deployments[agentId] = { ...task, events: task.events ?? (previous?.deployment_id === task.deployment_id ? previous.events : undefined), query_error: !!task.pending }
    clearTimeout(timers.get(agentId))
    if (task.pending) {
      schedule(agentId, 5000)
      return
    }
    const dnsWarnings = task.manual_rollback ? [] : task.dns_checks?.filter(check => check.level === 'warn')
    if (dnsWarnings?.length && (!terminal.has(task.status) || task.status === 'succeeded')) {
      const key = `${agentId}:${task.deployment_id}:dns-warnings`
      if (announce && !announced.has(key)) {
        notify.warning(task.status === 'succeeded' ? '配置已发布，DNS 检查有提醒' : 'DNS 检查有提醒', deploymentErrorSummary({ message: dnsWarnings[0].message }))
      }
      announced.add(key)
    }
    if (terminal.has(task.status)) {
      const key = `${agentId}:${task.deployment_id}:${task.status}`
      if (announce && !announced.has(key)) {
        if (recovering.has(task.deployment_id) && task.status === 'rolled_back') {
          notify.success('已恢复发布前配置', '服务已按原配置启动并通过检查，本次发布未生效。')
          onSuccess()
        } else if (recovering.has(task.deployment_id) && task.status === 'rollback_failed') {
          notify.error('重新恢复失败', deploymentErrorSummary(task))
        } else if (task.status === 'succeeded' || (task.manual_rollback && task.status === 'rolled_back')) {
          // Keep the DNS warning visible; completion remains in the page's status.
          if (!dnsWarnings?.length) notify.success(task.manual_rollback ? '手动回滚成功，发布前配置已恢复并通过服务检查' : '配置发布成功，服务检查已通过')
          onSuccess()
        } else {
          notify.error(deploymentLabels[task.status], deploymentErrorSummary(task))
        }
      }
      announced.add(key)
      recovering.delete(task.deployment_id)
    } else if (task.status !== 'ready') {
      // An older list response must not replace an in-progress status request.
      schedule(agentId, previous?.deployment_id === task.deployment_id ? 2000 : 0)
    }
  }

  async function refresh(agentId: string) {
    const task = deployments[agentId]
    if (!task || disposed || inFlight.has(agentId)) return
    inFlight.add(agentId)
    try {
      const { data } = await agentApi.getDeployment(agentId, task.deployment_id)
      if (!disposed && deployments[agentId] === task) {
        const submission = submissions.get(agentId)
        if (submission?.id === task.deployment_id) submission.observed = true
        track(agentId, data)
      }
    } catch (error: any) {
      if (!disposed && deployments[agentId] === task) {
        if (error.response?.status === 404 && submissions.get(agentId)?.id === task.deployment_id) {
          // The POST may not have registered its ID yet. It is still in flight,
          // so this is not evidence that the user should submit it again.
          schedule(agentId)
          return
        }
        deployments[agentId] = { ...task, query_error: true, not_recorded: error.response?.status === 404 }
        schedule(agentId, 5000)
      }
    } finally {
      inFlight.delete(agentId)
      const current = deployments[agentId]
      if (!disposed && current && !terminal.has(current.status) && current.status !== 'ready' && !timers.has(agentId)) schedule(agentId)
    }
  }

  async function retryPublish(agentId: string) {
    const task = deployments[agentId]
    if (!task?.not_recorded || task.manual_rollback || task.retrying || disposed || terminal.has(task.status)) return
    clearTimeout(timers.get(agentId))
    deployments[agentId] = { ...task, retrying: true, not_recorded: false, query_error: false }
    try {
      // Reuse the original identity: a delayed first request and this retry are one publication.
      const { data } = await agentApi.pushConfig(agentId, task.deployment_id)
      if (!disposed && deployments[agentId]?.deployment_id === task.deployment_id) track(agentId, data)
    } catch (error: any) {
      const current = deployments[agentId]
      if (disposed || current?.deployment_id !== task.deployment_id || terminal.has(current.status)) return
      if (error.response?.data?.deployment_id) track(agentId, error.response.data)
      else {
        const response = error.response?.data
        deployments[agentId] = {
          ...current, retrying: false, status: 'unknown', query_error: true,
          message: response?.error || response?.message || current.message,
          error_details: response?.error_details || current.error_details,
        }
        schedule(agentId, 5000)
        if (error.response) notify.error('重新提交未完成', deploymentErrorSummary(deployments[agentId]))
      }
    } finally {
      const current = deployments[agentId]
      if (current?.deployment_id === task.deployment_id && current.retrying) current.retrying = false
    }
  }

  async function activate(agentId: string) {
    const task = deployments[agentId]
    if (!task || task.status !== 'ready' || actions.has(agentId)) return
    actions.set(agentId, { id: task.deployment_id, kind: 'activate', observed: false })
    track(agentId, { ...task, status: 'verifying', request_pending: true, message: '正在提交激活请求，等待 Agent 报告执行步骤。' }, false)
    schedule(agentId, 0)
    try {
      const { data } = await agentApi.activateDeployment(agentId, task.deployment_id)
      if (disposed || deployments[agentId]?.deployment_id !== task.deployment_id) return
      if (!actions.get(agentId)?.observed) {
        actions.delete(agentId)
        track(agentId, data)
      }
    } catch (error: any) {
      if (disposed || deployments[agentId]?.deployment_id !== task.deployment_id || actions.get(agentId)?.observed) return
      actions.delete(agentId)
      if (error.response?.data?.deployment_id) track(agentId, error.response.data)
      else {
        deployments[agentId] = { ...task, status: 'unknown', query_error: true, message: error.response?.data?.message, error_details: error.response?.data?.error_details }
        schedule(agentId)
      }
    } finally { actions.delete(agentId) }
  }

  async function rollback(agentId: string, deploymentId: string) {
    const task = deployments[agentId]
    if (!task || task.deployment_id !== deploymentId || task.status !== 'succeeded' || disposed || actions.has(agentId)) return
    clearTimeout(timers.get(agentId))
    const rejectedKeyPrefix = `${agentId}:${deploymentId}:rollback_rejected:`
    for (const key of announced) if (key.startsWith(rejectedKeyPrefix)) announced.delete(key)
    actions.set(agentId, { id: deploymentId, kind: 'rollback', observed: false })
    track(agentId, { ...task, manual_rollback: true, rollback_rejected: false, rollback_message: undefined, rollback_error_details: undefined, status: 'rolling_back', query_error: false, request_pending: true, message: '正在提交回滚请求，等待 Agent 报告执行步骤。' }, false)
    schedule(agentId, 0)
    try {
      const { data } = await agentApi.rollbackDeployment(agentId, deploymentId)
      if (!disposed && deployments[agentId]?.deployment_id === deploymentId && !actions.get(agentId)?.observed) {
        actions.delete(agentId)
        track(agentId, { ...data, manual_rollback: true })
      }
    } catch (error: any) {
      if (disposed || deployments[agentId]?.deployment_id !== deploymentId || actions.get(agentId)?.observed) return
      actions.delete(agentId)
      if (error.response?.data?.deployment_id && error.response?.data?.manual_rollback) {
        track(agentId, error.response.data)
      } else if (error.response && error.response.status >= 400 && error.response.status < 500) {
        const reason = error.response.data?.message || '当前部署没有可用备份，或另一个操作正在进行。'
        deployments[agentId] = { ...task, rollback_rejected: true, rollback_message: reason, rollback_error_details: error.response.data?.rollback_error_details || error.response.data?.error_details }
        notify.error('无法手动回滚', deploymentErrorSummary({ error: reason }))
      } else {
        deployments[agentId] = { ...task, manual_rollback: true, rollback_rejected: false, rollback_message: undefined, rollback_error_details: error.response?.data?.rollback_error_details || error.response?.data?.error_details, status: 'unknown', query_error: true }
        schedule(agentId, 0)
      }
    } finally { actions.delete(agentId) }
  }

  /** Ask the Agent to recover a deployment whose rollback failed (same as -recover-and-exit). */
  async function recover(agentId: string) {
    const task = deployments[agentId]
    if (!task || task.status !== 'rollback_failed' || disposed || actions.has(agentId) || recovering.has(task.deployment_id)) return
    clearTimeout(timers.get(agentId))
    const id = task.deployment_id
    for (const status of ['rolled_back', 'rollback_failed']) announced.delete(`${agentId}:${id}:${status}`)
    recovering.add(id)
    try {
      const { data } = await agentApi.recoverDeployment(agentId, id)
      if (!disposed && deployments[agentId]?.deployment_id === id) track(agentId, data)
    } catch (error: any) {
      recovering.delete(id)
      if (disposed || deployments[agentId]?.deployment_id !== id) return
      const data = error.response?.data
      if (data?.deployment_id) deployments[agentId] = { ...task, ...data }
      announced.add(`${agentId}:${id}:rollback_failed`)
      notify.error('无法重新恢复', deploymentErrorSummary({ error: data?.message || '请求没有送达 Agent，请稍后重试。' }))
    }
  }

  onUnmounted(() => {
    disposed = true
    timers.forEach(clearTimeout)
    timers.clear()
    submissions.clear()
    actions.clear()
  })

  return { deployments, busy, track, refresh, beginSubmission, endSubmission, activate, retryPublish, rollback, recover, recovering }
}
