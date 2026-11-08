import type { BasePath } from './mihomoBaseSettings'
import { CLIENT_FINGERPRINTS } from './mihomoBaseSettings'

/** Help text of a base setting: what it does, the default / recommendation, the effect of a change and the risk. */
export function settingHelp(parts: { what: string; recommend: string; effect: string; risk?: string }): string {
  return [`【作用】\n${parts.what}`, `【默认 / 推荐】\n${parts.recommend}`, `【修改后】\n${parts.effect}`,
    parts.risk ? `【注意】\n${parts.risk}` : ''].filter(Boolean).join('\n\n')
}

export type BaseFieldType = 'bool' | 'int' | 'enum' | 'text' | 'list'
export interface BaseFieldDef {
  path: BasePath
  label: string
  type: BaseFieldType
  help: string
  /** Mihomo's own value when the key is missing. */
  coreDefault?: unknown
  options?: Array<{ value: string; label: string }>
  /** Enum fields that may be removed again. */
  unset?: string
  min?: number
  max?: number
  unit?: string
  placeholder?: string
  wide?: boolean
}

const RESTART = '保存并发布后生效。'

export const allowLanField: BaseFieldDef = {
  path: ['allow-lan'], label: '允许局域网连接代理端口', type: 'bool', coreDefault: false,
  help: settingHelp({
    what: '是否允许局域网里的其他设备连接本页的代理端口（mixed-port 等）。',
    recommend: 'Mihomo 默认关闭，初始模板开启。手机、电脑要手动填写代理地址时开启；只靠 TUN、网关或主路由分流接管，或只在本机使用时可以关闭。',
    effect: `关闭后其他设备连代理端口会被拒绝；TUN 接管的流量、DNS 监听和控制面板不受影响。${RESTART}`,
    risk: '开启后同一网络中的设备都能使用代理，必要时配合防火墙限制来源。',
  }),
}
export const bindAddressField: BaseFieldDef = {
  path: ['bind-address'], label: '代理监听地址（bind-address）', type: 'text', placeholder: '*', coreDefault: '*',
  help: settingHelp({
    what: '允许局域网连接时，代理端口监听哪个地址。只在 allow-lan 开启时生效。',
    recommend: '保持 *（所有地址）。设备有多个网卡、只想在其中一个网卡上提供代理时，填写该网卡的 IP。',
    effect: `只有填写的地址能连上代理端口。留空会删除该项，按 Mihomo 默认 * 处理。${RESTART}`,
    risk: '填写本机不存在的地址时代理端口无法监听，Mihomo 可能启动失败。',
  }),
}

