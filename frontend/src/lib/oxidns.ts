import { optionCopy } from './optionCopy'

export interface OxidnsField {
  path: string
  label: string
  type?: 'text' | 'number' | 'password' | 'switch' | 'tls' | 'textarea' | 'list' | 'numbers' | 'upstreams' | 'lines'
  when?: string
  hint?: string
  help?: string
  documentation?: { href: string; label: string }
  placeholder?: string
  min?: number
  max?: number
  rows?: number
  wide?: boolean
  emptyHint?: string
  options?: Array<{ value: string; label: string }>
}

type FieldCopy = ReturnType<typeof optionCopy>
const number = (path: string, label: string, min = 1, copy?: FieldCopy): OxidnsField => ({ path, label, type: 'number', min, ...copy })
const toggle = (path: string, label: string, copy: FieldCopy): OxidnsField => ({ path, label, type: 'switch', ...copy })
const text = (path: string, label: string, copy: FieldCopy): OxidnsField => ({ path, label, ...copy })
const select = (path: string, label: string, values: string[], copy: FieldCopy): OxidnsField => ({ path, label, ...copy, options: values.map(value => ({ value, label: value })) })

// Help follows the v1.6.1 runtime and ConfigFlow's generated execution paths.
const upstreamConcurrencyCopy = optionCopy('同时询问多个上游，以更多请求换取速度。', '已配置多个 DNS 上游，希望更快获得结果时调整。', '每次查询最多同时请求 32 个上游，且不超过已配置数量。增大可增加先拿到结果的机会，也会增加 DNS 请求量。')

export const coreFields: OxidnsField[] = [
  text('listen', 'DNS 监听地址', optionCopy('设置手机和电脑可访问的 DNS 服务地址。', '需要让本机或局域网设备使用 OxiDNS 时设置。', '填写 IP 和端口，UDP 与 TCP 共用此地址。例如 0.0.0.0:53 接收所有 IPv4 网卡上的查询，客户端需能访问该端口。')),
  { ...number('worker_threads', '工作线程数', 1, optionCopy('日常可用两个线程，高负载时再尝试增加。', '多台设备同时查询、需要分担处理负载时调整。', '已验证配置使用 2 个线程。增加线程可分担异步任务，也会增加调度开销，并非越多越快；必须大于 0。')), max: 256 },
  toggle('tcp_enabled', '启用 TCP DNS', optionCopy('兼容需要 TCP 的设备和较大的查询响应。', '客户端使用 TCP，或 UDP 响应过大被截断时需要。', '开启后在同一地址接收 TCP 查询，允许客户端改用 TCP 重试。关闭后只提供 UDP DNS，依赖 TCP 的查询可能无法完成。')),
  toggle('prefer_ipv4', '优先 IPv4', optionCopy('网站有 IPv4 地址时，优先让设备使用它。', '希望双栈网站优先使用 IPv4，而非 IPv6 时开启。', '同一域名有 A 记录时，对 AAAA 查询返回空成功响应，引导设备选用 IPv4。尚不清楚 IPv4 是否可用时，可能额外查询 A 记录。')),
  toggle('metrics_enabled', '轻量指标', optionCopy('查看请求量和耗时，无需保存逐条查询。', '需要在 API 或 WebUI 查看 DNS 运行概况时开启。', '收集请求数量、响应耗时等汇总指标，不保存每次查询的历史。若要追查某个域名的请求过程，还需单独开启查询记录。')),
  toggle('aggregate_rules', '合并相邻同策略规则', optionCopy('合并相邻同类规则，保持原有分流顺序。', '规则较多、希望减少生成的规则插件数量时开启。', '仅合并类型相同、位置相邻且转发目标一致的规则，保留直连和代理规则的原始优先级，不会把分散的同策略规则全部合并。')),
  { path: 'block_qtypes', label: '屏蔽查询类型', type: 'numbers', wide: true, ...optionCopy('只屏蔽选定查询类型，留空则全部放行。', '明确需要阻止某类 DNS 查询时设置，不确定可留空。', '对选定类型返回空成功响应，不是查询错误。填写数字代码，如 65 为 HTTPS、255 为 ANY；删除全部条目即可停止屏蔽。'), documentation: { href: 'https://oxidns.org/dns-codes/#qtype-%E6%9F%A5%E8%AF%A2%E7%B1%BB%E5%9E%8B', label: '查询类型表' } },
  { path: 'default_forward', label: '未匹配时转发到', ...optionCopy('未命中任何分流规则时，使用所选 DNS。', '为没有专门规则的网站指定默认查询方式。', '域名规则和动态域名都未匹配时，直接使用此上游，不先向直连 DNS 探测。选择代理 DNS 后，是否启用备用查询仍由备用 DNS 设置决定。'), options: [{ value: 'forward_remote', label: '代理 DNS' }, { value: 'forward_local', label: '直连 DNS' }] }
]

