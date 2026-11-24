package agents_test

import (
	"net/url"
	"strings"
	"testing"

	core "configflow/internal/agents"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/installer"
	"configflow/internal/installer/installertest"
)

// API cases of backend/test_standalone_forwarding_install.py (the shell and
// generator cases are in internal/installer/standalone_forwarding_test.go).

func forwardingServer(t *testing.T) (*httpxtest.Server, httpxtest.Option) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, DefaultFactories: true})
	m, err := core.New(srv.Context(), srv.Repo, core.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	apiagents.NewHandler(apiagents.Deps{Repo: srv.Repo, Agents: m}).Register(srv.App.Router)
	return srv, srv.Login()
}

// makeCommand is make(client, admin, **params): POST install-command, 200.
func makeCommand(t *testing.T, srv *httpxtest.Server, admin httpxtest.Option, pairs ...any) string {
	t.Helper()
	body := map[string]any{"name": "gateway", "type": "mihomo", "server_url": "https://config.test/panel"}
	for i := 0; i < len(pairs); i += 2 {
		body[pairs[i].(string)] = pairs[i+1]
	}
	return mapStr(parseBodyMap(t, srv.Post("/api/agents/install-command", httpxtest.JSON(body), admin).WantStatus(200)), "script")
}

func TestAPIEnablesForwardingForMihomoByDefault(t *testing.T) {
	srv, admin := forwardingServer(t)
	if !strings.Contains(makeCommand(t, srv, admin), "persist_ipv4_forwarding\n") ||
		strings.Contains(makeCommand(t, srv, admin, "ipv4_forwarding", false), "persist_ipv4_forwarding") ||
		!strings.Contains(makeCommand(t, srv, admin, "ipv4_forwarding", true), "persist_ipv4_forwarding\n") ||
		strings.Contains(makeCommand(t, srv, admin, "type", "oxidns"), "persist_ipv4_forwarding") {
		t.Fatal("forwarding defaults")
	}
}

func TestAPIOptOutMatchesTheGeneratorOptOut(t *testing.T) {
	srv, admin := forwardingServer(t)
	script := makeCommand(t, srv, admin, "ipv4_forwarding", false, "agent_ip", "192.0.2.20")
	want, err := installer.RenderAgentInstall(installer.AgentInstallOptions{ServerURL: "https://config.test/panel", AgentName: "gateway",
		ServiceType: "mihomo", AgentPort: 8080, AgentIP: "192.0.2.20", ConfigPath: "/etc/mihomo/config.yaml", RestartCommand: "systemctl restart mihomo",
		BinaryDownloadURL: "https://config.test/panel/api/agents/download"})
	if err != nil || installertest.BootstrapBaseline(script) != installertest.BootstrapBaseline(want) {
		t.Fatalf("API script differs from the generator (%v)", err)
	}
}

func TestAPICustomLifecycleWithoutTheFieldSkipsForwarding(t *testing.T) {
	srv, admin := forwardingServer(t)
	script := makeCommand(t, srv, admin, "restart_command", "/custom/restart", "stop_command", "/custom/stop",
		"start_command", "/custom/start", "status_command", "/custom/status")
	if strings.Contains(script, "persist_ipv4_forwarding") || strings.Contains(script, "ExecStartPre=") {
		t.Fatal("forwarding in a custom-lifecycle script")
	}
}

func TestAPIRejectsInvalidForwardingRequests(t *testing.T) {
	srv, admin := forwardingServer(t)
	for _, c := range []struct {
		params  []any
		message string
	}{
		{[]any{"ipv4_forwarding", "false"}, "ipv4_forwarding 必须为 true 或 false"},
		{[]any{"ipv4_forwarding", 1}, "ipv4_forwarding 必须为 true 或 false"},
		{[]any{"ipv4_forwarding", false, "type", "oxidns"}, "仅 Mihomo 单独安装"},
		{[]any{"ipv4_forwarding", false, "type", "aio"}, "仅 Mihomo 单独安装"},
		{[]any{"ipv4_forwarding", false, "agent_type", "shell"}, "旧版 Shell Agent"},
		{[]any{"ipv4_forwarding", true, "restart_command", "/custom/restart", "stop_command", "/s", "start_command", "/t", "status_command", "/u"}, "仅使用 systemd 或 OpenRC"},
	} {
		body := map[string]any{"name": "gateway", "type": "mihomo", "server_url": "https://config.test/panel"}
		for i := 0; i < len(c.params); i += 2 {
			body[c.params[i].(string)] = c.params[i+1]
		}
		res := srv.Post("/api/agents/install-command", httpxtest.JSON(body), admin).WantStatus(400)
		if !strings.Contains(mapStr(parseBodyMap(t, res), "message"), c.message) {
			t.Errorf("%v: %s", c.params, res.Text())
		}
	}
}

func TestInstallScriptQueryTakesTheOptOut(t *testing.T) {
	srv, _ := forwardingServer(t)
	query := url.Values{"type": {"mihomo"}, "name": {"single"}, "server_url": {"https://config.test"}}
	enabled := srv.Get("/api/agents/install-script?" + query.Encode()).WantStatus(200)
	query.Set("ipv4_forwarding", "false")
	disabled := srv.Get("/api/agents/install-script?" + query.Encode()).WantStatus(200)
	if !enabled.Contains("persist_ipv4_forwarding\n") || disabled.Contains("persist_ipv4_forwarding") {
		t.Fatal("opt-out ignored")
	}
	query.Set("ipv4_forwarding", "no")
	srv.Get("/api/agents/install-script?" + query.Encode()).WantStatus(400)
}
