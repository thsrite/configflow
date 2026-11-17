package agents_test

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

// gatewayAgent answers the Linux gateway API of one forwarding target.
type gatewayAgent struct {
	mu    sync.Mutex
	calls []string
}

func (a *gatewayAgent) Do(_ context.Context, r agents.Request) (*agents.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, _ := url.Parse(r.URL)
	a.calls = append(a.calls, r.Method+" "+u.Path)
	body := `{"success":true,"supported":true,"managed":{"active":false},"ipv4_forwarding":{"supported":true,"current":"1","persisted":true}}`
	return &agents.Response{StatusCode: 200, Header: http.Header{}, Body: []byte(body)}, nil
}

func (a *gatewayAgent) writes() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, call := range a.calls {
		if call != "GET /api/linux-gateway/status" {
			out = append(out, call)
		}
	}
	return out
}

func gatewayForwardingServer(t *testing.T) (*httpxtest.Server, *gatewayAgent, httpxtest.Option) {
	t.Helper()
	reset := func() { agents.ResetRuntime(); agents.ResetHooks() }
	reset()
	t.Cleanup(reset)
	fake := &gatewayAgent{}
	t.Cleanup(agents.SetDefaultClient(fake))
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(apiagents.Register)})
	ctx := s.Context()
	_, err := s.Repo.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{jsonx.Obj("id", "gw", "name", "GW", "service_type", "mihomo", "profile_id", "default",
			"token", "synthetic-agent-token", "host", "127.0.0.1", "port", 8081, "version", "1.8.43-go",
			"deployment_method", "shell", "status", "online", "last_heartbeat", time.Now().UTC().Format(time.RFC3339Nano))})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.Set("routers", jsonx.Obj("items", []any{jsonx.Obj("id", "r1", "name", "Router", "proxy_gateway_agent_id", "gw")}))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, fake, s.Login()
}

// A20 review: HEAD is served by the GET route. Python treated every method
// other than GET as the POST branch, so an authorised HEAD carrying a JSON
// body persisted forwarding on the device. HEAD must stay read-only.
func TestReviewA20HeadGatewayForwardingIsReadOnly(t *testing.T) {
	s, fake, admin := gatewayForwardingServer(t)
	before, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	s.Do("HEAD", "/api/agents/gateway-forwarding", admin, httpxtest.JSON(map[string]any{"agent_id": "gw"})).WantStatus(200)
	if writes := fake.writes(); len(writes) != 0 {
		t.Fatalf("HEAD wrote to the Agent: %v", writes)
	}
	after, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonx.Equal(before, after) {
		t.Fatal("HEAD changed stored state")
	}
	// Control: the same body through POST does reach the device.
	s.Post("/api/agents/gateway-forwarding", admin, httpxtest.JSON(map[string]any{"agent_id": "gw"})).WantStatus(200)
	if writes := fake.writes(); len(writes) != 1 || writes[0] != "POST /api/linux-gateway/forwarding" {
		t.Fatalf("POST control did not persist forwarding: %v", writes)
	}
}
