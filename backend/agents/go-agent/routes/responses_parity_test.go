package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"agent/netfilter"
)

func marshalJSONLine(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatalf("encode %T: %v", v, err)
	}
	return buf.Bytes()
}

// TestResponseStructsByteIdenticalToLegacyMaps verifies that every typed HTTP
// response struct in package routes produces byte-for-byte identical JSON
// (field names, types, null/omission semantics, and lexicographical member
// order) to the pre-change map[string]any / map[string]string payloads across
// all routes' success and error shapes.
func TestResponseStructsByteIdenticalToLegacyMaps(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	boolPtr := func(b bool) *bool { return &b }
	emptyStrings := []string{}
	geoStrings := []string{"geoip.dat", "geosite.dat"}
	haEnv := haEnvironmentInfo{Kind: "container", Ready: true}
	retentionDays := uint64(7)
	gwSpec := LinuxGatewaySpec{Interface: "eth0", Address: "192.0.2.2", LANCIDR: "192.0.2.0/24", UpstreamGateway: "192.0.2.1"}
	gwDet := gatewayDetection{Interfaces: []gatewayInterface{}, DefaultRoutes: []gatewayDefaultRoute{}}
	rrSpec := ReturnRoutingSpec{RouterIP: "192.0.2.1", Table: 104, Priority: 100, Mark: "0x1", SafetyVersion: 2}
	rrAlloc := returnAllocation{Table: 104, Priority: 100, Mark: "0x1"}
	src := LogSource{ID: "agent", Label: "Agent", Kind: "file", Path: "/tmp/agent.log", Clearable: true}

	tests := []struct {
		route  string
		kind   string
		typed  any
		legacy any
	}{
		// AuthMiddleware
		{
			route:  "AuthMiddleware",
			kind:   "error/401",
			typed:  StringStatusMessageResponse{Message: "Unauthorized", Success: "false"},
			legacy: map[string]string{"success": "false", "message": "Unauthorized"},
		},
		// GET /health
		{
			route:  "GET /health",
			kind:   "success",
			typed:  HealthResponse{Status: "ok"},
			legacy: map[string]string{"status": "ok"},
		},
		// POST /api/config/update (legacy disabled)
		{
			route:  "POST /api/config/update",
			kind:   "error/409",
			typed:  MessageResponse{Success: false, Message: "Legacy configuration updates are disabled; upgrade ConfigFlow and use deployment protocol 1"},
			legacy: map[string]any{"success": false, "message": "Legacy configuration updates are disabled; upgrade ConfigFlow and use deployment protocol 1"},
		},
		// POST /api/restart
		{
			route:  "POST /api/restart",
			kind:   "success",
			typed:  StringStatusMessageResponse{Message: "Service restarted", Success: "true"},
			legacy: map[string]string{"success": "true", "message": "Service restarted"},
		},
		{
			route:  "POST /api/restart",
			kind:   "error/409",
			typed:  DeploymentFailureResponse{Message: "busy", Success: false},
			legacy: map[string]any{"success": false, "message": "busy"},
		},
		// POST /api/uninstall
		{
			route:  "POST /api/uninstall",
			kind:   "success",
			typed:  StringStatusMessageResponse{Message: "Uninstall process started. The agent will be removed in a few seconds.", Success: "true"},
			legacy: map[string]string{"success": "true", "message": "Uninstall process started. The agent will be removed in a few seconds."},
		},
		{
			route:  "POST /api/uninstall",
			kind:   "error/409",
			typed:  DeploymentFailureResponse{Message: "ha active", ErrorDetails: "disable ha first", Success: false},
			legacy: map[string]any{"success": false, "message": "ha active", "error_details": "disable ha first"},
		},
		// GET /api/capabilities (mihomo empty resources, mihomo populated, oxidns container ready, mosdns)
		{
			route: "GET /api/capabilities",
			kind:  "success/mihomo-empty-resources",
			typed: CapabilitiesResponse{
				AIOForwardingProtocols:       []int{1},
				ConfigDirectory:              strPtr("/etc/mihomo"),
				ConfigPath:                   "config.yaml",
				DeploymentProtocols:          []int{1},
				FinalConfigProtocols:         []int{1},
				IPv6ClientProbeProtocols:     []int{1},
				IPv6ReadinessProtocols:       []int{1},
				IPv6VerificationProtocols:    []int{1},
				MihomoResources:              &emptyStrings,
				Platform:                     "linux",
				ReloadOnlyProtocols:          []int{1},
				ReturnRoutingSafetyProtocols: []int{1},
				RuleSyncSnapshotProtocols:    []int{1},
				ServiceType:                  "mihomo",
				Success:                      true,
			},
			legacy: map[string]any{
				"success":                         true,
				"deployment_protocols":            []int{1},
				"return_routing_safety_protocols": []int{1},
				"service_type":                    "mihomo",
				"config_path":                     "config.yaml",
				"reload_only_protocols":           []int{1},
				"final_config_protocols":          []int{1},
				"rule_sync_snapshot_protocols":    []int{1},
				"platform":                        "linux",
				"ipv6_client_probe_protocols":     []int{1},
				"ipv6_verification_protocols":     []int{1},
				"mihomo_resources":                []string{},
				"config_directory":                "/etc/mihomo",
				"aio_forwarding_protocols":        []int{1},
				"ipv6_readiness_protocols":        []int{1},
			},
		},
		{
			route: "GET /api/capabilities",
			kind:  "success/mihomo-with-resources",
			typed: CapabilitiesResponse{
				AIOForwardingProtocols:       []int{1},
				ConfigDirectory:              strPtr("/etc/mihomo"),
				ConfigPath:                   "config.yaml",
				DeploymentProtocols:          []int{1},
				FinalConfigProtocols:         []int{1},
				IPv6ReadinessProtocols:       []int{1},
				MihomoResources:              &geoStrings,
				Platform:                     "linux",
				ReloadOnlyProtocols:          []int{1},
				ReturnRoutingSafetyProtocols: []int{1},
				RuleSyncSnapshotProtocols:    []int{1},
				ServiceType:                  "mihomo",
				Success:                      true,
			},
			legacy: map[string]any{
				"success":                         true,
				"deployment_protocols":            []int{1},
				"return_routing_safety_protocols": []int{1},
				"service_type":                    "mihomo",
				"config_path":                     "config.yaml",
				"reload_only_protocols":           []int{1},
				"final_config_protocols":          []int{1},
				"rule_sync_snapshot_protocols":    []int{1},
				"platform":                        "linux",
				"mihomo_resources":                []string{"geoip.dat", "geosite.dat"},
				"config_directory":                "/etc/mihomo",
				"aio_forwarding_protocols":        []int{1},
				"ipv6_readiness_protocols":        []int{1},
			},
		},
		{
			route: "GET /api/capabilities",
			kind:  "success/oxidns-container",
			typed: CapabilitiesResponse{
				ConfigPath:                   "config.yaml",
				DeploymentProtocols:          []int{1},
				FinalConfigProtocols:         []int{1},
				OxiDNSHAContainerProtocols:   []int{oxidnsHAContainerProtocol},
				OxiDNSHAEnvironment:          &haEnv,
				OxiDNSHAHandoffProtocols:     []int{oxidnsHAHandoffProtocol},
				OxiDNSHAProtocols:            []int{oxidnsHAProtocol},
				Platform:                     "linux",
				ReloadOnlyProtocols:          []int{1},
				ReturnRoutingSafetyProtocols: []int{1},
				RuleSyncSnapshotProtocols:    []int{1},
				ServiceType:                  "oxidns",
				Success:                      true,
			},
			legacy: map[string]any{
				"success":                         true,
				"deployment_protocols":            []int{1},
				"return_routing_safety_protocols": []int{1},
				"service_type":                    "oxidns",
				"config_path":                     "config.yaml",
				"reload_only_protocols":           []int{1},
				"final_config_protocols":          []int{1},
				"rule_sync_snapshot_protocols":    []int{1},
				"platform":                        "linux",
				"oxidns_ha_protocols":             []int{oxidnsHAProtocol},
				"oxidns_ha_handoff_protocols":     []int{oxidnsHAHandoffProtocol},
				"oxidns_ha_environment":           haEnv,
				"oxidns_ha_container_protocols":   []int{oxidnsHAContainerProtocol},
			},
		},
		// POST /api/deployments & /api/deployments/<id>/*
		{
			route:  "POST /api/deployments/<id>/activate",
			kind:   "success/202",
			typed:  DeploymentAcceptedResponse{DeploymentID: "dep-1", Status: "verifying", Success: true},
			legacy: map[string]any{"success": true, "deployment_id": "dep-1", "status": "verifying"},
		},
		{
			route:  "POST /api/deployments/<id>/recover",
			kind:   "success/202",
			typed:  DeploymentAcceptedResponse{DeploymentID: "dep-1", Recovering: true, Status: "rolling_back", Success: true},
			legacy: map[string]any{"success": true, "deployment_id": "dep-1", "status": "rolling_back", "recovering": true},
		},
		{
			route:  "POST /api/deployments/<id>/rollback",
			kind:   "success/202",
			typed:  DeploymentAcceptedResponse{DeploymentID: "dep-1", ManualRollback: true, Status: "rolling_back", Success: true},
			legacy: map[string]any{"success": true, "deployment_id": "dep-1", "status": "rolling_back", "manual_rollback": true},
		},
		{
			route:  "POST /api/deployments",
			kind:   "error/400",
			typed:  DeploymentFailureResponse{Message: "missing deployment id", Success: false},
			legacy: map[string]any{"success": false, "message": "missing deployment id"},
		},
		// POST /api/config/final-preview
		{
			route:  "POST /api/config/final-preview",
			kind:   "success",
			typed:  FinalConfigPreviewResponse{Content: "port: 7890\n", Success: true},
			legacy: map[string]any{"success": true, "content": "port: 7890\n"},
		},
		{
			route:  "POST /api/config/final-preview",
			kind:   "error/400",
			typed:  MessageResponse{Message: "Invalid configuration preview", Success: false},
			legacy: map[string]any{"success": false, "message": "Invalid configuration preview"},
		},
		// GET /api/rule-sync/snapshot
		{
			route:  "GET /api/rule-sync/snapshot",
			kind:   "error/400",
			typed:  CodedErrorResponse{Code: "INVALID_REQUEST", Message: "bad request", Success: false},
			legacy: map[string]any{"success": false, "code": "INVALID_REQUEST", "message": "bad request"},
		},
		// GET /api/dns/active, /api/dns/listener, /api/dns/peer
		{
			route:  "GET /api/dns/active",
			kind:   "success",
			typed:  DNSActiveResponse{AgentID: "a1", DeploymentID: "d1", Success: true},
			legacy: map[string]any{"success": true, "agent_id": "a1", "deployment_id": "d1"},
		},
		{
			route:  "GET /api/dns/active",
			kind:   "error/400",
			typed:  MessageResponse{Message: "当前 Agent 不是 OxiDNS", Success: false},
			legacy: map[string]any{"success": false, "message": "当前 Agent 不是 OxiDNS"},
		},
		{
			route:  "GET /api/dns/listener",
			kind:   "success",
			typed:  DNSListenerResponse{AgentID: "a1", DeploymentID: "d1", Enabled: true, Host: "0.0.0.0", Listen: "0.0.0.0:1053", Port: 1053, Success: true},
			legacy: map[string]any{"success": true, "agent_id": "a1", "deployment_id": "d1", "enabled": true, "listen": "0.0.0.0:1053", "host": "0.0.0.0", "port": 1053},
		},
		{
			route:  "GET /api/dns/listener",
			kind:   "error/409",
			typed:  MessageResponse{Message: "设备正在执行其他操作，暂时无法确认 DNS 配置，请稍后重试", Success: false},
			legacy: map[string]any{"success": false, "message": "设备正在执行其他操作，暂时无法确认 DNS 配置，请稍后重试"},
		},
		{
			route:  "GET /api/dns/peer",
			kind:   "success",
			typed:  DNSPeerResponse{PeerAgentID: "m1", ServiceType: "mihomo", Success: true},
			legacy: map[string]any{"success": true, "peer_agent_id": "m1", "service_type": "mihomo"},
		},
		{
			route:  "GET /api/dns/peer",
			kind:   "error/409",
			typed:  MessageResponse{Message: "peer missing", Success: false},
			legacy: map[string]any{"success": false, "message": "peer missing"},
		},
		// GET /api/logs, GET /api/logs/sources, POST /api/logs/clear, GET /logs
		{
			route:  "GET /api/logs",
			kind:   "success/default",
			typed:  DefaultLogsResponse{ErrorLogs: "", Logs: "line1", ServiceName: "mihomo", Success: true},
			legacy: map[string]any{"success": true, "logs": "line1", "error_logs": "", "service_name": "mihomo"},
		},
		{
			route:  "GET /api/logs",
			kind:   "success/source",
			typed:  LogSourceContentResponse{Logs: "line1", ServiceName: "mihomo", Source: src, Success: true},
			legacy: map[string]any{"success": true, "logs": "line1", "source": src, "service_name": "mihomo"},
		},
		{
			route:  "GET /api/logs",
			kind:   "error/source",
			typed:  LogSourceErrorResponse{Message: "read failed", Source: src, Success: false},
			legacy: map[string]any{"success": false, "message": "read failed", "source": src},
		},
		{
			route:  "GET /api/logs",
			kind:   "success/log_path",
			typed:  LogPathContentResponse{Logs: "line1", ServiceName: "mihomo", Success: true},
			legacy: map[string]any{"success": true, "logs": "line1", "service_name": "mihomo"},
		},
		{
			route:  "GET /api/logs/sources",
			kind:   "success",
			typed:  LogSourcesResponse{Sources: []LogSource{src}, Success: true},
			legacy: map[string]any{"success": true, "sources": []LogSource{src}},
		},
		{
			route:  "POST /api/logs/clear",
			kind:   "success",
			typed:  MessageResponse{Message: "日志已清空", Success: true},
			legacy: map[string]any{"success": true, "message": "日志已清空"},
		},
		{
			route:  "POST /api/logs/clear",
			kind:   "error/400",
			typed:  MessageResponse{Message: "source or log_path is required", Success: false},
			legacy: map[string]any{"success": false, "message": "source or log_path is required"},
		},
		{
			route:  "GET /logs",
			kind:   "success/default",
			typed:  DefaultLogsResponse{ErrorLogs: "err1", Logs: "out1", ServiceName: "mosdns", Success: true},
			legacy: map[string]any{"success": true, "logs": "out1", "error_logs": "err1", "service_name": "mosdns"},
		},
		// GET /api/oxidns/overview, /rankings, /rules/update, /cache, /cache/flush
		{
			route: "GET /api/oxidns/overview",
			kind:  "success",
			typed: OxiDNSOverviewResponse{
				Capabilities:    oxMonitorCapabilities{Caches: []string{"main"}},
				CollectedAtMS:   1700000000000,
				Health:          json.RawMessage(`{"status":"ok"}`),
				Metrics:         "# metrics\n",
				RuntimeRevision: "rev-2",
				SourceRevision:  "rev-1",
				Success:         true,
				UpstreamLabels:  []oxUpstreamLabel{},
			},
			legacy: map[string]any{
				"success":          true,
				"capabilities":     oxMonitorCapabilities{Caches: []string{"main"}},
				"health":           json.RawMessage(`{"status":"ok"}`),
				"metrics":          "# metrics\n",
				"collected_at_ms":  int64(1700000000000),
				"source_revision":  "rev-1",
				"runtime_revision": "rev-2",
				"upstream_labels":  []oxUpstreamLabel{},
			},
		},
		{
			route:  "GET /api/oxidns/overview",
			kind:   "error/503",
			typed:  CodedErrorResponse{Code: "CORE_UNAVAILABLE", Message: "offline", Success: false},
			legacy: map[string]any{"success": false, "code": "CORE_UNAVAILABLE", "message": "offline"},
		},
		{
			route: "GET /api/oxidns/rankings",
			kind:  "success",
			typed: OxiDNSRankingsResponse{
				Blocked:         map[string]any{"total": 0},
				Clients:         oxRankingGroup{},
				CollectedAtMS:   1700000000000,
				Domains:         oxRankingGroup{},
				InstanceID:      "inst-1",
				Recorder:        "rec-1",
				RetentionDays:   &retentionDays,
				RowsLimit:       20,
				RuntimeRevision: "rev-2",
				SampleLimit:     oxMonitorClientSampleLimit,
				SourceRevision:  "rev-1",
				Success:         true,
			},
			legacy: map[string]any{
				"success":          true,
				"recorder":         "rec-1",
				"collected_at_ms":  int64(1700000000000),
				"source_revision":  "rev-1",
				"runtime_revision": "rev-2",
				"instance_id":      "inst-1",
				"retention_days":   &retentionDays,
				"sample_limit":     oxMonitorClientSampleLimit,
				"rows_limit":       20,
				"clients":          oxRankingGroup{},
				"domains":          oxRankingGroup{},
				"blocked":          map[string]any{"total": 0},
			},
		},
		{
			route:  "GET/POST /api/oxidns/rules/update",
			kind:   "success",
			typed:  OxiDNSRuleUpdateResponse{Success: true, Task: oxRuleUpdateTask{Status: "idle"}},
			legacy: map[string]any{"success": true, "task": oxRuleUpdateTask{Status: "idle"}},
		},
		{
			route:  "GET /api/oxidns/cache",
			kind:   "success",
			typed:  OxiDNSCacheListResponse{Caches: []string{"c1"}, Success: true},
			legacy: map[string]any{"success": true, "caches": []string{"c1"}},
		},
		{
			route:  "POST /api/oxidns/cache/flush",
			kind:   "success",
			typed:  OxiDNSCacheFlushResponse{Flushed: []string{"c1"}, Success: true},
			legacy: map[string]any{"success": true, "flushed": []string{"c1"}},
		},
		{
			route:  "POST /api/oxidns/cache/flush",
			kind:   "error/502",
			typed:  OxiDNSCacheFlushErrorResponse{Code: "CACHE_FLUSH_FAILED", Flushed: []string{}, Message: "failed", Success: false},
			legacy: map[string]any{"success": false, "code": "CACHE_FLUSH_FAILED", "message": "failed", "flushed": []string{}},
		},
		// /api/oxidns-ha, /api/oxidns-ha/preview, /api/oxidns-ha/handoff
		{
			route: "GET /api/oxidns-ha",
			kind:  "success/unmanaged-nulls",
			typed: OxiDNSHAStatusResponse{
				Environment: haEnv,
				Error:       "",
				Handoff:     nil,
				Managed:     nil,
				Protocol:    oxidnsHAProtocol,
				Runtime:     OxiDNSHARuntimeStatus{Healthy: false, KeepalivedRunning: false, Role: "unknown", VIPHealthy: false, VIPPresent: false},
				Success:     true,
			},
			legacy: map[string]any{
				"success":     true,
				"protocol":    oxidnsHAProtocol,
				"managed":     nil,
				"handoff":     nil,
				"error":       "",
				"environment": haEnv,
				"runtime": map[string]any{
					"keepalived_running": false,
					"vip_present":        false,
					"role":               "unknown",
					"healthy":            false,
					"vip_healthy":        false,
				},
			},
		},
		{
			route: "GET/PUT /api/oxidns-ha",
			kind:  "success/managed",
			typed: OxiDNSHAStatusResponse{
				Environment: haEnv,
				Error:       "",
				Handoff:     nil,
				Managed: &OxiDNSHAManagedStatus{
					Active:    true,
					AppliedAt: "2026-11-23T00:00:00Z",
					CIDR:      "192.0.2.10/24",
					Interface: "eth0",
					Pending:   false,
					Spec:      OxiDNSHASpec{VIP: "192.0.2.10"},
				},
				Protocol: oxidnsHAProtocol,
				Runtime:  OxiDNSHARuntimeStatus{Healthy: true, KeepalivedRunning: true, Role: "master", VIPHealthy: true, VIPPresent: true},
				Success:  true,
			},
			legacy: map[string]any{
				"success":  true,
				"protocol": oxidnsHAProtocol,
				"managed": map[string]any{
					"spec":       OxiDNSHASpec{VIP: "192.0.2.10"},
					"active":     true,
					"pending":    false,
					"applied_at": "2026-11-23T00:00:00Z",
					"interface":  "eth0",
					"cidr":       "192.0.2.10/24",
				},
				"handoff":     nil,
				"error":       "",
				"environment": haEnv,
				"runtime": map[string]any{
					"keepalived_running": true,
					"vip_present":        true,
					"role":               "master",
					"healthy":            true,
					"vip_healthy":        true,
				},
			},
		},
		{
			route:  "DELETE /api/oxidns-ha",
			kind:   "success",
			typed:  OxiDNSHARemoveResponse{Removed: true, Success: true},
			legacy: map[string]any{"success": true, "removed": true},
		},
		{
			route:  "PUT /api/oxidns-ha",
			kind:   "error/400",
			typed:  OxiDNSHAErrorResponse{Error: "bad json", Message: "bad json", Success: false},
			legacy: map[string]any{"success": false, "message": "bad json", "error": "bad json"},
		},
		{
			route: "POST /api/oxidns-ha/preview",
			kind:  "success",
			typed: OxiDNSHAPreviewResponse{
				Blockers:    []string{},
				CIDR:        "192.0.2.10/24",
				Environment: haEnv,
				Interface:   "eth0",
				OK:          true,
				Success:     true,
			},
			legacy: map[string]any{
				"success":     true,
				"ok":          true,
				"blockers":    []string{},
				"interface":   "eth0",
				"cidr":        "192.0.2.10/24",
				"environment": haEnv,
			},
		},
		// /api/linux-gateway/{status,preview,apply,remove,forwarding}
		{
			route: "GET /api/linux-gateway/status",
			kind:  "success/unmanaged-with-optional-errors",
			typed: LinuxGatewayStatusResponse{
				ConfigError:       "cfg err",
				Detection:         gwDet,
				DetectionError:    "det err",
				EnabledInConfig:   false,
				ForwardFilter:     netfilter.Result{},
				IPv4Forwarding:    map[string]any{"enabled": true},
				Managed:           LinuxGatewayManagedStatus{Active: false, Healthy: false, PreservesForwarding: true},
				Success:           true,
				Supported:         false,
				TUN:               map[string]any{"ready": false},
				UnsupportedReason: "linux only",
			},
			legacy: map[string]any{
				"success":            true,
				"supported":          false,
				"enabled_in_config":  false,
				"detection":          gwDet,
				"managed":            map[string]any{"active": false, "healthy": false, "preserves_forwarding": true},
				"detection_error":    "det err",
				"config_error":       "cfg err",
				"unsupported_reason": "linux only",
				"ipv4_forwarding":    map[string]any{"enabled": true},
				"tun":                map[string]any{"ready": false},
				"forward_filter":     netfilter.Result{},
			},
		},
		{
			route: "POST /api/linux-gateway/preview",
			kind:  "success/managed-pending-false",
			typed: LinuxGatewayPreviewResponse{
				Blockers:  []string{},
				Commands:  []string{"cmd1"},
				Detection: gwDet,
				Managed: LinuxGatewayManagedStatus{
					Active:              true,
					AppliedAt:           "2026-11-23T00:00:00Z",
					Healthy:             true,
					Pending:             boolPtr(false),
					PreservesForwarding: false,
					Spec:                &gwSpec,
				},
				Ready:    true,
				Spec:     gwSpec,
				Success:  true,
				Warnings: []string{"w1"},
			},
			legacy: map[string]any{
				"success":   true,
				"ready":     true,
				"spec":      gwSpec,
				"blockers":  []string{},
				"warnings":  []string{"w1"},
				"commands":  []string{"cmd1"},
				"detection": gwDet,
				"managed": map[string]any{
					"active":               true,
					"healthy":              true,
					"preserves_forwarding": false,
					"pending":              false,
					"spec":                 gwSpec,
					"applied_at":           "2026-11-23T00:00:00Z",
				},
			},
		},
		{
			route:  "POST /api/linux-gateway/apply",
			kind:   "success",
			typed:  LinuxGatewayActionResponse{Managed: LinuxGatewayManagedStatus{Active: true, Healthy: true}, Message: "ok", Success: true},
			legacy: map[string]any{"success": true, "message": "ok", "managed": map[string]any{"active": true, "healthy": true, "preserves_forwarding": false}},
		},
		{
			route:  "POST /api/linux-gateway/remove",
			kind:   "error/409",
			typed:  LinuxGatewayActionResponse{Managed: LinuxGatewayManagedStatus{Active: false, Healthy: false}, Message: "conflict", Success: false},
			legacy: map[string]any{"success": false, "message": "conflict", "managed": map[string]any{"active": false, "healthy": false, "preserves_forwarding": false}},
		},
		{
			route: "POST /api/linux-gateway/forwarding",
			kind:  "success",
			typed: LinuxGatewayForwardingSuccessResponse{
				IPv4Forwarding: map[string]any{"enabled": true},
				Managed:        LinuxGatewayManagedStatus{Active: false, Healthy: false},
				Message:        "IPv4 转发已开启并持久化",
				Success:        true,
				TUNPrepare:     nil,
			},
			legacy: map[string]any{
				"success":         true,
				"message":         "IPv4 转发已开启并持久化",
				"ipv4_forwarding": map[string]any{"enabled": true},
				"tun_prepare":     nil,
				"managed":         map[string]any{"active": false, "healthy": false, "preserves_forwarding": false},
			},
		},
		{
			route:  "POST /api/linux-gateway/forwarding",
			kind:   "error/409",
			typed:  LinuxGatewayForwardingErrorResponse{IPv4Forwarding: map[string]any{"enabled": false}, Message: "failed", Success: false},
			legacy: map[string]any{"success": false, "message": "failed", "ipv4_forwarding": map[string]any{"enabled": false}},
		},
		// GET/PUT/DELETE /api/return-routing
		{
			route: "GET /api/return-routing",
			kind:  "success/unmanaged-null",
			typed: ReturnRoutingGetResponse{
				FwmarkRules:      []fwmarkRule{},
				IPRouteAvailable: true,
				Managed:          nil,
				NFTAvailable:     true,
				NFTTables:        []string{},
				ReturnTables:     []returnTable{},
				Safety:           ReturnRoutingSafetyStatus{Allocation: &rrAlloc, Blockers: []string{}, OK: true, Protocols: returnProtocols, Version: 1},
				Success:          true,
				TableRoutes:      map[string][]string{},
				TunDevices:       []string{},
			},
			legacy: map[string]any{
				"success":           true,
				"managed":           nil,
				"safety":            map[string]any{"version": 1, "protocols": returnProtocols, "ok": true, "blockers": []string{}, "allocation": rrAlloc},
				"nft_available":     true,
				"nft_tables":        []string{},
				"return_tables":     []returnTable{},
				"iproute_available": true,
				"fwmark_rules":      []fwmarkRule{},
				"table_routes":      map[string][]string{},
				"tun_devices":       []string{},
			},
		},
		{
			route: "GET /api/return-routing",
			kind:  "success/managed-detail",
			typed: ReturnRoutingGetResponse{
				FwmarkRules:      []fwmarkRule{},
				IPRouteAvailable: false,
				Managed: &ReturnRoutingManagedDetail{
					Active:        true,
					AppliedAt:     "2026-11-23T00:00:00Z",
					Legacy:        nil,
					SafetyVersion: 2,
					Spec:          rrSpec,
					TablePresent:  true,
				},
				NFTAvailable: false,
				NFTTables:    []string{},
				ReturnTables: []returnTable{},
				Safety:       ReturnRoutingSafetyStatus{Blockers: []string{"err"}, OK: false, Protocols: returnProtocols, Version: 1},
				Success:      true,
				TableRoutes:  map[string][]string{},
				TunDevices:   []string{},
			},
			legacy: map[string]any{
				"success": true,
				"managed": map[string]any{
					"spec":           rrSpec,
					"applied_at":     "2026-11-23T00:00:00Z",
					"table_present":  true,
					"legacy":         nil,
					"safety_version": 2,
					"active":         true,
				},
				"safety":            map[string]any{"version": 1, "protocols": returnProtocols, "ok": false, "blockers": []string{"err"}},
				"nft_available":     false,
				"nft_tables":        []string{},
				"return_tables":     []returnTable{},
				"iproute_available": false,
				"fwmark_rules":      []fwmarkRule{},
				"table_routes":      map[string][]string{},
				"tun_devices":       []string{},
			},
		},
		{
			route:  "GET /api/return-routing",
			kind:   "managed-error-shape",
			typed:  &ReturnRoutingManagedError{Error: "corrupt state"},
			legacy: map[string]any{"error": "corrupt state"},
		},
		{
			route:  "PUT /api/return-routing",
			kind:   "success",
			typed:  ReturnRoutingApplyResponse{LanCIDR: "192.0.2.0/24", LanInterface: "eth0", RouterMAC: "00:11:22:33:44:55", SafetyVersion: 2, Success: true, TunDevice: "Meta"},
			legacy: map[string]any{"success": true, "safety_version": 2, "lan_interface": "eth0", "lan_cidr": "192.0.2.0/24", "router_mac": "00:11:22:33:44:55", "tun_device": "Meta"},
		},
		{
			route:  "DELETE /api/return-routing",
			kind:   "success/not-present",
			typed:  ReturnRoutingRemoveResponse{Removed: false, Success: true},
			legacy: map[string]any{"success": true, "removed": false},
		},
		{
			route:  "DELETE /api/return-routing",
			kind:   "success/removed",
			typed:  ReturnRoutingRemoveResponse{Removed: true, RetainedEmptyTable: true, Success: true},
			legacy: map[string]any{"success": true, "removed": true, "retained_empty_table": true},
		},
		// POST /api/ipv6-readiness/probe & error shapes
		{
			route:  "POST /api/ipv6-readiness/probe",
			kind:   "success",
			typed:  IPv6ConnectivityResponse{Connectivity: ipv6Connectivity{Status: "ok", Protocol: "tcp", Scope: "node_transport", Port: 443}, Success: true},
			legacy: map[string]any{"success": true, "connectivity": ipv6Connectivity{Status: "ok", Protocol: "tcp", Scope: "node_transport", Port: 443}},
		},
		{
			route:  "POST /api/ipv6-readiness/prepare",
			kind:   "error/400",
			typed:  MessageResponse{Message: "请先检查并选择有效上联网卡", Success: false},
			legacy: map[string]any{"success": false, "message": "请先检查并选择有效上联网卡"},
		},
		// GET /api/upgrade-info
		{
			route: "GET /api/upgrade-info",
			kind:  "success",
			typed: UpgradeInfoResponse{
				AgentID:          "agent-1",
				Architecture:     "linux-amd64",
				DeploymentMethod: "shell",
				MigrationReady:   true,
				UpdateProtocol:   2,
				Version:          "1.9.3-go",
			},
			legacy: map[string]any{
				"version":           "1.9.3-go",
				"agent_id":          "agent-1",
				"migration_ready":   true,
				"architecture":      "linux-amd64",
				"deployment_method": "shell",
				"update_protocol":   2,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.route+"_"+tc.kind, func(t *testing.T) {
			got := marshalJSONLine(t, tc.typed)
			want := marshalJSONLine(t, tc.legacy)
			if !bytes.Equal(got, want) {
				t.Fatalf("JSON bytes differ for %s (%s):\n got: %s\nwant: %s", tc.route, tc.kind, got, want)
			}
		})
	}

	// Negative control: swapping field order breaks byte-for-byte comparison.
	type wrongOrder struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if bytes.Equal(
		marshalJSONLine(t, wrongOrder{Success: false, Message: "err"}),
		marshalJSONLine(t, map[string]any{"success": false, "message": "err"}),
	) {
		t.Fatal("negative control failed: out-of-order struct fields unexpectedly matched map bytes")
	}
}

// TestRegisteredRoutesHTTPResponseParity exercises real HTTP handlers registered
// on a ServeMux for both success and error cases to confirm end-to-end status
// codes and exact JSON response bytes.
func TestRegisteredRoutesHTTPResponseParity(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("port: 7890\n"), 0600); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(dir, "sample.log")
	if err := os.WriteFile(logFile, []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		AgentID:     "agent-test",
		ServiceType: "mihomo",
		ServiceName: "mihomo",
		ConfigPath:  cfgPath,
		Token:       "test-token",
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, cfg)

	call := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if auth {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		auth       bool
		wantStatus int
		wantLegacy any
	}{
		{
			name:       "GET /health ok",
			method:     http.MethodGet,
			path:       "/health",
			auth:       false,
			wantStatus: http.StatusOK,
			wantLegacy: map[string]string{"status": "ok"},
		},
		{
			name:       "AuthMiddleware 401",
			method:     http.MethodGet,
			path:       "/api/capabilities",
			auth:       false,
			wantStatus: http.StatusUnauthorized,
			wantLegacy: map[string]string{"success": "false", "message": "Unauthorized"},
		},
		{
			name:       "POST /api/config/update 409",
			method:     http.MethodPost,
			path:       "/api/config/update",
			auth:       true,
			wantStatus: http.StatusConflict,
			wantLegacy: map[string]any{"success": false, "message": "Legacy configuration updates are disabled; upgrade ConfigFlow and use deployment protocol 1"},
		},
		{
			name:       "POST /api/config/final-preview 400",
			method:     http.MethodPost,
			path:       "/api/config/final-preview",
			body:       `{}`,
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "Invalid configuration preview"},
		},
		{
			name:       "GET /api/dns/active 400",
			method:     http.MethodGet,
			path:       "/api/dns/active",
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "需要有效的发布编号才能确认 DNS 状态"},
		},
		{
			name:       "GET /api/dns/listener 400",
			method:     http.MethodGet,
			path:       "/api/dns/listener",
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "需要有效的发布编号才能确认 DNS 状态"},
		},
		{
			name:       "GET /api/dns/peer 409",
			method:     http.MethodGet,
			path:       "/api/dns/peer",
			auth:       true,
			wantStatus: http.StatusConflict,
			wantLegacy: map[string]any{"success": false, "message": "未能确认同机 All in One 的 Mihomo Agent，请检查两个 Agent 的安装与注册，或填写可达的 Mihomo DNS 地址"},
		},
		{
			name:       "GET /api/logs unknown source",
			method:     http.MethodGet,
			path:       "/api/logs?source=missing",
			auth:       true,
			wantStatus: http.StatusOK,
			wantLegacy: map[string]any{"success": false, "message": "未知的日志来源: missing"},
		},
		{
			name:       "GET /api/logs log_path ok",
			method:     http.MethodGet,
			path:       "/api/logs?log_path=" + logFile,
			auth:       true,
			wantStatus: http.StatusOK,
			wantLegacy: map[string]any{"success": true, "logs": "hello\n", "service_name": "mihomo"},
		},
		{
			name:       "POST /api/logs/clear 400",
			method:     http.MethodPost,
			path:       "/api/logs/clear",
			body:       `{}`,
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "source or log_path is required"},
		},
		{
			name:       "POST /api/logs/clear ok",
			method:     http.MethodPost,
			path:       "/api/logs/clear",
			body:       `{"log_path":"` + logFile + `"}`,
			auth:       true,
			wantStatus: http.StatusOK,
			wantLegacy: map[string]any{"success": true, "message": "日志已清空"},
		},
		{
			name:       "POST /api/ipv6-readiness/verify 400",
			method:     http.MethodPost,
			path:       "/api/ipv6-readiness/verify",
			body:       `{invalid`,
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "验收请求无效"},
		},
		{
			name:       "POST /api/ipv6-readiness/verify-client 400",
			method:     http.MethodPost,
			path:       "/api/ipv6-readiness/verify-client",
			body:       `{invalid`,
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "客户端验收参数无效"},
		},
		{
			name:       "POST /api/ipv6-readiness/verify-connections 400",
			method:     http.MethodPost,
			path:       "/api/ipv6-readiness/verify-connections",
			body:       `{invalid`,
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "连接观察参数无效"},
		},
		{
			name:       "GET /api/oxidns/cache 400 invalid query",
			method:     http.MethodGet,
			path:       "/api/oxidns/cache?x=1",
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "code": "INVALID_REQUEST", "message": "Cache operations do not accept parameters"},
		},
		{
			name:       "POST /api/oxidns/cache/flush 400 bad body",
			method:     http.MethodPost,
			path:       "/api/oxidns/cache/flush",
			body:       `{}`,
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "code": "INVALID_REQUEST", "message": `POST requires {"tags": [...]}`},
		},
		{
			name:       "GET /api/oxidns/overview wrong service type",
			method:     http.MethodGet,
			path:       "/api/oxidns/overview",
			auth:       true,
			wantStatus: http.StatusConflict,
			wantLegacy: map[string]any{"success": false, "code": "CONFIG_UNSUPPORTED", "message": "Monitoring requires an OxiDNS agent"},
		},
		{
			name:       "GET /api/oxidns/rankings invalid query",
			method:     http.MethodGet,
			path:       "/api/oxidns/rankings",
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "code": "INVALID_QUERY", "message": "Invalid or unsupported monitoring query parameters"},
		},
		{
			name:       "GET /api/rule-sync/snapshot unavailable",
			method:     http.MethodGet,
			path:       "/api/rule-sync/snapshot",
			auth:       true,
			wantStatus: http.StatusConflict,
			wantLegacy: map[string]any{"success": false, "code": "SNAPSHOT_UNAVAILABLE", "message": "No active managed deployment is available"},
		},
		{
			name:       "PUT /api/return-routing 400",
			method:     http.MethodPut,
			path:       "/api/return-routing",
			body:       `{invalid`,
			auth:       true,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "请求格式错误：invalid character 'i' looking for beginning of object key string"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(tc.method, tc.path, tc.body, tc.auth)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			want := marshalJSONLine(t, tc.wantLegacy)
			if !bytes.Equal(rec.Body.Bytes(), want) {
				t.Fatalf("body mismatch:\n got: %s\nwant: %s", rec.Body.Bytes(), want)
			}
		})
	}
}
