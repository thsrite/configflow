package difftest

import (
	"net/http"
	"testing"

	"configflow/internal/yamlx"
)

// TestReviewC29ExactRequestKeys checks that unrelated JSON members cannot
// replace the explicitly named editor inputs, including optional edit intents.
func TestReviewC29ExactRequestKeys(t *testing.T) {
	w := newWorld(t)
	for _, prefix := range []string{"/api/oxidns", "/api/profiles/office/oxidns"} {
		for _, action := range []string{"render", "validate", "editor", "template-preview", "common-settings", "common-settings/apply", "native-settings/apply"} {
			t.Run(prefix+"/"+action, func(t *testing.T) {
				w.t = t
				settings := draft("runtime: {worker_threads: 4}\nplugins: []\n")
				w.check(post(prefix+"/"+action, map[string]any{"Settings": settings, "native_config": "plugins: []"}))
				w.check(post(prefix+"/"+action, map[string]any{
					"settings": settings, "native_config": "plugins: []",
					"BASE_URL": false, "CHANGES": false, "TEMPLATE_SETTINGS": false,
					"RESOLUTIONS": false, "RENAMES": false, "STANDARD_ADD": false,
				}))
				for _, field := range []struct {
					name  string
					value any
				}{
					{"BASE_URL", false}, {"CHANGES", map[string]any{"worker_threads": 6}},
					{"TEMPLATE_SETTINGS", false}, {"RESOLUTIONS", false},
					{"RENAMES", false}, {"STANDARD_ADD", false},
				} {
					value := map[string]any{"settings": settings, "native_config": "plugins: []"}
					value[field.name] = field.value
					w.check(post(prefix+"/"+action, value))
				}
				if action == "native-settings/apply" {
					w.check(post(prefix+"/"+action, map[string]any{"settings": settings, "NATIVE_CONFIG": "plugins: []"}))
				}
			})
		}
	}
}

// TestReviewC29NativeReferenceTargets checks candidate form locations when a
// batch adds unrelated features together with an updater that has no source.
func TestReviewC29NativeReferenceTargets(t *testing.T) {
	w := newWorld(t)
	for _, prefix := range []string{"/api/oxidns", "/api/profiles/office/oxidns"} {
		w.check(post(prefix+"/native-settings/apply", map[string]any{
			"settings":      draft("plugins: []"),
			"native_config": "plugins:\n- {tag: lazy_cache, type: cache, args: {size: 1024}}\n- {tag: rules_download, type: download, args: {downloads: []}}\n",
		}))
	}
}

// TestReviewC29ReaderPolicyHTTP keeps the already registered D1 refusal narrow:
// real Python accepts these duplicates; Go returns the shared line diagnostic
// and neither side may persist an editor draft.
func TestReviewC29ReaderPolicyHTTP(t *testing.T) {
	w := newWorld(t)
	for _, prefix := range []string{"/api/oxidns", "/api/profiles/office/oxidns"} {
		for _, field := range []string{"custom_config", "local_dns", "remote_dns", "fallback_dns"} {
			t.Run(prefix+"/"+field, func(t *testing.T) {
				source := "- {addr: udp://192.0.2.53, addr: udp://192.0.2.54}"
				if field == "custom_config" {
					source = "future: first\nfuture: second\nplugins: []"
				}
				_, cause := yamlx.Parse([]byte(source))
				if cause == nil {
					t.Fatal("missing D1 control")
				}
				r := post(prefix+"/render", map[string]any{"settings": map[string]any{
					"local_dns": "192.0.2.53", "remote_dns": "192.0.2.54", field: source,
				}})
				r.Headers = map[string]string{"Authorization": w.token, "Content-Type": "application/json"}
				before := snapshot(t, w.srv)
				py := w.py.send(t, r)
				got := sendGo(t, w.srv, r)
				if py.Status != http.StatusOK {
					t.Fatalf("Python duplicate control: %#v", py)
				}
				compare(t, "D1 status", got.Status, http.StatusBadRequest)
				compare(t, "D1 envelope and line diagnostic", got.Body, map[string]any{"success": false, "message": cause.Error()})
				compare(t, "Go D1 storage", got.State, before)
				compare(t, "Python draft storage", py.State, before)
			})
		}
	}
}

// TestReviewC29AdminBoundary checks all 18 method/path pairs: configuration and
// rule-proxy capabilities never authorize editor reads, rendering or saving.
func TestReviewC29AdminBoundary(t *testing.T) {
	w := newWorld(t)
	for _, prefix := range []string{"/api/oxidns", "/api/profiles/office/oxidns"} {
		for _, action := range []string{"get-settings", "settings", "render", "validate", "editor", "template-preview", "common-settings", "common-settings/apply", "native-settings/apply"} {
			for _, token := range []string{"c29-config-secret", "c29-rule-secret"} {
				path := prefix + "/" + action
				r := post(path, map[string]any{"settings": draft("plugins: []"), "native_config": "plugins: []"})
				if action == "get-settings" {
					r = request{Method: "GET", Path: prefix + "/settings"}
				}
				r.Path += "?token=" + token
				r.Headers = map[string]string{"Authorization": "Bearer " + token}
				if got := w.check(r); got.Status != http.StatusUnauthorized {
					t.Fatalf("capability authorized %s %s: %d", r.Method, r.Path, got.Status)
				}
			}
		}
	}
	// O9-L6: Python resolves this missing profile first; Go authenticates first.
	w.check(request{Method: "GET", Path: "/api/profiles/missing/oxidns/settings", Headers: map[string]string{"Authorization": ""}})
}
