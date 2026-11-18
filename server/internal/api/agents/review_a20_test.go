package agents_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// gatewayAgent answers the Linux gateway API of one forwarding target.
type gatewayAgent struct {
	mu     sync.Mutex
	calls  []string
	bodies map[string]string
}

func (a *gatewayAgent) Do(_ context.Context, r agents.Request) (*agents.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, _ := url.Parse(r.URL)
	a.calls = append(a.calls, r.Method+" "+u.Path)
	body := `{"success":true,"supported":true,"managed":{"active":false},"ipv4_forwarding":{"supported":true,"current":"1","persisted":true}}`
	if override, ok := a.bodies[u.Port()]; ok {
		body = override
	}
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
	get := s.Get("/api/agents/gateway-forwarding", admin).WantStatus(200)
	before, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.DataDir, "synthetic-gateway.yaml"), []byte("forwarding: retained\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	revision, err := s.Repo.ConfigRevision(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	files := forwardingFiles(t, s.DataDir)
	commits := 0
	s.Repo.Store().BeforeCommit = func() error {
		commits++
		return errors.New("HEAD must not save")
	}
	t.Cleanup(func() { s.Repo.Store().BeforeCommit = nil })
	for _, body := range []string{"", `{"agent_id":"gw"}`, `{broken`, `[]`} {
		head := s.Do("HEAD", "/api/agents/gateway-forwarding", admin, httpxtest.Body([]byte(body), "application/json")).WantStatus(get.Status)
		if len(head.Body) != 0 || !reflect.DeepEqual(head.Header, get.Header) {
			t.Fatalf("HEAD differs from GET headers/body: headers=%v GET=%v body=%s", head.Header, get.Header, head.Body)
		}
	}
	s.Repo.Store().BeforeCommit = nil
	if commits != 0 {
		t.Fatalf("HEAD entered %d save transactions", commits)
	}
	afterFingerprint, err := s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	afterRevision, err := s.Repo.ConfigRevision(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if afterFingerprint != fingerprint || afterRevision != revision {
		t.Fatal("HEAD changed SQLite state or revision")
	}
	if !reflect.DeepEqual(files, forwardingFiles(t, s.DataDir)) {
		t.Fatal("HEAD changed files")
	}
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

// forwardingFiles includes bytes, modes and mtimes of every ordinary data file;
// SQLite contents (including revision metadata) are checked by Fingerprint.
func forwardingFiles(t *testing.T, root string) map[string]any {
	t.Helper()
	out := map[string]any{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), repo.DatabaseFilename) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = []any{string(data), info.Mode(), info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStepBA20MalformedReportOnlyAffectsItsGateway(t *testing.T) {
	for _, filter := range []string{
		`{"state":[]}`, `{"state":{"token":"synthetic-report-secret"}}`,
		`{"state":"ok","sources":[{"kind":[]}]}`, `{"state":"ok","sources":[{"kind":{}}]}`,
		`{"state":"ok","remedies":[{"source":[]}]}`, `{"state":"ok","remedies":[{"source":{}}]}`,
	} {
		t.Run(filter, func(t *testing.T) {
			s, fake, admin := gatewayForwardingServer(t)
			_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
				healthy := jsonx.Obj("id", "healthy", "name", "Healthy", "service_type", "mihomo", "profile_id", "default",
					"token", "synthetic-healthy-token", "host", "127.0.0.1", "port", 8082, "version", "1.8.43-go",
					"deployment_method", "shell", "status", "online", "last_heartbeat", time.Now().UTC().Format(time.RFC3339Nano))
				current, _ := system.Get("agents")
				system.Set("agents", append(current.([]any), healthy))
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = s.Repo.UpdateProfileTransaction(s.Context(), "default", func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
				c.Set("routers", jsonx.Obj("items", []any{jsonx.Obj("id", "r1", "proxy_gateway_agent_id", "gw"), jsonx.Obj("id", "r2", "proxy_gateway_agent_id", "healthy")}))
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			fake.mu.Lock()
			fake.bodies = map[string]string{"8081": `{"success":true,"supported":true,"ipv4_forwarding":{"supported":true,"current":"1","persisted":true},"forward_filter":` + filter + `}`}
			fake.mu.Unlock()
			response := s.Get("/api/agents/gateway-forwarding", admin).WantStatus(200)
			var result struct {
				Gateways []struct {
					AgentID    string `json:"agent_id"`
					State      string `json:"state"`
					Message    string `json:"message"`
					CanPersist bool   `json:"can_persist"`
				} `json:"gateways"`
			}
			if err := json.Unmarshal(response.Body, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Gateways) != 2 {
				t.Fatalf("gateways: %s", response.Body)
			}
			seen := map[string]bool{}
			for _, row := range result.Gateways {
				seen[row.AgentID] = true
				if row.AgentID == "gw" && (row.State != "error" || row.CanPersist || row.Message == "") {
					t.Fatalf("malformed report not isolated as error: %s", response.Body)
				}
				if row.AgentID == "healthy" && row.State != "persisted" {
					t.Fatalf("healthy gateway lost: %s", response.Body)
				}
			}
			if !seen["gw"] || !seen["healthy"] {
				t.Fatalf("missing gateway: %s", response.Body)
			}
			if strings.Contains(string(response.Body), "synthetic-report-secret") {
				t.Fatal("malformed report leaked")
			}
			if writes := fake.writes(); len(writes) != 0 {
				t.Fatalf("status lookup wrote: %v", writes)
			}
		})
	}
}
