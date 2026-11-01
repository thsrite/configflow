export type MonitorNumber = number | null

export interface OxiDNSCache {
  tag: string
  lookups: MonitorNumber
  hits: MonitorNumber
  fresh_hits: MonitorNumber
  stale_hits: MonitorNumber
  misses: MonitorNumber
  expired: MonitorNumber
  entries: MonitorNumber
  hit_rate: MonitorNumber
}

export interface OxiDNSRanking {
  sample_size: number
  rows: Array<{ key: string; count: number; share: number }>
  at_limit: boolean
}
export interface OxiDNSRankings {
  success: true
  recorder: string
  collected_at_ms: number
  source_revision: string
  instance_id: string
  retention_days: MonitorNumber
  sample_limit: number
  clients: OxiDNSRanking
  domains: OxiDNSRanking
  blocked: OxiDNSRanking & { available: boolean; reason: string; matcher_tag?: string }
}
export interface OxiDNSOverview {
  success: true
  agent: { id: string; name: string; host: string; profile_id?: string; version: string; status: string }
  collected_at: string
  core: { version: string | null; status: string; uptime_ms: MonitorNumber; instance_id: string | null }
  recorders: string[]
  statistics?: {
    generation: string
    started_at_ms: number
    reason: 'initial' | 'manual' | 'core_changed' | 'configuration_changed' | 'target_changed' | 'counters_changed' | 'publication'
    partial: boolean
    publication_count?: number
  } | null
  rankings?: OxiDNSRankings | { available: false; reason: string }
  upstreams?: Array<{
    id?: string; tag: string; upstream: string; address?: string
    requests: MonitorNumber; failed: MonitorNumber; avg_latency_ms: MonitorNumber
  }>
  metrics: {
    requests_total: MonitorNumber
    failed_total: MonitorNumber
    avg_latency_ms: MonitorNumber
    inflight: MonitorNumber
    cache_hit_rate: MonitorNumber
  }
  caches: OxiDNSCache[]
  servers: Array<{
    tag: string; protocol: string; requests: MonitorNumber; failed: MonitorNumber
    avg_latency_ms: MonitorNumber; inflight: MonitorNumber
  }>
}

export interface OxiDNSQuestion { name: string; qtype: string | number; qclass: string | number }
export interface OxiDNSAnswer { name: string; rr_type: string | number; ttl: number; payload_text: string }
export type OxiDNSQuerySource = 'local' | 'remote' | 'fallback' | 'pinned' | 'cache' | 'hosts' | 'blocked' | 'rejected' | 'unknown' | 'unrecorded' | 'error' | 'no_response' | 'ipv4_preferred'
export interface OxiDNSQuery {
  id: number | string
  created_at_ms: number
  elapsed_ms: MonitorNumber
  client_ip: string
  questions_json: OxiDNSQuestion[]
  error: string | null
  has_response: boolean
  rcode: string | number | null
  answer_count: number
  answers_json: OxiDNSAnswer[]
  resolution_source?: OxiDNSQuerySource
  steps?: Array<{ event_index: number; sequence_tag: string; node_index: number | null; kind: string; tag: string | null; outcome: string }>
}
export interface OxiDNSQueries {
  success: true
  recorder: string
  records: OxiDNSQuery[]
  next_cursor: string | null
}
export interface OxiDNSClients {
  success: true
  recorder: string
  clients: Array<{ ip: string; count: number }>
  sample_size: number
  sample_limited: boolean
}
export interface OxiDNSQueryFilters {
  qname: string
  client_ip: string
  qtype: string
  rcode: string
  status: 'all' | 'error' | 'has_response' | 'no_response'
  range: '1h' | '24h' | 'all'
  matcher_tag: string
}
export const emptyQueryFilters = (): OxiDNSQueryFilters => ({
  qname: '', client_ip: '', qtype: '', rcode: '', status: 'all', range: '1h', matcher_tag: ''
})
export const monitorNumber = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value)
export const formatMonitorNumber = (value: unknown, precision = 0) =>
  monitorNumber(value) ? value.toLocaleString(undefined, { maximumFractionDigits: precision }) : '—'