export const PORT_FIELDS: Array<BaseFieldDef & { key: string; suggested: number }> = [
  { key: 'mixed-port', suggested: 7890, path: ['mixed-port'], label: '混合代理端口（mixed-port）', type: 'int', min: 1, max: 65535,
    help: settingHelp({
      what: 'HTTP 和 SOCKS5 共用的代理入口。手机、电脑手动设置代理时填写「设备地址:此端口」。',
      recommend: '一般只保留这一个代理端口即可，常用 7890（初始模板为 7893）。Mihomo 默认不开启。',
      effect: `已手动设置代理的设备要改用新端口；TUN、网关和主路由分流的接管不受影响。${RESTART}`,
      risk: '不能与其他代理端口、DNS 监听、控制面板或本机 Agent 的端口相同，否则 Mihomo 无法启动，保存时会被拒绝。',
    }) },
  { key: 'port', suggested: 7891, path: ['port'], label: 'HTTP 代理端口（port）', type: 'int', min: 1, max: 65535,
    help: settingHelp({
      what: '只接受 HTTP 代理的入口。',
      recommend: '有混合代理端口时通常不需要，可以关闭，少开放一个端口。初始模板为 7890。',
      effect: `关闭后删除 port，使用这个端口的设备需改用混合代理端口。${RESTART}`,
      risk: '端口不能与其他端口相同。',
    }) },
  { key: 'socks-port', suggested: 7892, path: ['socks-port'], label: 'SOCKS5 代理端口（socks-port）', type: 'int', min: 1, max: 65535,
    help: settingHelp({
      what: '只接受 SOCKS5 代理的入口。',
      recommend: '有混合代理端口时通常不需要。初始模板为 7891。',
      effect: `关闭后删除 socks-port。${RESTART}`,
      risk: '端口不能与其他端口相同。',
    }) },
  { key: 'redir-port', suggested: 7893, path: ['redir-port'], label: '透明代理端口（redir-port）', type: 'int', min: 1, max: 65535,
    help: settingHelp({
      what: 'Linux / macOS 透明代理（iptables / pf REDIRECT）的入口，只处理 TCP。',
      recommend: '使用 TUN（包括首次向导的方案和 Linux 网关）时不需要，可以关闭；只有自己编写 REDIRECT 规则时才保留。初始模板为 7892。',
      effect: `关闭后删除 redir-port，不影响 TUN 接管。${RESTART}`,
      risk: '端口不能与其他端口相同。',
    }) },
  { key: 'tproxy-port', suggested: 7894, path: ['tproxy-port'], label: 'TPROXY 端口（tproxy-port）', type: 'int', min: 1, max: 65535,
    help: settingHelp({
      what: 'Linux TPROXY 透明代理入口，处理 TCP 和 UDP。',
      recommend: '使用 TUN 时不需要，可以关闭；只有自己编写 TPROXY 规则时才保留。初始模板为 7894。',
      effect: `关闭后删除 tproxy-port，不影响 TUN 接管。${RESTART}`,
      risk: '端口不能与其他端口相同。',
    }) },
]

export const secretHelp = settingHelp({
  what: 'Mihomo 控制面板（external-controller）的访问密码。在浏览器打开网页面板或调用 API 时需要输入；Agent 在本机用同一密码读取连接和流量。',
  recommend: '8–64 个字母、数字或符号，不含空格，不能使用初始模板的 123456，与首次向导的规则相同。控制面板只监听本机时可以留空。',
  effect: '保存并发布后生效。Agent 会自动改用新密码；控制面板相关设置变化时 Mihomo 会重启而不是热重载，连接会短暂中断，已打开的网页面板需要重新输入密码。',
  risk: '密码以明文保存在基础配置 YAML 里，下方编辑器同样能看到；这里默认隐藏，可点眼睛图标查看。控制面板对局域网开放时，知道密码的人可以切换节点、查看连接。',
})
export const controllerHelp = settingHelp({
  what: '控制面板 API 的监听地址和端口。网页面板、Agent 读取连接与流量、配置热重载都通过它。',
  recommend: '初始模板为「局域网可访问」0.0.0.0:9090，方便在其他设备的浏览器中打开面板；只在设备本机使用或不想暴露时选「仅本机」127.0.0.1。',
  effect: '保存并发布后生效，Mihomo 会重启。Agent 遇到 0.0.0.0 / :: 时改用本机回环地址访问，两种选择都能读取连接和流量。改端口后网页面板地址随之变化。',
  risk: '自定义地址只能是 :: 或 ::1、localhost 等回环地址：监听某个具体局域网 IP 时 Agent 无法热重载配置、无法做 IPv6 检查，保存会被拒绝。端口不能与代理端口、DNS 监听或 Agent 端口相同。对局域网开放时必须设置密码。',
})
export const externalUiHelp = settingHelp({
  what: '让 Mihomo 自己提供网页控制面板，在浏览器访问「http://设备地址:控制面板端口/ui」。开启时写入 external-ui: ui，面板文件下载到 Mihomo 工作目录的 ui 文件夹。',
  recommend: '初始模板开启，使用 zashboard。用其他面板网站或客户端连接 API 时可以关闭。',
  effect: '关闭只删除 external-ui，控制面板 API 仍可用，Agent 不受影响，但浏览器打不开 /ui。保存并发布后生效，Mihomo 会重启。',
  risk: '首次启动需要从下载地址获取面板压缩包；下载失败时网页面板打不开，但不影响代理。',
})
export const externalUiUrlHelp = settingHelp({
  what: '网页面板压缩包（zip）的下载地址。',
  recommend: '初始模板为 zashboard 的 gh-pages 压缩包，也可改用 metacubexd 等面板。',
  effect: '保存并发布后 Mihomo 按新地址下载；已下载的旧面板可能仍然保留，可在面板内手动更新。留空会删除该项。',
  risk: '需为 http(s) 地址；地址无法访问时面板打不开。',
})

