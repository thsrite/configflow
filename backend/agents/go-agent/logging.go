package main

import (
	"log"
	"os"
	"strconv"
)

// debugEnabled 控制例行日志（心跳、服务状态探测）是否输出。
// 这些日志每个心跳周期都会产生，长期运行会累积到数十万行、占满小容量磁盘，
// 默认关闭；排查问题时设置环境变量 CONFIGFLOW_AGENT_DEBUG=1 打开。
var debugEnabled = func() bool {
	v := os.Getenv("CONFIGFLOW_AGENT_DEBUG")
	if v == "" {
		return false
	}
	enabled, err := strconv.ParseBool(v)
	return err == nil && enabled
}()

// logDebugf 输出例行调试日志，仅在 debugEnabled 为真时生效
func logDebugf(format string, v ...any) {
	if debugEnabled {
		log.Printf(format, v...)
	}
}

// lastReportedStatus 记录上一次心跳观察到的服务状态，
// 用于只在状态发生变化时输出日志（初始值为空，首次心跳必然记录一次）
var lastReportedStatus string
