package mosdns

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/testgolden"
)

type archiveCacheReference struct {
	Index       int    `json:"index"`
	Test        string `json:"test"`
	InputSHA256 string `json:"input_sha256"`
	Kind        string `json:"kind"`
	Error       string `json:"error"`
	Old         string `json:"old"`
	Native      string `json:"native"`
	RetainOld   bool   `json:"retain_old"`
}

func archiveCacheReferences(t *testing.T, rows []*jsonx.Object) map[int]archiveCacheReference {
	t.Helper()
	data, err := testgolden.ReadFile(t, "testdata/n89_archive_cache_expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var references struct {
		ArchiveSHA256 string                  `json:"archive_sha256"`
		Rows          []archiveCacheReference `json:"rows"`
	}
	if err := json.Unmarshal(data, &references); err != nil {
		t.Fatal(err)
	}
	archive, err := testgolden.ReadFile(t, "testdata/c28_recorded.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != references.ArchiveSHA256 {
		t.Fatal("archive reference does not bind the original frozen archive")
	}
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<27)
	var hashes []string
	for scanner.Scan() {
		hashes = append(hashes, fmt.Sprintf("%x", sha256.Sum256(scanner.Bytes())))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	result := map[int]archiveCacheReference{}
	for _, reference := range references.Rows {
		if reference.Index < 0 || reference.Index >= len(rows) || reference.Index >= len(hashes) || reference.InputSHA256 != hashes[reference.Index] || reference.Test != rows[reference.Index].GetString("test") {
			t.Fatal("D2 reference lost its exact original input binding")
		}
		if _, exists := result[reference.Index]; exists {
			t.Fatal("duplicate archive D2 input")
		}
		if reference.Kind != "native_cache" && reference.Kind != "unowned_refusal" {
			t.Fatal("unknown archive D2 boundary")
		}
		result[reference.Index] = reference
	}
	if len(result) != 9 {
		t.Fatal("expected two exact refusals and seven cache transitions")
	}
	return result
}

func archiveExpectedCaches(t *testing.T, row *jsonx.Object, reference archiveCacheReference) map[string]string {
	t.Helper()
	after := map[string]string{}
	for path, value := range row.GetObject("after").All() {
		content, ok := hexBytes(value)
		if !ok {
			t.Fatal("non-byte archive cache reference")
		}
		after[path] = string(content)
	}
	if reference.Kind == "native_cache" {
		content, exists := after[reference.Old]
		if !exists || reference.Old == reference.Native || reference.Native == "" {
			t.Fatal("D2 path transition does not bind an original cache")
		}
		if row.GetObject("world").GetObject("files").Has(reference.Old) != reference.RetainOld {
			t.Fatal("D2 rollback retention differs from the original world")
		}
		if !reference.RetainOld {
			delete(after, reference.Old)
		}
		after[reference.Native] = content
	}
	return after
}
