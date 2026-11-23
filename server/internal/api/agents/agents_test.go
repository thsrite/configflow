package agents_test

// Route ports of test_agent_registration_security.py,
// test_agent_heartbeat_security.py, test_agent_runtime_state.py (HTTP case)
// and the registry routes of test_agent_deployments.py.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/agents"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func newServer(t *testing.T) *httpxtest.Server {
	t.Helper()
	agents.ResetRuntime()
	agents.ResetHooks()
	t.Cleanup(func() { agents.ResetRuntime(); agents.ResetHooks() })
	t.Setenv("AGENT_REGISTRATION_KEY", "")
	os.Unsetenv("AGENT_REGISTRATION_KEY")
	return httpxtest.New(t, httpxtest.Options{DefaultFactories: true, Register: httpxtest.Routes(apiagents.Register)})
}

func payload(pairs ...any) map[string]any {
	p := map[string]any{"name": "secure-agent", "host": "10.0.0.8", "port": 8080, "service_type": "mihomo", "version": "1.2.3"}
	for i := 0; i+1 < len(pairs); i += 2 {
		p[pairs[i].(string)] = pairs[i+1]
	}
	return p
}

func storedAgents(t *testing.T, s *httpxtest.Server) []any {
	t.Helper()
	return mapList(system(t, s), "agents")
}

