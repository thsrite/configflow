package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

// rule_utils' pure parts. Python covers them through the remote cache and
// repository tests (test_remote_rule_cache_edges.py, test_rule_cache_identity.py,
// test_split_layout_migration.py), which need packages #2 and #12; the cases
// of those tests that only depend on path resolution are here, and
// TestCorpusCacheNames compares file names with Python's.

func hexOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func strp(s string) *string { return &s }

func TestSanitizeRuleName(t *testing.T) {
	for in, want := range map[string]string{
		"": "unnamed", "Rule": "Rule", "a/b": "a_b", `a\b`: "a_b", `a:b*c?d"e<f>g|h`: "a_b_c_d_e_f_g_h",
		". leading": "leading", "trailing. ": "trailing", "  ": "unnamed", "...": "unnamed",
		strings.Repeat("x", 250): strings.Repeat("x", 200), strings.Repeat("例", 250): strings.Repeat("例", 200),
	} {
		if got := SanitizeRuleName(in); got != want {
			t.Errorf("SanitizeRuleName(%.20q) = %.20q, want %.20q", in, got, want)
		}
	}
	// Colliding legacy names must still get distinct cache identities.
	for _, pair := range [][2]string{{"a:b", "a_b"}, {strings.Repeat("x", 201), strings.Repeat("x", 200) + "y"}, {"a.", "a"}} {
		if SanitizeRuleName(pair[0]) != SanitizeRuleName(pair[1]) {
			t.Errorf("%v do not collide as legacy names", pair)
		}
		a := CacheFilename(model.RuleConfig{ID: "r0", Name: pair[0], SourceType: strp("content")})
		b := CacheFilename(model.RuleConfig{ID: "r1", Name: pair[1], SourceType: strp("content")})
		if a == b {
			t.Errorf("%v share a cache file", pair)
		}
	}
}

