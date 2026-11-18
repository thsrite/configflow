package difftest

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestC34ProfilesAndCustomConfiguration(t *testing.T) {
	w := newWorld(t)
	for _, r := range []request{
		{Method: "GET", Path: "/api/profiles"},
		post("/api/profiles", map[string]any{"id": "phone", "name": "Phone"}),
		{Method: "GET", Path: "/api/profiles/phone"},
		{Method: "PUT", Path: "/api/profiles/phone", Body: body(map[string]string{"name": "手机", "description": "Portable"})},
		post("/api/profiles/phone/clone", map[string]string{"id": "copy", "name": "Copy"}),
		post("/api/profiles/copy/default", nil),
		{Method: "GET", Path: "/api/profiles/phone/agents"},
		post("/api/profiles/phone/agents/rebind", map[string]any{"target_profile_id": "copy", "agent_ids": []any{}}),
		{Method: "GET", Path: "/api/profiles/phone/export"},
		post("/api/profiles/office/agents/rebind", map[string]any{"target_profile_id": "copy", "agent_ids": []string{"synthetic-agent"}}),
		post("/api/profiles/copy/agents/rebind", map[string]any{"target_profile_id": "office", "agent_ids": []string{"synthetic-agent"}}),
		post("/api/profiles/phone/import", map[string]any{"loon": map[string]string{"custom_config": "[General]\nip-mode=ipv4-only"}}),
		{Method: "DELETE", Path: "/api/profiles/phone"},
		{Method: "GET", Path: "/api/profiles/missing"},
	} {
		w.check(r)
	}
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		w.check(request{Method: "GET", Path: "/api/custom-config/" + engine})
		w.check(post("/api/custom-config/"+engine, map[string]string{"config": ""}))
		w.check(request{Method: "GET", Path: "/api/custom-config/" + engine})
	}
	w.check(post("/api/custom-config/mihomo", map[string]any{"config": "ipv6: false\ndns: {ipv6: false}\n", "provider_update_interval": 60}))
	w.check(post("/api/custom-config/mihomo", map[string]any{"config": "", "provider_update_interval": 1}))
	w.check(post("/api/profiles/import-client", map[string]any{"name": "Imported", "id": "imported", "client_type": "loon", "content": "[Proxy Group]\nProxy = select,DIRECT\n[Rule]\nFINAL,Proxy\n", "dry_run": true}))
	w.check(post("/api/profiles/import-client", map[string]any{"name": "Imported", "id": "imported", "client_type": "loon", "content": "nothing"}))
}
func TestC34GenerationAndLiveConfig(t *testing.T) {
	w := newWorld(t)
	for _, prefix := range []string{"/api/generate", "/api/profiles/office/generate"} {
		for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
			t.Run(prefix+engine, func(t *testing.T) {
				w.t = t
				w.check(post(prefix+"/"+engine+"/preview", map[string]string{}))
				w.check(post(prefix+"/"+engine, map[string]string{}))
			})
		}
	}
	w.t = t
	for _, prefix := range []string{"/api/config", "/api/config/office"} {
		for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
			w.check(request{Method: "GET", Path: prefix + "/" + engine})
		}
	}
	w.check(request{Method: "GET", Path: "/api/config/office/loon/ignored.lcf"})
}
func TestC34ExpectedDifferenceHEADReadOnly(t *testing.T) {
	for _, path := range []string{"/api/profiles", "/api/profiles/office", "/api/custom-config/mihomo", "/api/custom-config/surge", "/api/custom-config/loon", "/api/custom-config/mosdns", "/api/custom-config/oxidns"} {
		t.Run(path, func(t *testing.T) {
			w := newWorld(t)
			get := w.check(request{Method: "GET", Path: path})
			request := request{Method: "HEAD", Path: path, Body: raw(`{"id":"head-created","name":"Changed","config":""}`), Headers: map[string]string{"Authorization": w.token, "Content-Type": "application/json"}}
			got := sendGo(t, w.srv, request)
			compare(t, "HEAD status follows GET", got.Status, get.Status)
			compare(t, "HEAD headers", headers(got), headers(get))
			compare(t, "HEAD preserves DB/files/revision/cache", got.State, get.State)
			if got.Body != "" {
				t.Fatal("HEAD body")
			}
			py := w.py.send(t, request)
			if py.Status == get.Status && reflect.DeepEqual(py.State, get.State) {
				t.Fatal("expected Python HEAD bug not exercised")
			}
			t.Log("expected_difference=head_read_only")
		})
	}
}
func TestC34GlobalExports(t *testing.T) {
	w := newWorld(t)
	w.check(request{Method: "GET", Path: "/api/config/export"})
	w.check(request{Method: "GET", Path: "/api/config/export?desensitize=true"})
}

