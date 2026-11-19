package convert

// Review C12: Python reference rows for edge inputs the porter's corpus
// misses (testdata/review_c12_gen.py → review_c12.jsonl.gz). Unlike TestCorpus,
// unintended exception classes must match too, and order-sensitive results
// (Mihomo listen ports) are compared in order.

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/rulesetfmt"
)

func loadRows(t *testing.T, path string) []corpusRow {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
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

// reviewConv mirrors conv() of review_c12_gen.py.
func reviewConv(v any) any {
	switch value := v.(type) {
	case jsonx.Number:
		switch value {
		case "NaN":
			return jsonx.Obj("__float__", "nan")
		case "Infinity":
			return jsonx.Obj("__float__", "inf")
		case "-Infinity":
			return jsonx.Obj("__float__", "-inf")
		}
		return value
	case *jsonx.Object:
		if value == nil {
			return nil
		}
		out := jsonx.NewObject()
		for k, child := range value.All() {
			out.Set(k, reviewConv(child))
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = reviewConv(child)
		}
		return out
	}
	return pyConv(v)
}

func runReviewRow(row corpusRow) (any, any, error) {
	a := row.args
	switch row.fn {
	case "listen_ports_ordered":
		owners, err := MihomoListenPorts(obj(a[0]))
		out := []any{}
		for _, o := range owners {
			out = append(out, []any{jsonx.Repr(o.Value), o.Owner})
		}
		return out, nil, err
	case "used_ports_repr":
		used, err := usedPorts(obj(a[0]))
		out := []any{}
		for _, p := range used {
			out = append(out, jsonx.Repr(p))
		}
		return out, nil, err
	case "dependency_node_ids":
		ids, err := DependencyNodeIDs(obj(a[0]), jsonx.Strings(a[1].([]any)), a[2] == true)
		items := []any{}
		for id := range ids {
			items = append(items, id)
		}
		return jsonx.Obj("__set__", sortedSet(items)), nil, err
	case "dependency_graph":
		g, err := DependencyGraph(obj(a[0]))
		if err != nil {
			return nil, nil, err
		}
		out := []any{}
		for _, k := range g.Keys() {
			children, _ := g.Children(k)
			refs := []any{}
			for _, c := range children {
				refs = append(refs, []any{c.Kind, c.IDValue()})
			}
			out = append(out, []any{[]any{k.Kind, k.IDValue()}, refs})
		}
		return out, nil, nil
	}
	return runCorpusRow(row)
}

// stripKeyMarks turns conv()'s "<key>1" (a dict key that is not a string)
// into the text json.dumps writes, which is what the Go port stores.
func stripKeyMarks(v any) any {
	switch value := v.(type) {
	case *jsonx.Object:
		out := jsonx.NewObject()
		for k, child := range value.All() {
			if text, ok := strings.CutPrefix(k, "<key>"); ok {
				k = text
			}
			out.Set(k, stripKeyMarks(child))
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = stripKeyMarks(child)
		}
		return out
	}
	return v
}

// reviewKnown lists rows whose Python result cannot be reproduced, with the
// reason (accepted differences recorded in PROGRESS C12).
func reviewKnown(t *testing.T, row corpusRow) string {
	text := compactText(t, row.args)
	switch {
	case (row.fn == "ruleset_format" || row.fn == "apply_github_proxy_domain") && hasNFKCDelimiterAuthority(row.args):
		// CPython urllib refuses NFKC URL delimiters in an authority; net/url
		// accepts them. These review rows are synthetic invalid rule URLs.
		return "N2 NFKC delimiter authority"
	case row.fn == "has_port_list" && strings.ContainsAny(text, "İıſ"):
		// IGNORECASE: Python folds İ/ı/ſ to i/s, the regexp2 engine keeps
		// them apart (rules: accepted difference turkishI).
		return "Turkish i under IGNORECASE"
	case (row.fn == "parse_structured_proxy_string" || row.fn == "convert_node_to_mihomo") &&
		(strings.Contains(text, "&a [*a]") || strings.Contains(text, "!!set")):
		// A recursive alias or a !!set cannot be held in jsonx (domain
		// decision on yamlx): the string is not parsed locally.
		return "unrepresentable YAML value"
	case row.fn == "filter_provider_candidates" && strings.Contains(text, `\\N{`):
		// \N{name} is refused like an invalid pattern (domain regex decision).
		return "unsupported regex"
	}
	return ""
}

func hasNFKCDelimiterAuthority(v any) bool {
	switch v := v.(type) {
	case string:
		i := strings.Index(v, "://")
		if i < 0 {
			return false
		}
		start := i + 3
		end := len(v)
		if n := strings.IndexAny(v[start:], "/?#"); n >= 0 {
			end = start + n
		}
		return strings.ContainsAny(v[start:end], "／？＃＠：﹕﹖﹟﹫︓︖℀℁℅℆⁇⁈⁉⩴")
	case []any:
		for _, item := range v {
			if hasNFKCDelimiterAuthority(item) {
				return true
			}
		}
	case *jsonx.Object:
		for _, key := range v.Keys() {
			if hasNFKCDelimiterAuthority(v.Value(key)) {
				return true
			}
		}
	}
	return false
}

// noneSlotOrder: a port-order row whose result has the same members as
// Python's, for a document whose port set also held None (an inbound key
// missing, or a listener without a port).
func noneSlotOrder(t *testing.T, row corpusRow, result any) bool {
	if row.fn != "listen_ports_ordered" && row.fn != "used_ports_repr" {
		return false
	}
	document := obj(row.args[0])
	hasNone := false
	for _, key := range InboundPortKeys {
		if document.Value(key) == nil {
			hasNone = true
		}
	}
	for _, item := range document.GetList("listeners") {
		if l, ok := item.(*jsonx.Object); ok && l.Value("port") == nil {
			hasNone = true
		}
	}
	got, _ := result.([]any)
	want, _ := row.out.([]any)
	return hasNone && compactText(t, sortedSet(append([]any(nil), got...))) == compactText(t, sortedSet(append([]any(nil), want...)))
}

func TestReviewC12References(t *testing.T) {
	nativeVersions := nativeSubsVersionCases(t, "review_c12")
	rows := loadRows(t, "testdata/review_c12.jsonl.gz")
	failures, known := 0, 0
	for i, row := range rows {
		applyNativeSubsVersion(&row, nativeVersions.GetObject(fmt.Sprintf("review_c12/%d", i)))
		if commonRowIssue(row) != "" {
			known++
			continue
		}
		if nodeRowIssue(row) != "" {
			known++
			continue
		}
		if reason := reviewKnown(t, row); reason != "" {
			known++
			continue
		}
		if easyTierRowIssue(row) != "" || dialerRowIssue(row) != "" {
			known++
			continue
		}
		if dialerRowIssue(row) != "" {
			known++
			continue
		}
		result, mutated, err := runReviewRow(row)
		if nativeProviderBoundary(t, row, result, err) || nativeProviderReplay(t, row, result, err) {
			known++
			continue
		}
		if nativePortReplay(t, row, result, err) {
			known++
			continue
		}
		if row.fn == "apply_github_proxy_domain" && row.errClass == "ValueError" {
			if err == nil {
				t.Fatal("invalid URL accepted")
			}
			continue
		}
		if nativeGeneralReplay(t, row, result, mutated, err) {
			known++
			continue
		}
		if row.fn == "ruleset_format" && row.errClass == "ValueError" {
			var invalid *rulesetfmt.URLParseError
			if !errors.As(err, &invalid) {
				t.Fatalf("invalid URL was not rejected: %v", err)
			}
			continue // native URL diagnostic, not a Python exception class
		}
		if nativeProxyReplay(t, "review", row, result, mutated, err) {
			continue
		}
		label := func() string { return row.fn + " " + compactText(t, row.args) }
		fail := func(format string, args ...any) {
			failures++
			if failures <= 60 {
				t.Errorf("row %d %s: "+format, append([]any{i, label()}, args...)...)
			}
		}
		if row.errClass != "" {
			if err == nil {
				fail("want %s(%q), got %s", row.errClass, row.errMsg, compactText(t, reviewConv(result)))
				continue
			}
			got := goClass(err)
			switch {
			case got != row.errClass:
				fail("want %s(%q), got %s(%q)", row.errClass, row.errMsg, got, err.Error())
			case chineseText(row.errMsg) && err.Error() != row.errMsg:
				fail("want message %q, got %q", row.errMsg, err.Error())
			}
		} else {
			if err != nil && refusedByNetURL(row.args) {
				known++ // N2: a URL net/url refuses
				continue
			}
			if err != nil {
				fail("unexpected error %s(%v), want %s", goClass(err), err, compactText(t, row.out))
				continue
			}
			g, w := compactText(t, reviewConv(result)), compactText(t, row.out)
			if g != w && strings.Contains(w, `"<key>`) && g == compactText(t, stripKeyMarks(row.out)) {
				// Keys that are not strings are written as json.dumps does.
				known++
				continue
			}
			if g != w && noneSlotOrder(t, row, result) {
				// The order depends on where hash(None), an address, put None
				// in the intermediate set (see pySet).
				known++
				continue
			}
			if g != w {
				fail("\n got  %s\n want %s", g, w)
			}
		}
		if row.hasMut {
			if g, w := compactText(t, reviewConv(mutated)), compactText(t, row.mutated); g != w {
				fail("mutated:\n got  %s\n want %s", g, w)
			}
		}
	}
	t.Logf("%d rows, %d known differences, %d failures", len(rows), known, failures)
}

// TestCorpusExceptionMessages: the unintended Python exceptions of the
// porter's corpus carry Python's message too (TestCorpus compares classes).
func TestCorpusExceptionMessages(t *testing.T) {
	failures := 0
	for i, row := range loadCorpus(t) {
		if commonRowIssue(row) != "" || nodeRowIssue(row) != "" || easyTierRowIssue(row) != "" || dialerRowIssue(row) != "" {
			continue
		}
		if row.errClass == "" || intentional(row.errClass) {
			continue
		}
		_, _, err := runCorpusRow(row)
		if nativeProviderBoundary(t, row, nil, err) {
			continue
		}
		if err != nil && err.Error() != row.errMsg && (chineseText(row.errMsg) || !strings.Contains(row.fn, "url") && row.fn != "ruleset_format" && row.fn != "apply_github_proxy_domain") {
			// URL parse errors carry net/url's text since N2.
			failures++
			if failures <= 20 {
				t.Errorf("row %d %s: message %q, Python %q", i, row.fn, err.Error(), row.errMsg)
			}
		}
	}
}

// Regression (review N2): a rule library name holding "%" or Chinese still
// becomes a local rule URL (url.Parse refused the stray "%" and config
// generation failed).
func TestReviewN2AppendURLQueryKeepsRuleName(t *testing.T) {
	for in, want := range map[string]string{
		"/api/profiles/p/rules/local/50%":       "/api/profiles/p/rules/local/50%?target=mihomo",
		"/api/rules/local/国外 规则":                "/api/rules/local/国外 规则?target=mihomo",
		"https://cf.example/api/x?a=b c&d#frag": "https://cf.example/api/x?a=b%20c&d=&target=mihomo#frag",
	} {
		if got, err := AppendQuery(in, [][2]string{{"target", "mihomo"}}); err != nil || got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
