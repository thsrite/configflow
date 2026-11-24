/**
 * Native configuration catalog, audited against the official OxiDNS 1.6.1 docs
 * and deserializers at the commit below. Defaults are suggestions for newly
 * added fields, never an instruction to overwrite omitted/existing values.
 */
import { optionCopy, optionHelp } from './optionCopy';

export interface OxidnsSchema {
  type: 'string' | 'number' | 'boolean' | 'null' | 'array' | 'object';
  label?: string;
  hint?: string;
  help: string;
  properties?: Record<string, OxidnsSchema>;
  items?: OxidnsSchema;
  enum?: Array<string | number>;
  default?: unknown;
  required?: boolean;
  documentation?: string;
  additionalProperties?: OxidnsSchema;
}

export interface OxidnsPluginDefinition {
  type: string;
  category: string;
  label: string;
  hint?: string;
  help: string;
  documentation: string;
  args: OxidnsSchema;
  example?: unknown;
}

export const oxidnsSchemaVersion = '1.6.1';
export const oxidnsSchemaSource = {
  repository: 'https://github.com/svenshi/oxidns',
  commit: 'bd183f69348aa9c78c3b92bddbce28651d2bf66e',
  documentation: 'https://oxidns.org/plugin-reference/overview/',
  checkedAt: '2026-10-16',
};

type SchemaCopy = string | ReturnType<typeof optionCopy>;
const description = (copy: SchemaCopy) => typeof copy === 'string' ? { help: copy } : copy;
const s = (label: string, copy: SchemaCopy, value?: string): OxidnsSchema => ({ type: 'string', label, ...description(copy), ...(value === undefined ? {} : { default: value }) });
const n = (label: string, copy: SchemaCopy, value?: number): OxidnsSchema => ({ type: 'number', label, ...description(copy), ...(value === undefined ? {} : { default: value }) });
const b = (label: string, copy: SchemaCopy, value?: boolean): OxidnsSchema => ({ type: 'boolean', label, ...description(copy), ...(value === undefined ? {} : { default: value }) });
const o = (label: string, copy: SchemaCopy, properties: Record<string, OxidnsSchema>): OxidnsSchema => ({ type: 'object', label, ...description(copy), properties });
const a = (label: string, copy: SchemaCopy, items: OxidnsSchema): OxidnsSchema => ({ type: 'array', label, ...description(copy), items });
const strings = (label: string, copy: SchemaCopy): OxidnsSchema => a(label, copy, s('条目', copy));
const e = (label: string, copy: SchemaCopy, values: Array<string | number>, value?: string | number): OxidnsSchema => ({ type: typeof values[0] === 'number' ? 'number' : 'string', label, ...description(copy), enum: values, ...(value === undefined ? {} : { default: value }) });
const required = (schema: OxidnsSchema): OxidnsSchema => ({ ...schema, required: true });
const map = (label: string, help: string, values: OxidnsSchema = s('值', '为这个名称填写对应的字符串值。')): OxidnsSchema => ({ ...o(label, help, {}), additionalProperties: values });
const none: OxidnsSchema = { type: 'null', label: '无参数', help: '此插件不需要 args，可省略或设置为空值。', default: null };
const stop = b('命中后结束', optionCopy('已有有效结果时结束处理，避免后续动作改写。', '当前插件已产生有效查询结果，希望立即返回时开启。', '开启后不再运行后续执行器；关闭时仍按 sequence 的顺序继续。应结合整条流程决定，避免过早结束而跳过必要的记录或分流动作。'), false);
const files = strings('规则文件', '规则文本文件路径；相对路径基于 OxiDNS 工作目录。可在附加文件中提供部署所需文件。');
const timeout = (value = '5s') => s('超时', '等待上限，使用带单位的时长，例如 500ms、5s、1m。', value);
const outbound = s('出站配置', optionCopy('复用已命名的解析器和代理，留空沿用默认。', '已在共享网络中定义出站配置，需要在当前操作中复用。', '填写 network.outbound.profiles 中的名称，采用对应解析器和连接代理。省略时使用默认出站，不会自动新增或停用代理。'));
const socks5 = s('SOCKS5 代理', optionCopy('为当前操作指定代理，覆盖共用出站的代理。', '需要让当前操作通过独立的 SOCKS5 代理连接时填写。', '格式为 host:port 或 username:password@host:port，IPv6 需加方括号。此局部设置优先于共享出站中的代理。'));
const insecure = b('跳过证书校验', optionCopy('仅临时排查证书问题时开启，日常保持关闭。', '加密连接因证书验证失败，需要临时定位原因时使用。', '开启后不核对远端 TLS 证书身份，连接仍加密，但无法确认对端是预期服务。排查完成后关闭，并修复证书信任或域名设置。'), false);
const errorMode = e('失败处理', optionCopy('决定操作失败后继续查询，还是停止当前处理。', '配置外部操作或学习动作，需要决定失败如何影响查询。', 'continue 记录错误后继续，stop 结束执行链，fail 将错误交给调用方。后两项可能影响 DNS 结果，不确定时可先用 continue。'), ['continue', 'stop', 'fail'], 'continue');
const phase = e('执行阶段', optionCopy('选择先执行当前动作，或等后续处理结束。', '需要将当前动作安排在后续查询处理之前或之后。', 'before 在后续执行器前运行；after 等后续执行器完成后再运行。需要读取最终响应的动作应结合所选阶段，避免在结果产生前执行。'), ['before', 'after'], 'after');
const asyncMode = b('异步执行', optionCopy('把操作交给后台，让当前查询继续向后处理。', '希望 DNS 查询无需等待本次外部操作完成时开启。', '开启后将任务投递到后台队列，当前请求继续执行；关闭时会等待本次操作。异步入队不等于执行成功，需要通过日志确认最终结果。'), true);

/** Also reused by the template's compact upstream cards. */
export const oxidnsUpstreamSchema = o('DNS 上游', '选择协议并设置该上游的解析、代理和连接参数。', {
  addr: required(s('上游地址', '支持 UDP、TCP、DoT、DoH、DoQ 和 DoH3；例如 223.5.5.5:53 或 https://dns.example/dns-query。')),
  tag: s('日志标识', '给此上游单独命名，便于识别日志和并发查询结果。'),
  outbound,
  dial_addr: s('连接 IP', optionCopy('直接连接指定 IP，仍按原域名验证服务身份。', '已知上游 IP，希望跳过域名查询或避免解析循环。', '实际连接使用此 IP，原域名仍用于 SNI、HTTP Host 和证书校验。此项优先于 bootstrap，不会把加密连接的域名身份改成 IP。')),
  port: n('端口', '覆盖协议默认端口；一般直接在上游地址中指定即可。'),
  bootstrap: s('引导 DNS', optionCopy('先用指定 DNS 找到上游服务器，避免循环查询。', '上游地址是域名，需要独立 DNS 查询它的真实 IP。', '必须填写 IP:端口，让上游的域名解析不再依赖自身。若同时设置连接 IP（dial_addr），以连接 IP 为准；纯 IP 上游通常无需填写。')),
  bootstrap_version: e('引导地址族', optionCopy('选择用 IPv4 还是 IPv6 连接域名型上游。', '已设置引导 DNS，需要限定查询上游域名的地址类型。', '4 查询 A 记录以获取 IPv4，6 查询 AAAA 以获取 IPv6。省略时使用 IPv4；此项影响上游引导解析，不会改变客户端原本查询的记录类型。'), [4, 6], 4),
  socks5: { ...socks5, help: optionHelp('当前上游使用支持 SOCKS5 的连接方式；UDP、DoQ、DoH3 不支持。', '填写主机:端口，或用户名:密码@主机:端口，IPv6 需加方括号。局部代理覆盖共享出站中的代理，不会改动其他上游。') },
  idle_timeout: n('连接空闲时间（秒）', '连接池中空闲连接保留多久。', 10),
  max_conns: n('最大连接数', '连接池上限，范围 1–4096。', 64),
  min_conns: n('最小预热连接数', '保持预热的连接数，范围 0–4096，不能超过最大连接数。', 0),
  insecure_skip_verify: insecure,
  timeout: timeout(),
  enable_pipeline: b('连接流水线（enable_pipeline）', optionCopy('上游支持时，在一条连接中同时发送多个查询。', '使用 TCP / DoT，且已确认上游支持查询连接复用。', '让同一连接承载多个同时进行的查询，减少额外连接开销。并非所有上游都兼容，不确定时保持默认关闭；UDP 不受此项影响。'), false),
  enable_http3: b('HTTP/3', optionCopy('上游支持时，为加密 DNS 使用 HTTP/3。', '使用 DoH，且网络和上游均支持 HTTP/3 时开启。', '为 DoH 启用 HTTP/3 传输。地址使用 h3:// 时会强制采用 HTTP/3，不能只靠关闭此开关改回其他传输方式；请同时核对地址。'), false),
  so_mark: n('SO_MARK', optionCopy('已有 Linux 策略路由时，为连接附加匹配标记。', 'Linux 已配置按 socket 标记分流，且服务有设置权限。', '给上游连接附加 SO_MARK，供系统已有策略匹配。单独填写数字不会自动创建策略路由，不确定标记用途时可省略，避免与现有规则冲突。')),
  bind_to_device: s('绑定网卡', optionCopy('多网卡设备可指定出口，需要对应系统权限。', '使用 Linux 多网卡设备，并已确认服务有绑定网卡权限。', '填写实际接口名称，如 eth0，通过 SO_BINDTODEVICE 约束连接出口。仅设置接口名不会替你配置地址和路由；一般无需填写。')),
});
oxidnsUpstreamSchema.documentation = 'https://oxidns.org/plugin-reference/executor/resolution/#forward';

