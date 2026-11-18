package ippresence

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type presenceCacheReference struct {
	Index        int    `json:"index"`
	PythonOutput bool   `json:"python_output"`
	NativeOutput *bool  `json:"native_output"`
	Kind         string `json:"kind"`
}

func presenceCacheReferences(t *testing.T, path string) map[string]presenceCacheReference {
	t.Helper()
	data, err := os.ReadFile("testdata/n89-unowned-cache.json")
	if err != nil {
		t.Fatal(err)
	}
	var refs map[string]struct {
		ArchiveSHA256 string                            `json:"archive_sha256"`
		Rows          map[string]presenceCacheReference `json:"rows"`
	}
	if err := json.Unmarshal(data, &refs); err != nil {
		t.Fatal(err)
	}
	ref, ok := refs[filepath.Base(path)]
	if !ok {
		t.Fatal("missing exact presence D2 archive")
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != ref.ArchiveSHA256 {
		t.Fatal("presence D2 archive changed")
	}
	return ref.Rows
}
