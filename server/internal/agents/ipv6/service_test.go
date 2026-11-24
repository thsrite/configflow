package ipv6

import (
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

type clientFunc func(context.Context, agents.Request) (*agents.Response, error)

func (f clientFunc) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	return f(ctx, r)
}
func response(data any) *agents.Response {
	return &agents.Response{StatusCode: 200, Body: jsonresp.Value(data)}
}
func hostStatus() Document {
	return Document{"success": jsonresp.Value(true), "interface": jsonresp.Value("eth0"), "interface_index": jsonresp.Value(2), "can_prepare": jsonresp.Value(true), "checks": jsonresp.Value([]any{map[string]string{"id": "interface", "status": "pass", "message": "synthetic evidence"}}), "persistence": jsonresp.Value(Document{}), "core": jsonresp.Value(Document{"ipv6": jsonresp.Value(true), "dns.ipv6": jsonresp.Value(false)})}
}
func fixture(t *testing.T) (*Service, context.Context) {
	t.Helper()
	r, e := repo.Open(t.TempDir(), repo.Options{})
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	m, e := agents.New(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	e = r.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		var src model.Agent
		data := jsonresp.Value(map[string]any{
			"id": "source", "host": "192.0.2.2", "port": 8080, "token": "synthetic-source-token",
			"profile_id": "default", "service_type": "mihomo", "deployment_method": "shell",
			"enabled": true, "status": "online", "last_heartbeat": time.Now().Format(time.RFC3339),
			"version": "1.8.50-go", "unknown_member": map[string]any{"keep": true},
		})
		if err := json.Unmarshal(data, &src); err != nil {
			return err
		}
		sys.Agents = []model.Agent{src}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	_, e = r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		p.Mihomo.CustomConfig = "ipv6: true\ndns: {ipv6: false}\n"
		p.Mihomo.LinuxGateway.UnmarshalJSON(json.RawMessage(`{"enabled":true}`))
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	m.SetClient(clientFunc(func(context.Context, agents.Request) (*agents.Response, error) { return response(hostStatus()), nil }))
	return &Service{Manager: m}, ctx
}
func requireCode(t *testing.T, e error, code string, status int) {
	t.Helper()
	var boundary *Error
	if !errors.As(e, &boundary) || boundary.Code != code || boundary.Status != status {
		t.Fatalf("error=%v want=%s/%d", e, code, status)
	}
}
func startRuntime(t *testing.T) {
	t.Helper()
	if e := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 10 * time.Millisecond}); e != nil {
		t.Fatal(e)
	}
	lifecycle.Start(context.Background())
	t.Cleanup(func() { lifecycle.Stop(time.Second) })
}
func TestReadinessCachePrepareAndStale(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	var mu sync.Mutex
	calls := []string{}
	fail := false
	s.Manager.SetClient(clientFunc(func(ctx context.Context, r agents.Request) (*agents.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, errors.New("synthetic unreachable")
		}
		u, _ := url.Parse(r.URL)
		calls = append(calls, u.Path)
		h := hostStatus()
		if strings.HasSuffix(u.Path, "/prepare") {
			h["checks"] = jsonresp.Value([]map[string]string{{"id": "tun_recovery", "message": "synthetic recovery"}})
		}
		return response(h), nil
	}))
	first, e := s.Status(ctx, "source", "default", false)
	if e != nil || !first.CanPrepare || len(first.CheckToken) != 64 {
		t.Fatalf("status=%+v err=%v", first, e)
	}
	cached, e := s.Status(ctx, "source", "default", false)
	if e != nil || cached.Cached == nil || !*cached.Cached || len(calls) != 1 {
		t.Fatalf("cache=%+v err=%v calls=%v", cached, e, calls)
	}
	result, e := s.Prepare(ctx, "source", "default", jsonresp.Value(Document{"interface": jsonresp.Value(first.PrepareInterface), "check_token": jsonresp.Value(first.CheckToken)}))
	if e != nil || result.ChangedRoutes == nil || *result.ChangedRoutes != 0 {
		t.Fatalf("prepare=%+v err=%v", result, e)
	}
	if !strings.Contains(string(jsonresp.Value(result)), "prepare_tun_recovery") {
		t.Fatal("recovery warning lost")
	}
	if got := strings.Join(calls, ","); got != "/api/ipv6-readiness,/api/ipv6-readiness,/api/ipv6-readiness/prepare,/api/ipv6-readiness" {
		t.Fatal(got)
	}
	// A target mutation must fail before host prepare, and not leak its new token.
	e = s.update(ctx, "source", func(_ context.Context, a Document, _ []Document) error {
		a["token"] = jsonresp.Value("synthetic-reinstalled-token")
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Prepare(ctx, "source", "default", jsonresp.Value(Document{"interface": jsonresp.Value("eth0"), "check_token": jsonresp.Value(first.CheckToken)}))
	requireCode(t, e, "CHECK_CHANGED", 409)
	mu.Lock()
	fail = true
	mu.Unlock()
	stale, e := s.Status(ctx, "source", "default", true)
	if e != nil || stale.Stale == nil || !*stale.Stale || stale.RefreshError == nil {
		t.Fatalf("stale=%+v err=%v", stale, e)
	}
	// Corrupt/foreign cache files cannot authorize another Agent/profile.
	if e = os.WriteFile(s.lastPath("source"), []byte(`{"agent_id":"other","profile_id":"default","key":"x","result":{}}`), 0600); e != nil {
		t.Fatal(e)
	}
	_, e = s.Status(ctx, "source", "default", false)
	requireCode(t, e, "AGENT_UNREACHABLE", 502)
}
func TestTransportRawRequestsAndClasses(t *testing.T) {
	s, ctx := fixture(t)
	var method, path, authorization, contentType, body string
	status := 200
	answer := `{"success":true}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, path, authorization, contentType, body = r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(b)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	p, _ := strconv.Atoi(u.Port())
	a := Document{"host": jsonresp.Value(u.Hostname()), "port": jsonresp.Value(p), "token": jsonresp.Value("synthetic-source-token")}
	s.Manager.SetClient(clientFunc(func(ctx context.Context, r agents.Request) (*agents.Response, error) {
		if !r.NoProxy || !r.NoRedirects || r.MaxBody != 512*1024 || r.Timeout != agents.Pair(3, 45) {
			t.Errorf("production transport bounds missing: %+v", r)
		}
		return (agents.HTTPClient{}).Do(ctx, r)
	}))
	if _, e := s.request(ctx, a, "", nil); e != nil {
		t.Fatal(e)
	}
	if method != "GET" || path != "/api/ipv6-readiness" || authorization != "Bearer synthetic-source-token" || contentType != "" || body != "" {
		t.Fatalf("raw GET %q %q %q %q %q", method, path, authorization, contentType, body)
	}
	if _, e := s.request(ctx, a, "probe", Document{"host": jsonresp.Value("2001:db8::2"), "port": jsonresp.Value(443)}); e != nil {
		t.Fatal(e)
	}
	if method != "POST" || path != "/api/ipv6-readiness/probe" || contentType != "application/json" || body != `{"host":"2001:db8::2","port":443}` {
		t.Fatalf("raw POST %q %q %q %q", method, path, contentType, body)
	}
	for _, tc := range []struct {
		name       string
		status     int
		body, code string
		want       int
	}{{"upgrade", 404, `{}`, "UPGRADE_REQUIRED", 409}, {"auth", 401, `{}`, "AGENT_AUTH_FAILED", 502}, {"json", 200, `{`, "INVALID_RESPONSE", 502}, {"shape", 200, `[]`, "INVALID_RESPONSE", 502}, {"failure", 422, `{"success":false,"message":"synthetic-source-token"}`, "IPV6_OPERATION_FAILED", 422}, {"too-large", 200, strings.Repeat("x", 512*1024+1), "INVALID_RESPONSE", 502}} {
		t.Run(tc.name, func(t *testing.T) {
			status, answer = tc.status, tc.body
			_, e := s.request(ctx, a, "", nil)
			requireCode(t, e, tc.code, tc.want)
			if strings.Contains(e.Error(), "synthetic-source-token") {
				t.Fatal("token echo")
			}
		})
	}
}
func TestReadinessRuntimeDisconnectAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			startRuntime(t)
			s, base := fixture(t)
			entered, gate := make(chan struct{}), make(chan struct{})
			s.Manager.SetClient(clientFunc(func(ctx context.Context, r agents.Request) (*agents.Response, error) {
				close(entered)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-gate:
					return response(hostStatus()), nil
				}
			}))
			request, cancel := context.WithCancel(lifecycle.BindRequest(base))
			defer cancel()
			done := make(chan error, 1)
			go func() { _, e := s.Status(request, "source", "default", true); done <- e }()
			select {
			case <-entered:
			case e := <-done:
				t.Fatalf("owner exited before I/O: %v", e)
			case <-time.After(5 * time.Second):
				t.Fatal("owner did not reach I/O")
			}
			if shutdown {
				lifecycle.Stop(time.Second)
			} else {
				cancel()
			}
			close(gate)
			e := <-done
			_, fileError := os.Stat(s.lastPath("source"))
			if shutdown {
				if e == nil || !os.IsNotExist(fileError) {
					t.Fatalf("shutdown wrote cache err=%v stat=%v", e, fileError)
				}
				_, e = s.Prepare(request, "source", "default", jsonresp.Value(Document{"interface": jsonresp.Value("eth0"), "check_token": jsonresp.Value(strings.Repeat("a", 64))}))
				if e == nil {
					t.Fatal("post-shutdown prepare accepted")
				}
			} else if e != nil || fileError != nil {
				t.Fatalf("disconnect aborted owner %v %v", e, fileError)
			}
		})
	}
}

func TestPrepareDeviceWriteDisconnectAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			startRuntime(t)
			s, base := fixture(t)
			first, e := s.Status(base, "source", "default", true)
			if e != nil {
				t.Fatal(e)
			}
			entered, gate := make(chan struct{}), make(chan struct{})
			var committed atomic.Int32
			s.Manager.SetClient(clientFunc(func(ctx context.Context, r agents.Request) (*agents.Response, error) {
				if strings.HasSuffix(r.URL, "/prepare") {
					close(entered)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-gate:
					}
					if e := ctx.Err(); e != nil {
						return nil, e
					}
					committed.Add(1)
				}
				return response(hostStatus()), nil
			}))
			request, cancel := context.WithCancel(lifecycle.BindRequest(base))
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, e := s.Prepare(request, "source", "default", jsonresp.Value(Document{"interface": jsonresp.Value("eth0"), "check_token": jsonresp.Value(first.CheckToken)}))
				done <- e
			}()
			select {
			case <-entered:
			case e := <-done:
				t.Fatalf("prepare exited before write: %v", e)
			case <-time.After(5 * time.Second):
				t.Fatal("prepare did not reach write")
			}
			if shutdown {
				lifecycle.Stop(time.Second)
			} else {
				cancel()
			}
			close(gate)
			e = <-done
			if shutdown {
				if e == nil || committed.Load() != 0 {
					t.Fatal("shutdown device write", e, committed.Load())
				}
			} else if e != nil || committed.Load() != 1 {
				t.Fatal("disconnect dropped device write", e, committed.Load())
			}
		})
	}
}

func TestReadinessReusesRouterOwnerAndSetupSuborder(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	var reads atomic.Int32
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("readiness attempted router mutation", r.Method)
		}
		reads.Add(1)
		_, _ = io.WriteString(w, `[]`)
	}))
	defer device.Close()
	_, e := s.Manager.Repository().UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		r1 := model.Router{ID: "router-one", Type: "routeros", Endpoint: device.URL, Username: "api", Password: "synthetic-router-password", ProxyGatewayAgentID: "source"}
		r2 := r1
		r2.ID = "router-clone"
		r2.Endpoint = device.URL + "/"
		p.Routers.Items = []model.Router{r1, r2}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	first, e := s.Status(ctx, "source", "default", true)
	if e != nil || len(first.Routers) != 2 || reads.Load() == 0 {
		t.Fatal(first, e)
	}
	setup := lockx.NewMutex(lockx.LevelSetupSession, "test-setup-session")
	held, release, e := setup.Lock(ctx, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	before := lockx.Violations()
	_, e = s.Prepare(held, "source", "default", jsonresp.Value(Document{"interface": jsonresp.Value("eth0"), "check_token": jsonresp.Value(first.CheckToken)}))
	if e != nil {
		var boundary *Error
		if !errors.As(e, &boundary) || boundary.Code != "PREPARE_UNAVAILABLE" {
			t.Fatal(e)
		}
	}
	if lockx.Violations() != before {
		t.Fatal("setup/Agent/router/repo lock order violated")
	}
}

func TestLinkedRouterBadRowsRemainIsolated(t *testing.T) {
	config := Document{"routers": json.RawMessage(`{"items":[false,null,"bad",{"id":"healthy","type":"routeros","endpoint":"https://router.example","proxy_gateway_agent_id":"source"}]}`)}
	if got := linked(config, "source"); len(got) != 1 || got[0].ID != "healthy" {
		t.Fatal("bad peer hid healthy router", got)
	}
}

func TestCapturedCredentialsSurviveConcurrentCredentialReplacement(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "report", true: "error"}[reject], func(t *testing.T) {
			startRuntime(t)
			s, ctx := fixture(t)
			e := s.Manager.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
				var peer model.Agent
				if err := json.Unmarshal([]byte(`{"id":"peer","profile_id":"default","token":"synthetic-peer-old-token","password":"synthetic-peer-old-password"}`), &peer); err != nil {
					return err
				}
				sys.Agents = append(sys.Agents, peer)
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			s.Manager.SetClient(clientFunc(func(ctx context.Context, _ agents.Request) (*agents.Response, error) {
				if e := s.update(ctx, "peer", func(_ context.Context, a Document, _ []Document) error {
					a["token"] = jsonresp.Value("synthetic-peer-new-token")
					a["password"] = jsonresp.Value("synthetic-peer-new-password")
					return nil
				}); e != nil {
					t.Error(e)
				}
				if reject {
					return &agents.Response{StatusCode: 422, Body: []byte(`{"success":false,"message":"synthetic-peer-old-token synthetic-peer-old-password"}`)}, nil
				}
				h := hostStatus()
				h["checks"] = jsonresp.Value([]Check{check("source", "source", "pass", "synthetic-peer-old-token synthetic-peer-old-password")})
				return response(h), nil
			}))
			result, e := s.Status(ctx, "source", "default", true)
			if reject {
				requireCode(t, e, "IPV6_OPERATION_FAILED", 422)
				if strings.Contains(e.Error(), "synthetic-peer-old") {
					t.Fatal("captured error leaked old peer credentials", e)
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				if strings.Contains(string(jsonresp.Value(result)), "synthetic-peer-old") {
					t.Fatal("captured report leaked old peer credentials")
				}
			}
		})
	}
}
