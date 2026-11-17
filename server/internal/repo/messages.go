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