func system(t *testing.T, s *httpxtest.Server) map[string]any {
	t.Helper()
	sys, err := s.Repo.System(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	return parseJSONMap(t, mustRawJSON(t, sys))
}

func TestFirstRegistrationReturnsTokenAndPersistsIt(t *testing.T) {
	s := newServer(t)
	body := parseBodyMap(t, s.Post("/api/agents/register", httpxtest.JSON(payload())).WantStatus(200))
	if strings.Join(sortedMapKeys(body), ",") != "id,is_new,status,success,token" || body["success"] != true || body["is_new"] != true {
		t.Fatalf("body %v", body)
	}
	persisted := storedAgents(t, s)[0].(map[string]any)
	if persisted["id"] != body["id"] || persisted["token"] != body["token"] {
		t.Fatal("not persisted")
	}
}

func TestDuplicateRegistrationRequiresExistingBearerAndDoesNotMutate(t *testing.T) {
	for name, authorization := range map[string]string{
		"missing": "", "wrong": "Bearer wrong-token", "trailing-field": "Bearer wrong-token extra",
		"double-space": "Bearer  wrong-token", "tab": "Bearer\twrong-token", "unicode": "Bearer 令牌🔐",
		"oversized": "Bearer " + strings.Repeat("x", auth.MaxAuthTokenLength+1),
	} {
		s := newServer(t)
		created := parseBodyMap(t, s.Post("/api/agents/register", httpxtest.JSON(payload())))
		before := storedAgents(t, s)
		options := []httpxtest.Option{httpxtest.JSON(payload("port", 9999, "version", "attacker"))}
		if authorization != "" {
			options = append(options, httpxtest.Header("Authorization", authorization))
		}
		wantBodyJSON(t, s.Post("/api/agents/register", options...).WantStatus(401), map[string]any{"success": false, "message": "Unauthorized"})
		after := storedAgents(t, s)
		if !testjson.Equal(after, before) || after[0].(map[string]any)["token"] != created["token"] {
			t.Fatalf("%s: mutated", name)
		}
	}
}

func TestAuthenticatedDuplicateRegistrationNeverReturnsToken(t *testing.T) {
	s := newServer(t)
	created := parseBodyMap(t, s.Post("/api/agents/register", httpxtest.JSON(payload())))
	result := s.Post("/api/agents/register", httpxtest.JSON(payload("port", 9090)), httpxtest.Bearer(mapStr(created, "token"))).WantStatus(200)
	wantBodyJSON(t, result, map[string]any{"success": true, "id": created["id"], "status": "online", "is_new": false})
	if result.Contains("token") {
		t.Fatal("token in body")
	}
	persisted := storedAgents(t, s)[0].(map[string]any)
	if persisted["token"] != created["token"] || mapInt(persisted, "port") != 9090 {
		t.Fatal("persisted")
	}
}

func TestRegistrationKeyDoesNotBypassExistingAgentToken(t *testing.T) {
	s := newServer(t)
	t.Setenv("AGENT_REGISTRATION_KEY", "valid-registration-key")
	p := payload("registration_key", "valid-registration-key")
	created := parseBodyMap(t, s.Post("/api/agents/register", httpxtest.JSON(p)).WantStatus(200))
	before := storedAgents(t, s)
	s.Post("/api/agents/register", httpxtest.JSON(p)).WantStatus(401)
	if mapStr(created, "token") == "" || !testjson.Equal(storedAgents(t, s), before) {
		t.Fatal("mutated")
	}
	wantBodyJSON(t, s.Post("/api/agents/register", httpxtest.JSON(payload("name", "other", "registration_key", "wrong"))).
		WantStatus(403), map[string]any{"success": false, "message": apiagents.RegistrationLocked})
	if mapHas(before[0].(map[string]any), "registration_key") {
		t.Fatal("key stored")
	}
}

func TestL1LockedOrdinaryRegistrationExplainsEnrollmentWithoutLeakingKey(t *testing.T) {
	s := newServer(t)
	t.Setenv("AGENT_REGISTRATION_KEY", "l1-private-registration-key")
	before := storedAgents(t, s)
	for _, method := range []string{"shell", "docker"} {
		r := s.Post("/api/agents/register", httpxtest.JSON(payload("deployment_method", method))).WantStatus(403)
		wantBodyJSON(t, r, map[string]any{"success": false, "message": apiagents.RegistrationLocked})
		if strings.Contains(r.Text(), os.Getenv("AGENT_REGISTRATION_KEY")) || !testjson.Equal(storedAgents(t, s), before) {
			t.Fatal("refusal leaked registration key or changed registry")
		}
	}
}

func TestHostMatchingHappensAfterRealClientIPNormalization(t *testing.T) {
	s := newServer(t)
	p := payload("host", "172.18.0.4")
	proxy := httpxtest.Header("X-Forwarded-For", "203.0.113.9, 172.18.0.1")
	created := parseBodyMap(t, s.Post("/api/agents/register", httpxtest.JSON(p), proxy))
	s.Post("/api/agents/register", httpxtest.JSON(p), proxy).WantStatus(401)
	accepted := s.Post("/api/agents/register", httpxtest.JSON(p), proxy, httpxtest.Bearer(mapStr(created, "token"))).WantStatus(200)
	if storedAgents(t, s)[0].(map[string]any)["host"] != "203.0.113.9" || mapHas(parseBodyMap(t, accepted), "token") {
		t.Fatal("host")
	}
}

func TestRegistrationLogsOnlyBoundedControlFreeWhitelistedFields(t *testing.T) {
	s := newServer(t)
	logFile := filepath.Join(t.TempDir(), "app.log")
	root, err := logx.Setup(logx.Config{Level: logx.Info, File: logFile})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	raw, prefix, nested, control := "raw-secret-8d2f", "prefix-secret-4a6c", "nested-secret-1b9e", "unicode-control-secret-3c7a"
	p := payload("name", "safe-agent-"+strings.Repeat("x", 256)+prefix, "service_type", "mihomo\r\n"+raw,
		"version", "1.2.3\u2028"+control, "raw", raw, "prefix", map[string]any{"value": prefix},
		"nested", map[string]any{"authorization": map[string]any{"token": nested}})
	result := s.Post("/api/agents/register", httpxtest.JSON(p)).WantStatus(nativeLogVersionStatus(t, p, 200))
	data, _ := os.ReadFile(logFile)
	text := string(data)
	for _, secret := range []string{raw, prefix, nested, control} {
		if strings.Contains(text, secret) || result.Contains(secret) {
			t.Fatalf("%s leaked", secret)
		}
	}
	if !strings.Contains(text, "safe-agent-") || !strings.Contains(text, "mihomo") || strings.Contains(text, "authorization") || strings.Contains(text, "\r") {
		t.Fatalf("log %q", text)
	}
}

func TestManualAgentCreateDoesNotReturnToken(t *testing.T) {
	s := newServer(t)
	result := s.Post("/api/agents", httpxtest.JSON(map[string]any{"name": "manual", "host": "10.0.0.9"})).WantStatus(200)
	token := mapStr(storedAgents(t, s)[0].(map[string]any), "token")
	if result.Contains(token) {
		t.Fatal("token returned")
	}
	wantBodyJSON(t, s.Post("/api/agents", httpxtest.JSON(map[string]any{"name": "manual", "host": "10.0.0.9"})).WantStatus(500),
		map[string]any{"success": false, "message": "Unauthorized"})
	wantBodyJSON(t, s.Post("/api/agents", httpxtest.JSON([]any{1})).WantStatus(500), nativeRegistryShape(t, []any{1}, "'list' object has no attribute 'get'"))
}

type heartbeatEnv struct {
	s     *httpxtest.Server
	id    string
	token string
}

func newHeartbeat(t *testing.T) heartbeatEnv {
	s := newServer(t)
	manager, err := agents.Get(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	id, token := registerAgentForTest(t, s.Context(), manager, map[string]any{"name": "secured", "host": "127.0.0.1"})
	return heartbeatEnv{s: s, id: id, token: token}
}

func (h heartbeatEnv) path() string { return "/api/agents/" + h.id + "/heartbeat" }

func (h heartbeatEnv) agent(t *testing.T) map[string]any {
	t.Helper()
	manager, _ := agents.Get(h.s.Context())
	agent, err := manager.ReadAgent(h.s.Context(), h.id, true)
	if err != nil {
		t.Fatal(err)
	}
	return parseJSONMap(t, mustRawJSON(t, agent))
}

func TestHeartbeatRejectsUnauthorizedWithoutMutatingSystem(t *testing.T) {
	for _, authorization := range []string{"", "Basic ignored", "Bearer wrong-token", "Bearer 令牌", "Bearer café",
		"Bearer token with spaces", "Bearer  token", "Bearer\twrong", "Bearer " + strings.Repeat("x", auth.MaxAuthTokenLength+1)} {
		h := newHeartbeat(t)
		before := system(t, h.s)
		options := []httpxtest.Option{httpxtest.JSON(map[string]any{"version": "attacker", "config_version": "attacker"})}
		if authorization != "" {
			options = append(options, httpxtest.Header("Authorization", authorization))
		}
		h.s.Post(h.path(), options...).WantStatus(401)
		if !testjson.Equal(system(t, h.s), before) {
			t.Fatalf("%q mutated", authorization)
		}
	}
}

func TestHeartbeatAcceptsMatchingBearerToken(t *testing.T) {
	h := newHeartbeat(t)
	wantBodyJSON(t, h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.JSON(map[string]any{"version": "2.0.0", "config_version": "cfg-2", "service_status": "running"})).
		WantStatus(200), map[string]any{"success": true, "domain_discovery_enabled": false, "traffic_history_enabled": false, "reads_host": true})
	agent := h.agent(t)
	if agent["version"] != "2.0.0" || agent["config_version"] != "cfg-2" || agent["service_status"] != "running" {
		t.Fatalf("agent %v", agent)
	}
}

func TestHeartbeatRejectsUnapprovedOrMalformedFieldsWithoutMutation(t *testing.T) {
	for _, body := range []any{
		[]any{}, map[string]any{"unexpected": "value"}, map[string]any{"version": 2}, map[string]any{"version": strings.Repeat("v", 129)},
		map[string]any{"config_version": nil}, map[string]any{"config_version": strings.Repeat("c", 129)}, map[string]any{"service_status": false},
		map[string]any{"service_status": strings.Repeat("s", 65)}, map[string]any{"system_metrics": []any{}},
		map[string]any{"system_metrics": map[string]any{"unexpected": map[string]any{}}},
		map[string]any{"system_metrics": map[string]any{"cpu": map[string]any{"unexpected": 1}}},
		map[string]any{"system_metrics": map[string]any{"cpu": map[string]any{"usage_percent": "high"}}},
		map[string]any{"system_metrics": map[string]any{"memory": map[string]any{"total": -1}}},
		map[string]any{"system_metrics": map[string]any{"collected_at": strings.Repeat("t", 65)}},
		map[string]any{"system_metrics": map[string]any{"memory": map[string]any{"total": 1.5}}},
		map[string]any{"system_metrics": map[string]any{"cpu": map[string]any{"usage_percent": 101}}},
		nil,
	} {
		h := newHeartbeat(t)
		before := system(t, h.s)
		wantBodyJSON(t, h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.JSON(body)).WantStatus(400),
			map[string]any{"success": false, "message": "Invalid heartbeat data"})
		if !testjson.Equal(system(t, h.s), before) {
			t.Fatalf("%v mutated", body)
		}
	}
}

