package convert

import (
	"container/list"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/yamlx"
)

func mustJSON(t *testing.T, text string) *jsonx.Object {
	t.Helper()
	v, err := jsonx.ParseString(text)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*jsonx.Object)
}

// ---------------------------------------------------- test_proxy_yaml_cache.py

// isolateCache is the isolated_cache fixture plus a counting safe_load spy.
func isolateCache(t *testing.T) *int {
	t.Helper()
	clearCache := func() {
		proxyYAMLMu.Lock()
		proxyYAMLOrder.Init()
		proxyYAMLIndex = map[string]*list.Element{}
		proxyYAMLMu.Unlock()
	}
	clearCache()
	calls := 0
	var mu sync.Mutex
	saved := safeLoad
	safeLoad = func(text string) (any, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return yamlx.SafeLoad(text)
	}
	savedSize, savedInput, savedResult, savedObjects, savedDepth :=
		proxyYAMLCacheSize, proxyYAMLMaxInputBytes, proxyYAMLMaxResultBytes, proxyYAMLMaxObjects, proxyYAMLMaxDepth
	t.Cleanup(func() {
		safeLoad = saved
		proxyYAMLCacheSize, proxyYAMLMaxInputBytes, proxyYAMLMaxResultBytes, proxyYAMLMaxObjects, proxyYAMLMaxDepth =
			savedSize, savedInput, savedResult, savedObjects, savedDepth
		clearCache()
	})
	return &calls
}

func cacheLen() int {
	proxyYAMLMu.Lock()
	defer proxyYAMLMu.Unlock()
	return proxyYAMLOrder.Len()
}

// test_old_parse_semantics_on_first_and_repeated_calls: the Python results
// are the corpus rows; here the second (cached) call must agree with them.
func TestOldParseSemanticsOnFirstAndRepeatedCalls(t *testing.T) {
	isolateCache(t)
	for _, row := range loadCorpus(t) {
		if row.fn != "parse_structured_proxy_string" {
			continue
		}
		text := row.args[0].(string)
		want := compactText(t, row.out)
		for i := 0; i < 2; i++ {
			got := ParseStructuredProxyString(text)
			var value any
			if got != nil {
				value = pyConv(got)
			}
			if g := compactText(t, value); g != want {
				t.Fatalf("call %d %q: got %s want %s", i, text, g, want)
			}
		}
	}
}

const nestedProxy = "type: vless\nname: original\nserver: proxy.example\nport: 443\n" +
	"uuid: 00000000-0000-4000-8000-000000000000\nalpn: [h2]\n" +
	"ws-opts:\n  headers:\n    Host: edge.example"

func TestNestedResultsAndConversionNamesDoNotPolluteCache(t *testing.T) {
	calls := isolateCache(t)
	first := ParseStructuredProxyString(nestedProxy)
	first.GetObject("ws-opts").GetObject("headers").Set("Host", "changed.invalid")
	first.Set("alpn", append(first.GetList("alpn"), "changed"))
	first.Set("dialer-proxy", "changed")
	later := ParseStructuredProxyString(nestedProxy)
	if later.GetObject("ws-opts").GetObject("headers").GetString("Host") != "edge.example" ||
		len(later.GetList("alpn")) != 1 || later.Has("dialer-proxy") {
		t.Fatalf("cache polluted: %s", compactText(t, later))
	}
	ctx := context.Background()
	one, _ := ConvertNodeToMihomo(ctx, jsonx.Obj("name", "one", "proxy_string", nestedProxy), nil)
	two, _ := ConvertNodeToMihomo(ctx, jsonx.Obj("name", "two", "proxy_string", nestedProxy), nil)
	if one.GetString("name") != "one" || two.GetString("name") != "two" {
		t.Fatal("names")
	}
	if ParseStructuredProxyString(nestedProxy).GetString("name") != "original" {
		t.Fatal("name leaked into cache")
	}
	if *calls != 1 {
		t.Fatalf("safe_load calls %d", *calls)
	}
}

func TestFullTextChangeInvalidatesAndLRUEvicts(t *testing.T) {
	calls := isolateCache(t)
	proxyYAMLCacheSize = 2
	a, b, c := "type: ss\nserver: a.example", "type: ss\nserver: b.example", "type: ss\nserver: c.example"
	for _, text := range []string{a, b, " \n" + a + "\n ", c} {
		trimmed := strings.TrimSpace(text)
		want := trimmed[strings.LastIndex(trimmed, ": ")+2:]
		if got := ParseStructuredProxyString(text).GetString("server"); got != want {
			t.Fatalf("%q: %s", text, got)
		}
	}
	proxyYAMLMu.Lock()
	_, hasA := proxyYAMLIndex[a]
	_, hasC := proxyYAMLIndex[c]
	proxyYAMLMu.Unlock()
	if !hasA || !hasC || cacheLen() != 2 || *calls != 3 {
		t.Fatalf("cache a=%v c=%v len=%d calls=%d", hasA, hasC, cacheLen(), *calls)
	}
	if ParseStructuredProxyString(b).GetString("server") != "b.example" || *calls != 4 || cacheLen() != 2 {
		t.Fatal("b reload")
	}
}

