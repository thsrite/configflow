/** A persisted, ordered publication across an OxiDNS high-availability pair. */
export interface AgentHAPublication {
  id: string
  status: string
  phase: 'preparing' | 'standby' | 'handoff' | 'primary' | 'keepalived' | 'done'
  message?: string
  owner_agent_id: string
  owner_deployment_id: string
  active_agent_id?: string
  members: { agent_id: string; name: string; deployment_id: string; status: string }[]
  /**
   * Automatic completion after a lost member returns: `status` on a pair stopped by the
   * lost member (waiting / running / blocked), `attempt` on the publication that completes it.
   */
  auto_complete?: { status?: 'waiting' | 'running' | 'blocked' | ''; message?: string; attempt?: boolean } | null
}

export const haPublicationFinished = (status: string) =>
  ['succeeded', 'failed', 'rolled_back', 'rollback_failed'].includes(status)
