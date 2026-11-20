package repo

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// xcBindNativeCachePath adjusts one exact source address, retaining strict
// directory membership and comparing the complete bytes of both cache files.
func xcBindNativeCachePath(t *testing.T, source, goDir, pyDir string, layout *testObj) {
	t.Helper()
	data := must(os.ReadFile("testdata/n89_cache_paths.json"))(t)
	refs := parseJSONObj(t, data)
	ref := refs.GetObject(filepath.Base(source))
	if ref == nil {
		t.Fatal("missing exact D2 repository reference")
	}
	original := must(os.ReadFile(source))(t)
	if fmt.Sprintf("%x", sha256.Sum256(original)) != ref.GetString("source_sha256") {
		t.Fatal("original repository operations changed")
	}
	old, native := ref.GetString("old"), ref.GetString("native")
	oldData := must(os.ReadFile(filepath.Join(pyDir, old)))(t)
	nativeData := must(os.ReadFile(filepath.Join(goDir, native)))(t)
	if !bytes.Equal(oldData, nativeData) {
		t.Fatal("D2 repository cache bytes differ")
	}
	if _, err := os.Stat(filepath.Join(goDir, old)); !os.IsNotExist(err) {
		t.Fatal("native writer created an old cache address", err)
	}
	if _, err := os.Stat(filepath.Join(pyDir, native)); !os.IsNotExist(err) {
		t.Fatal("Python writer created a native cache address", err)
	}
	entries := layout.GetList("result")
	replaced := 0
	for i, entry := range entries {
		if entry == old {
			entries[i] = native
			replaced++
		}
	}
	if replaced != 1 {
		t.Fatal("D2 repository layout reference did not bind exactly one entry")
	}
	layout.Set("result", entries)
}