func TestURIAndValidJSONDoNotTouchYAMLCache(t *testing.T) {
	isolateCache(t)
	safeLoad = func(string) (any, error) { panic("YAML path entered") }
	if ParseStructuredProxyString("ss://sample@proxy.example:443") != nil {
		t.Fatal("uri")
	}
	if got := compactText(t, ParseStructuredProxyString(`{"type":"ss","port":443}`)); got != `{"type":"ss","port":443}` {
		t.Fatal(got)
	}
	if cacheLen() != 0 {
		t.Fatal("cache touched")
	}
}

func TestFailedAndNonProxyResultsNotRetained(t *testing.T) {
	for _, text := range []string{"type: [", "null", "name: sample", "type: false"} {
		calls := isolateCache(t)
		if ParseStructuredProxyString(text) != nil || ParseStructuredProxyString(text) != nil {
			t.Fatalf("%q parsed", text)
		}
		if *calls != 2 || cacheLen() != 0 {
			t.Fatalf("%q: calls %d len %d", text, *calls, cacheLen())
		}
	}
}

func TestLoaderFailureDoesNotPoisonLaterRetry(t *testing.T) {
	isolateCache(t)
	calls := 0
	safeLoad = func(text string) (any, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("temporary failure")
		}
		return yamlx.SafeLoad(text)
	}
	if ParseStructuredProxyString("type: ss") != nil {
		t.Fatal("first")
	}
	for i := 0; i < 2; i++ {
		if got := compactText(t, ParseStructuredProxyString("type: ss")); got != `{"type":"ss"}` {
			t.Fatal(got)
		}
	}
	if calls != 2 {
		t.Fatalf("calls %d", calls)
	}
}

func TestInputByteBudgetBoundary(t *testing.T) {
	for _, tc := range []struct {
		extra     string
		cacheable bool
	}{{"", true}, {"x", false}, {"中", false}} {
		calls := isolateCache(t)
		text := "type: ss\nname: abc"
		proxyYAMLMaxInputBytes = len(text)
		for i := 0; i < 2; i++ {
			if got := ParseStructuredProxyString(text + tc.extra).GetString("name"); got != "abc"+tc.extra {
				t.Fatal(got)
			}
		}
		want := 2
		if tc.cacheable {
			want = 1
		}
		if *calls != want || (cacheLen() > 0) != tc.cacheable {
			t.Fatalf("%q: calls %d len %d", tc.extra, *calls, cacheLen())
		}
	}
}

func TestMultibyteInputBelowCharacterLimitStillBypassesByteLimit(t *testing.T) {
	calls := isolateCache(t)
	text := "type: ss\nname: " + strings.Repeat("中", 10)
	proxyYAMLMaxInputBytes = len([]rune(text)) + 1
	for i := 0; i < 2; i++ {
		if ParseStructuredProxyString(text).GetString("name") != strings.Repeat("中", 10) {
			t.Fatal("name")
		}
	}
	if *calls != 2 || cacheLen() != 0 {
		t.Fatalf("calls %d", *calls)
	}
}

func TestResultBudgetsBypassWithoutChangingResults(t *testing.T) {
	for _, tc := range []struct {
		limit *int
		value int
		text  string
	}{
		{&proxyYAMLMaxResultBytes, 100, "type: ss\ntext: " + strings.Repeat("x", 200)},
		{&proxyYAMLMaxObjects, 5, "type: ss\nvalues: [a, b, c, d, e, f]"},
		{&proxyYAMLMaxDepth, 2, "type: ss\nvalues: [[[item]]]"},
	} {
		calls := isolateCache(t)
		v, _ := yamlx.SafeLoad(tc.text)
		want := compactText(t, v)
		*tc.limit = tc.value
		for i := 0; i < 2; i++ {
			if got := compactText(t, ParseStructuredProxyString(tc.text)); got != want {
				t.Fatalf("%q: %s", tc.text, got)
			}
		}
		if *calls != 2 || cacheLen() != 0 {
			t.Fatalf("%q: calls %d len %d", tc.text, *calls, cacheLen())
		}
	}
}

