package difftest

// Flask↔Go differential of the A15 routes: Agent operations (restart,
// status, logs, log sources/clear/validate, logging configuration,
// uninstall), web updates (update, upgrade read/poll, legacy report,
// latest-version) and the address routes plus the heartbeat address follow.
// Both servers point their Agents at one fake Agent on 127.0.0.1:<port>;
// the same handler also listens on this host's own LAN address with the
// same port, so the heartbeat follow (which ignores loopback addresses)
// verifies and adopts that address identically on both sides. Traffic to
// the LAN address never leaves the host. Compared: status, headers, body
// (random update ids and times masked), the requests the fake Agent received
// from each server, the stored Agent records and notifications.json.
//
// TestUpgradesProxyDifferentialAgainstFlask runs the trust_env routes with
// HTTP_PROXY pointing at a loopback recording proxy (coordinator decision 5):
// Agent operations and the address check go through it, upgrades._request
// (trust_env=False) does not.
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run UpgradesDifferential ./internal/api/agents/difftest/'
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run UpgradesProxyDifferential ./internal/api/agents/difftest/'

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/upgrades"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// fakeOpsAgent answers the Agent API used by the A15 routes. Update ids are
// remembered per client (requests vs Go, told apart by User-Agent) so each
// server's worker poll sees its own job.
type fakeOpsAgent struct {
	mu      sync.Mutex
	phase   string
	log     []string
	updates map[string]string // client → update id last submitted
	lan     string
}

func (f *fakeOpsAgent) setPhase(phase string) {
	f.mu.Lock()
	f.phase = phase
	f.log = nil
	f.mu.Unlock()
}

func (f *fakeOpsAgent) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.log
	f.log = nil
	return out
}

var hexID = regexp.MustCompile(`[0-9a-f]{32}`)

func client(r *http.Request) string {
	if strings.HasPrefix(r.Header.Get("User-Agent"), "python-requests") {
		return "python"
	}
	return "go"
}

