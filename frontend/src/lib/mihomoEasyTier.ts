/**
 * EasyTier networking for generated Mihomo configs (Mihomo v1.19.31+ `type: easytier`).
 * Every option of the outbound keeps Mihomo's own key, so the form writes exactly the YAML users read in
 * https://wiki.metacubex.one/config/proxies/easytier/ — the server checks the same rules on save.
 */
import type { BaseFieldDef } from '@/lib/mihomoBaseCopy'

export type EasyTierApplyTo = 'subscription' | 'agent' | 'all'
export type EasyTierOptions = Record<string, string | number | boolean | string[]>
export interface EasyTierSettings {
  enabled: boolean
  name: string
  apply_to: EasyTierApplyTo
  routes: string[]
  magic_dns: boolean
  keepalive: { enabled: boolean; url: string; interval: number }
  options: EasyTierOptions
}

export const defaultEasyTier = (): EasyTierSettings => ({
  enabled: false, name: 'EasyTier', apply_to: 'subscription', routes: [], magic_dns: false,
  keepalive: { enabled: false, url: '', interval: 300 }, options: { udp: true },
})

export function readEasyTier(value: unknown): EasyTierSettings {
  const base = defaultEasyTier()
  if (!value || typeof value !== 'object') return base
  const source = value as Partial<EasyTierSettings>
  return {
    enabled: source.enabled === true,
    name: typeof source.name === 'string' && source.name ? source.name : base.name,
    apply_to: source.apply_to === 'agent' || source.apply_to === 'all' ? source.apply_to : 'subscription',
    routes: Array.isArray(source.routes) ? source.routes.map(String) : [],
    magic_dns: source.magic_dns === true,
    keepalive: {
      enabled: source.keepalive?.enabled === true,
      url: typeof source.keepalive?.url === 'string' ? source.keepalive.url : '',
      interval: typeof source.keepalive?.interval === 'number' ? source.keepalive.interval : 300,
    },
    options: source.options && typeof source.options === 'object' ? { ...source.options } : {},
  }
}

/** Stable text of a setting, for dirty checks. */
export function easyTierKey(value: EasyTierSettings): string {
  const options = Object.keys(value.options).sort().reduce<EasyTierOptions>((result, key) => {
    const item = value.options[key]
    if (item !== undefined && item !== '' && !(Array.isArray(item) && !item.length)) result[key] = item
    return result
  }, {})
  return JSON.stringify({ ...value, options })
}

export const SECRET_KEYS = ['network-secret', 'local-private-key'] as const

// 【作用】 plus an optional 【说明】, the headings the base settings sheet uses.
const help = (what: string, detail = '') => detail ? `【作用】\n${what}\n\n【说明】\n${detail}` : `【作用】\n${what}`
const field = (key: string, label: string, type: BaseFieldDef['type'], text: string, extra: Partial<BaseFieldDef> = {}): BaseFieldDef =>
  ({ ...extra, path: ['easytier', key], label: `${label}（${key}）`, type, help: help(text, extra.help || '') })

export interface EasyTierGroup { id: string; title: string; help: string; fields: Array<BaseFieldDef | { secret: (typeof SECRET_KEYS)[number]; label: string; help: string }> }

