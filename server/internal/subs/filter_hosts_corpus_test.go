package subs

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func TestFilterCorpus(t *testing.T) {
	rows := loadCorpus(t, "filter.jsonl.gz")
	native := nativeFilterExpectations(t)
	pending := 0
	for i, row := range rows {
		switch row.Fn {
		case "validate_regex_filter":
			var got string
			switch v := row.In.(type) {
			case nil:
				got = ""
			case string:
				got = ValidateFilter(v)
			default:
				got = msgFilterType
			}
			want, isStr := row.Out.(string)
			if row.Out != nil && !isStr {
				t.Fatalf("row %d: unexpected output %v", i, row.Out)
			}
			// The detail after the Chinese prefix is the regex engine's error
			// text; a pattern only Python rejects is accepted (rules: regexMoreLenient).
			const prefix = "节点筛选正则无效: "
			if strings.HasPrefix(want, prefix) && (got == "" || strings.HasPrefix(got, prefix)) {
				continue
			}
			if got != want {
				t.Fatalf("row %d validate_regex_filter(%s): got %q want %q", i, row.InRaw, got, want)
			}
		case "filter_subscription_items":
			var in struct {
				Sub   json.RawMessage `json:"sub"`
				Items json.RawMessage `json:"items"`
			}
			if err := json.Unmarshal(row.InRaw, &in); err != nil {
				t.Fatal(err)
			}
			var sub model.Subscription
			if err := jsondecode.Unmarshal(in.Sub, &sub); err != nil {
				sub = model.Subscription{}
			}
			itemsNode := yamlFromRaw(t, in.Items)
			selected := FilterProxies(sub, itemsNode.Content)
			gotRaw := yamlToRaw(t, yamlx.Seq(selected...))

			pattern := ""
			if sub.RegexFilter != nil {
				pattern = *sub.RegexFilter
			}
			if strings.Contains(pattern, `\N{`) {
				// Documented difference: \N{name} is not supported by the rules
				// regex engine, the filter is not applied (Python applies it).
				pending++
				if !testjson.Equal(in.Items, gotRaw) {
					t.Fatalf("row %d: unsupported pattern should leave the items unfiltered", i)
				}
				continue
			}
			if strings.Contains(pattern, "$") && strings.Contains(string(in.Items), `\n"`) {
				// Python's $ also matches before a trailing newline; RE2's only
				// at the end (names with a trailing newline are not realistic).
				continue
			}
			wantRaw := row.OutRaw
			if expected, ok := native[fmt.Sprint(i)]; ok {
				wantRaw = expected.Output
			}
			if !testjson.Equal(wantRaw, gotRaw) {
				t.Fatalf("row %d filter %s\n got  %s\n want %s", i, row.InRaw, gotRaw, wantRaw)
			}
		}
	}
	t.Logf("%d rows, %d pending (\\N{name})", len(rows), pending)
}

