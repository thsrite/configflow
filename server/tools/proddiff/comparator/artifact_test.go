package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const artifactRoute = "/api/generate/<service>/artifact"

type artifactFixture struct {
	public map[string]any
	folder string
}

func sha(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// newArtifact writes a preview record and bundle like the server does.
func newArtifact(t *testing.T, root, identity string, content, auxiliary []byte) artifactFixture {
	t.Helper()
	paths := []string{"config.yaml", "rules/example.txt"}
	data := map[string][]byte{"config.yaml": content, "rules/example.txt": auxiliary}
	var files []any
	for _, path := range paths {
		role := "custom"
		if path == "config.yaml" {
			role = "config"
		}
		files = append(files, map[string]any{"path": path, "role": role, "size": len(data[path]), "sha256": sha(data[path])})
	}
	manifest := map[string]any{"protocol_version": 1, "deployment_id": reverse(identity), "agent_id": "manual",
		"profile_id": "default", "service_type": "mihomo", "config_path": "config.yaml", "files": files}
	manifestBytes, _ := json.Marshal(manifest)
	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	tw := tar.NewWriter(gz)
	entries := [][2]any{{"manifest.json", manifestBytes}}
	for _, path := range paths {
		entries = append(entries, [2]any{"files/" + path, data[path]})
	}
	for _, entry := range entries {
		raw := entry[1].([]byte)
		if err := tw.WriteHeader(&tar.Header{Name: entry[0].(string), Size: int64(len(raw)), Mode: 0o644, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(raw)
	}
	tw.Close()
	gz.Close()
	archive := packed.Bytes()
	public := map[string]any{"success": true, "id": identity, "deployment_id": reverse(identity), "agent_id": "manual",
		"profile_id": "default", "service_type": "mihomo", "expires_at": time.Now().Unix() + 3600, "content": string(content),
		"config_path": "config.yaml", "files": files, "target_ids": []any{}, "subscription_hosts": nil, "subscription_dialers": nil}
	record := map[string]any{}
	for _, key := range []string{"id", "deployment_id", "agent_id", "profile_id", "service_type", "expires_at"} {
		record[key] = public[key]
	}
	record["bundle_meta"] = map[string]any{"manifest": manifest, "sha256": sha(archive)}
	folder := filepath.Join(root, ".generated-previews", identity)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	recordBytes, _ := json.Marshal(record)
	os.WriteFile(filepath.Join(folder, "record.json"), recordBytes, 0o600)
	os.WriteFile(filepath.Join(folder, "bundle.tar.gz"), archive, 0o600)
	return artifactFixture{public: public, folder: folder}
}

type artifactCase struct {
	t     *testing.T
	roots []string
	a, b  artifactFixture
}

func newArtifactCase(t *testing.T) *artifactCase {
	dir := t.TempDir()
	c := &artifactCase{t: t, roots: []string{filepath.Join(dir, "base"), filepath.Join(dir, "go")}}
	c.a = newArtifact(t, c.roots[0], strings.Repeat("a", 31)+"1", []byte("dns: {enable: true}\n"), []byte("example.com\n"))
	c.b = newArtifact(t, c.roots[1], strings.Repeat("b", 31)+"2", []byte("dns:\n  enable: true\n"), []byte("example.com\n"))
	return c
}

func (c *artifactCase) compare(a, b map[string]any, route string) []diff {
	c.t.Helper()
	proofs := newArtifactProofs(c.roots)
	found, err := testComparator(c.t).responseDiffs(jrep(c.t, a), jrep(c.t, b), route, nil, nil, proofs)
	if err != nil {
		c.t.Fatal(err)
	}
	return found
}

func clone(m map[string]any, edit func(map[string]any)) map[string]any {
	data, _ := json.Marshal(m)
	var out map[string]any
	json.Unmarshal(data, &out)
	if edit != nil {
		edit(out)
	}
	return out
}

func TestArtifactSemanticContentWithProvenIndependentIDsAndHashes(t *testing.T) {
	c := newArtifactCase(t)
	assertDiffs(t, c.compare(c.a.public, c.b.public, artifactRoute), nil)
	if len(c.compare(c.a.public, c.b.public, "/api/other")) == 0 {
		t.Fatal("other route proved")
	}
}

func TestArtifactRootSequenceYAMLUsesManifestFormat(t *testing.T) {
	dir := t.TempDir()
	roots := []string{filepath.Join(dir, "base"), filepath.Join(dir, "go")}
	a := newArtifact(t, roots[0], strings.Repeat("a", 31)+"1", []byte("- a\n- b\n"), []byte("x\n"))
	b := newArtifact(t, roots[1], strings.Repeat("b", 31)+"2", []byte("[a, b]\n"), []byte("x\n"))
	c := &artifactCase{t: t, roots: roots, a: a, b: b}
	assertDiffs(t, c.compare(a.public, b.public, artifactRoute), nil)
}

func TestArtifactPublicDigestCannotBeNormalizedWithoutActualBytes(t *testing.T) {
	c := newArtifactCase(t)
	forged := clone(c.b.public, func(m map[string]any) { m["files"].([]any)[0].(map[string]any)["sha256"] = strings.Repeat("0", 64) })
	if found := c.compare(c.a.public, forged, artifactRoute); !contains(found, diff{"body.files", "invalid-artifact-proof-go"}) {
		t.Fatal(found)
	}
}

func TestArtifactForeignExpiredAndCorruptRecordsFailProof(t *testing.T) {
	edits := map[string]func(c *artifactCase) map[string]any{
		"foreign": func(c *artifactCase) map[string]any {
			return clone(c.b.public, func(m map[string]any) { m["agent_id"] = "agent-a" })
		},
		"expired": func(c *artifactCase) map[string]any {
			return clone(c.b.public, func(m map[string]any) { m["expires_at"] = time.Now().Unix() + 10 })
		},
		"corrupt": func(c *artifactCase) map[string]any {
			os.WriteFile(filepath.Join(c.b.folder, "bundle.tar.gz"), []byte("broken"), 0o600)
			return c.b.public
		},
		"record-foreign": func(c *artifactCase) map[string]any {
			editRecord(t, c.b.folder, "agent_id", "foreign")
			return c.b.public
		},
		"record-expired": func(c *artifactCase) map[string]any {
			editRecord(t, c.b.folder, "expires_at", 1)
			return c.b.public
		},
		"targets": func(c *artifactCase) map[string]any {
			return clone(c.b.public, func(m map[string]any) { m["target_ids"] = []any{"x"} })
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			c := newArtifactCase(t)
			if found := c.compare(c.a.public, edit(c), artifactRoute); !contains(found, diff{"body.files", "invalid-artifact-proof-go"}) {
				t.Fatal(found)
			}
		})
	}
}

func TestArtifactAuxiliaryBytesAreComparedAfterIntegrityValidation(t *testing.T) {
	dir := t.TempDir()
	roots := []string{filepath.Join(dir, "base"), filepath.Join(dir, "go")}
	a := newArtifact(t, roots[0], strings.Repeat("a", 31)+"1", []byte("dns: {}\n"), []byte("example.com\n"))
	b := newArtifact(t, roots[1], strings.Repeat("b", 31)+"2", []byte("dns: {}\n"), []byte("changed.example\n"))
	c := &artifactCase{t: t, roots: roots, a: a, b: b}
	found := c.compare(a.public, b.public, artifactRoute)
	if len(found) == 0 {
		t.Fatal("auxiliary change hidden")
	}
	for _, d := range found {
		if strings.Contains(d.Kind, "invalid-artifact-proof") {
			t.Fatal(found)
		}
	}
}

func TestArtifactMainConfigSemanticDifferenceIsReported(t *testing.T) {
	dir := t.TempDir()
	roots := []string{filepath.Join(dir, "base"), filepath.Join(dir, "go")}
	a := newArtifact(t, roots[0], strings.Repeat("a", 31)+"1", []byte("dns: {enable: true}\n"), []byte("x\n"))
	b := newArtifact(t, roots[1], strings.Repeat("b", 31)+"2", []byte("dns: {enable: false}\n"), []byte("x\n"))
	c := &artifactCase{t: t, roots: roots, a: a, b: b}
	found := c.compare(a.public, b.public, artifactRoute)
	if !contains(found, diff{"body.content.dns.enable", "value"}) {
		t.Fatal(found)
	}
}

func TestArtifactCachedProofRejectsAnUnstablePublicRecord(t *testing.T) {
	c := newArtifactCase(t)
	proofs := newArtifactProofs(c.roots)
	cmp := testComparator(t)
	first, err := cmp.responseDiffs(jrep(t, c.a.public), jrep(t, c.b.public), artifactRoute, nil, nil, proofs)
	if err != nil || len(first) != 0 {
		t.Fatal(first, err)
	}
	changed := clone(c.b.public, func(m map[string]any) { m["service_type"] = "surge" })
	found, err := cmp.responseDiffs(jrep(t, c.a.public), jrep(t, changed), artifactRoute, nil, nil, proofs)
	if err != nil || !contains(found, diff{"body.files", "invalid-artifact-proof-go"}) {
		t.Fatal(found, err)
	}
}

func editRecord(t *testing.T, folder, key string, value any) {
	t.Helper()
	path := filepath.Join(folder, "record.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record[key] = value
	data, _ = json.Marshal(record)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
