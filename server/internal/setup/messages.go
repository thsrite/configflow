package setup

import "errors"

// ErrCorruptRecord refuses unreadable ownership evidence without overwriting it.
var ErrCorruptRecord = errors.New("invalid setup session")

// Error is a wizard validation failure with its public HTTP status.
type Error struct {
	Message string
	Status  int
	Cause   error
}

func (e *Error) Error() string { return e.Message }

// Unwrap retains the original owner failure for errors.Is/errors.As.
func (e *Error) Unwrap() error                 { return e.Cause }
func failure(message string, status int) error { return &Error{Message: message, Status: status} }

const (
	msgSessionID            = "向导记录不存在"
	msgMissingSession       = "向导记录不存在，请重新开始"
	msgBadBody              = "请提交有效 JSON 对象"
	msgLargeBody            = "向导请求过大"
	msgAwaitOwner           = "等待首次使用向导模块"
	msgGoal                 = "请选择使用方式"
	msgNameLength           = "配置名称需为 1–64 个字符"
	msgIdentityConflict     = "配置名称标识冲突，请重新开始"
	msgSessionConflict      = "该向导已创建，请继续原向导或新建一次配置"
	msgProgressFields       = "向导进度参数无效"
	msgProgressOrder        = "请先完成前面的步骤"
	msgRouterMode           = "请选择有效的路由器配置方式"
	msgRouterSelection      = "请选择当前配置空间中的路由器"
	msgSourcePrepared       = "方案已生成；修改来源请前往订阅来源或创建新的向导"
	msgSourceSelection      = "请选择有效的订阅或节点"
	msgSubscriptionFormat   = "订阅链接格式不正确"
	msgHTTPSubscription     = "请输入 HTTP(S) 订阅链接"
	msgSubscriptionName     = "订阅"
	msgSourceUnavailable    = "所选来源已删除或停用，请刷新后重选"
	msgUnlinkedSubscription = "所选订阅未填写订阅链接，请改用订阅链接添加"
	msgNeedSource           = "请添加订阅链接或选择已有节点"
	msgRouterDraftPhase     = "请在安装等待期间填写主路由信息；安装完成后使用路由器管理"
	msgRouterDraftFields    = "主路由连接参数无效"
	msgRouterDraftBinding   = "安装完成后自动关联本次设备，暂不启用规则自动同步"
	msgRouterDraftPolicy    = "新配置空间还没有设为智能分流的客户端设备，暂不能使用策略路由；完成向导后先在「客户端设备」中设置，再到上游路由中启用"
	msgGlobalDirect         = "🎯 全球直连"
	msgProxyFirst           = "优先代理"
	msgNodeSelection        = "✈️ 节点选择"
	msgUnmatched            = "🐟 漏网之鱼"
	msgBuiltinNameSuffix    = "（内置）"
	msgBuiltinNumberedName  = "%s（内置 %d）"
)

const (
	msgTemplateSecret = "不能使用初始模板的默认密码 123456，请自己设置一个"
	msgSecretFormat   = "控制面板密码需为 8–64 个字母、数字或符号，不含空格"
	msgSecretRequired = "请设置控制面板密码：Mihomo 控制面板（external-controller）需要用它防止他人访问"
	msgBaseParse      = "Mihomo 基础 YAML 无法解析，请先修正基础配置"
	msgBaseMapping    = "Mihomo 基础配置必须是 YAML 对象"
	msgEmptyBase      = "基础配置为空，已使用初始模板"
	msgNameservers    = "dns.nameserver 为空，已补入 "
	msgSecretSet      = "控制面板密码（secret）已设置"
	msgSecretUpdated  = "控制面板密码（secret）已更新"
	msgDNSSet         = "dns.%s 设为 %s"
	msgDNSChanged     = "dns.%s 由 %s 改为 %s"
	msgModeChanged    = "mode 由 %s 改为 rule"
	msgTunMapping     = "Mihomo tun 必须是对象，请先修正基础配置"
	msgFullTUN        = "tun 设为完整 TUN（enable、auto-route、auto-redirect、auto-detect-interface，由同机 OxiDNS 处理 DNS，dns-hijack 为空）"
	msgRemoved        = "，移除 "
	msgTunFilter      = "Linux 网关与现有 tun.%s 过滤冲突，请先在 Mihomo 基础 YAML 中移除该过滤，避免客户端绕过接管"
	msgTunDisabled    = "tun 已停用，由 Linux 网关启用时接管"
	msgPortCollision  = "Mihomo 基础配置的 %s 使用端口 %s，与%s冲突；请先在 Mihomo 基础配置中改用其他端口"
)