func TestHostsCorpus(t *testing.T) {
	rows := loadCorpus(t, "hosts.jsonl.gz")
	native := nativeHostsExpectations(t)
	oracle := nativeReaderCases(t)
	kinds := map[string]int{}
	for i, row := range rows {
		kinds[row.Fn]++
		wantRaw := row.OutRaw
		if expected, ok := native[fmt.Sprintf("hosts/%d", i)]; ok {
			wantRaw = expected.Output
		}
		switch row.Fn {
		case "extract_subscription_hosts":
			if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/hosts/%d", i)); changed {
				if expected.HasError {
					t.Fatalf("unexpected native oracle rejection %d", i)
				}
				wantRaw = expected.Value
			}
			got := ExtractHosts(row.In.(string))
			var g any
			if got != nil {
				g = got
			}
			if !testjson.Equal(wantRaw, g) {
				t.Fatalf("row %d extract(%q)\n got  %s\n want %s", i, row.In, compact(t, g), wantRaw)
			}
		case "hosts_digest":
			var rec *HostsData
			if err := jsondecode.Unmarshal(row.InRaw, &rec); err != nil {
				rec = nil
			}
			got, _ := HostDigest(rec)
			var want string
			_ = json.Unmarshal(wantRaw, &want)
			if got != want {
				t.Fatalf("row %d hosts_digest(%s): got %s want %s", i, row.InRaw, got, want)
			}
		case "describe_change":
			var in struct {
				Previous json.RawMessage `json:"previous"`
				Current  json.RawMessage `json:"current"`
			}
			if err := json.Unmarshal(row.InRaw, &in); err != nil {
				t.Fatal(err)
			}
			var prev, curr *HostsData
			_ = jsondecode.Unmarshal(in.Previous, &prev)
			_ = jsondecode.Unmarshal(in.Current, &curr)
			got, err := CompareHosts(prev, curr)
			if err != nil {
				t.Fatalf("row %d describe_change: %v", i, err)
			}
			if !testjson.Equal(wantRaw, got) {
				t.Fatalf("row %d describe_change got %s want %s", i, compact(t, got), wantRaw)
			}
		case "_matches":
			var in struct {
				Pattern any    `json:"pattern"`
				Domain  string `json:"domain"`
			}
			if err := json.Unmarshal(row.InRaw, &in); err != nil {
				t.Fatal(err)
			}
			pattern, ok := in.Pattern.(string)
			got := ok && hostPatternMatches(hostPatternText(pattern), in.Domain)
			if got != row.Out.(bool) {
				t.Fatalf("row %d _matches(%s): got %v", i, row.InRaw, got)
			}
		case "merge_subscription_hosts":
			checkMerge(t, i, row, wantRaw)
		}
	}
	t.Logf("%v", kinds)
}

func strList(v any) []string {
	var out []string
	if v == nil {
		return out
	}
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func checkMerge(t *testing.T, i int, row corpusRow, wantRaw json.RawMessage) {
	var in struct {
		Main       json.RawMessage            `json:"main"`
		Config     json.RawMessage            `json:"config"`
		Caches     map[string]json.RawMessage `json:"caches"`
		UserKeys   []string                   `json:"user_keys"`
		MosdnsKeys []string                   `json:"mosdns_keys"`
		Inline     []struct {
			Node  json.RawMessage `json:"node"`
			Proxy json.RawMessage `json:"proxy"`
		} `json:"inline"`
	}
	if err := json.Unmarshal(row.InRaw, &in); err != nil {
		t.Fatal(err)
	}
	mainNode := yamlFromRaw(t, in.Main)
	cfg, err := decodeConfigRaw(in.Config)
	if err != nil {
		t.Fatalf("row %d config: %v", i, err)
	}
	loader := func(sub model.Subscription) (*HostIndex, error) {
		raw, ok := in.Caches[sub.ID]
		if !ok {
			return nil, nil
		}
		var cache *SubscriptionCache
		if err := jsondecode.Unmarshal(raw, &cache); err != nil {
			cache = &SubscriptionCache{}
		}
		if cache == nil {
			cache = &SubscriptionCache{}
		}
		index, err := NewHostIndex(cache)
		if err != nil || index == nil {
			return nil, err
		}
		return index.ForSubscription(sub), nil
	}
	selected := make([]HostInline, 0, len(in.Inline))
	for _, item := range in.Inline {
		var node model.Node
		if err := jsondecode.Unmarshal(item.Node, &node); err != nil {
			t.Fatalf("row %d inline node: %v", i, err)
		}
		selected = append(selected, HostInline{Node: node, Proxy: yamlFromRaw(t, item.Proxy)})
	}
	report, merged, err := MergeHosts(mainNode, cfg, in.UserKeys, in.MosdnsKeys, selected, loader)
	if err != nil {
		t.Fatalf("row %d merge error: %v", i, err)
	}
	mergedRaw := json.RawMessage("{}")
	if merged != nil {
		mergedRaw = yamlToRaw(t, merged)
	}
	got := map[string]any{
		"report": report,
		"main":   yamlToRaw(t, mainNode),
		"merged": mergedRaw,
	}
	if !testjson.Equal(wantRaw, got) {
		t.Fatalf("row %d merge\n in   %s\n got  %s\n want %s\n diff %s", i, row.InRaw, compact(t, got), wantRaw, testjson.Diff(wantRaw, got))
	}
}