// test_special_yaml_types_bypass_with_original_types: dates and !!binary keep
// their types and bypass the cache. !!set values and non-string keys
// (yes/true) cannot be held by jsonx: yamlx refuses them, so the string is
// left to Sub-Store (nil) — a documented difference (Python returns a dict
// with a set value or a bool key).
func TestSpecialYAMLTypesBypassWithOriginalTypes(t *testing.T) {
	for _, text := range []string{"type: ss\nvalue: 2026-10-17", "type: ss\nvalue: !!binary aGVsbG8="} {
		calls := isolateCache(t)
		for i := 0; i < 2; i++ {
			got := ParseStructuredProxyString(text)
			switch got.Value("value").(type) {
			case yamlx.Timestamp, []byte:
			default:
				t.Fatalf("%q: %#v", text, got.Value("value"))
			}
		}
		if *calls != 2 || cacheLen() != 0 {
			t.Fatalf("%q: calls %d", text, *calls)
		}
	}
	for _, text := range []string{"type: ss\nvalue: !!set {a: null, b: null}", "type: ss\nyes: first\ntrue: second"} {
		isolateCache(t)
		if ParseStructuredProxyString(text) != nil {
			t.Fatalf("%q: expected the documented yamlx refusal", text)
		}
	}
}

// test_aliases_and_cycles_keep_identity_and_bypass_cache: shared containers
// bypass the cache (Go keeps the shared *Object of an alias; a recursive
// node is a yamlx error, so the string goes to Sub-Store — documented).
func TestAliasesAndCyclesBypassCache(t *testing.T) {
	calls := isolateCache(t)
	alias := "type: ss\na: &shared {x: 1}\nb: *shared"
	for i := 0; i < 2; i++ {
		result := ParseStructuredProxyString(alias)
		if result.GetObject("a") != result.GetObject("b") {
			t.Fatal("alias identity lost")
		}
		if ParseStructuredProxyString("&root {type: ss, self: *root}") != nil {
			t.Fatal("recursive node")
		}
	}
	if *calls != 4 || cacheLen() != 0 {
		t.Fatalf("calls %d len %d", *calls, cacheLen())
	}
}

func TestAliasDiamondDoesNotExpandDuringCacheScreening(t *testing.T) {
	calls := isolateCache(t)
	var b strings.Builder
	b.WriteString("type: ss\na0: &a0 {v: item}\n")
	for i := 1; i < 30; i++ {
		b.WriteString("a" + itoa(i) + ": &a" + itoa(i) + " {l: *a" + itoa(i-1) + ", r: *a" + itoa(i-1) + "}\n")
	}
	for i := 0; i < 2; i++ {
		result := ParseStructuredProxyString(b.String())
		a29 := result.GetObject("a29")
		if a29.GetObject("l") != a29.GetObject("r") || a29.GetObject("l") != result.GetObject("a28") {
			t.Fatal("identity")
		}
	}
	if *calls != 2 || cacheLen() != 0 {
		t.Fatalf("calls %d", *calls)
	}
}

func itoa(i int) string { return jsonx.Int(i).String() }

func TestConcurrentColdCallsParseOnceAndReturnPrivateResults(t *testing.T) {
	calls := isolateCache(t)
	var wg sync.WaitGroup
	results := make([]*jsonx.Object, 8)
	for i := range results {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r := ParseStructuredProxyString(nestedProxy)
			r.GetObject("ws-opts").GetObject("headers").Set("Host", itoa(n))
			results[n] = r
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r.GetObject("ws-opts").GetObject("headers").GetString("Host") != itoa(i) {
			t.Fatal("shared result")
		}
	}
	if *calls != 1 {
		t.Fatalf("calls %d", *calls)
	}
	if ParseStructuredProxyString(nestedProxy).GetObject("ws-opts").GetObject("headers").GetString("Host") != "edge.example" {
		t.Fatal("cache polluted")
	}
}

// test_deepcopy_does_not_hold_cache_lock is structural in Go: loadProxyYAML
// releases proxyYAMLMu before DeepCopy. Checked by taking the lock from
// another goroutine while results are copied in a loop.
func TestDeepCopyDoesNotHoldCacheLock(t *testing.T) {
	isolateCache(t)
	ParseStructuredProxyString(nestedProxy)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			ParseStructuredProxyString(nestedProxy)
		}
		close(done)
	}()
	for {
		select {
		case <-done:
			return
		default:
			proxyYAMLMu.Lock()
			proxyYAMLMu.Unlock() //nolint:staticcheck // only proves the lock is free
		}
	}
}

// ------------------------------------------------ test_node_conversion_cache.py

// countingConverter remembers conversions like the Sub-Store client does.
type countingConverter struct {
	mu       sync.Mutex
	requests int
	kept     map[string]*jsonx.Object
}