func TestHeartbeatRejectsMalformedJSONAndOversizedBody(t *testing.T) {
	h := newHeartbeat(t)
	before := system(t, h.s)
	h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.Body([]byte(`{"version":`), "application/json")).WantStatus(400)
	h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.Body([]byte{0xff}, "application/json")).WantStatus(400)
	h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.Body([]byte(`{}`), "text/plain")).WantStatus(400)
	wantBodyJSON(t, h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.Body(bytes.Repeat([]byte(" "), 32769), "application/json")).
		WantStatus(413), map[string]any{"success": false, "message": "Request body too large"})
	if !testjson.Equal(system(t, h.s), before) {
		t.Fatal("mutated")
	}
}

type countingReader struct {
	r     io.Reader
	count int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.count += n
	return n, err
}

func streamBody(reader io.Reader) httpxtest.Option {
	return func(r *http.Request) {
		r.Body = io.NopCloser(reader)
		r.ContentLength = -1
		r.Header.Del("Content-Length")
		r.Header.Set("Content-Type", "application/json")
	}
}

func TestHeartbeatWithoutContentLengthReadsOnlyMaxPlusOneBytes(t *testing.T) {
	h := newHeartbeat(t)
	reader := &countingReader{r: bytes.NewReader(bytes.Repeat([]byte(" "), 1024*1024))}
	h.s.Post(h.path(), httpxtest.Bearer(h.token), streamBody(reader)).WantStatus(413)
	if reader.count != 32769 {
		t.Fatalf("read %d", reader.count)
	}
	chunked := &countingReader{r: strings.NewReader(`{"version":"chunked"}`)}
	h.s.Post(h.path(), httpxtest.Bearer(h.token), streamBody(chunked)).WantStatus(200)
	if h.agent(t)["version"] != "chunked" {
		t.Fatal("chunked heartbeat")
	}
}

