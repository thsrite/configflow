import { isAlias, isMap, isScalar, parseDocument, visit } from 'yaml'
import { printYamlPreserving } from './yamlPreserve'

export type MihomoIPv6Field = 'core' | 'dns'
export const DEFAULT_MIHOMO_FAKE_IPV6_RANGE = 'fc00::1/64'
export interface MihomoIPv6Edit {
  content: string
  addedFakeIPv6Range: boolean
  notice: string
}
export interface MihomoIPv6Value {
  value: boolean | null
  source: 'explicit' | 'default' | 'invalid'
  editable: boolean
  message: string
}
export interface MihomoFakeIPv6Range {
  visible: boolean
  value: string
  source: 'explicit' | 'default' | 'invalid'
  editable: boolean
  error: string
  message: string
}
export interface MihomoIPv6Config {
  core: MihomoIPv6Value
  dns: MihomoIPv6Value
  error: string
}

const invalid = (message: string): MihomoIPv6Value => ({ value: null, source: 'invalid', editable: false, message })
const pathFor = (field: MihomoIPv6Field) => field === 'core' ? ['ipv6'] : ['dns', 'ipv6']

// Reading keeps the editor's YAML 1.2 rules (`ipv6: yes` is reported, not guessed).
const YAML_OPTIONS = { merge: true, uniqueKeys: true } as const

function parse(source: string) {
  const document = parseDocument(source, YAML_OPTIONS)
  if (document.errors.length) throw new Error(`YAML 无法解析：${document.errors[0].message}`)
  if (document.contents === null) throw new Error('当前没有可编辑的基础配置，请重新读取默认模板或在 YAML 中填写配置。')
  if (!isMap(document.contents)) throw new Error('基础配置必须是 YAML 对象，请在下方修正。')
  return document
}

export function readMihomoIPv6(source: string, defaultSource?: string): MihomoIPv6Config {
  try {
    const document = parse(source.trim() ? source : defaultSource || '')
    // Resolve aliases for display without ever writing through them.
    const values = document.toJS({ maxAliasCount: 100 }) as Record<string, unknown>
    const shared = new Set<unknown>()
    visit(document, { Alias(_key, alias) { shared.add(alias.resolve(document)) } })
    const read = (field: MihomoIPv6Field): MihomoIPv6Value => {
      const parent = field === 'core' ? values : values.dns
      if (field === 'dns' && parent !== undefined && (parent === null || typeof parent !== 'object' || Array.isArray(parent))) {
        return invalid('dns 必须是 YAML 对象，请先修正 DNS 配置。')
      }
      const value = (parent as Record<string, unknown> | undefined)?.ipv6
      if (value !== undefined && typeof value !== 'boolean') return invalid(`${field === 'dns' ? 'dns 下的 ' : ''}ipv6 只能填写 true 或 false，不要加引号。`)
      const node = field === 'core' ? document.contents : document.getIn(['dns'], true)
      const target = document.getIn(pathFor(field), true)
      const sharedAnchor = shared.has(document.contents) || (node !== undefined && shared.has(node))
        || (target !== undefined && shared.has(target))
      const inheritedDNS = field === 'dns' && node === undefined && parent !== undefined
      const editable = !sharedAnchor && !inheritedDNS && (field === 'core' || node === undefined || isMap(node))
      return {
        value: value === undefined ? field === 'core' : value,
        source: value === undefined ? 'default' : 'explicit',
        editable,
        message: sharedAnchor ? '此设置的 YAML 锚点被其他配置引用，请在下方 YAML 中修改。'
          : inheritedDNS ? 'DNS 来自 YAML 合并配置，请在下方 YAML 中修改。'
          : !editable ? 'DNS 使用 YAML 别名，请在下方 YAML 中修改。'
          : value === undefined ? `未填写，沿用 Mihomo 默认${field === 'core' ? '开启' : '关闭'}` : `YAML 已${value ? '开启' : '关闭'}`,
      }
    }
    return { core: read('core'), dns: read('dns'), error: '' }
  } catch (failure) {
    const message = failure instanceof Error ? failure.message : '无法读取 IPv6 设置，请检查 YAML。'
    return { core: invalid(message), dns: invalid(message), error: message }
  }
}

