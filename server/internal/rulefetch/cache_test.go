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

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

func cacheRepo(t *testing.T, rule *jsonx.Object) (context.Context, *repo.Repository) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	if rule != nil {
		if _, err := r.SaveShared(ctx, jsonx.Obj("rule_library", []any{rule.DeepCopy()}), nil); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, r
}

func remoteSource() *jsonx.Object {
	return jsonx.Obj("id", "remote", "name", "远端规则", "source_type", "url", "url", "https://rules.test/remote.txt",
		"behavior", "domain", "format", "text", "enabled", true)
}

func cachePath(t *testing.T, r *repo.Repository, rule *jsonx.Object) string {
	dir, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, rules.RuleCacheFilename(rule))
}

func fetchBytes(raw string) func(string) (any, error) {
	return func(string) (any, error) { return []byte(raw), nil }
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
		rule.Set("behavior", c[0])
		ctx, r := cacheRepo(t, rule)
		if _, err := WriteValidatedRuleCache(ctx, rule, []byte(c[1]), nil); err != nil {
			t.Fatal(err)
		}
		path := cachePath(t, r, rule)
		before, _ := os.Stat(path)
		_, _, err := RefreshRemoteRule(ctx, rule, nil, RefreshOptions{Fetch: fetchBytes(c[2])})
		var remote *RemoteRuleValidationError
		if !errors.As(err, &remote) || !strings.Contains(err.Error(), "第 2 行") || !strings.Contains(err.Error(), strings.Split(c[2], "\n")[1]) {
			t.Fatal(err)
		}
		value, refreshErr, err := RefreshRemoteRule(ctx, rule, nil, RefreshOptions{Fetch: fetchBytes(c[2]), AllowStale: true})
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
	ctx, r := cacheRepo(t, rule)
	_, _, err := RefreshRemoteRule(ctx, rule, nil, RefreshOptions{Fetch: fetchBytes("good.test\nDOMAIN,other.test\n"), AllowStale: true})
	var remote *RemoteRuleValidationError
	if !errors.As(err, &remote) || firstProblem(remote).GetString("before") != "DOMAIN,other.test" {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath(t, r, rule)); !os.IsNotExist(err) {
		t.Fatal("cache written")
	}
}

// test_old_invalid_cache_is_never_used_as_fallback (rule_utils part)
func TestOldInvalidCacheIsNeverUsedAsFallback(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, rule)
	if _, err := r.WriteSharedText(ctx, "rules/"+rules.RuleCacheFilename(rule), []byte("DOMAIN,wrong.test")); err != nil {
		t.Fatal(err)
	}
	if value, err := ReadValidatedRuleCache(ctx, rule, nil); err != nil || value != nil {
		t.Fatal(value, err)
	}
	if _, _, err := RefreshRemoteRule(ctx, rule, nil, RefreshOptions{Fetch: fetchBytes("DOMAIN-SUFFIX,wrong.test"), AllowStale: true}); err == nil {
		t.Fatal("served invalid cache")
	}
	if data, _ := os.ReadFile(cachePath(t, r, rule)); string(data) != "DOMAIN,wrong.test" {
		t.Fatal(string(data))
	}
}

