package convert_test

import (
	"compress/gzip"
	"configflow/internal/yamlx/yamlxtest"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// N7-4 changes the YAML writer once. Compare both complete outputs through the
// same parser; formatting may differ, but tags, values and list order may not.
type writerOracle map[string]*yamlx.Node

func loadWriterOracle(t *testing.T) writerOracle {
	t.Helper()
	refs := writerReferences(t)
	oracle := writerOracle{}
	for _, ref := range refs {
		n, err := ref.Document()
		if err != nil {
			t.Fatal(err)
		}
		if old := oracle[ref.SHA256]; old != nil && !yamlx.Equal(old, n) {
			t.Fatal("identical old output has inconsistent Python reference graphs")
		}
		oracle[ref.SHA256] = n
	}
	return oracle
}

func writerReferences(t *testing.T) []yamlxtest.WriterReference {
	t.Helper()
	f, err := os.Open("testdata/mihomo_writer_oracle.json.gz")
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
	return refs
}

func (oracle writerOracle) compare(want, got string) string {
	a, err := yamlx.Parse([]byte(want))
	if err != nil {
		return fmt.Sprintf("Python YAML cannot be parsed: %v", err)
	}
	if ref := oracle[fmt.Sprintf("%x", sha256.Sum256([]byte(want)))]; ref != nil {
		if yamlx.Equal(a, ref) {
			return "stale Python writer reference: native parsing now agrees"
		}
		a = ref
	}
	b, err := yamlx.Parse([]byte(got))
	if err != nil {
		return fmt.Sprintf("Go YAML cannot be parsed: %v", err)
	}
	if !yamlx.Equal(a, b) {
		return "YAML semantics differ: " + writerDifference(a, b, "$", 0) + "\n" + firstDiff(want, got)
	}
	return ""
}

// writerDifference locates a structural mismatch; textual quoting differences can
// precede the actual failure and are not useful diagnostics by themselves.
func writerDifference(a, b *yamlx.Node, path string, depth int) string {
	a, b = yamlx.Resolve(a), yamlx.Resolve(b)
	if yamlx.Equal(a, b) {
		return ""
	}
	if a == nil || b == nil {
		return path + ": missing value"
	}
	if a.Kind != b.Kind || a.ShortTag() != b.ShortTag() {
		return fmt.Sprintf("%s: %s %q vs %s %q", path, a.ShortTag(), a.Value, b.ShortTag(), b.Value)
	}
	if depth < 32 {
		switch a.ShortTag() {
		case "!!map":
			for key, value := range yamlx.Pairs(a) {
				if got := yamlx.Get(b, key); !yamlx.Equal(value, got) {
					return writerDifference(value, got, path+"."+key, depth+1)
				}
			}
			for key := range yamlx.Pairs(b) {
				if yamlx.Get(a, key) == nil {
					return path + "." + key + ": extra value"
				}
			}
		case "!!seq":
			if len(a.Content) != len(b.Content) {
				return fmt.Sprintf("%s: length %d vs %d", path, len(a.Content), len(b.Content))
			}
			for i, value := range a.Content {
				if !yamlx.Equal(value, b.Content[i]) {
					return writerDifference(value, b.Content[i], fmt.Sprintf("%s[%d]", path, i), depth+1)
				}
			}
		}
	}
	return fmt.Sprintf("%s: %s %q vs %s %q", path, a.ShortTag(), a.Value, b.ShortTag(), b.Value)
}

func TestGeneratedYAMLComparison(t *testing.T) {
	for _, tc := range []struct {
		name, want, got string
		equal           bool
	}{
		{"layout", "names:\n- 香港\n- Tokyo\nflag: true\n", "{flag: true, names: [香港, Tokyo]}", true},
		{"string-not-number", "port: '80'", "port: 80", false},
		{"bool-not-number", "port: true", "port: 1", false},
		{"missing-field", "a: 1\nb: 2", "a: 1", false},
		{"list-order", "a: [one, two]", "a: [two, one]", false},
		{"null-not-empty-list", "a: null", "a: []", false},
		{"duplicate-key", "a: 1", "a: 1\na: 2", false},
		{"invalid-expected", "a: [", "a: []", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if msg := writerOracle(nil).compare(tc.want, tc.got); (msg == "") != tc.equal {
				t.Fatalf("equal=%v: %s", tc.equal, msg)
			}
		})
	}
}

func TestMihomoWriterOracleProvenance(t *testing.T) {
	refs := writerReferences(t)
	if len(refs) != 111 {
		t.Fatalf("expected 111 explicitly classified Python writer bugs, got %d", len(refs))
	}
	corpora := map[string][]*jsonx.Object{}
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, ref := range refs {
		if corpora[ref.Corpus] == nil {
			corpora[ref.Corpus] = loadJSONLines(t, "testdata/"+ref.Corpus)
		}
		rows := corpora[ref.Corpus]
		if ref.Row < 0 || ref.Row >= len(rows) {
			t.Fatalf("invalid source row: %s:%d", ref.Corpus, ref.Row)
		}
		source := rows[ref.Row].GetString("out")
		if digest := fmt.Sprintf("%x", sha256.Sum256([]byte(source))); digest != ref.SHA256 {
			t.Fatalf("Python oracle source changed: %s:%d", ref.Corpus, ref.Row)
		}
		id := fmt.Sprintf("%s:%d", ref.Corpus, ref.Row)
		if seen[id] {
			t.Fatalf("duplicate oracle row %s", id)
		}
		seen[id] = true
		n, err := ref.Document()
		if err != nil {
			t.Fatal(err)
		}
		old, err := yamlx.Parse([]byte(source))
		if err != nil || yamlx.Equal(old, n) {
			t.Fatalf("stale/invalid oracle %s: %v", id, err)
		}
		categories := map[string]bool{}
		for _, change := range ref.Changes {
			categories[change.Category] = true
			if change.Path == "" {
				t.Fatalf("missing field path: %s", id)
			}
		}
		if len(categories) == 0 {
			t.Fatalf("unclassified writer difference: %s", id)
		}
		for category := range categories {
			counts[category]++
		}
	}
	if counts["string-read-as-number"] != 56 || counts["large-int-read-as-float"] != 22 || counts["timestamp-read-as-string"] != 33 || len(counts) != 3 {
		t.Fatalf("writer bug classifications changed: %v", counts)
	}
}