export const dnsPresetHelp = settingHelp({
  what: '一次设置 Mihomo 自己解析域名所用的上游。「国内 DoH」与初始模板一致：default-nameserver、direct-nameserver 使用 IP 形式的阿里、腾讯 DoH，nameserver、proxy-server-nameserver 使用 doh.pub、阿里 DoH，并开启 respect-rules。「国内外分开解析」：国内和私有域名（geosite:cn,private）用国内 DoH，其他域名用 Google、Cloudflare DoH，并按规则经代理查询（respect-rules），直连域名也遵循该策略（direct-nameserver-follow-policy）。',
  recommend: '大多数国内网络用「国内 DoH」。境外域名常被污染、节点稳定时可选「国内外分开解析」。当前写法与两种预设都不同时显示「自定义」，可以逐项编辑各列表。',
  effect: '切换预设只改 default-nameserver、nameserver、proxy-server-nameserver、direct-nameserver、respect-rules、direct-nameserver-follow-policy，并删除旧的 fallback、fallback-filter；nameserver-policy 只增删「geosite:cn,private」这一条（追加在最后），你自己写的其他条目（例如 +.115.com）原样保留，并且排在前面优先匹配。保存并发布后生效。',
  risk: '「国内外分开解析」依赖 GeoSite 数据（Mihomo 首次使用时自动下载）和可用节点：节点都不可用时境外域名无法解析。DNS 上游不能指向本机（127.0.0.1、localhost、本机 OxiDNS）或 system，否则会形成解析循环，保存会被拒绝。',
})
export const DNS_LIST_COPY: Record<string, { label: string; help: string }> = {
  'default-nameserver': { label: '引导 DNS（default-nameserver）', help: settingHelp({
    what: '用来解析其他 DNS 上游自身的域名（例如 doh.pub、dns.google）。',
    recommend: '只填写 IP 地址或 IP 形式的 DoH，例如 https://223.5.5.5/dns-query。',
    effect: '每行一个上游，保存并发布后生效。清空会删除该项。',
    risk: '填写域名形式的上游会无法引导；不能指向本机或 system。',
  }) },
  nameserver: { label: '默认 DNS（nameserver）', help: settingHelp({
    what: '没有命中 nameserver-policy 的域名都用这些上游解析。',
    recommend: '「国内 DoH」为 doh.pub、阿里 DoH；「国内外分开解析」为 Google、Cloudflare DoH。',
    effect: '每行一个上游，保存并发布后生效。',
    risk: '启用 DNS 时至少填写一个，不能指向本机或 system。',
  }) },
  'proxy-server-nameserver': { label: '节点域名 DNS（proxy-server-nameserver）', help: settingHelp({
    what: '只用于解析代理节点服务器自己的域名，确保节点能连上。',
    recommend: '使用国内 DoH，例如 doh.pub、阿里 DoH。',
    effect: '每行一个上游，保存并发布后生效。',
    risk: '开启 respect-rules 时必须填写，否则 Mihomo 无法启动。',
  }) },
  'direct-nameserver': { label: '直连 DNS（direct-nameserver）', help: settingHelp({
    what: '规则判定为直连的域名使用这些上游解析。',
    recommend: 'IP 形式的国内 DoH，例如 https://223.5.5.5/dns-query。',
    effect: '每行一个上游，保存并发布后生效；清空后直连域名改用默认 DNS。',
    risk: '不能指向本机或 system。',
  }) },
  fallback: { label: '备用 DNS（fallback）', help: settingHelp({
    what: '旧版的备用上游，与 fallback-filter 配合判断污染。',
    recommend: '新配置不建议使用，改用 nameserver-policy 或「国内外分开解析」。切换到任一预设会删除它。',
    effect: '每行一个上游，保存并发布后生效。清空会删除该项。',
  }) },
}
export const respectRulesHelp = settingHelp({
  what: 'DNS 查询本身也按规则选择代理或直连，例如境外 DoH 经节点查询。',
  recommend: '两种预设都开启。',
  effect: '开启时必须填写 proxy-server-nameserver，否则 Mihomo 无法启动，保存会被拒绝。保存并发布后生效。',
})
export const followPolicyHelp = settingHelp({
  what: '直连域名是否也先匹配 nameserver-policy，再使用直连 DNS。',
  recommend: '「国内 DoH」关闭（与初始模板一致），「国内外分开解析」开启。',
  effect: '开启后命中 geosite:cn 等策略的直连域名使用策略中的上游。保存并发布后生效。',
})

