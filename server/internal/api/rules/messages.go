package rules

const (
	msgRuleObject       = "规则请求必须是 JSON 对象"
	msgUDPFallback      = "节点不支持 UDP 时只能选择跟随客户端默认、直连或拒绝"
	msgNTP              = "对时始终直连只能是开启或关闭"
	msgLibraryReference = "规则集必须引用存在的共享规则库条目"
	msgSingleLine       = "规则值必须是单行文本。"
	msgUnknownKind      = "未知规则类型 %s。"
	msgMosDNSGuard      = "此规则已用于 MosDNS %s，%s 类型暂不能转换为 MosDNS 规则。请先在 MosDNS 设置中取消选择，或新建一条规则。"
	msgOxidnsGuard      = "此规则已用于 OxiDNS DNS 处理「%s」，%s 类型不参与 DNS 分流。请同时把 DNS 设为默认上游，或新建一条规则"
	msgDNSRoutes        = "请提供要修改 DNS 处理的规则"
	msgDNSLimit         = "一次最多修改 200 条规则的 DNS 处理"
	msgGroupBody        = "请提供规则 ID 列表和组名称"
	msgIDs              = "规则 ID 列表不能为空或包含重复项"
	msgGroupMissing     = "部分规则已被删除，请刷新列表后再重命名"
	msgPolicyBody       = "请提供规则 ID 列表和策略"
	msgPolicy           = "请选择策略"
	msgGroupText        = "组名称必须是文本"
	msgPolicyMissing    = "部分规则已被删除，请刷新列表后再修改"
	msgPolicyUnknown    = "策略不存在：%s"
	msgBatchValues      = "请提供规则值列表"
	msgLineError        = "第 %d 行：%s"
	msgWithheldWarning  = "Checking withheld pinned upstreams failed; committed save retained"
)

const (
	msgDNSChoice    = "请选择有效的 DNS 处理"
	msgNoBody       = "No request body provided"
	msgOrderMissing = "以下规则 id 不存在: %s"
)

const msgDNSUnsupported = "不支持的 DNS 处理：%s"

const (
	msgComposeObject        = "请求必须是 JSON 对象"
	msgPosition             = "位置只能是 top 或 bottom"
	msgPaste                = "请输入规则"
	msgPasteLimit           = "一次最多添加 200 条，较多条目请改为新建规则集"
	msgNoRules              = "没有可添加的规则"
	msgIgnorePolicy         = "忽略第 %d 行行尾策略 %s"
	msgWildcard             = "%s 将按 DOMAIN-SUFFIX 添加，也包含 %s 本身"
	msgUnsupportedLineDNS   = "第 %d 行 %s 规则不参与 DNS 分流，请把 DNS 设为默认上游"
	msgSelectLibraries      = "请选择 1 到 50 个规则集"
	msgLibrarySource        = "请提供新规则集的来源"
	msgLibraryName          = "请填写规则集名称"
	msgRuleSourceKind       = "请选择规则来源：values、library 或 new_library"
	msgDownloadFailed       = "规则源下载失败，未添加规则，请检查来源后重试"
	msgLibraryExists        = "规则库已有同名规则集「%s」"
	msgLibraryUsed          = "该规则集已在策略规则 #%d"
	msgInvalidSource        = "请提供有效的规则来源和订阅地址"
	msgComposedCacheWarning = "规则集已保存，但本地缓存失败，发布时会重新获取"
	msgComposedCacheLog     = "Caching composed rule library failed; committed save retained"
)

const (
	msgLocalFailed     = "Error getting local rule"
	msgLocalMissing    = "Rule not found in rule library: %s. Please add it to the rule library first."
	msgLocalReview     = "Rule '%s': %d entries need manual review (lines [%s])"
	msgLocalUTF8       = "Rule '%s' is not UTF-8; leaving rule content unchanged"
	msgQuery           = "请输入域名或IP地址"
	msgQueryInvalid    = "请输入有效的域名或IP地址"
	msgUnmatched       = "该域名/IP未匹配任何规则，将使用默认策略"
	msgMatchFailed     = "查询失败"
	msgDuplicateFailed = "查重失败"
	msgDirectRules     = "直接配置的规则"
	msgRuleset         = "规则集"
)

const (
	msgOrderObjects = "rule_configs must be a list"
	msgOrderIDs     = "ids must be a list"
)
