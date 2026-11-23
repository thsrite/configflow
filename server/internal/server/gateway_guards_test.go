package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/gateway"
	"configflow/internal/gateway/host"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

// gatewayAgent is a loopback Mihomo Agent that reports a Linux gateway
// journal and records every request path.
type gatewayAgent struct {
	mu     sync.Mutex
	paths  []string
	status string
}

func (g *gatewayAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.paths = append(g.paths, r.Method+" "+r.URL.Path)
	status := g.status
	g.mu.Unlock()
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/linux-gateway/status") {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(status))
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
}

func (g *gatewayAgent) calls() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.paths...)
}

func newGatewayGuardFixture(t *testing.T, config json.RawMessage) (context.Context, *agents.Manager, *gatewayAgent) {
	t.Helper()
	fake := &gatewayAgent{status: `{"managed":{"active":true}}`}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	n, _ := strconv.Atoi(port)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(repo.WithRepository(context.Background(), r))
	if config != nil {
		if _, _, err = r.SaveProfileFieldsJSON(ctx, "default", config, nil); err != nil {
			t.Fatal(err)
		}
	}
	err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{testjson.Decode[model.Agent](t, map[string]any{
			"id": "gw", "name": "GW", "host": host, "port": n, "token": "synthetic-guard-token",
			"profile_id": "default", "service_type": "mihomo", "version": "1.8.60-go",
			"deployment_method": "shell", "enabled": true,
		})}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := agents.New(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return ctx, m, fake
}

// A14 + A20: both registered guards read the live host journal and refuse a
// publication whose host change would drop IPv4 forwarding. The refusal is a
// preparation failure (push 400, recorded failed/preparing), and nothing but
// the read-only status request reaches the device.
func TestGatewayGuardsRefuseUnsafePublication(t *testing.T) {
	for name, guard := range map[string]func(context.Context, *agents.Manager, deploy.Target) error{
		"forwarding-kept": func(ctx context.Context, m *agents.Manager, t deploy.Target) error {
			return (&host.Bridge{Repository: m.Repository(), Manager: m}).RequireForwardingKept(ctx, t.ID, t.ProfileID)
		},
		"gateway-off": func(ctx context.Context, m *agents.Manager, t deploy.Target) error {
			return (&host.Bridge{Repository: m.Repository(), Manager: m}).RequireGatewayOffKept(ctx, t.ID, t.ProfileID)
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, m, fake := newGatewayGuardFixture(t, nil)
			target := deploy.Target{ID: "gw", ProfileID: "default", ServiceType: "mihomo"}
			err := guard(ctx, m, target)
			var refusal *gateway.Error
			if !errors.As(err, &refusal) || !deploy.IsPreparationFailure(err) {
				t.Fatal("unsafe host change was not refused as a preparation failure", err)
			}
			for _, call := range fake.calls() {
				if call != "GET /api/linux-gateway/status" {
					t.Fatal("guard wrote to the device", call)
				}
			}
			fake.mu.Lock()
			fake.status = `{"managed":null}`
			fake.mu.Unlock()
			if err = guard(ctx, m, target); err != nil {
				t.Fatal("host without a gateway record refused", err)
			}
		})
	}
}

// The production wiring installs the guards: a full-TUN publication to a
// host whose gateway record would turn forwarding off stops before any upload.
func TestNewAppGatewayGuardBlocksFullTunPublication(t *testing.T) {
	NewApp()
	marker := map[string]any{"version": 1, "takeover": "full_tun", "route_rule_set_ids": []any{}, "origin": "setup"}
	cfg, err := json.Marshal(map[string]any{
		"mihomo":        map[string]any{"custom_config": "mode: rule\ntun:\n  enable: true\n  stack: mixed\n  auto-route: true\n  auto-redirect: true\n  auto-detect-interface: true\n  dns-hijack: ['any:53', 'tcp://any:53']\n", "router_split": marker},
		"nodes":         []any{},
		"subscriptions": []any{},
		"proxy_groups":  []any{},
		"rule_configs":  []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, m, fake := newGatewayGuardFixture(t, cfg)
	response := deploy.PushConfig(ctx, m, "gw", deploy.PushRequest{Body: []byte(`{"deployment_id":"guarded"}`), Scheme: "https", Host: "panel.example.com"})
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	raw, _ := json.Marshal(response.Body)
	if response.Status != 400 || !strings.Contains(string(raw), "preparing") {
		t.Fatalf("full-TUN publication not refused by the guard: %d %s", response.Status, raw)
	}
	statusRead := false
	for _, call := range fake.calls() {
		if call == "GET /api/linux-gateway/status" {
			statusRead = true
		} else {
			t.Fatal("device received", call)
		}
	}
	if !statusRead {
		t.Fatalf("refusal did not come from the live guard: %s", raw)
	}
}