export const fakeIpFilterModeField: BaseFieldDef = {
  path: ['dns', 'fake-ip-filter-mode'], label: 'Fake-IP 过滤方式', type: 'enum', coreDefault: 'blacklist',
  options: [{ value: 'blacklist', label: 'blacklist：列表内返回真实 IP' }, { value: 'whitelist', label: 'whitelist：只有列表内用 Fake-IP' }, { value: 'rule', label: 'rule：列表每行是规则' }],
  help: settingHelp({
    what: '决定下方过滤列表的含义。blacklist：列表中的域名返回真实 IP，其他域名返回 Fake-IP；whitelist：只有列表中的域名返回 Fake-IP；rule：列表每行写一条规则，例如 RULE-SET,名称,real-ip。',
    recommend: 'blacklist（Mihomo 默认，初始模板也是）。',
    effect: '保存并发布后生效；客户端已缓存的地址要等缓存过期或重新联网才会变化。',
    risk: '改为 whitelist 后未列出的域名都返回真实 IP，网关和分流方案中这些连接可能不经过 Mihomo。',
  }),
}
export const fakeIpFilterField: BaseFieldDef = {
  path: ['dns', 'fake-ip-filter'], label: 'Fake-IP 过滤列表', type: 'list', wide: true, placeholder: '*.lan\n+.pool.ntp.org',
  help: settingHelp({
    what: 'blacklist 方式下，这些域名不使用 Fake-IP、直接返回真实 IP。每行一个，支持 +.example.com、*.lan、rule-set:名称、geosite:名称 等写法。',
    recommend: '局域网域名（*.lan、*.local）、对时服务器（+.pool.ntp.org、time.*.com）、联网检测、游戏主机等需要真实地址的服务。',
    effect: '保存并发布后生效。清空会删除该项，改用 Mihomo 自带的默认名单。',
    risk: '写入此项会替换 Mihomo 自带的默认名单。列入的域名返回真实 IP，在网关方案中如果该 IP 不在接管范围，连接会直连。',
  }),
}

