package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// TestW10bHandlersUseInjectedDependencies verifies that W10b handlers wired via
// server.New resolve their repository and service dependencies from the Handler
// fields rather than from request-context or process-global locators.
func TestW10bHandlersUseInjectedDependencies(t *testing.T) {
	t.Cleanup(convert.ResetMihomoEnvironmentForTest(t))
	t.Cleanup(convert.ResetMihomoIntegrationsForTest(t))

	// Open the App's own repository with distinct data so we can prove requests
	// hit the injected repo rather than the process default repo.
	r, err := repo.Open(t.TempDir(), RepoOptions())
	if err != nil {
		t.Fatalf("repo.Open: %v", err)
	}
	t.Cleanup(r.Close)
	if _, err := r.CreateProfileRecord(t.Context(), repo.ProfileCreation{ID: "w10b", Name: "W10b"}, ""); err != nil {
		t.Fatalf("CreateProfileRecord: %v", err)
	}
	if err := r.UpdateShared(t.Context(), func(_ context.Context, s *model.Shared) error {
		s.RuleLibrary = append(s.RuleLibrary, model.RuleLibraryItem{
			ID:         "lib-w10b",
			Name:       "W10b Library",
			SourceType: new("content"),
			Content:    new("DOMAIN,example.invalid"),
		})
		return nil
	}); err != nil {
		t.Fatalf("UpdateShared: %v", err)
	}
	if _, err := r.UpdateProfile(t.Context(), "default", func(_ context.Context, p *model.Profile) error {
		p.MosDNS.DirectRules = []string{"rule-w10b"}
		p.RuleConfigs = append(p.RuleConfigs, model.RuleConfig{
			ID:       "rule-w10b",
			ItemType: "rule",
			RuleType: "DOMAIN",
			Value:    new("w10b.example.invalid"),
			Policy:   new("DIRECT"),
		})
		return nil
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	app, err := New(t.Context(), Options{Repo: r, Runtime: rt})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	t.Cleanup(func() {
		if app.Agents != nil {
			app.Agents.Close()
		}
	})

	// Point process-global locators elsewhere so any handler falling back to
	// repo.GetRepository / agents.Get would read the empty store or fail.
	other, err := repo.Open(t.TempDir(), RepoOptions())
	if err != nil {
		t.Fatalf("repo.Open(other): %v", err)
	}
	t.Cleanup(other.Close)
	repo.SetRepository(other)
	t.Cleanup(func() { repo.SetRepository(nil) })

	auth.Configure(auth.Config{})

	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		status   int
		contains string
	}{
		{"routers", http.MethodGet, "/api/routers", "", http.StatusOK, `"routers"`},
		{"rules", http.MethodGet, "/api/rules", "", http.StatusOK, `"rule-w10b"`},
		{"rulelibrary", http.MethodGet, "/api/rule-library", "", http.StatusOK, `"lib-w10b"`},
		{"config_export", http.MethodGet, "/api/config/export", "", http.StatusOK, `"w10b"`},
		{"profiles", http.MethodGet, "/api/profiles", "", http.StatusOK, `"w10b"`},
		{"generate_artifact_missing", http.MethodGet, "/api/generate/artifacts/nonexistent/download", "", http.StatusConflict, `"preview_changed"`},
		{"mosdns_rulesets", http.MethodGet, "/api/mosdns/rulesets", "", http.StatusOK, `"rule-w10b"`},
		{"oxidns_validate", http.MethodPost, "/api/oxidns/validate", `{"settings":{}}`, http.StatusOK, `"valid"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body *bytes.Reader
			if tc.body != "" {
				body = bytes.NewReader([]byte(tc.body))
			} else {
				body = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(tc.method, tc.path, body).WithContext(context.Background())
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			app.HTTP.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("%s %s: status = %d, want %d; body=%s", tc.method, tc.path, rec.Code, tc.status, rec.Body.String())
			}
			if tc.contains != "" && !strings.Contains(rec.Body.String(), tc.contains) {
				t.Fatalf("%s %s: body = %s, want substring %s", tc.method, tc.path, rec.Body.String(), tc.contains)
			}
		})
	}
}
