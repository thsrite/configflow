package repo

const (
	msgStoreRecovery       = "无法加载配置数据库 %q：%s；请先停止服务并保留原文件，再从可用备份恢复；或使用最后一个能正常读取此库的版本导出全部数据后恢复。原数据库未改动。"
	msgModelFieldIssue     = "配置字段类型不匹配，已按缺省值解码：%s（%s）"
	msgModelRowInvalid     = "字段 %s/%s/%s 无法按配置模型读取（成员名重复或不是有效 JSON）：%w"
	msgStoreDocumentObject = "配置文档必须是对象"
	msgStoreSectionObject  = "配置节 %s/ 必须是对象"
	msgStoreProfilesObject = "profiles 必须是对象"
	msgStoreProfileObject  = "配置节 profile/%s 必须是对象"
)

const msgRouterJobBusy = "路由器正在发布或同步，不能替换连接或删除记录，请稍后重试"

// Stored-document checks shared by load and write validation.
const (
	msgSchemaMismatch         = "配置存储版本不匹配"
	msgSectionMissing         = "缺少配置节："
	msgDefaultProfileRequired = "必须保留默认配置"
	msgProfileIDMismatch      = "配置内容与 ID 不一致"
	msgRevisionInvalid        = "配置修订号无效"
)

// msgCommittedHookSkipped is logged when the committed document cannot be
// handed to the ConfigurationCommitted hook.
const msgCommittedHookSkipped = "配置已保存，但无法交给自动备份：%v"

// Old-format import conversion.
const (
	msgLegacyShape             = "旧配置格式无效：资源列表与条目必须是数组和对象"
	msgLegacyRuleName          = "旧配置的规则名称必须是字符串"
	msgLegacyDialerProxyString = "旧配置节点的 proxy_string 无法添加拨号代理"
)

// msgRouterIDInvalid refuses a router record without a scalar id.
const msgRouterIDInvalid = "路由器 ID 无效"

// msgImportResultShape refuses an import builder result of the wrong shape.
const msgImportResultShape = "导入结果格式无效"

const (
	msgRouterIDImmutable     = "不能修改路由器 ID"
	msgRouterSettingsChanged = "路由器设置已变化，请刷新后重试"
)
