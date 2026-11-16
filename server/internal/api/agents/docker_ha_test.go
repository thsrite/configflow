package agents_test

// Ports of backend/test_docker_ha_enrollment.py (all cases) and the API
// cases of backend/test_docker_ha_preflight.py: the guided Docker installer
// never needs an administrator or global key; the preflight check is
// authenticated, bounded and stores nothing.

import (
	"context"
	"encoding/base64"
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
	"configflow/internal/jsonx"
)

func dockerHABody() *jsonx.Object {
	return jsonx.Obj("server_url", "https://panel.example", "agent_name", "new-dns", "agent_ip", "192.168.50.20",
		"data_dir", "/srv/oxidns", "oxidns_port", 8082, "network_mode", "host")
}

// authenticatedApp is the authenticated_app fixture: login enabled, a fresh
// registry, the panel login header.
func authenticatedApp(t *testing.T) (*httpxtest.Server, *agents.Manager, httpxtest.Option) {
	t.Helper()
	agents.ResetRuntime()
	agents.ResetHooks()
	t.Cleanup(func() { agents.ResetRuntime(); agents.ResetHooks() })
	t.Setenv("AGENT_REGISTRATION_KEY", "")
	os.Unsetenv("AGENT_REGISTRATION_KEY")
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, DefaultFactories: true, Register: httpxtest.Routes(apiagents.Register)})
	manager, err := agents.Get(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	return srv, manager, srv.Login()
}