export const snifferEnableField: BaseFieldDef = {
  path: ['sniffer', 'enable'], label: '启用域名嗅探', type: 'bool', coreDefault: false,
  help: settingHelp({
    what: '从 HTTP Host、TLS SNI、QUIC 中识别连接的真实域名，让按 IP 进入的连接（例如客户端用了自己的 DNS、Fake-IP 缓存失效）也能匹配域名规则。',
    recommend: 'Mihomo 默认关闭，初始模板开启。网关和分流方案建议开启。',
    effect: '关闭后按 IP 进入的连接无法匹配域名规则，只能按 IP 规则或 MATCH 分流；开启时如果 YAML 中没有 sniff 段，会补上 HTTP（80、8080-8880）、TLS、QUIC（443、8443）的常用端口。保存并发布后生效。',
    risk: '嗅探会增加少量 CPU 开销；个别应用被改写目标域名后可能连不上，可以加入跳过列表。',
  }),
}
export const skipDomainField: BaseFieldDef = {
  path: ['sniffer', 'skip-domain'], label: '跳过嗅探的域名', type: 'list', wide: true, placeholder: '+.push.apple.com',
  help: settingHelp({
    what: '嗅探到这些域名时不改写连接目标，保持原来的连接方式。每行一个，支持 +.example.com 等写法。',
    recommend: '苹果推送、微信、远程控制等被嗅探改写后会异常的服务。初始模板已列出常见项。',
    effect: '保存并发布后生效。清空会删除该项。',
  }),
}
export const forceDomainField: BaseFieldDef = {
  path: ['sniffer', 'force-domain'], label: '强制嗅探的域名', type: 'list', wide: true, placeholder: '+.v2ex.com',
  help: settingHelp({
    what: '无论连接是否已有域名，都对这些域名嗅探并按嗅探结果改写目标。每行一个。',
    recommend: '通常留空或只列少数需要按真实域名分流的网站。',
    effect: '保存并发布后生效。清空会删除该项。',
  }),
}

export const findProcessField: BaseFieldDef = {
  path: ['find-process-mode'], label: '进程匹配（find-process-mode）', type: 'enum', coreDefault: 'strict',
  options: [{ value: 'off', label: 'off：不查找进程' }, { value: 'strict', label: 'strict：有进程规则时查找' }, { value: 'always', label: 'always：每个连接都查找' }],
  help: settingHelp({
    what: '是否查找发起连接的本机进程，供 PROCESS-NAME 等进程规则使用。always 每个连接都查找；strict 只在有进程规则时查找；off 不查找。',
    recommend: '网关、主路由分流等替别的设备转发的机器选 off：客户端的进程不在这台机器上，查找只会白白耗费 CPU。只有在本机上网并使用进程规则时才选 strict。已有配置不会被自动修改。',
    effect: 'off 后进程规则全部不生效。保存并发布后生效。',
  }),
}
export const logLevelField: BaseFieldDef = {
  path: ['log-level'], label: '核心日志级别（log-level）', type: 'enum', coreDefault: 'info',
  options: [{ value: 'silent', label: 'silent：不输出' }, { value: 'error', label: 'error：只记错误' }, { value: 'warning', label: 'warning：错误和警告' }, { value: 'info', label: 'info：常规信息' }, { value: 'debug', label: 'debug：调试详情' }],
  help: settingHelp({
    what: 'Mihomo 核心日志的详细程度，从 silent 到 debug 依次更详细。这不是 ConfigFlow 的操作日志。',
    recommend: 'Mihomo 默认 info，初始模板为 warning。平时用 warning 或 info；排查问题时临时改为 debug，查完改回。',
    effect: '保存并发布后生效。',
    risk: 'debug 日志量很大，长期开启会占用磁盘和 CPU。',
  }),
}