const dnsTypes: Record<string, string> = { '1': 'A', '2': 'NS', '5': 'CNAME', '6': 'SOA', '12': 'PTR', '15': 'MX', '16': 'TXT', '28': 'AAAA', '33': 'SRV', '64': 'SVCB', '65': 'HTTPS', '255': 'ANY' }
const dnsCodes: Record<string, string> = {
  '0': 'NOERROR', '1': 'FORMERR', '2': 'SERVFAIL', '3': 'NXDOMAIN', '4': 'NOTIMP', '5': 'REFUSED',
  '6': 'YXDOMAIN', '7': 'YXRRSET', '8': 'NXRRSET', '9': 'NOTAUTH', '10': 'NOTZONE',
  '16': 'BADVERS', '17': 'BADKEY', '18': 'BADTIME', '19': 'BADMODE', '20': 'BADNAME',
  '21': 'BADALG', '22': 'BADTRUNC', '23': 'BADCOOKIE'
}
// OxiDNS v1.6.1 serializes ResponseCode through its Display implementation.
const dnsDisplayCodes: Record<string, string> = {
  'no error': 'NOERROR', 'form error': 'FORMERR', 'server failure': 'SERVFAIL',
  'non-existent domain': 'NXDOMAIN', 'not implemented': 'NOTIMP', 'query refused': 'REFUSED',
  'name should not exist': 'YXDOMAIN', 'rr set should not exist': 'YXRRSET',
  'rr set does not exist': 'NXRRSET', 'not authorized': 'NOTAUTH', 'name not in zone': 'NOTZONE',
  'bad opt version': 'BADVERS', 'tsig signature failure': 'BADSIG', 'key not recognized': 'BADKEY',
  'signature out of time window': 'BADTIME', 'bad tkey mode': 'BADMODE', 'duplicate key name': 'BADNAME',
  'algorithm not supported': 'BADALG', 'bad truncation': 'BADTRUNC', 'bad server cookie': 'BADCOOKIE'
}
const dnsCodeLabels = new Map<string, string>([
  ...Object.entries(dnsCodes), ...Object.entries(dnsDisplayCodes),
  ...Object.values(dnsDisplayCodes).map(value => [value.toLowerCase(), value] as [string, string])
])
export const formatDNSType = (value: string | number) => dnsTypes[String(value)] || String(value)
export const formatDNSCode = (value: string | number | null) => {
  if (value === null) return '—'
  const original = String(value)
  const normalized = original.trim().toLowerCase()
  return dnsCodeLabels.get(normalized) || original
}
// ConfigFlow Agents verify DNS after each publish by resolving their default
// health_dns_name over loopback (preflight UDP/TCP, then consecutive health probes).
export const AGENT_PROBE_NAME = 'example.com'
const loopbackClient = (ip: string) => /^(?:::ffff:)?127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/i.test(ip) || ip === '::1'
export const isAgentProbe = (record: Pick<OxiDNSQuery, 'client_ip' | 'questions_json'>) =>
  loopbackClient(record.client_ip || '') && Array.isArray(record.questions_json) && record.questions_json.length > 0
  && record.questions_json.every(question => typeof question?.name === 'string' && question.name.toLowerCase().replace(/\.$/, '') === AGENT_PROBE_NAME)
export const AGENT_PROBE_NOTE = 'ConfigFlow Agent 在发布配置前后用 example.com 检查本机 DNS 是否可用，并非局域网设备的查询。'
export const formatQueryTime = (value: number) => Number.isFinite(value) ? new Date(value).toLocaleString() : '—'
export const queryTimeZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone
export { querySourceLabel } from '@/lib/oxidnsQuerySource'
export interface OxiDNSDevicePolicy {
  ip: string
  name: string
  mode: 'smart' | 'direct'
}

