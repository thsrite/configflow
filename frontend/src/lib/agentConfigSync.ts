interface DeploymentRecord {
  status: string
  profile_id?: string
  config_revision?: number
  config_version?: string
  updated_at?: string
}

interface AgentConfigRecord {
  profile_id?: string
  config_revision?: number
  config_version?: string
  latest_deployment?: DeploymentRecord
  deployments?: Record<string, DeploymentRecord>
}

interface DeploymentProgress {
  status: string
  pending?: boolean
  query_error?: boolean
}

const validRevision = (value: unknown): value is number =>
  typeof value === 'number' && Number.isSafeInteger(value) && value >= 0

/** Describe the last confirmed configuration without exposing long revisions on the card. */
export function getAgentConfigSync(agent: AgentConfigRecord, currentRevision: number | undefined, progress?: DeploymentProgress) {
  const revision = validRevision(agent.config_revision) ? agent.config_revision : undefined
  const current = validRevision(currentRevision) ? currentRevision : undefined
  const hash = agent.config_version && agent.config_version !== '0' ? agent.config_version : undefined
  const title = [
    `已推送修订号：${revision === undefined ? '未记录' : `v${revision}`}`,
    `当前配置修订号：${current === undefined ? '暂未获取' : `v${current}`}`,
    `配置哈希：${hash || '未记录'}`
  ].join('\n')
  const state = (label: string, tone: 'success' | 'warning' | 'muted' = 'muted', detail?: string) =>
    ({ label, tone, title: detail ? `${title}\n${detail}` : title })
  const unknown = () => state('同步状态待确认')
  const task = progress || agent.latest_deployment
  if (progress?.pending || progress?.query_error || task?.status === 'unknown' || task?.status === 'rollback_failed') return unknown()
  if (task?.status === 'ready') return state('已校验，待应用', 'warning')
  if (task && !['succeeded', 'failed', 'rolled_back'].includes(task.status)) return state('正在推送')

  const successful = [...Object.values(agent.deployments || {}), ...(agent.latest_deployment ? [agent.latest_deployment] : [])]
    .filter(record => record.status === 'succeeded')
  if (!hash && revision === undefined && !successful.length) return state('尚未推送')
  if (!hash || revision === undefined || current === undefined) return unknown()

  // A rebind retains the previous revision/hash. Check the latest successful
  // deployment; conflicting current data must not fall back to an older match.
  const latest = agent.latest_deployment
  const applied = latest?.status === 'succeeded'
    ? latest
    : successful.sort((a, b) => (Date.parse(b.updated_at || '') || 0) - (Date.parse(a.updated_at || '') || 0))[0]
  if (!applied?.profile_id || applied.config_revision !== revision || applied.config_version !== hash) return unknown()
  if (applied.profile_id !== (agent.profile_id || 'default')) {
    return state('绑定已变更，待推送', 'warning', `上次成功推送的配置空间：${applied.profile_id}`)
  }
  if (revision > current) return unknown()
  return revision < current ? state('有变更，待推送', 'warning') : state('已同步', 'success')
}
