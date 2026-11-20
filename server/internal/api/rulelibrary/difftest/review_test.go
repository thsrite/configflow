package difftest

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestReviewDraftRevisionShape(t *testing.T) {
	for _, test := range []struct {
		value any
		valid bool
	}{
		{strings.Repeat("a", 64), true}, {strings.Repeat("0", 64), true},
		{strings.Repeat("A", 64), false}, {strings.Repeat("a", 63), false},
		{strings.Repeat("a", 65), false}, {strings.Repeat("a", 64) + "\n", false},
		{42, false}, {nil, false},
	} {
		if validDraftRevision(test.value) != test.valid {
			t.Errorf("revision shape %v: want %t", test.value, test.valid)
		}
	}
}

func pythonOnly(w *world, method, path, requestBody string) answer {
	w.t.Helper()
	request := map[string]any{"method": method, "path": path, "body": requestBody, "headers": map[string]string{"Authorization": "Bearer " + w.token}}
	raw, err := json.Marshal(request)
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err = w.stdin.Write(append(raw, '\n')); err != nil {
		w.t.Fatal(err)
	}
	if !w.scanner.Scan() {
		w.t.Fatal(w.stderr.String())
	}
	var py answer
	if err = json.Unmarshal(w.scanner.Bytes(), &py); err != nil {
		w.t.Fatal(err)
	}
	if py.Error != "" {
		w.t.Fatal(py.Error)
	}
	return py
}

func TestReviewPreviewSelectionShapes(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, source := range []struct{ kind, key, id string }{{"subscription", "subscriptions", "feed"}, {"aggregation", "aggregations", "agg"}} {
		for _, selection := range []string{`null`, `false`, `0`, `0.0`, `""`, `{}`, `[]`, `[null,42,{},[],"` + source.id + `"]`} {
			request := `{"source":"` + source.kind + `","regex":".*","` + source.key + `":` + selection + `}`
			w.call("POST", "/api/proxy-groups/preview-regex", request, true)
		}
		for _, selection := range []string{`"` + source.id + `"`, `{"` + source.id + `":true}`} {
			request := `{"source":"` + source.kind + `","regex":".*","` + source.key + `":` + selection + `}`
			before := snapshot(t, w.srv)
			// Python accidentally treats strings and object keys as ID iterables.
			// Keep this validation correction explicit, like non-array rule_ids.
			py := pythonOnly(w, "POST", "/api/proxy-groups/preview-regex", request)
			if py.Status != 200 || !reflect.DeepEqual(w.normalize(py.State), w.normalize(jsonValue(t, before))) {
				t.Fatal("Python non-array selection control failed", py.Status)
			}
			w.srv.Post("/api/proxy-groups/preview-regex", httpxtest.Bearer(w.token), httpxtest.Body([]byte(request), "application/json")).WantStatus(400)
			if !reflect.DeepEqual(before, snapshot(t, w.srv)) {
				t.Fatal("invalid preview selection wrote state")
			}
		}
	}
}

func TestReviewReorderMalformedHTTPParity(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, path := range []string{"/api/rule-library/reorder", "/api/proxy-groups/reorder"} {
		for _, body := range []string{`null`, `false`, `0`, `""`, `[]`, `[1]`, `"bad"`, `42`, `true`} {
			w.call("POST", path, body, true)
		}
	}
}

func TestReviewStrictAndSilentBinding(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/rule-library"}, {"PUT", "/api/rule-library/missing"},
		{"PUT", "/api/rule-library/missing/draft-content"}, {"POST", "/api/rule-library/check-content"},
		{"POST", "/api/rule-library/cache"}, {"POST", "/api/rule-library/test-single"},
		{"POST", "/api/rule-library/reorder"}, {"POST", "/api/rule-library/proxy-domains"},
		{"POST", "/api/proxy-groups"}, {"POST", "/api/profiles/other/proxy-groups"},
		{"PUT", "/api/proxy-groups/group"}, {"PUT", "/api/proxy-groups/batch-health-url"},
		{"POST", "/api/proxy-groups/preview-regex"}, {"POST", "/api/proxy-groups/reorder"},
	} {
		for _, contentType := range []string{"application/json", "text/plain", "application/problem+json"} {
			for _, body := range []string{`{`, `{}`} {
				w.callHeaders(route.method, route.path, body, body, map[string]string{"Authorization": "Bearer " + w.token, "Content-Type": contentType})
			}
		}
	}
}