const resolver = o('域名解析器', '填写 nameservers 使用指定 DNS；也可将值类型改为字符串并填写 system，使用系统 DNS。', {
  nameservers: required(a('解析服务器', '指定用于解析出站目标的 DNS；域名型地址必须填写连接 IP，避免循环解析。', o('服务器', '解析服务器地址与实际连接 IP。', {
    addr: required(s('地址', '支持 UDP、TCP、DoT、DoH、DoQ、DoH3，未写协议时使用 UDP。')),
    dial_addr: s('连接 IP', '域名型解析服务器必须填写；地址中的域名仍用于证书验证。'),
  }))),
  ip_version: e('地址族', optionCopy('选择出站目标使用 IPv4 还是 IPv6 地址。', '使用自定义解析器，需要限定出站目标的地址类型。', '4 获取 IPv4，6 获取 IPv6。此项控制解析器为出站连接查询的地址类型，不会把客户端的 AAAA 查询改为 A 查询，应结合实际网络选择。'), [4, 6], 4),
  timeout: timeout(),
  proxy: e('解析代理', optionCopy('选择解析上游域名时直连，还是使用出站代理。', '已设置共享出站代理，需要决定解析器查询是否也走代理。', 'none 直接连接 DNS；profile 让 TCP、DoT、DoH 查询复用当前出站 SOCKS5。其他传输方式不能据此假定支持代理。'), ['none', 'profile'], 'none'),
});

export const oxidnsGlobalSchema = o('全局配置', '原生 OxiDNS 顶层设置。字符串支持环境变量占位符；需要将数字参数写成占位符时可切换值类型。', {
  runtime: o('运行时', '设置运行线程；省略时由 OxiDNS 自动选择。', {
    worker_threads: n('工作线程数', 'Tokio 工作线程数量，必须大于 0；省略时使用系统可用并行度。'),
  }),
  log: o('日志', '日志等级、文件和轮转；不填文件时仅输出到标准输出。', {
    level: e('日志级别', optionCopy('日常保留运行信息，排障时再增加日志细节。', '需要在日志量和排查细节之间取舍时设置。', '通常使用 info；debug 或 trace 可查看更多细节，但增加日志量。warn 和 error 只记录更严重的问题，off 则关闭日志。'), ['off', 'trace', 'debug', 'info', 'warn', 'error'], 'info'),
    file: s('日志文件', '可选输出文件，配置后仍保留标准输出。相对路径基于工作目录。'),
    rotation: o('日志轮转', '按时间分割日志；省略时不轮转。', {
      type: e('轮转周期', optionCopy('按时间拆分历史日志，便于查阅与定期清理。', '已经设置日志文件，并希望按时间生成独立历史文件。', 'never 不拆分；minutely、hourly、daily、weekly 分别按分钟、小时、天、周生成新文件。保留数量由历史文件上限决定。'), ['never', 'minutely', 'hourly', 'daily', 'weekly'], 'never'),
      max_files: n('历史文件上限', '最多保留多少个轮转文件；0 表示不自动清理。', 0),
    }),
  }),
  network: o('共享网络', '管理上游和 HTTP 客户端共用的解析器与代理。', {
    outbound: o('出站配置', '定义可复用的出站 profile。', {
      default: s('默认出站', '必须引用 profiles 中已存在的名称；留空不设置默认代理。'),
      profiles: map('出站列表', '每个条目的键是 profile 名称，可被上游、下载和 HTTP 请求引用。', o('出站', '独立设置目标域名的解析方式和连接代理。', {
        resolver,
        proxy: o('连接代理', '填写 socks5 使用代理；也可切换为字符串填写 none 或 direct。', { socks5 }),
      })),
    }),
  }),
  api: o('管理 API', '提供管理接口和可选 WebUI；API 与 DNS 监听相互独立。', {
    http: o('HTTP 服务', '详细设置监听、TLS、认证、跨域和 WebUI；也支持字符串形式的监听地址。', {
      listen: required(s('监听地址', '例如 127.0.0.1:9199；0.0.0.0 允许局域网 IPv4 访问，:端口 表示双栈监听。')),
      ssl: o('TLS', '证书和私钥必须同时配置；启用客户端证书验证时还需 CA。', {
        cert: required(s('证书文件', 'PEM 服务器证书链路径。')),
        key: required(s('私钥文件', '与证书匹配的 PEM 私钥路径。')),
        client_ca: s('客户端 CA', '用于验证客户端证书的 CA 文件路径。'),
        require_client_cert: b('要求客户端证书', '启用双向 TLS；必须配置 client_ca。', false),
      }),
      auth: o('身份认证', 'Basic 认证需要同时设置非空用户名和密码。', {
        type: e('认证方式', optionCopy('为 API 和网页设置登录认证，使用同一账号。', '需要限制管理 API 或 WebUI 的访问者时设置。', '当前原生 API 使用 HTTP Basic，用户名和密码必须同时非空。它不会自动启用 HTTPS，加密访问还需另外配置 TLS 证书和私钥。'), ['basic'], 'basic'),
        username: required(s('用户名', '管理 API / WebUI 登录用户名。')),
        password: required(s('密码', '管理 API / WebUI 登录密码；可使用 ${env:变量名}。')),
      }),
      cors: o('跨域访问', '限制哪些网页来源可调用 API；省略时根据监听地址推导。', {
        allowed_origins: strings('允许的来源', '按浏览器 Origin 精确匹配，例如 https://dns.example；* 不能与跨域浏览器凭据同时使用。'),
      }),
      webui: o('WebUI', '挂载静态管理页面，API 仍位于 /api/*。', {
        root: required(s('静态文件目录', 'WebUI 文件目录；相对路径基于 OxiDNS 工作目录。')),
        index: s('首页文件', '目录中的默认入口文件名。', 'index.html'),
      }),
    }),
  }),
  include: strings('包含配置', '按顺序递归载入其他 YAML 的 plugins；最多 8 层。相对路径以声明 include 的文件位置为基准。可将文件放入附加文件。'),
});
oxidnsGlobalSchema.documentation = 'https://oxidns.org/configuration/global/';

