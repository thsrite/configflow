package provider

// Ports of backend/test_provider_error_locations.py,
// test_dialer_provider_content.py::test_empty_provider_is_valid and the
// provider_delivery halves of test_subscription_refresh_policy.py,
// test_subscription_regex_filter.py and test_dialer_delivery_staging.py.
// deployment_locations (A11) is fakeLocations: these tests check what this
// module passes to it (field, line/column, label, owner, path); the final
// texts through the real A11 functions are in the corpus ('real').

import (
	"context"
	"errors"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
)

func repoContext(t *testing.T) (context.Context, *repo.Repository) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(SetLocations(fakeLocations{}))
	t.Cleanup(SetEnv(&Env{NodeID: SequentialNodeIDs(), Converter: nilConverter{}}))
	t.Cleanup(refresh.SetEnv(&refresh.Env{Clock: fixedClock{corpusNow}, Converter: nilConverter{}}))
	return repo.WithRepository(context.Background(), r), r
}

func emptySnapshot(t *testing.T, main *jsonx.Object) *Snapshot {
	if main == nil {
		main = jsonx.Obj("proxies", []any{}, "proxy-groups", []any{})
	}
	s, err := Capture("default", main, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustParseYAML(t *testing.T, text string) *jsonx.Object {
	v, err := readProviderTestValue(text)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*jsonx.Object)
}

func wantContains(t *testing.T, detail string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(detail, p) {
			t.Errorf("%q not in %q", p, detail)
		}
	}
}

func TestInvalidYAMLKeepsSourcePositionAndOriginalProblem(t *testing.T) {
	repoContext(t)
	_, err := ParseProviderProxies("proxies:\n  - name: Broken\n    type: [http\n", "机场 A", "./providers/a.yaml")
	var strategy *rules.StrategyReferenceError
	if !errors.As(err, &strategy) || errClass(err) != "DialerReferenceError" {
		t.Fatalf("err %v (%s)", err, errClass(err))
	}
	wantContains(t, err.Error(), "CTX[订阅|机场 A|./providers/a.yaml]",
		"SRC[Provider YAML 内容无效|2|1|本次订阅返回的内容|YAMLError:None:yaml: line 2:")
}

func TestShapesReportExactFieldAndSourceLine(t *testing.T) {
	repoContext(t)
	cases := []struct {
		text, field, line string
	}{
		{"proxies: wrong\n", "proxies", "1"},
		{"proxies:\n  - null\n", "proxies[0]", "2"},
		{"proxies:\n  - name: []\n", "proxies[0].name", "2"},
		{"proxies:\n  - name: Missing type\n    type: []\n", "proxies[0].type", "3"},
		{"proxies:\n  - name: Empty name\n    proxies: null\n", "proxies[0].proxies", "3"},
		{"proxies:\n  - name: Same\n    type: http\n  - name: Same\n    type: []\n", "proxies[1].type", "5"},
		{"base: &base\n  type: []\nproxies:\n  - <<: *base\n    name: Merged\n", "proxies[0].type", "2"},
	}
	for _, c := range cases {
		_, err := ParseProviderProxies(c.text, "Feed", "")
		if err == nil {
			t.Fatalf("%q accepted", c.text)
		}
		wantContains(t, err.Error(), "字段："+c.field+"|"+c.line+"|", "CTX[订阅|Feed|]")
	}
}

func TestUnknownDocumentShapeDoesNotInventALineNumber(t *testing.T) {
	repoContext(t)
	_, err := ParseProviderProxies("- not-a-provider\n", "Feed", "")
	wantContains(t, err.Error(), "CTX[订阅|Feed|]", "字段：文档根节点|None|None|")
}

