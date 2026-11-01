import api from '@/api'

export type PushDiffChange = 'added' | 'removed' | 'modified'
export type PushDiffRole = 'config' | 'provider' | 'rule' | 'custom'

/** 敏感片段（密码、令牌、网址里的账号密码等），打开「隐藏敏感信息」后显示为「已隐藏」 */
export interface PushDiffSecret { secret: string }
/** 显示文字：字符串里没有敏感内容；数组按顺序拼接，其中的 { secret } 是敏感片段 */
export type PushDiffText = string | Array<string | PushDiffSecret>

/** 文件摘要：订阅按节点、规则集按规则条数、主配置按配置项，其余按大小 */
export interface PushDiffSummary {
  kind: 'nodes' | 'rules' | 'size' | 'config'
  before?: number | null
  after?: number | null
  added?: number
  removed?: number
  modified?: number
  changes?: number
}

export interface PushDiffFile {
  path: string
  role: PushDiffRole | null
  /** 订阅名、规则集名；主配置没有 */
  name?: PushDiffText
  status: PushDiffChange | 'unchanged'
  /** 缺失表示上次推送的内容没有保存，无法细分 */
  summary?: PushDiffSummary
  /** 订阅、规则集的明细（哪些节点、规则变了），随接口一起返回；没有时不提供展开 */
  details?: PushDiffFileDetails
}

/** 一组明细：新增、删除或修改；接口只带每组前 20 条，omitted 为没有随接口返回的条数 */
export interface PushDiffDetailGroup {
  change: PushDiffChange
  total: number
  entries: PushDiffListEntry[]
  omitted: number
  /** 可分页读取的条数（含已返回的）；小于 total 时其余超出明细上限，没有保留 */
  available?: number
}

/** 订阅按节点名对应，修改的节点带逐个字段的原值、新值；规则集按规则行列出新增和删除 */
export interface PushDiffFileDetails {
  kind: 'nodes' | 'rules'
  groups: PushDiffDetailGroup[]
  /** 这次响应的明细总量已达上限，之后的条目只计数 */
  limited?: boolean
  /** 被多个节点共用的节点名个数：内容相同的视为未变，其余按出现顺序对应 */
  repeated?: number
  /** 服务端保留了全部条目，可以用 entries 接口加载更多和搜索 */
  paging?: boolean
}

/** 列表类配置项只给条数和增减；条目已在服务端截断，敏感片段已标出 */
export interface PushDiffList {
  before: number
  after: number
  added: number
  removed: number
  modified: number
  reordered: boolean
  entries?: PushDiffListEntry[]
  omitted?: number
  /** 按 name / tag 对应的列表（节点、策略组、插件） */
  identity?: string
  /** 条目内部的列表字段，只带少量增减条目 */
  added_items?: PushDiffText[]
  removed_items?: PushDiffText[]
}

export interface PushDiffListEntry {
  change: PushDiffChange
  text: PushDiffText
  /** 订阅里新增的节点：类型与服务器:端口 */
  info?: PushDiffText
  /** 同名节点中的第几个（只在节点名重复时提供） */
  nth?: number
  fields?: PushDiffItem[]
  fields_omitted?: number
}

/** 主配置的一个配置项；sensitive 表示整个值都是凭据（键名为 password、secret 等） */
export interface PushDiffItem {
  key: string
  change: PushDiffChange
  sensitive: boolean
  old?: PushDiffText | null
  new?: PushDiffText | null
  list?: PushDiffList
}

export interface PushDiffRaw {
  /** unified diff 的每一行；null 表示配置过大，没有生成 */
  lines: PushDiffText[] | null
  truncated?: boolean
  omitted_lines?: number
  reason?: 'too_large'
  total_lines?: number
}

export interface PushDiffConfig {
  format: 'yaml'
  /** compared：有配置项变化；formatting：只有顺序或格式不同；unparsable / unavailable：只能按文件比较 */
  status: 'compared' | 'formatting' | 'unparsable' | 'unavailable'
  reason?: string
  items: PushDiffItem[]
  omitted: number
  total?: number
  raw?: PushDiffRaw
}

export interface PushDiffResponse {
  /** checking 时继续轮询；never-pushed / unknown 时看 reason */
  status: 'checking' | 'synced' | 'changed' | 'never-pushed' | 'unknown'
  reason?: string
  checked_at?: string
  service_type?: string
  baseline?: { pushed_at?: string | null; config_revision?: number | null }
  /** full：有上次推送的发布包；digests：只能按文件摘要比较 */
  content?: 'full' | 'digests'
  content_reason?: string | null
  counts?: Record<PushDiffChange | 'unchanged', number>
  files?: PushDiffFile[] | null
  files_omitted?: number
  unchanged?: PushDiffFile[]
  unchanged_omitted?: number
  config?: PushDiffConfig | null
  /** 这次对比结果的版本；分页时带上，结果更新后旧版本的分页请求返回 stale */
  version?: string
}

export interface PushDiffEntriesQuery {
  version: string
  /** 文件路径（PushDiffFile.path） */
  file: string
  change: PushDiffChange
  offset: number
  limit?: number
  /** 搜索词，不区分大小写；敏感值不参与匹配 */
  q?: string
}

/** 一页明细；stale 表示对比结果已过期、配置已变或已有更新的结果，需要重新加载 */
export interface PushDiffEntriesPage {
  status: 'ok' | 'stale'
  reason?: 'expired' | 'changed' | 'replaced'
  /** 这一组可读取的条数；有搜索词时为匹配的条数 */
  total?: number
  offset?: number
  entries?: PushDiffListEntry[]
  next_offset?: number | null
}

export const agentPushDiffApi = {
  get: (agentId: string, signal?: AbortSignal) =>
    api.get<PushDiffResponse>(`/agents/${encodeURIComponent(agentId)}/push-diff`, { signal }),
  entries: (agentId: string, params: PushDiffEntriesQuery, signal?: AbortSignal) =>
    api.get<PushDiffEntriesPage>(`/agents/${encodeURIComponent(agentId)}/push-diff/entries`, { params, signal }),
}
