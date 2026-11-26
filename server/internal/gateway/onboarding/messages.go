package onboarding

const (
	msgFields            = "安装设置必须是 JSON 对象，且只能包含向导字段"
	fmtIDs               = "%s 必须为有效规则 ID 数组"
	msgOverlap           = "同一域名规则集不能同时选择直连和代理"
	fmtBoolean           = "%s 必须为布尔值"
	msgRevision          = "配置版本无效，请刷新安装向导"
	msgDefaultForward    = "请选择有效的未匹配域名 DNS"
	msgLocalDNS          = "请填写直连 DNS，使用 IP 地址的 DoH / DoT，例如 https://223.5.5.5/dns-query"
	msgLocalLimit        = "直连 DNS 最多支持 16 个上游"
	msgUnsafeDNS         = "直连 DNS 必须使用 IPv4 地址的 DoH / DoT，不能指向本机、53 / 1053 端口或代理出口"
	msgNativeListener    = "原生 DNS 监听入口使用自定义处理链，无法确认网关分流路径"
	msgNativeProxy       = "原生 proxy_dns_seq 使用自定义处理链，无法确认 Mihomo 上游"
	msgNativeFallback    = "原生 fallback 使用自定义处理链，无法确认 Mihomo 代理与备用上游"
	msgNativeSecondary   = "原生 fallback_dns 使用自定义处理链，无法确认 Mihomo 上游"
	msgNativeRemote      = "原生 remote_dns 使用自定义处理链，无法确认 Mihomo 上游"
	msgNativeInclude     = "原生备用 DNS 含 include，无法安全准备网关上游"
	msgNativePlugin      = "原生备用 DNS 使用自定义插件，无法安全准备网关上游"
	msgNativeReferences  = "原生备用 DNS 仍有自定义或动态引用，无法安全准备网关上游"
	msgWaitingSetup      = "等待首次使用向导模块"
	msgWaitingDNS        = "等待共享 DNS 回环校验模块"
	msgLocalAddresses    = "Linux 本机地址无效，请重新核对安装网络信息"
	msgBaseText          = "Mihomo 基础配置必须为 YAML 文本"
	msgBaseMapping       = "Mihomo 基础配置及 dns 必须是 YAML 对象，请先修正基础配置"
	msgDNSLoop           = "Mihomo DNS 上游不能指向本机 OxiDNS 或系统 DNS，否则会形成解析循环；请先修改基础配置"
	msgLegacy            = "此配置空间使用旧版 Linux 网关接管，保持不变。"
	msgLibraryDeleted    = "共享规则库条目已删除"
	msgExistingIP        = "现有 IP 分流选择会保留；安装向导只能新增域名或 Classical 规则集"
	msgMRS               = "OxiDNS 转换不支持 MRS，请改用 text / YAML 规则集"
	msgDisabled          = "规则集已停用，不会出现在新配置中"
	msgMissing           = "规则集已删除或不属于当前配置空间"
	msgClassic           = "混合规则中的域名和 IP 条件都会参与 OxiDNS 分流"
	msgDomain            = "按域名选择直连或代理 DNS"
	msgMissingChoice     = "不存在或不属于当前配置空间"
	msgUnavailableChoice = "所选域名规则集不可用："
	msgReadNative        = "现有 OxiDNS 无法安全读取："
	msgConfirmRead       = "。请修正原生配置，或明确确认转换为模板。"
	msgConvertedRead     = "已按明确选择转换 OxiDNS 为模板；原始配置保存在 gateway_onboarding_native_backup 中。"
	msgUpdateNative      = "现有 OxiDNS 自定义结构无法安全更新："
	msgConfirmUpdate     = "。请在完整参数中调整，或明确确认转换为模板；不会自动覆盖原生配置。"
	msgConfirmState      = "。请在完整参数中调整，或明确确认转换为模板。"
	msgConvertedUpdate   = "已按明确选择转换 OxiDNS 为模板；原生配置和附加文件保存在 gateway_onboarding_native_backup 中，可从配置导出恢复。"
	msgQueryRecorder     = "OxiDNS 查询记录使用自定义结构，向导未修改；请在 OxiDNS 设置的「特色功能」中调整。"
	msgHalfAccount       = "OxiDNS 管理 API 原有账号只填写了一半，已清空；如需认证，请在 OxiDNS 设置「API / 日志」中同时填写用户名和密码。"
	msgHalfTLS           = "OxiDNS 管理 API 的 TLS 证书和私钥只填写了一项，已清除，API 使用 HTTP。"
	fmtAPIListen         = "OxiDNS 管理 API 原监听 %s 且未设置账号，已改为仅本机 127.0.0.1:9199；如需局域网访问，请先在 OxiDNS 设置「API / 日志」中设置用户名和密码。"
	msgValidation        = "OxiDNS 配置校验未通过，请根据 validation.oxidns.issues 修正后重试"
	msgNoProxy           = "当前配置没有被启用规则引用的可用代理出口；请先添加节点或订阅、配置策略组，并为需要代理的规则选择该策略"
	msgNoMatch           = "当前未配置 Mihomo MATCH 漏网规则，请在规则页确认未匹配连接的处理方式。"
	msgBadMatch          = "Mihomo MATCH 漏网规则未指向可用代理策略或 DIRECT，请确认未匹配连接的处理方式。"
	fmtPolicy            = "规则集「%s」当前策略为 %s；DNS 或%s不会自动把此策略改为代理。"
	msgPolicyMissing     = "未设置"
	msgIPCapture         = " IP 接管"
	msgRouterForward     = "主路由转发"
	msgServerLoopback    = "系统设置中的 ConfigFlow 服务地址是本机回环地址，Linux 无法刷新规则；请先改为 Linux 能访问的局域网地址或域名，并在安装向导使用同一地址。"
	msgLocalWarning      = "当前 DNS 不适用于网关，请改用 IP 地址的 DoH / DoT，例如 https://223.5.5.5/dns-query。"
	msgPrepareProxy      = "请先准备代理节点或订阅、策略组及对应规则；预览会检查生成配置中的代理出口。"
	msgStale             = "配置或共享资源已更新，请刷新安装向导并重新预览后保存"
	msgTakeoverChanged   = "配置已更新，请刷新安装向导并重新预览后保存"
	msgReadyGateway      = "主路由分流不能同时启用 Linux 网关，请重新预览并保存"
	msgReadyTUN          = "Mihomo 基础 YAML 缺少完整 TUN，请重新预览并保存"
	msgReadySave         = "请先在安装向导预览并保存网关配置"
	msgReadyListen       = "已保存的 OxiDNS 监听、IPv4 或默认分流不符合安装方案，请重新预览并保存"
	msgReadyDefault      = "已保存的 OxiDNS 默认 DNS 无效，请重新预览并保存"
	msgReadyUpstreams    = "OxiDNS 代理和备用上游必须均为 127.0.0.1:1053，请重新预览并保存"
	msgReadyRouting      = "OxiDNS 自定义分流结构无法确认，请回到安装向导重新预览"
	msgReadyValidation   = "已保存的 OxiDNS 配置校验未通过，请修正后重新准备安装"
	msgReadyNameserver   = "Mihomo 缺少 DNS 上游，请重新预览并保存网关配置"
	msgReadyProxy        = "当前配置没有被启用规则引用的可用代理出口，请先完善节点和规则"
)

