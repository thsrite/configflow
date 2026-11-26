package yamlxtest

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"testing"
)

// Only this package's full replay decodes the complete historical archive.
// Callers embed the small index and keep all of their existing selected inputs.
//
//go:embed testdata/n73_frozen.json.gz
var frozenBytes []byte

type frozenReader struct {
	Source      string      `json:"source"`
	Category    string      `json:"category"`
	Native      []ParseNode `json:"native"`
	NativeError string      `json:"native_error"`
	Unknown     bool        `json:"unknown"`
	Revision    string      `json:"revision"`
}

// Every frozen source must execute even when no current caller selects it.
// Stream one record at a time, avoiding a second full in-memory graph archive.
func TestAllFrozenReaderSources(t *testing.T) {
	if fmt.Sprintf("%x", sha256.Sum256(frozenBytes)) != frozenArchiveSHA {
		t.Fatal("historical YAML comparison archive changed")
	}
	index, err := frozenSummaries()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(frozenBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		t.Fatalf("historical YAML archive header: %v %v", token, err)
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		id, ok := token.(string)
		if !ok || seen[id] {
			t.Fatalf("invalid or duplicate frozen source ID: %v", token)
		}
		seen[id] = true
		var row frozenReader
		if err := decoder.Decode(&row); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256([]byte(row.Source))) != id || row.Unknown || row.Category == "" || row.Revision != frozenRevision {
			t.Fatalf("invalid frozen source metadata %s", id)
		}
		digest, err := auditSHA(row.Native)
		if err != nil {
			t.Fatal(err)
		}
		if summary, ok := index[id]; !ok || summary != (frozenSummary{Category: row.Category, NativeSHA: digest, NativeError: row.NativeError}) {
			t.Fatalf("historical index differs from full source record %s", id)
		}
		category, err := Classify(row.Source)
		if err != nil {
			t.Errorf("frozen source %s: %v", id, err)
			continue
		}
		counts[category]++
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		t.Fatalf("historical YAML archive trailer: %v %v", token, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		t.Fatalf("historical YAML archive trailing data: %v", err)
	}
	if len(seen) != frozenCount || len(seen) != len(index) {
		t.Fatalf("historical input count changed: archive=%d index=%d", len(seen), len(index))
	}
	if t.Failed() {
		return
	}
	t.Logf("executed=%d unique historical sources; classes=%v unknown=0", len(seen), counts)
}

func TestFrozenIndexRejectsUnrecordedInput(t *testing.T) {
	if _, err := Classify("n73-gate-perf-unrecorded-source: true"); err == nil {
		t.Fatal("unrecorded input silently acquired a migration category")
	}
}

// Alter valid JSON data rather than gzip bytes: metadata/count stay valid, so
// this control detects a missing integrity check on a derived reference entry.
func TestFrozenIndexRejectsChangedEntry(t *testing.T) {
	var index frozenIndex
	if err := readArchive(frozenIndexBytes, &index); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte("mixed-port: 7890")))
	entry, ok := index.Records[id]
	if !ok {
		t.Fatal("negative-control input missing from the index")
	}
	entry.Category += "-tampered"
	index.Records[id] = entry
	var changed bytes.Buffer
	writer := gzip.NewWriter(&changed)
	if err := json.NewEncoder(writer).Encode(index); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeFrozenIndex(changed.Bytes()); err == nil {
		t.Fatal("tampered derived reference entry was accepted")
	}
}