/** Validate without rewriting host bits or the user's chosen IPv6 spelling. */
export function validateMihomoFakeIPv6Range(value: string): string {
  const match = /^([0-9a-f:.]+)\/(\d{1,3})$/i.exec(value.trim())
  const invalid = '请填写有效的 IPv6 网段，例如 fc00::1/64。'
  if (!match || !match[1].includes(':') || Number(match[2]) > 128) return invalid
  let address: string
  try {
    // The platform URL parser validates compressed IPv6 and embedded IPv4 forms;
    // the character allowlist above excludes zones, ports, paths and user info.
    address = new URL(`http://[${match[1]}]/`).hostname.slice(1, -1)
  } catch {
    return invalid
  }
  const first = Number.parseInt(address.split(':')[0] || '0', 16)
  if ((first & 0xfe00) !== 0xfc00 || Number(match[2]) < 7) {
    return '请使用 fc00::/7 范围内的内网网段，前缀长度为 7–128；不要填写公网或 fe80:: 链路本地地址。'
  }
  return ''
}

/** Read the effective range without materializing a missing value in the draft. */
export function readMihomoFakeIPv6Range(source: string, defaultSource?: string): MihomoFakeIPv6Range {
  try {
    const document = parse(source.trim() ? source : defaultSource || '')
    const values = document.toJS({ maxAliasCount: 100 }) as Record<string, unknown>
    const dns = values.dns
    if (dns === null || typeof dns !== 'object' || Array.isArray(dns)) {
      return { visible: false, value: '', source: 'invalid', editable: false, error: '', message: '' }
    }
    const settings = dns as Record<string, unknown>
    // Both IPv6 modes route the range upstream (dns.ipv6 only decides whether
    // clients receive it), so it is shown whenever Mihomo uses IPv6 and Fake-IP.
    const visible = (values.ipv6 === undefined || values.ipv6 === true) && settings.enable === true
      && settings['enhanced-mode'] === 'fake-ip'
    const raw = settings['fake-ip-range6']
    const missing = !Object.prototype.hasOwnProperty.call(settings, 'fake-ip-range6')
    const value = missing ? DEFAULT_MIHOMO_FAKE_IPV6_RANGE
      : typeof raw === 'string' ? raw : raw === null || typeof raw === 'object' ? '' : String(raw)
    const error = missing ? '' : typeof raw === 'string' ? validateMihomoFakeIPv6Range(raw)
      : 'IPv6 Fake-IP 网段必须是文本，请填写有效的 IPv6 网段。'
    const dnsNode = document.getIn(['dns'], true)
    const target = document.getIn(['dns', 'fake-ip-range6'], true)
    const shared = new Set<unknown>()
    visit(document, { Alias(_key, alias) { shared.add(alias.resolve(document)) } })
    const inheritedDNS = dnsNode === undefined
    const inheritedRange = target === undefined && !missing
    const sharedAnchor = shared.has(document.contents) || shared.has(dnsNode) || shared.has(target)
    const editable = isMap(dnsNode) && !inheritedRange && !sharedAnchor && !isAlias(target)
      && (target === undefined || isScalar(target))
    const message = sharedAnchor ? '此网段的 YAML 锚点被其他配置引用，请在下方 YAML 中修改。'
      : isAlias(dnsNode) || isAlias(target) ? '此网段使用 YAML 别名，请在下方 YAML 中修改。'
      : inheritedDNS || inheritedRange ? '此网段来自 YAML 合并配置，请在下方 YAML 中修改。'
      : !editable ? '此网段的 YAML 格式无法直接编辑，请在下方 YAML 中修正。'
      : missing ? 'YAML 尚未填写，上游路由暂不发布此网段；编辑网段或重新选择 IPv6 模式后补齐。' : ''
    return { visible, value, source: missing ? 'default' : error ? 'invalid' : 'explicit', editable, error, message }
  } catch (failure) {
    const message = failure instanceof Error ? failure.message : '无法读取 IPv6 Fake-IP 网段，请检查 YAML。'
    return { visible: false, value: '', source: 'invalid', editable: false, error: message, message }
  }
}

