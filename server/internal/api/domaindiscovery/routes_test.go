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
	"configflow/internal/jsonx"
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
	_, _, err := srv.Repo.UpdateProfileFields(ctx, "default", jsonx.Obj("domain_discovery", jsonx.Obj("future_setting", jsonx.Obj("keep", true), "history", []any{jsonx.Obj("value", "old.example.net", "target", "proxy", "ruleset_id", "missing", "future_history", true)})), nil)
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
	profile, err := srv.Repo.GetProfile(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	b, err := jsonx.Compact(profile.GetObject("domain_discovery"))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err = json.Unmarshal([]byte(b), &saved); err != nil {
		t.Fatal(err)
	}
	if saved["auto_probe"] != true || saved["future_setting"].(map[string]any)["keep"] != true || saved["website_notes"].(map[string]any)["example.com"] != "kept" || saved["history"].([]any)[0].(map[string]any)["future_history"] != true {
		t.Fatalf("lost persisted fields: %s", b)
	}
}