export const upstreamFields: OxidnsField[] = [
  { path: 'local_dns', label: '直连 DNS', type: 'upstreams', wide: true, placeholder: 'udp://223.5.5.5:53', emptyHint: '留空使用默认直连上游。', ...optionCopy('为已选直连 DNS 的域名查询网站地址。', '需要设置已选直连 DNS 的域名所使用的查询服务器。', '留空采用 223.5.5.5 和 119.29.29.29 的 UDP DNS。可逐条添加地址、展开连接参数，也可切换 YAML 编辑。') },
  number('local_concurrent', '直连并发数', 1, upstreamConcurrencyCopy),
  { path: 'remote_dns', label: '代理 DNS / 外部 Fake-IP 上游', type: 'upstreams', wide: true, placeholder: 'udp://192.0.2.53:1053', emptyHint: '留空使用默认代理上游。', ...optionCopy('设置代理分流所用 DNS，临时地址需外部提供。', '需代理分流时设置；临时地址（Fake-IP）须由外部上游提供。', '留空用 Cloudflare / Google DoH，名称不代表自动走代理。域名上游可设 bootstrap 或 dial_addr，避免循环解析。') },
  number('remote_concurrent', '代理并发数', 1, upstreamConcurrencyCopy),
  number('remote_ttl', '代理响应 TTL（秒）', 0, optionCopy('控制设备缓存代理查询结果的时间长短。', '希望缩短或固定客户端缓存代理 DNS 结果的时长时设置。', '将代理 DNS 分支的响应 TTL 固定为此秒数；0 保留上游原值。只修改该分支的响应，备用 DNS 的响应不会被改写。')),
  { path: 'fallback_dns', label: '备用 DNS', type: 'upstreams', wide: true, emptyHint: '未配置备用地址，不使用代理 DNS 回退；直连 DNS 不变。', ...optionCopy('代理 DNS 迟迟无响应时，尝试备用查询。', '担心代理 DNS 超时或执行失败时，添加备用上游。', '添加有效地址即启用，删完即关闭，不继承直连 DNS。已收到 SERVFAIL、NXDOMAIN 等响应时，不保证触发备用；直连查询不受影响。') },
  number('fallback_concurrent', '备用并发数', 1, optionCopy('启用备用查询后，控制同时询问的上游数。', '已配置备用 DNS，且希望多个备用上游同时查询时调整。', '并发最多 32 个，且不超过已配置上游数量。增大可增加先得到结果的机会，也会增加请求量；未启用备用查询时不生效。')),
  number('fallback_threshold', '触发备用等待时间（毫秒）', 1, optionCopy('主查询等待过久后，让备用 DNS 参与查询。', '已启用代理 DNS 的备用查询，希望控制等待时长时设置。', '超过此时长让备用参与竞争；主分支执行失败或无响应时可提前启动。500 毫秒为半秒，已有 DNS 错误响应不一定触发备用。')),
  toggle('always_standby', '同时请求备用上游', optionCopy('主备同时查询，以增加请求换取更快接替。', '已启用备用 DNS，希望主查询变慢时更快取得备用结果。', '开启后主备同时查询，但达到等待阈值或主分支失败后才能采用备用结果；关闭时按需启动备用。两种方式都不会改变直连 DNS。'))
]

export const cacheFields: OxidnsField[] = [
  toggle('cache_enabled', '启用主缓存', optionCopy('重复访问直连网站时，直接复用查询结果。', '希望减少直连网站的重复查询、加快 DNS 响应时开启。', '保存直连分支的 DNS 响应，命中后直接返回。此主缓存不用于代理或 Fake-IP 分支，关闭它不会关闭其他独立缓存。')),
  number('cache_size', '主缓存条目数', 1, optionCopy('保存更多直连查询结果，同时占用更多内存。', '经常访问的域名较多，且设备有足够内存时可增大。', '设置主缓存最多保存的查询条目数，单位是条而非字节。更大的容量能容纳更多结果，但不保证每次查询都命中缓存。')),
  number('cache_lazy_ttl', 'Lazy cache 总保留时长（秒）', 0, optionCopy('允许先用过期结果应答，再在后台刷新。', '希望减少缓存刚过期时的等待，可以设置大于 0 的秒数。', '3600 表示从写入起至少保留 3600 秒（1 小时），并非原 TTL 加 3600 秒；原 TTL 更长时沿用原值。过期命中后后台刷新，0 仅关闭此功能。')),
  toggle('cache_dump_enabled', '缓存持久化', optionCopy('将主缓存保存到磁盘，供服务重启后恢复。', '希望重启后继续使用主缓存，且能接受额外写盘时开启。', '定期保存缓存快照；v1.6.1 恢复时不会扣除停机时长，因此结果可能偏旧。默认关闭，只在主缓存同时启用时有意义。')),
  text('cache_dump_file', '缓存文件', optionCopy('指定缓存快照位置，使用工作目录内路径。', '已开启主缓存持久化，需要指定保存位置时填写。', '路径以 OxiDNS 工作目录为起点，如 ./state/cache.dump。只能使用相对路径，不能填写绝对路径或包含 ../ 的上级目录路径。')),
  number('cache_dump_interval', '持久化间隔（秒）', 1, optionCopy('控制缓存写盘频率，间隔越短写入越频繁。', '主缓存和缓存持久化均已开启时，此设置才生效。', '按此秒数定期将主缓存保存到磁盘。间隔较短可减少意外停止时丢失的缓存，但会增加磁盘写入；不会改变 DNS 响应的有效期。'))
]

