package routers_test

import (
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "configflow/internal/api/routers"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	core "configflow/internal/routers"
	"configflow/internal/routers/routerfake"
	"configflow/internal/testjson"
)

func TestR15MalformedProbePersistsOnlyProbe(t *testing.T) {
	for _, mode := range []string{"verinfo", "location"} {
		t.Run(mode, func(t *testing.T) {
			seed := routerfake.IkuaiState{}
			fault := routerfake.IkuaiFault{Func: "sysstat", Action: "show", Count: 1}
			if mode == "verinfo" {
				fault.Reply = map[string]any{"code": 0, "results": map[string]any{"verinfo": []int{1}}}
			} else {
				fault.Status = 302
				fault.Location = "http://[broken"
			}
			seed.Faults = []routerfake.IkuaiFault{fault}
			fake := routerfake.NewIkuai(seed)
			server := httptest.NewServer(fake)
			defer server.Close()
			cfg := inputConfig(server.URL)
			r := cfg["routers"].(map[string]any)["items"].([]any)[0].(map[string]any)
			r["type"] = "ikuai"
			r["username"] = routerfake.IkuaiUser
			r["password"] = routerfake.IkuaiPassword
			r["last_probe"] = map[string]any{"ok": true, "at": "2000-01-01T00:00:00"}
			r["unrelated"] = map[string]any{"must": "remain"}
			dir := t.TempDir()
			fake.Reset(seed)
			s := httpxtest.New(t, httpxtest.Options{DataDir: filepath.Join(dir, "go")})
			api.New(api.Deps{Repo: s.Repo, Routers: core.New(s.Repo, core.Options{})}).Register(s.App.Router)
			saveFixtureConfig(t, s, cfg)
			before, err := s.Repo.Profile(s.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			result := s.Post("/api/routers/router-one/probe")
			result.WantStatus(502)
			resObj := decodeJSON(t, result)
			resProbe, _ := resObj["probe"].(map[string]any)
			resMsg, _ := resObj["message"].(string)
			if resObj["success"] == true || resProbe["ok"] == true || resMsg == "" {
				t.Fatal("failure not represented")
			}
			after, err := s.Repo.Profile(s.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			if after.Revision != before.Revision+1 {
				t.Fatal("failed probe revision was not incremented exactly once")
			}
			beforeRaw, _ := json.Marshal(before)
			afterRaw, _ := json.Marshal(after)
			var beforeMap, afterMap map[string]any
			_ = json.Unmarshal(beforeRaw, &beforeMap)
			_ = json.Unmarshal(afterRaw, &afterMap)
			delete(beforeMap, "_revision")
			delete(afterMap, "_revision")
			delete(beforeMap, "updated_at")
			delete(afterMap, "updated_at")
			old := beforeMap["routers"].(map[string]any)["items"].([]any)[0].(map[string]any)
			current := afterMap["routers"].(map[string]any)["items"].([]any)[0].(map[string]any)
			currentProbe, _ := current["last_probe"].(map[string]any)
			if currentProbe["ok"] == true || currentProbe["message"] != resMsg {
				t.Fatal("failed probe not stored")
			}
			delete(old, "last_probe")
			delete(current, "last_probe")
			if !testjson.Equal(beforeMap, afterMap) {
				t.Fatal("probe replaced unrelated fields")
			}
			if len(fake.Snapshot().Tables["static_rt"]) != 0 {
				t.Fatal("probe mutated device")
			}
		})
	}
}
func TestR15ProbeRequestDisconnectAndRuntimeShutdown(t *testing.T) {
	for _, mode := range []string{"disconnect", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			entered, release, stopped, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			fake := routerfake.New(nil)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rest/system/resource" {
					close(entered)
					select {
					case <-r.Context().Done():
						close(stopped)
						return
					case <-release:
					}
				}
				fake.ServeHTTP(w, r)
			}))
			defer server.Close()
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
			runtime.Start(t.Context())
			defer runtime.Shutdown(t.Context())
			s := httpxtest.New(t, httpxtest.Options{Setup: func(app *httpx.App) {
				app.AfterRequest(func(c *httpx.Ctx, response *httpx.Response) {
					if strings.HasSuffix(c.Path(), "/probe") {
						close(finished)
					}
				})
			}})
			api.New(api.Deps{Repo: s.Repo, Routers: core.New(s.Repo, core.Options{})}).Register(s.App.Router)
			created := s.Post("/api/routers", httpxtest.JSON(routerBody(server.URL)))
			created.WantStatus(200)
			id := decodeJSON(t, created)["router"].(map[string]any)["id"].(string)
			before, _ := s.Repo.ConfigRevision(s.Context(), "default")
			boundHTTP := runtimeHTTP(t, s, runtime)
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			req, err := http.NewRequestWithContext(request, "POST", boundHTTP.URL+"/api/routers/"+id+"/probe", nil)
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				resp, err := http.DefaultClient.Do(req)
				if resp != nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				result <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("probe not started")
			}
			if mode == "disconnect" {
				cancel()
				close(release)
			} else {
				runtime.Shutdown(t.Context())
				select {
				case <-stopped:
				case <-time.After(2 * time.Second):
					t.Fatal("Runtime did not cancel device I/O")
				}
			}
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("owner did not finish")
			}
			<-result
			after, _ := s.Repo.ConfigRevision(s.Context(), "default")
			expected := before
			if mode == "disconnect" {
				expected++
			}
			if after != expected {
				t.Fatalf("%s revision %d expected %d", mode, after, expected)
			}
		})
	}
}

