package host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/gateway"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/setupflow"
)

type testManager struct {
	record map[string]any
	client agents.Client
}

func (m *testManager) ReadAgent(context.Context, string, bool) (*model.Agent, error) {
	raw, _ := json.Marshal(m.record)
	var out model.Agent
	err := json.Unmarshal(raw, &out)
	return &out, err
}
func (m *testManager) Client() agents.Client { return m.client }

type recordClient struct {
	requests []agents.Request
	call     func(context.Context, agents.Request) (*agents.Response, error)
}

func (c *recordClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	c.requests = append(c.requests, r)
	if c.call != nil {
		return c.call(ctx, r)
	}
	return &agents.Response{StatusCode: 200, Body: []byte(`{"success":true,"supported":true,"managed":{"active":false},"ipv4_forwarding":{"supported":true}}`)}, nil
}
func newBridge(t *testing.T) (*Bridge, *recordClient) {
	t.Helper()
	setupflow.Wire()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		p.Mihomo.LinuxGateway.UnmarshalJSON(json.RawMessage(`{"enabled":true,"rule_set_ids":[]}`))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &recordClient{}
	m := &testManager{record: map[string]any{"id": "gw", "name": "GW", "host": "127.0.0.1", "port": 80, "token": "synthetic-token", "profile_id": "default", "service_type": "mihomo", "version": "1.8.43-go", "deployment_method": "shell", "status": "online", "last_heartbeat": time.Now().UTC().Format(time.RFC3339Nano)}, client: client}
	return &Bridge{r, m}, client
}
func runtimeContext(t *testing.T, stopped bool) (context.Context, context.CancelFunc) {
	t.Helper()
	runtime := lifecycle.NewRuntime(lifecycle.Options{Mode: lifecycle.ModeManual, Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	request, cancel := context.WithCancel(context.Background())
	ctx := runtime.BindRequest(request)
	if stopped {
		runtime.Shutdown(context.Background())
	} else {
		t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	}
	return ctx, cancel
}

func TestWriteOwnersRuntimeAndDisconnect(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		for _, op := range []string{"apply", "remove", "forwarding", "persist", "raw"} {
			t.Run(strconv.FormatBool(stopped)+"/"+op, func(t *testing.T) {
				b, client := newBridge(t)
				ctx, cancel := runtimeContext(t, stopped)
				defer cancel()
				cancel()
				var err error
				switch op {
				case "persist":
					_, err = b.PersistForwarding(ctx, "gw", []RouterRef{{ID: "r"}})
				case "raw":
					_, err = b.Request(ctx, fixtureAgent(b.Manager.(*testManager).record), "remove", "POST", json.RawMessage(`{}`))
				default:
					body := json.RawMessage(`{}`)
					if op == "apply" {
						body = json.RawMessage(`{"interface":"eth0","address":"192.0.2.2","lan_cidr":"192.0.2.0/24","upstream_gateway":"192.0.2.1"}`)
					}
					if op == "forwarding" {
						_, e := b.Repository.UpdateProfile(context.Background(), "default", func(_ context.Context, p *model.Profile) error {
							p.Routers.Items = []model.Router{{ID: "r", ProxyGatewayAgentID: "gw"}}
							return nil
						})
						if e != nil {
							t.Fatal(e)
						}
					}
					_, err = b.Call(ctx, "gw", op, "POST", "default", body)
				}
				if stopped {
					if !errors.Is(err, context.Canceled) || len(client.requests) != 0 {
						t.Fatalf("stopped owner err=%v requests=%v", err, client.requests)
					}
				} else {
					if err != nil || len(client.requests) == 0 {
						t.Fatalf("disconnected owner err=%v requests=%v", err, client.requests)
					}
				}
			})
		}
	}
}

func TestAgentRawWireAndProductionBounds(t *testing.T) {
	var records []struct{ method, body, accept, auth, content string }
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		records = append(records, struct{ method, body, accept, auth, content string }{r.Method, string(raw), r.Header.Get("Accept"), r.Header.Get("Authorization"), r.Header.Get("Content-Type")})
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer fake.Close()
	b, client := newBridge(t)
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(fake.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	raw := b.Manager.(*testManager).record
	raw["port"] = port
	client.call = func(ctx context.Context, r agents.Request) (*agents.Response, error) {
		if r.Timeout != agents.Pair(3, 45) || r.MaxBody != MaxResponseBytes || !r.NoProxy || !r.NoRedirects {
			t.Fatalf("production limits changed: %+v", r)
		}
		return (agents.HTTPClient{}).Do(ctx, r)
	}
	for _, op := range []string{"status", "preview", "apply", "remove", "forwarding"} {
		method := allowed[op]
		var body json.RawMessage
		if method == "POST" {
			body = json.RawMessage(`{}`)
		}
		if _, err := b.Request(context.Background(), fixtureAgent(raw), op, method, body); err != nil {
			t.Fatal(err)
		}
	}
	if len(records) != 5 {
		t.Fatal(records)
	}
	for i, r := range records {
		wantBody, wantType := "{}", "application/json"
		if i == 0 {
			wantBody, wantType = "", ""
		}
		if r.body != wantBody || r.content != wantType || r.accept != "application/json" || r.auth != "Bearer synthetic-token" {
			t.Fatalf("wire %+v", r)
		}
	}
}

func TestResponseRefusalAndPrivacy(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, code string
	}{{"oversize", 200, strings.Repeat("x", MaxResponseBytes+1), "INVALID_RESPONSE"}, {"badjson", 200, "{", "INVALID_RESPONSE"}, {"list", 200, "[]", "INVALID_RESPONSE"}, {"null", 200, "null", "INVALID_RESPONSE"}, {"auth", 403, "{}", "AGENT_AUTH_FAILED"}, {"upgrade", 404, "{}", "UPGRADE_REQUIRED"}, {"refused", 422, `{"message":"拒绝"}`, "GATEWAY_ERROR"}} {
		t.Run(tc.name, func(t *testing.T) {
			b, c := newBridge(t)
			c.call = func(context.Context, agents.Request) (*agents.Response, error) {
				return &agents.Response{StatusCode: tc.status, Body: []byte(tc.body)}, nil
			}
			_, err := b.Call(context.Background(), "gw", "status", "GET", "default", nil)
			var e *monitor.Error
			if !errors.As(err, &e) || e.Code != tc.code {
				t.Fatalf("%v", err)
			}
		})
	}
	b, c := newBridge(t)
	c.call = func(context.Context, agents.Request) (*agents.Response, error) {
		return &agents.Response{StatusCode: 409, Body: []byte(`{"message":"api operator password=synthetic-password; synthetic-token"}`)}, nil
	}
	_, err := b.Call(context.Background(), "gw", "status", "GET", "default", nil)
	if err == nil || strings.Contains(err.Error(), "synthetic-password") || strings.Contains(err.Error(), "synthetic-token") {
		t.Fatal(err)
	}
}

func TestSpecAndBundleGuard(t *testing.T) {
	tests := []struct {
		raw   string
		valid bool
	}{{`{"interface":"eth0","address":"192.0.2.2","lan_cidr":"192.0.2.0/24","upstream_gateway":"192.0.2.1"}`, true}, {`{"interface":"eth0","address":"192.0.2.0","lan_cidr":"192.0.2.0/24","upstream_gateway":"192.0.2.1"}`, false}, {`{"interface":"eth0","address":"192.0.2.2","lan_cidr":"192.0.2.2/24","upstream_gateway":"192.0.2.1"}`, false}, {`[]`, false}, {`{"interface":"lo"}`, false}}
	for _, tc := range tests {
		_, err := ParseSpec(json.RawMessage(tc.raw))
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.raw, err)
		}
	}
	a := &model.Agent{ServiceType: "mihomo", Version: "1.8.24-go"}
	if err := RequireGatewayBundleAgent(a, []byte("x-configflow-linux-gateway: {enabled: true}\n")); err == nil {
		t.Fatal("old gateway accepted")
	}
	if err := RequireGatewayBundleAgent(a, []byte("mixed-port: 7890\n")); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"a: &a [*a]\n", "x: 1\nx: 2\n"} {
		if err := RequireGatewayBundleAgent(a, []byte(body)); err == nil {
			t.Fatal("unsafe YAML accepted")
		}
	}
}