const (
	msgPrepareFields     = "漏网处理参数无效"
	msgRemovedTemplate   = "代理优先模式已移除，请刷新向导并分别选择 DNS 与连接的漏网处理"
	msgFallback          = "请选择有效的 DNS 与连接漏网处理方式"
	msgQueryRecorder     = "DNS 查询记录开关必须为布尔值"
	msgLocalDNS          = "本地 DNS 必须为文本"
	msgDomainSelection   = "请选择有效的直连和代理域名规则集"
	msgDomainOptions     = "域名规则集必须是有效的选项列表"
	msgDomainOverlap     = "同一个域名规则集不能同时设为直连和代理"
	msgDomainUnavailable = "所选域名规则集已删除、停用或不支持，请刷新后重选"
	msgPrepareSource     = "所选来源已删除或停用，请返回检查节点来源"
	msgChangedSourceURL  = "订阅链接已变化，请重新读取订阅"
	msgNoNodes           = "当前没有可用节点，请先读取订阅或选择支持的节点"
	msgMihomoDNSPort     = " Mihomo DNS %s "
	msgOxiDNSPort        = " OxiDNS 监听 %s "
	msgOxiDNSAPIPort     = " OxiDNS 管理 API %s "
	msgBaseText          = "Mihomo 基础配置必须为 YAML 文本"
	msgForeignEdit       = "该空间已在其他页面修改，向导不会覆盖；请新建向导或前往配置生成继续"
	msgProfileChanged    = "配置已更新，请刷新向导后重试"
	msgGeneratedStrategy = "生成配置未包含有效代理策略，请检查来源"
)

const (
	msgOldAgent               = "请将 Mihomo Agent 更新到 1.8.40-go 或更新版本后再检查"
	msgForwardingOff          = "Linux IPv4 转发未开启，请在安装与检查中点击「开启并持久化 IPv4 转发」"
	msgForwardingNotPersisted = "IPv4 转发尚未持久化，重启后可能失效；请在安装与检查中点击「开启并持久化 IPv4 转发」"
	msgGatewayRemaining       = "设备上仍有 Linux 网关的转发记录，请先在配置生成页的 Linux 网关中撤销后再检查"
	msgPublishedGateway       = "已发布的 Mihomo 仍是 Linux 网关配置，请重新发布"
	msgTUNNotConfigured       = "已发布的 Mihomo 未启用完整 TUN，请检查基础 YAML 后重新发布"
	msgTUNNotRunning          = "Mihomo TUN（%s）未运行，请检查 Mihomo 日志"
	msgNeedPublish            = "请先在安装向导完成 Mihomo、OxiDNS 的发布与检查；配置变化后需重新发布"
	msgNeedPair               = "两个核心的设备尚未确认，请继续安装向导"
	msgAgentOffline           = "安装设备离线或已禁用，请检查连接后再检查"
	msgGatewayUnhealthy       = "Linux 转发尚未启用或健康状态未确认，请继续安装向导"
	msgRuleSet                = "规则集"
	msgMakeDefault            = "请选择是否设为默认配置空间"
	msgMissingRoutes          = "主路由转发规则集「%s」的 IP 段仍未下载，静态路由说明不完整。请检查 ConfigFlow 服务器能否访问规则地址后重新检查。"
	msgNewRoutes              = "主路由转发规则集「%s」的 IP 段已下载，静态路由表有新增网段。请重新检查，按更新后的列表配置主路由后再确认。"
	msgAcknowledgeRoutes      = "请先按说明配置主路由的静态路由与 DNS，再确认已完成"
	msgRouterPair             = "请选择本次 Mihomo 作为代理网关；DHCP DNS 选择本次 OxiDNS，或在其他设备提供 DHCP 时选择不关联"
	msgRouterUnsynced         = "路由器配置尚未同步，请在差异报告中预览并完成发布"
	msgRouterChanged          = "路由器设置在检查期间已变化，请重新检查"
	msgFinishRouterMode       = "请选择路由器配置方式"
	msgCoreChanged            = "核心配置在检查期间已变化，请重新发布后检查"
)

const (
	msgNeedPrepare              = "请先生成配置方案"
	msgUnknownOperation         = "未知向导操作"
	msgAbandonBody              = "请提交有效的放弃请求"
	msgDraftConflict            = "主路由记录已在其他页面修改，请检查路由器设置"
	msgAlreadyCompleted         = "向导已完成，无需放弃"
	msgEtAl                     = " 等"
	msgBoundDevices             = "已有设备在使用它（%s%s）"
	msgBoundGuidance            = "如不再需要，先在 Agent 页解绑或删除这些设备，再到「配置空间」删除。"
	msgKeepRouters              = "已添加主路由，路由器上的改动需要在这里管理或撤销"
	msgKeepRoutersGuidance      = "如不再需要，先在「上游路由」撤销改动并删除路由器，再到「配置空间」删除。"
	msgKeepInstallation         = "设备安装已经开始，安装命令仍可能被执行"
	msgKeepInstallationGuidance = "如不再需要，可在 Agent 页确认没有新设备后，再到「配置空间」删除。"
	msgKeepDefault              = "它是当前的默认配置空间"
	msgKeepDefaultGuidance      = "如不再需要，先在「配置空间」把其他配置设为默认，再删除它。"
	msgKeepRacedGuidance        = "如不再需要，请处理上述占用后再到「配置空间」删除。"
)
