import { isAlias, isMap, isScalar, isSeq, parseDocument, visit, type Document, type Node, Pair, Scalar, YAMLMap, YAMLSeq } from 'yaml'
import { YAML_1_1, printYamlPreserving, sameYamlValue as same } from './yamlPreserve'

/**
 * Form view of the Mihomo base YAML. The YAML stays the only source of truth:
 * each edit parses the text, changes only the targeted keys and writes back only
 * the changed lines (printYamlPreserving, shared with mihomoIPv6.ts), so comments,
 * spacing and every other key are kept.
 * Values are read and written with YAML 1.1 rules, like the server's PyYAML, so
 * a string such as `off` or `123456` is quoted instead of turning into a boolean or number.
 *
 * Checks mirror backend/common/mihomo_base_settings.py: a problem blocks saving
 * only when this edit introduced it; problems already in the saved YAML are shown as warnings.
 */

export type BasePath = readonly string[]
/** 'any' only checks that the key itself can be changed (used for removing a map). */
export type FieldKind = 'scalar' | 'list' | 'any'
export interface BaseFieldState {
  value: unknown
  present: boolean
  editable: boolean
  reason: string
}
export interface MihomoBaseView {
  error: string
  field(path: BasePath, kind?: FieldKind): BaseFieldState
  value(path: BasePath): unknown
}
export interface BaseEdit {
  path: BasePath
  /** undefined removes the key. */
  value: unknown
  kind?: FieldKind
}
export interface ReservedPort { port: number; owner: string }
export interface BaseContext { reservedPorts?: ReservedPort[]; localAddresses?: string[] }
export interface BaseIssue {
  /** Field id (dotted path) the message belongs to. */
  field: string
  message: string
  /** Introduced by this edit: saving is refused, as on the server. */
  blocking: boolean
}
export type MihomoTakeover = 'linux_gateway' | 'full_tun' | null

const YAML_OPTIONS = YAML_1_1

function parse(source: string) {
  const document = parseDocument(source, YAML_OPTIONS)
  if (document.errors.length) throw new Error(`YAML 无法解析：${document.errors[0].message}`)
  if (document.contents === null) throw new Error('当前没有可编辑的基础配置，请重新读取默认模板或在 YAML 中填写配置。')
  if (!isMap(document.contents)) throw new Error('基础配置必须是 YAML 对象，请在下方修正。')
  return document
}

export function pathId(path: BasePath): string { return path.join('.') }

function lookup(values: unknown, path: BasePath): unknown {
  let value = values
  for (const key of path) {
    if (value === null || typeof value !== 'object' || Array.isArray(value) || !Object.prototype.hasOwnProperty.call(value, key)) return undefined
    value = (value as Record<string, unknown>)[key]
  }
  return value
}

function sharedNodes(document: Document) {
  const shared = new Set<unknown>()
  visit(document, { Alias(_key, alias) { shared.add(alias.resolve(document)) } })
  return shared
}

const locked = (value: unknown, reason: string): BaseFieldState => ({ value, present: value !== undefined, editable: false, reason })

function locate(document: Document, values: unknown, shared: Set<unknown>, path: BasePath, kind: FieldKind): BaseFieldState {
  const value = lookup(values, path)
  let node: unknown = document.contents
  if (shared.has(node)) return locked(value, '基础配置被 YAML 锚点引用，请在下方 YAML 中修改。')
  for (let index = 0; index < path.length; index++) {
    const key = path[index]
    const child = (node as YAMLMap).get(key, true) as unknown
    const shown = pathId(path.slice(0, index + 1))
    if (child === undefined) {
      // Missing here but present in the parsed values: it comes from a `<<` merge.
      if (lookup(values, path.slice(0, index + 1)) !== undefined) return locked(value, `${shown} 来自 YAML 合并配置，请在下方 YAML 中修改。`)
      return { value: undefined, present: false, editable: true, reason: '' }
    }
    if (isAlias(child)) return locked(value, `${shown} 使用 YAML 别名，请在下方 YAML 中修改。`)
    if (shared.has(child)) return locked(value, `${shown} 的 YAML 锚点被其他配置引用，请在下方 YAML 中修改。`)
    if (index < path.length - 1) {
      if (isScalar(child) && child.value === null) return { value: undefined, present: false, editable: true, reason: '' }
      if (!isMap(child)) return locked(value, `${shown} 必须是 YAML 对象，请先在下方 YAML 中修正。`)
      node = child
      continue
    }
    if (kind === 'scalar' && !isScalar(child)) return locked(value, `${shown} 的写法无法在表单中编辑，请在下方 YAML 中修改。`)
    if (kind === 'list' && !(isScalar(child) && child.value === null)
      && !(isSeq(child) && child.items.every(item => isScalar(item) && !shared.has(item)))) {
      return locked(value, `${shown} 需为每行一项的列表，当前写法请在下方 YAML 中修改。`)
    }
  }
  return { value, present: value !== undefined, editable: true, reason: '' }
}

