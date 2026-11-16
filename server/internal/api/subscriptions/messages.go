package subscriptions

const (
	msgSubscriptionObject = "订阅请求必须是对象"
	msgRequiredField      = "订阅 %s 不能为空"
	msgOperationFailed    = "订阅操作失败"
	msgProviderFailed     = "获取订阅代理列表失败"
	msgMissingIDs         = "以下订阅 id 不存在: %s"
	msgFetchNoCache       = "从订阅URL获取失败: %s，且本地无缓存数据"
	msgProviderNoCache    = "订阅 '%s' 没有缓存数据，且从 Sub-Store 获取失败: %s"
	msgEmptyCache         = "订阅 '%s' 缓存中没有节点"
	msgUnknownError       = "未知错误"
	msgUnsupportedChain   = "%s 暂不支持代理链，请使用 Mihomo"
)
