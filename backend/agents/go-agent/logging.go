package main

import "agent/internal/logx"

// logger 是主包的统一分级日志器，名称遵循 Go 包路径（"agent"）。
// 例行调试日志（心跳、服务状态探测等）使用 Debugf，默认关闭；
// 排查问题时设置 CONFIGFLOW_AGENT_DEBUG=1 或 LOG_LEVEL=DEBUG 打开。
var logger = logx.Get("agent")

// logDebugf 输出例行调试日志，仅在 Debug 级别启用时生效。
func logDebugf(format string, v ...any) {
	logger.Debugf(format, v...)
}

// lastReportedStatus 记录上一次心跳观察到的服务状态，
// 用于只在状态发生变化时输出日志（初始值为空，首次心跳必然记录一次）。
var lastReportedStatus string