export function readMihomoBase(source: string, defaultSource?: string): MihomoBaseView {
  try {
    const document = parse(source.trim() ? source : defaultSource || '')
    const values = document.toJS({ maxAliasCount: 100 }) as Record<string, unknown>
    const shared = sharedNodes(document)
    const cache = new Map<string, BaseFieldState>()
    return {
      error: '',
      value: path => lookup(values, path),
      field(path, kind = 'scalar') {
        const key = `${kind}:${pathId(path)}`
        if (!cache.has(key)) cache.set(key, locate(document, values, shared, path, kind))
        return cache.get(key)!
      },
    }
  } catch (failure) {
    const error = failure instanceof Error ? failure.message : '无法读取基础配置，请检查 YAML。'
    // The form shows the parse error once; the fields themselves stay quiet.
    return { error, value: () => undefined, field: () => locked(undefined, '') }
  }
}

// New top-level keys are placed after a related key that already exists.
const KEY_GROUPS: string[][] = [
  ['port', 'socks-port', 'redir-port', 'mixed-port', 'tproxy-port', 'allow-lan', 'bind-address', 'ipv6', 'unified-delay',
    'tcp-concurrent', 'log-level', 'find-process-mode', 'global-client-fingerprint', 'global-ua', 'keep-alive-idle',
    'keep-alive-interval', 'disable-keep-alive', 'profile'],
  ['external-controller', 'secret', 'external-ui', 'external-ui-name', 'external-ui-url'],
]

function keyOf(pair: Pair) { return isScalar(pair.key) ? String(pair.key.value) : String(pair.key) }

function scalarFor(document: Document, value: unknown, previous?: Scalar) {
  const node = previous ?? (document.createNode(value) as Scalar)
  node.value = value
  if (node.tag) delete node.tag
  // A quoted value stays quoted when it is still text; numbers and booleans are written plain.
  if (typeof value !== 'string' || !(node.type === 'QUOTE_SINGLE' || node.type === 'QUOTE_DOUBLE')) node.type = undefined
  return node
}

function setList(document: Document, values: string[], previous: unknown) {
  const reusable = isSeq(previous) ? [...(previous.items as Scalar[])] : []
  const sequence = isSeq(previous) ? previous : (document.createNode([]) as YAMLSeq)
  // Keep the nodes (and their comments) of entries that remain.
  sequence.items = values.map(value => {
    const index = reusable.findIndex(item => item.value === value)
    return index >= 0 ? reusable.splice(index, 1)[0] : scalarFor(document, value)
  })
  if (!values.length) sequence.flow = true
  return sequence
}

function insertPair(map: YAMLMap, key: string, node: Node, root = false) {
  const group = root ? KEY_GROUPS.find(keys => keys.includes(key)) : undefined
  const pair = new Pair(new Scalar(key), node)
  if (group) {
    // After the closest related key that comes before it in the group, else before the closest one after it.
    const rank = group.indexOf(key)
    let after = -1
    let before = -1
    map.items.forEach((item, position) => {
      const other = group.indexOf(keyOf(item as Pair))
      if (other < 0) return
      if (other < rank && (after < 0 || other > group.indexOf(keyOf(map.items[after] as Pair)))) after = position
      if (other > rank && (before < 0 || other < group.indexOf(keyOf(map.items[before] as Pair)))) before = position
    })
    if (after >= 0 || before >= 0) {
      map.items.splice(after >= 0 ? after + 1 : before, 0, pair)
      if (after < 0 && before >= 0) {
        // The heading comment above the following key now belongs above the new one.
        const next = map.items[before + 1] as Pair
        if (isScalar(next.key) && next.key.commentBefore) {
          ;(pair.key as Scalar).commentBefore = next.key.commentBefore
          ;(pair.key as Scalar).spaceBefore = next.key.spaceBefore
          next.key.commentBefore = undefined
          next.key.spaceBefore = undefined
        }
      }
      return
    }
  }
  map.items.push(pair)
}

function removePair(map: YAMLMap, key: string) {
  const index = map.items.findIndex(item => keyOf(item as Pair) === key)
  if (index < 0) return
  const [pair] = map.items.splice(index, 1) as Pair[]
  // A heading comment above the removed key stays with the key that follows it.
  const comment = isScalar(pair.key) ? pair.key.commentBefore : undefined
  const next = map.items[index] as Pair | undefined
  if (comment && next && isScalar(next.key)) {
    next.key.commentBefore = next.key.commentBefore ? `${comment}\n${next.key.commentBefore}` : comment
    next.key.spaceBefore ||= pair.key && isScalar(pair.key) ? pair.key.spaceBefore : undefined
  }
}

function applyEdit(document: Document, edit: BaseEdit) {
  const kind = edit.kind ?? (Array.isArray(edit.value) ? 'list' : 'scalar')
  const root = document.contents as YAMLMap
  let map = root
  const parents = edit.path.slice(0, -1)
  const key = edit.path[edit.path.length - 1]
  for (const part of parents) {
    let child = map.get(part, true) as unknown
    if (child === undefined || (isScalar(child) && child.value === null)) {
      if (edit.value === undefined) return
      const created = document.createNode({}) as YAMLMap
      if (child === undefined) insertPair(map, part, created, map === root)
      else map.set(part, created)
      child = created
    }
    map = child as YAMLMap
  }
  const previous = map.get(key, true) as unknown
  if (edit.value === undefined) {
    removePair(map, key)
    return
  }
  const node = kind === 'list' ? setList(document, edit.value as string[], previous)
    : kind === 'any' ? document.createNode(edit.value) as Node
      : scalarFor(document, edit.value, isScalar(previous) ? previous : undefined)
  if (previous === undefined) insertPair(map, key, node, map === root)
  else if (node !== previous) map.set(key, node)
}

