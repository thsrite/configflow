package svc

// Messages printed by the svc subcommands.
const (
	msgUsage          = "用法：configflow-agent svc status|start|stop|restart <程序名>"
	msgUnknownProgram = "%s：本容器没有这个受管程序"
	msgStatusFailed   = "无法读取 %s 的运行状态：%v"
	msgSpawnFailed    = "%s 启动后多次很快退出，已停止自动拉起；请查看日志后再启动"
	msgActionFailed   = "%s %s 失败：%v"
	msgActionDone     = "%s：%s"
	msgExecFailed     = "无法启动 %s：%v"
	msgServeFailed    = "HA 辅助容器控制通道无法启动：%v"
)