// Shared by the gateway wizard switch and the settings feature group.
export const queryRecorderCopy = optionCopy('在面板查看每条查询，会持续写入磁盘。', '默认关闭；需要在 DNS 面板「查询记录」中追查域名结果、处理路径和耗时时开启。', '历史保存到 state/ 下的 SQLite，默认保留 7 天并持续写盘，闪存或 SD 卡设备请谨慎。运行概览不依赖它。')

export const apiFields: OxidnsField[] = [
  toggle('api.enabled', '启用 API / WebUI', optionCopy('DNS 面板和规则更新依赖它，建议保持开启。', '默认开启；ConfigFlow 的 DNS 面板、规则更新和发布热重载都通过它连接核心。', '关闭后面板无法读取数据，发布只能重启核心。填写网页目录后，可在同一端口访问 WebUI。')),
  { ...text('api.listen', 'API 监听地址', optionCopy('默认仅本机访问，开放局域网时建议设账号。', '需要从其他设备访问 API 或 WebUI 时。', '127.0.0.1:9199 仅本机可用；0.0.0.0:9199 允许局域网 IPv4 访问，建议同时设置账号。Docker 桥接网络还需映射端口。')), when: 'api.enabled' },
]

// Credentials are optional; both empty means the API answers without authentication.
export const apiAuthFields: OxidnsField[] = [
  { ...text('api.username', '用户名', optionCopy('可选，与密码一起设置后访问需要登录。', 'API 开放给局域网，或需限制访问者时填写。', 'API 和 WebUI 共用 HTTP Basic 认证，用户名和密码须同时填写，都留空则不验证身份。用户名不能含英文冒号，DNS 面板会自动使用。')), when: 'api.enabled' },
  { path: 'api.password', label: '密码', type: 'password', ...optionCopy('与用户名配套使用，分享配置前请移除。', '已填写用户名，需要为管理入口启用登录认证时填写。', 'API 和 WebUI 共用此密码，不能含 ${ 或控制字符。密码会写入生成的运行配置，分享或导出配置给他人前应先移除。'), when: 'api.enabled' },
]

export const apiExtraFields: OxidnsField[] = [
  { ...text('api.webui_root', 'WebUI 静态文件目录', optionCopy('填写网页文件所在目录，留空只提供 API。', '已在 OxiDNS 运行设备上准备 WebUI 文件时填写。', '目录位于运行设备，相对路径以工作目录为准。Docker 镜像通常用 /usr/share/oxidns/webui；留空只启用 API。')), wide: true, when: 'api.enabled' },
  { ...text('api.ssl.cert', 'API TLS 证书路径', optionCopy('使用 HTTPS 管理时，与私钥路径一起填写。', '只通过浏览器或其他工具以 HTTPS 访问 API 时设置。', '填写运行环境中可读取的证书路径，并配置匹配私钥；两项都留空使用 HTTP。DNS 面板暂不支持 HTTPS，启用后面板和规则更新将无法连接。')), when: 'api.enabled' },
  { path: 'api.ssl.key', label: 'API TLS 私钥路径', type: 'password', ...optionCopy('填写证书配套私钥的文件路径，非密钥内容。', '已经配置 API TLS 证书，需要启用 HTTPS 时填写。', '应填与证书匹配的私钥文件路径，而非粘贴密钥内容。文件必须存在于 OxiDNS 运行环境，且服务进程有权限读取。'), when: 'api.enabled' },
]

