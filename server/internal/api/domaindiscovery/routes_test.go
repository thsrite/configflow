package domaindiscovery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/api/domaindiscovery"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

func TestEveryRouteRequiresAdministratorAuthentication(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(domaindiscovery.Register)})
	for _, rt := range []struct{ method, path string }{{"GET", "settings"}, {"PUT", "settings"}, {"POST", "rulesets/init"}, {"GET", "domains"}, {"GET", "agents"}, {"POST", "apply"}, {"POST", "undo"}, {"GET", "history"}, {"POST", "ignore"}, {"DELETE", "ignore"}, {"POST", "probe"}, {"GET", "probe/missing"}, {"GET", "region"}, {"POST", "region/check"}, {"POST", "region/route"}, {"POST", "website/identify"}, {"PUT", "website/note"}} {
		t.Run(rt.method+"_"+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, "/api/domain-discovery/"+rt.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous status=%d", w.Code)
			}
		})
	}
}
func TestCancelledHTTPWritesFinishAndPreserveUnknownFields(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(domaindiscovery.Register), Setup: func(app *httpx.App) {
		app.BeforeRequest(func(c *httpx.Ctx) (*httpx.Response, error) {
			ctx, cancel := context.WithCancel(c.Context())
			cancel()
			c.SetContext(ctx)
			c.Request = c.Request.WithContext(ctx)
			return nil, nil
		})
	}})
	ctx := srv.Context()
	_, _, err := srv.Repo.SaveProfileFieldsJSON(ctx, "default", json.RawMessage(`{"domain_discovery":{"future_setting":{"keep":true},"history":[{"value":"old.example.net","target":"proxy","ruleset_id":"missing","future_history":true}]}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, rt := range []struct{ method, path, body string }{{"PUT", "settings", `{"auto_probe":true}`}, {"POST", "ignore", `{"values":["noise.example.net"]}`}, {"PUT", "website/note", `{"domain":"example.com","note":"kept"}`}} {
		req := httptest.NewRequest(rt.method, "/api/domain-discovery/"+rt.path, strings.NewReader(rt.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("cancelled %s: %d %s", rt.path, w.Code, w.Body)
		}
	}
	profile, err := srv.Repo.Profile(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(profile.DomainDiscovery)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err = json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	// Undeclared members are dropped (O18 T1c); the writes still finish.
	if saved["auto_probe"] != true || saved["future_setting"] != nil || saved["website_notes"].(map[string]any)["example.com"] != "kept" || saved["history"].([]any)[0].(map[string]any)["value"] != "old.example.net" {
		t.Fatalf("lost persisted fields: %s", b)
	}
}