func (f *fakeOpsAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	host := "loopback"
	if strings.HasPrefix(r.Host, f.lan+":") {
		host = "lan"
	}
	f.mu.Lock()
	phase := f.phase
	f.log = append(f.log, fmt.Sprintf("%s %s %s ?%s auth=%q type=%q body=%q", host, r.Method, r.URL.EscapedPath(),
		r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), hexID.ReplaceAllString(string(body), "<id>")))
	f.mu.Unlock()
	send := func(status int, v any) {
		data, _ := jsonx.Default(v)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(data)
	}
	raw := func(status int, data string) {
		w.WriteHeader(status)
		io.WriteString(w, data)
	}
	switch phase {
	case "500":
		send(500, jsonx.Obj("success", false, "message", "agent secret failure"))
		return
	case "notjson":
		raw(200, "not json")
		return
	case "list":
		send(200, []any{"logs", 1})
		return
	case "string":
		send(200, "a logs b")
		return
	case "null":
		send(200, nil)
		return
	case "404":
		raw(404, "404 page not found")
		return
	}
	token := r.Header.Get("Authorization")
	path := r.URL.Path
	switch {
	case path == "/api/status":
		send(200, jsonx.Obj("success", true, "status", "running", "uptime", 12))
	case path == "/api/logs":
		logs := "line one\nauth tok-op-2 and tok%2Dop%2D2 here\nGET http://x/?k=tok-op-2\nplain\n"
		send(200, jsonx.Obj("success", true, "logs", logs, "error_logs", "", "lines", r.URL.Query().Get("lines")))
	case path == "/api/logs/sources":
		send(200, jsonx.Obj("success", true, "sources", []any{jsonx.Obj("id", "mihomo", "label", "Mihomo")}))
	case path == "/api/logs/clear" || path == "/api/logs/validate":
		send(200, jsonx.Obj("success", true, "echo", string(body)))
	case path == "/api/config/logging":
		send(200, jsonx.Obj("success", true, "enabled", true))
	case path == "/api/restart":
		send(200, jsonx.Obj("success", true, "message", "restarting"))
	case path == "/api/uninstall":
		send(200, jsonx.Obj("success", true))
	case path == "/api/capabilities":
		if token != "Bearer tok-follow" && token != "Bearer tok-addr" {
			send(401, jsonx.Obj("success", false))
			return
		}
		send(200, jsonx.Obj("success", true))
	case path == "/api/agent-address":
		if token == "Bearer tok-addr-old" {
			raw(404, "404 page not found")
			return
		}
		if r.Method == "POST" {
			value, _ := jsonx.Parse(body)
			requested, _ := value.(*jsonx.Object).Value("host").(string)
			if requested != f.lan {
				send(400, jsonx.Obj("success", false, "message", "这台设备没有地址 "+requested))
				return
			}
			send(200, jsonx.Obj("success", true, "host", requested))
			return
		}
		send(200, jsonx.Obj("success", true, "configured", "", "addresses", []any{f.lan, "127.0.0.1", "172.17.0.2", 5}, "writable", true))
	case path == "/api/upgrade-info":
		switch {
		case phase == "legacy" || token == "Bearer tok-legacy":
			raw(404, "404 page not found")
		case phase == "wrongid":
			send(200, jsonx.Obj("agent_id", "someone-else", "deployment_method", "shell", "architecture", "linux-amd64"))
		case phase == "infolist":
			send(200, []any{1})
		case phase == "done":
			send(200, jsonx.Obj("agent_id", strings.TrimPrefix(token, "Bearer tok-"), "version", agents.GetLatestVersion(), "migration_ready", true))
		default:
			send(200, jsonx.Obj("agent_id", strings.TrimPrefix(token, "Bearer tok-"), "deployment_method", "shell", "architecture", "linux-amd64"))
		}
	case path == "/api/update" && r.Method == "POST":
		value, _ := jsonx.Parse(body)
		f.mu.Lock()
		f.updates[client(r)], _ = value.(*jsonx.Object).Value("update_id").(string)
		f.mu.Unlock()
		if phase == "reject" {
			send(409, jsonx.Obj("success", false, "message", "another Agent update or service operation is active"))
			return
		}
		send(202, jsonx.Obj("success", true))
	case path == "/api/update":
		f.mu.Lock()
		id := f.updates[client(r)]
		f.mu.Unlock()
		status := "downloading"
		if phase == "done" {
			status = "succeeded"
		}
		send(200, jsonx.Obj("update_id", id, "agent_id", strings.TrimPrefix(token, "Bearer tok-"), "status", status,
			"message", "下载 configflow-agent-linux-amd64", "updated_at", "2026-10-18T00:00:01Z",
			"events", []any{jsonx.Obj("at", "2026-10-18T00:00:00Z", "status", "queued"),
				jsonx.Obj("at", "2026-10-18T00:00:01Z", "status", status, "message", "https://u:p@dl.example/x")}))
	default:
		raw(404, "404 page not found")
	}
}

// lanAddress is this host's first IPv4 address address_follow.usable accepts.
func lanAddress(t *testing.T) string {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		if network, ok := address.(*net.IPNet); ok && network.IP.To4() != nil {
			if host := upgrades.Usable(network.IP.String()); host != "" {
				return host
			}
		}
	}
	t.Skip("no usable LAN IPv4 address on this host")
	return ""
}