// Bind before App.ServeHTTP: its default BindRequest intentionally preserves an
// existing generation. Binding inside BeforeRequest would retain the default
// Runtime already attached by App, and fail to exercise this test's Runtime.
func runtimeHTTP(t *testing.T, s *httpxtest.Server, runtime *lifecycle.Runtime) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Handler().ServeHTTP(w, r.WithContext(runtime.BindRequest(r.Context())))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestR15MalformedRecordIsolationAndPeerCredentials(t *testing.T) {
	fake := routerfake.New(nil)
	fake.Reset(deviceState())
	device := httptest.NewServer(fake)
	defer device.Close()
	s := fixture(t)
	login := s.Login()
	cfg := inputConfig(device.URL)
	r := cfg["routers"].(map[string]any)["items"].([]any)[0].(map[string]any)
	r["name"] = "Synthetic reference synthetic-peer-password"
	cfg["routers"].(map[string]any)["items"] = []any{r, map[string]any{"id": "peer", "type": "unsupported", "name": "Peer", "username": "visible-user", "password": "synthetic-peer-password", "rule_sync_interval": "broken", "proxy_gateway_agent_id": "gateway"}}
	saveFixtureConfig(t, s, cfg)
	addGateway(t, s)
	list := s.Get("/api/routers", login)
	list.WantStatus(200)
	routersList, _ := decodeJSON(t, list)["routers"].([]any)
	if len(routersList) < 2 || strings.Contains(list.Text(), "synthetic-peer-password") || !strings.Contains(list.Text(), "visible-user") {
		t.Fatalf("bad row or peer credential broke projection: %s", list.Body)
	}
	pending := s.Get("/api/agents/gateway/upstream-routers?profile=default", login)
	pending.WantStatus(200)
	pendingList, _ := decodeJSON(t, pending)["routers"].([]any)
	if strings.Contains(pending.Text(), "synthetic-peer-password") || len(pendingList) != 2 {
		t.Fatalf("global reminders lost isolation/privacy: %s", pending.Body)
	}
}
func TestR15PolicyReadGuardsRawRecordsBeforeDriverCreation(t *testing.T) {
	fake := routerfake.New(nil)
	device := httptest.NewServer(fake)
	defer device.Close()
	s := fixture(t)
	cfg := inputConfig(device.URL)
	cfg["routers"].(map[string]any)["items"].([]any)[0].(map[string]any)["rule_sync_interval"] = "first malformed value"
	saveFixtureConfig(t, s, cfg)
	service := core.ManagementService{Repo: s.Repo}
	records, err := service.Records(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	rows := records.Routers
	_, err = s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, current *model.Profile) error {
		return current.Routers.Items[0].SetJSON("rule_sync_interval", json.RawMessage(`30`))
	})
	if err != nil {
		t.Fatal(err)
	}
	constructed := false
	service.NewDriver = func(r *spec.Router) core.Driver { constructed = true; return core.NewDriver(r) }
	if _, err = service.PolicyObjects(s.Context(), "default", rows[0]); err == nil || constructed || len(fake.Calls()) != 0 {
		t.Fatalf("display defaults erased changed ownership evidence: %v", err)
	}
}