const plugin = (type: string, category: string, label: string, copy: SchemaCopy, page: string, args: OxidnsSchema, example?: unknown): OxidnsPluginDefinition => ({
  type, category, label, ...description(copy),
  documentation: `https://oxidns.org/plugin-reference/${page}/#${type === 'prefer_ipv4' || type === 'prefer_ipv6' ? 'prefer_ipv4--prefer_ipv6' : type}`,
  args,
  ...(example === undefined ? {} : { example }),
});
const serverEntry = required(s('入口执行器', '填写已定义的执行器 tag，通常为主 sequence 的名称，不加 $。'));
const listen = required(s('监听地址', '支持 IP:端口、[IPv6]:端口、:端口；:端口 监听双栈。'));
const cert = s('TLS 证书', 'PEM 证书链文件路径，必须与私钥配套。');
const key = s('TLS 私钥', 'PEM 私钥文件路径，必须与证书配套。');
const probeCache = o('探测缓存', '缓存 IP 测速结果，减少重复探测。', {
  enabled: b('启用缓存', '保存探测评分供后续请求复用。', true),
  size: n('缓存容量', '探测结果缓存的条目容量。', 4096),
  ttl: n('成功缓存（秒）', '成功探测结果保留时间。', 3600),
  failure_ttl: n('失败缓存（秒）', '失败探测结果保留时间。', 60),
});
const preferArgs = o('双栈优选参数', '偏好地址族有答案时抑制另一地址族；内部探针可使用独立执行器。', {
  probe_executor: s('探针执行器', '只处理偏好类型内部探针的执行器 tag，不加 $；不要指回自身导致循环。'),
  cache: b('探测结果缓存', '缓存偏好地址族是否存在，避免重复探针。', true),
  cache_ttl: n('探测缓存（秒）', '保存地址族存在状态的时长。', 3600),
});
const rosShared: Record<string, OxidnsSchema> = {
  address: required(s('RouterOS 地址', 'RouterOS API 的 host:port；通常明文为 8728，TLS 为 8729。')),
  username: required(s('用户名', '需要具备读取和维护目标地址列表或路由表的权限。')),
  password: required(s('密码', 'RouterOS API 登录密码，也可使用环境变量占位符。')),
  tls: o('API TLS', '添加此对象启用 API-SSL。', {
    server_name: s('证书服务器名', '覆盖由连接地址推导的证书主机名。'),
    ca: s('CA 文件', '私有 CA 或自签名证书的 PEM 文件路径。'),
    insecure: b('跳过校验', '关闭证书校验，不能与自定义 CA 同时使用。', false),
  }),
  connect_timeout: n('连接超时（秒）', '建立 API 连接的等待上限，必须大于 0。', 5),
  send_timeout: n('发送超时（秒）', '发送单条 API 命令的等待上限，必须大于 0。', 5),
  receive_timeout: n('接收超时（秒）', '等待下一段 API 响应的上限，必须大于 0。', 5),
  async: asyncMode,
  wait_timeout: { ...timeout('8s'), label: '同步等待超时', ...optionCopy('同步写入路由器时，限制 DNS 等待写入的时间。', '关闭 RouterOS 异步写入，希望限制查询额外等待时生效。', '等待一次后台写入尝试，超过上限后 DNS 仍继续返回。超时不等于写入已成功或已取消，需要查看后台状态与日志确认最终结果。') },
  queue_capacity: n('队列容量', '限制不同 IP / 路由键的入队及重试积压数量。', 16384),
  comment_prefix: s('注释前缀', '与插件 tag 共同标识 OxiDNS 管理的条目，用于对账和清理。', 'oxi'),
  persistent: o('常驻地址', '启动时同步、不随 DNS TTL 过期的固定 IP / CIDR。', {
    ips: strings('常驻 IP / CIDR', '逐条添加固定地址或网段。'),
    files,
  }),
  min_ttl: n('最小 TTL（秒）', '提升过短的动态地址租约。', 60),
  max_ttl: n('最大 TTL（秒）', '截断过长的动态地址租约。', 3600),
  fixed_ttl: n('固定 TTL（秒）', optionCopy('固定路由器动态租约，填零表示不按时间到期。', '需要覆盖 RouterOS 动态条目原本的有效期和上下限。', '此值优先于 DNS TTL 及最小、最大 TTL。0 表示动态项不按时间到期；与最大 TTL 填 0 会立即到期的含义不同，应按用途区分。')),
  cleanup_on_shutdown: b('关闭时清理', '正常关闭或重载时清理当前插件管理的 RouterOS 条目。', true),
};
const nftTarget = o('nftables 集合', '目标集合需在系统中预先创建。', {
  table_family: required(s('地址族', '例如 ip、ip6 或 inet。')),
  table_name: required(s('表名', '已有 nftables table 的名称。')),
  set_name: required(s('集合名', '已有 nftables set 的名称。')),
  mask: n('前缀长度', '写入地址使用的网络掩码；不配置时使用单地址前缀。'),
});
const records = (label: string) => strings(label, '逐条填写 zone record，例如 {qname} 60 {qclass} A 192.0.2.1；占位符仅允许在名称和类别位置。');
const domainRules = strings('域名规则', '支持 full:、domain:、keyword:、regexp:，或使用 $tag 引用域名 provider。');
const ipRules = strings('IP 规则', '逐条填写 IP、CIDR 或 $tag 形式的 IP provider 引用。');
const namedRules = (label: string, help: string) => strings(label, help);
const cacheArgs = o('缓存参数', '缓存按响应 TTL 过期，可配置负缓存、过期应答和持久化。', {
  size: n('容量', '最大缓存条目数。', 1024),
  lazy_cache_ttl: n('Lazy cache TTL（秒）', optionCopy('复用过期正响应并后台刷新，关闭需删除参数。', '编辑原生缓存插件，希望使用过期正响应减少查询等待。', '填大于 0 的秒数启用。原生参数要关闭应删除此参数，不能填 0；这与常用设置中的 0 表示关闭不同，不会自动把无效值改写。')),
  dump_file: s('持久化文件', '缓存快照路径；建议放在 state/ 下保留运行数据。'),
  dump_interval: n('持久化间隔（秒）', '定期保存缓存快照的周期。', 600),
  short_circuit: stop,
  cache_negative: b('负缓存', optionCopy('记住不存在或无记录的查询，减少重复请求。', '希望缓存域名不存在或没有所需记录的查询结果。', '缓存 NXDOMAIN 和 NODATA 负响应，减少反复向上游查询。结果会按负缓存时长保留；域名刚恢复时，可能需等待旧负响应缓存到期。'), true),
  max_negative_ttl: n('负缓存上限（秒）', '负响应缓存的最大时长。', 300),
  negative_ttl_without_soa: n('无 SOA 负缓存（秒）', '缺少 SOA 时的负缓存时长；0 表示不缓存该类响应。', 60),
  max_positive_ttl: n('正缓存上限（秒）', '限制正响应进入缓存后的最长有效期。'),
  min_positive_ttl: n('正缓存准入 TTL（秒）', optionCopy('跳过有效期过短的查询结果，不提高原有有效期。', '希望避免短寿命的正响应频繁进入缓存时设置。', '有效 TTL 低于此秒数的正响应不写入缓存。它是是否接纳结果的门槛，不会强行提高响应 TTL，也不会延长客户端的缓存时间。')),
  ecs_in_key: b('按 ECS 区分缓存', optionCopy('按客户端网段区分缓存，避免复用不适合的地址。', '上游结果受 ECS 网段影响，不希望不同来源共用结果。', '把 ECS scope 纳入缓存键，让不同网段区分保存查询结果。可能产生更多缓存条目；它只影响缓存区分方式，不会自动给请求添加 ECS。'), false),
});

