package generate_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	api "configflow/internal/api/generate"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

func TestGeneratedDownloadsPreviewsAndProfileIsolation(t *testing.T) {
	mihomoenv.Wire()
	s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(api.Register)})
	if _, err := s.Repo.CloneProfile(s.Context(), "default", jsonx.Obj("id", "office", "name", "Office")); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/api/generate", "/api/profiles/office/generate"} {
		for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
			t.Run(prefix+engine, func(t *testing.T) {
				preview := s.Post(prefix+"/"+engine+"/preview", httpxtest.JSON(map[string]string{}))
				preview.WantStatus(200)
				var content struct {
					Content string `json:"content"`
				}
				if err := json.Unmarshal(preview.Body, &content); err != nil || content.Content == "" {
					t.Fatalf("empty preview %s", preview.Body)
				}
				if engine == "oxidns" {
					node, err := yamlx.Parse([]byte(content.Content))
					if err != nil || yamlx.Get(node, "plugins") == nil {
						t.Fatal("OxiDNS preview lost its native document", err)
					}
				}
				download := s.Post(prefix+"/"+engine, httpxtest.JSON(map[string]string{}))
				download.WantStatus(200)
				if engine == "mosdns" || engine == "oxidns" {
					archive, err := zip.NewReader(bytes.NewReader(download.Body), int64(len(download.Body)))
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, f := range archive.File {
						if f.Name == "config.yaml" {
							found = true
						}
					}
					if !found {
						t.Fatal("archive lacks main config")
					}
				} else {
					if len(download.Body) == 0 {
						t.Fatal("empty download")
					}
					id := "default"
					if strings.Contains(prefix, "office") {
						id = "office"
					}
					name := map[string]string{"mihomo": "config.yaml", "surge": "config.conf", "loon": "loon.lcf"}[engine]
					path, err := s.Repo.ProfilePath(id, "generated/"+name)
					if err != nil {
						t.Fatal(err)
					}
					data, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(data, download.Body) {
						t.Fatal("download differs from stored bytes", err)
					}
				}
			})
		}
	}
	for _, engine := range []string{"mihomo", "surge", "loon", "mosdns"} {
		s.Post("/api/generate/"+engine, httpxtest.Body([]byte(`{`), "application/json")).WantStatus(400)
		s.Post("/api/generate/"+engine, httpxtest.Body([]byte(`[]`), "application/json")).WantStatus(400)
		s.Post("/api/generate/"+engine, httpxtest.Body([]byte(`{}`), "text/plain")).WantStatus(415)
	}
}