func TestHeartbeatAcceptsWhitelistedSystemMetrics(t *testing.T) {
	h := newHeartbeat(t)
	metrics := map[string]any{
		"cpu":          map[string]any{"usage_percent": 12.5, "core_count": 4},
		"memory":       map[string]any{"total": 1024, "used": 512, "available": 512, "used_percent": 50.0},
		"disk":         map[string]any{"total": 2048, "used": 1024, "free": 1024, "used_percent": 50.0},
		"network":      map[string]any{"bytes_sent": 10, "bytes_recv": 20, "speed_sent": 1, "speed_recv": 2},
		"collected_at": "2026-08-31T00:00:00Z",
	}
	var seen any
	agents.RegisterHooks(agents.Hooks{AddMetricsJSON: func(_ context.Context, _ *agents.Manager, _ string, raw json.RawMessage) error {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		seen = v
		return nil
	}})
	h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.JSON(map[string]any{"system_metrics": metrics})).WantStatus(200)
	if !testjson.Equal(h.agent(t)["system_metrics"], metrics) || !testjson.Equal(seen, metrics) {
		t.Fatal("metrics")
	}
}

func TestHeartbeatEndpointRecordsStateAndRejectsUnknownAgents(t *testing.T) {
	h := newHeartbeat(t)
	revision := mapInt(system(t, h.s), "_revision")
	h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.JSON(map[string]any{"version": "1.0.0", "service_status": "running"})).WantStatus(200)
	if mapInt(system(t, h.s), "_revision") != revision || h.agent(t)["service_status"] != "running" {
		t.Fatal("heartbeat wrote")
	}
	h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.JSON(map[string]any{"version": "2.0.0"})).WantStatus(200)
	if storedAgents(t, h.s)[0].(map[string]any)["version"] != "2.0.0" {
		t.Fatal("version not written")
	}
	h.s.Post("/api/agents/missing/heartbeat", httpxtest.Bearer(h.token), httpxtest.JSON(map[string]any{})).WantStatus(401)
}

func TestHeartbeatReportedHostUsesAddressFollow(t *testing.T) {
	h := newHeartbeat(t)
	// When reported host matches stored host, upgrades.ConfirmedModel returns it.
	wantBodyJSON(t, h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.JSON(map[string]any{"host": "192.0.2.10"})).WantStatus(200),
		map[string]any{"success": true, "domain_discovery_enabled": false, "traffic_history_enabled": false, "reads_host": true, "confirmed_host": "192.0.2.10"})
}