var gatewayChanges = []string{
	"启用当前配置空间的 Linux 网关；fake-IP 网段始终接管，真实 IP 只接管勾选的规则集。",
	"Mihomo 使用 rule / fake-IP 模式，DNS 监听 127.0.0.1:1053；保留现有规则顺序、策略组和节点。",
	"OxiDNS 在 0.0.0.0:53 同时监听 UDP / TCP，优先 IPv4；直连使用所填 IP 地址 DoH / DoT。",
	"OxiDNS 代理和备用上游均为 127.0.0.1:1053；未匹配域名使用已选择的默认 DNS。",
	"OxiDNS 管理 API 未启用时自动开启（仅本机 127.0.0.1:9199，不设账号），供 DNS 面板、规则更新和热重载使用；已启用的监听与认证设置不变。",
	"只保存两份配置草稿；后续分别发布并检查核心，再显式应用主机转发。",
}
var routerChanges = []string{
	"Mihomo 使用完整 TUN 接管主路由转来的连接，不启用 Linux 网关的选择性接管。",
	"安装时开启并持久化 IPv4 转发、准备 TUN，与普通同机安装一致。",
	gatewayChanges[1], gatewayChanges[2], gatewayChanges[3], gatewayChanges[4],
	"主路由只需转发 fake-IP 网段和所选「主路由转发」规则集；只保存两份配置草稿，之后依次发布并检查转发与 TUN。",
}
