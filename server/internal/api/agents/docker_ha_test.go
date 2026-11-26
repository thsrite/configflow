package agents_test

// Ports of backend/test_docker_ha_enrollment.py (all cases) and the API
// cases of backend/test_docker_ha_preflight.py: the guided Docker installer
// never needs an administrator or global key; the preflight check is
// authenticated, bounded and stores nothing.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/installer"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

func dockerHABody() map[string]any {
	return map[string]any{
		"server_url": "https://panel.example", "agent_name": "new-dns", "agent_ip": "192.168.50.20",
		"data_dir": "/srv/oxidns", "oxidns_port": 8082, "network_mode": "host",
	}
}

// authenticatedApp is the authenticated_app fixture: login enabled, a fresh
// registry, the panel login header.
func authenticatedApp(t *testing.T) (*httpxtest.Server, *agents.Manager, httpxtest.Option) {
	t.Helper()
	t.Setenv("AGENT_REGISTRATION_KEY", "")
	os.Unsetenv("AGENT_REGISTRATION_KEY")
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, DefaultFactories: true})
	manager, err := agents.New(srv.Context(), srv.Repo, agents.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	apiagents.NewHandler(apiagents.Deps{Repo: srv.Repo, Agents: manager}).Register(srv.App.Router)
	return srv, manager, srv.Login()
}

