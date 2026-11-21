package convert

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

// fakeConverter mirrors fake_convert of testdata/gen.py.
type fakeConverter struct{ calls []string }

func (f *fakeConverter) ConvertProxy(_ context.Context, s string) (*yamlx.Node, error) {
	f.calls = append(f.calls, s)
	if strings.Contains(s, "fail") {
		return nil, nil
	}
	if strings.Contains(s, "empty") {
		return yamlx.Map(), nil
	}
	proxy := yamlx.Map(
		yamlx.Str("name"), yamlx.Str("from-uri"),
		yamlx.Str("type"), yamlx.Str("http"),
		yamlx.Str("server"), yamlx.Str("fixture.test"),
		yamlx.Str("port"), yamlx.Int(80),
	)
	if _, after, ok := strings.Cut(s, "dial-"); ok {
		target, _, _ := strings.Cut(after, "&")
		yamlx.Set(proxy, "dialer-proxy", yamlx.Str(target))
	}
	if strings.Contains(s, "vless") {
		yamlx.Set(proxy, "type", yamlx.Str("vless"))
		yamlx.Set(proxy, "reality-opts", yamlx.Map(yamlx.Str("public-key"), yamlx.Str("k")))
	}
	return proxy, nil
}

type corpusRow struct {
	fn       string
	args     []any
	out      any
	hasOut   bool
	errClass string
	errMsg   string
	mutated  any
	hasMut   bool
}