/** The outbound's options, grouped as on the docs page. All 34 keys besides name and type. */
export const EASYTIER_GROUPS: EasyTierGroup[] = [
  {
    id: 'network', title: '网络', help: '加入哪个 EasyTier 网络、用什么身份。网络名和密钥与其他节点一致才能互通。',
    fields: [
      field('network-name', '网络名', 'text', '要加入的 EasyTier 网络名，和其他节点填写的一致。', { placeholder: '必填' }),
      { secret: 'network-secret', label: '网络密钥（network-secret）', help: help('网络密钥。网络名和密钥都相同的节点才在同一个虚拟网里。', '导出脱敏备份时会被隐去。') },
      field('hostname', '主机名', 'text', '本节点在虚拟网里的名字，也用于 MagicDNS（如 phone.et.net）。', { help: '同一个配置空间的订阅被多台设备使用时请留空：大家拿到的是同一份配置，写死会让几台设备同名，用名字访问时分不清是哪台。', placeholder: '自动' }),
      field('ipv4', '虚拟 IPv4', 'text', '本节点的虚拟网地址，例如 10.144.144.10/24。', { help: '不填时自动分配（DHCP），每次重启可能变化；要固定地址就在这里填写。\n同一个配置空间的订阅被多台设备使用时请留空：大家拿到的是同一份配置，写死会让几台设备抢同一个地址。', placeholder: '自动分配' }),
      field('dhcp', '自动分配地址', 'bool', '是否通过 DHCP 自动获取虚拟 IPv4。', { help: '不填虚拟 IPv4 时默认开启；填了则默认关闭。' }),
      field('peers', '入网节点', 'list', '启动时主动连接的节点，可以写多个，例如中转 VPS 和家里节点。', {
        help: '每行一个，如 tcp://203.0.113.10:11010、udp://[2001:db8::1]:11010。都会同时连接，入网后 EasyTier 自动选择直连或中转。不会自动连接公共节点。不监听端口时至少填一个。',
        placeholder: 'tcp://203.0.113.10:11010',
      }),
    ],
  },
  {
    id: 'listen', title: '监听', help: '默认不监听端口，只主动连出去；打开「监听端口」后，其他节点也能直接连进来。',
    fields: [
      field('listeners', '监听地址', 'list', '让其他节点连进来的地址，例如 tcp://0.0.0.0:11010。', {
        help: '不填时使用 tcp://0.0.0.0:11010。要同时接受 IPv6 连接，再加一行 tcp://[::]:11010。', placeholder: 'tcp://0.0.0.0:11010',
      }),
      field('mapped-listeners', '对外公布地址', 'list', '经过端口映射或内网穿透时，告诉其他节点从哪个公网地址连进来。', {
        help: '外面看到的地址或端口和本机监听的一样时不用填。', placeholder: 'tcp://203.0.113.10:11010',
      }),
    ],
  },
  {
    id: 'route', title: '路由与出口', help: '子网代理、出口节点和选路方式。',
    fields: [
      field('proxy-networks', '公布本地网段', 'list', '把本机所在的局域网公布给虚拟网里的其他节点访问，例如 192.168.50.0/24。', {
        help: '不支持「->」映射写法，只能原样公布网段。访问别的节点公布的网段不需要填写这里。', placeholder: '192.168.50.0/24',
      }),
      field('exit-nodes', '出口节点', 'list', '通过这些节点访问外部网络，填写它们的虚拟 IPv4。', { placeholder: '10.144.144.1' }),
      field('enable-exit-node', '作为出口节点', 'bool', '允许其他节点把本节点当作出口。', { coreDefault: false }),
      field('latency-first', '延迟优先', 'bool', '选路时优先延迟最低的路径，而不是跳数最少的路径。', { coreDefault: false }),
      field('disable-p2p', '禁用直连', 'bool', '不尝试点对点直连，所有流量都经中转。', { coreDefault: false }),
      field('accept-dns', '接受网络下发的 DNS', 'bool', '是否使用网络推送的 DNS 设置。', { coreDefault: false }),
      field('tld-dns-zone', 'MagicDNS 域名后缀', 'text', 'MagicDNS 使用的域名后缀。', { placeholder: 'et.net.' }),
    ],
  },
  {
    id: 'security', title: '安全', help: '加密、安全模式和固定节点身份。',
    fields: [
      field('secure-mode', '安全模式', 'bool', '启用 Noise 端到端加密。网络开启了安全模式时这里也要开启。', { coreDefault: false, help: '填写本机密钥或在入网节点地址里带 peer-public-key 时会自动开启。\nMihomo v1.19.34 以下（例如 FlClash 0.9.3 内置的 v1.19.33）开启安全模式时必须填写本机私钥和公钥，否则 EasyTier 启动失败（local private key is not set）；v1.19.34 起不填会自动生成。' }),
      { secret: 'local-private-key', label: '本机私钥（local-private-key）', help: help('本机的 X25519 私钥（Base64），固定节点身份，重启后不变。', '同一个配置空间的订阅被多台设备使用时请留空：大家拿到的是同一份配置，写死会让几台设备变成同一个身份，互相顶掉连接。\n导出脱敏备份时会被隐去。') },
      field('local-public-key', '本机公钥', 'text', '本机的 X25519 公钥（Base64），通常可由私钥推出。', { help: '需要和本机私钥一起填写。多台设备共用这份配置时和私钥一样留空。' }),
      field('enable-encryption', '节点间加密', 'bool', '节点之间的流量是否加密。', { coreDefault: true }),
      field('encryption-algorithm', '加密算法', 'text', '节点间加密使用的算法。', { placeholder: 'aes-gcm' }),
      field('private-mode', '私有模式', 'bool', '只与同网络名和密钥的节点通信，不为其他网络转发。', { coreDefault: false }),
    ],
  },
  {
    id: 'transport', title: '传输', help: 'UDP、MTU 和 KCP / QUIC 代理。',
    fields: [
      field('udp', 'UDP', 'bool', '是否通过 EasyTier 转发 UDP。', { coreDefault: false }),
      field('mtu', 'MTU', 'int', '虚拟网 MTU。', { placeholder: '1380', min: 576, max: 9000 }),
      field('enable-kcp-proxy', 'KCP 代理', 'bool', '用 KCP 转发 TCP 流量，弱网下可能更稳。', { coreDefault: false }),
      field('disable-kcp-input', '拒绝 KCP 输入', 'bool', '不接受其他节点发来的 KCP 代理流量。', { coreDefault: false }),
      field('enable-quic-proxy', 'QUIC 代理', 'bool', '用 QUIC 转发 TCP 流量。', { coreDefault: false }),
      field('disable-quic-input', '拒绝 QUIC 输入', 'bool', '不接受其他节点发来的 QUIC 代理流量。', { coreDefault: false }),
      field('ip-version', '底层 IP 版本', 'enum', '连接其他节点时用 IPv4 还是 IPv6。只影响底层连接，虚拟网仍只支持 IPv4。', {
        unset: '默认', options: ['dual', 'ipv4', 'ipv6', 'ipv4-prefer', 'ipv6-prefer'].map(value => ({ value, label: value })),
      }),
    ],
  },
  {
    id: 'advanced', title: '高级', help: '实例、状态目录和底层连接的出站方式。',
    fields: [
      field('instance-name', '实例名', 'text', 'EasyTier 实例名。', { placeholder: '同节点名称' }),
      field('state-dir', '状态目录', 'text', '保存实例 ID 的目录，让重启后保持同一身份。', { placeholder: 'easytier/<节点名称>' }),
      field('dialer-proxy', '前置代理', 'text', '让 EasyTier 的底层连接先经过另一个节点或策略组。', { placeholder: '节点或策略组名' }),
      field('interface-name', '出口网卡', 'text', '底层连接使用的网卡。'),
      field('routing-mark', '路由标记', 'int', 'Linux 路由标记（fwmark）。', { min: 0 }),
    ],
  },
]