func TestSourceExcerptAndURLNeverExposeNodeCredentials(t *testing.T) {
	repoContext(t)
	text := "proxies: [{name: Broken, type: [], password: secret-password, uuid: secret-uuid}]"
	_, err := ParseProviderProxies(text, "Feed", "https://user:secret-login@example.test/feed?token=secret-token")
	// The excerpt and path redaction are A11's (source_excerpt, failure_text in
	// preparation_context); this module passes the field's exact position.
	wantContains(t, err.Error(), "字段：proxies[0].type|1|", "|本次订阅返回的内容|None]")
}

func TestYAMLErrorOnSecretContinuationReportsLocationWithoutValue(t *testing.T) {
	repoContext(t)
	for _, text := range []string{
		"proxies:\n  - name: Broken\n    password: \"hidden-first\n      hidden-second\" broken\n",
		"proxies:\n  - name: Broken\n    uuid: \"hidden-first\n      hidden-second\" broken\n",
		"proxies: [{name: Broken, password: \"hidden-first\n    hidden-second\" broken}]\n",
		"proxies: [{name: Broken, password: \"hidden-first\n    hidden-second\\q",
	} {
		_, err := ParseProviderProxies(text, "Sensitive", "")
		if err == nil || !strings.Contains(err.Error(), "SRC[Provider YAML 内容无效|") || strings.Contains(err.Error(), "|None|None|") {
			t.Fatalf("%q: %v", text, err)
		}
	}
}

func TestClosedQuotedSecretDoesNotHideFollowingNonsecretField(t *testing.T) {
	repoContext(t)
	for _, quote := range []string{`"`, `'`} {
		flow := "proxies: [{name: Broken, password: " + quote + "hidden-first\n    hidden-second" + quote + ",\n    type: []}]\n"
		block := "proxies:\n  - name: Broken\n    password: " + quote + "hidden-first\n      hidden-second" + quote + "\n    type: []\n"
		for text, line := range map[string]string{flow: "3", block: "5"} {
			_, err := ParseProviderProxies(text, "Feed", "")
			wantContains(t, err.Error(), "字段：proxies[0].type|"+line+"|")
		}
	}
}

func TestValidProviderOutputAndSuccessPathParsingUnchanged(t *testing.T) {
	repoContext(t)
	for _, text := range []string{"{}", "proxies: []", "proxies: [{name: Good, type: http, server: example.test, port: 80}]"} {
		want, err := substore.ParseProxiesFromYAML(text)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ParseProviderProxies(text, "Feed", "")
		if err != nil || !jsonx.Equal(want, got) {
			t.Fatalf("%q: %v %v", text, got, err)
		}
	}
}

// test_dialer_provider_content.py::test_empty_provider_is_valid
func TestEmptyProviderIsValid(t *testing.T) {
	for _, text := range []string{"{}", "proxies: []"} {
		got, err := ParseProviderProxies(text, nil, nil)
		if err != nil || len(got) != 0 {
			t.Fatalf("%q: %v %v", text, got, err)
		}
	}
}

func fetchReturning(text, source string, calls *int) refresh.SourceFetch {
	return func(context.Context, string, string) (*substore.SubscriptionResult, error) {
		if calls != nil {
			*calls++
		}
		return &substore.SubscriptionResult{Text: text, Source: source, FetchedAt: corpusNowISO}, nil
	}
}

func fetchFailing(message string) refresh.SourceFetch {
	return func(context.Context, string, string) (*substore.SubscriptionResult, error) {
		return nil, &fakeErr{"ConnectionError", message}
	}
}