func capability(t *testing.T, srv *httpxtest.Server, body *jsonx.Object) *installer.Enrollment {
	t.Helper()
	if body == nil {
		body = dockerHABody()
	}
	profileID, err := srv.Repo.DefaultProfileID(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := installer.IssueEnrollment(srv.Repo.DataDir(), body, profileID)
	if err != nil {
		t.Fatal(err)
	}
	return enrollment
}

func sessionOf(e *installer.Enrollment) string { return e.URL[strings.LastIndex(e.URL, "/")+1:] }

func enrollPath(e *installer.Enrollment) string {
	return "/api/agents/docker-oxidns-ha-enroll/" + sessionOf(e)
}

func allAgents(t *testing.T, srv *httpxtest.Server, manager *agents.Manager) []*jsonx.Object {
	t.Helper()
	list, err := manager.GetAllAgents(srv.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func expireSession(t *testing.T, srv *httpxtest.Server, e *installer.Enrollment) string {
	t.Helper()
	path := filepath.Join(srv.Repo.DataDir(), "docker-ha-installations", sessionOf(e)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := jsonx.Parse(data)
	session := value.(*jsonx.Object)
	session.Set("expires", jsonx.Float(float64(time.Now().UnixNano())/1e9-1))
	text, _ := jsonx.Default(session)
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
	result := response.Object()
	if expires, ok := result.GetNumber("enrollment_expires_at"); !ok || expires.Float64() <= float64(time.Now().Unix()) {
		t.Fatalf("expires %v", result.Value("enrollment_expires_at"))
	}
	if !strings.Contains(result.GetString("install_command"), "docker-oxidns-ha-enroll/") {
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
	if !jsonx.Equal(jsonx.ListOf(allAgents(t, srv, manager)), jsonx.ListOf(before)) {
		t.Fatal("guide registered an Agent")
	}
}

func TestDockerHAEnrollmentLockedRegistrationAndLostResponseReplay(t *testing.T) {
	srv, manager, _ := authenticatedApp(t)
	t.Setenv("AGENT_REGISTRATION_KEY", "global-registration-secret")
	cap := capability(t, srv, nil)
	path, headers := enrollPath(cap), httpxtest.Bearer(cap.Token)
	empty := httpxtest.JSON(jsonx.NewObject())
	srv.Post(path, empty).WantStatus(401)
	srv.Post(path, empty, httpxtest.Bearer(strings.Repeat("x", 43))).WantStatus(401)
	identity := srv.Post(path, empty, headers).WantStatus(200).Object()
	if strings.Join(sortedKeys(identity.Clone()), ",") != "agent_id,success,token" {
		t.Fatalf("identity %v", identity)
	}
	if !jsonx.Equal(srv.Post(path, empty, headers).WantStatus(200).Object(), identity) {
		t.Fatal("replay differs")
	}
	agent, err := manager.GetAgentByID(srv.Context(), identity.GetString("agent_id"), false)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Value("token") != identity.Value("token") || agent.Value("name") != "new-dns-oxidns" || agent.Value("host") != "192.168.50.20" ||
		agent.Value("service_type") != "oxidns" || agent.Value("deployment_method") != "docker" || agent.Value("status") != "offline" {
		t.Fatalf("agent %v", agent)
	}
	srv.Post("/api/agents/"+identity.GetString("agent_id")+"/heartbeat",
		httpxtest.JSON(jsonx.Obj("version", "1.8.47-go", "service_status", "running")), httpxtest.Bearer(identity.GetString("token"))).WantStatus(200)
	agent, _ = manager.GetAgentByID(srv.Context(), identity.GetString("agent_id"), false)
	if agent.Value("status") != "online" {
		t.Fatalf("status %v", agent.Value("status"))
	}
	srv.Post(path, httpxtest.JSON(jsonx.Obj("host", "192.168.50.99")), headers).WantStatus(400)
}

func TestDockerHAEnrollmentRejectsExpiredAndChangedIdentity(t *testing.T) {
	srv, manager, _ := authenticatedApp(t)
	cap := capability(t, srv, nil)
	headers, empty := httpxtest.Bearer(cap.Token), httpxtest.JSON(jsonx.NewObject())
	identity := srv.Post(enrollPath(cap), empty, headers).WantStatus(200).Object()
	_, err := manager.UpdateAgents(srv.Context(), "", func(ctx context.Context, list *[]any) (any, error) {
		for _, a := range *list {
			if a.(*jsonx.Object).Value("id") == identity.Value("agent_id") {
				a.(*jsonx.Object).Set("port", 12345)
			}
		}
		return nil, nil
	})
	if err != nil {
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
	old, err := manager.RegisterAgent(srv.Context(), jsonx.Obj("name", "new-dns-oxidns", "host", "192.168.50.20", "port", 8082,
		"service_type", "oxidns", "deployment_method", "docker"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cap := capability(t, srv, nil)
	srv.Post(enrollPath(cap), httpxtest.JSON(jsonx.NewObject()), httpxtest.Bearer(cap.Token)).WantStatus(409)
	agent, _ := manager.GetAgentByID(srv.Context(), old.GetString("id"), false)
	if agent.Value("token") != old.Value("token") {
		t.Fatal("token replaced")
	}
}

func TestDockerHAEnrollmentRegenerationAndExpiryKeepExactInstallation(t *testing.T) {
	srv, _, _ := authenticatedApp(t)
	empty := httpxtest.JSON(jsonx.NewObject())
	cap := capability(t, srv, nil)
	if again := capability(t, srv, nil); *again != *cap {
		t.Fatalf("regenerated %v vs %v", again, cap)
	}
	identity := srv.Post(enrollPath(cap), empty, httpxtest.Bearer(cap.Token)).WantStatus(200).Object()
	expireSession(t, srv, cap)
	renewed := capability(t, srv, nil)
	if renewed.URL != cap.URL || renewed.Token == cap.Token {
		t.Fatal("renewal")
	}
	srv.Post(enrollPath(cap), empty, httpxtest.Bearer(cap.Token)).WantStatus(401)
	if !jsonx.Equal(srv.Post(enrollPath(renewed), empty, httpxtest.Bearer(renewed.Token)).WantStatus(200).Object(), identity) {
		t.Fatal("renewed enrollment returns another identity")
	}
	changedBody := dockerHABody()
	changedBody.Set("data_dir", "/different/data")
	changed := capability(t, srv, changedBody)
	if changed.URL == cap.URL {
		t.Fatal("changed settings share the installation")
	}
	srv.Post(enrollPath(changed), empty, httpxtest.Bearer(changed.Token)).WantStatus(409)
}

func TestDockerHAEnrollmentConcurrentRetriesCreateOneIdentity(t *testing.T) {
	srv, manager, _ := authenticatedApp(t)
	cap := capability(t, srv, nil)
	values := make([]*jsonx.Object, 6)
	errs := make([]error, 6)
	var wg sync.WaitGroup
	for i := range values {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			values[i], errs[i] = installer.Enroll(srv.Context(), srv.Repo.DataDir(), manager, sessionOf(cap), cap.Token, true)
		}(i)
	}
	wg.Wait()
	for i := range values {
		if errs[i] != nil || !jsonx.Equal(values[i], values[0]) {
			t.Fatalf("%d: %v %v", i, values[i], errs[i])
		}
	}
	count := 0
	for _, a := range allAgents(t, srv, manager) {
		if a.Value("name") == "new-dns-oxidns" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d identities", count)
	}
}

func TestDockerHAEnrollmentReadOnlyStepsIssueNoCapability(t *testing.T) {
	for _, body := range []*jsonx.Object{jsonx.Obj("action", "inspect"), jsonx.Obj("action", "verify", "vip", "192.168.50.53")} {
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
	body := jsonx.Obj("action", "check", "output", preflightBlock(), "network_mode", "host")
	srv.Post(endpoint, httpxtest.JSON(body)).WantStatus(401)
	before, _ := srv.Repo.GetSystem(srv.Context())
	response := srv.Post(endpoint, httpxtest.JSON(body), admin).WantStatus(200)
	if response.Header.Get("Cache-Control") != "no-store" || jsonx.Path(response.Object(), "preflight_report", "status") != "pass" {
		t.Fatal(response.Text())
	}
	after, _ := srv.Repo.GetSystem(srv.Context())
	if !jsonx.Equal(before, after) {
		t.Fatal("system changed")
	}
	if _, err := os.Stat(filepath.Join(srv.Repo.DataDir(), "docker-ha-installations")); err == nil {
		t.Fatal("check issued an enrollment")
	}
	body.Set("output", "old legacy text")
	if jsonx.Path(srv.Post(endpoint, httpxtest.JSON(body), admin).WantStatus(200).Object(), "preflight_report", "status") != "unknown" {
		t.Fatal("legacy text judged")
	}
}

func TestAPIBodyLimitOnlyExpandsForCheck(t *testing.T) {
	srv, _, admin := authenticatedApp(t)
	endpoint := "/api/agents/docker-oxidns-ha-guide"
	body := jsonx.Obj("action", "check", "output", strings.Repeat("terminal notes\n", 1400)+preflightBlock(), "network_mode", "host")
	if jsonx.Path(srv.Post(endpoint, httpxtest.JSON(body), admin).WantStatus(200).Object(), "preflight_report", "status") != "pass" {
		t.Fatal("large check refused")
	}
	srv.Post(endpoint, httpxtest.JSON(jsonx.Obj("action", "inspect", "padding", strings.Repeat("x", 17000))), admin).WantStatus(413)
	srv.Post(endpoint, httpxtest.Body([]byte(strings.Repeat("x", 17000)), "application/json"), admin).WantStatus(413)
	srv.Post(endpoint, httpxtest.JSON(jsonx.Obj("action", "check", "output", strings.Repeat("x", 129*1024))), admin).WantStatus(413)
	srv.Post("/api/agents/docker-oxidns-ha-enroll/"+strings.Repeat("a", 32), httpxtest.Body([]byte(strings.Repeat("x", 17000)), "application/json")).WantStatus(413)
}

func TestInvalidModeDoesNotFallBackToHost(t *testing.T) {
	srv, _, admin := authenticatedApp(t)
	srv.Post("/api/agents/docker-oxidns-ha-guide", httpxtest.JSON(jsonx.Obj("action", "check", "output", preflightBlock(), "network_mode", "bridge")), admin).WantStatus(400)
}