func TestRuleCacheFilenameNamespaces(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"id":"r1","source_type":"content"}`, "by-id/" + hexOf("r1") + ".list"},
		{`{"id":"r1","library_rule_id":"lib","source_type":"content"}`, "by-id/" + hexOf("lib") + ".list"},
		{`{"name":"Name","source_type":"content"}`, "by-name/" + hexOf("Name") + ".list"},
		{`{"source_type":"content"}`, "by-name/" + hexOf("") + ".list"},
		{`{"id":"r1","url":"https://x.test/a.yaml","behavior":"domain","format":"yaml"}`,
			"by-source/" + hexOf(`["r1", "https://x.test/a.yaml", "domain", "yaml"]`) + ".list"},
		{`{"id":"r1","url":"https://x.test/例"}`, "by-source/" + hexOf(`["r1", "https://x.test/例", "domain", ""]`) + ".list"},
		{`{"id":"r1","source_type":"url"}`, "by-id/" + hexOf("r1") + ".list"},
	}
	reference := cacheReference(t)
	for i, c := range cases {
		expected := reference.Namespace[i]
		assertCacheReferenceInput(t, expected, json.RawMessage(c.raw), c.want)
		var rule model.RuleConfig
		_ = jsondecode.Unmarshal([]byte(c.raw), &rule)
		if got := CacheFilename(rule); got != expected.Native {
			t.Errorf("CacheFilename(%s) = %s, want %s", c.raw, got, expected.Native)
		}
	}
}

func writeCache(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func ruleLibItem(r model.RuleConfig) model.RuleLibraryItem {
	return model.RuleLibraryItem{
		ID: r.ID, Name: r.Name, SourceType: r.SourceType, URL: r.URL,
		Behavior: r.Behavior, Format: r.Format, Content: r.Content, Enabled: r.Enabled,
	}
}

func TestRuleCacheReadPathPrefersCurrentIdentityThenSafeLegacyCaches(t *testing.T) {
	root := t.TempDir()
	rule := model.RuleConfig{
		ID: "r1", Name: "Remote", SourceType: strp("url"),
		URL: strp("https://rules.test/file"), Behavior: strp("domain"),
	}
	stored := []model.RuleLibraryItem{ruleLibItem(rule)}
	current := filepath.Join(root, CacheFilename(rule))
	if got := CacheReadPath(rule, nil, stored, root); got != current {
		t.Fatalf("no cache: %s", got)
	}
	// A legacy identity cache is used for the unchanged stored declaration.
	legacyID := writeCache(t, root, "by-id/"+hexOf("r1")+".list", "old.example")
	if got := CacheReadPath(rule, nil, stored, root); got != legacyID {
		t.Fatalf("legacy id cache not used: %s", got)
	}
	// A proposed source cannot borrow it.
	for _, prop := range []struct {
		key  string
		rule model.RuleConfig
	}{
		{"url", model.RuleConfig{ID: "r1", Name: "Remote", SourceType: strp("url"), URL: strp("https://rules.test/new-source.txt"), Behavior: strp("domain")}},
		{"behavior", model.RuleConfig{ID: "r1", Name: "Remote", SourceType: strp("url"), URL: strp("https://rules.test/file"), Behavior: strp("classical")}},
		{"format", model.RuleConfig{ID: "r1", Name: "Remote", SourceType: strp("url"), URL: strp("https://rules.test/file"), Behavior: strp("domain"), Format: strp("yaml")}},
	} {
		if got := CacheReadPath(prop.rule, nil, stored, root); got == legacyID {
			t.Errorf("proposed %s borrowed the legacy cache", prop.key)
		}
	}
	// A stale snapshot cannot claim the current source's legacy cache either.
	changed := ruleLibItem(rule)
	changed.URL = strp("https://rules.test/current-source.txt")
	if got := CacheReadPath(rule, stored, []model.RuleLibraryItem{changed}, root); got == legacyID {
		t.Error("stale snapshot claimed the legacy cache")
	}
	// Current-identity cache wins over every legacy file.
	writeCache(t, root, CacheFilename(rule), "current.example")
	if got := CacheReadPath(rule, nil, stored, root); got != current {
		t.Errorf("current cache not preferred: %s", got)
	}
}

func TestRuleCacheReadPathLegacyNameCaches(t *testing.T) {
	root := t.TempDir()
	unique := model.RuleConfig{ID: "remote", Name: "Remote", SourceType: strp("url"), URL: strp("https://rules.test/file")}
	library := []model.RuleLibraryItem{ruleLibItem(unique)}
	legacy := writeCache(t, root, "Remote.list", "legacy.test")
	if got := CacheReadPath(unique, library, library, root); got != legacy {
		t.Errorf("unique legacy url cache not used offline: %s", got)
	}
	// Two resources whose names collide must not claim the shared file.
	a := model.RuleConfig{ID: "r0", Name: "a:b", SourceType: strp("content")}
	b := model.RuleConfig{ID: "r1", Name: "a_b", SourceType: strp("content")}
	both := []model.RuleLibraryItem{ruleLibItem(a), ruleLibItem(b)}
	shared := writeCache(t, root, "a_b.list", "unknown-owner.test")
	for _, rule := range []model.RuleConfig{a, b} {
		if got := CacheReadPath(rule, both, both, root); got == shared {
			t.Errorf("%s read the ambiguous legacy file", rule.Name)
		}
	}
	// An unknown spelling of a known name is not attributed either.
	other := model.RuleConfig{ID: "r9", Name: "a?b", SourceType: strp("content")}
	if got := CacheReadPath(other, []model.RuleLibraryItem{ruleLibItem(a)}, []model.RuleLibraryItem{ruleLibItem(a)}, root); got == filepath.Join(root, "a_b.list") {
		t.Error("an unknown spelling claimed the legacy file")
	}
}

// TestLegacyIdentityCacheStoredNumberURL: library items are plain records
// (O18 T1c), so a URL stored as the number 5 reads as the text "5" and is the
// same declaration as a proposed "5"; the old identity cache is reused. R5
// kept the number and refused that ownership (accepted O18 T1c difference).
func TestLegacyIdentityCacheStoredNumberURL(t *testing.T) {
	root := t.TempDir()
	var current model.RuleLibraryItem
	_ = jsondecode.Unmarshal([]byte(`{"id":"r","name":"Synthetic","url":5,"source_type":"url","behavior":"domain"}`), &current)
	var proposed model.RuleConfig
	_ = jsondecode.Unmarshal([]byte(`{"id":"r","name":"Synthetic","url":"5","source_type":"url","behavior":"domain"}`), &proposed)
	old := writeCache(t, root, "by-id/"+hexOf("r")+".list", "old.example")
	if got := CacheReadPath(proposed, []model.RuleLibraryItem{current}, []model.RuleLibraryItem{current}, root); got != old {
		t.Fatalf("stored number URL: read path %q, want the identity cache %q", got, old)
	}
	var changed model.RuleConfig
	_ = jsondecode.Unmarshal([]byte(`{"id":"r","name":"Synthetic","url":"6","source_type":"url","behavior":"domain"}`), &changed)
	if got := CacheReadPath(changed, []model.RuleLibraryItem{current}, []model.RuleLibraryItem{current}, root); got == old {
		t.Fatal("a different URL claimed the old identity cache")
	}
}