/** Apply edits in order; throws without changing anything when one cannot be made here. */
export function editMihomoBase(source: string, edits: BaseEdit[], defaultSource?: string): string {
  const view = readMihomoBase(source, defaultSource)
  if (view.error) throw new Error(view.error)
  for (const edit of edits) {
    const kind = edit.kind ?? (Array.isArray(edit.value) ? 'list' : 'scalar')
    const state = view.field(edit.path, kind)
    if (!state.editable) throw new Error(state.reason)
  }
  const document = parse(source.trim() ? source : defaultSource || '')
  for (const edit of edits) applyEdit(document, edit)
  return printYamlPreserving(source, document)
}

/* ---------- DNS presets ---------- */

export type DnsPreset = 'domestic' | 'split' | 'custom'
export const DOMESTIC_IP_DOH = ['https://223.5.5.5/dns-query', 'https://1.12.12.12/dns-query']
export const DOMESTIC_DOH = ['https://doh.pub/dns-query', 'https://dns.alidns.com/dns-query']
export const FOREIGN_DOH = ['https://dns.google/dns-query', 'https://1.1.1.1/dns-query']
/** The one nameserver-policy entry the presets own; every other entry is the user's. */
export const DNS_POLICY_KEY = 'geosite:cn,private'
export const DNS_LIST_KEYS = ['default-nameserver', 'nameserver', 'proxy-server-nameserver', 'direct-nameserver', 'fallback'] as const
interface PresetShape {
  lists: Record<'default-nameserver' | 'nameserver' | 'proxy-server-nameserver' | 'direct-nameserver', string[]>
  followPolicy: boolean
  policy: string[] | null
}
export const DNS_PRESETS: Record<Exclude<DnsPreset, 'custom'>, PresetShape> = {
  domestic: {
    lists: { 'default-nameserver': DOMESTIC_IP_DOH, 'direct-nameserver': DOMESTIC_IP_DOH, 'proxy-server-nameserver': DOMESTIC_DOH, nameserver: DOMESTIC_DOH },
    followPolicy: false,
    policy: null,
  },
  split: {
    lists: { 'default-nameserver': DOMESTIC_IP_DOH, 'direct-nameserver': DOMESTIC_IP_DOH, 'proxy-server-nameserver': DOMESTIC_DOH, nameserver: FOREIGN_DOH },
    followPolicy: true,
    policy: DOMESTIC_DOH,
  },
}
const PRESET_PATHS: BasePath[] = [
  ...Object.keys(DNS_PRESETS.domestic.lists).map(key => ['dns', key]),
  ['dns', 'respect-rules'], ['dns', 'direct-nameserver-follow-policy'], ['dns', 'fallback'],
]
const sameList = (value: unknown, expected: string[]) =>
  Array.isArray(value) && value.length === expected.length && value.every((item, index) => item === expected[index])

export function readDnsPreset(view: MihomoBaseView): { preset: DnsPreset | null; editable: boolean; reason: string } {
  if (view.error) return { preset: null, editable: false, reason: '' }
  const dns = view.value(['dns'])
  if (dns !== undefined && (dns === null || typeof dns !== 'object' || Array.isArray(dns))) {
    return { preset: null, editable: false, reason: 'dns 必须是 YAML 对象，请先在下方 YAML 中修正。' }
  }
  const policy = view.value(['dns', 'nameserver-policy'])
  const policyMap = policy !== null && typeof policy === 'object' && !Array.isArray(policy) ? policy as Record<string, unknown> : undefined
  const reasons = [...PRESET_PATHS.map(path => view.field(path, path[1] === 'respect-rules' || path[1].startsWith('direct-nameserver-follow') ? 'scalar' : 'list')),
    view.field(['dns', 'nameserver-policy', DNS_POLICY_KEY], 'list'),
    // fallback-filter is a map that a preset only ever removes.
    view.field(['dns', 'fallback-filter'], 'any')]
    .filter(state => !state.editable).map(state => state.reason)
  const fallback = view.value(['dns', 'fallback'])
  const matches = (shape: PresetShape) => Object.entries(shape.lists).every(([key, list]) => sameList(view.value(['dns', key]), list))
    && view.value(['dns', 'respect-rules']) === true
    && (view.value(['dns', 'direct-nameserver-follow-policy']) ?? false) === shape.followPolicy
    && (fallback === undefined || fallback === null || (Array.isArray(fallback) && !fallback.length))
    && view.value(['dns', 'fallback-filter']) === undefined
    && (shape.policy === null ? policyMap?.[DNS_POLICY_KEY] === undefined : sameList(policyMap?.[DNS_POLICY_KEY], shape.policy))
  const preset: DnsPreset = matches(DNS_PRESETS.domestic) ? 'domestic' : matches(DNS_PRESETS.split) ? 'split' : 'custom'
  return { preset, editable: !reasons.length, reason: [...new Set(reasons)].join(' ') }
}

