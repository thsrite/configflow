package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// O9 round 2 L1: every Agent HTTP call has a total duration upper bound derived
// from Connect + Read * TotalTimeoutMultiplier, so a slow-drip peer cannot hold
// a goroutine or device lock indefinitely while normal multi-chunk replies
// still complete within budget.
func TestO9R2TotalTimeout(t *testing.T) {
	const (
		step    = 25 * time.Millisecond
		timeout = 60 * time.Millisecond // Connect=60ms, Read=60ms -> total = 60 + 5*60 = 360ms
	)
	tests := []struct {
		name     string
		chunks   int
		maxBody  int64
		wantFail bool
	}{
		{"default body slow-drip exceeds total cap", 40, 0, true},
		{"bounded body slow-drip exceeds total cap", 40, 4096, true},
		{"negative control: multi-chunk within total cap succeeds", 4, 0, false},
		{"negative control: bounded multi-chunk within total cap succeeds", 4, 4096, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				flusher, _ := w.(http.Flusher)
				for i := 0; i < tc.chunks; i++ {
					select {
					case <-r.Context().Done():
						return
					case <-time.After(step):
					}
					_, _ = w.Write([]byte("a"))
					if flusher != nil {
						flusher.Flush()
					}
				}
			}))
			defer srv.Close()

			resp, err := HTTPClient{}.Do(context.Background(), Request{
				Method:  http.MethodGet,
				URL:     srv.URL + "/api/status",
				Timeout: Timeout{Connect: timeout, Read: timeout},
				NoProxy: true,
				MaxBody: tc.maxBody,
			})
			if tc.wantFail {
				if tc.maxBody > 0 {
					if err != nil {
						if !errors.Is(err, ErrTotalTimeout) || !IsTransportError(err) {
							t.Fatalf("err = %v, want TransportError(ErrTotalTimeout)", err)
						}
						return
					}
					if resp == nil || !errors.Is(resp.BodyError, ErrTotalTimeout) || !IsTransportError(resp.BodyError) {
						t.Fatalf("BodyError = %v, want TransportError(ErrTotalTimeout)", resp.BodyError)
					}
					return
				}
				if !errors.Is(err, ErrTotalTimeout) || !IsTransportError(err) {
					t.Fatalf("err = %v, want TransportError(ErrTotalTimeout)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.BodyError != nil || len(resp.Body) != tc.chunks {
				t.Fatalf("resp body=%d BodyError=%v, want %d bytes and nil error", len(resp.Body), resp.BodyError, tc.chunks)
			}
		})
	}
}

