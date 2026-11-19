package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
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
		a := RuleCacheFilename(obj("id", "r0", "name", pair[0], "source_type", "content"))
		b := RuleCacheFilename(obj("id", "r1", "name", pair[1], "source_type", "content"))
		if a == b {
			t.Errorf("%v share a cache file", pair)
		}
	}
}

func TestRuleCacheFilenameNamespaces(t *testing.T) {
	cases := []struct {
		rule *jsonx.Object
		want string
	}{
		{obj("id", "r1", "source_type", "content"), "by-id/" + hexOf("r1") + ".list"},
		{obj("id", "r1", "library_rule_id", "lib", "source_type", "content"), "by-id/" + hexOf("lib") + ".list"},
		{obj("name", "Name", "source_type", "content"), "by-name/" + hexOf("Name") + ".list"},
		{obj("source_type", "content"), "by-name/" + hexOf("") + ".list"},
		{obj("id", "r1", "url", "https://x.test/a.yaml", "behavior", "domain", "format", "yaml"),
			"by-source/" + hexOf(`["r1", "https://x.test/a.yaml", "domain", "yaml"]`) + ".list"},
		{obj("id", "r1", "url", "https://x.test/例"), "by-source/" + hexOf(`["r1", "https://x.test/例", "domain", ""]`) + ".list"},
		{obj("id", "r1", "source_type", "url"), "by-id/" + hexOf("r1") + ".list"},
	}
	reference := cacheReference(t)
	for i, c := range cases {
		expected := reference.Namespace[i]
		assertCacheReferenceInput(t, expected, c.rule, c.want)
		if got := RuleCacheFilename(c.rule); got != expected.Native {
			t.Errorf("RuleCacheFilename(%s) = %s, want %s", jsonx.Repr(c.rule), got, expected.Native)
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

func TestRuleCacheReadPathPrefersCurrentIdentityThenSafeLegacyCaches(t *testing.T) {
	root := t.TempDir()
	rule := obj("id", "r1", "name", "Remote", "source_type", "url", "url", "https://rules.test/file", "behavior", "domain")
	stored := seq(rule.DeepCopy())
	current := filepath.Join(root, RuleCacheFilename(rule))
	if got := RuleCacheReadPath(rule, nil, stored, root); got != current {
		t.Fatalf("no cache: %s", got)
	}
	// A legacy identity cache is used for the unchanged stored declaration.
	legacyID := writeCache(t, root, "by-id/"+hexOf("r1")+".list", "old.example")
	if got := RuleCacheReadPath(rule, nil, stored, root); got != legacyID {
		t.Fatalf("legacy id cache not used: %s", got)
	}
	// A proposed source cannot borrow it.
	for key, value := range map[string]any{"url": "https://rules.test/new-source.txt", "behavior": "classical", "format": "yaml"} {
		proposed := rule.Clone()
		proposed.Set(key, value)
		if got := RuleCacheReadPath(proposed, nil, stored, root); got == legacyID {
			t.Errorf("proposed %s borrowed the legacy cache", key)
		}
	}
	// A stale snapshot cannot claim the current source's legacy cache either.
	changed := rule.Clone()
	changed.Set("url", "https://rules.test/current-source.txt")
	if got := RuleCacheReadPath(rule, seq(rule), seq(changed), root); got == legacyID {
		t.Error("stale snapshot claimed the legacy cache")
	}
	// Current-identity cache wins over every legacy file.
	writeCache(t, root, RuleCacheFilename(rule), "current.example")
	if got := RuleCacheReadPath(rule, nil, stored, root); got != current {
		t.Errorf("current cache not preferred: %s", got)
	}
}

func TestRuleCacheReadPathLegacyNameCaches(t *testing.T) {
	root := t.TempDir()
	unique := obj("id", "remote", "name", "Remote", "source_type", "url", "url", "https://rules.test/file")
	library := seq(unique.DeepCopy())
	legacy := writeCache(t, root, "Remote.list", "legacy.test")
	if got := RuleCacheReadPath(unique, library, library, root); got != legacy {
		t.Errorf("unique legacy url cache not used offline: %s", got)
	}
	// Two resources whose names collide must not claim the shared file.
	a := obj("id", "r0", "name", "a:b", "source_type", "content")
	b := obj("id", "r1", "name", "a_b", "source_type", "content")
	both := seq(a, b)
	shared := writeCache(t, root, "a_b.list", "unknown-owner.test")
	for _, rule := range []*jsonx.Object{a, b} {
		if got := RuleCacheReadPath(rule, both, both, root); got == shared {
			t.Errorf("%s read the ambiguous legacy file", rule.GetString("name"))
		}
	}
	// An unknown spelling of a known name is not attributed either.
	other := obj("id", "r9", "name", "a?b", "source_type", "content")
	if got := RuleCacheReadPath(other, seq(a), seq(a), root); got == filepath.Join(root, "a_b.list") {
		t.Error("an unknown spelling claimed the legacy file")
	}
}

func TestLegacyIdentityCacheDoesNotTreatR5CoercionAsOwnership(t *testing.T) {
	root := t.TempDir()
	current := obj("id", "r", "name", "Synthetic", "url", jsonx.Int(5), "source_type", "url", "behavior", "domain")
	proposed := current.DeepCopy()
	proposed.Set("url", "5")
	old := writeCache(t, root, "by-id/"+hexOf("r")+".list", "old.example")
	if got := RuleCacheReadPath(proposed, seq(current), seq(current), root); got == old {
		t.Fatal("R5-equivalent URL claimed old identity bytes")
	}
}

func ValidateRulePolicies(config *jsonx.Object, names []string, format string) error {
	var records []model.RuleConfig
	for _, entry := range config.GetList("rule_configs") {
		var rule model.RuleConfig
		if decodeRecord(entry, &rule) == nil {
			records = append(records, rule)
		}
	}
	return ValidatePolicies(records, names, format)
}
