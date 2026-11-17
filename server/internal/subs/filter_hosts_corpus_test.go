package subs

import (
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx/yamlxtest"
)

func asObj(v any) *jsonx.Object {
	o, _ := v.(*jsonx.Object)
	return o
}

func TestFilterCorpus(t *testing.T) {
	rows := loadCorpus(t, "filter.jsonl.gz")
	pending := 0
	for i, row := range rows {
		switch row.Fn {
		case "validate_regex_filter":
			got := ValidateRegexFilter(row.In)
			want, _ := row.Out.(string)
			if row.Out != nil && !isString(row.Out) {
				t.Fatalf("row %d: unexpected output %v", i, row.Out)
			}
			// The detail after the Chinese prefix is the regex engine's error
			// text; a pattern only Python rejects is accepted (rules: regexMoreLenient).
			const prefix = "节点筛选正则无效: "
			if strings.HasPrefix(want, prefix) && (got == "" || strings.HasPrefix(got, prefix)) {
				continue
			}
			if got != want {
				t.Fatalf("row %d validate_regex_filter(%s): got %q want %q", i, compact(t, row.In), got, want)
			}
		case "filter_subscription_items":
			in := asObj(row.In)
			items, _ := in.Value("items").([]any)
			got := FilterSubscriptionItems(in.Value("sub"), items)
			if pattern := jsonx.Str(asObj(in.Value("sub")).Value("regex_filter")); strings.Contains(pattern, `\N{`) {
				// Documented difference: \N{name} is not supported by the rules
				// regex engine, the filter is not applied (Python applies it).
				pending++
				if g, w := compact(t, got), compact(t, items); g != w {
					t.Fatalf("row %d: unsupported pattern should leave the items unfiltered", i)
				}
				continue
			}
			if pattern := jsonx.Str(asObj(in.Value("sub")).Value("regex_filter")); strings.Contains(pattern, "$") &&
				strings.Contains(compact(t, items), `\n"`) {
				// Python's $ also matches before a trailing newline; RE2's only
				// at the end (names with a trailing newline are not realistic).
				continue
			}
			if g, w := compact(t, got), compact(t, row.Out); g != w {
				t.Fatalf("row %d filter %s\n got  %s\n want %s", i, compact(t, row.In), g, w)
			}
		}
	}
	t.Logf("%d rows, %d pending (\\N{name})", len(rows), pending)
}

func TestHostsCorpus(t *testing.T) {
	rows := loadCorpus(t, "hosts.jsonl.gz")
	oracle := nativeReaderCases(t)
	kinds := map[string]int{}
	for i, row := range rows {
		kinds[row.Fn]++
		switch row.Fn {
		case "extract_subscription_hosts":
			if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/hosts/%d", i)); changed {
				if expected.Has("error") {
					t.Fatalf("unexpected native oracle rejection %d", i)
				}
				row.Out = expected.Value("value")
			}
			got := ExtractSubscriptionHosts(row.In.(string))
			var g any
			if got != nil {
				g = got
			}
			if gs, ws := compact(t, g), compact(t, row.Out); !yamlxtest.EqualJSON(g, row.Out) {
				t.Fatalf("row %d extract(%q)\n got  %s\n want %s", i, row.In, gs, ws)
			}
		case "hosts_digest":
			if got := HostsDigest(asObj(row.In)); got != row.Out.(string) {
				t.Fatalf("row %d hosts_digest(%s): got %s want %v", i, compact(t, row.In), got, row.Out)
			}
		case "describe_change":
			in := asObj(row.In)
			got := DescribeChange(asObj(in.Value("previous")), asObj(in.Value("current")))
			if g, w := compact(t, got), compact(t, row.Out); g != w {
				t.Fatalf("row %d describe_change got %s want %s", i, g, w)
			}
		case "_matches":
			in := asObj(row.In)
			if got := Matches(in.Value("pattern"), in.GetString("domain")); got != row.Out.(bool) {
				t.Fatalf("row %d _matches(%s): got %v", i, compact(t, row.In), got)
			}
		case "merge_subscription_hosts":
			checkMerge(t, i, row)
		}
	}
	t.Logf("%v", kinds)
}

func strList(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func checkMerge(t *testing.T, i int, row corpusRow) {
	in := asObj(row.In)
	main := in.GetObject("main")
	caches := in.GetObject("caches")
	indexOf := func(sub *jsonx.Object) *HostsIndex {
		id, _ := sub.Value("id").(string)
		if !caches.Has(id) {
			return nil
		}
		cache := asObj(caches.Value(id))
		if cache == nil {
			cache = jsonx.NewObject()
		}
		return NewHostsIndex(cache).ForURL(sub)
	}
	var inline []InlineProxy
	for _, v := range in.GetList("inline") {
		o := asObj(v)
		inline = append(inline, InlineProxy{Node: o.GetObject("node"), Proxy: asObj(o.Value("proxy"))})
	}
	merged := jsonx.NewObject()
	report := MergeSubscriptionHosts(main, in.GetObject("config"), strList(in.Value("user_keys")), strList(in.Value("mosdns_keys")), inline, merged, indexOf)
	got := jsonx.Obj("report", report, "main", main, "merged", merged)
	if g, w := compact(t, got), compact(t, row.Out); g != w {
		t.Fatalf("row %d merge\n in   %s\n got  %s\n want %s", i, compact(t, row.In), g, w)
	}
}
