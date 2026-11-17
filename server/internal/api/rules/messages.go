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