func TestAgentItemUpdateAndDelete(t *testing.T) {
	h := newHeartbeat(t)
	manager, _ := agents.Get(h.s.Context())
	if _, err := h.s.Repo.CreateProfileRecord(h.s.Context(), repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveDeployment(h.s.Context(), h.id, "binding-request"); err != nil {
		t.Fatal(err)
	}
	result := parseBodyMap(t, h.s.Put("/api/agents/"+h.id, httpxtest.JSON(map[string]any{"profile_id": "other"})).WantStatus(409))
	if !strings.Contains(mapStr(result, "message"), "unfinished deployment") || h.agent(t)["profile_id"] != "default" {
		t.Fatal("binding changed")
	}
	manager.FinishDeploymentPreparation(h.id, "binding-request")
	wantBodyJSON(t, h.s.Put("/api/agents/"+h.id, httpxtest.JSON(map[string]any{"name": "renamed", "token": "ignored"})).WantStatus(200), map[string]any{"success": true, "data": true})
	if h.agent(t)["name"] != "renamed" || h.agent(t)["token"] != h.token {
		t.Fatal("edit")
	}
	wantBodyJSON(t, h.s.Put("/api/agents/missing", httpxtest.JSON(map[string]any{})).WantStatus(404), map[string]any{"success": false, "message": "Agent not found"})
	wantBodyJSON(t, h.s.Put("/api/agents/"+h.id, httpxtest.JSON(map[string]any{"profile_id": "bad id"})).WantStatus(500), map[string]any{"success": false, "message": "Invalid profile id"})
	wantBodyJSON(t, h.s.Put("/api/agents/"+h.id, httpxtest.JSON(nil)).WantStatus(500), nativeRegistryShape(t, nil, "argument of type 'NoneType' is not iterable"))
	var cleared []string
	agents.OnAgentDeleted("test", func(_ context.Context, id string) error { cleared = append(cleared, id); return nil })
	wantBodyJSON(t, h.s.Delete("/api/agents/"+h.id).WantStatus(200), map[string]any{"success": true})
	h.s.Delete("/api/agents/" + h.id).WantStatus(404)
	if len(storedAgents(t, h.s)) != 0 || len(cleared) != 1 {
		t.Fatal("delete")
	}
}

// test_agent_registration_security.py::test_duplicate_registration_attack_is_rejected_after_repository_restart
func TestDuplicateRegistrationAttackIsRejectedAfterRepositoryRestart(t *testing.T) {
	first := newServer(t)
	created := parseBodyMap(t, first.Post("/api/agents/register", httpxtest.JSON(payload())).WantStatus(200))
	// A new process: a fresh repository on the same data directory, no
	// heartbeat state in memory, a new manager.
	agents.ResetRuntime()
	restarted := httpxtest.New(t, httpxtest.Options{DataDir: first.DataDir, DefaultFactories: true,
		Register: httpxtest.Routes(apiagents.Register)})
	before := storedAgents(t, restarted)
	restarted.Post("/api/agents/register", httpxtest.JSON(payload("port", 9999, "version", "attacker"))).WantStatus(401)
	legitimate := parseBodyMap(t, restarted.Post("/api/agents/register", httpxtest.JSON(payload("port", 9090)),
		httpxtest.Bearer(mapStr(created, "token"))).WantStatus(200))
	if mapInt(before[0].(map[string]any), "port") == 9999 || legitimate["id"] != created["id"] || mapHas(legitimate, "token") {
		t.Fatalf("legitimate %v", legitimate)
	}
	persisted := storedAgents(t, restarted)[0].(map[string]any)
	if persisted["token"] != created["token"] || mapInt(persisted, "port") != 9090 {
		t.Fatalf("persisted %v", persisted)
	}
}

// test_agent_heartbeat_security.py::test_heartbeat_rejects_malformed_encoding_or_json_without_mutation
// (streamed without Content-Length, as the Python test does).
func TestHeartbeatRejectsMalformedEncodingOrJSONWithoutMutation(t *testing.T) {
	for _, raw := range [][]byte{{0xff}, []byte(`{"version":`)} {
		h := newHeartbeat(t)
		before := system(t, h.s)
		wantBodyJSON(t, h.s.Post(h.path(), httpxtest.Bearer(h.token), streamBody(bytes.NewReader(raw))).WantStatus(400),
			map[string]any{"success": false, "message": "Invalid heartbeat data"})
		if !testjson.Equal(system(t, h.s), before) {
			t.Fatalf("%q mutated", raw)
		}
	}
}

// json.loads(bytes) detects UTF-16/32 and a UTF-8 BOM: such heartbeats are
// accepted by Python.
func TestHeartbeatAcceptsBOMAndUTF16Bodies(t *testing.T) {
	utf16 := []byte{}
	for _, r := range `{"version": "utf16"}` {
		utf16 = append(utf16, byte(r), 0)
	}
	for name, raw := range map[string][]byte{"bom": append([]byte{0xEF, 0xBB, 0xBF}, `{"version": "bom"}`...), "utf16": utf16} {
		h := newHeartbeat(t)
		h.s.Post(h.path(), httpxtest.Bearer(h.token), streamBody(bytes.NewReader(raw))).WantStatus(200)
		if h.agent(t)["version"] != name {
			t.Fatalf("%s: %v", name, h.agent(t)["version"])
		}
	}
}

// update_agent(agent_id, updates) with a body that is not an object fails
// on its first use of updates, exactly as Python's operators do.
func TestAgentItemUpdateWithNonObjectBodies(t *testing.T) {
	h := newHeartbeat(t)
	for _, c := range []struct {
		body any
		want string
	}{
		{"plain", "'str' object has no attribute 'get'"},
		{"xprofile_idx", "string indices must be integers, not 'str'"},
		{[]any{"other"}, "'list' object has no attribute 'get'"},
		{[]any{"profile_id"}, "list indices must be integers or slices, not str"},
		{5, "argument of type 'int' is not iterable"},
		{1.5, "argument of type 'float' is not iterable"},
		{true, "argument of type 'bool' is not iterable"},
		{nil, "argument of type 'NoneType' is not iterable"},
	} {
		wantBodyJSON(t, h.s.Put("/api/agents/"+h.id, httpxtest.JSON(c.body)).WantStatus(500), nativeRegistryShape(t, c.body, c.want))
	}
}

// DELETE /api/agents/<id> clears the per-Agent stores after the registry
// deleted the Agent, in Python's order whatever the order of registration;
// a failing store answers 500 with its text after the Agent is gone.
func TestAgentDeleteRunsStoreHooksInPythonOrder(t *testing.T) {
	h := newHeartbeat(t)
	var order []string
	for _, name := range []string{"region_check", "extra", "domain_probe", "traffic_store"} {
		name := name
		agents.OnAgentDeleted(name, func(_ context.Context, id string) error {
			order = append(order, name)
			if name == "region_check" {
				return errors.New("region store failed")
			}
			return nil
		})
	}
	if got := strings.Join(agents.AgentDeletedHookNames(), ","); got != "traffic_store,domain_probe,region_check,extra" {
		t.Fatalf("names %s", got)
	}
	wantBodyJSON(t, h.s.Delete("/api/agents/"+h.id).WantStatus(500), map[string]any{"success": false, "message": "region store failed"})
	if strings.Join(order, ",") != "traffic_store,domain_probe,region_check" || len(storedAgents(t, h.s)) != 0 {
		t.Fatalf("order %v", order)
	}
}

// _registration_log_fields on Unicode categories (Zs, Cf, Zl/Zp, C1,
// private use, code points unassigned in CPython 3.11's Unicode 14), length
// bounds and number texts; golden from testdata/gen_registration_log_fields.py.
func TestRegistrationLogFieldsMatchPythonGolden(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "registration_log_fields.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases := parseJSONAny(t, data)
	// N8-7: retain Unicode14 evidence; explicitly accept two newly assigned
	// printable characters under PORTING's native Unicode-range decision.
	changed := 0
	for i, item := range cases.([]any) {
		c := item.(map[string]any)
		expected := c["output"]
		input := mapObj(c, "input")
		switch mapStr(input, "name") {
		case "ext-h \U00031350":
			old := map[string]any{"name": "ext-h ", "host": "ext-h ", "port": "ext-h ", "service_type": "ext-h ", "version": "ext-h "}
			if !testjson.Equal(expected, old) {
				t.Fatal("Unicode14 Ext-H golden changed")
			}
			expected = map[string]any{"name": "ext-h \U00031350", "host": "ext-h \U00031350", "port": "ext-h \U00031350", "service_type": "ext-h \U00031350", "version": "ext-h \U00031350"}
			changed++
		case "kawi \U00011F00":
			old := map[string]any{"name": "kawi ", "host": "kawi ", "port": "kawi ", "service_type": "kawi ", "version": "kawi "}
			if !testjson.Equal(expected, old) {
				t.Fatal("Unicode14 Kawi golden changed")
			}
			expected = map[string]any{"name": "kawi \U00011F00", "host": "kawi \U00011F00", "port": "kawi \U00011F00", "service_type": "kawi \U00011F00", "version": "kawi \U00011F00"}
			changed++
		}
		if got := nativeRegistrationLogFields(t, input); !testjson.Equal(got, expected) {
			t.Errorf("%d: %v → %v, want %v", i, input["name"], got, expected)
		}
	}
	if changed != 2 {
		t.Fatalf("native Unicode rows=%d, want exactly2", changed)
	}
}

// test_agent_light_reads.py::test_heartbeat_and_agent_reports_never_copy_the_history
// (heartbeat half; which reads the repository serves cannot be counted
// without Python's monkeypatching, and the traffic-report half is A05's):
// a heartbeat that changes the version is recorded against the full record,
// so the stored publication history survives it.
func TestHeartbeatKeepsThePublicationHistory(t *testing.T) {
	h := newHeartbeat(t)
	manager, _ := agents.Get(h.s.Context())
	if err := manager.EditAgents(h.s.Context(), "", func(_ context.Context, list *[]model.Agent) error {
		deployments := map[string]any{}
		for _, key := range []string{"release-0", "release-1", "release-2", "release-3", "release-4"} {
			deployments[key] = map[string]any{"deployment_id": key, "status": "succeeded"}
		}
		if err := model.SetStoredMember(&(*list)[0], "deployments", mustRawJSON(t, deployments)); err != nil {
			return err
		}
		return model.SetStoredMember(&(*list)[0], "ipv6_verifications", mustRawJSON(t, map[string]any{"release-4": map[string]any{"status": "ok"}}))
	}); err != nil {
		t.Fatal(err)
	}
	parseBodyMap(t, h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.JSON(map[string]any{"version": "1.8.54-go", "service_status": "running"})).WantStatus(200))
	h.s.Post(h.path(), httpxtest.Header("Authorization", "Bearer wrong"), httpxtest.JSON(map[string]any{})).WantStatus(401)
	stored := h.agent(t)
	persisted := storedAgents(t, h.s)[0].(map[string]any)
	if stored["version"] != "1.8.54-go" || stored["service_status"] != "running" || len(mapObj(stored, "deployments")) != 5 ||
		persisted["version"] != "1.8.54-go" || len(mapObj(persisted, "deployments")) != 5 ||
		mapObj(mapObj(persisted, "ipv6_verifications"), "release-4")["status"] != "ok" {
		t.Fatalf("stored %v", persisted)
	}
}

// Unicode15 added Egyptian format controls U+13439–U+1343F (Cf), not
// printable letters. The registration log still truncates at each control.
// Source: https://www.unicode.org/Public/15.0.0/ucd/UnicodeData.txt
func TestRegistrationLogFieldsRejectUnicode15FormatControls(t *testing.T) {
	expected := map[string]any{"name": "safe", "host": "safe", "port": "safe", "service_type": "safe", "version": "safe"}
	for r := rune(0x13439); r <= 0x1343F; r++ {
		text := "safe" + string(r) + "forged"
		input := map[string]any{"name": text, "host": text, "port": text, "service_type": text, "version": text, "token": "synthetic-secret"}
		if got := nativeRegistrationLogFields(t, input); !testjson.Equal(got, expected) {
			t.Fatalf("%U: %v", r, got)
		}
	}
}

func nativeRegistrationLogFields(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	raw := mustRawJSON(t, input)
	encoded, err := json.Marshal(apiagents.RegistrationLogFields(raw))
	if err != nil {
		t.Fatal(err)
	}
	return parseJSONMap(t, encoded)
}
