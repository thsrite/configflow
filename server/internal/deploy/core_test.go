package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func sample(t *testing.T, service string, n int) *Bundle {
	t.Helper()
	files := map[string]Content{"config.yaml": {[]byte("plugins: []\n"), RoleConfig}, "rules/中文😀.txt": {[]byte("example.com\n"), RoleRule}, "empty.txt": {[]byte{}, RoleCustom}}
	for len(files) < n {
		files[fmt.Sprintf("rules/%04d.txt", len(files))] = Content{[]byte("example.net"), RoleRule}
	}
	m := Manifest{ProtocolVersion: 1, DeploymentID: "a11-example", AgentID: "agent-example", ProfileID: "profile-example", ServiceType: service, ConfigPath: "config.yaml", Files: []File{}}
	for p, c := range files {
		m.Files = append(m.Files, File{Path: p, Size: int64(len(c.Data)), SHA256: digest(c.Data), Role: c.Role})
	}
	b, err := Pack(m, files, digest(files["config.yaml"].Data)[:8], nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPythonCore(t *testing.T) {
	data, err := os.ReadFile("testdata/core.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Rows []struct {
			Name               string
			Manifest           json.RawMessage
			Fingerprint, Error string
			Digests            map[string]string
		}
		Paths []struct{ Value, Result, Error string }
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, row := range corpus.Rows {
		t.Run(row.Name, func(t *testing.T) {
			var m Manifest
			decodeErr := json.Unmarshal(row.Manifest, &m)
			if decodeErr != nil {
				if row.Error == "" {
					t.Fatal(decodeErr)
				}
				return
			}
			got, err := Fingerprint(m)
			if row.Error != "" {
				if err == nil {
					t.Fatalf("accepted: %s", row.Manifest)
				}
				if err.Error() != row.Error {
					t.Fatalf("error %q want %q", err, row.Error)
				}
				return
			}
			if err != nil || got != row.Fingerprint {
				t.Fatalf("fingerprint %q %v want %q", got, err, row.Fingerprint)
			}
			digests, err := Digests(m)
			if err != nil || !reflect.DeepEqual(digests, row.Digests) {
				t.Fatalf("digests %v %v", digests, err)
			}
		})
	}
	for _, row := range corpus.Paths {
		t.Run("path/"+row.Value, func(t *testing.T) {
			got, err := RelativePath(row.Value)
			if row.Error != "" {
				if err == nil || err.Error() != row.Error {
					t.Fatalf("%q %v want %q", got, err, row.Error)
				}
			} else if err != nil || got != row.Result {
				t.Fatalf("%q %v want %q", got, err, row.Result)
			}
		})
	}
	data, err = os.ReadFile("testdata/python-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile("testdata/python.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(m, archive, digest(archive), "version", nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := b.Files("rules/中文😀.txt")
	if err != nil || string(files["rules/中文😀.txt"]) != "example.com\n" {
		t.Fatal(files, err)
	}
}

func TestBundleImmutableLazyConcurrent(t *testing.T) {
	b := sample(t, "mihomo", 3)
	if b.archive != nil {
		t.Fatal("eager pack")
	}
	m := b.Manifest()
	m.Files[0].Name = "mutated"
	f, _ := b.Files()
	f["config.yaml"][0] = 'x'
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			a, err := b.Archive()
			if err != nil {
				t.Error(err)
				return
			}
			a[0] = 0
			files, err := b.Files()
			if err != nil || !bytes.Equal(files["config.yaml"], []byte("plugins: []\n")) {
				t.Error(files, err)
			}
		})
	}
	wg.Wait()
	if b.files != nil {
		t.Fatal("unpacked bytes retained")
	}
	a, _ := b.Archive()
	sha, _ := b.SHA256()
	o, err := Open(b.Manifest(), a, sha, b.ConfigVersion(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if equal, err := b.Equal(o); err != nil || !equal {
		t.Fatal(equal, err)
	}
	retargeted, err := Retarget(b, "中文😀.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := Fingerprint(b.Manifest())
	fp2, _ := Fingerprint(retargeted.Manifest())
	if fp != fp2 {
		t.Fatal(fp, fp2)
	}
	a[len(a)/2] ^= 1
	if _, err := Open(b.Manifest(), a, sha, b.ConfigVersion(), nil); err == nil {
		t.Fatal("accepted corrupt archive")
	}
}

// TestWriteServerArchives emits artifacts for the real go-agent extractor.
func TestWriteServerArchives(t *testing.T) {
	if os.Getenv("CONFIGFLOW_WRITE_ARCHIVES") == "" {
		t.Skip("conformance archive generation disabled")
	}
	if err := os.MkdirAll("testdata/archives", 0700); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"mihomo", "mosdns", "oxidns"} {
		for _, kind := range []string{"normal", "4096", "final", "reload", "resources"} {
			n := 3
			if kind == "4096" {
				n = 4096
			}
			b := sample(t, service, n)
			m := b.Manifest()
			files, _ := b.Contents()
			m.FinalConfig = kind == "final"
			m.ReloadOnly = kind == "reload"
			if kind == "resources" {
				files["GeoSite.dat"] = Content{[]byte("synthetic database"), RoleResource}
				m.Files = append(m.Files, File{Path: "GeoSite.dat", Size: 18, SHA256: digest(files["GeoSite.dat"].Data), Role: RoleResource})
			}
			b, err := Pack(m, files, b.ConfigVersion(), nil)
			if err != nil {
				t.Fatal(err)
			}
			a, err := b.Archive()
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join("testdata/archives", service+"-"+kind+".tar.gz"), a, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestFingerprintAggregateBound(t *testing.T) {
	m := Manifest{ProtocolVersion: 1, ServiceType: "mihomo", ConfigPath: "config.yaml", Files: []File{{Path: "config.yaml", Role: RoleConfig, Size: 1, SHA256: strings.Repeat("a", 64)}}}
	for i := range 4 {
		m.Files = append(m.Files, File{Path: fmt.Sprintf("rules/%d", i), Role: RoleRule, Size: MaxFileBytes, SHA256: strings.Repeat("a", 64)})
	}
	if _, err := Fingerprint(m); err == nil || err.Error() != "Deployment exceeds size limit" {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"reload_only":null}`, `{"final_config":"true"}`} {
		var manifest Manifest
		if err := json.Unmarshal([]byte(raw), &manifest); err == nil {
			t.Fatal("invalid flag accepted")
		}
	}
}