func (c *countingConverter) ConvertProxyString(_ context.Context, s string) *jsonx.Object {
	c.mu.Lock()
	defer c.mu.Unlock()
	if kept, ok := c.kept[s]; ok {
		return kept.DeepCopy()
	}
	c.requests++
	proxy := jsonx.Obj("name", "from-uri", "type", "anytls", "server", "node.example.test", "port", jsonx.Int(443),
		"password", "secret")
	c.kept[s] = proxy.DeepCopy()
	return proxy
}

// test_rendering_again_does_not_start_sub_store: the converted node is named
// by each caller and the caller's copy is private. (The client-side cache
// itself is C13's.)
func TestRenderingAgainDoesNotStartSubStore(t *testing.T) {
	ctx := context.Background()
	node := "anytls://secret@node.example.test:443?sni=example.test#one"
	conv := &countingConverter{kept: map[string]*jsonx.Object{}}
	first, _ := ConvertNodeToMihomo(ctx, jsonx.Obj("name", "Home", "proxy_string", node), conv)
	second, _ := ConvertNodeToMihomo(ctx, jsonx.Obj("name", "Home", "proxy_string", node), conv)
	third, _ := ConvertNodeToMihomo(ctx, jsonx.Obj("name", "Away", "proxy_string", node), conv)
	if conv.requests != 1 {
		t.Fatalf("requests %d", conv.requests)
	}
	if compactText(t, first) != compactText(t, second) || first.GetString("name") != "Home" ||
		first.GetString("server") != "node.example.test" {
		t.Fatal(compactText(t, first))
	}
	want := first.DeepCopy()
	want.Set("name", "Away")
	if compactText(t, third) != compactText(t, want) {
		t.Fatal(compactText(t, third))
	}
}

// node_conversions(results, reuse=True) answers from the record without
// asking Sub-Store, including a recorded failure.
func TestNodeConversionsReuse(t *testing.T) {
	ctx := context.Background()
	conv := &fakeConverter{}
	results := NewConversionResults()
	scoped := WithNodeConversions(ctx, results, false)
	for _, s := range []string{"ss://a@proxy.example:1", "ss://fail@proxy.example:1"} {
		ConvertNodeToMihomo(scoped, jsonx.Obj("name", "n", "proxy_string", s), conv)
	}
	if len(conv.calls) != 2 || results.Len() != 2 {
		t.Fatal(conv.calls)
	}
	reuse := WithNodeConversions(ctx, results, true)
	p, _ := ConvertNodeToMihomo(reuse, jsonx.Obj("name", "again", "proxy_string", "ss://a@proxy.example:1"), conv)
	failed, _ := ConvertNodeToMihomo(reuse, jsonx.Obj("name", "again", "proxy_string", "ss://fail@proxy.example:1"), conv)
	if len(conv.calls) != 2 || p.GetString("name") != "again" || failed != nil {
		t.Fatal("reuse asked Sub-Store again")
	}
	// Without reuse the same scope records again; outside a scope nothing is recorded.
	ConvertNodeToMihomo(ctx, jsonx.Obj("name", "x", "proxy_string", "ss://b@proxy.example:1"), conv)
	if results.Len() != 2 {
		t.Fatal("recorded outside the scope")
	}
}

// ------------------------------------------------------------ dialer tests

func dialerNode(id string, extra ...any) *jsonx.Object {
	n := jsonx.Obj("id", id, "name", id, "enabled", true, "type", "http", "server", "example.test", "port", jsonx.Int(80))
	for i := 0; i+1 < len(extra); i += 2 {
		n.Set(extra[i].(string), extra[i+1])
	}
	return n
}

func wantDialerError(t *testing.T, err error, fragment string) {
	t.Helper()
	var d *DialerReferenceError
	if !errors.As(err, &d) || !strings.Contains(d.Message, fragment) {
		t.Fatalf("want DialerReferenceError containing %q, got %v", fragment, err)
	}
}

// test_dialer_provider_scope.py::test_raw_dialer_dynamic_group_checks_real_candidate_edges
func TestRawDialerDynamicGroupChecksRealCandidateEdges(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, cycle := range []bool{false, true} {
			groups := []any{jsonx.Obj("name", "Dynamic", "use", []any{"Feed"})}
			if nested {
				groups = append(groups, jsonx.Obj("name", "Outer", "proxies", []any{"Dynamic"}))
			}
			target := "Dynamic"
			if nested {
				target = "Outer"
			}
			main := jsonx.Obj("proxies", []any{jsonx.Obj("name", "Exit", "dialer-proxy", target)},
				"proxy-groups", groups, "proxy-providers", jsonx.Obj("Feed", jsonx.NewObject()))
			candidate := jsonx.Obj("name", "Remote")
			if cycle {
				candidate.Set("dialer-proxy", target)
			}
			err := ValidateEmitted(main, EmitOptions{ProviderProxies: jsonx.Obj("Feed", []any{candidate}), RequireProviders: true})
			if cycle {
				wantDialerError(t, err, "")
			} else if err != nil {
				t.Fatalf("nested=%v: %v", nested, err)
			}
		}
	}
}

