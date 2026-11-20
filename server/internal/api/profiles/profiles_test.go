package profiles

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func profileApp(t *testing.T) *httpxtest.Server {
	t.Helper()
	return httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(Register)})
}
func state(t *testing.T, s *httpxtest.Server) string {
	t.Helper()
	raw, err := s.Repo.ExportAllJSON(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func files(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), "configflow.sqlite3") || d.Name() == ".config.lock" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[strings.TrimPrefix(path, root)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestProfilesCRUDAndClientImport(t *testing.T) {
	s := profileApp(t)
	s.Post("/api/profiles", httpxtest.JSON(map[string]any{"id": "phone", "name": "Phone"})).WantStatus(201)
	s.Put("/api/profiles/phone", httpxtest.JSON(map[string]any{"name": "手机", "description": "Portable"})).WantStatus(200)
	s.Get("/api/profiles/phone").WantStatus(200)
	s.Post("/api/profiles/phone/clone", httpxtest.JSON(map[string]any{"id": "copy", "name": "Copy"})).WantStatus(201)
	s.Post("/api/profiles/copy/default").WantStatus(200)
	s.Get("/api/profiles/phone/agents").WantStatus(200)
	s.Get("/api/profiles/phone/export").WantStatus(200)
	s.Post("/api/profiles/phone/import", httpxtest.JSON(map[string]any{"loon": map[string]any{"custom_config": "[General]\nip-mode=ipv4-only"}})).WantStatus(200)
	s.Delete("/api/profiles/phone").WantStatus(204)
	for _, body := range []string{`{`, `[]`, `null`, `"text"`} {
		s.Post("/api/profiles/copy/import", httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
		s.Post("/api/profiles/copy/agents/rebind", httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
	}
	payload := map[string]any{"client_type": "loon", "content": "[General]\nip-mode=ipv4-only\n[Proxy Group]\nProxy = select,DIRECT\n[Rule]\nFINAL,Proxy\n", "name": "Imported", "id": "imported", "dry_run": true}
	before := state(t, s)
	s.Post("/api/profiles/import-client", httpxtest.JSON(payload)).WantStatus(200)
	if state(t, s) != before {
		t.Fatal("dry-run wrote DB")
	}
	payload["dry_run"] = false
	s.Post("/api/profiles/import-client", httpxtest.JSON(payload)).WantStatus(201)
	s.Get("/api/profiles/imported").WantStatus(200)
	payload["id"] = "bad"
	payload["content"] = "nothing"
	before = state(t, s)
	s.Post("/api/profiles/import-client", httpxtest.JSON(payload)).WantStatus(400)
	if state(t, s) != before {
		t.Fatal("invalid import wrote DB")
	}
}

func TestProfileHEADIsReadOnly(t *testing.T) {
	s := profileApp(t)
	if _, err := s.Repo.WriteGenerated(s.Context(), "default", "config.yaml", []byte("frozen: true\n")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/profiles", "/api/profiles/default", "/api/profiles/default/agents", "/api/profiles/default/export"} {
		get := s.Get(path)
		get.WantStatus(200)
		before, oldFiles := state(t, s), files(t, s.DataDir)
		head := s.Do("HEAD", path, httpxtest.Body([]byte(`{"id":"intruder","name":"changed","new_default":"other"}`), "application/json"))
		head.WantStatus(200)
		if len(head.Body) != 0 {
			t.Fatal("HEAD body")
		}
		for key, values := range get.Header {
			if !reflect.DeepEqual(head.Header.Values(key), values) {
				t.Fatalf("HEAD %s header %s", path, key)
			}
		}
		if state(t, s) != before || !reflect.DeepEqual(oldFiles, files(t, s.DataDir)) {
			t.Fatal("HEAD changed DB/revision/business files/cache")
		}
	}
}

func TestProfileOwnerRuntimeShutdownAndDisconnect(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		t.Run(map[bool]string{true: "shutdown", false: "disconnect"}[shutdown], func(t *testing.T) {
			s := profileApp(t)
			before := state(t, s)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(t.Context())
			request, cancel := context.WithCancel(s.Context())
			defer cancel()
			ctx := runtime.BindRequest(request)
			t.Cleanup(func() { runtime.Shutdown(t.Context()) })
			if shutdown {
				runtime.Shutdown(t.Context())
			} else {
				cancel()
			}
			_, err := mutate(ctx, func(ctx context.Context, r *repo.Repository) (any, error) {
				return r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "new", Name: "New"}, "")
			})
			if shutdown {
				if err == nil || state(t, s) != before {
					t.Fatal("shutdown committed")
				}
			} else if err != nil || state(t, s) == before {
				t.Fatalf("request cancellation interrupted commit: %v", err)
			}
		})
	}
}

// Actual dispatch must keep the static import-client path ahead of the generic
// profile-id route and register every method exactly once (HANDOFF 3c).
func TestProfileRegistration(t *testing.T) {
	s := profileApp(t)
	seen := map[string]bool{}
	for _, r := range s.App.Router.Routes() {
		for _, m := range r.Methods {
			key := m + " " + r.Path
			if seen[key] {
				t.Fatal("duplicate " + key)
			}
			seen[key] = true
		}
	}
	req := httptest.NewRequest("POST", "/api/profiles/import-client", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), msgName) {
		t.Fatal("import-client shadowed", rec.Code, rec.Body.String())
	}
}