// O9 round 2 L1 + PORTING rule 4b: non-GET remote calls must not abort when the
// HTTP client disconnects; the total duration cap still applies independently.
func TestO9R2NonGETSurvivesClientDisconnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"done":true}`))
	}))
	defer srv.Close()

	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rootCtx := repo.WithRepository(repo.WithScope(context.Background()), r)
	rt.Start(rootCtx)
	t.Cleanup(func() { rt.Shutdown(context.Background()) })
	baseCtx := rt.BindRequest(rootCtx)
	m, err := New(baseCtx, r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	hostPort := strings.TrimPrefix(srv.URL, "http://")
	host, portText, _ := strings.Cut(hostPort, ":")
	port, _ := strconv.Atoi(portText)
	if err := m.EditAgents(baseCtx, "", func(_ context.Context, records *[]model.Agent) error {
		return json.Unmarshal([]byte(`[{"id":"a1","name":"A1","host":"`+host+`","port":`+strconv.Itoa(port)+`,"token":"synthetic-token","profile_id":"default"}]`), records)
	}); err != nil {
		t.Fatal(err)
	}

	reqCtx, disconnect := context.WithCancel(baseCtx)
	go func() {
		time.Sleep(25 * time.Millisecond)
		disconnect()
	}()

	raw, _, err := m.RemoteCall(reqCtx, "a1", RemoteRequest{
		Method:  http.MethodPost,
		Path:    "/api/logs/clear",
		Timeout: Seconds(2),
	})
	if err != nil {
		t.Fatalf("non-GET RemoteCall aborted on client disconnect: %v", err)
	}
	if !strings.Contains(string(raw), `"done":true`) {
		t.Fatalf("unexpected reply: %s", raw)
	}
}

// O9 round 2 L2: CallAgent sets NoRedirects so a compromised Agent returning
// 3xx cannot turn the panel into an SSRF proxy; 3xx is handled via existing
// HTTP failure error classes while 200 continues to succeed.
func TestO9R2CallAgentNoRedirects(t *testing.T) {
	var canaryHits atomic.Int32
	canary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		canaryHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"secret":"must-not-follow"}`))
	}))
	defer canary.Close()

	tests := []struct {
		name     string
		status   int
		body     string
		wantFail bool
	}{
		{"301 redirect not followed", http.StatusMovedPermanently, `redirect`, true},
		{"302 redirect not followed", http.StatusFound, `redirect`, true},
		{"302 redirect with JSON success:true still fails deployment", http.StatusFound, `{"success":true}`, true},
		{"307 redirect not followed", http.StatusTemporaryRedirect, `redirect`, true},
		{"308 redirect not followed", http.StatusPermanentRedirect, `redirect`, true},
		{"negative control: 200 OK succeeds", http.StatusOK, `{"success":true,"status":"ready"}`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			canaryHits.Store(0)
			agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.status >= 300 && tc.status < 400 {
					w.Header().Set("Location", canary.URL+"/internal")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer agentSrv.Close()

			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx := repo.WithRepository(repo.WithScope(context.Background()), r)
			m, err := New(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Close)

			hostPort := strings.TrimPrefix(agentSrv.URL, "http://")
			host, portText, _ := strings.Cut(hostPort, ":")
			port, _ := strconv.Atoi(portText)
			if err := m.EditAgents(ctx, "", func(_ context.Context, records *[]model.Agent) error {
				return json.Unmarshal([]byte(`[{"id":"a1","name":"A1","host":"`+host+`","port":`+strconv.Itoa(port)+`,"token":"synthetic-token","profile_id":"default"}]`), records)
			}); err != nil {
				t.Fatal(err)
			}
			a, err := m.ReadAgent(ctx, "a1", false)
			if err != nil || a == nil {
				t.Fatalf("read agent: %v %v", a, err)
			}

			// 1. Direct CallAgent
			resp, err := m.CallAgent(ctx, a, http.MethodGet, "/api/status", nil, nil, Seconds(3))
			if err != nil {
				t.Fatalf("CallAgent err = %v", err)
			}
			if resp.StatusCode != tc.status {
				t.Fatalf("CallAgent status = %d, want %d", resp.StatusCode, tc.status)
			}

			// 2. RemoteCall
			raw, _, err := m.RemoteCall(ctx, "a1", RemoteRequest{Method: http.MethodGet, Path: "/api/status", Timeout: Seconds(3)})
			if err != nil {
				t.Fatalf("RemoteCall err = %v", err)
			}
			var remoteObj struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(raw, &remoteObj); err != nil {
				t.Fatalf("unmarshal RemoteCall: %v", err)
			}
			if tc.wantFail {
				if remoteObj.Success || remoteObj.Message != "HTTP "+strconv.Itoa(tc.status) {
					t.Fatalf("RemoteCall = %+v, want success=false message=HTTP %d", remoteObj, tc.status)
				}
			} else if !remoteObj.Success {
				t.Fatalf("RemoteCall negative control failed: %s", raw)
			}

			// 3. DeploymentRequestModel
			dep, err := m.DeploymentRequestModel(ctx, a, http.MethodGet, "deployments/d1", nil, nil, nil)
			if err != nil {
				t.Fatalf("DeploymentRequestModel err = %v", err)
			}
			depJSON, _ := model.Marshal(dep)
			var depObj map[string]any
			_ = json.Unmarshal(depJSON, &depObj)
			if tc.wantFail {
				if depObj["success"] != false {
					t.Fatalf("DeploymentRequestModel success = %v, want false (%s)", depObj["success"], depJSON)
				}
				if got, ok := depObj["http_status"].(float64); !ok || int(got) != tc.status {
					t.Fatalf("DeploymentRequestModel http_status = %v, want %d (%s)", depObj["http_status"], tc.status, depJSON)
				}
			} else if depObj["success"] != true {
				t.Fatalf("DeploymentRequestModel negative control failed: %s", depJSON)
			}

			// 4. AgentRequestModel
			_, reqErr := m.AgentRequestModel(ctx, a, RemoteRequest{Method: http.MethodGet, Path: "/api/return-routing", Timeout: Seconds(3)}, msgReturnRoutingStatus, false)
			if tc.wantFail {
				var remErr *RemoteError
				if !errors.As(reqErr, &remErr) || !strings.Contains(remErr.Message, strconv.Itoa(tc.status)) {
					t.Fatalf("AgentRequestModel err = %v, want RemoteError with %d", reqErr, tc.status)
				}
			} else if reqErr != nil {
				t.Fatalf("AgentRequestModel negative control failed: %v", reqErr)
			}

			if hits := canaryHits.Load(); hits != 0 {
				t.Fatalf("redirect target received %d hits, want 0", hits)
			}
		})
	}
}