func TestInvalidDownloadDoesNotFallBackToCache(t *testing.T) {
	ctx, r := repoContext(t)
	// A valid cache exists: content errors must not use it.
	if _, err := r.WriteSharedJSON(ctx, "subscribes/s.json", jsonx.Obj("nodes", []any{jsonx.Obj("name", "Cached", "type", "http", "server", "c.test", "port", jsonx.Int(1))})); err != nil {
		t.Fatal(err)
	}
	sub := jsonx.Obj("id", "s", "name", "Feed", "url", "https://example.test/sub")
	_, err := PrepareSubscriptionProvider(ctx, sub, jsonx.NewObject(), emptySnapshot(t, nil), PrepareOptions{
		Fetch: fetchReturning("proxies: [{name: Bad, type: []}]", "rendered_yaml", nil), AllowTransportFallback: true,
		SourcePath: "./providers/s.yaml"})
	if err == nil {
		t.Fatal("accepted")
	}
	if n := strings.Count(err.Error(), "CTX[订阅|Feed|./providers/s.yaml]"); n != 1 {
		t.Fatalf("context added %d times: %q", n, err.Error())
	}
	wantContains(t, err.Error(), "proxies[0].type")
}

func TestMissingCacheRetainsTransportDiagnosticAndSource(t *testing.T) {
	ctx, _ := repoContext(t)
	sub := jsonx.Obj("id", "s", "name", "Feed", "url", "https://example.test/sub")
	_, err := PrepareSubscriptionProvider(ctx, sub, jsonx.NewObject(), emptySnapshot(t, nil), PrepareOptions{
		Fetch: fetchFailing("Connection refused https://user:secret-login@example.test/sub?token=secret-token")})
	if err == nil {
		t.Fatal("accepted")
	}
	detail := err.Error()
	wantContains(t, detail, "CTX[订阅|Feed|]", "订阅没有可用缓存或远程代理", "Connection refused")
	if strings.Contains(detail, "secret-login") || strings.Contains(detail, "secret-token") || strings.Contains(detail, "SRC[") {
		t.Fatalf("detail %q", detail)
	}
}