// test_good_refresh_recovers_and_preserves_original_framing
func TestGoodRefreshPreservesOriginalFraming(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, rule)
	if _, err := WriteValidatedRuleCache(ctx, rule, []byte("old.test"), nil); err != nil {
		t.Fatal(err)
	}
	raw := "\xef\xbb\xbf# source\r\n+.new.test\r\n"
	value, refreshErr, err := RefreshRemoteRule(ctx, rule, nil, RefreshOptions{Fetch: fetchBytes(raw)})
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
	old := rule.DeepCopy()
	rule.Set("url", "https://rules.test/new.txt")
	ctx, _ := cacheRepo(t, rule)
	if _, err := WriteValidatedRuleCache(ctx, rule, []byte("new.test"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteValidatedRuleCache(ctx, old, []byte("old.test"), nil); err != nil {
		t.Fatal(err)
	}
	if rules.RuleCacheFilename(rule) == rules.RuleCacheFilename(old) {
		t.Fatal("same name")
	}
	if v, _ := ReadValidatedRuleCache(ctx, rule, nil); string(v) != "new.test" {
		t.Fatal(string(v))
	}
	if v, _ := ReadValidatedRuleCache(ctx, old, nil); string(v) != "old.test" {
		t.Fatal(string(v))
	}
}

// test_network_failure_also_keeps_valid_cache
func TestNetworkFailureAlsoKeepsValidCache(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, rule)
	if _, err := WriteValidatedRuleCache(ctx, rule, []byte("old.test"), nil); err != nil {
		t.Fatal(err)
	}
	timeout := &FetchError{Class: "Timeout", Message: "controlled"}
	fail := func(string) (any, error) { return nil, timeout }
	value, refreshErr, err := RefreshRemoteRule(ctx, rule, nil, RefreshOptions{Fetch: fail, AllowStale: true})
	if err != nil || string(value) != "old.test" || refreshErr != error(timeout) {
		t.Fatal(value, refreshErr, err)
	}
	if _, _, err := RefreshRemoteRule(ctx, rule, nil, RefreshOptions{Fetch: fail}); err != error(timeout) {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(cachePath(t, r, rule)); string(data) != "old.test" {
		t.Fatal(string(data))
	}
}

// Identical bytes only get a new modification time (_touch_unchanged_cache).
func TestUnchangedRefreshOnlyTouches(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, rule)
	for i := 0; i < 2; i++ {
		if _, err := WriteValidatedRuleCache(ctx, rule, "same.test\n", nil); err != nil {
			t.Fatal(err)
		}
	}
	path := cachePath(t, r, rule)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(path, old, old)
	if _, err := WriteValidatedRuleCache(ctx, rule, []byte("same.test\n"), nil); err != nil {
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
	ctx, r := cacheRepo(t, rule)
	if _, err := r.WriteSharedText(ctx, "rules/by-id/"+sha("remote")+".list", []byte("legacy.test\n")); err != nil {
		t.Fatal(err)
	}
	if v, err := ReadValidatedRuleCache(ctx, rule, nil); err != nil || string(v) != "legacy.test\n" {
		t.Fatal(string(v), err)
	}
	// A proposed declaration cannot borrow the stored source's legacy cache.
	proposed := with(rule, "url", "https://rules.test/other.txt")
	if v, _ := ReadValidatedRuleCache(ctx, proposed, nil); v != nil {
		t.Fatal(string(v))
	}
	// Legacy name caches shared by two library names are never attributed.
	content := jsonx.Obj("id", "a", "name", "a:b", "source_type", "content", "content", "x.test")
	other := jsonx.Obj("id", "b", "name", "a_b", "source_type", "content", "content", "y.test")
	ctx2, r2 := cacheRepo(t, nil)
	r2.SaveShared(ctx2, jsonx.Obj("rule_library", []any{content, other}), nil)
	r2.WriteSharedText(ctx2, "rules/a_b.list", []byte("shared.test\n"))
	library := []any{content, other}
	for _, item := range []*jsonx.Object{content, other} {
		if v, _ := ReadValidatedRuleCache(ctx2, item, jsonx.Obj("rule_library", library)); v != nil {
			t.Fatalf("%s read ambiguous legacy cache", item.GetString("id"))
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
	ctx, r := cacheRepo(t, rule)
	client := &Client{Transport: staticTransport{200}, Sleep: func(time.Duration) {}}
	if v, err := ReadRuleSourceContent(ctx, jsonx.Obj("source_type", "content", "content", "x"), client); err != nil || v != "x" {
		t.Fatal(v, err)
	}
	if v, err := ReadRuleSourceContent(ctx, jsonx.Obj("source_type", "url"), client); err != nil || v != nil {
		t.Fatal(v, err)
	}
	if v, err := ReadRuleSourceContent(ctx, rule, client); err != nil || v != "+.ok.test\n" {
		t.Fatal(v, err)
	}
	name, err := SaveRuleToLocal(ctx, rule, client)
	if err != nil || name != rules.RuleCacheFilename(rule) {
		t.Fatal(name, err)
	}
	if data, _ := os.ReadFile(cachePath(t, r, rule)); string(data) != "+.ok.test\n" {
		t.Fatal(string(data))
	}
	failing := &Client{Transport: staticTransport{503}, Sleep: func(time.Duration) {}}
	if _, err := ReadRuleSourceContent(ctx, rule, failing); err == nil || err.Error() != "Rule download failed (HTTPError)" || PythonClass(err) != "RequestException" {
		t.Fatal(err)
	}
	if _, err := SaveRuleToLocal(ctx, jsonx.Obj("source_type", "url"), client); err == nil || err.Error() != "请提供有效的规则来源和订阅地址" {
		t.Fatal(err)
	}
}

func legacyConfig(config *SourceConfig) *jsonx.Object {
	if config == nil {
		return nil
	}
	return legacyObject(struct {
		Settings model.SystemConfig      `json:"system_config"`
		Library  []model.RuleLibraryItem `json:"rule_library"`
		Rules    []model.RuleConfig      `json:"rule_configs"`
		RuleSets []model.RuleConfig      `json:"rule_sets"`
	}{config.Settings, config.Library, config.Rules, config.RuleSets})
}

func contentBytes(value any) []byte { content, _ := legacyContent(value); return content.Data }

func ReadRuleSourceContent(ctx context.Context, rule *jsonx.Object, client *Client) (any, error) {
	content, err := ReadSource(ctx, legacyRule(rule), client)
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, nil
	}
	if content.Text {
		return string(content.Data), nil
	}
	return content.Data, nil
}

func WriteValidatedRuleCache(ctx context.Context, rule *jsonx.Object, value any, config *jsonx.Object) (string, error) {
	content, err := legacyContent(value)
	if err != nil {
		return "", err
	}
	return WriteCache(ctx, legacyRule(rule), content, sourceConfig(config))
}

func SaveRuleToLocal(ctx context.Context, rule *jsonx.Object, client *Client) (string, error) {
	return SaveSource(ctx, legacyRule(rule), client)
}

func RefreshRemoteRule(ctx context.Context, rule, config *jsonx.Object, opt RefreshOptions) ([]byte, error, error) {
	native := CacheOptions{Timeout: opt.Timeout, AllowStale: opt.AllowStale, Client: opt.Client}
	if opt.Fetch != nil {
		native.Fetch = func(ctx context.Context, url string) (Content, error) {
			if err := ctx.Err(); err != nil {
				return Content{}, err
			}
			value, err := opt.Fetch(url)
			if err != nil {
				return Content{}, err
			}
			if err := ctx.Err(); err != nil {
				return Content{}, err
			}
			return legacyContent(value)
		}
	}
	return RefreshRule(ctx, legacyRule(rule), sourceConfig(config), native)
}

type RefreshOptions struct {
	Timeout    time.Duration
	Fetch      func(string) (any, error)
	AllowStale bool
	Client     *Client
}
