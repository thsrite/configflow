package yamlx_test

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

// The old writer's style options collapse to the one native canonical writer.
// Its values come from independent real Python safe_load output references.
func TestNativeWriterPythonReferences(t *testing.T) {
	f, err := os.Open("testdata/writer_reference.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	var refs []yamlxtest.WriterReference
	if err := json.NewDecoder(gz).Decode(&refs); err != nil {
		t.Fatal(err)
	}
	corpus, err := os.Open("testdata/corpus.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer corpus.Close()
	type source struct {
		Kind string          `json:"kind"`
		Out  json.RawMessage `json:"out"`
	}
	var rows []source
	s := bufio.NewScanner(corpus)
	s.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for s.Scan() {
		var row source
		if err := json.Unmarshal(s.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	seen := map[int]bool{}
	for _, ref := range refs {
		if ref.Row < 0 || ref.Row >= len(rows) || seen[ref.Row] || ref.Corpus != "corpus.jsonl" {
			t.Fatalf("invalid/duplicate reference row %d", ref.Row)
		}
		seen[ref.Row] = true
		row := rows[ref.Row]
		var old string
		if row.Kind == "dump" {
			var options map[string]string
			if err := json.Unmarshal(row.Out, &options); err != nil {
				t.Fatal(err)
			}
			old = options["safe_u"]
		} else if row.Kind == "spec" {
			if err := json.Unmarshal(row.Out, &old); err != nil {
				t.Fatal(err)
			}
		} else {
			t.Fatalf("reference of non-writer row %d", ref.Row)
		}
		if digest := fmt.Sprintf("%x", sha256.Sum256([]byte(old))); digest != ref.SHA256 {
			t.Fatalf("old Python output changed in row %d", ref.Row)
		}
		counts[row.Kind]++
		t.Run(fmt.Sprintf("%s/%d", row.Kind, ref.Row), func(t *testing.T) {
			want, err := ref.Document()
			if err != nil {
				t.Fatal(err)
			}
			out, err := yamlx.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			got, err := yamlx.Parse(out)
			if err != nil || !yamlx.Equal(want, got) {
				t.Fatalf("native writer changed Python value: %q: %v", out, err)
			}
		})
	}
	if counts["dump"] != 1374 || counts["spec"] != 21 || len(refs) != 1395 {
		t.Fatalf("incomplete Python writer corpus: %v", counts)
	}
}
