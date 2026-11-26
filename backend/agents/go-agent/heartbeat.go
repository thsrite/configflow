package main

import (
	"time"
)

// StartHeartbeatLoop 启动一个后台 goroutine 来周期性发送心跳
func StartHeartbeatLoop(cfg *Config) {
	logger.Infof("Starting heartbeat loop with interval: %d seconds", cfg.HeartbeatInterval)
	ticker := time.NewTicker(time.Duration(cfg.HeartbeatInterval) * time.Second)

	go func() {
		// 首次立即发送一个心跳
		cfg.SendHeartbeat()

		for range ticker.C {
			cfg.SendHeartbeat()
		}
	}()
}