export const logFields: OxidnsField[] = [
  select('log.level', '日志级别', ['off', 'error', 'warn', 'info', 'debug', 'trace'], optionCopy('日常使用 info，排查问题时提高详细程度。', '按日常运行或排查问题的需要，选择记录详细程度。', '从 error 到 trace 记录逐渐增多；off 关闭日志。通常用 info，排障时可临时切换 debug 或 trace，之后调回以减少日志量。')),
  text('log.file', '日志文件', optionCopy('需要单独保存日志文件时填写，留空也可用。', '希望在服务日志之外另存一份日志文件时填写。', '留空仅输出到服务日志；填写后同时写入标准输出和指定文件。路径位于 OxiDNS 运行环境，服务进程须有写入权限。')),
  select('log.rotation', '日志轮转', ['never', 'minutely', 'hourly', 'daily', 'weekly'], optionCopy('按时间分开保存日志，便于查阅和清理。', '已设置日志文件，希望按时间拆分历史记录时使用。', 'never 不拆分；其余选项分别按分钟、小时、天、周生成新文件。此设置只作用于日志文件，不改变服务标准输出中的日志。')),
  number('log.max_files', '保留日志文件数', 0, optionCopy('控制历史日志数量，填零则不自动删除。', '已设置日志文件并启用按时间轮转时生效。', '轮转时最多保留此数量的历史文件，超过后清理较旧文件。0 表示不自动删除，需要自行留意磁盘空间；不会限制单个文件大小。'))
]

const rosFields = (name: string): OxidnsField[] => [
  text(`${name}.args.address`, 'RouterOS API 地址', optionCopy('填写 OxiDNS 可访问的路由器 API 地址。', '使用 OxiDNS 联动 RouterOS，且已开启路由器 API 时填写。', '格式如 192.168.0.1:8728。明文 API 通常用 8728，TLS 通常用 8729；实际端口需与路由器设置一致。')),
  text(`${name}.args.username`, '用户名', optionCopy('使用有权限维护目标列表或路由的账号。', 'RouterOS 已提供 API 访问，并准备好授权账号。', '账号须能读取和维护目标地址列表或路由。OxiDNS 使用此账号连接路由器，普通只读账号可能可以连接，但无法完成写入操作。')),
  { path: `${name}.args.password`, label: '密码', type: 'password', ...optionCopy('填写路由器 API 密码，生成配置会包含它。', '与上方 RouterOS 用户名配套，用于建立 API 连接。', '连接和断线重连都会使用此密码，并将它保存在生成的运行配置中。分享配置前移除凭据，修改路由器密码后也需同步更新这里。') },
  { path: `${name}.args.tls`, label: '使用 TLS', type: 'tls', ...optionCopy('路由器已启用 API-SSL 时，使用加密连接。', 'RouterOS 已开启 API-SSL，并可访问对应端口时开启。', '通过 TLS 加密 API 连接，管理地址需填写实际 TLS API 端口。关闭后改用明文 API；仅开启本选项不会替路由器启用服务。') },
  { ...text(`${name}.args.tls.server_name`, 'TLS 服务器名称', optionCopy('证书使用域名、连接使用 IP 时填写域名。', '使用 TLS，以 IP 连接但证书签发给域名时填写。', '此名称用于核对远端证书身份，不会修改 API 的实际连接地址。留空时从 API 地址推断；填写与路由器证书一致的名称即可。')), when: `${name}.args.tls` },
  { ...text(`${name}.args.tls.ca`, 'TLS CA 文件路径', optionCopy('使用私有证书时填写 CA，留空用系统证书。', '已启用 TLS，路由器证书不受系统信任时填写。', '指定签发该证书的 CA 文件路径，让 OxiDNS 能验证路由器身份。留空使用系统证书；配置 CA 后不能同时开启跳过证书验证。')), when: `${name}.args.tls` },
  { ...toggle(`${name}.args.tls.insecure`, '跳过 TLS 证书验证', optionCopy('仅排查证书问题时使用，日常建议关闭。', '已启用 TLS，需要临时排查证书验证问题时使用。', '开启后连接仍加密，但不核对远端证书身份，无法确认连接的就是目标路由器。不能与自定义 CA 同时使用，排查完成后应关闭。')), when: `${name}.args.tls` },
  toggle(`${name}.args.async`, '异步写入', optionCopy('在后台更新路由器，减少 DNS 查询等待。', '希望 DNS 查询无需等待路由器写入尝试完成时开启。', '开启后先提交任务，再由后台写入 RouterOS。关闭时等待一次写入尝试，可能增加 DNS 响应延迟；开启不代表写入已立即完成。')),
  number(`${name}.args.min_ttl`, '最小 TTL（秒）', 0, optionCopy('避免动态条目过快到期，填零保留原下限。', '希望 RouterOS 动态条目至少保留一段时间时设置。', '将较短的 DNS 有效期提升到此秒数；0 表示不提高原值。最小值不能大于最大 TTL，这里只控制路由器条目的租约时长。')),
  number(`${name}.args.max_ttl`, '最大 TTL（秒）', 0, optionCopy('限制动态条目寿命，填零会让租约立即到期。', '希望 RouterOS 动态条目及时过期时，填写正数秒数。', '将过长的 DNS 有效期截短到此值。0 会让动态租约立即到期，不表示永久保留；数值应不小于最小 TTL。')),
  number(`${name}.args.queue_capacity`, '队列容量', 1, optionCopy('缓存待写入 IP，高峰超过容量会丢弃新增项。', '查询高峰时 RouterOS 写入跟不上，可按资源增加容量。', '入口队列和重试积压分别限制不同 IP 数，同一 IP 会合并。满载会丢弃新增 IP，避免阻塞 DNS；更大容量也会占用更多资源。')),
  text(`${name}.args.comment_prefix`, '注释前缀', optionCopy('标记自动创建的条目，供后续识别和清理。', '需要区分 OxiDNS 管理的条目和手工条目时设置。', '此前缀会写入 RouterOS 条目注释，供后续维护和清理使用。不能包含分号或等号；不要手工更改受管条目的归属注释。')),
  toggle(`${name}.args.cleanup_on_shutdown`, '正常停止时清理临时条目', optionCopy('正常停机时清理自动条目，关闭则保留。', '希望 OxiDNS 正常关闭或重载时同步清理路由器条目。', '仅清理本插件管理的条目。关闭后保留 RouterOS 中的已有状态，可减少重启时策略暂时缺失的时间；异常退出不等于正常清理。'))
]

