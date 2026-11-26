package rulefetch

// Ported from backend/test_remote_rule_cache.py (the cases on rule_utils;
// the /api/rule-library/cache and /api/rules/local route cases are deferred
// to C32/C33), plus the legacy-cache read cases of
// test_rule_cache_identity.py / test_remote_rule_cache_edges.py that do not
// need routes.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

func boolp(v bool) *bool { return &v }

func ruleToLibraryItem(rule model.RuleConfig) model.RuleLibraryItem {
	return model.RuleLibraryItem{
		ID:         rule.ID,
		Name:       rule.Name,
		SourceType: rule.SourceType,
		URL:        rule.URL,
		Behavior:   rule.Behavior,
		Format:     rule.Format,
		Content:    rule.Content,
		Enabled:    rule.Enabled,
	}
}

func cacheRepo(t *testing.T, rule *model.RuleConfig) (context.Context, *repo.Repository) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if rule != nil {
		if err := r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
			shared.RuleLibrary = []model.RuleLibraryItem{ruleToLibraryItem(*rule)}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, r
}

func remoteSource() model.RuleConfig {
	return model.RuleConfig{
		ID:         "remote",
		Name:       "远端规则",
		SourceType: strp("url"),
		URL:        strp("https://rules.test/remote.txt"),
		Behavior:   strp("domain"),
		Format:     strp("text"),
		Enabled:    boolp(true),
	}
}

func cachePath(t *testing.T, r *repo.Repository, rule model.RuleConfig) string {
	t.Helper()
	dir, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, rules.CacheFilename(rule))
}

func fetchBytes(raw string) func(context.Context, string) (Content, error) {
	return func(context.Context, string) (Content, error) {
		return Content{Data: []byte(raw)}, nil
	}
}

// test_refresh_rejects_entire_body_keeps_bytes_and_enabled (refresh part)
func TestRefreshRejectsEntireBodyKeepsBytes(t *testing.T) {
	cases := [][3]string{
		{"domain", "+.example.com\n*.test.com\n", "good.test\nDOMAIN-SUFFIX,example.com\n"},
		{"ipcidr", "10.0.0.0/8\n2001:db8::/32\n", "10.0.0.0/8\n+.example.com\n"},
		{"classical", "DOMAIN,example.com\n+.test.com\n", "DOMAIN,example.com\nfull:other.test\n"},
	}
	for _, c := range cases {
		rule := remoteSource()
		rule.Behavior = strp(c[0])
		ctx, r := cacheRepo(t, &rule)
		if _, err := WriteCache(ctx, r, rule, Content{Data: []byte(c[1])}, nil); err != nil {
			t.Fatal(err)
		}
		path := cachePath(t, r, rule)
		before, _ := os.Stat(path)
		_, _, err := RefreshRule(ctx, r, rule, nil, CacheOptions{Fetch: fetchBytes(c[2])})
		var remote *RemoteRuleValidationError
		if !errors.As(err, &remote) || !strings.Contains(err.Error(), "第 2 行") || !strings.Contains(err.Error(), strings.Split(c[2], "\n")[1]) {
			t.Fatal(err)
		}
		value, refreshErr, err := RefreshRule(ctx, r, rule, nil, CacheOptions{Fetch: fetchBytes(c[2]), AllowStale: true})
		if err != nil || string(value) != c[1] || !errors.As(refreshErr, &remote) {
			t.Fatal(string(value), refreshErr, err)
		}
		data, _ := os.ReadFile(path)
		after, _ := os.Stat(path)
		if string(data) != c[1] || !after.ModTime().Equal(before.ModTime()) {
			t.Fatal("cache changed")
		}
	}
}

// test_no_valid_cache_returns_specific_error_and_never_saves_partial_response (rule_utils part)
func TestNoValidCacheNeverSavesPartialResponse(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, &rule)
	_, _, err := RefreshRule(ctx, r, rule, nil, CacheOptions{Fetch: fetchBytes("good.test\nDOMAIN,other.test\n"), AllowStale: true})
	var remote *RemoteRuleValidationError
	if !errors.As(err, &remote) || len(remote.Errors) == 0 || textOr(remote.Errors[0].Before, "") != "DOMAIN,other.test" {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath(t, r, rule)); !os.IsNotExist(err) {
		t.Fatal("cache written")
	}
}

