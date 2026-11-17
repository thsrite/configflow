package difftest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

type writerHTTPRecord struct {
	Path         string                   `json:"path"`
	OraclePath   string                   `json:"oracle_path"`
	OldSHA256    string                   `json:"old_sha256"`
	Status       int                      `json:"status"`
	ContentType  string                   `json:"content_type"`
	ExpectedYAML string                   `json:"expected_yaml"`
	Changes      []yamlxtest.WriterChange `json:"changes"`
	Calls        []struct{ Graph string } `json:"calls"`
}

type writerHTTPFixture struct {
	Records []writerHTTPRecord `json:"records"`
	Graphs  map[string]struct {
		Nodes []yamlxtest.WriterNode `json:"nodes"`
		YAML  string                 `json:"yaml"`
	} `json:"graphs"`
	Unknown      int    `json:"unknown"`
	SourceSHA256 string `json:"source_sha256"`
}

var writerHTTPReferences = sync.OnceValues(func() (writerHTTPFixture, error) {
	data, err := os.ReadFile("testdata/n74_writer_http_expected.json")
	var result writerHTTPFixture
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	return result, err
})

// nativeWriterHTTPExpected is an explicit expectation for four recorded NEL
// requests. The real Flask answer's digest must match before using the fixture.
func nativeWriterHTTPExpected(t *testing.T, path string, old response) (response, bool) {
	t.Helper()
	fixture, err := writerHTTPReferences()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range fixture.Records {
		if record.Path != path {
			continue
		}
		sum := sha256.Sum256(old.body)
		if hex.EncodeToString(sum[:]) != record.OldSHA256 || old.status != record.Status || old.header.Get("Content-Type") != record.ContentType {
			t.Fatal("recorded NEL writer source no longer matches real Flask")
		}
		old.body = []byte(record.ExpectedYAML)
		return old, true
	}
	return old, false
}

func TestNativeWriterHTTPProvenance(t *testing.T) {
	fixture, err := writerHTTPReferences()
	if err != nil || fixture.Unknown != 0 || len(fixture.Records) != 4 {
		t.Fatalf("invalid HTTP writer oracle: %v", err)
	}
	data, err := os.ReadFile("testdata/n74_writer_http_inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != fixture.SourceSHA256 {
		t.Fatal("HTTP writer source input fixture changed")
	}
	seen := map[string]bool{}
	for _, record := range fixture.Records {
		path, changed := nativeQueryVariant(record.Path)
		if !changed {
			path = record.Path
		}
		if seen[path] || path != record.OraclePath || len(record.Calls) != 1 || len(record.Changes) == 0 {
			t.Fatal("invalid HTTP source, boundary input or writer call count")
		}
		seen[path] = true
		graph, ok := fixture.Graphs[record.Calls[0].Graph]
		if !ok {
			t.Fatal("HTTP writer graph was not recorded")
		}
		ref := yamlxtest.WriterReference{Nodes: graph.Nodes}
		n, err := ref.Document()
		if err == nil {
			n, err = yamlbridge.Encode(n)
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := yamlx.Marshal(n)
		if err != nil || string(data) != graph.YAML || graph.YAML != record.ExpectedYAML {
			t.Fatalf("cannot reproduce full native Flask response: %v", err)
		}
		for _, change := range record.Changes {
			if change.Category != "unicode-line-separator-preservation" || len(change.Restored) != 1 || change.Restored["U+0085"] <= 0 {
				t.Fatal("HTTP writer change is not an explicitly recorded NEL fix")
			}
		}
	}
}
