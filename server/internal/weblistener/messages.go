package weblistener

const (
	msgBusy         = "Web 端口正在更新，请稍后重试"
	msgHealthFailed = "Nginx 未能在新端口提供服务，已尝试恢复原端口"
	msgSaveFailed   = "保存 Web 端口失败，已恢复原配置"
)

const (
	msgPort              = "Web 端口须为 1–65535 的整数"
	msgEnvironmentPort   = "%s 须为 1–65535 的整数"
	msgUnreadable        = "无法读取已保存的 Web 端口；可设置 CONFIGFLOW_WEB_PORT 恢复访问"
	msgReservedConflict  = "ConfigFlow 后端与 Sub-Store 须使用不同的端口"
	msgReserved          = "端口 %d 已保留给 %s，请选择其他端口"
	msgOccupied          = "端口 %d 已被占用，请选择其他端口"
	msgListenFailed      = "无法监听端口 %d，请检查权限和网络配置"
	msgUninitialized     = "Web 监听配置尚未初始化，请重启容器"
	msgInconsistent      = "Web 监听配置与保存的端口不一致，请重启容器"
	msgUnsupportedUpdate = "当前部署不支持在 Web 中修改监听端口"
	msgUnsupported       = "当前部署不支持在 Web 中修改监听端口，请通过部署配置调整"
	msgEnvironment       = "端口由 CONFIGFLOW_WEB_PORT 指定；移除该环境变量并重建容器后可在此修改"
)