/** 「监听端口」 stands for no-listener: off writes nothing (Mihomo's default, no listener); on writes no-listener: false
 *  until listeners are filled in, which listen by themselves. */
export const LISTEN_FIELD: BaseFieldDef = {
  path: ['easytier', 'listen'], label: '监听端口', type: 'bool', coreDefault: false,
  help: help('是否监听端口，让其他节点直接连进来。', '默认关闭，只主动连出去，外出设备一般不需要。打开后不填监听地址时使用 tcp://0.0.0.0:11010（对应 no-listener: false）。关闭时一并清除监听地址和对外公布地址。'),
}
export const isListening = (options: EasyTierOptions) => (Array.isArray(options.listeners) && options.listeners.length > 0) || options['no-listener'] === false

export const OUTPUT_FIELDS = {
  name: { path: ['easytier', 'name'], label: '节点名称', type: 'text', help: help('生成的 EasyTier 节点名，规则里也可以直接引用。', '不能和已有节点或策略组重名。'), placeholder: 'EasyTier' },
  apply_to: {
    path: ['easytier', 'apply_to'], label: '下发到', type: 'enum', help: help('哪些配置带上 EasyTier。',
      '订阅和下载适合手机、笔记本等外出设备；家里网关上的 Mihomo 一般不需要。推送到 Agent 时，设备内核需要 v1.19.31 或更新。'),
    showOptionLabel: true,
    options: [{ value: 'subscription', label: '订阅和下载' }, { value: 'agent', label: 'Agent 设备' }, { value: 'all', label: '全部' }],
  },
  routes: {
    path: ['easytier', 'routes'], label: '走 EasyTier 的网段', type: 'list', help: help('这些 IPv4 网段的流量交给 EasyTier，规则排在最前面。',
      '通常填虚拟网网段（如 10.144.144.0/24）和其他节点公布的网段（如 10.200.0.0/24）。'), placeholder: '10.144.144.0/24',
  },
  magic_dns: { path: ['easytier', 'magic_dns'], label: '解析组网设备名', type: 'bool', coreDefault: false, help: help('用主机名访问虚拟网设备，例如 nas.et.net。', '添加该域名后缀的分流规则，并把它的解析交给 EasyTier（nameserver-policy）。') },
  keepalive: { path: ['easytier', 'keepalive', 'enabled'], label: '启动即连接', type: 'bool', coreDefault: false, help: help('Mihomo 启动后马上加入虚拟网，而不是等第一次访问时才连接。',
    '生成一个带健康检查的策略组，定时访问下面的地址来保持连接。需要别人连进来时（例如开启了监听）必须开启。') },
  keepalive_url: { path: ['easytier', 'keepalive', 'url'], label: '检测地址', type: 'text', help: help('保持连接用的网址，填虚拟网里某台设备上的网页。', '访问失败也会触发连接。'), placeholder: 'http://10.144.144.1/' },
  keepalive_interval: { path: ['easytier', 'keepalive', 'interval'], label: '检测间隔', type: 'int', help: help('多久检测一次。'), unit: '秒', min: 10, max: 86400 },
} satisfies Record<string, BaseFieldDef>

