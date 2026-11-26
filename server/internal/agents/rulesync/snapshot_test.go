package rulesync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/deploy"
)

func snapshotFixture(t testing.TB) (deploy.Manifest, map[string]deploy.Content) {
	t.Helper()
	files := map[string]deploy.Content{"config.yaml": {Data: []byte("rules: []\n"), Role: deploy.RoleConfig}, "rules/a.list": {Data: []byte("example.com\n"), Role: deploy.RoleRule}}
	m := deploy.Manifest{ProtocolVersion: 1, DeploymentID: "release", AgentID: "device", ProfileID: "default", ServiceType: "mihomo", ConfigPath: "config.yaml"}
	for _, p := range []string{"config.yaml", "rules/a.list"} {
		f := files[p]
		m.Files = append(m.Files, deploy.File{Path: p, Role: f.Role, Size: int64(len(f.Data)), SHA256: digest(f.Data)})
	}
	return m, files
}
func snapshotArchive(t testing.TB, m deploy.Manifest, files map[string]deploy.Content, extra *tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tr := tar.NewWriter(gz)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	entries := []struct {
		name string
		data []byte
	}{{"manifest.json", raw}}
	for _, f := range m.Files {
		entries = append(entries, struct {
			name string
			data []byte
		}{"files/" + f.Path, files[f.Path].Data})
	}
	for _, e := range entries {
		if err = tr.WriteHeader(&tar.Header{Name: e.name, Size: int64(len(e.data)), Mode: 0600}); err != nil {
			t.Fatal(err)
		}
		if _, err = tr.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		if err = tr.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
	}
	if err = tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err = gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func TestSnapshotIntegrity(t *testing.T) {
	m, files := snapshotFixture(t)
	tests := []struct {
		name    string
		edit    func(*deploy.Manifest)
		extra   *tar.Header
		version string
		want    string
	}{
		{name: "valid", version: m.Files[0].SHA256},
		{name: "short version", version: m.Files[0].SHA256[:8]},
		{name: "foreign identity", edit: func(m *deploy.Manifest) { m.AgentID = "other" }, version: m.Files[0].SHA256, want: msgSnapshotChanged},
		{name: "bad hash", edit: func(m *deploy.Manifest) { m.Files[1].SHA256 = digest([]byte("other")) }, version: m.Files[0].SHA256, want: msgSnapshotDigest},
		{name: "missing manifest entry", edit: func(m *deploy.Manifest) { m.Files = m.Files[:1] }, version: m.Files[0].SHA256, want: msgSnapshotIncomplete},
		{name: "symlink", extra: &tar.Header{Name: "files/link", Typeflag: tar.TypeSymlink, Linkname: "/tmp/forbidden"}, version: m.Files[0].SHA256, want: msgSnapshotInvalid},
		{name: "traversal", extra: &tar.Header{Name: "files/../config.yaml", Mode: 0600}, version: m.Files[0].SHA256, want: msgSnapshotForeign},
		{name: "duplicate", extra: &tar.Header{Name: "manifest.json", Mode: 0600}, version: m.Files[0].SHA256, want: msgSnapshotInvalid},
		{name: "main changed", version: digest([]byte("different")), want: msgMainChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copy := m
			copy.Files = append([]deploy.File(nil), m.Files...)
			if tt.edit != nil {
				tt.edit(&copy)
			}
			data := snapshotArchive(t, copy, files, tt.extra)
			b, err := parseSnapshot(context.Background(), data, m, files, tt.version)
			if tt.want != "" {
				if err == nil || err.Error() != tt.want {
					t.Fatalf("got %v, want %s", err, tt.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := b.Contents()
			if err != nil || !bytes.Equal(got["rules/a.list"].Data, files["rules/a.list"].Data) {
				t.Fatalf("snapshot files %v", err)
			}
		})
	}
}
func FuzzSnapshotArchive(f *testing.F) {
	m, files := snapshotFixture(f)
	f.Add(snapshotArchive(f, m, files, nil))
	f.Add([]byte("bad gzip"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2<<20 {
			t.Skip()
		}
		_, _ = parseSnapshot(context.Background(), data, m, files, m.Files[0].SHA256)
	})
}
