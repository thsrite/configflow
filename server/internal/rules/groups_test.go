package rules

import (
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

// Ported from backend/test_group_exclusion_generation.py (the parts that do not
// run the Mihomo converter, which belongs to package #6) plus direct tests of
// the other group_candidates functions; the Python-vs-Go corpus covers them
// broadly (TestCorpusGroupCandidates, TestCorpusFilters).

func TestCandidateFilterPreservesRecordsOrderAndEmptyInclude(t *testing.T) {
	nodes := []any{
		obj("name", "US-new", "params", obj("password", "unchanged")), obj("name", "US-expired"), obj("name", "HK-new"),
	}
	kept, err := FilterCandidates(nodes, "", "expired")
	if err != nil || len(kept) != 2 || kept[0] != nodes[0] || kept[1] != nodes[2] {
		t.Fatalf("exclude only: %v %v", kept, err)
	}
	kept, err = FilterCandidates(nodes, "^US", "expired")
	if err != nil || len(kept) != 1 || kept[0] != nodes[0] {
		t.Fatalf("include and exclude: %v %v", kept, err)
	}
	if _, err := FilterCandidates(nodes, "", "["); !IsCandidateValidationError(err) {
		t.Fatalf("invalid regex: %v", err)
	}
	if _, err := FilterCandidates(nodes, 5, ""); err == nil || err.Error() != "正则筛选必须是字符串或 null" {
		t.Fatalf("non-string: %v", err)
	}
}

func TestExclusionValidationAndRenderingTrimInlineFlagsConsistently(t *testing.T) {
	expression := "  (?i)expired  "
	if err := ValidateFilters(obj("exclude_regex", expression, "aggregation_exclude_regex", expression), nil); err != nil {
		t.Fatal(err)
	}
	// The converter renders the trimmed text; filtering by the raw text must agree.
	nodes := []any{obj("name", "US-EXPIRED"), obj("name", "US-new")}
	kept, err := FilterCandidates(nodes, nil, expression)
	if err != nil || len(kept) != 1 || kept[0] != nodes[1] {
		t.Fatalf("kept %v %v", kept, err)
	}
	if got := filterValue(obj("exclude_regex", expression), "exclude_regex"); got != "(?i)expired" {
		t.Fatalf("filterValue = %q", got)
	}
}

func TestLookaroundFiltersRunOnTheBacktrackingEngine(t *testing.T) {
	nodes := []any{obj("name", "香港 01"), obj("name", "香港 02 过期"), obj("name", "US 剩余流量 10G"), obj("name", "US-1")}
	kept, err := FilterCandidates(nodes, "^(?!.*(过期|剩余|流量)).*$", "")
	if err != nil || len(kept) != 2 || kept[0] != nodes[0] || kept[1] != nodes[3] {
		t.Fatalf("kept %v, %v", kept, err)
	}
	re, _ := CompilePyRegex("^(?!.*(过期|剩余|流量)).*$", false)
	if re.re2 != nil {
		t.Fatal("look-ahead must run on regexp2")
	}
	if err := ValidateFilters(obj("exclude_regex", "^(?!US)"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestBacktrackingTimeoutIsNoMatchWithOneWarning(t *testing.T) {
	old := BacktrackTimeout
	BacktrackTimeout = 50 * time.Millisecond
	defer func() { BacktrackTimeout = old }()
	logger := &countingLogger{}
	oldLogger := regexLogger
	regexLogger = logger
	defer func() { regexLogger = oldLogger }()
	re, err := CompilePyRegex(`^(?:a|aa)+(?!x)$`, false)
	if err != nil || re.re2 != nil {
		t.Fatalf("compile: %v", err)
	}
	subject := strings.Repeat("a", 60) + "b"
	for i := 0; i < 3; i++ {
		if re.Search(subject) {
			t.Fatal("matched")
		}
	}
	if logger.warnings != 1 {
		t.Fatalf("warnings = %d", logger.warnings)
	}
}

type countingLogger struct{ warnings int }

func (*countingLogger) Debugf(string, ...any)     {}
func (l *countingLogger) Warningf(string, ...any) { l.warnings++ }
func (*countingLogger) Errorf(string, ...any)     {}

func TestExclusionFilterShapes(t *testing.T) {
	for _, c := range []struct{ include, exclude, want string }{
		{"^US", "", "^US"},
		{"", "expired", `^(?![\s\S]*?(?:expired))`},
		{"^US", "(?i)exp", `^(?![\s\S]*?(?i:exp))(?=[\s\S]*?(?:^US))`},
		{"", "a`b", `^(?![\s\S]*?(?:a\x60b))`},
		{"", `a\` + "`b", `^(?![\s\S]*?(?:a\x60b))`},
		{"", `a\\` + "`b", `^(?![\s\S]*?(?:a\\\x60b))`},
	} {
		if got := ExclusionFilter(c.include, c.exclude); got != c.want {
			t.Errorf("ExclusionFilter(%q, %q) = %q, want %q", c.include, c.exclude, got, c.want)
		}
	}
}

func catalogFixture() (shared, profile *jsonx.Object) {
	shared = obj(
		"nodes", seq(obj("id", "n1", "name", "Node 1"), obj("id", "n2", "name", "Node 2", "enabled", false)),
		"subscriptions", seq(obj("id", "s1", "name", "Sub1"), obj("id", "s2", "name", "Sub2")),
		"subscription_aggregations", seq(obj("id", "a1", "name", "Agg1", "subscriptions", seq("s2"))),
	)
	profile = obj("proxy_groups", seq(obj("id", "g1", "name", "G1", "type", "select", "manual_nodes", seq("n1"))))
	return
}

func TestValidateCandidatesRejectsEmptyAndUnknownSources(t *testing.T) {
	shared, profile := catalogFixture()
	cases := []struct {
		group *jsonx.Object
		want  string
	}{
		{obj("id", "x", "type", "select"), "策略组必须选择实际消费的候选来源，不能隐式使用 DIRECT"},
		{obj("id", "x", "type", "select", "manual_nodes", seq("bogus")), "候选来源必须引用有效启用资源"},
		{obj("id", "x", "type", "select", "manual_nodes", seq("n2")), "候选来源必须引用有效启用资源"},
		{obj("id", "x", "type", "select", "manual_nodes", "n1"), "manual_nodes 必须是数组"},
		{obj("id", "x", "type", "select", "follow_group", 5), "follow_group 必须是 ID 字符串"},
		{obj("id", "x", "type", "select", "follow_group", "missing"), "跟随来源必须是有效启用的普通来源组"},
		{obj("id", "x", "type", "select", "subscriptions", seq("s1"), "exclude_regex", "["), "exclude_regex 正则表达式无效"},
		{obj("id", "x", "type", "select", "regex", 5), "regex 必须是字符串或 null"},
		{obj("id", "x", "type", "select", "manual_nodes", seq("n1"), "proxies_order", seq(obj("type", "node", "id", "n1"), obj("type", "node", "id", "n1"))), "排序项不能重复"},
		{obj("id", "x", "type", "select", "manual_nodes", seq("n1"), "proxies_order", seq(obj("type", "node"))), "排序项必须包含且仅包含 type 和 id"},
		{obj("id", "x", "type", "select", "manual_nodes", seq("n1", "DIRECT"), "proxies_order", seq(obj("type", "node", "id", "n1"))), "排序必须包含所有手动节点和引用策略组"},
	}
	for _, c := range cases {
		err := ValidateCandidates(c.group, profile, shared, nil)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: err = %v, want %q", jsonx.Repr(c.group), err, c.want)
		}
	}
	for _, ok := range []*jsonx.Object{
		obj("id", "x", "type", "select", "manual_nodes", seq("n1", "DIRECT", "REJECT")),
		obj("id", "x", "type", "select", "subscriptions", seq("s1"), "aggregations", seq("a1")),
		obj("id", "x", "type", "chain"),
		obj("id", "x", "type", "select", "follow_group", "g1"),
		obj("id", "x", "type", "select", "source", "node", "proxies", seq("n1")),
	} {
		if err := ValidateCandidates(ok, profile, shared, nil); err != nil {
			t.Errorf("%s rejected: %v", jsonx.Repr(ok), err)
		}
	}
	// An already empty legacy record with unchanged sources stays editable.
	legacy := obj("id", "x", "type", "select", "name", "old")
	if err := ValidateCandidates(obj("id", "x", "type", "select", "name", "renamed"), profile, shared, legacy); err != nil {
		t.Errorf("legacy empty group rejected: %v", err)
	}
}

func TestRenderMembersFollowsOrderAndDeduplicatesProviders(t *testing.T) {
	shared, profile := catalogFixture()
	config := shared.Clone()
	config.Set("proxy_groups", profile.Value("proxy_groups"))
	group := obj("manual_nodes", seq("n1", "DIRECT"), "include_groups", seq("g1"), "subscriptions", seq("s1", "s2"),
		"aggregations", seq("a1"), "regex", "^US", "aggregation_regex", "^HK", "proxy_order", "strategies_first")
	rendered := RenderMembers(group, config)
	if got := compact(t, rendered); got != `{"proxies":["G1","Node 1","DIRECT"],"use":["Sub1","Agg1"],"filter":"(^US)|(^HK)"}` {
		t.Errorf("rendered %s", got)
	}
	selection := ManagedMembers(group)
	if len(selection.Members) != 3 || selection.Members[0].Kind != "strategy" {
		t.Errorf("members %v", selection.Members)
	}
}

func TestMergeGroupUpdate(t *testing.T) {
	original := obj("id", "g", "name", "G", "url", "https://t.test", "interval", jsonx.Int(300))
	merged, err := MergeGroupUpdate(original, obj("name", "H", "delete_fields", seq("url")))
	if err != nil || compact(t, merged) != `{"id":"g","name":"H","interval":300}` {
		t.Fatalf("merged %v %v", merged, err)
	}
	if original.GetString("url") == "" {
		t.Fatal("original mutated")
	}
	for patch, want := range map[*jsonx.Object]string{
		obj("delete_fields", seq("url", "url")):      "delete_fields 必须是无重复的健康检查字段数组",
		obj("delete_fields", seq("bogus")):           "delete_fields 必须是无重复的健康检查字段数组",
		obj("delete_fields", "url"):                  "delete_fields 必须是无重复的健康检查字段数组",
		obj("url", "x", "delete_fields", seq("url")): "同一字段不能同时赋值和删除",
		obj("regex", 5):                              "regex 必须是字符串或 null",
	} {
		if _, err := MergeGroupUpdate(original, patch); err == nil || err.Error() != want {
			t.Errorf("patch %s: %v, want %q", jsonx.Repr(patch), err, want)
		}
	}
}

func TestLowerGroupProviderFiltersScopesExclusionsToProviders(t *testing.T) {
	shared, profile := catalogFixture()
	config := shared.Clone()
	profile.GetList("proxy_groups")[0].(*jsonx.Object).Set("subscriptions", seq("s1", "s2"))
	profile.GetList("proxy_groups")[0].(*jsonx.Object).Set("exclude_regex", "expired")
	config.Set("proxy_groups", profile.Value("proxy_groups"))
	main := obj(
		"proxies", seq(obj("name", "Node 1")),
		"proxy-providers", obj("Sub1", obj("type", "http", "url", "u1", "path", "./p1.yaml"), "Sub2", obj("type", "http", "url", "u2", "path", "./p2.yaml")),
		"proxy-groups", seq(obj("name", "G1", "type", "select", "proxies", seq("Node 1"), "use", seq("Sub1", "Sub2"))),
	)
	LowerGroupProviderFilters(config, main)
	group := main.GetList("proxy-groups")[0].(*jsonx.Object)
	// One shared rule: folded into the group filter, providers untouched.
	if got := group.GetString("filter"); got != `^(?![\s\S]*?(?:expired))` {
		t.Errorf("filter = %q", got)
	}
	if main.GetObject("proxy-providers").Len() != 2 {
		t.Errorf("providers %v", main.GetObject("proxy-providers").Keys())
	}
}