func TestC34ObjectFieldValues(t *testing.T) {
	w := newWorld(t)
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		for _, value := range []any{nil, false, 0, []any{}, map[string]any{}, true, 3} {
			w.check(post("/api/custom-config/"+engine, map[string]any{"config": value}))
		}
	}
}
func TestC34GenerationAcceptedJSONShapes(t *testing.T) {
	w := newWorld(t)
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		for _, text := range []string{`null`, `[]`, `false`, `0`, `""`, `"nonempty"`, `[1]`, `true`} {
			w.check(request{Method: "POST", Path: "/api/generate/" + engine + "/preview", Body: raw(text)})
		}
	}
}

// This table is an explicit approved schema correction, not a parity skip.
// Every old/new status and each side's independent durable effect is asserted.
func TestC34ExpectedDifferenceCustomJSONShapes(t *testing.T) {
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		t.Run(engine, func(t *testing.T) {
			w := newWorld(t)
			path := "/api/custom-config/" + engine
			for _, shape := range []struct {
				name, text string
				null       bool
				iterable   bool
			}{
				{"null", "null", true, false}, {"missing", "", true, false}, {"empty-string", `""`, false, true}, {"string", `"nonempty"`, false, true}, {"key-string", `"config"`, false, false}, {"empty-array", `[]`, false, true}, {"array", `[1]`, false, true}, {"key-array", `["config"]`, false, false}, {"zero", `0`, false, true}, {"number", `3`, false, false}, {"false", `false`, false, true}, {"true", `true`, false, false},
			} {
				t.Run(shape.name, func(t *testing.T) {
					headers := map[string]string{"Authorization": w.token, "Content-Type": "application/json"}
					get := request{Method: "GET", Path: path, Headers: headers}
					beforePy := w.py.send(t, get)
					beforeGo := sendGo(t, w.srv, get)
					call := request{Method: "POST", Path: path, Body: raw(shape.text), Headers: headers}
					py := w.py.send(t, call)
					got := sendGo(t, w.srv, call)
					wantPy := 500
					if engine == "oxidns" {
						wantPy = 400
					} else if engine == "surge" || engine == "loon" {
						if shape.name != "missing" && (shape.iterable || shape.name == "null") {
							wantPy = 200
						}
					}
					wantGo := 400
					if shape.null {
						wantGo = 200
					}
					if py.Status != wantPy || got.Status != wantGo {
						t.Fatalf("%s: Python=%d want %d, Go=%d want %d", shape.name, py.Status, wantPy, got.Status, wantGo)
					}
					assertUpdateEffect(t, "Python", beforePy.State, py.State, py.Status == 200)
					assertUpdateEffect(t, "Go", beforeGo.State, got.State, shape.null)
					t.Logf("expected_difference=custom_json_schema shape=%s Python=%d Go=%d", shape.name, py.Status, got.Status)
				})
			}
		})
	}
}
func assertUpdateEffect(t *testing.T, label string, before, after any, commit bool) {
	t.Helper()
	a := native(t, before).(map[string]any)
	b := native(t, after).(map[string]any)
	left := a["document"].(map[string]any)["profiles"].(map[string]any)["default"].(map[string]any)
	right := b["document"].(map[string]any)["profiles"].(map[string]any)["default"].(map[string]any)
	delta := right["_revision"].(float64) - left["_revision"].(float64)
	want := float64(0)
	if commit {
		want = 1
	}
	if delta != want {
		t.Fatalf("%s revision delta=%v want %v", label, delta, want)
	}
	if !commit {
		compare(t, label+" rejected shape preserves full storage", after, before)
		return
	}
	delete(left, "_revision")
	delete(right, "_revision")
	delete(left, "updated_at")
	delete(right, "updated_at")
	compare(t, label+" empty update preserves every field/file", a, b)
}