export const oxidnsPluginCatalog: OxidnsPluginDefinition[] = [
  plugin('udp_server', 'server', 'UDP 入口', optionCopy('为普通设备提供 DNS 查询入口，使用 UDP。', '设备需要传统 UDP DNS，且监听端口可用时添加。', '接收客户端查询后交给指定入口执行器处理。需要同时填写监听地址和已定义的执行器名称；仅有入口不会替你生成完整分流流程。'), 'server/datagram-stream', o('UDP 参数', 'DNS UDP 监听设置。', { entry: serverEntry, listen })),
  plugin('tcp_server', 'server', 'TCP / DoT 入口', optionCopy('兼容 TCP 查询，配好证书后也可提供 DoT。', '需要 TCP DNS 或加密的 DoT，且监听端口可用。', '查询交给所选入口执行器。证书和私钥同时配置时提供 DoT；只配置普通 TCP 地址不会自动生成证书或启用 TLS。'), 'server/datagram-stream', o('TCP 参数', 'DNS TCP 和可选 TLS 设置。', { entry: serverEntry, listen, cert, key, idle_timeout: n('空闲超时（秒）', '空闲 TCP / DoT 连接的回收时间。', 10) })),
  plugin('http_server', 'server', 'HTTP / DoH 入口', optionCopy('让支持 DoH 的设备通过网页协议查询域名。', '客户端支持 DoH，且已有可用的监听地址和处理流程。', '按 URL 路径把查询交给不同执行器，可按需启用 TLS、JSON API 和 HTTP/3；加密与 HTTP/3 需配套证书和私钥。'), 'server/encrypted-http', o('HTTP 参数', '每条 URL 路径可进入不同执行链。', {
    listen, cert, key,
    entries: required(a('路径映射', '为每个 DoH 路径选择执行器。', o('映射', '路径必须以 / 开头。', {
      path: required(s('路径', '例如 /dns-query。', '/dns-query')),
      exec: required(s('执行器', '填写处理此路径的执行器 tag。')),
      json_api: b('JSON DNS API', '允许 name/type 形式的 JSON DNS 查询。', false),
    }))),
    idle_timeout: n('空闲超时（秒）', 'HTTP 连接空闲后的回收时间。', 30),
    src_ip_header: s('客户端 IP 请求头', optionCopy('经过可信反向代理时，还原发起查询的设备地址。', 'DoH 请求通过可信反向代理，真实客户端 IP 放在请求头中。', '填写代理提供的头名，如 X-Forwarded-For。只应信任受控代理提供的值，否则客户端可能伪造来源，影响记录和按设备分流。')),
    enable_http3: b('HTTP/3', '额外开启 QUIC DoH 监听，必须同时提供证书和私钥。', false),
  })),
  plugin('quic_server', 'server', 'DoQ 入口', optionCopy('为支持 DoQ 的设备提供加密的 DNS 入口。', '设备支持 DoQ，且已准备 TLS 证书、私钥和可用 UDP 端口。', '通过 QUIC 接收查询，再交给入口执行器。它占用底层 UDP 端口，应避免与传统 UDP DNS 使用相同监听地址而冲突。'), 'server/encrypted-http', o('QUIC 参数', '监听底层 UDP 端口，请避免与 UDP DNS 冲突。', { entry: serverEntry, listen, cert: required(cert), key: required(key), idle_timeout: n('空闲超时（秒）', '回收空闲 QUIC 连接的等待时间。') })),

  plugin('sequence', 'executor', '规则执行链', optionCopy('按顺序检查条件，决定查询接下来如何处理。', '需要编排分流流程，或把多个插件串起来处理查询。', '从前往后判断规则，条件满足后执行动作。同一条规则的多个条件必须全部满足；没有条件时总是执行，可用 $tag 引用已定义插件。'), 'executor/control-flow', a('规则', '通过 $tag 引用插件；支持内建 accept、return、reject、mark、set_mark、jump、goto。', o('规则', '不填 matches 时总是执行。', {
    matches: strings('匹配条件', '条件使用 $匹配器 或 quick setup 表达式，前加 ! 可取反；也支持单字符串形式。'),
    exec: s('执行动作', '填写 $执行器、quick setup，或 accept、return、reject [rcode]、mark、set_mark、jump/goto tag。'),
  })), [{ exec: 'return' }]),
  plugin('fallback', 'executor', '主备切换', optionCopy('主查询迟迟无结果时，让备用流程参与查询。', '已准备主、备两个执行器，希望减少主路径异常时的等待。', '超过阈值启动备用，也可选择主备同时执行。是否成功只看有无 DNS 响应；若需排除错误响应，必须在分支内另外设置判断条件。'), 'executor/control-flow', o('主备参数', '分支是否成功只看有没有 DNS 响应；若需过滤错误响应，请在分支内增加匹配条件。', {
    primary: required(s('主执行器', '主路径执行器 tag。')), secondary: required(s('备用执行器', '备用路径执行器 tag。')),
    threshold: n('等待阈值（毫秒）', '超过阈值未得到主响应时启动备用路径。', 0),
    always_standby: b('备用同时待命', '让备用路径与主路径同时执行，以额外查询换取较低回退延迟。', false), short_circuit: stop,
  })),
  plugin('forward', 'executor', 'DNS 转发', optionCopy('向所选 DNS 上游查询，可同时询问多个服务器。', '已准备可访问的 DNS 上游，并在处理流程中引用本插件。', '把查询发给一个或多个上游，按并发数和响应选择策略取结果。选择上游名称不等于启用网络代理，连接方式还需按实际网络配置。'), 'executor/resolution', o('转发参数', '多上游竞争时可选择偏向速度、正响应或负响应共识的策略。', {
    upstreams: required(a('上游', '至少配置一个上游。', oxidnsUpstreamSchema)),
    concurrent: n('并发数', '每次最多并行查询 1–32 个上游，且不超过上游数量。', 1),
    response_selection: e('响应选择', optionCopy('多个上游并行时，选择更快应答或更谨慎取结果。', '有多个上游时，按速度与结果取舍。', 'fastest 最快；balanced 折中；prefer_positive 等正响应；consensus 更谨慎确认负响应。负响应包括域名不存在等结果。'), ['fastest', 'balanced', 'prefer_positive', 'consensus'], 'balanced'),
    short_circuit: stop,
  }), { upstreams: [{ addr: '223.5.5.5' }], concurrent: 1 }),
  plugin('cache', 'executor', 'DNS 缓存', optionCopy('复用已有查询结果，减少重复向上游发起请求。', '重复查询较多，希望减少等待和上游请求时添加并引用。', '可缓存正负响应，并设置过期应答、缓存分区和磁盘快照。缓存按响应 TTL 过期；原生 lazy cache 关闭时需删除参数，不能填 0。'), 'executor/resolution', cacheArgs),
  plugin('hosts', 'executor', '静态 Hosts', optionCopy('为指定域名固定返回本地地址，支持双栈。', '需要手动指定网站或局域网设备的域名查询结果。', '按域名规则返回 A / AAAA 地址，可同时使用内联条目和规则文件。新增插件后还需在处理流程引用；其他记录类型可用静态 DNS 记录。'), 'executor/resolution', o('Hosts 参数', '内联条目和文件可以同时提供规则。', {
    entries: strings('条目', '格式：域名规则 IP1 IP2，例如 full:router.example.com 192.0.2.1。'), files, short_circuit: stop,
  })),
  plugin('arbitrary', 'executor', '静态 DNS 记录', optionCopy('自定义 TXT 等记录，直接返回静态应答。', '需要 MX、TXT、SRV 等无法用 Hosts 表示的记录。', '按 zone record 语法填写本地 DNS 应答，可从条目或文件加载。适合精确配置记录类型和内容，添加后还需在执行链中引用。'), 'executor/resolution', o('静态记录参数', '适合 MX、TXT、SRV 等 Hosts 无法表示的记录。', {
    rules: strings('记录', '每条使用 zone record 语法，例如 example.test. 60 IN TXT "hello"。'), files, short_circuit: stop,
  })),
  plugin('response', 'executor', '构造完整响应', optionCopy('直接设置响应码和记录，生成完整 DNS 应答。', '需要精确控制某类查询的返回状态、记录内容和标志。', '设置应答、权威及附加记录区，支持 {qname} 和 {qclass} 占位符。由执行链调用时生成响应；是否继续处理由命中后结束选项决定。'), 'executor/resolution', o('响应参数', '可在记录模板中使用 {qname} 与 {qclass}。', {
    rcode: s('响应码', '填写 NOERROR、NXDOMAIN 等名称，也可切换为数字填写代码。', 'NOERROR'),
    answers: records('应答记录'), authorities: records('权威记录'), additionals: records('附加记录'),
    authoritative: b('权威应答 AA', '设置响应的 authoritative-answer 标志。', false),
    authentic_data: b('认证数据 AD', '设置响应的 authenticated-data 标志。', false),
    short_circuit: { ...stop, default: true },
  })),
  plugin('redirect', 'executor', '域名重定向', optionCopy('把指定域名的查询转向另一个域名进行解析。', '希望查询某个域名时，改为获取另一个域名的结果。', '规则命中后改写查询目标，返回时恢复客户端可见的 CNAME 关系。需要填写域名规则和目标域名，并在 DNS 执行链中引用。'), 'executor/resolution', o('重定向参数', '匹配域名后查询另一个域名。', { rules: strings('重定向规则', '格式：域名规则 目标域名，例如 full:old.example new.example。'), files })),
  plugin('client_ip_from_ecs', 'executor', '从 ECS 读取来源', optionCopy('从可信转发设备还原来源 IP，供后续分流使用。', '查询经可信转发设备到达，真实来源写在 ECS 扩展中。', '只接受可信来源提供的 ECS，恢复客户端 IP 供分流和记录。默认只信任本机回环地址；应按实际可信设备填写 IP 或网段。'), 'executor/resolution', strings('可信客户端', '允许提供 ECS 的原始客户端 IP / CIDR；默认仅信任 127.0.0.1 和 ::1。')),
  plugin('ecs_handler', 'executor', 'ECS 处理', optionCopy('附带客户端网段信息，帮助上游选择合适结果。', '上游支持 ECS，且希望按来源网段影响 DNS 返回结果。', '可保留已有 ECS、按来源补充，或使用固定地址生成网段信息。按 IPv4 / IPv6 前缀控制范围，不会直接修改客户端的真实 IP。'), 'executor/resolution', o('ECS 参数', '按来源地址或指定地址附加客户端网段。', {
    forward: b('保留已有 ECS', '保留客户端已发送的 ECS。', false), send: b('自动补充 ECS', '请求缺少 ECS 时根据来源地址生成。', false),
    preset: s('固定来源地址', '使用此 IP 作为生成 ECS 的来源。'), mask4: n('IPv4 前缀', 'IPv4 ECS 网络前缀长度。', 24), mask6: n('IPv6 前缀', 'IPv6 ECS 网络前缀长度。', 48),
  })),
  plugin('forward_edns0opt', 'executor', '转发 EDNS0 选项', optionCopy('按需把请求中的 DNS 扩展复制到最终应答。', '明确知道客户端需要哪些 EDNS0 扩展选项时使用。', '填写数字选项代码，将对应扩展从请求复制到最终响应。它不会转发整份请求或替换查询结果；不确定用途时无需添加。'), 'executor/resolution', o('EDNS0 参数', '按数字 option code 选择需要复制的扩展。', { codes: a('选项代码', 'EDNS0 option code 数字列表。', n('代码', '要复制的 EDNS0 option code。')) })),

  plugin('ttl', 'executor', 'TTL 改写', optionCopy('控制设备复用查询结果的时长，以秒为单位。', '需要固定 DNS 响应的有效期，或限制过短、过长的 TTL。', '可设置固定值，或指定上下限，修改响应记录中的 TTL。应在有响应的处理位置调用；这里不等同于设置缓存插件的保存容量。'), 'executor/response', o('TTL 参数', '所有时间均为秒。', { fix: n('固定 TTL', '将全部记录 TTL 设为这个值。'), min: n('最小 TTL', '提高低于此值的 TTL。'), max: n('最大 TTL', '降低高于此值的 TTL。') })),
  plugin('ip_selector', 'executor', 'IP 优选', optionCopy('同一网站有多个 IP 时，优先选择较快的地址。', '响应含多个地址，且希望通过测速改善连接选择。', '探测后选择或重排地址，会增加连接开销。后台模式先应答再测速，前台模式可能增加等待；可用探测缓存减少反复测速。'), 'executor/response', o('优选参数', '探测会消耗额外连接，可使用后台模式及缓存降低查询延迟。', {
    selection_mode: e('优选模式', optionCopy('选择先返回地址，或等待测速完成后再应答。', '已经启用 IP 优选，按能接受的额外查询等待时间选择。', 'first_success 用首个成功地址；best_within_budget 在限时内比较；background 先应答再后台刷新，前两项可能增加等待。'), ['first_success', 'best_within_budget', 'background'], 'first_success'),
    probe_methods: strings('探测方法', '支持 tcp:端口、ping、none；也可切换为单字符串。默认 tcp:443 与 tcp:80。'), outbound, socks5,
    probe_stagger: n('探测错峰（毫秒）', '多种探测方法之间的启动间隔。', 200), probe_timeout: n('探测超时（毫秒）', '单个 IP 探测的等待上限。', 600), max_wait: n('总等待预算（毫秒）', '本次响应优选最多等待多久。', 1000),
    top_n: n('保留地址数', '保留排名前 N 个地址；0 表示只重排。', 1),
    reorder_only: b('DNSSEC 仅重排（兼容）', optionCopy('兼容旧版签名处理设置，新配置建议用策略选项。', '旧配置仍使用 reorder_only，且没有设置 dnssec_policy。', '此项为 true 时对应仅重排策略；一旦显式设置 DNSSEC 策略，以新参数为准。新配置建议直接设置策略，避免两处含义混淆。'), false),
    dnssec_policy: e('DNSSEC 策略', optionCopy('对带签名的响应，只调整地址顺序或跳过优选。', 'IP 优选遇到带 DNSSEC 的查询或响应时生效。', 'reorder_only 只调整地址顺序，不裁剪记录；skip 完全跳过优选。两种方式均用于保护签名记录完整性，不会替你开启 DNSSEC 验证。'), ['reorder_only', 'skip'], 'reorder_only'),
    max_parallel_probes: n('最大并行探测', '插件级探测并发上限，避免冷请求同时发起过多连接。', 256), cache: probeCache,
  })),
  plugin('prefer_ipv4', 'executor', '优先 IPv4', optionCopy('网站有 IPv4 地址时，引导设备优先使用它。', '双栈网络下希望优先 IPv4，并已在查询流程引用插件。', '探测并缓存 IPv4 可用状态；确认存在时抑制另一地址族响应。内部探针可使用独立执行器，不能引用自身而造成循环。'), 'executor/response', preferArgs),
  plugin('prefer_ipv6', 'executor', '优先 IPv6', optionCopy('网站有 IPv6 地址时，引导设备优先使用它。', 'IPv6 网络可用，希望双栈网站优先选择 IPv6。', '探测并缓存 IPv6 可用状态；确认存在时抑制另一地址族响应。内部探针可单独设置执行器，引用自身会造成循环，应避免。'), 'executor/response', preferArgs),
  plugin('black_hole', 'executor', '拦截响应', optionCopy('命中指定规则后，返回拦截或自定义地址应答。', '需要广告屏蔽或自定义拒绝策略，并已设置命中条件。', '可返回域名不存在、空应答、空地址、自定义 IP 或拒绝查询。未填模式时，有 ips 则用 custom，否则用 nxdomain；null 是模式名。'), 'executor/response', o('拦截参数', '可用于广告屏蔽或自定义拒绝策略。', {
    mode: e('响应模式', optionCopy('选择拦截后返回什么状态，或使用自定义地址。', '已经设置拦截动作，需要确定命中后的 DNS 返回方式。', '未填时，有 ips 使用 custom，否则用 nxdomain。null 是模式名称字符串而非空值；按模式返回空应答、空地址、拒绝或自定义地址。'), ['nxdomain', 'nodata', 'null', 'custom', 'refused']),
    ips: strings('自定义 IP', 'custom 模式按查询地址族选择对应 IP；其他类型返回空应答。'), short_circuit: stop,
  })),
  plugin('drop_resp', 'executor', '清空响应', optionCopy('清除已经取得的应答，让后续流程重新查询。', '执行链已有响应，但希望后续步骤按其他方式重新处理。', '删除当前查询上下文中的响应，再继续执行后面的动作。它本身不会发起新的上游查询，后续流程需配置实际的解析步骤。'), 'executor/response', none),
  plugin('reverse_lookup', 'executor', '反向查询缓存', optionCopy('记住 IP 对应的域名，按需回答反向查询。', '需要从普通 DNS 应答还原域名，或处理 PTR 查询。', '从响应学习 IP 到域名的映射，按容量和 TTL 保留。直接回答 PTR 需要开启 handle_ptr；空参数合法，插件仍需在执行链中引用。'), 'executor/response', o('反查参数', '缓存从 DNS 响应中学习的映射。', { size: n('容量', '最多保存的 IP 到域名映射数。', 65535), handle_ptr: b('处理 PTR', '使用缓存直接响应 PTR 请求。', false), ttl: n('映射 TTL（秒）', 'IP 到域名映射的保留时间。', 7200) })),

  plugin('query_summary', 'executor', '查询摘要', optionCopy('记录简短查询摘要，便于查看处理结果。', '希望在日志中查看查询概况，而非保存完整历史。', '在后续执行链结束时输出摘要，使用全局日志目标。可设标题区分不同输出位置；若需历史检索和统计，应使用查询记录插件。'), 'executor/observability', o('摘要参数', '日志输出到全局日志目标。', { msg: s('摘要标题', '用于区分不同位置输出的摘要。', 'query summary') })),
  plugin('query_recorder', 'executor', '查询记录', optionCopy('保存查询历史和处理路径，便于查阅与排错。', '需要逐条追查 DNS 查询，且设备可承担数据库写入。', '将查询、响应和执行路径保存到 SQLite，提供历史与统计 API。建议把数据库放在 state/，避免发布配置时覆盖运行数据。'), 'executor/observability', o('记录参数', '数据库应放在 state/ 下，避免部署覆盖运行数据。', {
    path: required(s('数据库路径', '此记录器独立使用的 SQLite 文件路径。', 'state/query-recorder.sqlite')),
    queue_size: n('写入队列', '查询处理与后台写线程之间的有界队列容量。', 8192), batch_size: n('写入批量', '每批写入数据库的记录数。', 256),
    flush_interval_ms: n('刷新间隔（毫秒）', '等待不足一批数据时的定期写入周期。', 200), memory_tail: n('内存最近记录', '用于实时流回放的内存记录数量。', 1024),
    retention_days: n('保留天数', '自动清理超过此时间的查询记录。', 7), cleanup_interval_hours: n('清理间隔（小时）', '执行过期数据清理的周期。', 1), reader_concurrency: n('并发读取数', '限制 WebUI 历史与统计同时运行的数据库读取任务。', 2),
  })),
  plugin('metrics_collector', 'executor', '指标收集', optionCopy('收集请求量和延迟，供监控系统读取统计。', '使用 Prometheus 等监控，需要汇总 DNS 指标。', '收集请求数量、耗时等统计，支持按名称区分不同处理路径。它提供汇总指标，不替代逐条历史；需在目标执行链中引用。'), 'executor/observability', o('指标参数', '为不同链路提供独立指标名称。', { name: s('名称', '输出指标使用的名称标签。', 'default') })),
  plugin('debug_print', 'executor', '调试输出', optionCopy('排障时记录请求和响应内容，日常可移除。', '需要分析实际 DNS 请求或响应内容，且可查看服务日志。', '把请求和响应对象输出到全局日志，便于定位处理问题。信息量可能较大，排查完成后移除此执行器，避免长期产生大量日志。'), 'executor/observability', o('调试参数', '调试输出可能较多，排障后可移除此执行器。', { msg: s('日志标题', '识别此处输出的调试内容。', 'debug print') })),
  plugin('sleep', 'executor', '异步等待', optionCopy('测试主备或超时处理时，主动增加查询延迟。', '需要验证慢查询、主备接替或超时策略时临时添加。', '为当前查询增加指定毫秒数的等待，不阻塞整个运行线程。它会实际拖慢命中的查询，完成验证后应从正常执行链中移除。'), 'executor/observability', o('等待参数', '只延迟当前任务，不阻塞运行线程。', { duration: n('等待时间（毫秒）', '增加的异步等待时长。', 0) })),
  plugin('learn_domain', 'executor', '域名学习', optionCopy('把查询域名加入动态规则，方便后续直接分流。', '已准备可写的 dynamic_domain_set，且希望自动学习域名。', '将经过执行链的查询域名写入目标集合，可限定类型、阶段和成功条件。规则会影响后续分流；目标必须是动态集合，普通文件集合不可替代。'), 'executor/observability', o('学习参数', '目标 provider 必须是可写的动态域名集合。', {
    provider: required(s('目标 Provider', 'dynamic_domain_set 插件的 tag。')), phase,
    questions: e('学习范围', optionCopy('普通查询只需学习首个问题，也可检查全部。', '域名学习已启用，需要决定多问题请求的处理范围。', 'first 只处理第一个 DNS 问题，all 检查全部。常见请求通常只有一个问题，此时没有区别；是否学习还受类型、阶段和响应条件限制。'), ['first', 'all'], 'first'),
    qtypes: strings('查询类型', '只学习指定查询类型，例如 A、AAAA；也接受数字代码。'),
    success_only: b('仅成功响应', 'after 阶段只学习 NOERROR 响应。', true), answer_required: b('要求有应答记录', 'after 阶段只学习包含 answer 的响应。', true),
    rule_kind: e('规则类型', optionCopy('选择只匹配学到的域名，或覆盖它的子域名。', '启用域名学习，需要决定新增规则覆盖的域名范围。', 'full 只匹配完整域名，domain 还包含其下级子域名。不确定时可先选 full，避免学习一个域名后连带改变更多子域名的分流。'), ['full', 'domain'], 'full'), async: asyncMode, error_mode: errorMode, timeout: timeout('1s'),
  })),

  plugin('http_request', 'executor', 'HTTP 回调', optionCopy('查询触发外部 HTTP 请求，用于通知或服务联动。', '已有可调用的外部 HTTP 服务，并需要与查询过程联动。', '按配置发送请求，URL、头和请求体可引用域名、来源 IP 等占位符。同步模式会增加查询等待，失败如何处理由对应选项决定。'), 'executor/integrations', o('HTTP 请求参数', 'URL、请求头与请求体可使用 ${qname}、${client_ip} 等运行期占位符。', {
    method: required(e('方法', '请求使用的 HTTP 方法。', ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'], 'POST')),
    url: required(s('URL', '目标 http:// 或 https:// 地址。')), phase, async: asyncMode, timeout: timeout(), error_mode: errorMode,
    headers: map('请求头', '逐项添加 HTTP 头名称和字符串值。'), query_params: map('查询参数', '逐项添加到 URL 查询字符串。'),
    body: s('文本请求体', '原始请求体；body、json、form 不能同时配置。'), json: o('JSON 请求体', '可切换为数组或其他 JSON 值，支持递归编辑字段。', {}), form: map('表单请求体', '作为 application/x-www-form-urlencoded 发送。'), content_type: s('内容类型', '仅为原始 body 设置 Content-Type。'),
    outbound, socks5, insecure_skip_verify: insecure, max_redirects: n('重定向上限', '最多跟随多少次 HTTP 重定向。', 5), queue_size: n('异步队列容量', '异步模式待发送请求的最大数量。', 256),
  })),
  plugin('script', 'executor', '外部命令', optionCopy('查询时运行设备上的程序，传入所需上下文。', '目标设备已有可执行程序，并希望在 DNS 流程中调用。', '直接启动程序，不会自动添加 shell。参数和环境变量可引用查询上下文；须在目标主机准备好程序，并设置超时和失败处理。'), 'executor/integrations', o('命令参数', '直接启动程序，不自动添加 shell；在目标主机准备对应命令。', {
    command: required(s('程序', '要执行的命令或可执行文件路径。')), args: strings('参数', '每项为一个参数，支持 ${qname}、${client_ip} 等占位符。'), env: map('环境变量', '为本次子进程设置变量，可引用运行期占位符。'), cwd: s('工作目录', '子进程执行时的工作目录。'), timeout: timeout(), error_mode: errorMode, max_output_bytes: n('输出捕获上限（字节）', '超过上限的 stdout / stderr 会被截断。', 4096),
  })),
  plugin('ipset', 'executor', 'Linux ipset', optionCopy('把应答 IP 加入集合，供现有系统策略使用。', '目标是 Linux，已创建 ipset，且服务具备相应权限。', '通过内置 netlink 后端写入应答 IP，支持 IPv4 / IPv6 集合和前缀。集合供已有防火墙或路由策略匹配，不会自动创建这些策略。'), 'executor/integrations', o('ipset 参数', '使用内置 netlink 后端，需要 Linux 及相应权限。', {
    set_name4: s('IPv4 集合', 'IPv4 地址写入的已有 ipset 名称。'), set_name6: s('IPv6 集合', 'IPv6 地址写入的已有 ipset 名称。'), mask4: n('IPv4 前缀', '写入 IPv4 网络前缀长度。', 32), mask6: n('IPv6 前缀', '写入 IPv6 网络前缀长度。', 128),
  })),
  plugin('nftset', 'executor', 'Linux nftables', optionCopy('把应答 IP 写入防火墙集合，供现有规则使用。', 'Linux 已创建 nftables 表和集合，并允许服务写入。', '按 IPv4 / IPv6 配置写入应答地址。优先使用分组对象，也兼容旧扁平字段；集合须提前存在，不会替你创建防火墙策略。'), 'executor/integrations', o('nftables 参数', '优先使用 ipv4 / ipv6 对象；旧扁平字段同时保留兼容。', {
    ipv4: nftTarget, ipv6: nftTarget,
    table_family4: s('IPv4 地址族（兼容）', '旧格式的 table family，通常为 ip。'), table_family6: s('IPv6 地址族（兼容）', '旧格式的 table family，通常为 ip6。'),
    table_name4: s('IPv4 表名（兼容）', '旧格式 IPv4 table 名称。'), table_name6: s('IPv6 表名（兼容）', '旧格式 IPv6 table 名称。'), set_name4: s('IPv4 集合（兼容）', '旧格式 IPv4 set 名称。'), set_name6: s('IPv6 集合（兼容）', '旧格式 IPv6 set 名称。'), mask4: n('IPv4 前缀（兼容）', '旧格式 IPv4 网络前缀长度。', 32), mask6: n('IPv6 前缀（兼容）', '旧格式 IPv6 网络前缀长度。', 128),
  })),
  plugin('ros_address_list', 'executor', 'RouterOS 地址列表', optionCopy('将应答 IP 同步到路由器的指定地址列表。', 'RouterOS API 可用，账号有权限，且已有使用列表的策略。', '把到达本插件的响应 IP 写入地址列表，维护 TTL 和常驻地址。至少选一个地址族的列表；是否仅处理直连响应，由调用位置决定。'), 'executor/integrations', o('地址列表参数', '至少选择一个地址族的目标列表。', { ...rosShared, address_list4: s('IPv4 地址列表', '接收 A 记录的 address-list 名称。'), address_list6: s('IPv6 地址列表', '接收 AAAA 记录的 address-list 名称。') })),
  plugin('ros_route', 'executor', 'RouterOS 路由', optionCopy('把应答 IP 写成单地址路由，并自动维护租约。', 'RouterOS 已准备路由表和网关，账号有 API 写入权限。', '将到达本插件的应答 IP 写入目标表，维护主机路由租约。不会创建路由表、默认路由或路由规则，处理范围取决于插件调用位置。'), 'executor/integrations', o('路由参数', '路由表和网关需预先存在；插件不创建默认路由或 routing rule。', {
    ...rosShared, routing_table: required(s('路由表', 'RouterOS 中已有的目标 routing table 名称。')), gateway4: s('IPv4 网关', 'IPv4 路由使用的 RouterOS gateway 表达式。'), gateway6: s('IPv6 网关', 'IPv6 路由使用的 RouterOS gateway 表达式。'), distance: n('路由距离', '写入路由的 distance。', 100), conntrack_guard: b('连接跟踪保护', optionCopy('路由租约到期时，尽量保留仍在使用的连接。', 'RouterOS 主机路由可能在长连接仍活动时到期。', '删除到期主机路由前检查连接跟踪，仍有活跃连接时延后 30 秒再检查。它延后的是路由清理，不会延长 DNS 响应的 TTL。'), false),
  })),

  plugin('upgrade', 'executor', '自动升级', optionCopy('按需升级核心和网页界面，也可定时执行。', '希望由 OxiDNS 执行自身升级，且目标设备可下载发布文件。', '替换核心程序和可选 WebUI，支持保留备份或暂不重启。可由定时任务调用；清理备份后会失去对应回滚文件，应按需要设置。'), 'executor/maintenance', o('升级参数', '此执行器执行 apply；可选择保留备份或跳过重启。', {
    repository: s('GitHub 仓库', '获取 Release 的 owner/repository。', 'svenshi/oxidns'), asset: s('发布文件', 'auto 按平台和构建版本选择；也可填写确切资产名称。', 'auto'), bundle: e('构建版本', optionCopy('自动选发布文件时，决定要使用哪种核心构建。', '发布文件 asset 设为 auto，希望按构建类型选择升级包。', 'auto 跟随当前二进制的构建类型，也可选 full、standard 或 minimal。手动指定确切文件名时不使用此选项，应与目标平台匹配。'), ['auto', 'full', 'standard', 'minimal'], 'auto'),
    github_token: s('GitHub Token', '可提高 API 限额或访问私有仓库；建议使用环境变量。'), cache_dir: s('下载缓存目录', '保存升级下载文件的目录。'), backup_dir: s('备份目录', '替换前的文件备份目录。'), webui_dir: s('WebUI 目录', '应与 api.http.webui.root 保持一致。', './webui'),
    skip_webui: b('跳过 WebUI', '只升级二进制。', false), no_restart: b('暂不重启', '升级完成后不自动重启 OxiDNS。', false), allow_prerelease: b('包含预发布版', '允许选择 GitHub 预发布版本；同版本官方源码支持此参数。', false), force: b('强制升级', '即使目标版本不比当前新也执行替换。', false), cleanup: b('完成后清理', '成功后清理下载缓存与备份；需要保留回滚文件时关闭。', true), timeout: timeout('30s'), outbound, socks5, insecure_skip_verify: insecure,
  })),
  plugin('download', 'executor', '文件下载', optionCopy('逐个下载文件，完整写入后再替换旧版本。', '规则或其他文件已有 HTTP(S) 地址，需要自动获取。', '按列表顺序下载，单个文件写入完整后再替换旧文件。后续可串联 reload_provider 让规则生效；启动补齐失败时会阻止启动。'), 'executor/maintenance', o('下载参数', '文件更新后可串联 reload_provider，让规则 provider 重新载入。', {
    timeout: timeout('30s'), outbound, socks5, insecure_skip_verify: insecure,
    startup_if_missing: b('启动时补齐文件', '启动时下载缺失目标；失败会阻止启动，已有文件不会被覆盖。', true),
    downloads: required(a('下载列表', '每项指定 URL、目录和可选文件名。', o('下载项', '仅支持 HTTP / HTTPS。', { url: required(s('URL', '远端文件地址。')), dir: required(s('保存目录', '目标本地目录，不存在时创建。')), filename: s('文件名', '省略时根据 URL 推导文件名。') }))),
  })),
  plugin('reload_provider', 'executor', '重新载入规则', optionCopy('文件更新后刷新指定规则，无需重建整条流程。', '文件型规则已经更新，希望运行中的集合读取新内容。', '用 $provider_tag 指定需要重载的规则集合，按 tag 刷新快照。只重载所选资源，不会自动下载文件，也不重建整条执行链。'), 'executor/maintenance', strings('Provider 列表', '填写 $provider_tag，按需要刷新文件型规则集合。')),
  plugin('reload', 'executor', '重新载入全部配置', optionCopy('重新读取全部配置，重建运行中的依赖和插件。', '配置文件已准备好，希望应用完整变更而非只刷新规则。', '触发应用级重载，重新构建依赖和插件。只更新文件型规则时可改用指定规则重载；此操作本身不会替你生成或编辑配置文件。'), 'executor/maintenance', none),
  plugin('cron', 'executor', '定时任务', optionCopy('按指定时间或间隔，自动执行已配置维护动作。', '已有下载、重载或升级等执行器，希望安排自动运行。', '每个任务在 Cron 和固定间隔中二选一，按指定顺序调用执行器。时间按任务时区解释；省略时区时使用系统时区。'), 'executor/maintenance', o('调度参数', '每个任务必须选择 schedule 或 interval 之一。', {
    timezone: s('时区', 'IANA 时区，例如 Asia/Shanghai；省略时使用系统时区。'),
    jobs: required(a('任务', '为每个任务添加名称、时间条件和执行器。', o('任务', '任务中的执行器按填写顺序执行。', {
      name: required(s('任务名称', '用于日志和运行时标识。')), schedule: s('Cron 表达式', '标准 5 字段表达式，例如 0 */6 * * *；与 interval 互斥。'), interval: s('固定间隔', '例如 5m、1h、1d；与 schedule 互斥。'), executors: required(strings('执行器', '支持 $tag、裸 tag 或 quick setup 表达式。')),
    }))),
  })),

  plugin('qname', 'matcher', '查询域名', optionCopy('按设备正在查询的域名，选择后续处理方式。', '需要基于查询域名区分直连、代理或其他处理动作。', '用内联域名规则或域名集合判断是否命中，支持精确、子域名等表达式。它只判断条件，命中后做什么仍由执行链中的动作决定。'), 'matcher/request', domainRules),
  plugin('question', 'matcher', '完整问题匹配', optionCopy('同时按域名和查询类型等条件判断请求。', '已有支持完整问题匹配的规则资源，例如 AdGuard 规则。', '引用支持 question 匹配的 provider，同时判断域名、类型等信息。它只给出是否命中，不会自行拦截；需在执行链中配置后续动作。'), 'matcher/request', strings('Provider 引用', '使用 $tag 引用支持 question 匹配的 provider，例如 adguard_rule。')),
  plugin('qtype', 'matcher', '查询类型', optionCopy('区分不同地址类型的查询，分别安排处理。', '需要让不同 DNS 记录类型使用不同处理方式时添加。', '按 A、AAAA、HTTPS 等类型匹配，支持类型名称或数字代码。它判断的是查询类型，不直接判断网络能否使用对应地址族。'), 'matcher/request', namedRules('查询类型', '填写数字代码或类型名称，例如 A、AAAA、65。')),
  plugin('qclass', 'matcher', '查询类别', optionCopy('按 DNS 查询类别匹配，普通网络通常无需调整。', '存在需要按 DNS class 区分处理的特殊查询。', '填写类别名称或数字，如 IN 或 1。匹配请求问题中的类别，常见互联网查询通常属于 IN；它不是 A、AAAA 等记录类型。'), 'matcher/request', namedRules('查询类别', '填写类别名称或数字，例如 IN、1。')),
  plugin('client_ip', 'matcher', '客户端 IP', optionCopy('按发起查询的设备 IP 或网段，区分处理方式。', '希望指定手机、电脑或整个网段使用不同 DNS 策略。', '将客户端来源地址与 IP、CIDR 或引用集合匹配。只决定当前条件是否命中；查询经转发器到达时，应先确认看到的来源是否真实。'), 'matcher/request', ipRules),
  plugin('ptr_ip', 'matcher', 'PTR 地址', optionCopy('反查域名时，按其中对应的 IP 判断条件。', '需要对 PTR 反向查询按 IP 或网段分别处理。', '从反向查询名称提取 IP，再与指定地址、网段或 IP 集合匹配。它不查找应答中的 IP；仅在此类反向查询符合条件时命中。'), 'matcher/request', ipRules),
  plugin('resp_ip', 'matcher', '响应 IP', optionCopy('按查询结果里的真实 IP，决定后续分流方式。', '执行链已经获得包含 A / AAAA 地址的 DNS 应答。', '将应答地址与 IP、网段或规则集合匹配，用于响应后的处理分支。没有目标地址时不会凭查询域名推断；它与按来源设备匹配不同。'), 'matcher/response', ipRules),
  plugin('cname', 'matcher', 'CNAME 目标', optionCopy('按查询结果指向的别名目标，决定后续处理。', '已有 DNS 响应，且需要检查其中的 CNAME 指向。', '匹配应答中的 CNAME 目标域名，支持域名规则或集合。它不直接改写查询，也不是匹配最初查询的名称；后续动作由执行链决定。'), 'matcher/response', domainRules),
  plugin('rcode', 'matcher', '响应码', optionCopy('按查询成功、域名不存在等状态决定下一步。', '已经取得 DNS 响应，需要按响应状态区分处理。', '支持 NOERROR、NXDOMAIN 等名称或数字代码。成功状态不一定含有效答案；若关心是否有目标记录，还需结合所需记录条件判断。'), 'matcher/response', namedRules('响应码', '填写 NOERROR、NXDOMAIN 等名称或数字代码。')),
  plugin('has_resp', 'matcher', '已有响应', optionCopy('检查是否已经取得 DNS 应答，避免重复查询。', '执行链前面可能已取得结果，需要决定是否继续处理。', '当前上下文只要存在 DNS 响应就命中，不要求成功状态或答案非空。若只接受有效结果，应再检查响应码或是否包含所需记录。'), 'matcher/response', none),
  plugin('has_wanted_ans', 'matcher', '包含所需记录', optionCopy('检查应答是否包含当前查询需要的记录。', '已经取得 DNS 响应，希望区分有效答案和空结果。', '应答区包含本次查询类型需要的记录时命中。它比单纯检查已有响应更具体，但不负责重新查询；未命中时的动作由执行链设置。'), 'matcher/response', none),
  plugin('mark', 'matcher', '上下文标记', optionCopy('按前面步骤添加的标记，区分当前查询流程。', '执行链已使用标记动作，需要在后续步骤识别查询状态。', '当前请求的 marks 与配置标记有任意交集时命中。标记仅属于请求上下文，不是 Linux 的 socket 路由标记，也不会自动改变出口。'), 'matcher/context', namedRules('标记', '每项为无符号整数标记，也支持在字符串中用空白或逗号分隔。')),
  plugin('env', 'matcher', '环境变量', optionCopy('根据运行设备的环境变量，启用对应处理分支。', '目标设备已设置环境变量，希望同一配置按环境选择策略。', '所有变量条件都满足才命中。支持检查变量存在或值相等；每项中的空格、逗号会保留，不会被自动拆成多条条件。'), 'matcher/context', strings('环境条件', 'KEY=VALUE 或 KEY:VALUE 检查等值；KEY、KEY=、KEY: 检查存在。每项保留其中的空格和逗号。')),
  plugin('time', 'matcher', '时间条件', optionCopy('按时间段、星期或日期，自动切换处理策略。', '希望工作时段、周末等不同时间使用不同 DNS 处理分支。', '按指定时区判断，多个时段满足任一即可，同一时段的条件需全部满足。跨午夜时段归属于开始日，省略时区时使用系统时区。'), 'matcher/context', o('时间参数', '多个时段为 OR，同一时段的条件为 AND。', {
    timezone: s('时区', 'IANA 时区名称，例如 Asia/Shanghai；省略时使用系统时区。'),
    periods: required(a('时段', '添加 1–64 个时段；跨午夜条件归属于开始日。', o('时段', '起止时间需同时填写或同时省略；省略时需配置星期或月日。', {
      start: s('开始时间', 'HH:MM，包含此时刻。'), end: s('结束时间', 'HH:MM，不包含此时刻；早于开始时间表示跨午夜。'), weekdays: strings('星期', 'mon 至 sun，或切换条目为数字填写 1–7。'), monthdays: a('每月日期', '1–31；该月不存在的日期不会命中。', n('日期', '每月第几日，1–31。')),
    }))),
  })),
  plugin('random', 'matcher', '概率匹配', optionCopy('按指定概率命中，用于抽样测试或随机分流。', '需要按比例选择部分查询，而非按域名或设备固定分组。', '只填一个 0 到 1 的概率，如 0.1 表示约 10%。每次判断是随机抽样，不保证少量请求恰好达到该比例；命中后由执行链决定动作。'), 'matcher/context', strings('概率', '只添加一个值，范围 0.0–1.0；例如 0.1 表示约 10%。'), ['0.1']),
  plugin('rate_limiter', 'matcher', '客户端限速', optionCopy('限制设备或网段的查询频率，允许短时突发。', '希望按客户端控制请求频率，并已配置超限后的处理动作。', '用令牌桶限制速率，按 IP 前缀归组。命中表示还有额度、允许通过，false 才表示已限流；插件只判断，拦截动作需在执行链配置。'), 'matcher/context', o('限速参数', '可按 IPv4 / IPv6 前缀聚合客户端。', { qps: n('每秒令牌', '令牌每秒补充速率。', 20), burst: n('突发容量', '令牌桶最多容纳的令牌数量。', 40), mask4: n('IPv4 聚合前缀', '32 表示每个 IPv4 地址独立限速。', 32), mask6: n('IPv6 聚合前缀', '按此前缀聚合 IPv6 客户端。', 48) })),
  plugin('string_exp', 'matcher', '字符串表达式', optionCopy('用文本表达式检查域名等查询信息，组合条件。', '需要使用现成匹配器之外的字符串判断条件。', '按来源、运算和参数填写，如 qname suffix .example.com。也可切为数组逐项编辑；表达式只判断是否命中，后续动作另行设置。'), 'matcher/composition', s('表达式', '格式：来源 运算 参数，例如 qname suffix .example.com；也可切为数组，逐项填写 source、op、参数。')),
  plugin('any_match', 'matcher', '任一条件', optionCopy('多个条件中有一个满足，即执行后续匹配动作。', '希望几个可替代条件满足任意一个就命中，而非全部满足。', '填写 $matcher 或快速匹配表达式，逐项判断是否有一个成立。它只合并条件，不直接执行转发或拦截；命中后的动作在执行链中配置。'), 'matcher/composition', strings('条件', '填写 $matcher 或 quick setup 匹配表达式。')),
  plugin('_true', 'matcher', '恒为真', optionCopy('始终让条件成立，适合临时验证处理流程。', '需要测试某个分支是否能正常执行，而不依赖真实查询内容。', '每次判断都返回命中，不需要 args。它本身不会生成应答或转发；应在执行链中配套动作，测试完成后检查是否需要保留。'), 'matcher/composition', none),
  plugin('_false', 'matcher', '恒为假', optionCopy('始终让条件不成立，适合临时验证跳过分支。', '需要测试未命中时的执行路径，而不依赖真实查询内容。', '每次判断都不命中，不需要 args。它不会主动拦截 DNS；只是使依赖此条件的分支不成立，后续处理仍按执行链配置继续。'), 'matcher/composition', none),

  plugin('domain_set', 'provider', '域名集合', optionCopy('把多处域名规则放在一个集合里，方便复用。', '多条分流规则需要共用同一组域名匹配范围。', '汇总内联、文件和其他域名 provider 中的规则，可供 qname、cname 等引用。集合只提供匹配资源，不会自行决定直连、代理或拦截。'), 'provider/domain', o('域名集合参数', '可由 qname、cname 等匹配器引用。', { exps: strings('域名表达式', '支持 full:、domain:、keyword:、regexp: 或无前缀域名；复用 provider 请添加到 sets。'), files, sets: strings('引用集合', '引用具有域名匹配能力的 provider tag。') })),
  plugin('dynamic_domain_set', 'provider', '动态域名集合', optionCopy('维护可随时更新的域名规则，重启后仍可保留。', '需要通过 API 或自动学习改规则，而不只使用静态文件。', '维护可写且能保存到文件的域名集合，供匹配器和学习插件使用。运行文件建议放在 state/，初始规则只在文件不存在时写入。'), 'provider/domain', o('动态集合参数', '运行期生成的文件建议放在 state/ 中。', { path: required(s('规则文件', '动态集合自己的本地文件路径。')), bootstrap_rules: strings('初始规则', '仅在规则文件不存在时写入的域名规则。'), queue_size: n('写入队列', '待写入域名的队列容量。', 1024), batch_size: n('写入批量', '触发批量追加的规则数量。', 256), flush_interval_ms: n('刷新间隔（毫秒）', '后台定期追加文件的间隔。', 200) })),
  plugin('geosite', 'provider', 'GeoSite 数据库', optionCopy('从域名数据库选择一组网站，供分流匹配。', '已有 geosite.dat，且知道所需的分类或属性名称。', '按分类选择域名，如 cn，也支持 code@attribute。可通过附加文件提供数据库；集合需被匹配器引用，选分类不会自动确定查询出口。'), 'provider/domain', o('GeoSite 参数', '加载二进制规则库；可通过附加文件上传数据库。', { file: required(s('数据库文件', 'geosite.dat 路径。')), selectors: strings('分类选择', '例如 cn、geolocation-!cn，也支持 code@attribute。') })),
  plugin('adguard_rule', 'provider', 'AdGuard 规则', optionCopy('载入 DNS 过滤规则，用于判断广告等域名查询。', '已有 AdGuard DNS 规则，并准备通过 question 匹配器引用。', '从条目或文件加载支持的 DNS 过滤规则子集。它是规则资源，不会自行返回拦截应答；拦截动作需在执行链中配置，不含网页元素过滤。'), 'provider/domain', o('AdGuard 参数', '支持官方文档列出的 DNS 过滤规则子集。', { rules: strings('规则', '逐条添加，例如 ||ads.example^ 或 @@||safe.example^。'), files })),
  plugin('ip_set', 'provider', 'IP 集合', optionCopy('把 IP 和网段集中管理，供多个匹配条件复用。', '多条设备或响应分流规则需要使用同一组地址。', '汇总内联 IP、CIDR、文件和其他 IP 集合，供客户端、响应及 PTR 匹配器引用。它只提供地址匹配资源，不会自动修改系统路由。'), 'provider/ip', o('IP 集合参数', '将可复用的 IP 规则集中管理。', { ips: strings('IP / CIDR', '逐条填写 IP 或网段。'), files, sets: strings('引用集合', '引用具有 IP 匹配能力的 provider tag，例如 ip_set、geoip。') })),
  plugin('geoip', 'provider', 'GeoIP 数据库', optionCopy('从地址数据库提取一组 IP，供分流匹配。', '已有 geoip.dat，并知道数据库中要使用的分类。', '按 code 提取地址与网段，如 cn。可通过附加文件提供数据库，再供 IP 匹配器引用；选中分类不会自行创建代理或路由策略。'), 'provider/ip', o('GeoIP 参数', '加载二进制规则库；可通过附加文件上传数据库。', { file: required(s('数据库文件', 'geoip.dat 路径。')), selectors: strings('分类选择', '填写数据库中的 code，例如 cn。') })),
];
