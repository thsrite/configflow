package config

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/configexport"
	"configflow/internal/convert"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routers"
)

func exportApp(t *testing.T) *httpxtest.Server {
	t.Helper()
	previous := repo.SetHooks(repo.Hooks{})
	t.Cleanup(func() { repo.SetHooks(previous) })
	configexport.Wire()
	routers.Wire()
	repo.RegisterHooks(repo.Hooks{RecordHooks: repo.RecordHooks{RedactEasytierJSON: convert.RedactEasyTierJSON}})
	return httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(Register)})
}

func TestFullAndShareableBackupHTTP(t *testing.T) {
	s := exportApp(t)
	err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		if err := system.SystemConfig.SetJSON("config_token", json.RawMessage(`"synthetic-config-token"`)); err != nil {
			return err
		}
		return system.SystemConfig.SetJSON("rule_proxy_token", json.RawMessage(`"synthetic-rule-token"`))
	})
	if err != nil {
		t.Fatal(err)
	}
	full := s.Get("/api/config/export")
	full.WantStatus(200)
	if !bytes.Contains(full.Body, []byte("synthetic-rule-token")) || !bytes.Contains(full.Body, []byte("synthetic-config-token")) {
		t.Fatal("restorable backup lost credentials")
	}
	if full.Header.Get("Content-Disposition") != "attachment; filename=config.json" {
		t.Fatal(full.Header)
	}
	description := "contains synthetic-rule-token"
	_, err = s.Repo.UpdateProfileMetadataRecord(s.Context(), "default", repo.ProfileMetadataUpdate{Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	share := s.Get("/api/config/export?desensitize=true")
	share.WantStatus(200)
	if !bytes.Contains(share.Body, []byte("[REDACTED]")) {
		t.Fatal("desensitize bypassed external payload policy")
	}
	if bytes.Contains(share.Body, []byte("synthetic-rule-token")) || bytes.Contains(share.Body, []byte("synthetic-config-token")) {
		t.Fatal("shareable backup leaked credentials")
	}
	s.Post("/api/config/import", httpxtest.Body(full.Body, "application/json")).WantStatus(200)
	for _, body := range []string{`[]`, `null`, `"text"`, `{`} {
		s.Post("/api/config/import", httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
	}
	s.Post("/api/config/import", httpxtest.Body([]byte(`{}`), "text/plain")).WantStatus(415)
	s.Post("/api/config/reset").WantStatus(200)
}

func TestGlobalOwnersRuntimeShutdownAndDisconnect(t *testing.T) {
	for _, reset := range []bool{true, false} {
		for _, shutdown := range []bool{true, false} {
			t.Run(map[bool]string{true: "reset", false: "import"}[reset]+map[bool]string{true: "/shutdown", false: "/disconnect"}[shutdown], func(t *testing.T) {
				s := exportApp(t)
				_, err := s.Repo.CreateProfileRecord(s.Context(), repo.ProfileCreation{ID: "extra", Name: "Extra"}, "")
				if err != nil {
					t.Fatal(err)
				}
				before, err := s.Repo.ExportAllJSON(s.Context(), false)
				if err != nil {
					t.Fatal(err)
				}
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
				var encoded []byte
				if !reset {
					var doc map[string]any
					if err := json.Unmarshal(before, &doc); err != nil {
						t.Fatal(err)
					}
					doc["profiles"].(map[string]any)["extra"].(map[string]any)["name"] = "Changed"
					var encodeErr error
					encoded, encodeErr = json.Marshal(doc)
					if encodeErr != nil {
						t.Fatal(encodeErr)
					}
				} else {
					encoded = []byte("null")
				}
				err = writeGlobal(ctx, encoded, reset)
				after, readErr := s.Repo.ExportAllJSON(s.Context(), false)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if shutdown {
					if err == nil || !bytes.Equal(before, after) {
						t.Fatal("shutdown committed global write")
					}
				} else if err != nil || bytes.Equal(before, after) {
					t.Fatalf("disconnect interrupted write: %v", err)
				}
			})
		}
	}
}
