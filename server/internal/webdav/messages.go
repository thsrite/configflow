package webdav

const (
	msgManual          = "手动"
	msgAutomatic       = "自动"
	msgIncomplete      = "请填写完整的 WebDAV 配置"
	msgUnsafePath      = "WebDAV 地址和备份路径不能包含 . 或 .. 路径段，请改用不含 . 或 .. 的普通目录"
	msgCreateDirectory = "无法创建目录 %s：%s"
	msgUploaded        = "WebDAV %s备份完成：%s%s（%.1f KB）"
	msgListFailed      = "WebDAV 旧备份清理失败：无法列出备份目录（%s）"
	msgDeleteFailed    = "WebDAV 旧备份 %s 删除失败（%s）"
	msgPruned          = "WebDAV 已清理 %d 个旧备份（保留最近 %d 份及最近 %d 天每天最后一份）"
	msgAutoFailed      = "WebDAV 自动备份失败（%s）；已保存的配置不受影响"
	msgScheduleFailed  = "无法安排 WebDAV 自动备份；已保存的配置不受影响"
	msgUnexpected      = "WebDAV 自动备份任务异常终止；已保存的配置不受影响"
)