// test_dialer_provider_scope.py::test_static_provider_dialer_and_builtin_allowed
func TestStaticProviderDialerAndBuiltinAllowed(t *testing.T) {
	for _, target := range []string{"DIRECT", "Relay"} {
		err := ValidateEmitted(jsonx.Obj("proxies", []any{jsonx.Obj("name", "Remote", "dialer-proxy", target)},
			"proxy-groups", []any{jsonx.Obj("name", "Relay", "proxies", []any{"DIRECT"}),
				jsonx.Obj("name", "UnrelatedDynamic", "use", []any{"Feed"})},
			"proxy-providers", jsonx.Obj("Feed", jsonx.NewObject())), EmitOptions{})
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
	}
}

// test_dialer_provider_scope.py::test_no_chain_dynamic_group_legacy_allowed
func TestNoChainDynamicGroupLegacyAllowed(t *testing.T) {
	if err := ValidateEmitted(jsonx.Obj("proxies", []any{jsonx.Obj("name", "Remote")},
		"proxy-groups", []any{jsonx.Obj("name", "Entry", "use", []any{"Feed"})},
		"proxy-providers", jsonx.Obj("Feed", jsonx.NewObject())), EmitOptions{}); err != nil {
		t.Fatal(err)
	}
}

// test_dialer_review_fixes.py::test_emitted_dialer_graph_checks_ordinary_group_members
func TestEmittedDialerGraphChecksOrdinaryGroupMembers(t *testing.T) {
	err := ValidateEmitted(jsonx.Obj("proxies", []any{jsonx.Obj("name", "exit", "dialer-proxy", "relay"), jsonx.Obj("name", "relay")},
		"proxy-groups", []any{jsonx.Obj("name", "Entry", "proxies", []any{"gone"})}), EmitOptions{})
	wantDialerError(t, err, "未生成")
}

// test_dialer_round2.py::test_disabled_managed_source_keeps_manual_only_closure_contract
func TestDisabledManagedSourceKeepsManualOnlyClosureContract(t *testing.T) {
	source := dialerNode("source", "dialer_ref", jsonx.Obj("type", "node", "id", "relay"))
	source.Set("enabled", false)
	profile := jsonx.Obj("nodes", []any{source, dialerNode("relay", "subscription_id", "s")})
	wantDialerError(t, ValidateDialers(profile), "手动")
}

func deepNodes(cycle bool) []any {
	var nodes []any
	for i := 0; i < 1100; i++ {
		if i < 1099 || cycle {
			nodes = append(nodes, dialerNode(itoa(i), "dialer_ref", jsonx.Obj("type", "node", "id", itoa((i+1)%1100))))
		} else {
			nodes = append(nodes, dialerNode(itoa(i)))
		}
	}
	return nodes
}

