package server

import (
	"context"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

// test_http_server.py::test_invalid_backend_port_fails_before_starting_server
func TestInvalidBackendPortFailsBeforeStartingServer(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "65536", "1.5", "true", "+5001", "5_001", "５００１", strings.Repeat("9", 5000)} {
		t.Setenv("CONFIGFLOW_BACKEND_PORT", value)
		if _, err := BackendAddressFromEnv(); err == nil || !strings.Contains(err.Error(), "CONFIGFLOW_BACKEND_PORT") {
			t.Errorf("%q: %v", value, err)
		}
	}
}

// test_http_server.py::test_backend_port_accepts_range_boundaries and
// test_backend_endpoint_can_avoid_host_network_port_conflicts
func TestBackendPortAcceptsRangeBoundaries(t *testing.T) {
	for _, value := range []string{"1", "65535", " 15801 "} {
		t.Setenv("CONFIGFLOW_BACKEND_PORT", value)
		address, err := BackendAddressFromEnv()
		if err != nil || address.Host != "0.0.0.0" || address.Port <= 0 {
			t.Errorf("%q: %+v %v", value, address, err)
		}
	}
	t.Setenv("CONFIGFLOW_BACKEND_HOST", " 127.0.0.1 ")
	if address, _ := BackendAddressFromEnv(); address.Host != "127.0.0.1" {
		t.Fatal(address)
	}
}

// test_http_server.py::test_empty_backend_host_fails_at_startup
func TestEmptyBackendHostFailsAtStartup(t *testing.T) {
	t.Setenv("CONFIGFLOW_BACKEND_HOST", "  ")
	if _, err := BackendAddressFromEnv(); err == nil || !strings.Contains(err.Error(), "CONFIGFLOW_BACKEND_HOST") {
		t.Fatal(err)
	}
}

// The application has every implemented endpoint; the routes waiting for
// other packages are absent rather than partial.
func TestNewAppRegistersImplementedRoutes(t *testing.T) {
	app := NewApp()
	for _, endpoint := range []string{"auth.auth_status", "auth.login", "auth.verify", "settings.handle_web_port",
		"settings.handle_rule_fetch_proxy", "settings.handle_server_domain", "settings.handle_config_token",
		"settings.get_version", "settings.backup_config", "settings.handle_subscription_aggregation",
		"settings.test_backup", "settings.backup_now",
		"settings.list_third_party_dependencies", "settings.update_sub_store_dependency", "settings.set_sub_store_mode",
		"logs.get_logs", "logs.get_log_info", "logs.clear_logs", "stats.get_overview",
		"notifications.read_notifications", "notifications.list_notifications", "tasks.get_tasks", "tasks.run_task", "tasks.set_interval",
		"agents.gateway_installations", "agents.gateway_installation_status", "agents.gateway_installation_network",
		"agents.gateway_installation_publish", "agents.gateway_installation_enroll", "agents.gateway_installation_script"} {
		if !app.Router.HasEndpoint(endpoint) {
			t.Errorf("missing %s", endpoint)
		}
	}

}

// Python's DELETE /api/agents/<id> always clears the traffic store, the
// domain probe store and the region check store, and delete_agent clears the
// metrics and traffic history. In Go each is a hook its owning package
// registers from its wire function; this fails when a fully wired server
// would delete an Agent and leave any of those files behind.
func TestDeleteAgentClearsEveryPerAgentStoreInAFullyWiredServer(t *testing.T) {
	app := newProductionApp(t)
	stores := app.Agents.DeletedStores()
	names := make([]string, len(stores))
	for i, s := range stores {
		names[i] = s.Name
	}
	if got, want := strings.Join(names, ","), strings.Join(agents.AgentDeletedStores, ","); !strings.HasPrefix(got, want) {
		t.Errorf("DELETE /api/agents/<id> store hooks %q, want %q first", got, want)
	}
	if app.Metrics == nil {
		t.Error("delete_agent: metrics service (A04/A05) not wired")
	}
}

// A05: the full application wires the traffic history (Agent deletion,
// the save inside 监控历史保存) and the traffic store the DELETE route
// clears first.
func TestNewAppWiresTheTrafficStores(t *testing.T) {
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	app, err := New(context.Background(), Options{Repo: r, Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Agents.Close)
	stores := app.Agents.DeletedStores()
	if len(stores) == 0 || stores[0].Name != "traffic_store" {
		t.Errorf("DELETE /api/agents/<id> store hooks %+v, want traffic_store first", stores)
	}
	if app.Metrics == nil || app.Metrics.Traffic() == nil {
		t.Error("delete_agent: traffic_history (A05) not wired")
	}
}

// A04: the full application wires the metrics history into the registry
// (heartbeat metrics, Agent deletion) and provides the metrics task row.
func TestNewAppWiresTheMetricsHistory(t *testing.T) {
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	app, err := New(context.Background(), Options{Repo: r, Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Agents.Close)
	if app.Metrics == nil || app.Metrics.History() == nil {
		t.Fatal("metrics_history service not wired")
	}
	for _, slot := range app.Tasks.Missing() {
		if slot == tasks.SlotMetrics {
			t.Fatal("metrics_history task row not provided")
		}
	}
}
