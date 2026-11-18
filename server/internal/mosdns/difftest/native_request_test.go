package difftest

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	apimosdns "configflow/internal/api/mosdns"
	"configflow/internal/repo"
	"configflow/internal/store"
)

func nativeRequestState(t *testing.T, dir string) any {
	t.Helper()
	repository, err := repo.Open(dir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := repository.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return struct {
		Rows  store.Fingerprint
		Files map[string]any
	}{rows, written(t, dir)}
}

// Every rejected native request runs on an independent fixture. Python's
// accepted legacy coercions cannot alter the input or revisions of later cases.
func TestMosDNSNativeRequestDifferences(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	tests := []struct {
		request          request
		python, goStatus int
	}{
		{request{name: "post rulesets unknown", method: "POST", path: "/api/mosdns/rulesets", body: `{"direct_rules": ["missing"]}`}, 500, 409},
		{request{name: "post rulesets text", method: "POST", path: "/api/mosdns/rulesets", body: `x`, contentType: "text/plain"}, 500, 400},
		{request{name: "post rulesets list", method: "POST", path: "/api/mosdns/rulesets", body: `[1]`}, 500, 400},
		{request{name: "post dns-servers broken", method: "POST", path: "/api/mosdns/dns-servers", body: `{"local_dns"`}, 500, 400},
		{request{name: "post api-settings null", method: "POST", path: "/api/mosdns/api-settings", body: `null`}, 500, 400},
		{request{name: "post cache-settings", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_size": "128", "cache_lazy_ttl": 60.9, "cache_dump_interval": -5, "cache_dump_file": null, "cache_enabled": 0}`}, 200, 400},
		{request{name: "post cache-settings bad ints", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_size": "x", "cache_lazy_ttl": [1], "cache_dump_interval": true, "cache_dump_file": "/tmp/c.dump"}`}, 200, 400},
		{request{name: "post cache-settings big ints", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_size": 100000000000000000000, "cache_lazy_ttl": 1e30, "cache_dump_interval": " 1_000 ", "cache_dump_file": "x.dump"}`}, 200, 400},
		{request{name: "post cache-settings list", method: "POST", path: "/api/mosdns/cache-settings", body: `[1]`}, 500, 400},
		{request{name: "post cache-settings text", method: "POST", path: "/api/mosdns/cache-settings", body: `x`, contentType: "text/plain"}, 500, 400},

		{request{name: "null editor cache boolean", method: "POST", path: "/api/mosdns/settings", body: `{"cache_enabled":null}`}, 200, 400},
		{request{name: "null cache boolean", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_enabled":null}`}, 200, 400},
		{request{name: "null dump boolean", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_dump_enabled":null}`}, 200, 400},
		{request{name: "fractional integer", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_lazy_ttl":60.9}`}, 200, 400},
		{request{name: "boolean integer", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_size":true}`}, 200, 400},
		{request{name: "numeric boolean", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_enabled":0}`}, 200, 400},
		{request{name: "underscored integer", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_size":" 1_000 "}`}, 200, 400},
		{request{name: "positive sign integer", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_size":"+60"}`}, 200, 400},
		{request{name: "fractional integer text", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_size":"60.9"}`}, 200, 400},
		{request{name: "overflow integer", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_size":100000000000000000000}`}, 200, 400},
	}
	for _, tt := range tests {
		t.Run(tt.request.name, func(t *testing.T) {
			up := upstream(t)
			base := t.TempDir()
			pyDir, goDir := filepath.Join(base, "python"), filepath.Join(base, "go")
			seed(t, pyDir, up.URL)
			seed(t, goDir, up.URL)
			pyURL, goURL := startPython(t, pyDir), startGo(t, goDir)
			pyToken, goToken := login(t, pyURL), login(t, goURL)
			beforePy, beforeGo := nativeRequestState(t, pyDir), nativeRequestState(t, goDir)
			p, g := send(t, pyURL, pyToken, tt.request), send(t, goURL, goToken, tt.request)
			if p.Status != tt.python || g.Status != tt.goStatus {
				t.Fatalf("Python=%+v Go=%+v", p, g)
			}
			if !reflect.DeepEqual(beforeGo, nativeRequestState(t, goDir)) {
				t.Fatal("rejected native request wrote DB rows/revisions or business files")
			}
			pyChanged := !reflect.DeepEqual(beforePy, nativeRequestState(t, pyDir))
			if pyChanged != (tt.python == 200) {
				t.Fatalf("Python write=%v status=%d", pyChanged, p.Status)
			}
			body, ok := g.JSON.(map[string]any)
			if !ok || body["success"] != false {
				t.Fatalf("native error body: %+v", g)
			}
			expectedMessage := apimosdns.SettingsObjectMessage
			if tt.python == 200 {
				expectedMessage = apimosdns.SettingsFieldTypeMessage
			}
			if tt.goStatus == 400 && body["message"] != expectedMessage {
				t.Fatalf("native error message: %+v", g)
			}
			if tt.goStatus == 409 {
				pyBody, ok := p.JSON.(map[string]any)
				if !ok || body["message"] != pyBody["message"] {
					t.Fatal("missing reference explanation changed")
				}
				usages, ok := body["usages"].([]any)
				if !ok || len(usages) != 1 || usages[0].(map[string]any)["profile_id"] != "default" {
					t.Fatal("missing reference usages lost")
				}
			}
			if tt.request.name == "post cache-settings big ints" {
				query := request{name: "get cache-settings big ints", method: "GET", path: "/api/mosdns/cache-settings"}
				pget, gget := send(t, pyURL, pyToken, query), send(t, goURL, goToken, query)
				pyExpected := map[string]any{"cache_enabled": true, "cache_dump_enabled": true, "cache_size": float64(1e20), "cache_lazy_ttl": float64(1e30), "cache_dump_file": "x.dump", "cache_dump_interval": float64(1000)}
				goExpected := map[string]any{"cache_enabled": true, "cache_dump_enabled": true, "cache_size": float64(10240), "cache_lazy_ttl": float64(21600), "cache_dump_file": "./cache.dump", "cache_dump_interval": float64(300)}
				if pget.Status != 200 || gget.Status != 200 || !reflect.DeepEqual(pget.JSON, pyExpected) || !reflect.DeepEqual(gget.JSON, goExpected) {
					t.Fatalf("big-integer GET changed: Python=%+v Go=%+v", pget, gget)
				}
				if !reflect.DeepEqual(beforeGo, nativeRequestState(t, goDir)) {
					t.Fatal("GET changed the rejected request snapshot")
				}
			}
			t.Logf("Python %d / Go %d; Go rows/revisions/files unchanged", p.Status, g.Status)
		})
	}
}
