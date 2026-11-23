package agents

import (
	"configflow/internal/version"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// nativeVersionReferenceWant changes only the nine explicitly approved machine
// version rows. It binds each independent expectation to the original archive,
// complete row and recorded Python result; every other comparison is unchanged.
func nativeVersionReferenceWant(t *testing.T, index int, rowRaw json.RawMessage, want int) int {
	t.Helper()
	data, err := os.ReadFile("testdata/n810_version_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Archive string `json:"corpus_sha256"`
		Rows    []struct {
			Index  int    `json:"index"`
			SHA    string `json:"sha256"`
			Python int    `json:"python_want"`
			Native int    `json:"native_want"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if reference.Archive != "82281a960707379c8ce0ddc54da84992c81e3820968e55998bf3d7772747368a" || len(reference.Rows) != 9 {
		t.Fatal("unreviewed native version reference")
	}
	archive, err := os.ReadFile("testdata/a02_reference.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(archive)) != reference.Archive {
		t.Fatal("original Python version archive changed", err)
	}
	seen := map[int]bool{}
	for _, r := range reference.Rows {
		if seen[r.Index] || r.Native != 1 {
			t.Fatal("invalid independent version row", r.Index)
		}
		seen[r.Index] = true
		if r.Index != index {
			continue
		}
		raw := compactRawJSON(t, rowRaw)
		if fmt.Sprintf("%x", sha256.Sum256(raw)) != r.SHA || want != r.Python {
			t.Fatalf("original version row %d changed", index)
		}
		want = r.Native
	}
	return want
}

func TestNativeVersionBusinessRules(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.2-go", "1.2.0", 0}, {"1.2", "1.2.1", -1},
		{"1.alpha", "1.beta", -1}, {"1.", "1.0", -1},
		// Spaces and underscores are text (O17); long numbers stay numbers.
		{"1. 02 ", "1.2", -1}, {"1.2_0", "1.20", 1},
		{"1.٣", "1.4", 1}, {strings.Repeat("1", 4301), "2", 1},
	}
	for _, tt := range tests {
		t.Run(tt.a[:min(len(tt.a), 32)]+"/"+tt.b, func(t *testing.T) {
			if got := version.CompareAgent(tt.a, tt.b); got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}

func FuzzNativeVersionComparison(f *testing.F) {
	for _, s := range []string{"1.2.3", "1.2-go", "", "1.٣", "1..2", "1.0_alpha"} {
		f.Add(s, "1.0")
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		ab, ba := version.CompareAgent(a, b), version.CompareAgent(b, a)
		if ab < -1 || ab > 1 || ab != -ba || version.CompareAgent(a, a) != 0 {
			t.Fatalf("invalid comparison %q %q: %d/%d", a, b, ab, ba)
		}
		if version.CompareAgent(a+"-native", b+"-native") != ab {
			t.Fatal("suffix changed comparison")
		}
	})
}