const isIPv4 = (text: string) => /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/.test(text)
function networkError(item: string, label: string): string {
  if (item.includes('->')) return `${label}「${item}」：Mihomo 的 EasyTier 不支持「->」网段映射`
  const [address, prefix, extra] = item.split('/')
  if (extra !== undefined || !isIPv4(address) || (prefix !== undefined && !(/^\d+$/.test(prefix) && Number(prefix) <= 32))) {
    return `${label}「${item}」不是有效的 IPv4 网段`
  }
  return ''
}
const uriError = (item: string, label: string) => /^[a-z0-9]+:\/\/\S+$/i.test(item) ? '' : `${label}「${item}」格式不对，应类似 tcp://203.0.113.10:11010`
const list = (value: unknown) => Array.isArray(value) ? value.map(String) : []

/** Blocking problems by field key (`name`, `routes`, `keepalive.url` or an option key). Matches the server's checks. */
export function easyTierIssues(value: EasyTierSettings): Record<string, string> {
  const issues: Record<string, string> = {}
  const options = value.options
  const set = (key: string, message: string) => { if (message && !issues[key]) issues[key] = message }
  if (!value.name.trim()) set('name', '请填写节点名称')
  else if (value.name.includes(',')) set('name', '节点名称不能包含英文逗号')
  for (const item of value.routes) set('routes', networkError(item, '网段'))
  for (const item of list(options['proxy-networks'])) set('proxy-networks', networkError(item, '网段'))
  for (const key of ['peers', 'listeners', 'mapped-listeners']) for (const item of list(options[key])) set(key, uriError(item, '地址'))
  for (const item of list(options['exit-nodes'])) set('exit-nodes', isIPv4(item) ? '' : `「${item}」应填写出口节点的虚拟 IPv4`)
  const ipv4 = typeof options.ipv4 === 'string' ? options.ipv4.trim() : ''
  if (ipv4) {
    const [address, prefix] = ipv4.split('/')
    if (!isIPv4(address) || (prefix !== undefined && !(/^\d+$/.test(prefix) && Number(prefix) <= 32))) set('ipv4', '应类似 10.144.144.10 或 10.144.144.10/24')
  }
  if (typeof options.mtu === 'number' && (options.mtu < 576 || options.mtu > 9000)) set('mtu', 'MTU 需在 576 ~ 9000 之间')
  if (value.keepalive.interval < 10 || value.keepalive.interval > 86400 || !Number.isInteger(value.keepalive.interval)) set('keepalive.interval', '需为 10 ~ 86400 秒的整数')
  if (value.keepalive.url && !/^https?:\/\/\S+$/.test(value.keepalive.url)) set('keepalive.url', '应为 http:// 或 https:// 开头的网址')
  if (!value.enabled) return issues
  if (value.keepalive.enabled && !value.keepalive.url.trim()) set('keepalive.url', '开启后请填写检测地址')
  if (!String(options['network-name'] ?? '').trim()) set('network-name', '请填写网络名')
  const listeners = list(options.listeners)
  if (listeners.length && options['no-listener'] === true) set('no-listener', '填写了监听地址时不能开启')
  if (!list(options.peers).length && !listeners.length && options['no-listener'] !== false) set('peers', '不监听端口时至少填写一个入网节点')
  if (String(options['local-public-key'] ?? '').trim() && !String(options['local-private-key'] ?? '').trim()) set('local-private-key', '填写本机公钥时也要填写私钥')
  return issues
}
