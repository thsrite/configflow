import axios from 'axios'
import { notify } from '@/lib/feedback'
import router from '@/router'
import { getAuthSessionGeneration, getAuthToken, invalidateAuthSession } from '@/authSession'
import { beginScopedRequest, clearActiveProfileId, endScopedRequest, getActiveProfileId } from '@/profileContext'

const api = axios.create({
  baseURL: '/api',
  timeout: 30000
})

declare module 'axios' {
  interface InternalAxiosRequestConfig {
    profileRequestTracked?: boolean
    authSessionToken?: string | null
    authSessionGeneration?: number
  }
}

const scopedPath = /^\/(?:profiles\/[^/]+\/generate|proxy-groups(?:\/|$)|rules(?:\/|$)|rule-sets(?:\/|$)|rule-configs(?:\/|$)|custom-config(?:\/|$)|mosdns(?:\/|$)|stats(?:\/|$))/
const profileOptions = (profileId = getActiveProfileId()) => ({
  headers: { 'X-ConfigFlow-Profile': profileId }
})

let profileRecovery: Promise<void> | null = null

const recoverFromMissingProfile = (): Promise<void> => {
  if (!profileRecovery) {
    clearActiveProfileId()
    profileRecovery = import('@/stores/profile')
      .then(({ useProfileStore }) => useProfileStore().refreshProfiles())
      .then(() => undefined)
      .finally(() => {
        profileRecovery = null
      })
  }
  return profileRecovery
}

// 请求拦截器 - 添加 token
api.interceptors.request.use(
  (config) => {
    const token = getAuthToken()
    config.authSessionToken = token
    config.authSessionGeneration = getAuthSessionGeneration()
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    if (!config.headers.has('X-ConfigFlow-Profile')) {
      config.headers.set('X-ConfigFlow-Profile', getActiveProfileId())
    }
    if (scopedPath.test(config.url || '')) {
      config.profileRequestTracked = true
      beginScopedRequest()
    }
    return config
  },
  (error) => {
    return Promise.reject(error)
  },
  { synchronous: true }
)

// 响应拦截器 - 处理认证错误
api.interceptors.response.use(
  (response) => {
    if (response.config.profileRequestTracked) endScopedRequest()
    return response
  },
  (error) => {
    if (error.config?.profileRequestTracked) endScopedRequest()
    const message = error.response?.data?.message
    if (error.response?.status === 404 && typeof message === 'string' && message.startsWith('Profile not found:')) {
      void recoverFromMissingProfile().catch(() => undefined)
    } else if (
      error.response?.status === 401
      && error.config?.authSessionToken === getAuthToken()
      && error.config?.authSessionGeneration === getAuthSessionGeneration()
    ) {
      // token 无效或过期
      // 已退出或重新登录后的旧请求不能清除新会话。
      invalidateAuthSession()
      localStorage.removeItem('token')
      localStorage.removeItem('username')
      notify.error('登录已过期，请重新登录')
      router.push('/login')
    }
    return Promise.reject(error)
  }
)

// 订阅相关
export const subscriptionApi = {
  getAll: () => api.get('/subscriptions'),
  create: (data: any) => api.post('/subscriptions', data),
  update: (id: string, data: any) => api.put(`/subscriptions/${id}`, data),
  delete: (id: string) => api.delete(`/subscriptions/${id}`),
  fetch: (id: string, preview: boolean = false) => api.post(`/subscriptions/${id}/fetch`, { preview }),
  /** 最近 24 次拉取记录与 subscription-userinfo 流量信息 */
  health: () => api.get('/subscriptions/health')
}

// 节点相关
export const nodeApi = {
  getAll: () => api.get('/nodes'),
  create: (data: any) => api.post('/nodes', data),
  update: (id: string, data: any) => api.put(`/nodes/${id}`, data),
  delete: (id: string) => api.delete(`/nodes/${id}`),
  /** 最近一次 TCP 延迟结果（按节点名） */
  latency: () => api.get('/nodes/latency'),
  /** 发起 TCP 延迟测试；names 为空测全部候选节点 */
  testLatency: (names?: string[]) => api.post('/nodes/latency', names ? { names } : {}, { timeout: 120000 }),
  /** 用内置 Sub-Store 把 Surge / Loon 节点行（如 snell）转成结构化节点，按行返回 */
  parseLines: (lines: string[]) =>
    api.post<{ success: boolean; results: Array<{ proxy?: Record<string, any>; error?: string }> }>(
      '/nodes/parse-lines', { lines }, { timeout: 120000 }
    )
}

