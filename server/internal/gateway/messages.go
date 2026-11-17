package gateway

// User-facing messages (byte-identical to common/linux_gateway.py; the
// frontend shows them and tests match on them).
const (
	msgSettingsNotObject = "Linux 网关设置必须是对象"
	msgEnabledNotBool    = "enabled 必须是布尔值"
	msgInvalidRuleSetIDs = "请选择有效的 IP 规则集，最多 1024 个"

	msgBaseYAMLInvalid = "Mihomo 基础 YAML 无法解析，请先修正基础配置"
	msgBaseNotMapping  = "Mihomo 基础配置必须是 YAML 对象"
	msgDNSNotObject    = "Mihomo dns 必须是对象"
	msgFakeRange       = "fake-ip-range 必须是有效 IPv4 网段（/8 至 /30），不能是回环、多播或链路本地地址"

	reasonDeleted         = "规则集已删除或不属于当前配置空间"
	reasonLibraryDeleted  = "共享规则库条目已删除"
	reasonBehavior        = "仅支持 IP-CIDR 或 Classical 规则集"
	reasonMRS             = "网关提取暂不支持 MRS，请使用 text / YAML 格式的 IP 规则集"
	reasonDisabled        = "当前配置空间已停用此规则集，接管范围将清空"
	reasonLibraryDisabled = "共享规则库已停用此规则集，接管范围将清空"

	msgContentTooLarge = "IP 规则内容必须为 UTF-8 文本且不超过 16 MiB"
	msgContentBinary   = "IP 规则不是有效文本，暂不支持二进制规则"
	msgYAMLSyntax      = "IP 规则集 YAML 语法错误"
	msgYAMLPayload     = "IP 规则集 YAML 必须包含字符串数组 payload"
	msgTooManyRules    = "IP 规则数量超过 250000 条限制"

	msgExtractBehavior = "网关仅支持 IP-CIDR / Classical 规则集"
	msgClassicalShape  = "Classical 目的 IP 规则只能包含 IP-CIDR,网段[,no-resolve]"
	msgIPCIDRShape     = "IP-CIDR 规则集中只能填写 IP / CIDR，不能包含其他匹配条件"
	msgInvalidCIDR     = "IP 规则包含无效 IP / CIDR"
	msgVersionMismatch = "IP-CIDR / IP-CIDR6 规则类型必须与目标 IP 版本一致"
	msgPrefixTooShort  = "Linux 网关只接受 /8 至 /32 的 IPv4 网段，请拆分或移除 /0、/1 等超大接管范围"
	msgFullCapture     = "所选 IP 规则覆盖全部 IPv4 地址，无法用于 Linux 网关"
	msgUnreadable      = "无法读取 IP 规则集，请先在规则库刷新并检查下载状态"

	// fmtRoutingMark: the path of the first routing-mark found.
	fmtRoutingMark  = "Linux 网关与 %s 冲突，请先移除 routing-mark"
	msgTunNotObject = "Mihomo tun 必须是对象"
	// fmtTunFilter: the TUN filter key.
	fmtTunFilter          = "Linux 网关与现有 tun.%s 过滤冲突，请先在 Mihomo 基础 YAML 中移除该过滤，避免客户端绕过接管"
	msgProvidersNotObject = "Mihomo rule-providers 必须是对象"
	msgReservedPrefix     = "cf_gateway_ 是网关内部规则集前缀，请修改同名前缀的自定义规则集"
	msgTokenRequired      = "已开启登录验证，请先在系统设置中配置订阅令牌（config_token），供 Mihomo 自动刷新网关规则"

	// Roots of the routing-mark paths in fmtRoutingMark.
	pathMihomo = "Mihomo"
	pathNodes  = "节点"

	msgSelectedMissing = "所选规则集不存在或不属于当前配置空间"
	msgSelectBehavior  = "只能选择 IP-CIDR / Classical 规则集"

	// fmtOverlap: taken set, later direct rules (joined by "、"), number of
	// overlapping ranges, example, taken set again.
	fmtOverlap = "IP 接管「%s」与后面的直连规则「%s」有 %d 段地址重叠（如 %s）。" +
		"这些地址的连接会先命中「%s」，部分直连网站可能会走代理。"
	fmtOverlapSame     = "%s 两边都有"
	fmtOverlapCovers   = "%s 覆盖 %s"
	fmtOverlapPartial  = "%s 与 %s 重叠"
	fmtOverlapUnread   = "未能读取「%s」的内容，没有检查 IP 接管与它们的重叠；可在规则库刷新后重新预览。"
	overlapNameJoiner  = "、"
	descriptionClassic = "只提取无条件目标 IP-CIDR；忽略域名、来源 IP、逻辑规则及 IPv6"
	descriptionIPCIDR  = "只接管 IPv4 IP / CIDR"
	reasonMissing      = "规则集已删除或不属于当前配置空间，接管范围将清空"
	// fmtCandidateWarning: rule set name, reason.
	fmtCandidateWarning = "%s：%s"
	// fmtMissingWarning: reasonMissing, rule set ID.
	fmtMissingWarning = "%s（%s）"
)

// guidance are the fixed notes at the top of the editor's warnings.
var guidance = []string{
	"仅支持 Linux + nftables 的 IPv4 网关；客户端需停用 IPv6 或另行配置 IPv6 路由。",
	"OxiDNS 直连上游请使用带 IP 地址的 DoH / DoT（例如 https://223.5.5.5/dns-query）；普通 UDP/TCP 53 可能被 auto-redirect 接管。",
	"这里只决定哪些 IP 进入 Mihomo；启用后使用 rule 模式，实际出口仍由规则顺序及策略组当前选择决定。",
	"IPv4 接管网段须为 /8 至 /32；/0、/1 等超大范围会被拒绝，避免意外接管全网。",
}