func capability(t *testing.T, srv *httpxtest.Server, body map[string]any) *installer.Capability {
	t.Helper()
	if body == nil {
		body = dockerHABody()
	}
	profileID, err := srv.Repo.DefaultProfileID(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw := mustRawJSON(t, body)
	enrollment, err := installer.IssueCapability(srv.Context(), srv.Repo.DataDir(), raw, profileID)
	if err != nil {
		t.Fatal(err)
	}
	return enrollment
}

func sessionOf(e *installer.Capability) string { return e.URL[strings.LastIndex(e.URL, "/")+1:] }

func enrollPath(e *installer.Capability) string {
	return "/api/agents/docker-oxidns-ha-enroll/" + sessionOf(e)
}

func allAgents(t *testing.T, srv *httpxtest.Server, manager *agents.Manager) []model.Agent {
	t.Helper()
	list, err := manager.ListAgents(srv.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func expireSession(t *testing.T, srv *httpxtest.Server, e *installer.Capability) string {
	t.Helper()
	path := filepath.Join(srv.Repo.DataDir(), "docker-ha-installations", sessionOf(e)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	session := parseJSONMap(t, data)
	session["expires"] = float64(time.Now().UnixNano())/1e9 - 1
	text, _ := json.Marshal(session)
	if err := os.WriteFile(path, text, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDockerHAEnrollmentGuideIsAuthenticatedAndDoesNotRegister(t *testing.T) {
	srv, manager, admin := authenticatedApp(t)
	t.Setenv("AGENT_REGISTRATION_KEY", "global-registration-secret")
	before := allAgents(t, srv, manager)
	srv.Post("/api/agents/docker-oxidns-ha-guide", httpxtest.JSON(dockerHABody())).WantStatus(401)
	response := srv.Post("/api/agents/docker-oxidns-ha-guide", httpxtest.JSON(dockerHABody()), admin).WantStatus(200)
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("Cache-Control")
	}
	result := parseBodyMap(t, response)
	if expires, ok := mapFloat(result, "enrollment_expires_at"); !ok || expires <= float64(time.Now().Unix()) {
		t.Fatalf("expires %v", result["enrollment_expires_at"])
	}
	if !strings.Contains(mapStr(result, "install_command"), "docker-oxidns-ha-enroll/") {
		t.Fatal("no enrollment URL in the install command")
	}
	if strings.Contains(response.Text(), "global-registration-secret") {
		t.Fatal("global key leaked")
	}
	probe, _ := http.NewRequest("GET", "/", nil)
	admin(probe)
	if token := strings.Fields(probe.Header.Get("Authorization"))[1]; strings.Contains(response.Text(), token) {
		t.Fatal("panel token leaked")
	}
	if !testjson.Equal(allAgents(t, srv, manager), before) {
		t.Fatal("guide registered an Agent")
	}
}

func TestDockerHAEnrollmentLockedRegistrationAndLostResponseReplay(t *testing.T) {
	srv, manager, _ := authenticatedApp(t)
	t.Setenv("AGENT_REGISTRATION_KEY", "global-registration-secret")
	cap := capability(t, srv, nil)
	path, headers := enrollPath(cap), httpxtest.Bearer(cap.Token)
	empty := httpxtest.JSON(map[string]any{})
	srv.Post(path, empty).WantStatus(401)
	srv.Post(path, empty, httpxtest.Bearer(strings.Repeat("x", 43))).WantStatus(401)
	identity := parseBodyMap(t, srv.Post(path, empty, headers).WantStatus(200))
	if strings.Join(sortedMapKeys(identity), ",") != "agent_id,success,token" {
		t.Fatalf("identity %v", identity)
	}
	if !testjson.Equal(parseBodyMap(t, srv.Post(path, empty, headers).WantStatus(200)), identity) {
		t.Fatal("replay differs")
	}
	agent, err := manager.ReadAgent(srv.Context(), mapStr(identity, "agent_id"), false)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Token != mapStr(identity, "token") || agent.Name != "new-dns-oxidns" || agent.Host != "192.168.50.20" ||
		agent.ServiceType != "oxidns" || agent.DeploymentMethod != "docker" || agent.Status != "offline" {
		t.Fatalf("agent %+v", agent)
	}
	srv.Post("/api/agents/"+mapStr(identity, "agent_id")+"/heartbeat",
		httpxtest.JSON(map[string]any{"version": "1.8.47-go", "service_status": "running"}), httpxtest.Bearer(mapStr(identity, "token"))).WantStatus(200)
	agent, _ = manager.ReadAgent(srv.Context(), mapStr(identity, "agent_id"), false)
	if agent.Status != "online" {
		t.Fatalf("status %v", agent.Status)
	}
	srv.Post(path, httpxtest.JSON(map[string]any{"host": "192.168.50.99"}), headers).WantStatus(400)
}

func TestDockerHAEnrollmentRejectsExpiredAndChangedIdentity(t *testing.T) {
	srv, manager, _ := authenticatedApp(t)
	cap := capability(t, srv, nil)
	headers, empty := httpxtest.Bearer(cap.Token), httpxtest.JSON(map[string]any{})
	identity := parseBodyMap(t, srv.Post(enrollPath(cap), empty, headers).WantStatus(200))
	if err := manager.EditAgents(srv.Context(), "", func(_ context.Context, list *[]model.Agent) error {
		for i := range *list {
			if (*list)[i].ID == mapStr(identity, "agent_id") {
				(*list)[i].Port = 12345
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	srv.Post(enrollPath(cap), empty, headers).WantStatus(409)
	path := filepath.Join(srv.Repo.DataDir(), "docker-ha-installations", sessionOf(cap)+".json")
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	expireSession(t, srv, cap)
	srv.Post(enrollPath(cap), empty, headers).WantStatus(401)
}

func TestDockerHAEnrollmentCannotTakeOverExistingDevice(t *testing.T) {
	srv, manager, _ := authenticatedApp(t)
	oldID, oldToken := registerAgentForTest(t, srv.Context(), manager, map[string]any{
		"name": "new-dns-oxidns", "host": "192.168.50.20", "port": 8082,
		"service_type": "oxidns", "deployment_method": "docker",
	})
	cap := capability(t, srv, nil)
	srv.Post(enrollPath(cap), httpxtest.JSON(map[string]any{}), httpxtest.Bearer(cap.Token)).WantStatus(409)
	agent, _ := manager.ReadAgent(srv.Context(), oldID, false)
	if agent.Token != oldToken {
		t.Fatal("token replaced")
	}
}

func TestDockerHAEnrollmentRegenerationAndExpiryKeepExactInstallation(t *testing.T) {
	srv, _, _ := authenticatedApp(t)
	empty := httpxtest.JSON(map[string]any{})
	cap := capability(t, srv, nil)
	if again := capability(t, srv, nil); *again != *cap {
		t.Fatalf("regenerated %v vs %v", again, cap)
	}
	identity := parseBodyMap(t, srv.Post(enrollPath(cap), empty, httpxtest.Bearer(cap.Token)).WantStatus(200))
	expireSession(t, srv, cap)
	renewed := capability(t, srv, nil)
	if renewed.URL != cap.URL || renewed.Token == cap.Token {
		t.Fatal("renewal")
	}
	srv.Post(enrollPath(cap), empty, httpxtest.Bearer(cap.Token)).WantStatus(401)
	if !testjson.Equal(parseBodyMap(t, srv.Post(enrollPath(renewed), empty, httpxtest.Bearer(renewed.Token)).WantStatus(200)), identity) {
		t.Fatal("renewed enrollment returns another identity")
	}
	changedBody := dockerHABody()
	changedBody["data_dir"] = "/different/data"
	changed := capability(t, srv, changedBody)
	if changed.URL == cap.URL {
		t.Fatal("changed settings share the installation")
	}
	srv.Post(enrollPath(changed), empty, httpxtest.Bearer(changed.Token)).WantStatus(409)
}

func TestDockerHAEnrollmentConcurrentRetriesCreateOneIdentity(t *testing.T) {
	srv, manager, _ := authenticatedApp(t)
	cap := capability(t, srv, nil)
	values := make([]installer.AgentIdentity, 6)
	errs := make([]error, 6)
	var wg sync.WaitGroup
	for i := range values {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			values[i], errs[i] = installer.EnrollIdentity(srv.Context(), srv.Repo.DataDir(), manager, sessionOf(cap), cap.Token, true)
		}(i)
	}
	wg.Wait()
	for i := range values {
		if errs[i] != nil || values[i] != values[0] {
			t.Fatalf("%d: %v %v", i, values[i], errs[i])
		}
	}
	count := 0
	for _, a := range allAgents(t, srv, manager) {
		if a.Name == "new-dns-oxidns" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d identities", count)
	}
}

func TestDockerHAEnrollmentReadOnlyStepsIssueNoCapability(t *testing.T) {
	for _, body := range []map[string]any{{"action": "inspect"}, {"action": "verify", "vip": "192.168.50.53"}} {
		srv, _, admin := authenticatedApp(t)
		srv.Post("/api/agents/docker-oxidns-ha-guide", httpxtest.JSON(body), admin).WantStatus(200)
		if _, err := os.Stat(filepath.Join(srv.Repo.DataDir(), "docker-ha-installations")); err == nil {
			t.Fatalf("%v issued a capability", body)
		}
	}
}

// preflightBlock is block() of test_docker_ha_preflight.py with the default evidence.
func preflightBlock() string {
	fields := [][3]string{{"kernel", "0", "Linux"}, {"docker_endpoint", "0", "local-unix"},
		{"docker_version", "0", `{"client":"29.9.2","server":"29.9.2"}`}, {"daemon_os", "0", "linux"},
		{"daemon_platform", "0", `{"os":"Arch Linux","kernel":"6.18.3"}`},
		{"daemon_security", "0", `["name=seccomp,profile=builtin","name=cgroupns"]`}, {"compose", "0", "2.30.3"},
		{"ipv4", "0", "2: eth0    inet 192.168.10.20/24 brd 192.168.10.255 scope global eth0\n"},
		{"udp53", "0", ""}, {"tcp53", "0", ""}, {"nesting", "0", "none"}, {"machine_fingerprint", "0", strings.Repeat("a", 64)}}
	lines := []string{installer.PreflightBegin}
	for _, f := range fields {
		lines = append(lines, "CFHA1\t"+f[0]+"\t"+f[1]+"\t"+base64.StdEncoding.EncodeToString([]byte(f[2])))
	}
	return strings.Join(append(lines, installer.PreflightEnd), "\n") + "\n"
}

func TestPreflightAPIRequiresAuthAndNeverIssuesEnrollmentOrPersistsInput(t *testing.T) {
	srv, _, admin := authenticatedApp(t)
	endpoint := "/api/agents/docker-oxidns-ha-guide"
	body := map[string]any{"action": "check", "output": preflightBlock(), "network_mode": "host"}
	srv.Post(endpoint, httpxtest.JSON(body)).WantStatus(401)
	before := system(t, srv)
	response := srv.Post(endpoint, httpxtest.JSON(body), admin).WantStatus(200)
	if response.Header.Get("Cache-Control") != "no-store" || mapObj(parseBodyMap(t, response), "preflight_report")["status"] != "pass" {
		t.Fatal(response.Text())
	}
	after := system(t, srv)
	if !testjson.Equal(before, after) {
		t.Fatal("system changed")
	}
	if _, err := os.Stat(filepath.Join(srv.Repo.DataDir(), "docker-ha-installations")); err == nil {
		t.Fatal("check issued an enrollment")
	}
	body["output"] = "old legacy text"
	if mapObj(parseBodyMap(t, srv.Post(endpoint, httpxtest.JSON(body), admin).WantStatus(200)), "preflight_report")["status"] != "unknown" {
		t.Fatal("legacy text judged")
	}
}

func TestAPIBodyLimitOnlyExpandsForCheck(t *testing.T) {
	srv, _, admin := authenticatedApp(t)
	endpoint := "/api/agents/docker-oxidns-ha-guide"
	body := map[string]any{"action": "check", "output": strings.Repeat("terminal notes\n", 1400) + preflightBlock(), "network_mode": "host"}
	if mapObj(parseBodyMap(t, srv.Post(endpoint, httpxtest.JSON(body), admin).WantStatus(200)), "preflight_report")["status"] != "pass" {
		t.Fatal("large check refused")
	}
	srv.Post(endpoint, httpxtest.JSON(map[string]any{"action": "inspect", "padding": strings.Repeat("x", 17000)}), admin).WantStatus(413)
	srv.Post(endpoint, httpxtest.Body([]byte(strings.Repeat("x", 17000)), "application/json"), admin).WantStatus(413)
	srv.Post(endpoint, httpxtest.JSON(map[string]any{"action": "check", "output": strings.Repeat("x", 129*1024)}), admin).WantStatus(413)
	srv.Post("/api/agents/docker-oxidns-ha-enroll/"+strings.Repeat("a", 32), httpxtest.Body([]byte(strings.Repeat("x", 17000)), "application/json")).WantStatus(413)
}

func TestInvalidModeDoesNotFallBackToHost(t *testing.T) {
	srv, _, admin := authenticatedApp(t)
	srv.Post("/api/agents/docker-oxidns-ha-guide", httpxtest.JSON(map[string]any{"action": "check", "output": preflightBlock(), "network_mode": "bridge"}), admin).WantStatus(400)
}