// 规则相关
export const ruleApi = {
  getAll: (profileId?: string) => api.get('/rules', profileOptions(profileId)),
  create: (data: unknown, profileId?: string) => api.post('/rules', data, profileOptions(profileId)),
  update: (id: string, data: unknown, profileId?: string) => api.put(`/rules/${id}`, data, profileOptions(profileId)),
  delete: (id: string, profileId?: string) => api.delete(`/rules/${id}`, profileOptions(profileId)),
  batchCreate: (data: unknown, profileId?: string) => api.post('/rules/batch', data, profileOptions(profileId)),
  findDuplicates: (profileId?: string, signal?: AbortSignal) => api.post('/rules/find-duplicates', {}, { ...profileOptions(profileId), timeout: 120000, signal })
}

// 规则集相关
export const ruleSetApi = {
  getAll: (profileId?: string) => api.get('/rule-sets', profileOptions(profileId)),
  create: (data: unknown, profileId?: string) => api.post('/rule-sets', data, profileOptions(profileId)),
  update: (id: string, data: unknown, profileId?: string) => api.put(`/rule-sets/${id}`, data, profileOptions(profileId)),
  delete: (id: string, profileId?: string) => api.delete(`/rule-sets/${id}`, profileOptions(profileId))
}

// 策略组相关
export const proxyGroupApi = {
  getAll: (profileId?: string) => api.get('/proxy-groups', profileOptions(profileId)),
  create: (data: unknown, profileId?: string) => api.post('/proxy-groups', data, profileOptions(profileId)),
  update: (id: string, data: unknown, profileId?: string) => api.put(`/proxy-groups/${id}`, data, profileOptions(profileId)),
  delete: (id: string, profileId?: string) => api.delete(`/proxy-groups/${id}`, profileOptions(profileId)),
  previewRegex: (data: unknown, profileId?: string) => api.post('/proxy-groups/preview-regex', data, profileOptions(profileId))
}

// 获取服务域名配置（优先使用配置的域名，否则使用当前页面的 base URL）
const getBaseUrl = () => {
  return localStorage.getItem('serverDomain') || window.location.origin
}

const profilePath = (profileId: string, path: string = '') =>
  `/profiles/${encodeURIComponent(profileId)}${path}`

export const profileApi = {
  list: () => api.get('/profiles'),
  get: (id: string) => api.get(profilePath(id)),
  create: (data: any) => api.post('/profiles', data),
  update: (id: string, data: any) => api.put(profilePath(id), data),
  delete: (id: string) => api.delete(profilePath(id)),
  clone: (id: string, data: any) => api.post(profilePath(id, '/clone'), data),
  export: (id: string) => api.get(profilePath(id, '/export'), { responseType: 'blob' }),
  import: (id: string, data: any) => api.post(profilePath(id, '/import'), data),
  importClient: (data: {
    client_type: string
    content: string
    name: string
    id?: string
    description?: string
    default_subscription_ids?: string[]
    dry_run?: boolean
  }) => api.post('/profiles/import-client', data),
}

// 配置生成
export const generateApi = {
  mihomo: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/mihomo'), { base_url: getBaseUrl() }, { responseType: 'blob' }),
  surge: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/surge'), { base_url: getBaseUrl() }, { responseType: 'blob' }),
  loon: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/loon'), { base_url: getBaseUrl() }, { responseType: 'blob' }),
  mosdns: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/mosdns'), { base_url: getBaseUrl() }, { responseType: 'blob', timeout: 60000 }),
  previewMihomo: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/mihomo/preview'), { base_url: getBaseUrl() }),
  previewSurge: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/surge/preview'), { base_url: getBaseUrl() }),
  previewLoon: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/loon/preview'), { base_url: getBaseUrl() }),
  previewMosdns: (profileId = getActiveProfileId()) => api.post(profilePath(profileId, '/generate/mosdns/preview'), { base_url: getBaseUrl() })
}

// 配置导入导出
export const configApi = {
  export: () => api.get('/config/export', { responseType: 'blob' }),
  exportDesensitized: () => api.get('/config/export?desensitize=true', { responseType: 'blob' }),
  import: (data: any) => api.post('/config/import', data)
}

// 自定义配置
export const customConfigApi = {
  getMihomo: (profileId?: string) => api.get('/custom-config/mihomo', profileOptions(profileId)),
  saveMihomo: (data: unknown, profileId?: string) => api.post('/custom-config/mihomo', data, profileOptions(profileId)),
  getSurge: (profileId?: string) => api.get('/custom-config/surge', profileOptions(profileId)),
  saveSurge: (data: unknown, profileId?: string) => api.post('/custom-config/surge', data, profileOptions(profileId)),
  getLoon: (profileId?: string) => api.get('/custom-config/loon', profileOptions(profileId)),
  saveLoon: (data: unknown, profileId?: string) => api.post('/custom-config/loon', data, profileOptions(profileId)),
  getMosdns: (profileId?: string) => api.get('/custom-config/mosdns', profileOptions(profileId)),
  saveMosdns: (data: unknown, profileId?: string) => api.post('/custom-config/mosdns', data, profileOptions(profileId))
}