export const featureGroups: Array<{ key: string; title: string; hint: string; help: string; fields: OxidnsField[] }> = [
  { key: 'query_recorder', title: '查询记录', ...queryRecorderCopy, fields: [
    text('query_recorder.args.path', '数据库路径', optionCopy('将查询历史保存在 state 目录下的文件中。', '开启查询记录，需要指定数据库保存位置时填写。', '使用 state/ 下的相对路径保存 SQLite 文件。此目录中的运行数据会在部署时保留，避免更新配置后丢失查询历史。')),
    number('query_recorder.args.retention_days', '保留天数', 1, optionCopy('只保留最近指定天数的查询，定期清理旧记录。', '查询记录已开启，需要控制历史保存范围时设置。', '超过此天数的记录由定时任务清理，可能等到下一轮才删除。删除后数据库文件不一定立即缩小，但释放的空间可以复用。')),
    number('query_recorder.args.queue_size', '写入队列', 1, optionCopy('缓冲等待写入的查询记录，应对请求高峰。', '短时间查询较多，希望缓冲等待写入的记录时调整。', '设置后台数据库写入前最多暂存的记录数量。更大容量能容纳更多待写记录，也可能占用更多内存；它不是历史记录的保留总量。')),
    number('query_recorder.args.batch_size', '每批条数', 1, optionCopy('多条记录一起写入，减少数据库写入次数。', '查询量较大，希望减少数据库频繁提交时调整。', '指定每批写入 SQLite 的记录数，较大批次可减少提交次数。新记录不一定立即可见，未满一批时还会按刷新间隔提交。')),
    number('query_recorder.args.flush_interval_ms', '刷新间隔（毫秒）', 1, optionCopy('更短间隔能更快看到记录，也会增加写入。', '希望调整查询历史从内存写入数据库的及时程度。', '后台按此毫秒数定期提交记录，即使尚未凑满一批。缩短可更快看到新记录，也会增加磁盘写入频率；不是 DNS 查询超时。')),
    number('query_recorder.args.memory_tail', '内存最近记录数', 0, optionCopy('为实时查询回放保留最近记录，会占用内存。', '使用实时查询流，希望能回放最近记录时调整。', '设置内存中保留的最近记录数，不改变数据库历史的保留天数。数量越多越占内存；v1.6.1 中填 0 仍至少保留 1 条。')),
    number('query_recorder.args.cleanup_interval_hours', '清理间隔（小时）', 1, optionCopy('定期删除过期记录，间隔以小时为单位。', '已设置查询历史保留天数，需要控制清理频率。', '按此小时数检查并删除超出保留范围的记录。因此记录到期后，可能继续保留到下一次清理；此处不改变保留天数本身。')),
    number('query_recorder.args.reader_concurrency', '读取并发数', 1, optionCopy('限制同时查阅历史和统计的任务，节省资源。', '多人同时查看 WebUI 或调用历史统计 API 时调整。', '限制同时运行的 SQLite 读取任务数量，以控制线程和内存开销。增大可允许更多任务同时读取，也会增加设备负担。'))
  ] },
  { key: 'ip_selector', title: 'IP 优选（实验性）', ...optionCopy('网站有多个真实 IP 时，尝试优先使用更快地址。', '希望优化直连网站的地址选择，且愿意试用实验功能。', '仅探测直连真实 IP，代理和 Fake-IP 分支不参与。后台模式配合保留数量 0 只调整地址顺序；前台模式可能增加查询等待。'), fields: [
    select('ip_selector.args.selection_mode', '探测模式', ['background', 'first_success', 'best_within_budget'], optionCopy('选择后台测速，或等待测速后再返回地址。', 'IP 优选已启用，按能接受的查询等待时间选择。', 'background 先返回、后台测；first_success 用首个成功 IP；best_within_budget 在限时内比延迟。后两项会增加等待。')),
    number('ip_selector.args.top_n', '保留 IP 数量', 0, optionCopy('限制优选后返回的地址数，填零保留全部。', 'IP 优选已启用，希望控制返回给客户端的 IP 数量。', '填正数时只保留优选后的指定数量，可能减少候选地址。0 表示保留全部地址，仅按探测结果排序，不表示关闭 IP 优选。')),
    { path: 'ip_selector.args.probe_methods', label: '探测方法', type: 'list', wide: true, ...optionCopy('按设备支持选择测速方式，失败仍保留解析。', 'IP 优选已启用，按设备支持的方式填写。', '可用 tcp:443、tcp:80、ping 或 none。TCP 测连接耗时；ping 需要系统支持和权限，无法执行时保留 DNS 结果。') },
    select('ip_selector.args.dnssec_policy', 'DNSSEC 策略', ['reorder_only', 'skip'], optionCopy('对带签名的查询，选择只排序或跳过优选。', '请求带 DNSSEC DO 标记，或响应包含相关签名时生效。', 'reorder_only 只调整地址顺序，不裁剪；skip 完全跳过优选。两种方式都用于保护签名记录完整性。')),
    number('ip_selector.args.probe_stagger', '探测间隔（毫秒）', 0, optionCopy('错开同一地址的多种测速，填零则同时开始。', '同一 IP 配置了多种探测方式，希望错开启动时设置。', '每种探测方法之间等待此毫秒数；0 表示无需等待。它控制一次测速内的方法启动间隔，不是隔多久重新测一次地址。')),
    number('ip_selector.args.probe_timeout', '探测超时（毫秒）', 1, optionCopy('限制单次测速的等待时间，超时记为失败。', '希望避免某个 IP 的单次探测长时间等待时设置。', '每次探测最多等待此毫秒数，超时记为该次探测失败。数值过短可能把慢但可用的 IP 判为失败，可根据实际网络延迟调整。')),
    number('ip_selector.args.max_wait', '等待预算（毫秒）', 1, optionCopy('控制等待测速的总时长，避免查询拖太久。', '选择 first_success 或 best_within_budget 前台模式时使用。', '限制一次 IP 优选最多等待多久，控制额外的查询延迟。这是整体等待预算，与单次探测超时分别设置。')),
    number('ip_selector.args.max_parallel_probes', '最大探测并发', 1, optionCopy('限制同时测速的连接数，避免设备负载过高。', '许多未缓存的 IP 同时需要测速，或设备资源有限时调整。', '限制整个 IP 优选插件同时进行的探测数量，而非每次查询的数量。更高并发可能加快探测，也会增加连接和资源开销。')),
    toggle('ip_selector.args.cache.enabled', '缓存探测结果', optionCopy('复用最近的测速评分，减少反复探测地址。', '相同 IP 会被反复查询，希望减少重复测速时开启。', '保存并复用 IP 探测评分，减少连接开销。缓存的是测速结果，与保存 DNS 查询响应的主缓存独立，关闭主缓存不会关闭此项。')),
    number('ip_selector.args.cache.size', '探测结果缓存条数', 1, optionCopy('保存更多地址的测速评分，同时多占用内存。', '已开启探测结果缓存，且待比较的目标 IP 较多时调整。', '设置测速评分缓存的目标条目容量。容量增加可减少重复探测，但也会增加内存占用；这里不控制 DNS 主缓存大小。')),
    number('ip_selector.args.cache.ttl', '探测成功缓存（秒）', 1, optionCopy('复用成功测速的时长越长，重新探测越少。', '已开启探测结果缓存，需要平衡测速次数和更新速度。', '成功评分在此秒数内可复用。时间较长能减少探测，但网络发生变化后更新较慢；缩短可更快重新评估 IP，也会增加测速请求。')),
    number('ip_selector.args.cache.failure_ttl', '探测失败缓存（秒）', 1, optionCopy('控制多久后重试失败地址，越短恢复越及时。', '已开启探测结果缓存，希望控制失败 IP 的重试速度。', '失败评分保留到期后才会重新探测。较短可更快发现已恢复的地址，但会对持续故障的 IP 重复测速，增加连接开销。'))
  ] },
  { key: 'dynamic_domain_set', title: '动态域名集（实验性）', ...optionCopy('按动态名单分流域名，明确的规则仍优先。', '需要使用已有动态域名，或通过 API 增删域名规则。', '名单保存在 state/，部署时保留。标准流程不再根据查询结果的 IP 自动学习；手工添加和历史已有的域名继续参与匹配。'), fields: [
    text('dynamic_domain_set.args.path', '动态规则文件', optionCopy('在 state 目录保存动态规则，重启后继续使用。', '使用动态域名集，需要保存 API 添加或学习到的规则。', '填写 state/ 下的相对路径。首次没有文件时创建，已有文件会在启动时读取；部署时保留该目录中的运行数据。')),
    { path: 'dynamic_domain_set.route', label: '动态规则转发到', ...optionCopy('选择动态域名使用的 DNS，明确规则仍优先。', '希望指定动态域名集的查询去向时选择。', '命中名单后使用所选 DNS，已有明确的域名规则仍优先。这里只设置动态名单中域名的去向，不会根据查询结果的 IP 增加新域名。'), options: [{ value: 'direct', label: '直连 DNS' }, { value: 'proxy', label: '代理 DNS' }] },
    { path: 'dynamic_domain_set.args.bootstrap_rules', label: '初始规则', type: 'list', wide: true, ...optionCopy('首次创建动态文件时使用，不覆盖已有规则。', '动态规则文件尚不存在，需要预置域名规则时填写。', '仅首次创建文件时写入。full:example.com 只匹配该域名，domain:example.com 还匹配子域名；已有文件不覆盖。') },
  ] },
  { key: 'adguard', title: 'AdGuard DNS 过滤', ...optionCopy('按域名拦截广告，不支持网页内元素过滤。', '希望在 DNS 查询阶段拦截广告或其他域名时使用。', '支持 AdGuard DNS 规则子集，不支持网页元素、URL 路径、dnsrewrite 或 $client。规则文件随完整下载包提供。'), fields: [
    { path: 'adguard.content', label: '自定义规则', type: 'textarea', wide: true, rows: 5, placeholder: '||ads.example.com^\n@@||allowed.example.com^', ...optionCopy('每行填写一条拦截或放行规则，与订阅共用。', '希望在订阅规则之外，自行指定拦截或放行的域名。', '每行一条，如 ||ads.example.com^ 拦截，@@||allowed.example.com^ 放行；自定义与订阅规则一起参与匹配。') },
    { path: 'adguard.urls', label: '规则订阅', type: 'list', wide: true, ...optionCopy('添加过滤规则订阅，按需启用自动更新。', '已有可下载的 AdGuard DNS 过滤规则文件时填写。', '每项一个 HTTP(S) 地址，文件会加入完整下载包。仅添加订阅不代表定时刷新，还需启用原生规则定时更新。') },
    select('adguard.mode', '拦截响应', ['nxdomain', 'nodata', 'null', 'refused'], optionCopy('选择被拦截的查询收到哪种 DNS 应答。', '已启用 DNS 过滤，需要设置命中拦截规则后的返回方式。', 'nxdomain 表示域名不存在，nodata 返回空成功应答，null 对 A / AAAA 返回 0.0.0.0 / ::，refused 拒绝查询。'))
  ] },
  { key: 'ros_address_list', title: 'RouterOS 地址列表', ...optionCopy('将直连网站的真实 IP 同步到路由器列表。', '已有使用地址列表的 RouterOS 策略，并准备好 API 权限。', '仅将直连分支的真实 A / AAAA 地址写入目标列表，供现有策略使用。不会为代理或 Fake-IP 分支写入，也不会自动创建使用列表的策略。'), fields: [
    ...rosFields('ros_address_list'),
    text('ros_address_list.args.address_list4', 'IPv4 地址列表', optionCopy('填写接收直连 IPv4 的列表名，留空则跳过。', 'RouterOS 已有使用该 IPv4 地址列表的策略时填写。', '将直连响应中的 A 记录写入指定 address-list，供路由器现有策略匹配。留空不处理 IPv4，不会替你创建使用该列表的策略。')),
    text('ros_address_list.args.address_list6', 'IPv6 地址列表', optionCopy('填写接收直连 IPv6 的列表名，留空则跳过。', 'RouterOS 已有使用该 IPv6 地址列表的对应策略时填写。', '将直连响应中的 AAAA 记录写入指定 address-list。留空不处理 IPv6；只填写列表名称不会自动创建 IPv6 分流策略。'))
  ] },
  { key: 'ros_route', title: 'RouterOS 主机路由', ...optionCopy('为直连网站的真实 IP 自动维护单地址路由。', '已在 RouterOS 配好路由表和网关，且具备 API 写入权限。', '仅为直连分支的真实 IP 创建主机路由，并维护租约。不会创建路由表、默认路由或路由规则，代理和 Fake-IP 分支不参与。'), fields: [
    ...rosFields('ros_route'),
    text('ros_route.args.routing_table', '已有路由表', optionCopy('选择路由器中已准备好的路由表，写入新路由。', '已在 RouterOS 建好目标路由表及所需策略时填写。', '自动维护的主机路由将写入此表。插件不会创建路由表、路由规则或默认路由，单独填名称无法替代路由器端的前置配置。')),
    text('ros_route.args.gateway4', 'IPv4 网关', optionCopy('填写 IPv4 路由的下一跳，留空则不处理。', '需要为直连响应中的 IPv4 创建主机路由时填写。', '使用 RouterOS 支持的网关表达式，作为 IPv4 路由下一跳。留空会忽略 IPv4；IPv4 和 IPv6 两类网关至少填写一个。')),
    text('ros_route.args.gateway6', 'IPv6 网关', optionCopy('填写 IPv6 路由的下一跳，留空则不处理。', '需要为直连响应中的 IPv6 创建主机路由时填写。', '使用 RouterOS 支持的网关表达式，作为 IPv6 路由下一跳。留空会忽略 IPv6；IPv4 和 IPv6 两类网关至少填写一个。')),
    number('ros_route.args.distance', '路由距离', 1, optionCopy('协调相同目标路由的优先级，数值越小越优先。', '目标地址已有其他路由，需要协调自动路由优先级时设置。', '写入 RouterOS 路由的 distance。仅在目标前缀相同的比较中，较小值通常更优先；不能仅靠此值覆盖更具体的目标路由。'))
  ] },
  { key: 'rule_update', title: '原生规则定时更新', ...optionCopy('定时更新设备规则，关闭后仍可手动更新。', '设备需定时更新规则，且没有外部脚本更新同一目录。', '与 ConfigFlow 规则库自动同步独立；失败文件保留旧版，无整批回滚。Agent 手动更新仅全部下载成功后热重载；关闭仍可手动更新。'), fields: [
    text('rule_update.args.schedule', 'Cron（五段）', optionCopy('指定每天或每周何时更新规则，按所选时区执行。', '已启用原生规则定时更新，需要指定更新时间。', '使用五段 Cron，依次是分、时、日、月、星期。例如 30 3 * * * 表示每天 03:30 更新，实际时间由下方时区决定。')),
    text('rule_update.args.timezone', '时区', optionCopy('指定更新时间所属地区，避免任务错时执行。', '已设置定时更新，需要明确计划时间所属时区。', '填写 IANA 时区名称，如 Asia/Shanghai 表示中国标准时间。Cron 中的时分按此时区解释，不一定与浏览器或服务器显示时间一致。')),
    text('rule_update.args.timeout', '下载超时', optionCopy('限制每个文件的下载等待，总耗时可能更长。', '手动与定时更新共用此设置，需限制单个文件的下载等待。', '填写带单位的时长，如 60s 或 2m。文件按顺序下载，超时按单文件计算；更新多个文件的总耗时可能超过这里设置的时长。')),
    text('rule_update.args.outbound', '出站配置标签', optionCopy('复用已配置的解析器和代理，留空用默认出口。', '手动与定时更新共用；已在完整参数中定义出站配置。', '填写 network.outbound.profiles 中的名称，复用其解析器与代理。留空使用 OxiDNS 默认出站，不会自动创建新的代理配置。')),
    text('rule_update.args.socks5', 'SOCKS5 代理', optionCopy('为规则下载单独指定代理，留空沿用出站配置。', '手动与定时更新共用，需单独指定规则下载代理时填写。', '格式为主机:端口或用户名:密码@主机:端口。填写后覆盖出站配置中的代理；留空继续沿用，不会关闭已配置的出站代理。'))
  ] }
]