export const ADVANCED_FIELDS: BaseFieldDef[] = [
  { path: ['tun', 'stack'], label: 'TUN 协议栈（tun.stack）', type: 'enum', coreDefault: 'gvisor',
    options: [{ value: 'mixed', label: 'mixed：TCP 用 system，UDP 用 gvisor' }, { value: 'system', label: 'system：系统协议栈' }, { value: 'gvisor', label: 'gvisor：用户态协议栈' }],
    help: settingHelp({
      what: 'TUN 处理连接所用的网络协议栈。system 使用系统协议栈，性能好；gvisor 在用户态实现，兼容性好但 CPU 稍高；mixed 的 TCP 用 system、UDP 用 gvisor。',
      recommend: 'mixed（初始模板、首次向导和 Linux 网关的默认值）。只在 TUN 开启时有意义。',
      effect: '保存并发布后生效，Mihomo 会重新建立 TUN，连接短暂中断。',
      risk: '个别系统上 system 协议栈可能与防火墙冲突导致连不上，遇到问题改回 mixed。',
    }) },
  { path: ['tun', 'mtu'], label: 'TUN MTU（tun.mtu）', type: 'int', min: 576, max: 65535, placeholder: '9000',
    help: settingHelp({
      what: 'TUN 虚拟网卡一次能传输的最大数据包大小。',
      recommend: '通常不填写，使用 Mihomo 默认的 9000。遇到某些游戏、VPN 套娃连接异常时可试 1500。',
      effect: '保存并发布后生效，Mihomo 会重新建立 TUN。清空会删除该项。',
      risk: '设得过小会降低吞吐量。',
    }) },
  { path: ['unified-delay'], label: '统一延迟（unified-delay）', type: 'bool', coreDefault: false,
    help: settingHelp({
      what: '节点测速时扣除握手耗时，让不同协议节点的延迟数值可以直接比较。',
      recommend: 'Mihomo 默认关闭，初始模板开启。',
      effect: '只影响显示的延迟数值和按延迟自动选择的结果，不影响实际转发。保存并发布后生效。',
    }) },
  { path: ['tcp-concurrent'], label: 'TCP 并发连接（tcp-concurrent）', type: 'bool', coreDefault: false,
    help: settingHelp({
      what: '域名解析出多个 IP 时同时尝试连接，使用最先成功的一个。',
      recommend: 'Mihomo 默认关闭，初始模板开启。',
      effect: '可缩短连接时间，连接数略有增加。保存并发布后生效。',
    }) },
  { path: ['global-client-fingerprint'], label: '全局 TLS 指纹（global-client-fingerprint）', type: 'enum', unset: '不设置',
    options: CLIENT_FINGERPRINTS.map(value => ({ value, label: value === 'random' ? 'random：每次随机' : value })),
    help: settingHelp({
      what: '给支持 uTLS 的节点（VLESS、Trojan 等）统一伪装成指定浏览器的 TLS 指纹。节点自己写的 client-fingerprint 优先。',
      recommend: '初始模板为 chrome。机场或节点没有要求时可以不设置。',
      effect: '保存并发布后生效。选「不设置」会删除该项。',
      risk: '不支持 uTLS 的协议会忽略此项；个别节点要求特定指纹，设错可能连不上。',
    }) },
  { path: ['keep-alive-idle'], label: 'TCP 保活空闲时间（keep-alive-idle）', type: 'int', min: 1, max: 86400, unit: '秒',
    help: settingHelp({
      what: 'TCP 连接空闲多少秒后开始发送保活探测。',
      recommend: '初始模板为 600 秒；不填写时使用 Mihomo 默认值。',
      effect: '保存并发布后生效。清空会删除该项。',
      risk: '设得过短会增加流量和耗电，过长则断开的连接要更久才能发现。',
    }) },
  { path: ['keep-alive-interval'], label: 'TCP 保活间隔（keep-alive-interval）', type: 'int', min: 1, max: 86400, unit: '秒',
    help: settingHelp({
      what: '开始保活探测后，每隔多少秒发送一次。',
      recommend: '初始模板为 15 秒；不填写时使用 Mihomo 默认值。',
      effect: '保存并发布后生效。清空会删除该项。',
    }) },
  { path: ['disable-keep-alive'], label: '关闭 TCP 保活（disable-keep-alive）', type: 'bool', coreDefault: false,
    help: settingHelp({
      what: '完全关闭 TCP 保活探测。',
      recommend: '保持关闭（false）。只有手机等对耗电非常敏感的设备才考虑开启。',
      effect: '开启后长时间空闲的连接断开时 Mihomo 不能及时发现。保存并发布后生效。',
    }) },
  { path: ['profile', 'store-selected'], label: '记住手动选择的节点（profile.store-selected）', type: 'bool', coreDefault: true,
    help: settingHelp({
      what: '把在面板中为策略组手动选择的节点保存到缓存文件，Mihomo 重启后保留选择。',
      recommend: '开启（Mihomo 默认和初始模板都开启）。',
      effect: '关闭后每次重启或发布都回到策略组的第一个节点。保存并发布后生效。',
    }) },
  { path: ['profile', 'store-fake-ip'], label: '保留 Fake-IP 映射（profile.store-fake-ip）', type: 'bool', coreDefault: false,
    help: settingHelp({
      what: '重启后保留域名与 Fake-IP 的对应关系，客户端缓存的 Fake-IP 在 Mihomo 重启后仍然有效。',
      recommend: '网关和分流方案建议开启（初始模板开启）。',
      effect: '关闭后 Mihomo 重启时客户端缓存的 Fake-IP 会暂时失效，需等 DNS 缓存过期。保存并发布后生效。',
    }) },
  { path: ['global-ua'], label: '下载使用的 User-Agent（global-ua）', type: 'text', placeholder: '不填写则使用 Mihomo 默认值',
    help: settingHelp({
      what: 'Mihomo 下载订阅、规则集、GeoIP / GeoSite 数据和网页面板时使用的 User-Agent。',
      recommend: '通常不填写。只有某些下载源按 User-Agent 限制访问时才设置。',
      effect: '保存并发布后生效。留空会删除该项。',
      risk: '部分订阅会按 User-Agent 返回不同格式，设错可能导致订阅解析失败。',
    }) },
]

