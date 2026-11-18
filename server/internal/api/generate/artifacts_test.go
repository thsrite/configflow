package generate_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	api "configflow/internal/api/generate"
	apiprofiles "configflow/internal/api/profiles"
	"configflow/internal/deploy"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/server"
)

func artifactServer(t *testing.T) *httpxtest.Server {
	t.Helper()
	return httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(api.Register, apiprofiles.Register), Setup: func(_ *httpx.App) { server.NewApp() }})
}

func createArtifact(t *testing.T, s *httpxtest.Server, path string) deploy.Artifact {
	t.Helper()
	response := s.Post(path, httpxtest.JSON(struct{}{})).WantStatus(200)
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("artifact must not be cached")
	}
	var artifact deploy.Artifact
	if err := json.Unmarshal(response.Body, &artifact); err != nil {
		t.Fatal(err)
	}
	if !artifact.Success || artifact.AgentID != "manual" || len(artifact.ID) != 32 || len(artifact.DeploymentID) != 32 || artifact.ExpiresAt <= time.Now().Unix() {
		t.Fatal("invalid artifact identity")
	}
	return artifact
}

func TestArtifactSuccessfulPathsAndFrozenBytes(t *testing.T) {
	s := artifactServer(t)
	s.Post("/api/profiles", httpxtest.JSON(struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{"office", "Office"})).WantStatus(201)
	for _, prefix := range []string{"/api/generate", "/api/profiles/office/generate"} {
		for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
			t.Run(prefix+"/"+engine, func(t *testing.T) {
				artifact := createArtifact(t, s, prefix+"/"+engine+"/artifact")
				wantProfile := "default"
				if strings.Contains(prefix, "office") {
					wantProfile = "office"
				}
				if artifact.ProfileID != wantProfile || artifact.ServiceType != engine || len(artifact.TargetIDs) != 0 {
					t.Fatal("lost profile/owner binding")
				}
				base := "/api/generate/artifacts/" + artifact.ID
				result := s.Get(base + "/download").WantStatus(200)
				if result.Header.Get("Cache-Control") != "no-store" || !strings.Contains(result.Header.Get("Content-Disposition"), engine+"-final-config.zip") {
					t.Fatal("incorrect download headers")
				}
				archive, err := zip.NewReader(bytes.NewReader(result.Body), int64(len(result.Body)))
				if err != nil {
					t.Fatal(err)
				}
				if len(archive.File) != len(artifact.Files) {
					t.Fatal("file list mismatch")
				}
				for i, file := range archive.File {
					if file.ExternalAttrs>>16 != 0600 {
						t.Fatal("configuration ZIP must keep private file permissions")
					}
					reader, err := file.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(reader)
					reader.Close()
					if err != nil {
						t.Fatal(err)
					}
					sum := sha256.Sum256(data)
					if metadata := artifact.Files[i]; metadata.Path != file.Name || metadata.Size != int64(len(data)) || metadata.SHA256 != hex.EncodeToString(sum[:]) {
						t.Fatal("manifest does not describe exported bytes")
					}
					part := s.Get(base + "/file?path=" + file.Name).WantStatus(200)
					if !bytes.Equal(part.Body, data) || part.Header.Get("Cache-Control") != "no-store" {
						t.Fatal("file bytes/headers mismatch")
					}
					if file.Name == artifact.ConfigPath && artifact.Content != string(data) {
						t.Fatal("preview differs from download")
					}
				}
				for _, path := range []string{"", "../record.json", "/etc/passwd", "config.yaml/../record.json", "missing"} {
					s.Get(base + "/file?path=" + url.QueryEscape(path)).WantStatus(404)
				}
				before, err := os.ReadFile(filepath.Join(s.DataDir, ".generated-previews", artifact.ID, "record.json"))
				if err != nil {
					t.Fatal(err)
				}
				head := s.Do("HEAD", base+"/download").WantStatus(200)
				if len(head.Body) != 0 {
					t.Fatal("HEAD returned body")
				}
				after, _ := os.ReadFile(filepath.Join(s.DataDir, ".generated-previews", artifact.ID, "record.json"))
				if !bytes.Equal(before, after) {
					t.Fatal("HEAD mutated preview")
				}
			})
		}
	}
	// The captured artifact survives edits and a default-profile switch.
	frozen := createArtifact(t, s, "/api/profiles/office/generate/mihomo/artifact?profile_id=default")
	if frozen.ProfileID != "office" {
		t.Fatal("query overrode explicit path")
	}
	if err := s.Repo.Update(s.Context(), func(_ context.Context, d *model.Document) error {
		for _, p := range d.Profiles {
			if p.ID == "office" {
				p.Mihomo.CustomConfig = "mode: global\n"
				p.Revision++
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.Post("/api/profiles/office/default").WantStatus(200)
	downloaded := s.Get("/api/generate/artifacts/" + frozen.ID + "/file?path=config.yaml").WantStatus(200)
	if string(downloaded.Body) != frozen.Content {
		t.Fatal("read regenerated from current profile")
	}
	s.Post("/api/generate/unknown/artifact").WantStatus(400)
	s.Get("/api/generate/artifacts/synthetic/unknown").WantStatus(404)
	s.Get("/api/generate/artifacts/synthetic/file").WantStatus(409)
}

func TestArtifactRejectsForeignExpiredAndCorruptPreviews(t *testing.T) {
	for _, kind := range []string{"foreign", "expired", "archive-corrupt", "archive-symlink", "record-symlink", "directory-symlink", "root-symlink"} {
		t.Run(kind, func(t *testing.T) {
			s := artifactServer(t)
			artifact := createArtifact(t, s, "/api/generate/surge/artifact")
			folder := filepath.Join(s.DataDir, ".generated-previews", artifact.ID)
			recordPath := filepath.Join(folder, "record.json")
			raw, err := os.ReadFile(recordPath)
			if err != nil {
				t.Fatal(err)
			}
			var record deploy.Preview
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "foreign":
				record.AgentID = "other-agent"
			case "expired":
				record.ExpiresAt = time.Now().Unix() - 1
			case "archive-corrupt":
				if err := os.WriteFile(filepath.Join(folder, "bundle.tar.gz"), []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				path := filepath.Join(folder, "bundle.tar.gz")
				if kind == "record-symlink" {
					path = recordPath
				}
				if kind == "directory-symlink" {
					path = folder
				}
				if kind == "root-symlink" {
					path = filepath.Dir(folder)
				}
				moved := filepath.Join(t.TempDir(), "foreign")
				if err := os.Rename(path, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, path); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "foreign" || kind == "expired" {
				raw, err = json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(recordPath, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := s.Repo.ExportAll(s.Context(), false)
			for _, action := range []string{"file?path=surge.conf", "download"} {
				result := s.Get("/api/generate/artifacts/" + artifact.ID + "/" + action).WantStatus(409)
				var body struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(result.Body, &body); err != nil || body.Code != "preview_changed" {
					t.Fatal("missing preview error")
				}
			}
			after, _ := s.Repo.ExportAll(s.Context(), false)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("read changed configuration")
			}
		})
	}
}

func TestCompleteC34RegistrationHasOneOwner(t *testing.T) {
	app := server.NewApp()
	seen := map[string]string{}
	count := 0
	for _, r := range app.Router.Routes() {
		for _, method := range r.Methods {
			key := method + " " + r.Path
			if old, ok := seen[key]; ok {
				t.Fatalf("route conflict %s: %s %s", key, old, r.Endpoint)
			}
			seen[key] = r.Endpoint
			if method == "HEAD" || method == "OPTIONS" {
				continue
			}
			if c34Owned(r) {
				count++
			}
		}
	}
	if count != 59 {
		t.Fatalf("C34 owns %d methods, want 59", count)
	}
}

// Review (artifacts HR): only a canonical lowercase 32-hex capability selects a
// directory. Each variant has a planted, otherwise valid record under exactly
// that name, so a looser ID check would serve it instead of 409.
func TestArtifactPreviewIDVariantsNeverSelectPlantedRecords(t *testing.T) {
	s := artifactServer(t)
	artifact := createArtifact(t, s, "/api/generate/surge/artifact")
	root := filepath.Join(s.DataDir, ".generated-previews")
	source := filepath.Join(root, artifact.ID)
	raw, err := os.ReadFile(filepath.Join(source, "record.json"))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join(source, "bundle.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, id string }{
		{"uppercase", strings.ToUpper(artifact.ID)},
		{"trailing-newline", artifact.ID + "\n"},
		{"short", artifact.ID[:31]},
		{"long", artifact.ID + "0"},
		{"dot-prefix", "." + artifact.ID[1:]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var record map[string]any
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			record["id"] = tt.id
			planted, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			folder := filepath.Join(root, tt.id)
			if err := os.Mkdir(folder, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(folder, "record.json"), planted, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(folder, "bundle.tar.gz"), archive, 0600); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"file?path=surge.conf", "download"} {
				result := s.Get("/api/generate/artifacts/" + url.PathEscape(tt.id) + "/" + action).WantStatus(409)
				var body struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(result.Body, &body); err != nil || body.Code != "preview_changed" {
					t.Fatalf("%s: planted record selected: %s", action, result.Body)
				}
			}
		})
	}
	// Positive control: the canonical ID with the same bytes is served.
	s.Get("/api/generate/artifacts/" + artifact.ID + "/file?path=surge.conf").WantStatus(200)
}