export const applyVerifiedOxidnsTemplate = (settings: Record<string, any>) => Object.assign(settings, {
  worker_threads: 2, aggregate_rules: true, metrics_enabled: true,
  local_concurrent: 2, remote_concurrent: 1, fallback_concurrent: 2,
  fallback_threshold: 500, always_standby: false, remote_ttl: 1,
  cache_enabled: true, cache_size: 10240, cache_lazy_ttl: 3600, cache_dump_enabled: false
})

/** MosDNS 支持的单条规则类型；OxiDNS 仅使用下面的域名子集。 */
export const DNS_RULE_TYPES = ['DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD', 'DOMAIN-REGEX', 'IP-CIDR', 'IP-CIDR6']
export const isDnsRuleType = (type: unknown) => typeof type === 'string' && DNS_RULE_TYPES.includes(type)

/** OxiDNS 不再按响应 IP 分类；混合规则集只使用域名部分。 */
export const OXIDNS_RULE_TYPES = ['DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD', 'DOMAIN-REGEX']
export const isOxidnsRuleType = (type: unknown) => typeof type === 'string' && OXIDNS_RULE_TYPES.includes(type)
export const isOxidnsRule = (item: { itemType?: unknown; rule_type?: unknown; behavior?: unknown }) =>
  item.itemType === 'ruleset' ? item.behavior !== 'ipcidr' : isOxidnsRuleType(item.rule_type)
