import { onUnmounted, reactive } from 'vue'
import { agentApi } from '@/api'
import { notify } from '@/lib/feedback'

export interface AgentDeployment {
  deployment_id: string
  status: string
  message?: string
  error?: string
  rollback_error?: string
  config_version?: string
  query_error?: boolean
  pending?: boolean
  not_recorded?: boolean
  retrying?: boolean
}

export const deploymentLabels: Record<string, string> = {
  preparing: '准备文件', uploading: '上传中', receiving: '接收中',
  verifying: '校验中', ready: '已校验，待激活', stopping: '停止服务',
  backing_up: '备份中', replacing: '替换中', starting: '启动服务',
  checking: '启动检查', rolling_back: '回滚中', succeeded: '上次配置推送：成功',
  recovery_pending: '旧文件已恢复，等待服务检查',
  failed: '发布失败', rolled_back: '发布失败，已回滚',
  rollback_failed: '回滚失败，需要处理', unknown: '结果待确认'
}

const terminal = new Set(['succeeded', 'failed', 'rolled_back', 'rollback_failed'])

export function createDeploymentId() {
  // LAN HTTP installations do not expose crypto.randomUUID (secure-context only).
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 15) | 64
  bytes[8] = (bytes[8] & 63) | 128
  const hex = Array.from(bytes, value => value.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

export function useAgentDeployments(onSuccess: () => void) {
  const deployments = reactive<Record<string, AgentDeployment>>({})
  const timers = new Map<string, ReturnType<typeof setTimeout>>()
  const inFlight = new Set<string>()
  const announced = new Set<string>()
  let disposed = false

  function busy(agentId: string) {
    const task = deployments[agentId]
    return !!task && !terminal.has(task.status) && task.status !== 'ready'
  }

  function schedule(agentId: string, delay = 2000) {
    clearTimeout(timers.get(agentId))
    if (!disposed) timers.set(agentId, setTimeout(() => { void refresh(agentId) }, delay))
  }

  function track(agentId: string, task: AgentDeployment, announce = true) {
    if (disposed || !task.deployment_id) return
    const previous = deployments[agentId]
    if (previous?.deployment_id === task.deployment_id && terminal.has(previous.status) && task.status !== previous.status) {
      // A failed rollback can recover after an Agent restart; completed releases cannot regress.
      const recovering = previous.status === 'rollback_failed' && ['recovery_pending', 'rolling_back', 'rolled_back'].includes(task.status)
      if (!recovering) return
    }
    deployments[agentId] = { ...task, query_error: !!task.pending }
    clearTimeout(timers.get(agentId))
    if (task.pending) {
      schedule(agentId, 5000)
      return
    }
    if (terminal.has(task.status)) {
      const key = `${agentId}:${task.deployment_id}:${task.status}`
      if (announce && !announced.has(key)) {
        if (task.status === 'succeeded') {
          notify.success('配置发布成功，服务检查已通过')
          onSuccess()
        } else {
          notify.error(deploymentLabels[task.status], task.error || task.message)
        }
      }
      announced.add(key)
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
        track(agentId, data)
      }
    } catch (error: any) {
      if (!disposed && deployments[agentId] === task) {
        deployments[agentId] = { ...task, query_error: true, not_recorded: error.response?.status === 404 }
        schedule(agentId, 5000)
      }
    } finally {
      inFlight.delete(agentId)
    }
  }

  async function retryPublish(agentId: string) {
    const task = deployments[agentId]
    if (!task?.not_recorded || task.retrying || disposed || terminal.has(task.status)) return
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
        deployments[agentId] = { ...current, retrying: false, status: 'unknown', query_error: true }
        schedule(agentId, 5000)
        if (error.response) notify.error('重新提交未完成', error.response.data?.message)
      }
    } finally {
      const current = deployments[agentId]
      if (current?.deployment_id === task.deployment_id && current.retrying) current.retrying = false
    }
  }

  async function activate(agentId: string) {
    const task = deployments[agentId]
    if (!task || task.status !== 'ready') return
    deployments[agentId] = { ...task, status: 'verifying' }
    try {
      const { data } = await agentApi.activateDeployment(agentId, task.deployment_id)
      track(agentId, data)
    } catch (error: any) {
      if (error.response?.data?.deployment_id) track(agentId, error.response.data)
      else {
        deployments[agentId] = { ...task, status: 'unknown', query_error: true }
        schedule(agentId)
      }
    }
  }

  onUnmounted(() => {
    disposed = true
    timers.forEach(clearTimeout)
    timers.clear()
  })

  return { deployments, busy, track, refresh, activate, retryPublish }
}