export const MANAGED_FIELDS: Array<{ path: BasePath; label: string; help: string }> = [
  { path: ['dns', 'listen'], label: 'DNS 监听（dns.listen）', help: 'Mihomo DNS 的监听地址。首次向导固定为 127.0.0.1:1053，由同机 OxiDNS 把需要代理的域名转给它；Linux 网关发布时也改写为 127.0.0.1:1053。改动会让 OxiDNS 或网关找不到 Mihomo DNS。' },
  { path: ['dns', 'enhanced-mode'], label: 'DNS 模式（dns.enhanced-mode）', help: '网关、主路由分流和 IPv6 设置都依赖 fake-ip：客户端拿到 Fake-IP 后连接才会进入 Mihomo。' },
  { path: ['tun', 'enable'], label: 'TUN 开关（tun.enable）', help: '决定 Mihomo 是否接管本机和转发来的连接。主路由分流要求保持开启，Linux 网关发布时自动开启。' },
  { path: ['tun', 'auto-route'], label: '自动路由（tun.auto-route）', help: '自动添加把流量送进 TUN 的路由。由向导和 Linux 网关设定。' },
  { path: ['tun', 'auto-redirect'], label: '自动重定向（tun.auto-redirect）', help: '在 Linux 上用 nftables 把转发来的 TCP 交给 Mihomo，网关和主路由分流需要它。' },
  { path: ['tun', 'dns-hijack'], label: 'DNS 劫持（tun.dns-hijack）', help: '把发往 53 端口的 DNS 查询交给 Mihomo。完整 TUN 方案要求包含 any:53；Linux 网关改为空，由 OxiDNS 负责 DNS。' },
]
export const MANAGED_NOTES: Record<'linux_gateway' | 'full_tun' | 'none', string> = {
  linux_gateway: '此配置空间启用了 Linux 网关：发布时这些值会被自动改写为接管所需的值，在这里修改不会生效。',
  full_tun: '此配置空间由首次向导设为「Mihomo + OxiDNS 分流（完整 TUN）」：发布前检查要求 TUN 保持完整，改动会被拒绝。',
  none: '这些键决定流量和 DNS 由谁接管，由首次向导、Linux 网关和 Agent 设定，改错可能让设备断网，所以表单只显示不修改；确实需要调整时请在下方 YAML 中编辑。',
}