func upgradesSeed(t *testing.T, dir string, port, closed int) {
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent := func(id, service string, p int) *jsonx.Object {
		return jsonx.Obj("id", id, "name", id, "token", "tok-"+id, "profile_id", "default", "service_type", service,
			"host", "127.0.0.1", "port", p, "version", "1.1.0-go", "config_version", "0", "enabled", true,
			"deployment_method", "shell", "created_at", "2026-11-01T00:00:00")
	}
	_, err = r.UpdateSystemTransaction(context.Background(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		docker := agent("op-docker", "mihomo", port)
		docker.Set("deployment_method", "docker")
		busy := agent("op-busy", "mihomo", port)
		busy.Set("latest_upgrade", jsonx.Obj("update_id", "busy-job", "status", "checking", "protocol", 2))
		upgrading := agent("op-upgrading", "mihomo", port)
		upgrading.Set("latest_upgrade", jsonx.Obj("update_id", strings.Repeat("b", 32), "agent_id", "op-upgrading", "protocol", 1,
			"target_version", agents.GetLatestVersion(), "previous_version", "1.1.0-go", "status", "downloading",
			"started_at", jsonx.Float(float64(time.Now().Unix())), "updated_at", 1.5))
		failed := agent("op-failed", "mihomo", port)
		failed.Set("latest_upgrade", jsonx.Obj("update_id", "old-job", "status", "failed", "protocol", 2,
			"error", "Agent download size or SHA-256 mismatch", "target_version", "9.9.9-go", "previous_version", "1.1.0-go"))
		legacy := agent("legacy", "mihomo", port)
		ha := agent("follow-ha", "oxidns", port)
		ha.Set("oxidns_ha", jsonx.Obj("enabled", true, "vip", "192.0.2.53"))
		addrHA := agent("addr-ha", "oxidns", port)
		addrHA.Set("oxidns_ha", jsonx.Obj("enabled", true, "vip", "192.0.2.54"))
		addrOld := agent("addr-old", "oxidns", port)
		addrOld.Set("deployment_method", "docker")
		system.Set("agents", []any{agent("op-1", "mihomo", port), agent("op-2", "mihomo", port), docker, busy, upgrading,
			failed, legacy, agent("op-gone", "mihomo", closed), agent("op-uninstall", "mihomo", port),
			agent("follow", "oxidns", port), ha, agent("addr", "oxidns", port), addrHA, addrOld})
		config := system.GetObject("system_config")
		if config == nil {
			config = jsonx.NewObject()
			system.Set("system_config", config)
		}
		config.Set("server_domain", "http://127.0.0.1:5001")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type upgradesRequest struct {
	request
	phase string
	wait  time.Duration // background address check
}

var upgradeMask = []string{"update_id", "started_at", "updated_at", "recovery_reported_at", "events.*.at"}

func upgradesRequests(lan string) []upgradesRequest {
	J := "application/json"
	b := "/api/agents/"
	get := func(name, path, phase string) upgradesRequest {
		return upgradesRequest{request{name: name, method: "GET", path: path, admin: true}, phase, 0}
	}
	post := func(name, path, body, contentType, phase string) upgradesRequest {
		return upgradesRequest{request{name: name, method: "POST", path: path, admin: true, body: body, contentType: contentType}, phase, 0}
	}
	agentAuth := func(token string) map[string]string { return map[string]string{"Authorization": "Bearer " + token} }
	masked := func(r upgradesRequest, mask ...string) upgradesRequest { r.mask = mask; return r }
	return []upgradesRequest{
		get("latest version", b+"latest-version", ""),
		{request{name: "latest version unauthenticated", method: "GET", path: b + "latest-version"}, "", 0},
		// status, restart
		get("status", b+"op-1/status", ""),
		get("status missing", b+"missing/status", ""),
		{request{name: "status agent token", method: "GET", path: b + "op-1/status", headers: agentAuth("tok-op-1")}, "", 0},
		post("restart", b+"op-1/restart", "", "", ""),
		post("restart busy", b+"op-busy/restart", "", "", ""),
		post("restart missing", b+"missing/restart", "", "", ""),
		masked(post("restart unreachable", b+"op-gone/restart", "", "", ""), "message", "error_details"),
		post("restart agent 500", b+"op-1/restart", "", "", "500"),
		post("restart agent not json", b+"op-1/restart", "", "", "notjson"),
		// logs
		get("logs", b+"op-1/logs?lines=50", ""),
		get("logs default lines", b+"op-1/logs", ""),
		get("logs bad lines", b+"op-1/logs?lines=abc", ""),
		get("logs unicode lines", b+"op-1/logs?lines=%D9%A3%20", ""),
		get("logs huge lines", b+"op-1/logs?lines=123456789012345678901234567890", ""),
		get("logs source", b+"op-1/logs?source=mihomo%2F%E6%97%A5%E5%BF%97%20a&log_path=/x", ""),
		get("logs path", b+"op-1/logs?log_path=/var/log/a%20b%3F.log", ""),
		masked(get("logs unreachable", b+"op-gone/logs", ""), "message", "error_details"),
		get("logs missing", b+"missing/logs", ""),
		get("logs agent 500", b+"op-1/logs", "500"),
		get("logs not json", b+"op-1/logs", "notjson"),
		get("logs list", b+"op-1/logs", "list"),
		get("logs string", b+"op-1/logs", "string"),
		get("logs null", b+"op-1/logs", "null"),
		get("log sources", b+"op-1/logs/sources", ""),
		get("log sources legacy", b+"op-1/logs/sources", "404"),
		get("log sources list", b+"op-1/logs/sources", "list"),
		post("log clear source", b+"op-1/logs/clear", `{"source": "mihomo", "log_path": "/x"}`, J, ""),
		post("log clear path", b+"op-1/logs/clear", `{"log_path": "/var/log/x.log"}`, J, ""),
		post("log clear number", b+"op-1/logs/clear", `{"source": 5}`, J, ""),
		post("log clear empty", b+"op-1/logs/clear", `{}`, J, ""),
		post("log clear null", b+"op-1/logs/clear", `null`, J, ""),
		post("log clear list", b+"op-1/logs/clear", `[1]`, J, ""),
		post("log clear text", b+"op-1/logs/clear", `{"source": "x"}`, "text/plain", ""),
		post("log clear bad json", b+"op-1/logs/clear", `{`, J, ""),
		masked(post("log clear unreachable", b+"op-gone/logs/clear", `{"source": "x"}`, J, ""), "message", "error_details"),
		post("log validate", b+"op-1/logs/validate", `{"path": "/var/log/x.log"}`, J, ""),
		post("log validate empty", b+"op-1/logs/validate", `{"path": ""}`, J, ""),
		post("log validate string body", b+"op-1/logs/validate", `"x"`, J, ""),
		get("logging config", b+"op-1/config/logging", ""),
		get("logging config missing", b+"missing/config/logging", ""),
		post("logging config set", b+"op-1/config/logging", `{"enabled": false}`, J, ""),
		post("logging config default", b+"op-2/config/logging", `{}`, J, ""),
		post("logging config odd value", b+"op-2/config/logging", `{"enabled": "x"}`, J, ""),
		post("logging config NaN", b+"op-2/config/logging", `{"enabled": NaN}`, J, ""),
		post("logging config list body", b+"op-2/config/logging", `[true]`, J, ""),
		post("logging config agent 500", b+"op-2/config/logging", `{"enabled": true}`, J, "500"),
		// uninstall
		post("uninstall busy", b+"op-busy/uninstall", "", "", ""),
		post("uninstall agent 500", b+"op-uninstall/uninstall", "", "", "500"),
		post("uninstall missing", b+"missing/uninstall", "", "", ""),
		post("uninstall", b+"op-uninstall/uninstall", "", "", ""),
		get("status after uninstall", b+"op-uninstall/status", ""),
		// updates
		masked(post("update docker", b+"op-docker/update", "", "", "")),
		post("update missing", b+"missing/update", "", "", ""),
		post("update unreachable", b+"op-gone/update", "", "", ""),
		post("update busy", b+"op-busy/update", "", "", ""),
		post("update wrong identity", b+"op-2/update", "", "", "wrongid"),
		post("update info list", b+"op-2/update", "", "", "infolist"),
		masked(post("update rejected", b+"op-2/update", "", "", "reject"), upgradeMask...),
		masked(get("upgrade read rejected", b+"op-2/upgrade", ""), upgradeMask...),
		masked(post("update", b+"op-1/update", "", "", ""), upgradeMask...),
		post("update again", b+"op-1/update", "", "", ""),
		masked(get("upgrade poll downloading", b+"op-1/upgrade", ""), upgradeMask...),
		masked(get("upgrade poll done", b+"op-1/upgrade", "done"), upgradeMask...),
		masked(get("upgrade poll terminal", b+"op-1/upgrade", ""), upgradeMask...),
		masked(post("update legacy", b+"legacy/update", "", "", ""), upgradeMask...),
		masked(get("upgrade failed localized", b+"op-failed/upgrade", ""), upgradeMask...),
		get("upgrade none recorded", b+"op-docker/upgrade", ""),
		{request{name: "upgrade unauthenticated", method: "GET", path: b + "op-upgrading/upgrade"}, "", 0},
		{request{name: "upgrade foreign token", method: "GET", path: b + "op-upgrading/upgrade", headers: agentAuth("tok-op-1")}, "", 0},
		{request{name: "upgrade own read", method: "GET", path: b + "op-upgrading/upgrade", headers: agentAuth("tok-op-upgrading")}, "", 0},
		{request{name: "report no auth", method: "POST", path: b + "op-upgrading/upgrade/report", body: `{}`, contentType: J}, "", 0},
		{request{name: "report admin", method: "POST", path: b + "op-upgrading/upgrade/report", admin: true, body: `{}`, contentType: J}, "", 0},
		{request{name: "report wrong task", method: "POST", path: b + "op-upgrading/upgrade/report", headers: agentAuth("tok-op-upgrading"),
			body: `{"update_id": "x", "status": "rolling_back"}`, contentType: J}, "", 0},
		{request{name: "report list", method: "POST", path: b + "op-upgrading/upgrade/report", headers: agentAuth("tok-op-upgrading"),
			body: `[1]`, contentType: J}, "", 0},
		{request{name: "report text", method: "POST", path: b + "op-upgrading/upgrade/report", headers: agentAuth("tok-op-upgrading"),
			body: `{}`, contentType: "text/plain"}, "", 0},
		{request{name: "report bad json", method: "POST", path: b + "op-upgrading/upgrade/report", headers: agentAuth("tok-op-upgrading"),
			body: `{`, contentType: J}, "", 0},
		{request{name: "report too large", method: "POST", path: b + "op-upgrading/upgrade/report", headers: agentAuth("tok-op-upgrading"),
			body: `{"error": "` + strings.Repeat("x", 4100) + `"}`, contentType: J}, "", 0},
		{request{name: "report", method: "POST", path: b + "op-upgrading/upgrade/report", headers: agentAuth("tok-op-upgrading"), mask: upgradeMask,
			body: `{"update_id": "` + strings.Repeat("b", 32) + `", "status": "rolling_back", "error": {"detail": "systemctl operation failed", "token": "tok-op-upgrading"}}`, contentType: J}, "", 0},
		masked(get("upgrade poll rolling back", b+"op-upgrading/upgrade", "legacy"), upgradeMask...),
		// address
		get("address describe", b+"addr/address", ""),
		get("address describe HA", b+"addr-ha/address", ""),
		get("address describe old docker", b+"addr-old/address", ""),
		get("address describe unreachable", b+"op-gone/address", ""),
		get("address describe missing", b+"missing/address", ""),
		{request{name: "address bad host", method: "PUT", path: b + "addr/address", admin: true, body: `{"host": "127.0.0.1"}`, contentType: J}, "", 0},
		{request{name: "address no body", method: "PUT", path: b + "addr/address", admin: true}, "", 0},
		{request{name: "address HA", method: "PUT", path: b + "addr-ha/address", admin: true, body: `{"host": "` + lan + `"}`, contentType: J}, "", 0},
		{request{name: "address not on device", method: "PUT", path: b + "addr/address", admin: true, body: `{"host": "127.0.0.1x"}`, contentType: J}, "", 0},
		{request{name: "address old docker", method: "PUT", path: b + "addr-old/address", admin: true, body: `{"host": " ` + lan + ` "}`, contentType: J}, "", 0},
		{request{name: "address change", method: "PUT", path: b + "addr/address", admin: true, body: `{"host": "` + lan + `"}`, contentType: J}, "", 0},
		{request{name: "address unauthenticated", method: "PUT", path: b + "addr/address", body: `{"host": "` + lan + `"}`, contentType: J}, "", 0},
		// heartbeat address follow
		{request{name: "follow heartbeat", method: "POST", path: b + "follow/heartbeat", headers: agentAuth("tok-follow"),
			body: `{"version": "1.8.60-go", "host": "` + lan + `"}`, contentType: J}, "", 1500 * time.Millisecond},
		{request{name: "follow confirmed", method: "POST", path: b + "follow/heartbeat", headers: agentAuth("tok-follow"),
			body: `{"version": "1.8.60-go", "host": "` + lan + `"}`, contentType: J}, "", 0},
		{request{name: "follow HA member", method: "POST", path: b + "follow-ha/heartbeat", headers: agentAuth("tok-follow-ha"),
			body: `{"version": "1.8.60-go", "host": "` + lan + `"}`, contentType: J}, "", 1500 * time.Millisecond},
		{request{name: "follow unreachable token", method: "POST", path: b + "op-2/heartbeat", headers: agentAuth("tok-op-2"),
			body: `{"version": "1.1.0-go", "host": "` + lan + `"}`, contentType: J}, "", 1500 * time.Millisecond},
		{request{name: "follow docker bridge", method: "POST", path: b + "op-2/heartbeat", headers: agentAuth("tok-op-2"),
			body: `{"version": "1.1.0-go", "host": "172.17.0.2"}`, contentType: J}, "", 0},
	}
}

// upgradeAgents is the stored Agent list with random values masked.
func upgradeAgents(t *testing.T, dir string) any {
	value := finalAgents(t, dir)
	for _, item := range value.([]any) {
		agent := item.(*jsonx.Object)
		maskJSON(agent, []string{"latest_upgrade.update_id", "latest_upgrade.started_at", "latest_upgrade.updated_at",
			"latest_upgrade.recovery_reported_at", "latest_upgrade.events.*.at"})
		if state, ok := agent.Value("latest_upgrade").(*jsonx.Object); ok && hexID.MatchString(jsonx.Str(state.Value("update_id"))) {
			state.Set("update_id", "<id>")
		}
	}
	return value
}

// notificationItems is notifications.json with times masked.
func notificationItems(t *testing.T, dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "notifications.json"))
	if err != nil {
		return err.Error()
	}
	text := regexp.MustCompile(`"(checked_at|first_seen_at|last_seen_at|updated_at|seen_at|fingerprint|version)":"[^"]*"`).
		ReplaceAllString(string(data), `"$1":"<t>"`)
	return regexp.MustCompile(`"evidence":"\d+"`).ReplaceAllString(text, `"evidence":"<n>"`)
}

func artifactsDir(t *testing.T) string {
	dir := t.TempDir()
	for _, arch := range upgrades.Arches {
		os.WriteFile(filepath.Join(dir, "configflow-agent-"+arch), []byte("\x7fELF"+arch+strings.Repeat("x", 100)), 0o755)
	}
	return dir
}

func startFakeOps(t *testing.T, lan string) (*fakeOpsAgent, int, int) {
	fake := &fakeOpsAgent{updates: map[string]string{}, lan: lan}
	loop, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := loop.Addr().(*net.TCPAddr).Port
	servers := []*http.Server{{Handler: fake}}
	go servers[0].Serve(loop)
	if lan != "" {
		lanListener, err := net.Listen("tcp4", net.JoinHostPort(lan, strconv.Itoa(port)))
		if err != nil {
			t.Fatal(err)
		}
		servers = append(servers, &http.Server{Handler: fake})
		go servers[1].Serve(lanListener)
	}
	t.Cleanup(func() {
		for _, s := range servers {
			s.Close()
		}
	})
	closedListener, _ := net.Listen("tcp4", "127.0.0.1:0")
	closed := closedListener.Addr().(*net.TCPAddr).Port
	closedListener.Close()
	return fake, port, closed
}

func TestUpgradesDifferentialAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	lan := lanAddress(t)
	fake, port, closed := startFakeOps(t, lan)
	t.Setenv("AGENTS_STATIC_DIR", artifactsDir(t))
	upgrades.ResetArtifactCache()
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	upgradesSeed(t, seedDir, port, closed)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase, stopPython := startPython(t, pyDir)
	defer stopPython()
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	failed := 0
	requests := upgradesRequests(lan)
	for _, r := range requests {
		fake.setPhase(r.phase)
		py := send(t, pyBase, r.request, pyToken, nil)
		time.Sleep(r.wait)
		pyCalls := fake.take()
		gor := send(t, goBase, r.request, goToken, nil)
		time.Sleep(r.wait)
		goCalls := fake.take()
		diffs := compare(r.request, py, gor)
		if strings.Join(pyCalls, "\n") != strings.Join(goCalls, "\n") {
			diffs = append(diffs, fmt.Sprintf("agent requests\n    python %q\n    go     %q", pyCalls, goCalls))
		}
		if len(diffs) > 0 {
			failed++
			t.Errorf("DIFF %s %s %s:\n  %s", r.name, r.method, r.path, strings.Join(diffs, "\n  "))
		} else {
			t.Logf("same %-36s %s %s → %d (%d agent requests)", r.name, r.method, r.path, py.status, len(pyCalls))
		}
	}
	pyAgents, goAgents := upgradeAgents(t, pyDir), upgradeAgents(t, goDir)
	if !jsonx.Equal(pyAgents, goAgents) {
		failed++
		pyText, _ := jsonx.Sorted(pyAgents)
		goText, _ := jsonx.Sorted(goAgents)
		t.Errorf("DIFF stored agents:\n  python %s\n  go     %s", pyText, goText)
	}
	if py, gor := notificationItems(t, pyDir), notificationItems(t, goDir); py != gor {
		failed++
		t.Errorf("DIFF notifications.json:\n  python %s\n  go     %s", py, gor)
	}
	// The DNS HA pair lock files Python's service_operation creates for
	// restart/uninstall belong to A18 (oxidns_ha_publish); excluded here.
	withoutHALocks := func(files []string) string {
		var out []string
		for _, file := range files {
			if !strings.HasPrefix(file, ".oxidns-ha-publications/") {
				out = append(out, file)
			}
		}
		return strings.Join(out, " ")
	}
	if py, gofiles := withoutHALocks(dataFiles(t, pyDir)), withoutHALocks(dataFiles(t, goDir)); py != gofiles {
		failed++
		t.Errorf("DIFF files under DATA_DIR:\n  python %s\n  go     %s", py, gofiles)
	}
	t.Logf("%d requests + agent requests + stored agents + notifications + DATA_DIR files, %d differences", len(requests), failed)
}

