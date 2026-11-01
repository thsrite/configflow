import api from '@/api'

export interface NotificationItem {
  id: string
  state: 'pending' | 'resolved'
  read: boolean
  version: string
  title: string
  message: string
  impact?: string
  status_label: string
  severity: 'error' | 'warning' | 'neutral'
  source: string
  scope: 'profile' | 'shared' | 'system'
  profile_id?: string
  profile_name?: string
  checked_at?: string
  last_seen_at: string
  resolved_at?: string
  resolution?: string
  stale?: boolean
  occurrences: number
  failure_count?: number
  target: { path: string; query: Record<string, string>; label: string }
}

export const notificationsApi = {
  list: (signal?: AbortSignal) => api.get<{ success: boolean; items: NotificationItem[] }>('/notifications', { signal }),
  read: (versions: Record<string, string>) => api.post('/notifications/read', { versions }),
}
