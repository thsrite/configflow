package rules

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cacheExpectedRow struct {
	Input       json.RawMessage `json:"input"`
	Original    string          `json:"original"`
	PythonTyped string          `json:"python_typed"`
	Native      string          `json:"native"`
	Class       string          `json:"class"`
}
type cacheExpected struct {
	Policies      []cacheExpectedRow `json:"policies"`
	ReviewSHA256  string             `json:"review_sha256"`
	ArchiveSHA256 string             `json:"archive_sha256"`
	Rows          []cacheExpectedRow `json:"rows"`
	Namespace     []cacheExpectedRow `json:"namespace"`
}

func cacheReference(t *testing.T) cacheExpected {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_cache_expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected cacheExpected
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile("testdata/corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != expected.ArchiveSHA256 {
		t.Fatal("cache archive changed without re-recording real Python reference")
	}
	return expected
}
func assertCacheReferenceInput(t *testing.T, expected cacheExpectedRow, rule *jsonx.Object, original any) {
	t.Helper()
	input, err := jsonx.Parse(expected.Input)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonx.Equal(input, rule) || expected.Original != original {
		t.Fatal("reference row input/original drift")
	}
	switch expected.Class {
	case "unchanged":
		if expected.Native != expected.Original {
			t.Fatal("unchanged reference changed")
		}
	case "R5", "D2", "R5+D2":
	default:
		t.Fatalf("unreviewed reference class %q", expected.Class)
	}
}

func BenchmarkMatcherProjectionUnrelatedConfig(b *testing.B) {
	for _, size := range []int{0, 20000} {
		b.Run(fmt.Sprintf("nodes_%d", size), func(b *testing.B) {
			config := jsonx.Obj("rule_configs", []any{jsonx.Obj("itemType", "ruleset", "library_rule_id", "r")}, "rule_library", []any{jsonx.Obj("id", "r", "url", "https://example.com/list.yaml")})
			nodes := make([]any, size)
			for i := range nodes {
				nodes[i] = jsonx.Obj("name", strings.Repeat("n", 1024))
			}
			config.Set("nodes", nodes)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				input := MatcherInputFromJSON(config)
				if len(input.Library) != 1 {
					b.Fatal("missing declaration")
				}
			}
		})
	}
}
func TestMatcherProjectionAllocationsIgnoreUnrelatedConfig(t *testing.T) {
	small := jsonx.Obj("rule_configs", []any{jsonx.Obj("id", "r", "itemType", "rule", "rule_type", "MATCH")})
	large := small.Clone()
	large.Set("nodes", []any{jsonx.Obj("params", strings.Repeat("x", 4<<20))})
	empty := testing.AllocsPerRun(10, func() { _ = MatcherInputFromJSON(small) })
	huge := testing.AllocsPerRun(10, func() { _ = MatcherInputFromJSON(large) })
	if huge > empty+1 {
		t.Fatalf("unrelated config allocated: small=%g large=%g", empty, huge)
	}
}
func TestNativeMatcherOwnsHitCopy(t *testing.T) {
	cache := NewCache(CacheOptions{RulesDir: func() string { return t.TempDir() }})
	t.Cleanup(cache.Clear)
	matcher := cache.LocalMatcher(MatcherInput{Rules: []model.RuleConfig{{ID: "r", RuleType: "DOMAIN-SUFFIX", Value: "example.com"}}}, "p")
	first := matcher.Match("example.com")
	if first == nil {
		t.Fatal("missing hit")
	}
	first.Policy = "changed"
	*first.RuleID = "changed"
	second := matcher.Match("example.com")
	if second.Policy != "DIRECT" || *second.RuleID != "r" {
		t.Fatal("borrowed cached result")
	}
}

func TestNativeCacheReadPathOwnsBorrowedSnapshot(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "validated.list")
	if err := os.WriteFile(path, []byte("example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var rule model.RuleConfig
	if err := json.Unmarshal([]byte(`{"id":"r","name":"Synthetic","itemType":"ruleset","source_type":"url","url":"https://example.com/rules","behavior":"domain","format":"text","policy":"DIRECT"}`), &rule); err != nil {
		t.Fatal(err)
	}
	reads := 0
	cache := NewCache(CacheOptions{RulesDir: func() string { return directory }, StoredLibrary: func() []model.RuleLibraryItem { t.Fatal("native read path copied the entire library"); return nil }, ReadPath: func(received model.RuleConfig, _ []model.RuleLibraryItem, root string) string {
		reads++
		if received.ID != "r" || root != directory {
			t.Fatal("lost declaration or namespace")
		}
		return path
	}})
	t.Cleanup(cache.Clear)
	matcher := cache.LocalMatcher(MatcherInput{Rules: []model.RuleConfig{rule}}, "default")
	hit := matcher.Match("example.com")
	if hit == nil || hit.Policy != "DIRECT" || reads < 2 {
		t.Fatalf("native path not used by signature and lazy load: hit=%v reads=%d", hit, reads)
	}
}