// test_dialer_round2.py::test_deep_graph_validation_and_closure_are_stack_safe
func TestDeepGraphValidationAndClosureAreStackSafe(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		profile := jsonx.Obj("nodes", deepNodes(cycle))
		err := ValidateDialers(profile)
		if cycle {
			wantDialerError(t, err, "循环")
		} else {
			if err != nil {
				t.Fatal(err)
			}
			ids, err := DependencyNodeIDs(profile, []string{"0"}, true)
			if err != nil || len(ids) != 1100 {
				t.Fatalf("%d %v", len(ids), err)
			}
		}
		var proxies []any
		for _, n := range profile.GetList("nodes") {
			node := n.(*jsonx.Object)
			p := jsonx.Obj("name", node.Value("name"))
			if ref := node.GetObject("dialer_ref"); ref != nil {
				p.Set("dialer-proxy", ref.Value("id"))
			}
			proxies = append(proxies, p)
		}
		err = ValidateEmitted(jsonx.Obj("proxies", proxies), EmitOptions{})
		if cycle {
			wantDialerError(t, err, "循环")
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

// --------------------------------------------------- test_mihomo_easytier.py

func easytierSettings(extra ...any) *jsonx.Object {
	value := jsonx.Obj("enabled", true, "name", "EasyTier", "routes", []any{"10.144.144.0/24", "10.200.0.5/24"},
		"options", jsonx.Obj("network-name", "home", "network-secret", "s3cret-value",
			"peers", []any{"tcp://203.0.113.10:11010"}, "udp", true))
	for i := 0; i+1 < len(extra); i += 2 {
		value.Set(extra[i].(string), extra[i+1])
	}
	return value
}

func easytierConfig(settings any) *jsonx.Object {
	return jsonx.Obj("mihomo", jsonx.Obj("easytier", settings))
}

func baseDocument() *jsonx.Object {
	return jsonx.Obj("mixed-port", jsonx.Int(7890), "dns", jsonx.Obj("enable", true,
		"nameserver", []any{"https://223.5.5.5/dns-query"}), "rules", []any{"MATCH,DIRECT"})
}

// test_every_documented_outbound_option_is_accepted_and_written_verbatim
// (apply_easytier directly; the generate_mihomo_config round trip is C18's).
func TestEveryDocumentedOutboundOptionIsAcceptedAndWrittenVerbatim(t *testing.T) {
	options := mustJSON(t, `{"network-name":"home","network-secret":"secret","hostname":"phone-1","ipv4":"10.144.144.10/24",
		"dhcp":false,"peers":["tcp://203.0.113.10:11010","udp://[2001:db8::1]:11010"],"secure-mode":true,
		"local-private-key":"cHJpdmF0ZQ==","local-public-key":"cHVibGlj","listeners":["tcp://0.0.0.0:11010"],
		"mapped-listeners":["tcp://203.0.113.20:11010"],"exit-nodes":["10.144.144.1"],"proxy-networks":["192.168.50.0/24"],
		"instance-name":"inst","state-dir":"./easytier","udp":true,"accept-dns":false,"enable-exit-node":false,
		"enable-encryption":true,"encryption-algorithm":"aes-gcm","private-mode":true,"latency-first":true,
		"disable-p2p":false,"enable-kcp-proxy":true,"disable-kcp-input":false,"enable-quic-proxy":true,
		"disable-quic-input":false,"mtu":1380,"tld-dns-zone":"et.net.","dialer-proxy":"DIRECT","interface-name":"eth0",
		"routing-mark":6666,"ip-version":"ipv4-prefer"}`)
	keys := map[string]bool{"no-listener": true}
	for _, k := range options.Keys() {
		keys[k] = true
	}
	if len(keys) != len(EasyTierFields) {
		t.Fatal("FIELDS changed")
	}
	document := baseDocument()
	if _, err := ApplyEasyTier(document, easytierConfig(easytierSettings("options", options)), false, nil); err != nil {
		t.Fatal(err)
	}
	proxies := document.GetList("proxies")
	want := jsonx.Obj("name", "EasyTier", "type", "easytier")
	want.Update(options)
	if got := compactText(t, proxies[len(proxies)-1]); got != compactText(t, want) {
		t.Fatalf("got %s", got)
	}
}

func TestRoutesGoBeforeEveryRuleAndDisabledChangesNothing(t *testing.T) {
	document := baseDocument()
	if _, err := ApplyEasyTier(document, easytierConfig(easytierSettings()), false, nil); err != nil {
		t.Fatal(err)
	}
	rules := document.GetList("rules")
	if rules[0] != "IP-CIDR,10.144.144.0/24,EasyTier,no-resolve" || rules[1] != "IP-CIDR,10.200.0.0/24,EasyTier,no-resolve" ||
		rules[len(rules)-1] != "MATCH,DIRECT" {
		t.Fatal(rules)
	}
	off := baseDocument()
	result, err := ApplyEasyTier(off, easytierConfig(easytierSettings("enabled", false)), false, nil)
	if err != nil || result != nil || compactText(t, off) != compactText(t, baseDocument()) {
		t.Fatal("disabled changed the document")
	}
}

func TestKeepaliveGroupStartsTheLazyInstanceAndTakesTheRoutes(t *testing.T) {
	document := baseDocument()
	settings := easytierSettings("keepalive", jsonx.Obj("enabled", true, "url", "http://10.144.144.2/", "interval", jsonx.Int(60)))
	if _, err := ApplyEasyTier(document, easytierConfig(settings), false, nil); err != nil {
		t.Fatal(err)
	}
	groups := document.GetList("proxy-groups")
	if got := compactText(t, groups[len(groups)-1]); got != `{"name":"EasyTier 保活","type":"fallback","proxies":["EasyTier"],"url":"http://10.144.144.2/","interval":60}` {
		t.Fatal(got)
	}
	if document.GetList("rules")[0] != "IP-CIDR,10.144.144.0/24,EasyTier 保活,no-resolve" {
		t.Fatal(document.GetList("rules"))
	}
}

func TestMagicDNSRoutesTheZoneAndKeepsAUserPolicy(t *testing.T) {
	options := easytierSettings().GetObject("options")
	options.Set("tld-dns-zone", "lan.example.")
	settings := easytierSettings("magic_dns", true, "options", options)
	document := baseDocument()
	if _, err := ApplyEasyTier(document, easytierConfig(settings), false, nil); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range document.GetList("rules")[:3] {
		found = found || r == "DOMAIN-SUFFIX,lan.example,EasyTier"
	}
	if !found || compactText(t, document.GetObject("dns").Value("nameserver-policy")) != `{"+.lan.example":"et://EasyTier"}` {
		t.Fatal(compactText(t, document))
	}
	user := baseDocument()
	user.GetObject("dns").Set("nameserver-policy", jsonx.Obj("+.lan.example", "192.0.2.53"))
	if _, err := ApplyEasyTier(user, easytierConfig(settings), false, nil); err != nil {
		t.Fatal(err)
	}
	if compactText(t, user.GetObject("dns").Value("nameserver-policy")) != `{"+.lan.example":"192.0.2.53"}` {
		t.Fatal("user policy replaced")
	}
}

func TestTargetScope(t *testing.T) {
	for _, tc := range []struct {
		applyTo             string
		subscription, agent bool
	}{{"subscription", true, false}, {"agent", false, true}, {"all", true, true}} {
		for _, forAgent := range []bool{false, true} {
			document := baseDocument()
			result, err := ApplyEasyTier(document, easytierConfig(easytierSettings("apply_to", tc.applyTo)), forAgent, nil)
			want := tc.subscription
			if forAgent {
				want = tc.agent
			}
			if err != nil || (result != nil) != want || document.Has("proxies") != want {
				t.Fatalf("%s agent=%v: %v %v", tc.applyTo, forAgent, result, err)
			}
		}
	}
}

func TestOldAgentCoreIsRefusedAndUnknownVersionsPass(t *testing.T) {
	config := easytierConfig(easytierSettings("apply_to", "agent"))
	_, err := ApplyEasyTier(baseDocument(), config, true, "v1.19.30")
	var e *EasyTierSettingsError
	if !errors.As(err, &e) || !strings.Contains(e.Message, "v1.19.31") {
		t.Fatal(err)
	}
	for _, version := range []string{"v1.19.34", "alpha-abc"} {
		document := baseDocument()
		if _, err := ApplyEasyTier(document, config, true, version); err != nil {
			t.Fatal(err)
		}
		proxies := document.GetList("proxies")
		if proxies[len(proxies)-1].(*jsonx.Object).GetString("type") != "easytier" {
			t.Fatal(version)
		}
	}
}

func TestNameClashAndUnknownDialerAreRefused(t *testing.T) {
	document := jsonx.Obj("proxies", []any{jsonx.Obj("name", "EasyTier", "type", "ss")}, "proxy-groups", []any{}, "rules", []any{})
	_, err := ApplyEasyTier(document, easytierConfig(easytierSettings()), false, nil)
	if err == nil || !strings.Contains(err.Error(), "重名") {
		t.Fatal(err)
	}
	options := easytierSettings().GetObject("options")
	options.Set("dialer-proxy", "missing")
	_, err = ApplyEasyTier(jsonx.Obj("proxies", []any{}, "proxy-groups", []any{}, "rules", []any{}),
		easytierConfig(easytierSettings("options", options)), false, nil)
	if err == nil || !strings.Contains(err.Error(), "dialer-proxy") {
		t.Fatal(err)
	}
}

func TestInvalidSettingsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		change func(s *jsonx.Object)
		text   string
	}{
		{func(s *jsonx.Object) { s.GetObject("options").Delete("network-name") }, "network-name"},
		{func(s *jsonx.Object) { s.GetObject("options").Delete("peers") }, "peers"},
		{func(s *jsonx.Object) {
			s.GetObject("options").Set("listeners", []any{"tcp://0.0.0.0:11010"})
			s.GetObject("options").Set("no-listener", true)
		}, "no-listener"},
		{func(s *jsonx.Object) {
			s.GetObject("options").Set("proxy-networks", []any{"192.168.0.0/24->10.200.0.0/24"})
		}, "映射"},
		{func(s *jsonx.Object) { s.Set("routes", []any{"2001:db8::/64"}) }, "IPv4"},
		{func(s *jsonx.Object) { s.GetObject("options").Set("peers", []any{"203.0.113.10:11010"}) }, "格式不对"},
		{func(s *jsonx.Object) { s.GetObject("options").Set("mtu", jsonx.Int(100)) }, "mtu"},
		{func(s *jsonx.Object) { s.GetObject("options").Set("ip-version", "v4") }, "ip-version"},
		{func(s *jsonx.Object) { s.GetObject("options").Set("udp", "yes") }, "udp"},
		{func(s *jsonx.Object) { s.GetObject("options").Set("bogus", jsonx.Int(1)) }, "bogus"},
		{func(s *jsonx.Object) { s.GetObject("options").Set("exit-nodes", []any{"node-a"}) }, "exit-nodes"},
		{func(s *jsonx.Object) { s.GetObject("options").Set("local-public-key", "abc") }, "local-private-key"},
		{func(s *jsonx.Object) {
			s.Set("keepalive", jsonx.Obj("enabled", true, "url", "", "interval", jsonx.Int(300)))
		}, "检测地址"},
		{func(s *jsonx.Object) { s.Set("name", "a,b") }, "逗号"},
		{func(s *jsonx.Object) { s.Set("apply_to", "phones") }, "下发范围"},
	} {
		value := easytierSettings()
		tc.change(value)
		_, err := NormalizeEasyTierSettings(value)
		var e *EasyTierSettingsError
		if !errors.As(err, &e) || !strings.Contains(e.Message, tc.text) {
			t.Fatalf("%s: %v", tc.text, err)
		}
	}
}

