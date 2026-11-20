package convert

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func mustJSON(t *testing.T, text string) *JObj {
	t.Helper()
	v, err := ParseJString(text)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*JObj)
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
	saved := parseProxyYAML
	parseProxyYAML = func(text string) (*yamlx.Node, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return saved(text)
	}
	savedSize, savedInput, savedResult, savedObjects, savedDepth :=
		proxyYAMLCacheSize, proxyYAMLMaxInputBytes, proxyYAMLMaxResultBytes, proxyYAMLMaxObjects, proxyYAMLMaxDepth
	t.Cleanup(func() {
		parseProxyYAML = saved
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
		if nodeRowIssue(row) != "" {
			continue
		} // covered by explicit typed validation/string cases
		text := row.args[0].(string)
		want := compactText(t, row.out)
		for i := 0; i < 2; i++ {
			node, err := StructuredProxy(text)
			var got any
			if err == nil && node != nil {
				got, err = JFromYAMLNode(node)
			}
			if nativeProxyReplay(t, "corpus", row, got, nil, err) {
				continue
			}
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
	first := parsedProxyNodeForTest(nestedProxy)
	yamlx.Set(yamlx.Get(yamlx.Get(first, "ws-opts"), "headers"), "Host", yamlx.Str("changed.invalid"))
	alpn := yamlx.Get(first, "alpn")
	alpn.Content = append(alpn.Content, yamlx.Str("changed"))
	yamlx.Set(first, "dialer-proxy", yamlx.Str("changed"))
	later := parsedProxyNodeForTest(nestedProxy)
	if scalarText(yamlx.Get(yamlx.Get(yamlx.Get(later, "ws-opts"), "headers"), "Host")) != "edge.example" ||
		len(yamlx.Get(later, "alpn").Content) != 1 || yamlx.Get(later, "dialer-proxy") != nil {
		t.Fatalf("cache polluted: %v", later)
	}
	ctx := context.Background()
	src := nestedProxy
	one, _ := NodeProxy(ctx, model.Node{Name: "one", ProxyString: &src}, nil)
	two, _ := NodeProxy(ctx, model.Node{Name: "two", ProxyString: &src}, nil)
	if scalarText(yamlx.Get(one, "name")) != "one" || scalarText(yamlx.Get(two, "name")) != "two" {
		t.Fatal("names")
	}
	if scalarText(yamlx.Get(parsedProxyNodeForTest(nestedProxy), "name")) != "original" {
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
		if got := scalarText(yamlx.Get(parsedProxyNodeForTest(text), "server")); got != want {
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
	if scalarText(yamlx.Get(parsedProxyNodeForTest(b), "server")) != "b.example" || *calls != 4 || cacheLen() != 2 {
		t.Fatal("b reload")
	}
}

func TestURIAndValidJSONDoNotTouchYAMLCache(t *testing.T) {
	isolateCache(t)
	parseProxyYAML = func(string) (*yamlx.Node, error) { panic("YAML path entered") }
	if parsedProxyNodeForTest("ss://sample@proxy.example:443") != nil {
		t.Fatal("uri")
	}
	if got := compactText(t, jNodeOrNil(parsedProxyNodeForTest(`{"type":"ss","port":443}`))); got != `{"type":"ss","port":443}` {
		t.Fatal(got)
	}
	if cacheLen() != 0 {
		t.Fatal("cache touched")
	}
}

func TestFailedAndNonProxyResultsNotRetained(t *testing.T) {
	for _, text := range []string{"type: [", "null", "name: sample", "type: false"} {
		calls := isolateCache(t)
		if parsedProxyNodeForTest(text) != nil || parsedProxyNodeForTest(text) != nil {
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
	parseProxyYAML = func(text string) (*yamlx.Node, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("temporary failure")
		}
		return yamlx.Parse([]byte(text))
	}
	if parsedProxyNodeForTest("type: ss") != nil {
		t.Fatal("first")
	}
	for i := 0; i < 2; i++ {
		if got := compactText(t, jNodeOrNil(parsedProxyNodeForTest("type: ss"))); got != `{"type":"ss"}` {
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
			if got := scalarText(yamlx.Get(parsedProxyNodeForTest(text+tc.extra), "name")); got != "abc"+tc.extra {
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
		if scalarText(yamlx.Get(parsedProxyNodeForTest(text), "name")) != strings.Repeat("中", 10) {
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
		want  any
	}{
		{&proxyYAMLMaxResultBytes, 100, "type: ss\ntext: " + strings.Repeat("x", 200), JObjKV("type", "ss", "text", strings.Repeat("x", 200))},
		{&proxyYAMLMaxObjects, 5, "type: ss\nvalues: [a, b, c, d, e, f]", JObjKV("type", "ss", "values", []any{"a", "b", "c", "d", "e", "f"})},
		{&proxyYAMLMaxDepth, 2, "type: ss\nvalues: [[[item]]]", JObjKV("type", "ss", "values", []any{[]any{[]any{"item"}}})},
	} {
		calls := isolateCache(t)
		want := compactText(t, tc.want)
		*tc.limit = tc.value
		for i := 0; i < 2; i++ {
			if got := compactText(t, jNodeOrNil(parsedProxyNodeForTest(tc.text))); got != want {
				t.Fatalf("%q: %s", tc.text, got)
			}
		}
		if *calls != 2 || cacheLen() != 0 {
			t.Fatalf("%q: calls %d len %d", tc.text, *calls, cacheLen())
		}
	}
}

// Native scalar policy keeps timestamps and binary as text, standard tagged
// collections as their node shape, and YAML 1.2-style yes as a string key.
func TestNativeYAMLScalarBoundary(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   *JObj
	}{
		{"type: ss\nvalue: 2026-10-17", JObjKV("type", "ss", "value", "2026-10-17")},
		{"type: ss\nvalue: !!binary aGVsbG8=", JObjKV("type", "ss", "value", "aGVsbG8=")},
		{"type: ss\nyes: first\ntrue: second", JObjKV("type", "ss", "yes", "first", "true", "second")},
		{"type: ss\nvalue: !!set {a: null, b: null}", JObjKV("type", "ss", "value", JObjKV("a", nil, "b", nil))},
	} {
		isolateCache(t)
		for i := 0; i < 2; i++ {
			node, err := StructuredProxy(tc.source)
			if err != nil {
				t.Fatalf("%s: %v", tc.source, err)
			}
			got, err := JFromYAMLNode(node)
			if err != nil || !EqualJ(got, tc.want) {
				t.Fatalf("%s: %v %v", tc.source, got, err)
			}
		}
	}
}

// test_aliases_and_cycles_keep_identity_and_bypass_cache: shared containers
// bypass the cache (Go keeps the shared *yamlx.Node of an alias; a recursive
// node is a yamlx error, so the string goes to Sub-Store — documented).
func TestAliasesAndCyclesBypassCache(t *testing.T) {
	calls := isolateCache(t)
	alias := "type: ss\na: &shared {x: 1}\nb: *shared"
	for i := 0; i < 2; i++ {
		result := parsedProxyNodeForTest(alias)
		if yamlx.Resolve(yamlx.Get(result, "a")) != yamlx.Resolve(yamlx.Get(result, "b")) {
			t.Fatal("alias identity lost")
		}
		if parsedProxyNodeForTest("&root {type: ss, self: *root}") != nil {
			t.Fatal("recursive node")
		}
	}
	if *calls != 4 || cacheLen() != 0 {
		t.Fatalf("calls %d len %d", *calls, cacheLen())
	}
}

func TestAliasDiamondDoesNotExpandDuringCacheScreening(t *testing.T) {
	isolateCache(t)
	var b strings.Builder
	b.WriteString("type: ss\na0: &a0 {v: item}\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "a%d: &a%d {l: *a%d, r: *a%d}\n", i, i, i-1, i-1)
	}
	if _, err := StructuredProxy(b.String()); !errors.Is(err, yamlx.ErrAliasBudget) {
		t.Fatalf("expected alias budget: %v", err)
	}
	if cacheLen() != 0 {
		t.Fatal("unsafe document cached")
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestConcurrentColdCallsParseOnceAndReturnPrivateResults(t *testing.T) {
	calls := isolateCache(t)
	var wg sync.WaitGroup
	results := make([]*yamlx.Node, 8)
	for i := range results {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r := parsedProxyNodeForTest(nestedProxy)
			yamlx.Set(yamlx.Get(yamlx.Get(r, "ws-opts"), "headers"), "Host", yamlx.Str(itoa(n)))
			results[n] = r
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if scalarText(yamlx.Get(yamlx.Get(yamlx.Get(r, "ws-opts"), "headers"), "Host")) != itoa(i) {
			t.Fatal("shared result")
		}
	}
	if *calls != 1 {
		t.Fatalf("calls %d", *calls)
	}
	if scalarText(yamlx.Get(yamlx.Get(yamlx.Get(parsedProxyNodeForTest(nestedProxy), "ws-opts"), "headers"), "Host")) != "edge.example" {
		t.Fatal("cache polluted")
	}
}

// test_deepcopy_does_not_hold_cache_lock is structural in Go: loadProxyYAML
// releases proxyYAMLMu before cloning. Checked by taking the lock from
// another goroutine while results are copied in a loop.
func TestDeepCopyDoesNotHoldCacheLock(t *testing.T) {
	isolateCache(t)
	parsedProxyNodeForTest(nestedProxy)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			parsedProxyNodeForTest(nestedProxy)
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
	kept     map[string][]byte
}

func (c *countingConverter) ConvertProxy(_ context.Context, s string) (*yamlx.Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if kept, ok := c.kept[s]; ok {
		return yamlx.Parse(kept)
	}
	c.requests++
	proxy := yamlx.Map(
		yamlx.Str("name"), yamlx.Str("from-uri"),
		yamlx.Str("type"), yamlx.Str("anytls"),
		yamlx.Str("server"), yamlx.Str("node.example.test"),
		yamlx.Str("port"), yamlx.Int(443),
		yamlx.Str("password"), yamlx.Str("secret"),
	)
	data, err := yamlx.Marshal(proxy)
	if err != nil {
		return nil, err
	}
	c.kept[s] = data
	return proxy, nil
}

// test_rendering_again_does_not_start_sub_store: the converted node is named
// by each caller and the caller's copy is private. (The client-side cache
// itself is C13's.)
func TestRenderingAgainDoesNotStartSubStore(t *testing.T) {
	ctx := context.Background()
	node := "anytls://secret@node.example.test:443?sni=example.test#one"
	conv := &countingConverter{kept: map[string][]byte{}}
	first, _ := NodeProxy(ctx, model.Node{Name: "Home", ProxyString: &node}, conv)
	second, _ := NodeProxy(ctx, model.Node{Name: "Home", ProxyString: &node}, conv)
	third, _ := NodeProxy(ctx, model.Node{Name: "Away", ProxyString: &node}, conv)
	if conv.requests != 1 {
		t.Fatalf("requests %d", conv.requests)
	}
	firstJ := jNodeOrNil(first).(*JObj)
	secondJ := jNodeOrNil(second).(*JObj)
	thirdJ := jNodeOrNil(third).(*JObj)
	if compactText(t, firstJ) != compactText(t, secondJ) || firstJ.GetString("name") != "Home" ||
		firstJ.GetString("server") != "node.example.test" {
		t.Fatal(compactText(t, firstJ))
	}
	want := firstJ.DeepCopy()
	want.Set("name", "Away")
	if compactText(t, thirdJ) != compactText(t, want) {
		t.Fatal(compactText(t, thirdJ))
	}
}

// node_conversions(results, reuse=True) answers from the record without
// asking Sub-Store, including a recorded failure.
func TestNodeConversionsReuse(t *testing.T) {
	ctx := context.Background()
	conv := &fakeConverter{}
	results := NewConversions()
	scoped := WithConversions(ctx, results, false)
	for _, s := range []string{"ss://a@proxy.example:1", "ss://fail@proxy.example:1"} {
		ps := s
		_, _ = NodeProxy(scoped, model.Node{Name: "n", ProxyString: &ps}, conv)
	}
	if len(conv.calls) != 2 || results.Len() != 2 {
		t.Fatal(conv.calls)
	}
	reuse := WithConversions(ctx, results, true)
	s1 := "ss://a@proxy.example:1"
	s2 := "ss://fail@proxy.example:1"
	p, _ := NodeProxy(reuse, model.Node{Name: "again", ProxyString: &s1}, conv)
	failed, _ := NodeProxy(reuse, model.Node{Name: "again", ProxyString: &s2}, conv)
	if len(conv.calls) != 2 || scalarText(yamlx.Get(p, "name")) != "again" || failed != nil {
		t.Fatal("reuse asked Sub-Store again")
	}
	// Without reuse the same scope records again; outside a scope nothing is recorded.
	s3 := "ss://b@proxy.example:1"
	_, _ = NodeProxy(ctx, model.Node{Name: "x", ProxyString: &s3}, conv)
	if results.Len() != 2 {
		t.Fatal("recorded outside the scope")
	}
}

// ------------------------------------------------------------ dialer tests

func dialerNode(id string, extra ...any) *JObj {
	n := JObjKV("id", id, "name", id, "enabled", true, "type", "http", "server", "example.test", "port", JInt(80))
	for i := 0; i+1 < len(extra); i += 2 {
		n.Set(extra[i].(string), extra[i+1])
	}
	return n
}

func wantDialerError(t *testing.T, err error, fragment string) {
	t.Helper()
	err = DialerFailure(err)
	var d *DialerReferenceError
	if !errors.As(err, &d) || !strings.Contains(d.Message, fragment) {
		t.Fatalf("want DialerReferenceError containing %q, got %v", fragment, err)
	}
}

// test_dialer_provider_scope.py::test_raw_dialer_dynamic_group_checks_real_candidate_edges
func TestRawDialerDynamicGroupChecksRealCandidateEdges(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, cycle := range []bool{false, true} {
			groups := []any{JObjKV("name", "Dynamic", "use", []any{"Feed"})}
			if nested {
				groups = append(groups, JObjKV("name", "Outer", "proxies", []any{"Dynamic"}))
			}
			target := "Dynamic"
			if nested {
				target = "Outer"
			}
			main, err := YAMLNodeFromJ(JObjKV("proxies", []any{JObjKV("name", "Exit", "dialer-proxy", target)},
				"proxy-groups", groups, "proxy-providers", JObjKV("Feed", NewJObj())))
			if err != nil {
				t.Fatal(err)
			}
			candidate := JObjKV("name", "Remote")
			if cycle {
				candidate.Set("dialer-proxy", target)
			}
			candNode, err := YAMLNodeFromJ(candidate)
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateTopology(main, TopologyOptions{
				ProviderProxies:  map[string][]*yamlx.Node{"Feed": {candNode}},
				RequireProviders: true,
			})
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
		main, err := YAMLNodeFromJ(JObjKV("proxies", []any{JObjKV("name", "Remote", "dialer-proxy", target)},
			"proxy-groups", []any{JObjKV("name", "Relay", "proxies", []any{"DIRECT"}),
				JObjKV("name", "UnrelatedDynamic", "use", []any{"Feed"})},
			"proxy-providers", JObjKV("Feed", NewJObj())))
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateTopology(main, TopologyOptions{}); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
	}
}

// test_dialer_provider_scope.py::test_no_chain_dynamic_group_legacy_allowed
func TestNoChainDynamicGroupLegacyAllowed(t *testing.T) {
	main, err := YAMLNodeFromJ(JObjKV("proxies", []any{JObjKV("name", "Remote")},
		"proxy-groups", []any{JObjKV("name", "Entry", "use", []any{"Feed"})},
		"proxy-providers", JObjKV("Feed", NewJObj())))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTopology(main, TopologyOptions{}); err != nil {
		t.Fatal(err)
	}
}

// test_dialer_review_fixes.py::test_emitted_dialer_graph_checks_ordinary_group_members
func TestEmittedDialerGraphChecksOrdinaryGroupMembers(t *testing.T) {
	main, err := YAMLNodeFromJ(JObjKV("proxies", []any{JObjKV("name", "exit", "dialer-proxy", "relay"), JObjKV("name", "relay")},
		"proxy-groups", []any{JObjKV("name", "Entry", "proxies", []any{"gone"})}))
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateTopology(main, TopologyOptions{})
	wantDialerError(t, err, "未生成")
}

// test_dialer_round2.py::test_disabled_managed_source_keeps_manual_only_closure_contract
func TestDisabledManagedSourceKeepsManualOnlyClosureContract(t *testing.T) {
	source := dialerNode("source", "dialer_ref", JObjKV("type", "node", "id", "relay"))
	source.Set("enabled", false)
	cfg, err := DecodeDialerConfigJ(JObjKV("nodes", []any{source, dialerNode("relay", "subscription_id", "s")}))
	if err != nil {
		t.Fatal(err)
	}
	wantDialerError(t, ConfigDialers(cfg), "手动")
}

func deepNodes(cycle bool) []any {
	var nodes []any
	for i := 0; i < 1100; i++ {
		if i < 1099 || cycle {
			nodes = append(nodes, dialerNode(itoa(i), "dialer_ref", JObjKV("type", "node", "id", itoa((i+1)%1100))))
		} else {
			nodes = append(nodes, dialerNode(itoa(i)))
		}
	}
	return nodes
}

// test_dialer_round2.py::test_deep_graph_validation_and_closure_are_stack_safe
func TestDeepGraphValidationAndClosureAreStackSafe(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		profile := JObjKV("nodes", deepNodes(cycle))
		cfg, err := DecodeDialerConfigJ(profile)
		if err != nil {
			t.Fatal(err)
		}
		err = ConfigDialers(cfg)
		if cycle {
			wantDialerError(t, err, "循环")
		} else {
			if err != nil {
				t.Fatal(err)
			}
			ids, err := RequiredNodes(cfg, []string{"0"}, true)
			if err != nil || len(ids) != 1100 {
				t.Fatalf("%d %v", len(ids), err)
			}
		}
		var proxies []any
		for _, n := range profile.GetList("nodes") {
			node := n.(*JObj)
			p := JObjKV("name", node.Value("name"))
			if ref := node.GetObject("dialer_ref"); ref != nil {
				p.Set("dialer-proxy", ref.Value("id"))
			}
			proxies = append(proxies, p)
		}
		main, err := YAMLNodeFromJ(JObjKV("proxies", proxies))
		if err != nil {
			t.Fatal(err)
		}
		err = ValidateTopology(main, TopologyOptions{})
		if cycle {
			wantDialerError(t, err, "循环")
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

// --------------------------------------------------- test_mihomo_easytier.py

func easytierSettings(extra ...any) *JObj {
	value := JObjKV("enabled", true, "name", "EasyTier", "routes", []any{"10.144.144.0/24", "10.200.0.5/24"},
		"options", JObjKV("network-name", "home", "network-secret", "s3cret-value",
			"peers", []any{"tcp://203.0.113.10:11010"}, "udp", true))
	for i := 0; i+1 < len(extra); i += 2 {
		value.Set(extra[i].(string), extra[i+1])
	}
	return value
}

func easytierConfig(t *testing.T, settings *JObj) *model.Config {
	t.Helper()
	s, err := DecodeJ[model.EasyTierSettings](settings)
	if err != nil {
		t.Fatal(err)
	}
	return &model.Config{Profile: &model.Profile{Mihomo: model.MihomoSettings{EasyTier: &s}}}
}

func baseDocumentNode(t *testing.T) *yamlx.Node {
	t.Helper()
	doc, err := YAMLNodeFromJ(JObjKV("mixed-port", JInt(7890), "dns", JObjKV("enable", true,
		"nameserver", []any{"https://223.5.5.5/dns-query"}), "rules", []any{"MATCH,DIRECT"}))
	if err != nil {
		t.Fatal(err)
	}
	return doc
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
	document := baseDocumentNode(t)
	if _, err := ApplyEasyTierNode(document, easytierConfig(t, easytierSettings("options", options)), false, nil); err != nil {
		t.Fatal(err)
	}
	docJ := jNodeOrNil(document).(*JObj)
	proxies := docJ.GetList("proxies")
	want := JObjKV("name", "EasyTier", "type", "easytier")
	want.Update(options)
	if got := compactText(t, proxies[len(proxies)-1]); got != compactText(t, want) {
		t.Fatalf("got %s", got)
	}
}

func TestRoutesGoBeforeEveryRuleAndDisabledChangesNothing(t *testing.T) {
	document := baseDocumentNode(t)
	if _, err := ApplyEasyTierNode(document, easytierConfig(t, easytierSettings()), false, nil); err != nil {
		t.Fatal(err)
	}
	rules := jNodeOrNil(document).(*JObj).GetList("rules")
	if rules[0] != "IP-CIDR,10.144.144.0/24,EasyTier,no-resolve" || rules[1] != "IP-CIDR,10.200.0.0/24,EasyTier,no-resolve" ||
		rules[len(rules)-1] != "MATCH,DIRECT" {
		t.Fatal(rules)
	}
	off := baseDocumentNode(t)
	result, err := ApplyEasyTierNode(off, easytierConfig(t, easytierSettings("enabled", false)), false, nil)
	if err != nil || result != nil || compactText(t, jNodeOrNil(off)) != compactText(t, jNodeOrNil(baseDocumentNode(t))) {
		t.Fatal("disabled changed the document")
	}
}

func TestKeepaliveGroupStartsTheLazyInstanceAndTakesTheRoutes(t *testing.T) {
	document := baseDocumentNode(t)
	settings := easytierSettings("keepalive", JObjKV("enabled", true, "url", "http://10.144.144.2/", "interval", JInt(60)))
	if _, err := ApplyEasyTierNode(document, easytierConfig(t, settings), false, nil); err != nil {
		t.Fatal(err)
	}
	docJ := jNodeOrNil(document).(*JObj)
	groups := docJ.GetList("proxy-groups")
	if got := compactText(t, groups[len(groups)-1]); got != `{"name":"EasyTier 保活","type":"fallback","proxies":["EasyTier"],"url":"http://10.144.144.2/","interval":60}` {
		t.Fatal(got)
	}
	if docJ.GetList("rules")[0] != "IP-CIDR,10.144.144.0/24,EasyTier 保活,no-resolve" {
		t.Fatal(docJ.GetList("rules"))
	}
}

func TestMagicDNSRoutesTheZoneAndKeepsAUserPolicy(t *testing.T) {
	options := easytierSettings().GetObject("options")
	options.Set("tld-dns-zone", "lan.example.")
	settings := easytierSettings("magic_dns", true, "options", options)
	document := baseDocumentNode(t)
	if _, err := ApplyEasyTierNode(document, easytierConfig(t, settings), false, nil); err != nil {
		t.Fatal(err)
	}
	docJ := jNodeOrNil(document).(*JObj)
	found := false
	for _, r := range docJ.GetList("rules")[:3] {
		found = found || r == "DOMAIN-SUFFIX,lan.example,EasyTier"
	}
	if !found || compactText(t, docJ.GetObject("dns").Value("nameserver-policy")) != `{"+.lan.example":"et://EasyTier"}` {
		t.Fatal(compactText(t, docJ))
	}
	user := baseDocumentNode(t)
	yamlx.Set(yamlx.Get(user, "dns"), "nameserver-policy", yamlx.Map(yamlx.Str("+.lan.example"), yamlx.Str("192.0.2.53")))
	if _, err := ApplyEasyTierNode(user, easytierConfig(t, settings), false, nil); err != nil {
		t.Fatal(err)
	}
	userJ := jNodeOrNil(user).(*JObj)
	if compactText(t, userJ.GetObject("dns").Value("nameserver-policy")) != `{"+.lan.example":"192.0.2.53"}` {
		t.Fatal("user policy replaced")
	}
}

func TestTargetScope(t *testing.T) {
	for _, tc := range []struct {
		applyTo             string
		subscription, agent bool
	}{{"subscription", true, false}, {"agent", false, true}, {"all", true, true}} {
		for _, forAgent := range []bool{false, true} {
			document := baseDocumentNode(t)
			result, err := ApplyEasyTierNode(document, easytierConfig(t, easytierSettings("apply_to", tc.applyTo)), forAgent, nil)
			want := tc.subscription
			if forAgent {
				want = tc.agent
			}
			if err != nil || (result != nil) != want || (yamlx.Get(document, "proxies") != nil) != want {
				t.Fatalf("%s agent=%v: %v %v", tc.applyTo, forAgent, result, err)
			}
		}
	}
}

func TestOldAgentCoreAndInvalidVersionsAreRefused(t *testing.T) {
	config := easytierConfig(t, easytierSettings("apply_to", "agent"))
	_, err := ApplyEasyTierNode(baseDocumentNode(t), config, true, json.RawMessage(`"v1.19.30"`))
	if err == nil || !strings.Contains(err.Error(), "v1.19.31") {
		t.Fatal(err)
	}
	_, err = ApplyEasyTierNode(baseDocumentNode(t), config, true, json.RawMessage(`"alpha-abc"`))
	if err == nil || !strings.Contains(err.Error(), "alpha-abc") {
		t.Fatal("invalid nonempty Agent version was not refused", err)
	}
	for _, version := range []json.RawMessage{json.RawMessage(`"v1.19.34"`), json.RawMessage(`""`)} {
		document := baseDocumentNode(t)
		if _, err := ApplyEasyTierNode(document, config, true, version); err != nil {
			t.Fatal(err)
		}
		proxies := jNodeOrNil(document).(*JObj).GetList("proxies")
		if proxies[len(proxies)-1].(*JObj).GetString("type") != "easytier" {
			t.Fatal(string(version))
		}
	}
}

func TestNameClashAndUnknownDialerAreRefused(t *testing.T) {
	document, err := YAMLNodeFromJ(JObjKV("proxies", []any{JObjKV("name", "EasyTier", "type", "ss")}, "proxy-groups", []any{}, "rules", []any{}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ApplyEasyTierNode(document, easytierConfig(t, easytierSettings()), false, nil)
	if err == nil || !strings.Contains(err.Error(), "重名") {
		t.Fatal(err)
	}
	options := easytierSettings().GetObject("options")
	options.Set("dialer-proxy", "missing")
	emptyDoc, err := YAMLNodeFromJ(JObjKV("proxies", []any{}, "proxy-groups", []any{}, "rules", []any{}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ApplyEasyTierNode(emptyDoc, easytierConfig(t, easytierSettings("options", options)), false, nil)
	if err == nil || !strings.Contains(err.Error(), "dialer-proxy") {
		t.Fatal(err)
	}
}

func TestInvalidSettingsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		change func(s *JObj)
		text   string
	}{
		{func(s *JObj) { s.GetObject("options").Delete("network-name") }, "network-name"},
		{func(s *JObj) { s.GetObject("options").Delete("peers") }, "peers"},
		{func(s *JObj) {
			s.GetObject("options").Set("listeners", []any{"tcp://0.0.0.0:11010"})
			s.GetObject("options").Set("no-listener", true)
		}, "no-listener"},
		{func(s *JObj) {
			s.GetObject("options").Set("proxy-networks", []any{"192.168.0.0/24->10.200.0.0/24"})
		}, "映射"},
		{func(s *JObj) { s.Set("routes", []any{"2001:db8::/64"}) }, "IPv4"},
		{func(s *JObj) { s.GetObject("options").Set("peers", []any{"203.0.113.10:11010"}) }, "格式不对"},
		{func(s *JObj) { s.GetObject("options").Set("mtu", JInt(100)) }, "mtu"},
		{func(s *JObj) { s.GetObject("options").Set("ip-version", "v4") }, "ip-version"},
		{func(s *JObj) { s.GetObject("options").Set("udp", "yes") }, "udp"},
		{func(s *JObj) { s.GetObject("options").Set("bogus", JInt(1)) }, "bogus"},
		{func(s *JObj) { s.GetObject("options").Set("exit-nodes", []any{"node-a"}) }, "exit-nodes"},
		{func(s *JObj) { s.GetObject("options").Set("local-public-key", "abc") }, "local-private-key"},
		{func(s *JObj) {
			s.Set("keepalive", JObjKV("enabled", true, "url", "", "interval", JInt(300)))
		}, "检测地址"},
		{func(s *JObj) { s.Set("name", "a,b") }, "逗号"},
		{func(s *JObj) { s.Set("apply_to", "phones") }, "下发范围"},
	} {
		value := easytierSettings()
		tc.change(value)
		s, err := DecodeJ[model.EasyTierSettings](value)
		if err != nil {
			t.Fatal(err)
		}
		_, err = NormalizeEasyTier(&s)
		if err == nil || !strings.Contains(err.Error(), tc.text) {
			t.Fatalf("%s: %v", tc.text, err)
		}
	}
}

func TestListeningWithoutPeersIsAllowedAndBlankValuesAreDropped(t *testing.T) {
	value := easytierSettings("options", JObjKV("network-name", "home", "listeners", []any{"tcp://0.0.0.0:11010"},
		"hostname", " ", "peers", []any{}, "mtu", nil))
	s, err := DecodeJ[model.EasyTierSettings](value)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NormalizeEasyTier(&s)
	if err != nil {
		t.Fatal(err)
	}
	gotJ, err := modelToJ(got)
	if err != nil || compactText(t, gotJ.(*JObj).Value("options")) != `{"network-name":"home","listeners":["tcp://0.0.0.0:11010"]}` {
		t.Fatal(err, compactText(t, gotJ))
	}
	sOff, err := DecodeJ[model.EasyTierSettings](JObjKV("enabled", false, "options", NewJObj()))
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := NormalizeEasyTier(&sOff)
	if err != nil {
		t.Fatal(err)
	}
	disJ, err := modelToJ(disabled)
	if err != nil || disJ.(*JObj).Value("enabled") != false || disJ.(*JObj).GetObject("options").Len() != 0 {
		t.Fatal(err)
	}
}

func TestRedactedCopyKeepsShape(t *testing.T) {
	options := easytierSettings().GetObject("options")
	options.Set("local-private-key", "cHJpdg==")
	s, err := DecodeJ[model.EasyTierSettings](easytierSettings("options", options))
	if err != nil {
		t.Fatal(err)
	}
	value, err := NormalizeEasyTier(&s)
	if err != nil {
		t.Fatal(err)
	}
	redactedModel, err := RedactEasyTier(value)
	if err != nil {
		t.Fatal(err)
	}
	redactedJ, err := modelToJ(redactedModel)
	if err != nil {
		t.Fatal(err)
	}
	valJ, err := modelToJ(value)
	if err != nil {
		t.Fatal(err)
	}
	redacted := redactedJ.(*JObj).GetObject("options")
	if redacted.GetString("network-secret") != "[REDACTED]" || redacted.GetString("local-private-key") != "[REDACTED]" ||
		redacted.GetString("network-name") != "home" || valJ.(*JObj).GetObject("options").GetString("network-secret") != "s3cret-value" {
		t.Fatal(compactText(t, redacted))
	}
}

// ---------------------------------------------------- documented differences

// Python sets of ports hold True as the port True (printed "True"); Go only
// counts integer ports.
func TestBoolPortIsPortOne(t *testing.T) {
	used := UsedPorts(yamlx.Map(yamlx.Str("port"), yamlx.Bool(true), yamlx.Str("mixed-port"), yamlx.Int(1)))
	if len(used) != 1 || !used[1] {
		t.Fatal(used)
	}
}

// The INFO lines NodeProxy writes (logger backend.converters.mihomo).
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
	ss := "type: ss"
	uri := "ss://a@proxy.example:1"
	_, _ = NodeProxy(ctx, model.Node{Name: "节点", ProxyString: &ss}, nil)
	_, _ = NodeProxy(ctx, model.Node{Name: "5", ProxyString: &uri}, &fakeConverter{})
	_, _ = NodeProxy(ctx, model.Node{ProxyString: &uri}, &fakeConverter{})
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

// parsedProxyNodeForTest exposes the parsed node; policy errors are
// asserted separately by the native-reader boundary tests.
func parsedProxyNodeForTest(text string) *yamlx.Node {
	value, _ := StructuredProxy(text)
	return value
}