// Agent 管理
export const agentApi = {
  getAll: () => api.get('/agents'),
  create: (data: any) => api.post('/agents', data),
  update: (id: string, data: any = {}) => api.post(`/agents/${id}/update`, data),
  getUpgrade: (id: string) => api.get(`/agents/${id}/upgrade`),
  bindProfile: (id: string, profileId: string) => api.put(`/agents/${id}`, { profile_id: profileId }),
  delete: (id: string) => api.delete(`/agents/${id}`),
  restart: (id: string) => api.post(`/agents/${id}/restart`),
  getStatus: (id: string) => api.get(`/agents/${id}/status`),
  getLogs: (id: string, lines: number = 100, logPath: string = '') => {
    const params = new URLSearchParams({ lines: lines.toString() })
    if (logPath) {
      params.append('log_path', logPath)
    }
    return api.get(`/agents/${id}/logs?${params.toString()}`)
  },
  clearLog: (id: string, logPath: string) => api.post(`/agents/${id}/logs/clear`, { log_path: logPath }),
  validateLogPath: (id: string, path: string) => api.post(`/agents/${id}/logs/validate`, { path }),
  getLoggingConfig: (id: string) => api.get(`/agents/${id}/config/logging`),
  setLoggingConfig: (id: string, enabled: boolean) => api.post(`/agents/${id}/config/logging`, { enabled }),
  pushConfig: (id: string, deploymentId?: string) => api.post(`/agents/${id}/push-config`, {
    base_url: getBaseUrl(), deployment_id: deploymentId
  }, { timeout: 120000 }),
  getDeployment: (id: string, deploymentId: string) => api.get(`/agents/${id}/deployments/${deploymentId}`),
  activateDeployment: (id: string, deploymentId: string) => api.post(`/agents/${id}/deployments/${deploymentId}/activate`),
  uninstall: (id: string) => api.post(`/agents/${id}/uninstall`),
  generateScript: (params: {
    name: string; type: string; port?: number; agent_ip?: string; config_path?: string;
    restart_command?: string; server_url?: string; service_manager?: string;
    service_unit?: string; service_binary?: string; stop_command?: string;
    start_command?: string; status_command?: string;
  }) =>
    api.get('/agents/install-script', { params, responseType: 'text' }),
  generateDockerCompose: (params: any) =>
    api.get('/agents/docker-compose', { params, responseType: 'text' }),
  generateDockerRun: (params: any) =>
    api.get('/agents/docker-run', { params, responseType: 'text' })
}

// 系统信息
export const systemApi = {
  getVersion: () => api.get('/version')
}

// 服务域名管理
export const serverDomainApi = {
  get: () => api.get('/server-domain'),
  update: (data: { new_domain: string }) => api.post('/server-domain', data)
}

// 配置令牌管理
export const configTokenApi = {
  get: () => api.get('/config-token'),
  update: (data: { token?: string, generate?: boolean }) => api.post('/config-token', data),
  delete: () => api.delete('/config-token')
}

// 第三方依赖（内置 Sub-Store 等）检测与在线更新
export interface DependencyUpdateState {
  state: 'idle' | 'running' | 'success' | 'failed'
  message: string
  target_version: string
  finished_at: number
}

export interface DependencyStatus {
  key: string
  name: string
  description: string
  homepage: string
  running: boolean
  current_version: string
  builtin_version: string
  online_updated: boolean
  runtime: string
  latest_version: string
  latest_published_at: string
  release_url: string
  release_notes: string
  check_error: string
  has_update: boolean
  updatable: boolean
  update: DependencyUpdateState
}

export const dependenciesApi = {
  list: (refresh = false) =>
    api.get<{ dependencies: DependencyStatus[] }>('/settings/dependencies', { params: refresh ? { refresh: 1 } : {} }),
  update: (key: string) =>
    api.post<{ success: boolean; message: string }>(`/settings/dependencies/${key}/update`)
}

// 规则下载 HTTP 代理（与 GitHub 镜像域名前缀独立）
export const ruleFetchProxyApi = {
  get: () => api.get<{ rule_fetch_proxy: string }>('/settings/rule-fetch-proxy'),
  update: (data: { rule_fetch_proxy: string }) =>
    api.post<{ success: boolean; rule_fetch_proxy: string }>('/settings/rule-fetch-proxy', data)
}

// 统计数据
export const statsApi = {
  getOverview: () => api.get('/stats/overview')
}

export default api