func loadCorpus(t *testing.T) []corpusRow {
	t.Helper()
	file, err := os.Open("testdata/corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	var rows []corpusRow
	for scanner.Scan() {
		value, err := ParseJ(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		entry := value.(*JObj)
		row := corpusRow{fn: entry.GetString("fn"), args: entry.GetList("args")}
		row.out, row.hasOut = entry.Get("out")
		if e := entry.GetList("err"); e != nil {
			row.errClass, row.errMsg = e[0].(string), e[1].(string)
		}
		row.mutated, row.hasMut = entry.Get("mutated")
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func compactText(t *testing.T, v any) string {
	t.Helper()
	b, err := CompactJ(v)
	if err != nil {
		t.Fatalf("encode %#v: %v", v, err)
	}
	return string(b)
}

// pyConv mirrors conv() of gen.py for Go results.
func pyConv(v any) any {
	switch value := v.(type) {
	case *JObj:
		if value == nil {
			return nil
		}
		out := NewJObj()
		for k, child := range value.All() {
			out.Set(k, pyConv(child))
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = pyConv(child)
		}
		return out
	case []byte:
		return JObjKV("__bytes__", hex.EncodeToString(value))
	}
	return v
}

// sortedSet is conv() of a Python set: sorted by json.dumps of the items.
func sortedSet(items []any) []any {
	keyed := make([]struct {
		key  string
		item any
	}, len(items))
	for i, item := range items {
		b, _ := EncodeJ(item, true, true)
		keyed[i].key, keyed[i].item = string(b), item
	}
	sort.Slice(keyed, func(i, j int) bool { return keyed[i].key < keyed[j].key })
	out := make([]any, len(items))
	for i := range keyed {
		out[i] = keyed[i].item
	}
	return out
}

func refsOut(refs []Ref) []any {
	out := make([]any, 0, len(refs))
	for _, r := range refs {
		out = append(out, []any{r.Kind, r.ID})
	}
	return out
}

func obj(v any) *JObj {
	o, _ := v.(*JObj)
	return o
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func modelToJ(v any) (any, error) {
	data, err := model.Marshal(v)
	if err != nil {
		return nil, err
	}
	return ParseJ(data)
}

// runCorpusRow calls the typed Go port of row.fn; it returns the conv() result and
// the mutated argument.
func runCorpusRow(row corpusRow) (result any, mutated any, err error) {
	ctx := context.Background()
	conv := &fakeConverter{}
	a := row.args
	switch row.fn {
	case "udp_fallback":
		cfg, err := DecodeRulesConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		s := FallbackPolicy(cfg)
		if s == "" {
			return nil, nil, nil
		}
		return s, nil, nil
	case "ntp_direct":
		cfg, err := DecodeRulesConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		return DirectNTP(cfg), nil, nil
	case "emitted_rules":
		cfg, err := DecodeRulesConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		rules := RulesForOutput(cfg)
		if len(rules) == 0 {
			return []any{}, nil, nil
		}
		out, err := modelToJ(rules)
		return out, nil, err
	case "after_match_comment":
		rule, err := DecodeJ[model.RuleConfig](a[0])
		if err != nil {
			return nil, nil, err
		}
		return RuleComment(rule), nil, nil
	case "network_as_protocol":
		x, y := NetworkAsProtocol(str(a[0]), str(a[1]))
		return []any{x, y}, nil, nil
	case "has_port_list":
		return HasPortList(str(a[0]), str(a[1])), nil, nil
	case "set_general_option":
		return SetGeneralOption(str(a[0]), str(a[1]), str(a[2])), nil, nil
	case "ruleset_format":
		rule, err := DecodeJ[model.RuleConfig](a[0])
		if err != nil {
			return nil, nil, err
		}
		cfg, err := DecodeRuleLibraryConfigJ(obj(a[1]))
		if err != nil {
			return nil, nil, err
		}
		f, err := RuleFormat(rule, cfg, str(a[2]))
		return f, nil, err
	case "parse_hosts_pairs":
		out := []any{}
		for _, p := range ParseHostsPairs(str(a[0])) {
			out = append(out, []any{p.Domain, p.Address})
		}
		return out, nil, nil
	case "fix_proxy_fields":
		proxy := obj(a[0])
		node, err := YAMLNodeFromJ(proxy)
		if err != nil {
			return nil, proxy, nil
		}
		FixProxyNode(node)
		if proxy != nil {
			updated, err := JFromYAMLNode(node)
			if err == nil {
				if uo, ok := updated.(*JObj); ok {
					for _, k := range proxy.Keys() {
						proxy.Delete(k)
					}
					proxy.Update(uo)
				}
			}
		}
		return nil, proxy, nil
	case "apply_github_proxy_domain":
		var cfg *model.Config
		if o := obj(a[1]); o != nil {
			wire, err := DecodeJ[model.System](selectJMembers(o, "system_config"))
			if err != nil {
				return "", nil, err
			}
			cfg = &model.Config{SystemConfig: wire.SystemConfig}
		}
		u, err := GitHubURL(str(a[0]), cfg)
		return u, nil, err
	case "mihomo_used_ports":
		node, err := YAMLNodeFromJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		used := UsedPorts(node)
		var items []any
		for p := range used {
			items = append(items, JInt(p))
		}
		return sortedSet(items), nil, nil
	case "mihomo_listen_ports":
		node, err := YAMLNodeFromJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		owners := ListenPorts(node)
		ports := map[json.Number]string{}
		var keys []json.Number
		for _, o := range owners {
			n := JInt(o.Port)
			ports[n] = o.Owner
			keys = append(keys, n)
		}
		sortPorts(keys)
		out := []any{}
		for _, k := range keys {
			out = append(out, []any{k, ports[k]})
		}
		return out, nil, nil
	case "parse_structured_proxy_string":
		p, err := StructuredProxy(str(a[0]))
		if err != nil || p == nil {
			return nil, nil, err
		}
		jv, err := JFromYAMLNode(p)
		if err != nil {
			return nil, nil, err
		}
		return pyConv(jv), nil, nil
	case "convert_node_to_mihomo":
		node, err := DecodeJ[model.Node](a[0])
		if err != nil {
			return nil, nil, err
		}
		p, err := NodeProxy(ctx, node, conv)
		if err != nil || p == nil {
			return nil, nil, err
		}
		jv, err := JFromYAMLNode(p)
		if err != nil {
			return nil, nil, err
		}
		return pyConv(jv), nil, nil
	case "node_conversions":
		results := NewConversions()
		var outs []any
		strs := a[0].([]any)
		scoped := WithConversions(ctx, results, false)
		for _, s := range strs {
			ps, _ := s.(string)
			var ptr *string
			if s != nil {
				ptr = &ps
			}
			p, _ := NodeProxy(scoped, model.Node{Name: "n", ProxyString: ptr}, conv)
			outs = append(outs, jNodeOrNil(p))
		}
		reuse := WithConversions(ctx, results, true)
		for _, s := range strs {
			ps, _ := s.(string)
			var ptr *string
			if s != nil {
				ptr = &ps
			}
			p, _ := NodeProxy(reuse, model.Node{Name: "again", ProxyString: ptr}, conv)
			outs = append(outs, jNodeOrNil(p))
		}
		record := NewJObj()
		for _, k := range results.Keys() {
			v, _, _ := results.Get(k)
			record.Set(k, jNodeOrNil(v))
		}
		return []any{outs, record}, nil, nil
	case "validate_dialers":
		if err := ValidateResourceArraysJ(obj(a[0])); err != nil {
			return nil, nil, err
		}
		cfg, err := DecodeDialerConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, DialerFailure(ConfigDialers(cfg))
	case "validate_shapes":
		if err := ValidateResourceArraysJ(obj(a[0])); err != nil {
			return nil, nil, err
		}
		cfg, err := DecodeDialerConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, DialerFailure(ConfigShapes(cfg, a[1] == true))
	case "dependency_graph":
		if err := ValidateResourceArraysJ(obj(a[0])); err != nil {
			return nil, nil, err
		}
		cfg, err := DecodeDialerConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		g, err := BuildDialerGraph(cfg)
		if err != nil {
			return nil, nil, DialerFailure(err)
		}
		out := []any{}
		for _, k := range g.Keys() {
			children, _ := g.Children(k)
			out = append(out, []any{[]any{k.Kind, k.ID}, refsOut(children)})
		}
		return out, nil, nil
	case "incoming_dialers":
		if err := ValidateResourceArraysJ(obj(a[0])); err != nil {
			return nil, nil, err
		}
		cfg, err := DecodeDialerConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		refs, err := IncomingRefs(cfg, str(a[1]), str(a[2]))
		return refsOut(refs), nil, DialerFailure(err)
	case "dependency_node_ids":
		if err := ValidateResourceArraysJ(obj(a[0])); err != nil {
			return nil, nil, err
		}
		cfg, err := DecodeDialerConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		ids, err := RequiredNodes(cfg, JStrings(a[1].([]any)), a[2] == true)
		var items []any
		for id := range ids {
			items = append(items, id)
		}
		return sortedSet(items), nil, DialerFailure(err)
	case "reject_surge_dialers":
		if err := ValidateResourceArraysJ(obj(a[0])); err != nil {
			return nil, nil, err
		}
		cfg, err := DecodeDialerConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, DialerFailure(RejectUnsupportedDialers(ctx, cfg, str(a[1]), conv))
	case "overlay_dialer":
		if err := ValidateResourceArraysJ(obj(a[0])); err != nil {
			return nil, nil, err
		}
		cfg, err := DecodeDialerConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		node, err := DecodeJ[model.Node](a[1])
		if err != nil {
			return nil, nil, err
		}
		var proxyNode *yamlx.Node
		if p := obj(a[2]); p != nil {
			proxyNode, err = YAMLNodeFromJ(p)
			if err != nil {
				return nil, nil, err
			}
		}
		out, err := ApplyNodeDialer(cfg, node, proxyNode)
		if err != nil {
			return nil, nil, DialerFailure(err)
		}
		return jNodeOrNil(out), nil, nil
	case "raw_dialer":
		node, err := DecodeJ[model.Node](a[0])
		if err != nil {
			return nil, nil, err
		}
		val, err := NodeDialer(node)
		if err != nil {
			return nil, nil, DialerFailure(err)
		}
		if val == nil {
			return nil, nil, nil
		}
		jv, err := JFromYAMLNode(val)
		return jv, nil, err
	case "resolve_dialer":
		if err := ValidateResourceArraysJ(obj(a[0])); err != nil {
			return nil, nil, err
		}
		cfg, err := DecodeDialerConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		node, err := DecodeJ[model.Node](a[1])
		if err != nil {
			return nil, nil, err
		}
		name, err := ResolveNodeDialer(cfg, node)
		if err != nil {
			return nil, nil, DialerFailure(err)
		}
		if name == nil {
			return nil, nil, nil
		}
		return *name, nil, nil
	case "dialer_target":
		if err := ValidateResourceArraysJ(obj(a[0])); err != nil {
			return nil, nil, err
		}
		cfg, err := DecodeDialerConfigJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		node, err := DecodeJ[model.Node](a[1])
		if err != nil {
			return nil, nil, err
		}
		r, err := NodeDialerTarget(cfg, node)
		if err != nil {
			return nil, nil, DialerFailure(err)
		}
		if r == nil {
			return nil, nil, nil
		}
		return []any{r.Kind, r.ID}, nil, nil
	case "group_members":
		group, err := DecodeJ[model.ProxyGroup](a[0])
		if err != nil {
			return nil, nil, err
		}
		return refsOut(GroupRefs(group)), nil, nil
	case "filter_provider_candidates":
		var proxies []*yamlx.Node
		for _, p := range a[0].([]any) {
			n, err := YAMLNodeFromJ(p)
			if err != nil {
				return nil, nil, err
			}
			proxies = append(proxies, n)
		}
		got, err := FilterProxyNodes(proxies, FilterSpec{Include: str(a[1]), Exclude: str(a[2]), ExcludeType: str(a[3])})
		if err != nil {
			return nil, nil, DialerFailure(err)
		}
		out := make([]any, len(got))
		for i, n := range got {
			jv, err := JFromYAMLNode(n)
			if err != nil {
				return nil, nil, err
			}
			out[i] = jv
		}
		return out, nil, nil
	case "provider_proxy_name":
		override, err := DecodeJ[NameOverride](a[1])
		if err != nil {
			return nil, nil, err
		}
		name, err := ProviderName(obj(a[0]).GetString("name"), override)
		return name, nil, DialerFailure(err)
	case "validate_emitted":
		doc, err := YAMLNodeFromJ(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		var payloads map[string][]*yamlx.Node
		if p := obj(a[2]); p != nil {
			payloads = map[string][]*yamlx.Node{}
			for k, raw := range p.All() {
				items, _ := raw.([]any)
				list := make([]*yamlx.Node, len(items))
				for i, item := range items {
					n, err := YAMLNodeFromJ(item)
					if err != nil {
						return nil, nil, err
					}
					list[i] = n
				}
				payloads[k] = list
			}
		}
		err = ValidateTopology(doc, TopologyOptions{
			ForceChains:      a[1] == true,
			ProviderProxies:  payloads,
			RequireProviders: a[3] == true,
		})
		return nil, nil, DialerFailure(err)
	case "normalize_settings":
		var settings *model.EasyTierSettings
		if a[0] != nil {
			s, err := DecodeJ[model.EasyTierSettings](a[0])
			if err != nil {
				return nil, nil, easytierError(err.Error())
			}
			settings = &s
		}
		normalized, err := NormalizeEasyTier(settings)
		if err != nil {
			return nil, nil, easytierError(err.Error())
		}
		if normalized == nil {
			return nil, nil, nil
		}
		out, err := modelToJ(normalized)
		return out, nil, err
	case "redact_settings":
		if a[0] == nil {
			return nil, nil, nil
		}
		s, err := DecodeJ[model.EasyTierSettings](a[0])
		if err != nil {
			return a[0], nil, nil
		}
		redacted, err := RedactEasyTier(&s)
		if err != nil {
			return a[0], nil, nil
		}
		out, err := modelToJ(redacted)
		if err != nil {
			return a[0], nil, nil
		}
		return out, nil, nil
	case "apply_easytier":
		document := obj(a[0])
		docNode, err := YAMLNodeFromJ(document)
		if err != nil {
			return nil, document, err
		}
		var cfg *model.Config
		if o := obj(a[1]); o != nil {
			profile, err := DecodeJ[model.Profile](selectJMembers(o, "mihomo"))
			if err != nil {
				return nil, document, err
			}
			cfg = &model.Config{Profile: &profile}
		}
		var version json.RawMessage
		if a[3] != nil {
			raw, err := CompactJ(a[3])
			if err != nil {
				return nil, document, err
			}
			version = raw
		}
		r, err := ApplyEasyTierNode(docNode, cfg, a[2] == true, version)
		if err != nil {
			err = easytierError(err.Error())
		}
		updated, jerr := JFromYAMLNode(docNode)
		if jerr == nil && document != nil {
			if uo, ok := updated.(*JObj); ok {
				for _, k := range document.Keys() {
					document.Delete(k)
				}
				document.Update(uo)
			}
		}
		if r == nil {
			return nil, document, err
		}
		return JObjKV("proxy", r.Proxy, "target", r.Target, "rules", JInt(int64(r.Rules))), document, err
	}
	panic("unknown corpus function " + row.fn)
}

func jNodeOrNil(p *yamlx.Node) any {
	if p == nil {
		return nil
	}
	jv, err := JFromYAMLNode(p)
	if err != nil {
		return nil
	}
	return jv
}

func intentional(class string) bool {
	return class == "DialerReferenceError" || class == "EasyTierSettingsError"
}

func goClass(err error) string {
	var d *DialerReferenceError
	var e *EasyTierSettingsError
	switch {
	case errors.As(err, &d):
		return "DialerReferenceError"
	case errors.As(err, &e):
		return "EasyTierSettingsError"
	}
	return "error"
}

// TestCorpus runs every recorded Python call through the typed Go port: results,
// mutated arguments and intentional error messages must be identical; an
// unintended Python exception must be an unintended Go error (the class is
// compared and only reported).
func TestCorpus(t *testing.T) {
	rows := loadCorpus(t)
	counts := map[string]int{}
	typedStorage := map[string]int{}
	classDiff := map[string]int{}
	for i, row := range rows {
		counts[row.fn]++
		if issue := commonRowIssue(row); issue != "" {
			typedStorage[issue]++
			continue
		} // malformed storage: explicit typed tests, not a Python oracle
		if issue := nodeRowIssue(row); issue != "" {
			typedStorage[issue]++
			continue
		}
		if issue := easyTierRowIssue(row); issue != "" {
			typedStorage[issue]++
			continue
		}
		if issue := dialerRowIssue(row); issue != "" {
			typedStorage[issue]++
			continue
		}
		result, mutated, err := runCorpusRow(row)
		if nativeScalarReplay(t, i, row, result, err) {
			continue
		}
		if nativeProviderBoundary(t, row, result, err) || nativeProviderReplay(t, row, result, err) {
			continue
		}
		if nativeProxyReplay(t, "corpus", row, result, mutated, err) {
			continue
		}
		label := func() string { return row.fn + " " + compactText(t, row.args) }
		if row.errClass != "" {
			if err == nil {
				t.Errorf("row %d %s: want %s(%q), got %s", i, label(), row.errClass, row.errMsg, compactText(t, result))
				continue
			}
			got := goClass(err)
			if intentional(row.errClass) || intentional(got) {
				if got != row.errClass || (chineseText(row.errMsg) && err.Error() != row.errMsg) {
					t.Errorf("row %d %s:\n want %s(%q)\n got  %s(%q)", i, label(), row.errClass, row.errMsg, got, err.Error())
				}
			} else if got != row.errClass {
				classDiff[row.errClass+"->"+got]++
			}
		} else {
			if err != nil && refusedByNetURL(row.args) {
				continue // accepted N2 difference: a URL net/url refuses
			}
			if err != nil {
				t.Errorf("row %d %s: unexpected error %v", i, label(), err)
				continue
			}
			if g, w := compactText(t, pyConv(result)), compactText(t, row.out); g != w {
				t.Errorf("row %d %s:\n got  %s\n want %s", i, label(), g, w)
			}
		}
		if row.hasMut {
			if g, w := compactText(t, pyConv(mutated)), compactText(t, row.mutated); g != w {
				t.Errorf("row %d %s mutated:\n got  %s\n want %s", i, label(), g, w)
			}
		}
	}
	t.Logf("%d rows: %v; explicit typed storage classes: %v", len(rows), counts, typedStorage)
	if len(classDiff) > 0 {
		t.Logf("unintended exception classes that differ (Python->Go): %v", classDiff)
	}
}

// chineseText reports whether a Python message is a Chinese user-facing text;
// other exception texts are compared by class only (N2: URL parsing moved to
// net/url, whose error texts differ).
func chineseText(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r >= 0x4e00 && r <= 0x9fff })
}

// refusedByNetURL reports whether v holds a URL string that net/url refuses
// although urllib.parse accepted it (an IPvFuture literal, an escape in the
// host, ...); since N2 such URLs are errors.
func refusedByNetURL(v any) bool {
	switch v := v.(type) {
	case string:
		if !strings.Contains(v, "://") {
			return false
		}
		_, err := weburl.Parse(v)
		return err != nil
	case []any:
		for _, item := range v {
			if refusedByNetURL(item) {
				return true
			}
		}
	case *JObj:
		for _, key := range v.Keys() {
			if refusedByNetURL(v.Value(key)) {
				return true
			}
		}
	}
	return false
}