/** Change only the local range; aliases and merge sources stay under manual control. */
export function setMihomoFakeIPv6Range(source: string, value: string, defaultSource?: string): string {
  const state = readMihomoFakeIPv6Range(source, defaultSource)
  if (!state.editable) throw new Error(state.message || state.error || '当前网段无法直接编辑，请在 YAML 中修改。')
  if (!state.visible) throw new Error('请先开启 IPv6，并使用 Fake-IP 模式。')
  const error = validateMihomoFakeIPv6Range(value)
  if (error) throw new Error(error)
  const original = source.trim() ? source : defaultSource || ''
  const document = parse(original)
  const path = ['dns', 'fake-ip-range6']
  const previous = document.getIn(path, true)
  if (isScalar(previous)) {
    previous.value = value.trim()
    // Explicitly correcting a typed scalar must not leave e.g. !!int on a CIDR.
    if (previous.tag && previous.tag !== 'tag:yaml.org,2002:str') delete previous.tag
  } else document.setIn(path, value.trim())
  return printYamlPreserving(original, document, YAML_OPTIONS)
}

/** Edit the selected flag and fill a missing Fake-IP IPv6 range once Mihomo uses IPv6 with Fake-IP. */
export function setMihomoIPv6WithDetails(source: string, field: MihomoIPv6Field, enabled: boolean, defaultSource?: string): MihomoIPv6Edit {
  const state = readMihomoIPv6(source, defaultSource)
  if (!state[field].editable) throw new Error(state[field].message)
  const original = source.trim() ? source : defaultSource || ''
  const document = parse(original)
  const path = pathFor(field)
  const previous = document.getIn(path, true)
  if (isScalar(previous) && !isAlias(previous)) previous.value = enabled
  else document.setIn(path, enabled)

  let addedFakeIPv6Range = false
  let notice = ''
  // Read the effective values so inherited or aliased custom ranges remain intact.
  // Auto-fill never turns on DNS/TUN, changes DNS mode, or writes shared mappings.
  const values = document.toJS({ maxAliasCount: 100 }) as Record<string, unknown>
  const dns = values.dns
  if (enabled && (values.ipv6 === undefined || values.ipv6 === true)
    && dns !== null && typeof dns === 'object' && !Array.isArray(dns)) {
    const settings = dns as Record<string, unknown>
    // dns.ipv6 is not required: both IPv6 modes publish the range's upstream route.
    if (settings.enable === true && settings['enhanced-mode'] === 'fake-ip') {
      if (Object.prototype.hasOwnProperty.call(settings, 'fake-ip-range6')) {
        if (typeof settings['fake-ip-range6'] !== 'string' || !settings['fake-ip-range6'].trim()) {
          notice = '已有 fake-ip-range6 的值为空或不是文本，未自动覆盖；请在 YAML 中填写有效的 IPv6 网段。'
        }
      } else if (!state.dns.editable) {
        notice = 'IPv6 开关已更新，但 DNS 使用共享或继承配置，未自动补充 IPv6 Fake-IP 网段；请在 YAML 中填写 fake-ip-range6。'
      } else {
        document.setIn(['dns', 'fake-ip-range6'], DEFAULT_MIHOMO_FAKE_IPV6_RANGE)
        addedFakeIPv6Range = true
        notice = `已补充 IPv6 Fake-IP 网段 ${DEFAULT_MIHOMO_FAKE_IPV6_RANGE}。`
      }
    }
  }
  // Turning dns.ipv6 off keeps the range in use (and on screen) for the route.
  if (!enabled && field === 'core' && dns !== null && typeof dns === 'object' && !Array.isArray(dns)
    && Object.prototype.hasOwnProperty.call(dns, 'fake-ip-range6')) {
    notice = '已关闭 IPv6 Fake-IP，网段保留供再次开启。'
  }
  // Only the changed lines are written back; the rest of the text stays as typed.
  return { content: printYamlPreserving(original, document, YAML_OPTIONS), addedFakeIPv6Range, notice }
}

/** String-only compatibility API for callers that do not display edit notices. */
export function setMihomoIPv6(source: string, field: MihomoIPv6Field, enabled: boolean, defaultSource?: string): string {
  return setMihomoIPv6WithDetails(source, field, enabled, defaultSource).content
}

/**
 * The three supported uses of IPv6, mapped onto the effective top-level `ipv6`
 * (Mihomo default: on) and `dns.ipv6` (default: off):
 *   off     → ipv6: false, dns.ipv6: false
 *   nodes   → ipv6: true,  dns.ipv6: false  (「Mihomo 用 IPv6，客户端走 IPv4 分流」)
 *   clients → ipv6: true,  dns.ipv6: true   (「客户端也走 IPv6」)
 * Both IPv6 modes fill a missing Fake-IP range (Fake-IP DNS only), since the
 * upstream router routes it in either mode.
 * Any other handwritten combination is reported as `custom` and never rewritten
 * until the user picks a mode.
 */
