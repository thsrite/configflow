package routes

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestServerArchive uses the Agent's production extractor and manifest decoder.
func TestServerArchive(t *testing.T) {
	root := os.Getenv("CONFIGFLOW_SERVER_ARCHIVES")
	if root == "" {
		t.Skip("server archives not supplied")
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.tar.gz"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("archives: %v %v", paths, err)
	}
	for _, name := range paths {
		t.Run(filepath.Base(name), func(t *testing.T) {
			archive, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			gz, err := gzip.NewReader(f)
			if err != nil {
				t.Fatal(err)
			}
			defer gz.Close()
			tr := tar.NewReader(gz)
			var manifest DeploymentManifest
			for {
				h, err := tr.Next()
				if err != nil {
					t.Fatal(err)
				}
				if h.Name == "manifest.json" {
					if err = json.NewDecoder(tr).Decode(&manifest); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			cfg := &Config{ConfigPath: filepath.Join(t.TempDir(), manifest.ConfigPath), ServiceType: manifest.ServiceType, AgentID: manifest.AgentID}
			state := &DeploymentState{DeploymentID: manifest.DeploymentID}
			dir := deploymentPath(cfg, state.DeploymentID)
			if err = os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "bundle.tar.gz"), archive, 0600); err != nil {
				t.Fatal(err)
			}
			if err = extractDeployment(cfg, state); err != nil {
				t.Fatal(err)
			}
			for _, entry := range state.Manifest.Files {
				out, err := os.Open(filepath.Join(dir, "stage", filepath.FromSlash(entry.Path)))
				if err != nil {
					t.Fatal(err)
				}
				h := sha256.New()
				n, err := io.Copy(h, out)
				out.Close()
				if err != nil || n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
					t.Fatal("extracted bytes differ", entry.Path, err)
				}
			}
		})
	}
}