/** Switch only the preset-owned keys; the user's own nameserver-policy entries stay first. */
export function applyDnsPreset(source: string, preset: Exclude<DnsPreset, 'custom'>, defaultSource?: string): string {
  const view = readMihomoBase(source, defaultSource)
  const state = readDnsPreset(view)
  if (!state.editable) throw new Error(state.reason)
  const shape = DNS_PRESETS[preset]
  const edits: BaseEdit[] = Object.entries(shape.lists).map(([key, list]) => ({ path: ['dns', key], value: [...list], kind: 'list' }))
  edits.push({ path: ['dns', 'respect-rules'], value: true }, { path: ['dns', 'direct-nameserver-follow-policy'], value: shape.followPolicy })
  if (view.value(['dns', 'fallback']) !== undefined) edits.push({ path: ['dns', 'fallback'], value: undefined, kind: 'list' })
  const policy = view.value(['dns', 'nameserver-policy'])
  const policyEntries = policy !== null && typeof policy === 'object' && !Array.isArray(policy) ? Object.keys(policy) : []
  // Edit one document so the printout keeps the user's formatting (see printYamlPreserving).
  const document = parse(source.trim() ? source : defaultSource || '')
  for (const edit of edits) {
    const state = view.field(edit.path, edit.kind)
    if (!state.editable) throw new Error(state.reason)
    applyEdit(document, edit)
  }
  // fallback-filter is a map, so it is removed outside the scalar / list edits.
  const dnsNode = document.getIn(['dns'], true)
  if (isMap(dnsNode) && dnsNode.has('fallback-filter')) removePair(dnsNode, 'fallback-filter')
  if (shape.policy) {
    if (!isMap(document.getIn(['dns', 'nameserver-policy'], true))) {
      if (policy !== undefined && policy !== null) throw new Error('dns.nameserver-policy 必须是 YAML 对象，请先在下方 YAML 中修正。')
      ;(dnsNode as YAMLMap).set('nameserver-policy', document.createNode({}))
    }
    const map = document.getIn(['dns', 'nameserver-policy'], true) as YAMLMap
    const previous = map.get(DNS_POLICY_KEY, true)
    const node = setList(document, [...shape.policy], previous)
    if (previous === undefined) map.items.push(new Pair(new Scalar(DNS_POLICY_KEY), node))
    else map.set(DNS_POLICY_KEY, node)
  } else if (policyEntries.includes(DNS_POLICY_KEY)) {
    const map = document.getIn(['dns', 'nameserver-policy'], true) as YAMLMap
    removePair(map, DNS_POLICY_KEY)
    if (!map.items.length && isMap(dnsNode)) removePair(dnsNode, 'nameserver-policy')
  }
  return printYamlPreserving(source, document)
}

/* ---------- controller ---------- */

export type ControllerScope = 'lan' | 'local' | 'custom'
export function splitHostPort(value: unknown): { host: string; port: number } | null {
  if (typeof value !== 'string') return null
  const text = value.trim()
  const index = text.lastIndexOf(':')
  if (index < 0) return null
  let host = text.slice(0, index)
  const port = text.slice(index + 1)
  if (!/^\d+$/.test(port) || Number(port) < 1 || Number(port) > 65535) return null
  if (host.startsWith('[')) {
    if (!host.endsWith(']')) return null
    host = host.slice(1, -1)
  } else if (host.includes(':')) return null
  return { host, port: Number(port) }
}
export function joinHostPort(host: string, port: number | string) {
  return `${host.includes(':') ? `[${host}]` : host}:${port}`
}
function isLoopback(host: string) {
  const value = host.toLowerCase()
  if (value === 'localhost' || value === '::1' || /^0*:(0*:)*0*1$/.test(value) || value === '0:0:0:0:0:0:0:1') return true
  return /^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(value)
}
/** Same as controller_scope in the backend: 'lan' wildcard, 'local' loopback, otherwise 'other'. */
export function controllerScope(host: string): 'lan' | 'local' | 'other' {
  if (host === '' || host === '0.0.0.0' || host === '::') return 'lan'
  return isLoopback(host) ? 'local' : 'other'
}

/** Same rules as controller_secret_error in backend/common/setup_flow.py. */
export function controllerSecretError(value: unknown): string {
  if (value === '123456') return '不能使用初始模板的默认密码 123456，请自己设置一个'
  if (typeof value !== 'string' || value.length < 8 || value.length > 64 || !/^[!-~]+$/.test(value)) return '控制面板密码需为 8–64 个字母、数字或符号，不含空格'
  return ''
}

/* ---------- sniffer protocols ---------- */

