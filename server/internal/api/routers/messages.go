package routers

const (
	messageMissing          = "路由器不存在"
	messageBusy             = "此路由器正在发布或同步，请稍后重试"
	messageChanged          = "路由器设置已变化，请刷新后重试"
	messagePolicyOnly       = "策略路由设置仅支持 RouterOS"
	messagePolicyViewOnly   = "策略路由查看仅支持 RouterOS"
	messageWorkspaceFormat  = "策略路由设置格式无效"
	messagePreviewFormat    = "发布预览参数无效"
	messageDraftStale       = "配置已更新，请重新载入后预览。"
	messageTargetsChanged   = "OxiDNS 发布目标或绑定已变化，请重新载入。"
	messagePreviewChanged   = "生成预览期间配置已更新，请重新预览。"
	messageSyncOnly         = "远程规则自动同步仅支持 RouterOS"
	messageSyncFormat       = "请提供同步开关和检查间隔"
	messageSyncFirst        = "请开启自动同步，并先手动预览发布一次"
	messageSyncNotReady     = "同步尚未就绪"
	messagePendingTimeout   = "读取上游路由状态超时，请稍后重试。"
	messageRouteWritten     = "路由变更已写入并通过回读校验"
	messageRouteUnchanged   = "路由没有变化"
	messageForwardFormat    = "请求只能包含 agent_id。"
	messageForwardNotLinked = "这台 Agent 不是当前配置空间中任何路由器的代理网关。"
	messageForwardSaved     = "已为代理网关 Agent %s 开启并持久化 IPv4 转发"
)