// test_old_invalid_cache_is_never_used_as_fallback (rule_utils part)
func TestOldInvalidCacheIsNeverUsedAsFallback(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, &rule)
	if _, err := r.WriteSharedText(ctx, "rules/"+rules.CacheFilename(rule), []byte("DOMAIN,wrong.test")); err != nil {
		t.Fatal(err)
	}
	if value, err := ReadCache(ctx, r, rule, nil); err != nil || value != nil {
		t.Fatal(value, err)
	}
	if _, _, err := RefreshRule(ctx, r, rule, nil, CacheOptions{Fetch: fetchBytes("DOMAIN-SUFFIX,wrong.test"), AllowStale: true}); err == nil {
		t.Fatal("served invalid cache")
	}
	if data, _ := os.ReadFile(cachePath(t, r, rule)); string(data) != "DOMAIN,wrong.test" {
		t.Fatal(string(data))
	}
}

// test_good_refresh_recovers_and_preserves_original_framing
func TestGoodRefreshPreservesOriginalFraming(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, &rule)
	if _, err := WriteCache(ctx, r, rule, Content{Data: []byte("old.test")}, nil); err != nil {
		t.Fatal(err)
	}
	raw := "\xef\xbb\xbf# source\r\n+.new.test\r\n"
	value, refreshErr, err := RefreshRule(ctx, r, rule, nil, CacheOptions{Fetch: fetchBytes(raw)})
	if err != nil || refreshErr != nil || string(value) != raw {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(cachePath(t, r, rule)); string(data) != raw {
		t.Fatal(string(data))
	}
}