func FuzzGatewaySpec(f *testing.F) {
	for _, s := range []string{`{}`, `null`, `{"interface":"eth0","address":"192.0.2.2","lan_cidr":"192.0.2.0/24","upstream_gateway":"192.0.2.1"}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		spec, err := ParseSpec(json.RawMessage(s))
		if err == nil {
			raw, _ := json.Marshal(spec)
			again, err := ParseSpec(raw)
			if err != nil || again != spec {
				t.Fatal("valid spec not stable", err)
			}
		}
	})
}
func FuzzGatewayForwardFilter(f *testing.F) {
	for _, s := range []string{`{}`, `null`, `{"state":"may_drop","device":"Meta","remedies":[{"source":"nftables","device":"Meta","commands":["nft 'insert rule inet filter forward oifname \"Meta\" accept'"]}]}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		filter := SanitizeForwardFilter(json.RawMessage(s))
		if filter == nil {
			return
		}
		raw, err := json.Marshal(filter)
		if err != nil {
			t.Fatal(err)
		}
		again := SanitizeForwardFilter(raw)
		other, err := json.Marshal(again)
		if err != nil || string(raw) != string(other) {
			t.Fatal("filter not idempotent", string(raw), string(other))
		}
	})
}

func TestShutdownDuringPreflightPreventsAgentWrite(t *testing.T) {
	b, c := newBridge(t)
	b.Manager.(*testManager).record["version"] = "1.8.40-go"
	runtime := lifecycle.NewRuntime(lifecycle.Options{Mode: lifecycle.ModeManual, Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	ctx := runtime.BindRequest(context.Background())
	c.call = func(_ context.Context, r agents.Request) (*agents.Response, error) {
		if r.Method != "GET" {
			t.Fatal("write after shutdown")
		}
		runtime.Shutdown(context.Background())
		return &agents.Response{StatusCode: 200, Body: []byte(`{"success":true,"supported":true,"ipv4_forwarding":{"supported":true}}`)}, nil
	}
	_, err := b.PersistForwarding(ctx, "gw", []RouterRef{{ID: "r"}})
	if !errors.Is(err, context.Canceled) || len(c.requests) != 1 {
		t.Fatalf("err=%v requests=%+v", err, c.requests)
	}
}

func TestMalformedJournalIsNotEmptyEvidence(t *testing.T) {
	for _, raw := range []string{`{"managed":"unreadable"}`, `{"managed":{"active":[]}}`, `{"managed":{"pending":{}}}`, `{"managed":{"error":"unreadable"}}`} {
		status := object(json.RawMessage(raw))
		if !HasRecord(status) || KeepsForwarding(status) {
			t.Fatalf("malformed journal proved absence: %s", raw)
		}
	}
}
func FuzzGatewayBundle(f *testing.F) {
	for _, s := range []string{"mixed-port: 7890\n", "x-configflow-linux-gateway: {enabled: true}\n", "a: &a [*a]\n", "a: &a [1,2]\nb: [*a,*a]\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_ = RequireGatewayBundleAgent(&model.Agent{ServiceType: "mihomo", Version: "1.8.24-go"}, []byte(s))
	})
}

func TestDeviceUsernameIsNotCredential(t *testing.T) {
	b, c := newBridge(t)
	c.call = func(context.Context, agents.Request) (*agents.Response, error) {
		return &agents.Response{StatusCode: 409, Body: []byte(`{"message":"operator api rest-api"}`)}, nil
	}
	_, err := b.Call(context.Background(), "gw", "status", "GET", "default", nil)
	if err == nil || err.Error() != "operator api rest-api" {
		t.Fatal("username destroyed diagnostics", err)
	}
}

func TestPublicationRefusalsKeepGatewayErrorAndCause(t *testing.T) {
	b, c := newBridge(t)
	c.call = func(context.Context, agents.Request) (*agents.Response, error) {
		return &agents.Response{StatusCode: 200, Body: []byte(`{"managed":{"active":true}}`)}, nil
	}
	for _, check := range []func(context.Context, string, string) error{b.RequireForwardingKept, b.RequireGatewayOffKept} {
		err := check(context.Background(), "gw", "default")
		var gatewayErr *gateway.Error
		if !errors.As(err, &gatewayErr) {
			t.Fatal("publication refusal lost class", err)
		}
	}
	c.call = func(context.Context, agents.Request) (*agents.Response, error) {
		return nil, errors.New("synthetic transport")
	}
	err := b.RequireForwardingKept(context.Background(), "gw", "default")
	var gatewayErr *gateway.Error
	var monitorErr *monitor.Error
	if !errors.As(err, &gatewayErr) || !errors.As(err, &monitorErr) {
		t.Fatal("publication transport refusal lost chain", err)
	}
}

func fixtureAgent(value any) *model.Agent {
	raw, _ := json.Marshal(value)
	var out model.Agent
	_ = json.Unmarshal(raw, &out)
	return &out
}

// TestRouterLinkStoredIdentity: router records are plain (O18 T1c), so a
// stored number in proxy_gateway_agent_id reads as its literal text and links
// the Agent with that ID (accepted difference; R5 kept the number and refused
// the link). Values of another kind read as no link.
func TestRouterLinkStoredIdentity(t *testing.T) {
	tests := []struct {
		name   string
		stored string
		want   int
	}{
		{"text", `"1"`, 1},
		{"number reads as its text", `1`, 1},
		{"boolean", `true`, 0},
		{"object", `{"id":"1"}`, 0},
		{"null", `null`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := model.DecodeConfig([]byte(`{"routers":{"items":[{"id":"synthetic","proxy_gateway_agent_id":` + tt.stored + `}]}}`))
			if err != nil {
				t.Fatal(err)
			}
			if got := len(ProxyRouters(config, "1")); got != tt.want {
				t.Fatalf("linked routers = %d, want %d", got, tt.want)
			}
		})
	}
}
