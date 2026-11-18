package routers_test

import (
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
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	core "configflow/internal/routers"
	"configflow/internal/routers/routerfake"
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
			steps := []boundaryRequest{{Method: "POST", Path: "/api/routers/router-one/probe"}}
			oracle := r15Python(t, dir, map[string]any{"config": cfg, "system": map[string]any{}, "requests": steps})
			if oracle.Responses[0].Status != 500 {
				t.Fatalf("reference bug changed: %d", oracle.Responses[0].Status)
			}
			fake.Reset(seed)
			s := httpxtest.New(t, httpxtest.Options{DataDir: filepath.Join(dir, "go"), Register: httpxtest.Routes(api.Register)})
			before, err := s.Repo.GetProfile(s.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			result := s.Post("/api/routers/router-one/probe")
			result.WantStatus(502)
			if result.Object().GetBool("success") || result.Object().GetObject("probe").GetBool("ok") || result.Object().GetString("message") == "" {
				t.Fatal("failure not represented")
			}
			after, err := s.Repo.GetProfile(s.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			if after.GetInt("_revision") != before.GetInt("_revision")+1 {
				t.Fatal("failed probe revision was not incremented exactly once")
			}
			before.Delete("_revision")
			after.Delete("_revision")
			before.Delete("updated_at")
			after.Delete("updated_at")
			old := before.GetObject("routers").GetList("items")[0].(*jsonx.Object)
			current := after.GetObject("routers").GetList("items")[0].(*jsonx.Object)
			if current.GetObject("last_probe").GetBool("ok") || current.GetObject("last_probe").GetString("message") != result.Object().GetString("message") {
				t.Fatal("failed probe not stored")
			}
			old.Delete("last_probe")
			current.Delete("last_probe")
			a, _ := json.Marshal(before)
			b, _ := json.Marshal(after)
			if string(a) != string(b) {
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
			s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(api.Register), Setup: func(app *httpx.App) {
				app.AfterRequest(func(c *httpx.Ctx, response *httpx.Response) {
					if strings.HasSuffix(c.Path(), "/probe") {
						close(finished)
					}
				})
			}})
			created := s.Post("/api/routers", httpxtest.JSON(routerBody(server.URL)))
			created.WantStatus(200)
			id := created.Object().GetObject("router").GetString("id")
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
	if len(list.Object().GetList("routers")) < 2 || strings.Contains(list.Text(), "synthetic-peer-password") || !strings.Contains(list.Text(), "visible-user") {
		t.Fatalf("bad row or peer credential broke projection: %s", list.Body)
	}
	pending := s.Get("/api/agents/gateway/upstream-routers?profile=default", login)
	pending.WantStatus(200)
	if strings.Contains(pending.Text(), "synthetic-peer-password") || len(pending.Object().GetList("routers")) != 2 {
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
	_, rows, err := service.Records(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(s.Context(), "default", func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		current.GetObject("routers").GetList("items")[0].(*jsonx.Object).Set("rule_sync_interval", "second malformed value")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	constructed := false
	service.NewDriver = func(r *core.Router) core.Driver { constructed = true; return core.NewDriver(r) }
	if _, err = service.PolicyObjects(s.Context(), "default", rows[0]); err == nil || constructed || len(fake.Calls()) != 0 {
		t.Fatalf("display defaults erased changed ownership evidence: %v", err)
	}
}
