package main

const (
	msgInitialized        = "✅ 应用初始化完成"
	msgInvalidGrace       = "CONFIGFLOW_SHUTDOWN_GRACE_SECONDS must be a positive number of seconds"
	msgStoppedBeforeStart = "等待数据目录时收到停止信号，服务未启动"
	msgSignalDuringSave   = "正在保存监控历史，忽略重复的停止信号；保存完成后再次发送可强制退出"
)
