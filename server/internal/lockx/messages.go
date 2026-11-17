package lockx

const (
	msgOrderViolation = "锁顺序违规：持有 %q（级别 %d）时等待 %q（级别 %d）"
	msgFlightPanic    = "共享任务 %q 异常退出"
	msgMissingSpawner = "未配置 spawner"
)