func TestTransportFallbackStillReturnsEmptyWithoutFalseContentError(t *testing.T) {
	ctx, _ := repoContext(t)
	sub := jsonx.Obj("id", "s", "name", "Feed", "url", "https://example.test/sub")
	got, err := PrepareSubscriptionProvider(ctx, sub, jsonx.NewObject(), emptySnapshot(t, nil), PrepareOptions{
		Fetch: fetchFailing("unavailable"), AllowTransportFallback: true})
	if err != nil || got.HasName || got.Content != "" || len(got.CacheUpdates) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestFinalBundleErrorIdentifiesActualProviderFile(t *testing.T) {
	repoContext(t)
	_, err := ValidateRenderedBundle(emptySnapshot(t, nil), []*Rendered{
		{Name: "First", HasName: true, LocalPath: "./providers/first.yaml", Content: "proxies: []"},
		{Name: "Second", HasName: true, LocalPath: "./providers/second.yaml", Content: "proxies:\n - name: Bad\n   type: 11"},
	})
	if err == nil {
		t.Fatal("accepted")
	}
	wantContains(t, err.Error(), "CTX[节点来源|Second|./providers/second.yaml]", "|3|", "本次准备的文件内容")
	if strings.Contains(err.Error(), "First") {
		t.Fatal(err)
	}
}

func TestPreparedProviderFailureKeepsAliasAndRealOutputPath(t *testing.T) {
	ctx, _ := repoContext(t)
	main := mustParseYAML(t, `proxy-providers:
  Provider alias: {url: /subscriptions/s/proxies, path: ./providers/actual.yaml}
  Other alias: {url: /subscriptions/s/proxies, path: ./providers/other.yaml}
`)
	config := jsonx.Obj("profile_id", "default", "subscriptions", []any{jsonx.Obj("id", "s", "name", "Source name")})
	_, err := PrepareProviderBundle(ctx, config, main, BundleOptions{RenderAll: true, Prepared: map[string]*Rendered{
		"Provider alias": {Content: "proxies:\n - name: Invalid\n   type: []", CacheUpdates: []CacheUpdate{}}}})
	if err == nil {
		t.Fatal("accepted")
	}
	wantContains(t, err.Error(), "Provider alias", "./providers/actual.yaml", "|3|")
	if strings.Contains(err.Error(), "Other alias") {
		t.Fatal(err)
	}
}

func TestTopologyErrorHasSourceButNoGuessedLine(t *testing.T) {
	repoContext(t)
	main := mustParseYAML(t, `proxies:
  - {name: Entry, type: http, dialer-proxy: Upstream}
  - {name: Upstream, type: http}
proxy-groups: []
`)
	_, err := ValidateRenderedBundle(emptySnapshot(t, main), []*Rendered{{Name: "Feed", HasName: true,
		LocalPath: "./providers/feed.yaml", Content: "proxies: [{name: Bad, type: http, dialer-proxy: Missing}]"}})
	if err == nil {
		t.Fatal("accepted")
	}
	wantContains(t, err.Error(), "CTX[节点来源|Feed|./providers/feed.yaml]")
	if strings.Contains(err.Error(), "SRC[") {
		t.Fatal(err)
	}
}

// test_subscription_regex_filter.py::test_provider_delivers_filtered_proxies_but_caches_raw_nodes
func TestProviderDeliversFilteredProxiesButCachesRawNodes(t *testing.T) {
	ctx, _ := repoContext(t)
	text := `proxies:
  - {name: 香港 01, type: ss, server: 1.1.1.1, port: 1, cipher: aes-128-gcm, password: p}
  - {name: 日本 01, type: ss, server: 1.1.1.2, port: 2, cipher: aes-128-gcm, password: p}
  - {name: 剩余流量 100G, type: ss, server: 1.1.1.3, port: 3, cipher: aes-128-gcm, password: p}
`
	sub := jsonx.Obj("id", "s1", "name", "Air", "url", "http://airport.test/sub", "regex_filter", "香港|日本")
	got, err := PrepareSubscriptionProvider(ctx, sub, jsonx.NewObject(), emptySnapshot(t, nil), PrepareOptions{
		Fetch: fetchReturning(text, "sub_store", nil)})
	if err != nil {
		t.Fatal(err)
	}
	var names []any
	for _, p := range mustParseYAML(t, got.Content).GetList("proxies") {
		names = append(names, p.(*jsonx.Object).Value("name"))
	}
	if !jsonx.Equal(names, []any{"香港 01", "日本 01"}) || len(got.CacheUpdates) != 1 || len(got.CacheUpdates[0].Nodes) != 3 {
		t.Fatalf("%v %+v", names, got.CacheUpdates)
	}
}

const policyURL = "https://airport.test/sub?token=private"

func policyCache(t *testing.T, ctx context.Context) *jsonx.Object {
	proxies := []any{jsonx.Obj("name", "Remote", "type", "ss", "server", "example.test", "port", jsonx.Int(443), "cipher", "aes-128-gcm", "password", "p")}
	nodes, err := substore.ProxiesToNodes(proxies)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := substore.SaveSubscriptionNodes(ctx, "s", nodes, jsonx.Obj("url", policyURL))
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

// test_subscription_refresh_policy.py::test_publish_preparation_uses_cache_and_stages_no_fake_refresh
func TestPublishPreparationUsesCacheAndStagesNoFakeRefresh(t *testing.T) {
	ctx, _ := repoContext(t)
	before := policyCache(t, ctx)
	sub := jsonx.Obj("id", "s", "name", "Feed", "url", policyURL, "enabled", true, "interval", jsonx.Int(0))
	got, err := PrepareSubscriptionProvider(ctx, sub, jsonx.NewObject(), emptySnapshot(t, nil), PrepareOptions{
		Fetch: func(context.Context, string, string) (*substore.SubscriptionResult, error) {
			t.Fatal("publish must not bypass manual mode")
			return nil, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	want := []any{jsonx.Obj("name", "Remote", "type", "ss", "server", "example.test", "port", jsonx.Int(443), "cipher", "aes-128-gcm", "password", "p")}
	if !jsonx.Equal(mustParseYAML(t, got.Content).Value("proxies"), want) || len(got.CacheUpdates) != 0 {
		t.Fatalf("%q %+v", got.Content, got.CacheUpdates)
	}
	after, _ := substore.LoadSubscriptionCache(ctx, "s")
	if !jsonx.Equal(before, after) {
		t.Fatal("cache changed")
	}
}

// test_subscription_refresh_policy.py::test_dry_previews_share_download_across_restart_without_changing_validated_nodes
func TestDryPreviewsShareDownloadAcrossRestartWithoutChangingValidatedNodes(t *testing.T) {
	ctx, r := repoContext(t)
	content := "proxies:\n- cipher: aes-128-gcm\n  name: Remote\n  password: p\n  port: 443\n  server: example.test\n  type: ss\n"
	calls := 0
	sub := jsonx.Obj("id", "s", "name", "Feed", "url", policyURL, "enabled", true, "interval", jsonx.Int(259200))
	first, err := PrepareSubscriptionProvider(ctx, sub, jsonx.NewObject(), emptySnapshot(t, nil), PrepareOptions{Fetch: fetchReturning(content, "direct_yaml", &calls)})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := repo.Open(r.DataDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx = repo.WithRepository(context.Background(), reopened)
	second, err := PrepareSubscriptionProvider(ctx, sub, jsonx.NewObject(), emptySnapshot(t, nil), PrepareOptions{Fetch: fetchReturning(content, "direct_yaml", &calls)})
	if err != nil {
		t.Fatal(err)
	}
	if first.Content != second.Content || calls != 1 ||
		!jsonx.Equal(first.CacheUpdates[0].Metadata.Value("fetched_at"), second.CacheUpdates[0].Metadata.Value("fetched_at")) {
		t.Fatalf("calls %d", calls)
	}
	if cache, _ := substore.LoadSubscriptionCache(ctx, "s"); cache != nil {
		t.Fatal("validated nodes written")
	}
}

// test_dialer_delivery_staging.py::test_no_chain_direct_subscription_preserves_duplicate_legacy_names (provider half)
func TestNoChainDirectSubscriptionPreservesDuplicateLegacyNames(t *testing.T) {
	ctx, _ := repoContext(t)
	text := "proxies: [{name: Same, type: http, server: a.test, port: 80}, {name: Same, type: http, server: b.test, port: 80}]"
	sub := jsonx.Obj("id", "s", "name", "Feed", "url", "https://example.test/sub", "interval", jsonx.Int(259200))
	got, err := PrepareSubscriptionProvider(ctx, sub, jsonx.NewObject(), emptySnapshot(t, nil), PrepareOptions{Fetch: fetchReturning(text, "rendered_yaml", nil)})
	if err != nil {
		t.Fatal(err)
	}
	var servers []any
	for _, p := range mustParseYAML(t, got.Content).GetList("proxies") {
		servers = append(servers, p.(*jsonx.Object).Value("server"))
	}
	if !jsonx.Equal(servers, []any{"a.test", "b.test"}) {
		t.Fatal(servers)
	}
}

func TestDefaultHooksFailClosedAndPassThrough(t *testing.T) {
	if got := (PassthroughLocations{}).PreparationContext("m", "n", "p", "k"); got != "k「n」准备失败：m" {
		t.Fatal(got)
	}
	ctx, _ := repoContext(t)
	SetLocations(nil)
	defer SetLocations(fakeLocations{})
	config := jsonx.Obj("profile_id", "default", "subscription_aggregations", []any{jsonx.Obj("id", "agg", "name", "Agg")})
	main := mustParseYAML(t, "proxy-providers:\n  Agg: {url: /aggregations/agg/provider, path: ./providers/agg.yaml}\n")
	_, err := PrepareProviderBundle(ctx, config, main, BundleOptions{RenderAll: true})
	if !errors.Is(err, ErrAggregationProviderMissing) {
		t.Fatalf("err %v", err)
	}
}
