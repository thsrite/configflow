package agents

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	core "configflow/internal/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func TestN810MalformedLoggingReplyIsStructured502WithoutWrite(t *testing.T) {
	for _, body := range []string{`[1]`, `null`, `not JSON`, `{"success":1}`, `{"success":true,"success":false}`, `{"success":true,"extra":{"x":1,"x":2}}`} {
		t.Run(body, func(t *testing.T) {
			s, admin, _ := newDeployHTTP(t)
			type observed struct {
				method, path, authorization, contentType string
				body                                     []byte
			}
			seen := make(chan observed, 1)
			device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				seen <- observed{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), data}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, body)
			}))
			defer device.Close()
			parsed, _ := url.Parse(device.URL)
			host, port, _ := net.SplitHostPort(parsed.Host)
			number, err := strconv.Atoi(port)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
				system.Agents[0].Host = host
				system.Agents[0].Port = model.Int(number)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			manager, err := core.Get(s.Context())
			if err != nil {
				t.Fatal(err)
			}
			attempts := make(chan n810HTTPAttempt, 1)
			manager.SetClient(n810ObservedHTTPClient{attempts})
			before, err := s.Repo.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			response := s.Post("/api/agents/publish-agent/config/logging", admin, httpxtest.JSON(jsonx.Obj("enabled", true)))
			response.WantStatus(http.StatusBadGateway).WantJSON(jsonx.Obj("success", false, "code", "INVALID_RESPONSE", "message", core.ErrInvalidAgentReply.Message))
			if strings.Contains(response.Text(), "synthetic-publish-token") {
				t.Fatal("credential leaked")
			}
			if after, err := s.Repo.Store().Fingerprint(); err != nil || after != before {
				t.Fatal("malformed logging reply committed", err)
			}
			attempt := <-attempts
			if attempt.err != nil {
				t.Fatal("device transport failed", attempt.err)
			}
			request := <-seen
			if request.method != http.MethodPost || request.path != "/api/config/logging" || request.authorization != "Bearer synthetic-publish-token" || request.contentType != "application/json" || !bytes.Equal(request.body, []byte(`{"enabled": true}`)) {
				t.Fatalf("raw device request changed: %+v", request)
			}
		})
	}
}

func TestN810MalformedRestartReplyIsStructured502WithoutWrite(t *testing.T) {
	for _, body := range []string{`[1]`, `upstream unavailable password=do-not-display`} {
		t.Run(body, func(t *testing.T) {
			s, admin, _ := newDeployHTTP(t)
			seen := make(chan []byte, 1)
			device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.RequestURI() != "/api/restart" || r.Header.Get("Authorization") != "Bearer synthetic-publish-token" || r.Header.Get("Content-Type") != "" {
					t.Error("raw restart request changed")
				}
				seen <- raw
				_, _ = io.WriteString(w, body)
			}))
			defer device.Close()
			parsed, _ := url.Parse(device.URL)
			host, port, _ := net.SplitHostPort(parsed.Host)
			number, _ := strconv.Atoi(port)
			if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
				system.Agents[0].Host = host
				system.Agents[0].Port = model.Int(number)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			manager, err := core.Get(s.Context())
			if err != nil {
				t.Fatal(err)
			}
			attempts := make(chan n810HTTPAttempt, 1)
			manager.SetClient(n810ObservedHTTPClient{attempts})
			before, err := s.Repo.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			response := s.Post("/api/agents/publish-agent/restart", admin)
			response.WantStatus(http.StatusBadGateway).WantJSON(jsonx.Obj("success", false, "code", "INVALID_RESPONSE", "message", core.ErrInvalidAgentReply.Message))
			if strings.Contains(response.Text(), "do-not-display") {
				t.Fatal("reply credential leaked")
			}
			if after, err := s.Repo.Store().Fingerprint(); err != nil || after != before {
				t.Fatal("malformed restart reply wrote transaction", err)
			}
			if attempt := <-attempts; attempt.err != nil {
				t.Fatal(attempt.err)
			}
			if raw := <-seen; len(raw) != 0 {
				t.Fatalf("restart body changed: %q", raw)
			}
		})
	}
}