func TestListeningWithoutPeersIsAllowedAndBlankValuesAreDropped(t *testing.T) {
	value := easytierSettings("options", jsonx.Obj("network-name", "home", "listeners", []any{"tcp://0.0.0.0:11010"},
		"hostname", " ", "peers", []any{}, "mtu", nil))
	got, err := NormalizeEasyTierSettings(value)
	if err != nil || compactText(t, got.Value("options")) != `{"network-name":"home","listeners":["tcp://0.0.0.0:11010"]}` {
		t.Fatal(err, compactText(t, got))
	}
	disabled, err := NormalizeEasyTierSettings(jsonx.Obj("enabled", false, "options", jsonx.NewObject()))
	if err != nil || disabled.Value("enabled") != false || disabled.GetObject("options").Len() != 0 {
		t.Fatal(err)
	}
}

func TestRedactedCopyKeepsShape(t *testing.T) {
	options := easytierSettings().GetObject("options")
	options.Set("local-private-key", "cHJpdg==")
	value, err := NormalizeEasyTierSettings(easytierSettings("options", options))
	if err != nil {
		t.Fatal(err)
	}
	redacted := RedactEasyTierSettings(value).(*jsonx.Object).GetObject("options")
	if redacted.GetString("network-secret") != "[REDACTED]" || redacted.GetString("local-private-key") != "[REDACTED]" ||
		redacted.GetString("network-name") != "home" || value.GetObject("options").GetString("network-secret") != "s3cret-value" {
		t.Fatal(compactText(t, redacted))
	}
}