func TestC34ExplicitBindingDifferences(t *testing.T) {
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		w := newWorld(t)
		for _, input := range []struct {
			name, body, media string
			status            int
		}{
			{"malformed", `{"config":`, "application/json", 400},
			{"media", `{}`, "text/plain", 415},
			{"nonfinite", `{"config":NaN}`, "application/json", 400},
		} {
			t.Run(engine+"/"+input.name, func(t *testing.T) {
				headers := map[string]string{"Authorization": w.token, "Content-Type": input.media}
				path := "/api/custom-config/" + engine
				get := request{Method: "GET", Path: path, Headers: map[string]string{"Authorization": w.token}}
				beforePy := w.py.send(t, get)
				beforeGo := sendGo(t, w.srv, get)
				call := request{Method: "POST", Path: path, Body: raw(input.body), Headers: headers}
				py := w.py.send(t, call)
				got := sendGo(t, w.srv, call)
				wantPy := 500
				wantGo := input.status
				if engine == "oxidns" {
					wantPy = 400
					wantGo = 400
				} else if input.name == "nonfinite" && engine != "mihomo" {
					wantPy = 200
				}
				if py.Status != wantPy || got.Status != wantGo {
					t.Fatalf("Python=%d want %d Go=%d want %d", py.Status, wantPy, got.Status, wantGo)
				}
				assertUpdateEffect(t, "Go", beforeGo.State, got.State, false)
				if wantPy != 200 {
					assertUpdateEffect(t, "Python", beforePy.State, py.State, false)
				} else {
					original := native(t, beforePy.State).(map[string]any)
					current := native(t, py.State).(map[string]any)
					profile := original["document"].(map[string]any)["profiles"].(map[string]any)["default"].(map[string]any)
					profile[engine].(map[string]any)["custom_config"] = map[string]any{"$oracle_nonfinite": "NaN"}
					profile["_revision"] = profile["_revision"].(float64) + 1
					compare(t, "Python NaN changes only accepted field/revision/time", current, original)
				}
				t.Logf("expected_difference=binding shape=%s Python=%d Go=%d", input.name, py.Status, got.Status)
			})
		}
	}
	w := newWorld(t)
	for _, path := range []string{"/api/profiles/office/import", "/api/profiles/office/agents/rebind"} {
		w.check(request{Method: "POST", Path: path, Body: raw(`{`)})
	}
}

func TestC34BaseURLInputAlternatives(t *testing.T) {
	w := newWorld(t)
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		baseline := w.check(post("/api/generate/"+engine+"/preview", map[string]any{}))
		for _, test := range []struct {
			name    string
			value   any
			invalid bool
		}{
			{"null", nil, false}, {"false", false, true}, {"zero", 0, true}, {"zero-float", json.RawMessage(`0.0`), true}, {"array", []any{}, true}, {"object", map[string]any{}, true}, {"empty", "", false}, {"true", true, true}, {"number", 3, true}, {"url", "http://example.com", false},
		} {
			r := post("/api/generate/"+engine+"/preview", map[string]any{"base_url": test.value})
			r.Headers = map[string]string{"Content-Type": "application/json", "Authorization": w.token}
			py := w.py.send(t, r)
			got := sendGo(t, w.srv, r)
			want := 200
			if test.invalid {
				want = 400
			}
			if got.Status != want {
				t.Fatalf("%s/%s Go=%d want %d", engine, test.name, got.Status, want)
			}
			compare(t, "Go URL input preserves DB/files/cache", got.State, baseline.State)
			compare(t, "Python preview does not write", py.State, baseline.State)
			wantPy := 200
			if engine == "oxidns" && test.name != "empty" && test.name != "url" {
				wantPy = 500
			}
			if py.Status != wantPy {
				t.Fatalf("%s/%s Python=%d want %d", engine, test.name, py.Status, wantPy)
			}
			if engine == "surge" && test.name == "null" {
				content := py.Body.(map[string]any)["content"].(string)
				if !strings.Contains(content, "None/api/") {
					t.Fatal("Python invalid None URL was not exercised")
				}
			}
			if test.invalid {
				for _, prefix := range []string{"/api/generate", "/api/profiles/office/generate"} {
					call := post(prefix+"/"+engine, map[string]any{"base_url": test.value})
					call.Headers = r.Headers
					rejected := sendGo(t, w.srv, call)
					if rejected.Status != 400 {
						t.Fatal("download URL shape did not reject")
					}
					compare(t, "invalid URL download preserves DB/files/cache", rejected.State, baseline.State)
				}
			}
			t.Logf("base_url engine=%s shape=%s Python=%d Go=%d", engine, test.name, py.Status, got.Status)
			if test.name == "null" {
				compare(t, "null means no override", got.Body, baseline.Body)
			}
			if test.name == "empty" || test.name == "url" {
				compare(t, "valid string status", got.Status, py.Status)
				compare(t, "valid string output", got.Body, py.Body)
			}
		}
	}
}