// recordingProxy is a forward HTTP proxy on loopback that logs the absolute
// request URI of every request it relays.
type recordingProxy struct {
	mu  sync.Mutex
	log []string
}

func (p *recordingProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.log = append(p.log, r.Method+" "+r.URL.String())
	p.mu.Unlock()
	outgoing, _ := http.NewRequest(r.Method, r.URL.String(), r.Body)
	outgoing.Header = r.Header.Clone()
	response, err := (&http.Transport{Proxy: nil}).RoundTrip(outgoing)
	if err != nil {
		w.WriteHeader(502)
		return
	}
	defer response.Body.Close()
	for key, values := range response.Header {
		w.Header()[key] = values
	}
	w.WriteHeader(response.StatusCode)
	io.Copy(w, response.Body)
}

func (p *recordingProxy) take() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.log
	p.log = nil
	return out
}

func TestUpgradesProxyDifferentialAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	lan := lanAddress(t)
	fake, port, closed := startFakeOps(t, lan)
	proxy := &recordingProxy{}
	proxyListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyServer := &http.Server{Handler: proxy}
	go proxyServer.Serve(proxyListener)
	defer proxyServer.Close()
	proxyURL := "http://" + proxyListener.Addr().String()
	t.Setenv("AGENTS_STATIC_DIR", artifactsDir(t))
	upgrades.ResetArtifactCache()
	for _, name := range []string{"HTTP_PROXY", "http_proxy"} {
		t.Setenv(name, proxyURL)
	}
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	upgradesSeed(t, seedDir, port, closed)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase, stopPython := startPython(t, pyDir)
	defer stopPython()
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	agentPort := ":" + strconv.Itoa(port) + "/"
	onlyAgent := func(log []string) []string {
		var out []string
		for _, line := range log {
			if strings.Contains(line, agentPort) {
				out = append(out, line)
			}
		}
		return out
	}
	b := "/api/agents/"
	cases := []upgradesRequest{
		{request{name: "proxy status", method: "GET", path: b + "op-1/logs/sources", admin: true}, "", 0},
		{request{name: "proxy logs", method: "GET", path: b + "op-1/logs?lines=5", admin: true}, "", 0},
		{request{name: "proxy restart", method: "POST", path: b + "op-1/restart", admin: true}, "", 0},
		{request{name: "proxy address describe", method: "GET", path: b + "addr/address", admin: true}, "", 0},
		{request{name: "proxy update (trust_env off)", method: "POST", path: b + "op-1/update", admin: true, mask: upgradeMask}, "", 0},
		{request{name: "proxy follow", method: "POST", path: b + "follow/heartbeat", headers: map[string]string{"Authorization": "Bearer tok-follow"},
			body: `{"version": "1.8.60-go", "host": "` + lan + `"}`, contentType: "application/json"}, "", 1500 * time.Millisecond},
	}
	failed := 0
	for _, r := range cases {
		fake.setPhase(r.phase)
		proxy.take()
		py := send(t, pyBase, r.request, pyToken, nil)
		time.Sleep(r.wait)
		pyProxied, pyCalls := onlyAgent(proxy.take()), fake.take()
		gor := send(t, goBase, r.request, goToken, nil)
		time.Sleep(r.wait)
		goProxied, goCalls := onlyAgent(proxy.take()), fake.take()
		diffs := compare(r.request, py, gor)
		if strings.Join(pyProxied, "\n") != strings.Join(goProxied, "\n") {
			diffs = append(diffs, fmt.Sprintf("proxied\n    python %q\n    go     %q", pyProxied, goProxied))
		}
		if strings.Join(pyCalls, "\n") != strings.Join(goCalls, "\n") {
			diffs = append(diffs, fmt.Sprintf("agent requests\n    python %q\n    go     %q", pyCalls, goCalls))
		}
		if len(diffs) > 0 {
			failed++
			t.Errorf("DIFF %s:\n  %s", r.name, strings.Join(diffs, "\n  "))
		} else {
			t.Logf("same %-30s → %d (%d proxied, %d agent requests)", r.name, py.status, len(pyProxied), len(pyCalls))
		}
	}
	t.Logf("%d proxy cases, %d differences", len(cases), failed)
}
