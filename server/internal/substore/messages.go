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

const (
	msgRuntimeStateShape    = "Sub-Store 运行状态必须是 JSON 对象"
	msgRuntimeStateLastUsed = "Sub-Store 运行状态的 last_used 必须是有限数字"
	msgRuntimeModeInvalid   = "运行模式必须是 on_demand 或 always"
)

const (
	msgDirectYAML       = "订阅 URL 未直接返回有效的 proxies YAML。响应预览: %s"
	msgConvertedYAML    = "Sub-Store 返回了非 YAML 内容（可能是 HTML 页面），请检查 Sub-Store URL 配置是否正确。响应预览: %s"
	msgBodyConversion   = "已拉取的订阅正文无法转换为目标 YAML"
	msgProxyList        = "订阅 proxies 必须是数组，当前值：%s"
	msgProxyObject      = "订阅节点必须是对象，当前值：%s"
	msgProxyType        = "节点 type 必须是字符串，当前值：%s"
	msgTrafficNonfinite = "订阅流量 %s 必须是有限数字"
)

const (
	msgBuiltinPort = "SUB_STORE_BACKEND_API_PORT must be an integer between 1 and 65535"
	msgCommandUTF8 = "Sub-Store 命令输出必须是 UTF-8 文本"
	msgVersionUTF8 = "Sub-Store VERSION 必须是 UTF-8 文本"
)

const (
	msgReleaseShape          = "Sub-Store release 必须是 JSON 对象"
	msgReleaseField          = "Sub-Store release 字段类型无效：%s"
	msgReleaseMissing        = "最新 release 中没有 sub-store.bundle.js"
	msgDependencyDescription = "订阅解析与节点格式转换"
	msgUnexpectedUpdate      = "Sub-Store 更新发生内部错误"
)

const msgManageUnavailable = "无法管理内置 Sub-Store，请检查容器服务状态"

const (
	msgRuntimeDirectory             = "环境变量 %s 指向的 Sub-Store 临时目录「%s」不可用：%v"
	msgRuntimeDirectoryNotDirectory = "路径不是目录"
)

const (
	msgRuntimeDirectoryMissing    = "目录不存在"
	msgRuntimeDirectoryPermission = "目录不可写或无法访问"
	msgRuntimeDirectoryIO         = "文件系统操作失败"
)

const msgRuntimeDirectoryReadOnly = "目录位于只读文件系统"
