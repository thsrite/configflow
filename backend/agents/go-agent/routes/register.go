package routes

import (
	"net/http"
)

// RegisterRoutes 注册所有路由
func RegisterRoutes(mux *http.ServeMux, cfg *Config) {
	// 注册健康检查路由
	mux.HandleFunc("/health", HealthHandler)
	
	// 注册配置更新路由
	mux.HandleFunc("/api/config/update", AuthMiddleware(cfg, ConfigUpdateHandler(cfg)))
	
	// 注册重启路由
	mux.HandleFunc("/api/restart", AuthMiddleware(cfg, RestartHandler(cfg)))
	
	// 注册日志路由
	mux.HandleFunc("/api/logs", AuthMiddleware(cfg, GetLogsHandler(cfg)))
	mux.HandleFunc("/api/logs/clear", AuthMiddleware(cfg, ClearLogHandler(cfg)))
	
	// 注册卸载路由
	mux.HandleFunc("/api/uninstall", AuthMiddleware(cfg, UninstallHandler(cfg)))

	// 注册更新路由
	mux.HandleFunc("/api/update", AuthMiddleware(cfg, HandleUpdate(cfg)))

	// 注册基于端口的日志路由
	mux.HandleFunc("/logs", PortBasedLogsHandler(cfg))
}