export const SNIFF_PROTOCOLS = ['HTTP', 'TLS', 'QUIC'] as const
export type SniffProtocol = typeof SNIFF_PROTOCOLS[number]
/** The initial template's ports, written when a protocol is switched on. */
export const SNIFF_DEFAULT_PORTS: Record<SniffProtocol, Array<number | string>> = { HTTP: [80, '8080-8880'], TLS: [443, 8443], QUIC: [443, 8443] }
export interface SniffProtocolState {
  /** The key in sniffer.sniff (Mihomo matches it case-insensitively); the canonical name when missing. */
  key: string
  present: boolean
  ports: BaseFieldState
}
export interface SniffState {
  editable: boolean
  reason: string
  protocols: Record<SniffProtocol, SniffProtocolState>
  /** sniffer.enable is on but no protocol is sniffed. */
  idle: boolean
}

const asMap = (value: unknown) => value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined

export function readSniff(view: MihomoBaseView): SniffState {
  const sniff = view.value(['sniffer', 'sniff'])
  const map = asMap(sniff)
  const legacy = view.value(['sniffer', 'sniffing'])
  const container = view.field(['sniffer', 'sniff'], 'any')
  let reason = view.error ? '' : !container.editable ? container.reason
    : sniff !== undefined && sniff !== null && !map ? 'sniffer.sniff 必须是 YAML 对象，请在下方 YAML 中修改。' : ''
  // Mihomo reads the older `sniffing` list only while `sniff` is empty.
  if (!reason && Array.isArray(legacy) && legacy.length && !Object.keys(map || {}).length) reason = 'YAML 使用旧写法 sniffing，请在下方 YAML 中修改。'
  const protocols = Object.fromEntries(SNIFF_PROTOCOLS.map(name => {
    const key = Object.keys(map || {}).find(item => item.toUpperCase() === name) ?? name
    const present = !!map && Object.prototype.hasOwnProperty.call(map, key)
    const ports = view.field(['sniffer', 'sniff', key, 'ports'], 'list')
    return [name, { key, present, ports: reason ? { ...ports, editable: false, reason } : ports }]
  })) as Record<SniffProtocol, SniffProtocolState>
  const sniffed = SNIFF_PROTOCOLS.some(name => protocols[name].present) || (Array.isArray(legacy) && legacy.length > 0)
  return { editable: !view.error && !reason, reason, protocols, idle: view.value(['sniffer', 'enable']) === true && !sniffed }
}

/** Same as sniff_ports_error in the backend: 80 or 8080-8880, each port 1–65535. */
export function sniffPortError(value: unknown): boolean {
  if (typeof value === 'number') return !Number.isInteger(value) || value < 1 || value > 65535
  if (typeof value !== 'string') return true
  const match = /^\s*(\d+)\s*(?:-\s*(\d+)\s*)?$/.exec(value)
  if (!match) return true
  const low = Number(match[1])
  const high = Number(match[2] ?? match[1])
  return low < 1 || high > 65535 || low > high
}
export const SNIFF_PORTS_HINT = '端口写成 80 或 8080-8880，多个用逗号分隔，范围 1–65535'
export const sniffPortsText = (value: unknown) => Array.isArray(value) ? value.map(String).join(', ') : ''
/** Ports typed in a cell: numbers stay numbers, ranges are text. */
export function parseSniffPorts(text: string): { ports: Array<number | string>; error: string } {
  const parts = text.split(/[,，\s]+/).filter(Boolean)
  if (!parts.length) return { ports: [], error: '至少填写一个端口；不需要这个协议时关闭右侧开关' }
  if (parts.some(sniffPortError)) return { ports: [], error: SNIFF_PORTS_HINT }
  return { ports: parts.map(part => /^\d+$/.test(part) ? Number(part) : part), error: '' }
}

/* ---------- checks ---------- */

export const INBOUND_PORT_KEYS = ['mixed-port', 'port', 'socks-port', 'redir-port', 'tproxy-port'] as const
export const LOG_LEVELS = ['silent', 'error', 'warning', 'info', 'debug'] as const
export const FIND_PROCESS_MODES = ['always', 'strict', 'off'] as const
export const FAKE_IP_FILTER_MODES = ['blacklist', 'whitelist', 'rule'] as const
export const TUN_STACKS = ['system', 'gvisor', 'mixed'] as const
export const CLIENT_FINGERPRINTS = ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random'] as const

export const CACHE_ALGORITHMS = ['lru', 'arc'] as const

const BOOLEANS: BasePath[] = [['allow-lan'], ['unified-delay'], ['tcp-concurrent'], ['disable-keep-alive'], ['profile', 'store-selected'],
  ['profile', 'store-fake-ip'], ['sniffer', 'enable'], ['dns', 'respect-rules'], ['dns', 'direct-nameserver-follow-policy'], ['dns', 'prefer-h3']]
const INTEGERS: Array<[BasePath, number, number]> = [...INBOUND_PORT_KEYS.map(key => [[key], 1, 65535] as [BasePath, number, number]),
  [['tun', 'mtu'], 576, 65535], [['keep-alive-idle'], 1, 86400], [['keep-alive-interval'], 1, 86400]]
const ENUMS: Array<[BasePath, readonly string[]]> = [[['log-level'], LOG_LEVELS], [['find-process-mode'], FIND_PROCESS_MODES],
  [['dns', 'fake-ip-filter-mode'], FAKE_IP_FILTER_MODES], [['tun', 'stack'], TUN_STACKS], [['global-client-fingerprint'], CLIENT_FINGERPRINTS],
  [['dns', 'cache-algorithm'], CACHE_ALGORITHMS]]