// test_source_identity_prevents_late_old_refresh_overwriting_changed_source
func TestSourceIdentityPreventsLateOldRefresh(t *testing.T) {
	rule := remoteSource()
	old := rule
	rule.URL = strp("https://rules.test/new.txt")
	ctx, r := cacheRepo(t, &rule)
	if _, err := WriteCache(ctx, r, rule, Content{Data: []byte("new.test")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCache(ctx, r, old, Content{Data: []byte("old.test")}, nil); err != nil {
		t.Fatal(err)
	}
	if rules.CacheFilename(rule) == rules.CacheFilename(old) {
		t.Fatal("same name")
	}
	if v, _ := ReadCache(ctx, r, rule, nil); string(v) != "new.test" {
		t.Fatal(string(v))
	}
	if v, _ := ReadCache(ctx, r, old, nil); string(v) != "old.test" {
		t.Fatal(string(v))
	}
}

// test_network_failure_also_keeps_valid_cache
func TestNetworkFailureAlsoKeepsValidCache(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, &rule)
	if _, err := WriteCache(ctx, r, rule, Content{Data: []byte("old.test")}, nil); err != nil {
		t.Fatal(err)
	}
	timeout := &FetchError{Class: "Timeout", Message: "controlled"}
	fail := func(context.Context, string) (Content, error) { return Content{}, timeout }
	value, refreshErr, err := RefreshRule(ctx, r, rule, nil, CacheOptions{Fetch: fail, AllowStale: true})
	if err != nil || string(value) != "old.test" || refreshErr != error(timeout) {
		t.Fatal(value, refreshErr, err)
	}
	if _, _, err := RefreshRule(ctx, r, rule, nil, CacheOptions{Fetch: fail}); err != error(timeout) {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(cachePath(t, r, rule)); string(data) != "old.test" {
		t.Fatal(string(data))
	}
}

// Identical bytes only get a new modification time (_touch_unchanged_cache).
func TestUnchangedRefreshOnlyTouches(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, &rule)
	for i := 0; i < 2; i++ {
		if _, err := WriteCache(ctx, r, rule, Content{Data: []byte("same.test\n"), Text: true}, nil); err != nil {
			t.Fatal(err)
		}
	}
	path := cachePath(t, r, rule)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(path, old, old)
	if _, err := WriteCache(ctx, r, rule, Content{Data: []byte("same.test\n")}, nil); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	bak, _ := os.ReadFile(path + ".bak")
	if !info.ModTime().After(old) || string(bak) != "same.test\n" {
		t.Fatal(info.ModTime(), string(bak))
	}
}

func sha(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// test_unique_legacy_url_cache_remains_available_offline /
// test_ambiguous_legacy_cache_is_not_read_as_either_source (read side)
func TestLegacyCacheReadSide(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, &rule)
	if _, err := r.WriteSharedText(ctx, "rules/by-id/"+sha("remote")+".list", []byte("legacy.test\n")); err != nil {
		t.Fatal(err)
	}
	if v, err := ReadCache(ctx, r, rule, nil); err != nil || string(v) != "legacy.test\n" {
		t.Fatal(string(v), err)
	}
	// A proposed declaration cannot borrow the stored source's legacy cache.
	proposed := rule
	proposed.URL = strp("https://rules.test/other.txt")
	if v, _ := ReadCache(ctx, r, proposed, nil); v != nil {
		t.Fatal(string(v))
	}
	// Legacy name caches shared by two library names are never attributed.
	content := model.RuleConfig{ID: "a", Name: "a:b", SourceType: strp("content"), Content: strp("x.test")}
	other := model.RuleConfig{ID: "b", Name: "a_b", SourceType: strp("content"), Content: strp("y.test")}
	ctx2, r2 := cacheRepo(t, nil)
	library := []model.RuleLibraryItem{ruleToLibraryItem(content), ruleToLibraryItem(other)}
	if err := r2.UpdateShared(ctx2, func(_ context.Context, shared *model.Shared) error {
		shared.RuleLibrary = library
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r2.WriteSharedText(ctx2, "rules/a_b.list", []byte("shared.test\n"))
	cfg := &SourceConfig{Library: library}
	for _, item := range []model.RuleConfig{content, other} {
		if v, _ := ReadCache(ctx2, r2, item, cfg); v != nil {
			t.Fatalf("%s read ambiguous legacy cache", item.ID)
		}
	}
}

type staticTransport struct{ status int }

func (s staticTransport) Do(_ context.Context, req TransportRequest) (*Response, error) {
	r := reply(s.status, "+.ok.test\n")
	r.URL = req.URL
	return r, nil
}

func TestReadRuleSourceContentAndSaveRuleToLocal(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, &rule)
	client := &Client{Transport: staticTransport{200}, Sleep: func(time.Duration) {}}
	if v, err := ReadSource(ctx, r, model.RuleConfig{SourceType: strp("content"), Content: strp("x")}, client); err != nil || v == nil || !v.Text || string(v.Data) != "x" {
		t.Fatal(v, err)
	}
	if v, err := ReadSource(ctx, r, model.RuleConfig{SourceType: strp("url")}, client); err != nil || v != nil {
		t.Fatal(v, err)
	}
	if v, err := ReadSource(ctx, r, rule, client); err != nil || v == nil || !v.Text || string(v.Data) != "+.ok.test\n" {
		t.Fatal(v, err)
	}
	name, err := SaveSource(ctx, r, rule, client)
	if err != nil || name != rules.CacheFilename(rule) {
		t.Fatal(name, err)
	}
	if data, _ := os.ReadFile(cachePath(t, r, rule)); string(data) != "+.ok.test\n" {
		t.Fatal(string(data))
	}
	failing := &Client{Transport: staticTransport{503}, Sleep: func(time.Duration) {}}
	if _, err := ReadSource(ctx, r, rule, failing); err == nil || err.Error() != "Rule download failed (HTTPError)" || errorClass(err) != "RequestException" {
		t.Fatal(err)
	}
	if _, err := SaveSource(ctx, r, model.RuleConfig{SourceType: strp("url")}, client); err == nil || err.Error() != "请提供有效的规则来源和订阅地址" {
		t.Fatal(err)
	}
}
