package difftest

import (
	"bytes"
	"configflow/internal/model/modeltest"
	"encoding/json"
	"testing"

	"configflow/internal/httpx"
)

func TestN811W5MetadataValidation(t *testing.T) {
	w := newWorld(t)
	for _, payload := range []any{map[string]any{"name": 17}, map[string]any{"description": false}, map[string]any{"extra": true, "name": 17}, map[string]any{"name": " "}} {
		w.check(request{Method: "PUT", Path: "/api/profiles/office", Body: body(payload)})
	}
	w.check(post("/api/profiles", map[string]any{"id": "bad-name", "name": 17}))
	w.check(post("/api/profiles/office/import", []any{1}))
	w.check(post("/api/profiles/office/agents/rebind", map[string]any{"target_profile_id": "default", "agent_ids": "not-an-array"}))
	w.check(post("/api/profiles/import-client", map[string]any{"client_type": "loon", "content": "[Rule]\nFINAL,DIRECT\n", "name": "Import", "default_subscription_ids": "wrong"}))
}

func TestN811W5DescriptionBoundary(t *testing.T) {
	for _, path := range []string{"/api/profiles", "/api/profiles/office/clone", "/api/profiles/import-client"} {
		t.Run(path, func(t *testing.T) {
			w := newWorld(t)
			payload := map[string]any{"id": "description-boundary", "name": "Description", "description": true}
			if path == "/api/profiles/import-client" {
				payload["client_type"] = "loon"
				payload["content"] = "[Rule]\nFINAL,DIRECT\n"
			}
			r := post(path, payload)
			r.Headers = map[string]string{"Authorization": w.token, "Content-Type": "application/json"}
			before := snapshot(t, w.srv)
			py := w.py.send(t, r)
			got := sendGo(t, w.srv, r)
			compare(t, "Python description coercion status", py.Status, 201)
			compare(t, "Go typed description status", got.Status, 400)
			compare(t, "Go typed description envelope", got.Body, map[string]any{"success": false, "message": "配置名称和说明必须是字符串，名称不能为空"})
			compare(t, "Go rejected description storage", got.State, before)
			profile := py.Body.(map[string]any)
			if path == "/api/profiles/import-client" {
				profile = profile["profile"].(map[string]any)
			}
			compare(t, "Python coerced description", profile["description"], "True")
		})
	}
}

func TestN811W5ProfileJSONBoundary(t *testing.T) {
	for _, route := range []struct {
		method, path         string
		malformed, nonfinite int
	}{
		{"POST", "/api/profiles", 201, 500}, {"PUT", "/api/profiles/office", 200, 400}, {"POST", "/api/profiles/office/clone", 201, 400},
	} {
		for _, input := range []struct {
			name, body string
			python     int
		}{{"malformed", "{", route.malformed}, {"empty JSON", "", route.malformed}, {"nonfinite", "NaN", route.nonfinite}} {
			t.Run(route.path+"/"+input.name, func(t *testing.T) {
				w := newWorld(t)
				r := request{Method: route.method, Path: route.path, Body: raw(input.body), Headers: map[string]string{"Authorization": w.token, "Content-Type": "application/json"}}
				before := snapshot(t, w.srv)
				py := w.py.send(t, r)
				got := sendGo(t, w.srv, r)
				compare(t, "Python original JSON boundary", py.Status, input.python)
				compare(t, "Go native JSON status", got.Status, 400)
				compare(t, "Go native JSON envelope", got.Body, map[string]any{"success": false, "message": httpx.DescBadRequest})
				compare(t, "Go rejected JSON storage", got.State, before)
			})
		}
	}
}

func TestN811W5ClientImportShape(t *testing.T) {
	w := newWorld(t)
	payload := map[string]any{"id": "typed-import", "name": "Typed import", "client_type": "loon", "content": "[Proxy Group]\nProxy = select,DIRECT\n[Rule]\nFINAL,Proxy\n"}
	payload["dry_run"] = true
	preview := w.check(post("/api/profiles/import-client", payload))
	fields := preview.Body.(map[string]any)
	if len(fields) != 3 || fields["summary"] == nil || fields["warnings"] == nil {
		t.Fatalf("preview diagnostics shape: %v", fields)
	}
	payload["dry_run"] = false
	r := post("/api/profiles/import-client", payload)
	r.Headers = map[string]string{"Authorization": w.token, "Content-Type": "application/json"}
	py := w.py.send(t, r)
	got := sendGo(t, w.srv, r)
	compare(t, "client import status", got.Status, py.Status)
	compare(t, "client import response", got.Body, py.Body)
	fields = got.Body.(map[string]any)
	if len(fields) != 4 || fields["profile"] == nil || fields["summary"] == nil || fields["warnings"] == nil {
		t.Fatalf("import diagnostics shape: %v", fields)
	}
	normalizeGroup := func(state any) any {
		tree := native(t, state).(map[string]any)
		profile := tree["document"].(map[string]any)["profiles"].(map[string]any)["typed-import"].(map[string]any)
		groups := profile["proxy_groups"].([]any)
		rules := profile["rule_configs"].([]any)
		if len(groups) != 1 || len(rules) != 1 {
			t.Fatalf("imported group/rule counts: %v / %v", groups, rules)
		}
		ids := []string{}
		for i, row := range []any{groups[0], rules[0]} {
			record := row.(map[string]any)
			id, ok := record["id"].(string)
			prefix := []string{"group_", "rule_"}[i]
			if !ok || len(id) != len(prefix)+12 || id[:len(prefix)] != prefix {
				t.Fatalf("invalid generated id: %v", id)
			}
			for _, digit := range id[len(prefix):] {
				if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f') {
					t.Fatalf("invalid id: %s", id)
				}
			}
			ids = append(ids, id)
		}
		// Replace only the proven random suffixes, consistently in every durable projection.
		raw, _ := json.Marshal(tree)
		for i, id := range ids {
			raw = bytes.ReplaceAll(raw, []byte(id), []byte([]string{"<generated-group>", "<generated-rule>"}[i]))
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	compare(t, "client import stored document and files", modeltest.StateThroughModel(normalizeGroup(got.State)), modeltest.StateThroughModel(normalizeGroup(py.State)))
}
