package convert

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

// fakeConverter mirrors fake_convert of testdata/gen.py.
type fakeConverter struct{ calls []string }

func (f *fakeConverter) ConvertProxyString(_ context.Context, s string) *jsonx.Object {
	f.calls = append(f.calls, s)
	if strings.Contains(s, "fail") {
		return nil
	}
	if strings.Contains(s, "empty") {
		return jsonx.NewObject()
	}
	proxy := jsonx.Obj("name", "from-uri", "type", "http", "server", "fixture.test", "port", jsonx.Int(80))
	if _, after, ok := strings.Cut(s, "dial-"); ok {
		target, _, _ := strings.Cut(after, "&")
		proxy.Set("dialer-proxy", target)
	}
	if strings.Contains(s, "vless") {
		proxy.Set("type", "vless")
		proxy.Set("reality-opts", jsonx.Obj("public-key", "k"))
	}
	return proxy
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
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		entry := value.(*jsonx.Object)
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
	b, err := jsonx.Compact(v)
	if err != nil {
		t.Fatalf("encode %#v: %v", v, err)
	}
	return string(b)
}

// pyConv mirrors conv() of gen.py for Go results.
func pyConv(v any) any {
	switch value := v.(type) {
	case *jsonx.Object:
		if value == nil {
			return nil
		}
		out := jsonx.NewObject()
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
		return jsonx.Obj("__bytes__", hex.EncodeToString(value))
	case yamlx.Timestamp:
		return jsonx.Obj("__date__", value.String())
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
		b, _ := jsonx.Encode(item, jsonx.Options{EnsureASCII: true, SortKeys: true})
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

func obj(v any) *jsonx.Object {
	o, _ := v.(*jsonx.Object)
	return o
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// runCorpusRow calls the Go port of row.fn; it returns the conv() result and
// the mutated argument.
func runCorpusRow(row corpusRow) (result any, mutated any, err error) {
	ctx := context.Background()
	conv := &fakeConverter{}
	a := row.args
	switch row.fn {
	case "udp_fallback":
		s, err := UDPFallback(obj(a[0]))
		if s == "" {
			return nil, nil, err
		}
		return s, nil, err
	case "ntp_direct":
		b, err := NTPDirect(obj(a[0]))
		return b, nil, err
	case "emitted_rules":
		r, err := EmittedRules(obj(a[0]))
		if r == nil {
			r = []any{}
		}
		return r, nil, err
	case "after_match_comment":
		return AfterMatchComment(obj(a[0])), nil, nil
	case "network_as_protocol":
		x, y := NetworkAsProtocol(str(a[0]), str(a[1]))
		return []any{x, y}, nil, nil
	case "has_port_list":
		return HasPortList(str(a[0]), str(a[1])), nil, nil
	case "set_general_option":
		return SetGeneralOption(str(a[0]), str(a[1]), str(a[2])), nil, nil
	case "ruleset_format":
		f, err := RulesetFormat(obj(a[0]), obj(a[1]), str(a[2]))
		return f, nil, err
	case "parse_hosts_pairs":
		out := []any{}
		for _, p := range ParseHostsPairs(str(a[0])) {
			out = append(out, []any{p.Domain, p.Address})
		}
		return out, nil, nil
	case "fix_proxy_fields":
		proxy := obj(a[0])
		FixProxyFields(proxy)
		return nil, proxy, nil
	case "apply_github_proxy_domain":
		u, err := ApplyGithubProxyDomain(str(a[0]), obj(a[1]))
		return u, nil, err
	case "mihomo_used_ports":
		used, err := MihomoUsedPorts(obj(a[0]))
		var items []any
		for p := range used {
			items = append(items, p)
		}
		return sortedSet(items), nil, err
	case "mihomo_listen_ports":
		owners, err := MihomoListenPorts(obj(a[0]))
		ports := map[jsonx.Number]string{}
		var keys []jsonx.Number
		for _, o := range owners {
			ports[o.Port] = o.Owner
			keys = append(keys, o.Port)
		}
		sortPorts(keys)
		out := []any{}
		for _, k := range keys {
			out = append(out, []any{k, ports[k]})
		}
		return out, nil, err
	case "parse_structured_proxy_string":
		p, err := ParseStructuredProxyString(str(a[0]))
		if err != nil {
			return nil, nil, err
		}
		if p == nil {
			return nil, nil, nil
		}
		return pyConv(p), nil, nil
	case "convert_node_to_mihomo":
		p, err := ConvertNodeToMihomo(ctx, obj(a[0]), conv)
		if p == nil {
			return nil, nil, err
		}
		return pyConv(p), nil, err
	case "node_conversions":
		results := NewConversionResults()
		var outs []any
		strs := a[0].([]any)
		scoped := WithNodeConversions(ctx, results, false)
		for _, s := range strs {
			p, _ := ConvertNodeToMihomo(scoped, jsonx.Obj("name", "n", "proxy_string", s), conv)
			outs = append(outs, nilIfNone(p))
		}
		reuse := WithNodeConversions(ctx, results, true)
		for _, s := range strs {
			p, _ := ConvertNodeToMihomo(reuse, jsonx.Obj("name", "again", "proxy_string", s), conv)
			outs = append(outs, nilIfNone(p))
		}
		record := jsonx.NewObject()
		for _, k := range results.Keys() {
			v, _ := results.Get(k)
			record.Set(k, nilIfNone(v))
		}
		return []any{outs, record}, nil, nil
	case "validate_dialers":
		return nil, nil, ValidateDialers(obj(a[0]))
	case "validate_shapes":
		return nil, nil, ValidateShapes(obj(a[0]), a[1] == true)
	case "dependency_graph":
		g, err := DependencyGraph(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		out := []any{}
		for _, k := range g.Keys() {
			children, _ := g.Children(k)
			out = append(out, []any{[]any{k.Kind, k.ID}, refsOut(children)})
		}
		return out, nil, nil
	case "incoming_dialers":
		refs, err := IncomingDialers(obj(a[0]), str(a[1]), str(a[2]))
		return refsOut(refs), nil, err
	case "dependency_node_ids":
		ids, err := DependencyNodeIDs(obj(a[0]), jsonx.Strings(a[1].([]any)), a[2] == true)
		var items []any
		for id := range ids {
			items = append(items, id)
		}
		return sortedSet(items), nil, err
	case "reject_surge_dialers":
		return nil, nil, RejectSurgeDialers(ctx, obj(a[0]), str(a[1]), conv)
	case "overlay_dialer":
		p, err := OverlayDialer(obj(a[0]), obj(a[1]), obj(a[2]))
		if p == nil {
			return nil, nil, err
		}
		return p, nil, err
	case "raw_dialer":
		v, err := RawDialer(obj(a[0]))
		return v, nil, err
	case "resolve_dialer":
		v, err := ResolveDialer(obj(a[0]), obj(a[1]))
		return v, nil, err
	case "dialer_target":
		r, err := DialerTarget(obj(a[0]), obj(a[1]))
		if r == nil {
			return nil, nil, err
		}
		return []any{r.Kind, r.IDValue()}, nil, err
	case "group_members":
		refs, err := GroupMembers(obj(a[0]))
		return refsOut(refs), nil, err
	case "filter_provider_candidates":
		var proxies []*jsonx.Object
		for _, p := range a[0].([]any) {
			proxies = append(proxies, obj(p))
		}
		got, err := FilterProviderCandidates(proxies, a[1], a[2], a[3])
		return jsonx.ListOf(got), nil, err
	case "provider_proxy_name":
		name, err := ProviderProxyName(obj(a[0]), obj(a[1]))
		return name, nil, err
	case "validate_emitted":
		return nil, nil, ValidateEmitted(obj(a[0]), EmitOptions{ForceChains: a[1] == true,
			ProviderProxies: obj(a[2]), RequireProviders: a[3] == true})
	case "normalize_settings":
		s, err := NormalizeEasyTierSettings(a[0])
		if s == nil {
			return nil, nil, err
		}
		return s, nil, err
	case "redact_settings":
		return RedactEasyTierSettings(a[0]), nil, nil
	case "apply_easytier":
		document := obj(a[0])
		r, err := ApplyEasyTier(document, a[1], a[2] == true, a[3])
		if r == nil {
			return nil, document, err
		}
		return jsonx.Obj("proxy", r.Proxy, "target", r.Target, "rules", jsonx.Int(r.Rules)), document, err
	}
	panic("unknown corpus function " + row.fn)
}

func nilIfNone(p *jsonx.Object) any {
	if p == nil {
		return nil
	}
	return p
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
	if c := PyErrorClass(err); c != "" {
		return c
	}
	return "error"
}

// TestCorpus runs every recorded Python call through the Go port: results,
// mutated arguments and intentional error messages must be identical; an
// unintended Python exception must be an unintended Go error (the class is
// compared and only reported).
func TestCorpus(t *testing.T) {
	rows := loadCorpus(t)
	counts := map[string]int{}
	classDiff := map[string]int{}
	for i, row := range rows {
		counts[row.fn]++
		result, mutated, err := runCorpusRow(row)
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
	t.Logf("%d rows: %v", len(rows), counts)
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
	case *jsonx.Object:
		for _, key := range v.Keys() {
			if refusedByNetURL(v.Value(key)) {
				return true
			}
		}
	}
	return false
}
