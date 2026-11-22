package provider

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"

	"configflow/internal/testjson"
)

func nativeDeliveryReferences(t *testing.T, rows []map[string]any) map[int]map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/n88d_native_reference.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	raw, err = io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ArchiveSHA string `json:"archive_sha256"`
		Rows       map[string]struct {
			Input, Original, Expected json.RawMessage
			Reason                    string
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile("testdata/corpus.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != manifest.ArchiveSHA {
		t.Fatal("original provider archive changed")
	}
	// The recorded rows are replayed as committed; the recorder script and the
	// Python owner module are no longer hashed (the Python reference is retired).
	if len(manifest.Rows) != 79 {
		t.Fatal("native reference matrix changed")
	}
	result := map[int]map[string]any{}
	for key, ref := range manifest.Rows {
		index, err := strconv.Atoi(key)
		if err != nil || index < 0 || index >= len(rows) || ref.Reason == "" {
			t.Fatal("invalid native row", key)
		}
		input, err := decodeAny(ref.Input)
		if err != nil || !testjson.Equal(input, rows[index]["input"]) {
			t.Fatal("native row input changed", key)
		}
		original, err := decodeAny(ref.Original)
		if err != nil || !testjson.Equal(original, expectedOf(rows[index])) {
			t.Fatal("native row original changed", key)
		}
		expected, err := decodeMap(ref.Expected)
		if err != nil {
			t.Fatal(err)
		}
		result[index] = expected
	}
	return result
}
