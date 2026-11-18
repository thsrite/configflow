package difftest

import (
	"encoding/json"
	"testing"
)

// TestReviewC34CustomEmptyBodies separates explicit {} from null and absent
// bodies using a nonempty saved draft and the real Flask request boundary.
func TestReviewC34CustomEmptyBodies(t *testing.T) {
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		for _, shape := range []struct{ name, text, media string }{
			{"null", "null", "application/json"},
			{"missing-json", "", "application/json"},
			{"missing-no-media", "", ""},
			{"object", "{}", "application/json"},
		} {
			t.Run(engine+"/"+shape.name, func(t *testing.T) {
				w := newWorld(t)
				path := "/api/custom-config/" + engine
				w.check(post(path, map[string]string{"config": "# retained synthetic draft\n"}))
				get := request{Method: "GET", Path: path, Headers: map[string]string{"Authorization": w.token}}
				beforePy := w.py.send(t, get)
				beforeGo := sendGo(t, w.srv, get)
				call := request{Method: "POST", Path: path, Body: raw(shape.text), Headers: map[string]string{"Authorization": w.token}}
				if shape.media != "" {
					call.Headers["Content-Type"] = shape.media
				}
				py := w.py.send(t, call)
				got := sendGo(t, w.srv, call)
				status := 500
				if engine == "oxidns" {
					status = 400
				} else if shape.name == "object" || shape.name == "null" && (engine == "surge" || engine == "loon") {
					status = 200
				}
				if py.Status != status {
					t.Fatalf("unexpected Flask status %d want %d", py.Status, status)
				}
				clears := shape.name == "object" && (engine == "mihomo" || engine == "mosdns")
				assertEmptyBodyEffect(t, "Python", beforePy.State, py.State, engine, status == 200, clears)
				t.Logf("Flask status=%d revision_delta=%d clears_config=%v", status, map[bool]int{true: 1}[status == 200], clears)
				if got.Status != status {
					t.Errorf("Go status=%d want Flask=%d", got.Status, status)
				}
				assertEmptyBodyEffect(t, "Go", beforeGo.State, got.State, engine, status == 200, clears)
				compare(t, "Flask/Go complete state", got.State, py.State)
			})
		}
	}
}

func assertEmptyBodyEffect(t *testing.T, label string, before, after any, engine string, commit, clears bool) {
	t.Helper()
	want := native(t, before).(map[string]any)
	profile := want["document"].(map[string]any)["profiles"].(map[string]any)["default"].(map[string]any)
	if commit {
		profile["_revision"] = profile["_revision"].(float64) + 1
	}
	if clears {
		profile[engine].(map[string]any)["custom_config"] = ""
	}
	actual := native(t, after).(map[string]any)["document"].(map[string]any)["profiles"].(map[string]any)["default"].(map[string]any)
	// The general differential compares parsed YAML. A comment-only draft
	// and an empty draft parse identically, so assert the stored text too.
	gotDraft := actual[engine].(map[string]any)["custom_config"].(string)
	wantDraft := profile[engine].(map[string]any)["custom_config"].(string)
	if gotDraft != wantDraft {
		t.Fatalf("%s changed stored draft text: got %q want %q", label, gotDraft, wantDraft)
	}
	compare(t, label+" exact fields/revision/files/cache", after, want)
}

func TestReviewC34ClientImportDryRunShapes(t *testing.T) {
	w := newWorld(t)
	for _, value := range []json.RawMessage{rawJSON(`true`), rawJSON(`"yes"`), rawJSON(`1`), rawJSON(`[true]`), rawJSON(`{"preview":true}`)} {
		payload := map[string]any{"client_type": "loon", "content": "[Proxy Group]\nProxy = select,DIRECT\n[Rule]\nFINAL,Proxy\n", "name": "Dry run", "id": "dry-run", "dry_run": value}
		before := snapshot(t, w.srv)
		r := post("/api/profiles/import-client", payload)
		r.Headers = map[string]string{"Authorization": w.token, "Content-Type": "application/json"}
		py := w.py.send(t, r)
		got := sendGo(t, w.srv, r)
		compare(t, "Python dry_run status", py.Status, 200)
		compare(t, "Python dry_run storage", py.State, before)
		compare(t, "Go dry_run status", got.Status, py.Status)
		compare(t, "Go dry_run response", got.Body, py.Body)
		compare(t, "Go dry_run storage", got.State, before)
	}
}

func TestReviewC34ProfileEmptyAlternatives(t *testing.T) {
	w := newWorld(t)
	for _, text := range []string{`{}`, `null`, `false`, `[]`, `""`, `0`, `0.0`, `-0`, `-0.00`, `0e0`, `0.0e5`} {
		w.check(request{Method: "PUT", Path: "/api/profiles/office", Body: raw(text)})
	}
	for _, text := range []string{`"0"`, `"0.0"`, `true`, `[1]`} {
		w.check(request{Method: "PUT", Path: "/api/profiles/office", Body: raw(text)})
		w.check(request{Method: "POST", Path: "/api/profiles/office/clone", Body: raw(text)})
	}
	for _, value := range []json.RawMessage{rawJSON(`null`), rawJSON(`false`), rawJSON(`0`), rawJSON(`""`), rawJSON(`{}`), rawJSON(`[]`), rawJSON(`[1,null,false,"missing"]`)} {
		w.check(post("/api/profiles/import-client", map[string]any{"client_type": "loon", "content": "[Rule]\nFINAL,DIRECT\n", "name": "Dry run", "dry_run": true, "default_subscription_ids": value}))
	}
}

func rawJSON(value string) json.RawMessage { return json.RawMessage(value) }

func TestReviewC34InvalidCloneSourceCannotCreateProfile(t *testing.T) {
	w := newWorld(t)
	for _, source := range []any{1, true, []any{"office"}, map[string]any{"id": "office"}} {
		r := post("/api/profiles", map[string]any{"id": "invalid-clone", "name": "Invalid clone", "clone_from": source})
		r.Headers = map[string]string{"Authorization": w.token, "Content-Type": "application/json"}
		before := snapshot(t, w.srv)
		py, got := w.py.send(t, r), sendGo(t, w.srv, r)
		compare(t, "Python invalid clone source", py.Status, 400)
		compare(t, "Go invalid clone source", got.Status, py.Status)
		compare(t, "invalid clone response", got.Body, py.Body)
		compare(t, "Python invalid clone storage", py.State, before)
		compare(t, "Go invalid clone storage", got.State, before)
	}
	w.check(post("/api/profiles", map[string]any{"id": "valid-clone", "name": "Valid clone", "clone_from": "office"}))
}
