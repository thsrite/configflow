package substore

const (
	msgInvalidYAML       = "订阅 YAML 无法解析：%w"
	msgUpdateNoBuiltin   = "当前环境没有内置 Sub-Store，无法在线更新"
	msgUpdateBusy        = "已有更新任务在进行中"
	msgUpdateUnavailable = "Sub-Store 正忙或服务不可用，请稍后重试"
	msgLatestUnavailable = "无法获取最新版本"
	msgAlreadyLatest     = "当前已是最新版本 %s"
	msgPreparingUpdate   = "准备更新到 %s…"
	msgStartingUpdate    = "开始更新到 %s"
)

const msgRuntimeBusy = "Sub-Store 正忙或正在更新，请稍后重试"

const (
	msgCacheDir            = "创建订阅缓存目录失败: %v"
	msgCacheDelete         = "删除订阅缓存失败 (%s): %v"
	msgCachePrune          = "检查订阅缓存失败，跳过清理: %v"
	msgCacheSubscriptionID = "订阅 id 必须是非空字符串"
	msgCacheOrphan         = "删除孤立订阅缓存失败 (%s): %v"
	msgCachePruned         = "已清理 %d 个已删除订阅的节点缓存"
	msgCacheWrite          = "写入订阅缓存失败 (%s): %v"
	msgCacheRead           = "读取订阅缓存失败 (%s): %v"
	msgCacheCount          = "订阅 %s 节点数变化：%d → %d"
	msgCacheHosts          = "订阅 %s 的 hosts 有变化：新增 %d 条、删除 %d 条、修改 %d 条；重新发布 Mihomo 配置后生效"
	msgCacheInvalidator    = "作废 Agent 内容核对缓存失败"
)

const (
	msgCacheSubscriptionsShape = "检查订阅缓存失败，跳过清理: subscriptions 必须是数组"
	msgCacheRootShape          = "缓存必须是 JSON 对象"
)