func TestProfileExportRedactsCredentialEncodingCombinations(t *testing.T) {
	s := profileApp(t)
	err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		if err := system.SystemConfig.SetJSON("rule_proxy_token", json.RawMessage(`"synthetic-rule+/ 当前"`)); err != nil {
			return err
		}
		if err := system.SystemConfig.SetJSON("retired_rule_proxy_tokens", json.RawMessage(`["synthetic-retired+/ 旧"]`)); err != nil {
			return err
		}
		return system.SetJSON("agents", json.RawMessage(`[{"id":"synthetic-agent","name":"Agent","profile_id":"default","token":"synthetic-agent+/ 设备"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	embedded := map[string]string{}
	for _, credential := range []string{"synthetic-rule+/ 当前", "synthetic-retired+/ 旧", "synthetic-agent+/ 设备"} {
		text := credential
		for level := 0; level <= 20; level++ {
			embedded[credential+"-"+string(rune('a'+level))] = "https://example.com/source?token=" + text
			text = url.QueryEscape(text)
		}
	}
	fieldsRaw, _ := json.Marshal(map[string]any{"mihomo": map[string]any{"future_payload": embedded}})
	_, _, err = s.Repo.SaveProfileFieldsJSON(s.Context(), "default", fieldsRaw, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := state(t, s)
	response := s.Get("/api/profiles/default/export")
	response.WantStatus(200)
	for _, credential := range []string{"synthetic-rule+/ 当前", "synthetic-retired+/ 旧", "synthetic-agent+/ 设备"} {
		encoded := credential
		for level := 0; level <= 20; level++ {
			if bytes.Contains(response.Body, []byte(encoded)) {
				t.Fatal("profile export leaked raw/encoded stored credential")
			}
			encoded = url.QueryEscape(encoded)
		}
	}
	if state(t, s) != before {
		t.Fatal("profile export mutated stored payload")
	}
}

func TestAgentBindingHTTPUsesAtomicManagerOwner(t *testing.T) {
	s := profileApp(t)
	if _, err := s.Repo.CreateProfileRecord(s.Context(), repo.ProfileCreation{ID: "target", Name: "Target"}, ""); err != nil {
		t.Fatal(err)
	}
	err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		return system.SetJSON("agents", json.RawMessage(`[{"id":"first","name":"First","host":"192.0.2.1","port":8080,"profile_id":"default","token":"synthetic-first"},{"id":"second","name":"Second","host":"192.0.2.2","port":8080,"profile_id":"default","token":"synthetic-second","latest_upgrade":{"status":"installing"}}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"target_profile_id": "target", "agent_ids": []string{"first", "second"}}
	before := state(t, s)
	s.Post("/api/profiles/default/agents/rebind", httpxtest.JSON(payload)).WantStatus(409)
	if state(t, s) != before {
		t.Fatal("busy rebind partially committed")
	}
	err = s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		system.Agents[1].LatestUpgrade = nil
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response := s.Post("/api/profiles/default/agents/rebind", httpxtest.JSON(payload))
	response.WantStatus(200)
	if bytes.Contains(response.Body, []byte("synthetic-first")) || bytes.Contains(response.Body, []byte("synthetic-second")) {
		t.Fatal("rebind leaked credentials")
	}
	bindings, err := s.Repo.ProfileAgentBindingsRecord(s.Context(), "target")
	if err != nil || len(bindings.Agents) != 2 {
		t.Fatal("bindings not moved atomically", err)
	}
}
