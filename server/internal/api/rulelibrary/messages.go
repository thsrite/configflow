package rulelibrary

const (
	msgObject         = "规则库请求必须是 JSON 对象"
	msgID             = "规则库条目必须有 ID"
	msgDuplicate      = "规则库 ID 已存在，请刷新后重试"
	msgSource         = "请提供有效的规则来源和订阅地址"
	msgDownloadCreate = "规则源下载失败，未添加规则，请检查来源后重试"
	msgSaveCreate     = "规则保存失败，未添加规则，请稍后重试"
	msgDownloadEdit   = "规则源下载失败，未修改规则，原配置与有效缓存已保留"
	msgDraftMissing   = "规则集已不存在，请重新载入。"
	msgDraftBody      = "请提交规则内容及读取时的版本。"
	msgDraftConflict  = "规则集已在其他页面更新，请重新读取后再编辑。"
	msgDraftRemote    = "订阅规则由订阅源更新，请使用内容型规则集添加手动 IP。"
	msgCheckBody      = "请提交规则类型和文本内容。"
	msgAppend         = "新增规则应为单行文本列表。"
	msgCacheSelect    = "请选择要缓存的规则"
	msgCacheMissing   = "未找到要缓存的规则"
	msgOrderMissing   = "以下规则仓库 id 不存在: %s"
	msgCacheFailed    = "规则缓存更新失败，请稍后重试。"
)