const STRING_LISTS: BasePath[] = [...DNS_LIST_KEYS.map(key => ['dns', key]), ['dns', 'fake-ip-filter'], ['sniffer', 'skip-domain'], ['sniffer', 'force-domain']]
const TEXTS: BasePath[] = [['external-ui'], ['global-ua']]
const DNS_UPSTREAM_PATHS: BasePath[] = [...DNS_LIST_KEYS.map(key => ['dns', key]), ['dns', 'nameserver-policy'], ['dns', 'proxy-server-nameserver-policy']]
const PORT_PATHS: BasePath[] = [...INBOUND_PORT_KEYS.map(key => [key]), ['external-controller'], ['dns', 'listen'], ['dns', 'enable'], ['listeners']]
const REGION_PROBE_LISTENER = 'configflow-region-probe'

function parseIP(host: string): 'v4' | 'v6' | null {
  if (/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.test(host)) return host.split('.').every(part => Number(part) <= 255) ? 'v4' : null
  if (!host.includes(':') || !/^[0-9a-f:.]+$/i.test(host)) return null
  try {
    new URL(`http://[${host}]/`)
    return 'v6'
  } catch { return null }
}

function hostOfUpstream(value: string): { host: string; port: number | null; scheme: string } | null {
  const raw = value.split('#', 1)[0].trim()
  try {
    const url = new URL(raw.includes('://') ? raw : `udp://${raw}`)
    let host = url.hostname
    if (host.startsWith('[')) host = host.slice(1, -1)
    return { host: host.toLowerCase(), port: url.port ? Number(url.port) : null, scheme: url.protocol.replace(/:$/, '') }
  } catch { return null }
}

/** Same as mihomo_dns_loop in backend/common/gateway_onboarding.py. */
export function dnsLoop(dns: unknown, localAddresses: string[] = []): { key: string; value: string } | null {
  if (!dns || typeof dns !== 'object' || Array.isArray(dns)) return null
  const keys = ['nameserver', 'fallback', 'default-nameserver', 'proxy-server-nameserver', 'direct-nameserver', 'nameserver-policy', 'proxy-server-nameserver-policy']
  const hosts = new Set(localAddresses.map(item => item.toLowerCase()))
  for (const key of keys) {
    const values = (dns as Record<string, unknown>)[key]
    const pending: unknown[] = values && typeof values === 'object' && !Array.isArray(values) ? Object.values(values) : [values]
    while (pending.length) {
      const value = pending.pop()
      if (Array.isArray(value)) { pending.push(...value); continue }
      if (typeof value !== 'string') continue
      const raw = value.split('#', 1)[0].trim()
      if (raw === 'system' || raw === 'system://') return { key, value }
      const parsed = hostOfUpstream(value)
      if (!parsed) continue
      const kind = parseIP(parsed.host)
      const local = parsed.host === 'localhost' || (kind !== null && (isLoopback(parsed.host) || parsed.host === '0.0.0.0'
        || /^[0:]+$/.test(parsed.host) || /^::ffff:(127\.|7f[0-9a-f]{2}:)/.test(parsed.host)))
      const port = parsed.port ?? (['udp', 'tcp'].includes(parsed.scheme) ? 53 : null)
      if (local || (hosts.has(parsed.host) && (port === 53 || port === 1053))) return { key, value }
    }
  }
  return null
}

function listenPort(value: unknown) { return splitHostPort(value)?.port ?? null }

const ipv4Number = (text: string) => text.split('.').reduce((total, part) => total * 256 + Number(part), 0)
// Both ends of the network inside a special block, as ipaddress.IPv4Network's is_loopback / is_multicast / is_link_local.
const SPECIAL_IPV4: Array<[string, number]> = [['127.0.0.0', 8], ['224.0.0.0', 4], ['169.254.0.0', 16]]
/** Same as fake_ip_network in backend/common/linux_gateway.py: an IPv4 network /8–/30, not loopback, multicast, link-local or 0.0.0.0. */
export function fakeIpRangeError(value: unknown): string {
  const message = 'dns.fake-ip-range 需为 IPv4 网段（/8 至 /30），例如 198.18.0.1/16，不能是回环、多播或链路本地地址'
  if (typeof value !== 'string') return message
  // No spaces or leading zeros: Python's ipaddress refuses them.
  const octet = '(?:25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)'
  const match = new RegExp(`^(${octet}(?:\\.${octet}){3})/(\\d{1,2})$`).exec(value)
  if (!match) return message
  const prefix = Number(match[2])
  if (prefix < 8 || prefix > 30) return message
  const size = 2 ** (32 - prefix)
  const first = Math.floor(ipv4Number(match[1]) / size) * size
  const last = first + size - 1
  if (first === 0) return message
  for (const [base, bits] of SPECIAL_IPV4) {
    const start = ipv4Number(base)
    const end = start + 2 ** (32 - bits) - 1
    if (first >= start && last <= end) return message
  }
  return ''
}

