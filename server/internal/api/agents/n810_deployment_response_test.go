package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	core "configflow/internal/agents"
	"configflow/internal/model"
)

type n810HTTPAttempt struct {
	request core.Request
	err     error
}
type n810ObservedHTTPClient struct{ attempts chan n810HTTPAttempt }

func (c n810ObservedHTTPClient) Do(ctx context.Context, request core.Request) (*core.Response, error) {
	response, err := (core.HTTPClient{}).Do(ctx, request)
	c.attempts <- n810HTTPAttempt{request, err}
	return response, err
}

func TestN810MalformedDeploymentReplyIsStructured502WithoutWrite(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		failure    *core.DeviceResponseError
	}{
		{"status array", `{"success":true,"status":[],"message":"synthetic-publish-token"}`, core.ErrInvalidDeploymentStatus},
		{"status object", `{"success":true,"status":{"phase":"ready"},"message":"synthetic-publish-token"}`, core.ErrInvalidDeploymentStatus},
		{"duplicate status", `{"success":true,"status":"checking","status":"succeeded","message":"synthetic-publish-token"}`, core.ErrInvalidAgentReply},
		{"duplicate nested", `{"success":true,"status":"checking","future":{"x":1,"x":2}}`, core.ErrInvalidAgentReply},
		{"not JSON", `not JSON`, core.ErrInvalidAgentReply},
		{"null", `null`, core.ErrInvalidAgentReply},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, admin, _ := newDeployHTTP(t)
			seen := make(chan struct {
				method, path, authorization string
				body                        []byte
			}, 1)
			device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				seen <- struct {
					method, path, authorization string
					body                        []byte
				}{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), body}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.body)
			}))
			defer device.Close()
			u, _ := url.Parse(device.URL)
			host, port, _ := net.SplitHostPort(u.Host)
			n, err := strconv.Atoi(port)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
				system.Agents[0].Host = host
				system.Agents[0].Port = model.Int(n)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			m, err := core.Get(s.Context())
			if err != nil {
				t.Fatal(err)
			}
			attempts := make(chan n810HTTPAttempt, 2)
			m.SetClient(n810ObservedHTTPClient{attempts})
			const id = "malformed-status"
			if _, err := m.BeginDeployment(s.Context(), "publish-agent", id); err != nil {
				t.Fatal(err)
			}
			defer m.FinishDeploymentPreparation("publish-agent", id)
			patch, err := core.DecodeDeploymentUpdate(json.RawMessage(`{"status":"uploading","submitted":true}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.RecordDeploymentModel(s.Context(), "publish-agent", id, patch, core.RecordOptions{}); err != nil {
				t.Fatal(err)
			}
			before, err := s.Repo.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			result := s.Get("/api/agents/publish-agent/deployments/"+id, admin)
			select {
			case attempt := <-attempts:
				if attempt.err != nil {
					t.Fatalf("device transport failed: URL=%s err=%v", attempt.request.URL, attempt.err)
				}
			default:
				t.Fatal("API used a different device client")
			}
			result.WantStatus(http.StatusBadGateway)
			wantBodyJSON(t, result, map[string]any{"success": false, "code": "INVALID_RESPONSE", "message": tt.failure.Message})
			if strings.Contains(result.Text(), "synthetic-publish-token") {
				t.Fatal("device diagnostic leaked credential")
			}
			if after, err := s.Repo.Store().Fingerprint(); err != nil || after != before {
				t.Fatal("malformed upstream reply committed", err)
			}
			request := <-seen
			if request.method != http.MethodGet || request.path != "/api/deployments/"+id || request.authorization != "Bearer synthetic-publish-token" || !bytes.Equal(request.body, []byte{}) {
				t.Fatalf("device request changed: %+v", request)
			}
		})
	}
}
