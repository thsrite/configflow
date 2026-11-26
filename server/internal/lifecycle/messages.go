package lifecycle

const (
	msgTaskPanic       = "后台任务 %q 异常退出"
	msgRoundFailed     = "周期任务 %q 执行失败：%s"
	msgStartupFailed   = "启动任务 %q 执行失败：%s"
	msgDrainFailed     = "等待请求结束失败：%s"
	msgInvalidInterval = "周期任务 %q 的间隔必须大于零"
	msgShutdownTimeout = "后台任务未在关机宽限期内结束"
	msgServiceTimeout  = "常驻服务未在关机宽限期内结束"
	msgReadyFailed     = "写入测试就绪标记失败：%v"
	msgEventsFailed    = "写入测试任务事件失败：%v"
)