/** Every problem; `changed` decides which of them this edit introduced. */
function problems(values: Record<string, unknown>, changed: (path: BasePath) => boolean, context: BaseContext): Array<{ field: string; message: string }> {
  const found: Array<{ field: string; message: string }> = []
  const add = (path: BasePath | string, message: string) => found.push({ field: typeof path === 'string' ? path : pathId(path), message })
  const get = (path: BasePath) => lookup(values, path)
  for (const path of BOOLEANS) {
    const value = get(path)
    if (changed(path) && value !== undefined && typeof value !== 'boolean') add(path, `${pathId(path)} 只能填写 true 或 false，不要加引号`)
  }
  for (const [path, low, high] of INTEGERS) {
    const value = get(path)
    if (changed(path) && value !== undefined && (typeof value !== 'number' || !Number.isInteger(value) || value < low || value > high)) {
      add(path, `${pathId(path)} 需为 ${low}–${high} 的整数`)
    }
  }
  for (const [path, options] of ENUMS) {
    let value = get(path)
    if (!changed(path) || value === undefined) continue
    if (pathId(path) === 'find-process-mode' && typeof value === 'boolean') continue
    if (typeof value === 'number' && Number.isInteger(value)) value = String(value)
    if (typeof value !== 'string' || !options.includes(value.trim().toLowerCase())) add(path, `${pathId(path)} 只能是 ${options.join(' / ')} 之一`)
  }
  for (const path of STRING_LISTS) {
    const value = get(path)
    if (changed(path) && value !== undefined && value !== null
      && (!Array.isArray(value) || !value.every(item => typeof item === 'string' && item.trim()))) {
      add(path, `${pathId(path)} 需为文本列表，每行一项，不能有空项`)
    }
  }
  for (const path of TEXTS) {
    const value = get(path)
    if (changed(path) && value !== undefined && (typeof value !== 'string' || !value.trim())) add(path, `${pathId(path)} 需为非空文本；不需要时删除该项`)
  }

  const fakeRange = get(['dns', 'fake-ip-range'])
  if (changed(['dns', 'fake-ip-range']) && fakeRange !== undefined && fakeIpRangeError(fakeRange)) add(['dns', 'fake-ip-range'], fakeIpRangeError(fakeRange))

  // Sniffer protocols: Mihomo refuses the whole configuration for an unknown protocol or a bad port.
  const sniff = get(['sniffer', 'sniff'])
  if (sniff !== undefined && sniff !== null) {
    const map = asMap(sniff)
    if (!map) {
      if (changed(['sniffer', 'sniff'])) add('sniffer.sniff', 'sniffer.sniff 需为对象，按协议 HTTP / TLS / QUIC 分别填写')
    } else {
      for (const [key, item] of Object.entries(map)) {
        const name = key.toUpperCase()
        if (!(SNIFF_PROTOCOLS as readonly string[]).includes(name)) {
          if (changed(['sniffer', 'sniff'])) add('sniffer.sniff', `sniffer.sniff 只支持 HTTP / TLS / QUIC，「${key}」无法识别，Mihomo 会拒绝加载`)
          continue
        }
        if (!changed(['sniffer', 'sniff', key])) continue
        const field = `sniffer.sniff.${name}`
        const config = asMap(item)
        const ports = config?.ports
        if (!config || !Array.isArray(ports) || !ports.length || ports.some(sniffPortError)) add(field, `sniffer.sniff.${key}.ports：${SNIFF_PORTS_HINT}`)
        else if (config['override-destination'] !== undefined && typeof config['override-destination'] !== 'boolean') {
          add(field, `sniffer.sniff.${key}.override-destination 只能填写 true 或 false，不要加引号`)
        }
      }
    }
  }

  // Controller and password.
  const controller = get(['external-controller'])
  let exposed = false
  if (controller !== undefined && controller !== null) {
    const parsed = splitHostPort(controller)
    if (changed(['external-controller']) && !parsed) add('external-controller', 'external-controller 需写成「地址:端口」，例如 0.0.0.0:9090 或 127.0.0.1:9090，端口 1–65535')
    const scope = parsed ? controllerScope(parsed.host) : 'other'
    if (changed(['external-controller']) && parsed && scope === 'other') {
      add('external-controller', 'external-controller 只能监听 0.0.0.0 / ::（局域网可访问）或 127.0.0.1 / ::1（仅本机）：Agent 经本机回环地址读取连接、流量并热重载配置，监听其他地址时这些功能不可用')
    }
    exposed = scope !== 'local'
  }
  let secret = get(['secret'])
  if (secret !== undefined && secret !== null && typeof secret !== 'string') {
    if (changed(['secret'])) add('secret', '控制面板密码（secret）必须是文本，纯数字等请加引号')
    secret = undefined
  }
  if (changed(['secret']) || changed(['external-controller'])) {
    if (exposed) {
      if (secret === undefined || secret === null || secret === '') add('secret', '控制面板对局域网开放（external-controller 监听所有地址）时必须设置控制面板密码（secret）')
      else if (controllerSecretError(secret)) add('secret', controllerSecretError(secret))
    } else if (changed(['secret']) && typeof secret === 'string' && secret && controllerSecretError(secret)) add('secret', controllerSecretError(secret))
  }
  const uiUrl = get(['external-ui-url'])
  if (changed(['external-ui-url']) && uiUrl !== undefined) {
    let valid = false
    try {
      const url = new URL(String(uiUrl))
      valid = typeof uiUrl === 'string' && ['http:', 'https:'].includes(url.protocol) && !!url.hostname
    } catch { valid = false }
    if (!valid) add('external-ui-url', 'external-ui-url 需为 http(s) 下载地址，例如 https://github.com/…/gh-pages.zip')
  }
  const bind = get(['bind-address'])
  if (changed(['bind-address']) && bind !== undefined) {
    const text = typeof bind === 'string' ? bind.trim().replace(/^\[(.*)\]$/, '$1') : ''
    if (text !== '*' && !parseIP(text)) add('bind-address', 'bind-address 只能填写 *（所有地址）或本机的一个 IP 地址')
  }

  // Ports.
  if (PORT_PATHS.some(changed)) {
    const owners: Array<[number, string, string]> = []
    for (const key of INBOUND_PORT_KEYS) {
      const value = values[key]
      if (typeof value === 'number' && Number.isInteger(value) && value >= 1 && value <= 65535) owners.push([value, key, key])
    }
    const controllerPort = listenPort(controller)
    if (controllerPort) owners.push([controllerPort, 'external-controller', 'external-controller'])
    const dns = values.dns
    if (dns && typeof dns === 'object' && !Array.isArray(dns) && (dns as Record<string, unknown>).enable !== false) {
      const port = listenPort((dns as Record<string, unknown>).listen)
      if (port) owners.push([port, 'dns.listen', 'dns.listen'])
    }
    const listeners = values.listeners
    if (Array.isArray(listeners)) {
      for (const item of listeners) {
        if (item && typeof item === 'object' && (item as Record<string, unknown>).name !== REGION_PROBE_LISTENER
          && Number.isInteger((item as Record<string, unknown>).port)) {
          owners.push([(item as Record<string, number>).port, `listeners（${(item as Record<string, string>).name || '未命名'}）`, 'listeners'])
        }
      }
    }
    const reserved = new Map((context.reservedPorts || []).map(item => [item.port, item.owner]))
    const seen = new Map<number, [string, string]>()
    for (const [port, owner, field] of owners) {
      const first = seen.get(port)
      if (first) {
        const message = `端口 ${port} 同时被 ${first[0]} 和 ${owner} 使用，请改用不同端口`
        add(first[1], message)
        add(field, message)
      } else seen.set(port, [owner, field])
      if (reserved.has(port)) add(field, `${owner} 使用端口 ${port}，与${reserved.get(port)}冲突，请改用其他端口`)
    }
  }

  // DNS.
  const dns = values.dns
  if (dns && typeof dns === 'object' && !Array.isArray(dns)) {
    const section = dns as Record<string, unknown>
    if (DNS_UPSTREAM_PATHS.some(changed)) {
      const loop = dnsLoop(section, context.localAddresses)
      if (loop) add(['dns', loop.key], `dns.${loop.key} 中的 ${loop.value} 指向本机或系统 DNS，会形成解析循环；请改用外部 DNS，例如 https://223.5.5.5/dns-query`)
    }
    const empty = (value: unknown) => value === undefined || value === null || value === '' || (Array.isArray(value) && !value.length)
    if (changed(['dns', 'nameserver']) && section.enable === true && empty(section.nameserver)) add('dns.nameserver', 'dns.nameserver 至少填写一个上游')
    if ((changed(['dns', 'respect-rules']) || changed(['dns', 'proxy-server-nameserver'])) && section['respect-rules'] === true
      && empty(section['proxy-server-nameserver'])) {
      add('dns.proxy-server-nameserver', '开启 dns.respect-rules 时必须填写 dns.proxy-server-nameserver，否则 Mihomo 无法启动')
    }
  }
  return found
}

function valuesOf(source: string, defaultSource?: string): Record<string, unknown> | null {
  try {
    const document = parse(source.trim() ? source : defaultSource || '')
    return document.toJS({ maxAliasCount: 100 }) as Record<string, unknown>
  } catch { return null }
}

/** Problems of `source`; those that `saved` does not already have block saving, as on the server. */
export function checkMihomoBase(source: string, saved: string, defaultSource: string | undefined, context: BaseContext = {}): BaseIssue[] {
  const values = valuesOf(source, defaultSource)
  if (!values) return []
  const previous = valuesOf(saved, defaultSource)
  const all = problems(values, () => true, context)
  const blocking = problems(values, path => !previous || !same(lookup(values, path), lookup(previous, path)), context)
  const blockingKeys = new Set(blocking.map(item => `${item.field}\n${item.message}`))
  const seen = new Set<string>()
  return all.filter(item => {
    const key = `${item.field}\n${item.message}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  }).map(item => ({ ...item, blocking: blockingKeys.has(`${item.field}\n${item.message}`) }))
}

/** Text of a list field, one entry per line; blank lines are dropped. */
export function parseLines(text: string): string[] {
  return text.split('\n').map(line => line.trim()).filter(Boolean)
}