func TestReviewNonFiniteBindingUsesMalformedBoundary(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/rule-library"}, {"PUT", "/api/rule-library/missing"},
		{"PUT", "/api/rule-library/missing/draft-content"}, {"POST", "/api/rule-library/check-content"},
		{"POST", "/api/rule-library/cache"}, {"POST", "/api/rule-library/test-single"},
		{"POST", "/api/rule-library/reorder"}, {"POST", "/api/rule-library/proxy-domains"},
		{"POST", "/api/proxy-groups"}, {"POST", "/api/profiles/other/proxy-groups"},
		{"PUT", "/api/proxy-groups/group"}, {"PUT", "/api/proxy-groups/batch-health-url"},
		{"POST", "/api/proxy-groups/preview-regex"}, {"POST", "/api/proxy-groups/reorder"},
	} {
		for _, literal := range []string{"NaN", "Infinity", "-Infinity"} {
			// N8 native JSON rejects these Python-only literals. Real Flask's
			// malformed request supplies the complete expected error for each
			// strict/silent/caught boundary, without normalizing its response.
			w.callWithBody(route.method, route.path, `{`, `{"unused":`+literal+`}`, true)
		}
	}
}

func TestReviewPythonNonFiniteAcceptanceIsExplicit(t *testing.T) {
	w := newWorld(t, seed(t))
	before := snapshot(t, w.srv)
	requestBody := `{"content":"+.example.com\n","behavior":"domain","format":"text","unused":NaN}`
	request := map[string]any{"method": "POST", "path": "/api/rule-library/check-content", "body": requestBody, "headers": map[string]string{"Authorization": "Bearer " + w.token}}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.stdin.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	if !w.scanner.Scan() {
		t.Fatal(w.stderr.String())
	}
	var py answer
	if err = json.Unmarshal(w.scanner.Bytes(), &py); err != nil {
		t.Fatal(err)
	}
	if py.Error != "" || py.Status != 200 || !reflect.DeepEqual(w.normalize(py.State), w.normalize(jsonValue(t, before))) {
		t.Fatal("Python non-finite acceptance/state control failed", py.Status, py.Error)
	}
	w.srv.Post("/api/rule-library/check-content", httpxtest.Bearer(w.token), httpxtest.Body([]byte(requestBody), "application/json")).WantStatus(400)
	if !reflect.DeepEqual(before, snapshot(t, w.srv)) {
		t.Fatal("non-finite rejection wrote Go state")
	}
}

func TestReviewAuthenticationAllowlist(t *testing.T) {
	w := newWorld(t, func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		err := r.UpdateSystem(ctx, func(_ context.Context, p *model.System) error {
			p.SystemConfig.ConfigToken = "synthetic-config-token"
			p.SystemConfig.RuleProxyToken = "synthetic-rule-proxy-token"
			p.SystemConfig.RetiredRuleProxyTokens = []string{"synthetic-retired-token"}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	w.call("POST", "/api/rule-library", `{"id":"manual","name":"Manual","source_type":"content","behavior":"domain","format":"text","content":"+.manual.example\n"}`, true)
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/proxy-groups"}, {"POST", "/api/proxy-groups"},
		{"GET", "/api/profiles/other/proxy-groups"}, {"POST", "/api/profiles/other/proxy-groups"},
		{"PUT", "/api/proxy-groups/group"}, {"DELETE", "/api/proxy-groups/group"},
		{"PUT", "/api/proxy-groups/batch-health-url"}, {"GET", "/api/proxy-groups/health-defaults"},
		{"POST", "/api/proxy-groups/preview-regex"}, {"POST", "/api/proxy-groups/reorder"},
		{"GET", "/api/rule-library"}, {"POST", "/api/rule-library"},
		{"PUT", "/api/rule-library/manual"}, {"DELETE", "/api/rule-library/manual"},
		{"GET", "/api/rule-library/manual/draft-content"}, {"PUT", "/api/rule-library/manual/draft-content"},
		{"POST", "/api/rule-library/cache"}, {"POST", "/api/rule-library/check-content"},
		{"GET", "/api/rule-library/proxy-domains"}, {"POST", "/api/rule-library/proxy-domains"},
		{"POST", "/api/rule-library/reorder"}, {"POST", "/api/rule-library/test"}, {"POST", "/api/rule-library/test-single"},
	} {
		for _, token := range []string{"", "synthetic-config-token", "synthetic-rule-proxy-token", "synthetic-retired-token", "wrong"} {
			path := route.path + "?token=" + token
			// A capability in either channel must not grant management rights.
			headers := map[string]string{}
			if token != "" {
				headers["Authorization"] = "Bearer " + token
			}
			if reply := w.callHeaders(route.method, path, `{}`, `{}`, headers); reply.Status != 401 {
				t.Fatalf("management %s %s: %d", route.method, route.path, reply.Status)
			}
		}
	}
	for _, path := range []string{"/api/rule-library/content/manual", "/api/profiles/other/rule-library/content/manual"} {
		for _, token := range []string{"", "synthetic-config-token", "synthetic-rule-proxy-token", "synthetic-retired-token", "wrong"} {
			for _, headers := range []map[string]string{{}, {"Authorization": "Bearer " + token}} {
				if reply := w.callHeaders("GET", path+"?token="+token, "", "", headers); reply.Status != 200 {
					t.Fatalf("public content %s: %d", path, reply.Status)
				}
			}
		}
	}
}
