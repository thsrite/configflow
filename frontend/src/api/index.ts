import axios from 'axios'
import { ElMessage } from 'element-plus'
import router from '@/router'

const api = axios.create({
  baseURL: '/api',
  timeout: 30000
})

// 请求拦截器 - 添加 token
api.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

// 响应拦截器 - 处理认证错误
api.interceptors.response.use(
  (response) => {
    return response
  },
  (error) => {
    if (error.response?.status === 401) {
      // token 无效或过期
      localStorage.removeItem('token')
      localStorage.removeItem('username')
      ElMessage.error('登录已过期，请重新登录')
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
  fetch: (id: string, preview: boolean = false) => api.post(`/subscriptions/${id}/fetch`, { preview })
}

// 节点相关
export const nodeApi = {
  getAll: () => api.get('/nodes'),
  create: (data: any) => api.post('/nodes', data),
  update: (id: string, data: any) => api.put(`/nodes/${id}`, data),
  delete: (id: string) => api.delete(`/nodes/${id}`)
}

// 规则相关
export const ruleApi = {
  getAll: () => api.get('/rules'),
  create: (data: any) => api.post('/rules', data),
  update: (id: string, data: any) => api.put(`/rules/${id}`, data),
  delete: (id: string) => api.delete(`/rules/${id}`),
  batchCreate: (data: any) => api.post('/rules/batch', data)
}

// 规则集相关
export const ruleSetApi = {
  getAll: () => api.get('/rule-sets'),
  create: (data: any) => api.post('/rule-sets', data),
  update: (id: string, data: any) => api.put(`/rule-sets/${id}`, data),
  delete: (id: string) => api.delete(`/rule-sets/${id}`)
}

// 策略组相关
export const proxyGroupApi = {
  getAll: () => api.get('/proxy-groups'),
  create: (data: any) => api.post('/proxy-groups', data),
  update: (id: string, data: any) => api.put(`/proxy-groups/${id}`, data),
  delete: (id: string) => api.delete(`/proxy-groups/${id}`)
}

// 获取服务域名配置（优先使用配置的域名，否则使用当前页面的 base URL）
const getBaseUrl = () => {
  return localStorage.getItem('serverDomain') || window.location.origin
}

// 配置生成
export const generateApi = {
  mihomo: () => api.post('/generate/mihomo', { base_url: getBaseUrl() }, { responseType: 'blob' }),
  surge: () => api.post('/generate/surge', { base_url: getBaseUrl() }, { responseType: 'blob' }),
  mosdns: () => api.post('/generate/mosdns', { base_url: getBaseUrl() }, { responseType: 'blob' }),
  previewMihomo: () => api.post('/generate/mihomo/preview', { base_url: getBaseUrl() }),
  previewSurge: () => api.post('/generate/surge/preview', { base_url: getBaseUrl() }),
  previewMosdns: () => api.post('/generate/mosdns/preview', { base_url: getBaseUrl() })
}

// 配置导入导出
export const configApi = {
  export: () => api.get('/config/export', { responseType: 'blob' }),
  exportDesensitized: () => api.get('/config/export?desensitize=true', { responseType: 'blob' }),
  import: (data: any) => api.post('/config/import', data)
}

// 自定义配置
export const customConfigApi = {
  getMihomo: () => api.get('/custom-config/mihomo'),
  saveMihomo: (data: any) => api.post('/custom-config/mihomo', data),
  getSurge: () => api.get('/custom-config/surge'),
  saveSurge: (data: any) => api.post('/custom-config/surge', data),
  getMosdns: () => api.get('/custom-config/mosdns'),
  saveMosdns: (data: any) => api.post('/custom-config/mosdns', data)
}

// Agent 管理
export const agentApi = {
  getAll: () => api.get('/agents'),
  create: (data: any) => api.post('/agents', data),
  update: (id: string, data: any = {}) => api.post(`/agents/${id}/update`, data),
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
  pushConfig: (id: string) => api.post(`/agents/${id}/push-config`, { base_url: getBaseUrl() }),
  uninstall: (id: string) => api.post(`/agents/${id}/uninstall`),
  generateScript: (params: { name: string; type: string; port?: number; host?: string }) =>
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

// Sub-Store URL 管理
export const subStoreUrlApi = {
  get: () => api.get('/settings/sub-store-url'),
  update: (data: { sub_store_url: string }) => api.post('/settings/sub-store-url', data)
}

// 统计数据
export const statsApi = {
  getOverview: () => api.get('/stats/overview')
}

export default api