export type MihomoIPv6Mode = 'off' | 'nodes' | 'clients'
export const MIHOMO_IPV6_MODES: readonly MihomoIPv6Mode[] = ['off', 'nodes', 'clients']
export interface MihomoIPv6ModeOption {
  available: boolean
  reason: string
}
export interface MihomoIPv6ModeState {
  /** null when the YAML cannot be read or a value has the wrong type. */
  mode: MihomoIPv6Mode | 'custom' | null
  config: MihomoIPv6Config
  options: Record<MihomoIPv6Mode, MihomoIPv6ModeOption>
  /** Why a handwritten combination matches none of the modes. */
  customReason: string
  /** Why the YAML cannot be read, or why a value cannot be edited here. */
  lockedReason: string
  /** Missing values shown with Mihomo's defaults. */
  defaultNote: string
}

const targets: Record<MihomoIPv6Mode, Record<MihomoIPv6Field, boolean>> = {
  off: { core: false, dns: false },
  nodes: { core: true, dns: false },
  clients: { core: true, dns: true },
}

export function readMihomoIPv6Mode(source: string, defaultSource?: string): MihomoIPv6ModeState {
  const config = readMihomoIPv6(source, defaultSource)
  const blocked = (reason: string): MihomoIPv6ModeState => ({
    mode: null, config, customReason: '', lockedReason: reason, defaultNote: '',
    options: { off: { available: false, reason }, nodes: { available: false, reason }, clients: { available: false, reason } },
  })
  if (config.error) return blocked(config.error)
  const invalid = [config.core, config.dns].filter(value => value.source === 'invalid')
  if (invalid.length) return blocked(invalid.map(value => value.message).join(' '))
  const core = config.core.value === true
  const dns = config.dns.value === true
  const mode: MihomoIPv6ModeState['mode'] = !core ? (dns ? 'custom' : 'off') : dns ? 'clients' : 'nodes'
  const options = Object.fromEntries(MIHOMO_IPV6_MODES.map(target => {
    const locked = (['core', 'dns'] as const).filter(field => config[field].value !== targets[target][field] && !config[field].editable)
    return [target, { available: !locked.length, reason: locked.map(field => config[field].message).join(' ') }]
  })) as Record<MihomoIPv6Mode, MihomoIPv6ModeOption>
  const lockedReason = [...new Set((['core', 'dns'] as const).filter(field => !config[field].editable).map(field => config[field].message))].join(' ')
  const defaults = [config.core.source === 'default' && 'ipv6（默认开启）', config.dns.source === 'default' && 'dns.ipv6（默认关闭）'].filter(Boolean)
  return {
    mode, config, options, lockedReason,
    customReason: mode === 'custom'
      ? '顶层 ipv6 为 false，dns.ipv6 却为 true：Mihomo 不使用 IPv6，DNS 仍会返回 IPv6 地址。选择任一模式会改写这两项。' : '',
    defaultNote: defaults.length ? `YAML 未填写 ${defaults.join('、')}，按 Mihomo 默认值显示。` : '',
  }
}

/** Write only the values the chosen mode changes; comments and other YAML stay. */
export function setMihomoIPv6Mode(source: string, mode: MihomoIPv6Mode, defaultSource?: string): MihomoIPv6Edit {
  const state = readMihomoIPv6Mode(source, defaultSource)
  if (state.mode === null) throw new Error(state.lockedReason)
  if (!state.options[mode].available) throw new Error(state.options[mode].reason)
  let content = source
  let addedFakeIPv6Range = false
  const notices: string[] = []
  // Turn the core on before DNS so the Fake-IP range is filled exactly once,
  // and turn DNS off before the core so no step passes through `custom`.
  const order: MihomoIPv6Field[] = targets[mode].core ? ['core', 'dns'] : ['dns', 'core']
  for (const field of order) {
    const current = readMihomoIPv6(content, defaultSource)[field].value
    if (current === targets[mode][field]) continue
    const edit = setMihomoIPv6WithDetails(content, field, targets[mode][field], defaultSource)
    content = edit.content
    addedFakeIPv6Range ||= edit.addedFakeIPv6Range
    if (edit.notice && !notices.includes(edit.notice)) notices.push(edit.notice)
  }
  return { content, addedFakeIPv6Range, notice: notices.join(' ') }
}
