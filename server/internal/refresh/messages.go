package refresh

const msgTaskBusy = "这个任务正在运行，请稍后查看结果"

const msgSourceYAML = "订阅 YAML 校验失败：%v"

const (
	msgTrafficFailed     = "读取订阅流量信息失败: %s"
	msgHealthWriteFailed = "写入订阅健康记录失败: %s"
)

const (
	msgIntervalRange = "必须为 0（关闭自动更新）或 60 ~ "
	msgIntervalUnit  = " 秒的整数"
)

const (
	msgSourceIdentity = "订阅来源缺少有效 ID 或地址"
	msgNoSourceNodes  = "订阅没有返回任何节点，已保留原有缓存"
	msgNoManualCache  = "订阅「%s」已关闭自动更新，且没有当前订阅地址的可用缓存。请先在「订阅来源」中手动更新，再重新发布或同步。"
)

const (
	msgRuleIdentity       = "规则来源缺少有效 ID"
	msgRetainedNodes      = "，继续使用原有缓存"
	msgRetainedRule       = "，继续使用上一次有效缓存"
	msgSubscriptionFailed = "订阅「%s」更新失败%s：%s"
	msgRuleFailed         = "规则集「%s」更新失败%s：%s"
	msgRuleRefreshFailed  = "规则集更新失败"
	msgRefreshPanic       = "scheduled refresh %s panicked"
	msgSourceGone         = "任务对应的来源已不存在，请刷新后重试"
	msgNoSourceAddress    = "这个来源没有订阅地址，不需要更新"
	msgUpstreamStatus     = "上游返回 HTTP %d"
	msgInvalidSourceURL   = "地址无效，请检查是否为完整的 http(s) 地址"
	msgSourceTLS          = "TLS 证书校验失败"
	msgSourceTimeout      = "连接上游超时"
	msgSourceConnection   = "无法连接上游"
	msgRefreshFailed      = "更新失败（%s）"
)

const (
	msgNoURL                = "没有订阅地址"
	msgSubscriptionDisabled = "订阅已停用"
	msgRuleDisabled         = "规则集已停用"
	msgRoundPanic           = "Scheduled refresh round panicked"
	msgRoundFailed          = "Scheduled refresh round failed: %s"
)

const msgSubscriptionFetchUnknown = "更新失败（SubscriptionFetchError）"
