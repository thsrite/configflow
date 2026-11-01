package routes

import (
	"net/http"
)

// RegisterRoutes 注册所有路由
func RegisterRoutes(mux *http.ServeMux, cfg *Config) {
	monitor := oxidnsMonitorAuth(cfg, OxiDNSMonitorHandler(cfg))
	mux.HandleFunc("/api/oxidns/overview", monitor)
	mux.HandleFunc("/api/oxidns/rankings", monitor)
	mux.HandleFunc("/api/oxidns/clients", monitor)
	mux.HandleFunc("/api/oxidns/queries", monitor)
	mux.HandleFunc("/api/oxidns/queries/", monitor)
	mux.HandleFunc("/api/oxidns/rules/update", oxidnsMonitorAuth(cfg, OxiDNSRuleUpdateHandler(cfg)))
	cache := oxidnsMonitorAuth(cfg, OxiDNSCacheHandler(cfg))
	mux.HandleFunc("/api/oxidns/cache", cache)
	mux.HandleFunc("/api/oxidns/cache/flush", cache)
	mux.HandleFunc("/api/capabilities", AuthMiddleware(cfg, CapabilitiesHandler(cfg)))
	mux.HandleFunc("/api/config/final-preview", AuthMiddleware(cfg, FinalConfigPreviewHandler(cfg)))
	mux.HandleFunc("/api/rule-sync/snapshot", oxidnsMonitorAuth(cfg, RuleSyncSnapshotHandler(cfg)))
	mux.HandleFunc("/api/dns/listener", AuthMiddleware(cfg, DNSListenerHandler(cfg)))
	mux.HandleFunc("/api/dns/active", AuthMiddleware(cfg, DNSActiveHandler(cfg)))
	mux.HandleFunc("/api/dns/peer", AuthMiddleware(cfg, DNSPeerHandler(cfg)))
	mux.HandleFunc("/api/return-routing", AuthMiddleware(cfg, ReturnRoutingHandler(cfg)))
	mux.HandleFunc("/api/ipv6-readiness/verify-client", AuthMiddleware(cfg, IPv6ReadinessHandler(cfg, "verify-client")))
	if cfg.ServiceType == "mihomo" {
		mux.HandleFunc("/api/ipv6-readiness", AuthMiddleware(cfg, IPv6ReadinessHandler(cfg, "status")))
		mux.HandleFunc("/api/ipv6-readiness/prepare", AuthMiddleware(cfg, IPv6ReadinessHandler(cfg, "prepare")))
		mux.HandleFunc("/api/ipv6-readiness/probe", AuthMiddleware(cfg, IPv6ReadinessHandler(cfg, "probe")))
		mux.HandleFunc("/api/ipv6-readiness/verify", AuthMiddleware(cfg, IPv6ReadinessHandler(cfg, "verify")))
		mux.HandleFunc("/api/ipv6-readiness/verify-connections", AuthMiddleware(cfg, IPv6ReadinessHandler(cfg, "verify-connections")))
	}
	// DNS 高可用只属于 OxiDNS Agent；其他类型不注册，请求得到 404。
	if cfg.ServiceType == "oxidns" {
		mux.HandleFunc("/api/oxidns-ha", AuthMiddleware(cfg, OxiDNSHAHandler(cfg)))
		mux.HandleFunc("/api/oxidns-ha/preview", AuthMiddleware(cfg, OxiDNSHAPreviewHandler(cfg)))
		mux.HandleFunc("/api/oxidns-ha/handoff", AuthMiddleware(cfg, OxiDNSHAHandoffHandler(cfg)))
	}
	for _, action := range []string{"status", "preview", "apply", "remove", "forwarding"} {
		mux.HandleFunc("/api/linux-gateway/"+action, AuthMiddleware(cfg, LinuxGatewayHandler(cfg, action)))
	}
	mux.HandleFunc("/api/deployments", AuthMiddleware(cfg, DeploymentUploadHandler(cfg)))
	mux.HandleFunc("/api/deployments/", AuthMiddleware(cfg, DeploymentHandler(cfg)))
	// 注册健康检查路由
	mux.HandleFunc("/health", HealthHandler)

	// 注册配置更新路由
	mux.HandleFunc("/api/config/update", AuthMiddleware(cfg, ConfigUpdateHandler(cfg)))

	// 注册重启路由
	mux.HandleFunc("/api/restart", AuthMiddleware(cfg, RestartHandler(cfg)))

	// 注册日志路由
	mux.HandleFunc("/api/logs", AuthMiddleware(cfg, GetLogsHandler(cfg)))
	mux.HandleFunc("/api/logs/clear", AuthMiddleware(cfg, ClearLogHandler(cfg)))
	mux.HandleFunc("/api/logs/sources", AuthMiddleware(cfg, LogSourcesHandler(cfg)))

	// 注册卸载路由
	mux.HandleFunc("/api/uninstall", AuthMiddleware(cfg, UninstallHandler(cfg)))

	// 注册更新路由
	mux.HandleFunc("/api/update", AuthMiddleware(cfg, HandleUpdate(cfg)))
	mux.HandleFunc("/api/upgrade-info", AuthMiddleware(cfg, UpgradeInfoHandler(cfg)))

	// 注册基于端口的日志路由
	mux.HandleFunc("/logs", PortBasedLogsHandler(cfg))
}
