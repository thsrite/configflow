package oxidns_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	api "configflow/internal/api/oxidns"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/server"
)

func TestC29RouteRegistrationHasOneOwner(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{RepoOptions: server.RepoOptions()})
	full, err := server.New(srv.Context(), server.Options{Repo: srv.Repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	app := full.HTTP
	seen := map[string]string{}
	count := 0
	for _, route := range app.Router.Routes() {
		for _, method := range route.Methods {
			key := method + " " + route.Path
			if previous, ok := seen[key]; ok {
				t.Fatalf("route conflict %s: %s and %s", key, previous, route.Endpoint)
			}
			seen[key] = route.Endpoint
			if strings.HasPrefix(route.Path, "/api/oxidns/") || strings.HasPrefix(route.Path, "/api/profiles/<profile_id>/oxidns/") {
				count++
			}
		}
	}
	if count != 18 {
		t.Fatalf("C29 methods=%d, want 18", count)
	}
	// Exercise actual dispatch in the complete app: static routes must not be
	// captured by an older generic profile rule (HANDOFF 3c).
	for _, prefix := range []string{"/api/oxidns", "/api/profiles/default/oxidns"} {
		for _, suffix := range []string{"settings", "render", "validate", "editor", "template-preview", "common-settings", "common-settings/apply", "native-settings/apply"} {
			req := httptest.NewRequest("POST", prefix+"/"+suffix, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req.WithContext(req.Context()))
			want := 400
			if suffix == "settings" {
				want = 200
			}
			if rec.Code != want {
				t.Fatalf("route shadowed: %s %d", req.URL, rec.Code)
			}
		}
	}
}

func TestC29HEADReturnsGETHeadersWithoutSaving(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{RepoOptions: server.RepoOptions()})
	api.New(api.Deps{Repo: srv.Repo}).Register(srv.App.Router)
	for _, path := range []string{"/api/oxidns/settings", "/api/profiles/default/oxidns/settings"} {
		get := httptest.NewRecorder()
		srv.Handler().ServeHTTP(get, httptest.NewRequest("GET", path, nil))
		before, err := srv.Repo.ExportAllJSON(srv.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		revision, err := srv.Repo.ConfigRevision(srv.Context(), "default")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("HEAD", path, strings.NewReader(`{"worker_threads": 9}`))
		req.Header.Set("Content-Type", "application/json")
		head := httptest.NewRecorder()
		srv.Handler().ServeHTTP(head, req)
		if head.Code != get.Code || head.Body.Len() != 0 {
			t.Fatalf("HEAD status/body: %d %q", head.Code, head.Body.String())
		}
		for key, want := range get.Header() {
			if strings.Join(head.Header().Values(key), "\n") != strings.Join(want, "\n") {
				t.Fatalf("HEAD header %s: %v != %v", key, head.Header().Values(key), want)
			}
		}
		after, err := srv.Repo.ExportAllJSON(srv.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		now, err := srv.Repo.ConfigRevision(srv.Context(), "default")
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) || now != revision {
			t.Fatal("JSON HEAD changed DB/revision")
		}
	}
}

func TestNativeRequestTypesReturn400WithoutWrites(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{RepoOptions: server.RepoOptions()})
	api.New(api.Deps{Repo: srv.Repo}).Register(srv.App.Router)
	for _, body := range []string{`{"custom_matches":[{"enabled":0}]}`, `{"ros_route":{"args":{"routing_table":5}}}`, `{"ros_route":{"args":{"gateway4":[]}}}`} {
		before, err := srv.Repo.ExportAllJSON(srv.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/api/oxidns/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		srv.Handler().ServeHTTP(response, req)
		if response.Code != 400 {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		after, err := srv.Repo.ExportAllJSON(srv.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("rejected request wrote database")
		}
	}
}