// ---------------------------------------------------- documented differences

// Python sets of ports hold True as the port True (printed "True"); Go keys
// it as 1.
func TestBoolPortIsPortOne(t *testing.T) {
	used, err := MihomoUsedPorts(jsonx.Obj("port", true, "mixed-port", jsonx.Int(1)))
	if err != nil || len(used) != 1 || !used[jsonx.Int(1)] {
		t.Fatal(used, err)
	}
}

// The INFO lines convert_node_to_mihomo writes (logger backend.converters.mihomo).
func TestConversionLogLines(t *testing.T) {
	var buffer strings.Builder
	root, err := logx.Setup(logx.Config{Level: logx.Info, Console: &buffer})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		root.Close()
		quiet, _ := logx.Setup(logx.Config{Level: logx.Critical})
		_ = quiet
	}()
	ctx := context.Background()
	ConvertNodeToMihomo(ctx, jsonx.Obj("name", "节点", "proxy_string", "type: ss"), nil)
	ConvertNodeToMihomo(ctx, jsonx.Obj("name", jsonx.Int(5), "proxy_string", "ss://a@proxy.example:1"), &fakeConverter{})
	ConvertNodeToMihomo(ctx, jsonx.Obj("proxy_string", "ss://a@proxy.example:1"), &fakeConverter{})
	want := []string{
		" - backend.converters.mihomo - INFO - 节点 '节点' 为 JSON/YAML 格式，本地解析",
		" - backend.converters.mihomo - INFO - 节点 '5' 为 URI/base64 格式，使用 Sub-Store 的转换结果",
		" - backend.converters.mihomo - INFO - 节点 '' 为 URI/base64 格式，使用 Sub-Store 的转换结果",
	}
	lines := strings.Split(strings.TrimRight(buffer.String(), "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("lines %q", lines)
	}
	for i, line := range lines {
		if !strings.HasSuffix(line, want[i]) || len(line) != 23+len(want[i]) {
			t.Fatalf("line %d %q", i, line)
		}
	}
}
