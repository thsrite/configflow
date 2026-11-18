package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
)

func stored(t *testing.T, s *httpxtest.Server) string {
	t.Helper()
	doc, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	return string(raw)
}
func businessFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == ".config.lock" || strings.HasPrefix(d.Name(), "configflow.sqlite3") {
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
func TestCustomHEADAndBodyBinding(t *testing.T) {
	s := exportApp(t)
	if _, err := s.Repo.WriteGenerated(s.Context(), "default", "config.yaml", []byte("cache: unchanged\n")); err != nil {
		t.Fatal(err)
	}
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		t.Run(engine, func(t *testing.T) {
			path := "/api/custom-config/" + engine
			get := s.Get(path)
			get.WantStatus(200)
			before, files := stored(t, s), businessFiles(t, s.DataDir)
			head := s.Do("HEAD", path, httpxtest.Body([]byte(`{"config":"changed","provider_update_interval":60}`), "application/json"))
			head.WantStatus(get.Status)
			if len(head.Body) != 0 {
				t.Fatal("HEAD body")
			}
			for key, values := range get.Header {
				if !reflect.DeepEqual(values, head.Header.Values(key)) {
					t.Fatalf("HEAD header %s", key)
				}
			}
			if before != stored(t, s) || !reflect.DeepEqual(files, businessFiles(t, s.DataDir)) {
				t.Fatal("HEAD wrote DB/revisions/files/caches")
			}
			for _, body := range []string{`{`, `[]`, `null`, `"text"`, `{"config":NaN}`} {
				before = stored(t, s)
				s.Post(path, httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
				if before != stored(t, s) {
					t.Fatal("invalid body wrote DB")
				}
			}
		})
	}
}
func TestCustomSavesAndPublishGuards(t *testing.T) {
	s := exportApp(t)
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		content := ""
		if engine == "mihomo" {
			content = "ipv6: false\n"
		}
		response := s.Post("/api/custom-config/"+engine, httpxtest.JSON(jsonx.Obj("config", content)))
		response.WantStatus(200)
		var out struct {
			Success bool
			Guard   json.RawMessage `json:"publish_guard"`
		}
		if err := json.Unmarshal(response.Body, &out); err != nil {
			t.Fatal(err)
		}
		if !out.Success || (engine != "loon" && engine != "oxidns" && len(out.Guard) == 0) {
			t.Fatal("missing saved revision guard")
		}
	}
	before := stored(t, s)
	for _, body := range []string{`{"config":"bad: ["}`, `{"config":"ipv6: yes"}`, `{"config":"x: 1\nx: 2"}`, `{"config":"a: &a [*a]"}`, `{"config":"","provider_update_interval":1}`} {
		s.Post("/api/custom-config/mihomo", httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
		if stored(t, s) != before {
			t.Fatal("invalid YAML/settings committed")
		}
	}
}
func TestEveryCustomOwnerRuntime(t *testing.T) {
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
		for _, shutdown := range []bool{true, false} {
			t.Run(engine+map[bool]string{true: "/shutdown", false: "/disconnect"}[shutdown], func(t *testing.T) {
				s := exportApp(t)
				before := stored(t, s)
				rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				rt.Start(t.Context())
				t.Cleanup(func() { rt.Shutdown(t.Context()) })
				request, cancel := context.WithCancel(s.Context())
				defer cancel()
				ctx := rt.BindRequest(request)
				if shutdown {
					rt.Shutdown(t.Context())
				} else {
					cancel()
				}
				body := customRequest{Config: json.RawMessage(`""`)}
				_, err := saveCustom(ctx, "default", engine, body)
				if shutdown {
					if err == nil || stored(t, s) != before {
						t.Fatal("shutdown committed")
					}
				} else if err != nil || stored(t, s) == before {
					t.Fatalf("disconnect did not complete: %v", err)
				}
			})
		}
	}
}

func TestMihomoFormDefaultTemplateAndNullableTakeover(t *testing.T) {
	s := exportApp(t)
	result := s.Get("/api/custom-config/mihomo")
	result.WantStatus(200)
	var form struct {
		DefaultConfig string `json:"default_config"`
		Base          struct {
			Takeover *string `json:"takeover"`
		} `json:"base_settings"`
	}
	if err := json.Unmarshal(result.Body, &form); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(form.DefaultConfig, "mixed-port:") || !strings.Contains(form.DefaultConfig, "nameserver:") {
		t.Fatal("default template was lost at legacy/native YAML boundary")
	}
	if form.Base.Takeover != nil {
		t.Fatal("absent takeover must be null")
	}
}
