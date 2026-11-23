package rules

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// Ported from backend/test_group_exclusion_generation.py (the parts that do not
// run the Mihomo converter, which belongs to package #6) plus direct tests of
// the other group_candidates functions; the Python-vs-Go corpus covers them
// broadly (TestCorpusGroupCandidates, TestCorpusFilters).

type testCandidate struct {
	name     string
	password string
}

func TestCandidateFilterPreservesRecordsOrderAndEmptyInclude(t *testing.T) {
	nodes := []testCandidate{
		{name: "US-new", password: "unchanged"},
		{name: "US-expired"},
		{name: "HK-new"},
	}
	kept, err := Filter(nodes, strp(""), strp("expired"), func(c testCandidate) string { return c.name })
	if err != nil || len(kept) != 2 || kept[0] != nodes[0] || kept[1] != nodes[2] {
		t.Fatalf("exclude only: %v %v", kept, err)
	}
	kept, err = Filter(nodes, strp("^US"), strp("expired"), func(c testCandidate) string { return c.name })
	if err != nil || len(kept) != 1 || kept[0] != nodes[0] {
		t.Fatalf("include and exclude: %v %v", kept, err)
	}
	if _, err := Filter(nodes, strp(""), strp("["), func(c testCandidate) string { return c.name }); !IsCandidateValidationError(err) {
		t.Fatalf("invalid regex: %v", err)
	}
}

func TestExclusionValidationAndRenderingTrimInlineFlagsConsistently(t *testing.T) {
	expression := "  (?i)expired  "
	group := model.ProxyGroup{ExcludeRegex: &expression, AggregationExcludeRegex: &expression}
	if err := CheckFilters(group, nil); err != nil {
		t.Fatal(err)
	}
	// The converter renders the trimmed text; filtering by the raw text must agree.
	nodes := []testCandidate{{name: "US-EXPIRED"}, {name: "US-new"}}
	kept, err := Filter(nodes, nil, &expression, func(c testCandidate) string { return c.name })
	if err != nil || len(kept) != 1 || kept[0] != nodes[1] {
		t.Fatalf("kept %v %v", kept, err)
	}
	if got := trimFilter(expression); got != "(?i)expired" {
		t.Fatalf("trimFilter = %q", got)
	}
}

func TestLookaroundFiltersRunOnTheBacktrackingEngine(t *testing.T) {
	nodes := []testCandidate{{name: "香港 01"}, {name: "香港 02 过期"}, {name: "US 剩余流量 10G"}, {name: "US-1"}}
	kept, err := Filter(nodes, strp("^(?!.*(过期|剩余|流量)).*$"), strp(""), func(c testCandidate) string { return c.name })
	if err != nil || len(kept) != 2 || kept[0] != nodes[0] || kept[1] != nodes[3] {
		t.Fatalf("kept %v, %v", kept, err)
	}
	re, _ := CompilePyRegex("^(?!.*(过期|剩余|流量)).*$", false)
	if re.re2 != nil {
		t.Fatal("look-ahead must run on regexp2")
	}
	if err := CheckFilters(model.ProxyGroup{ExcludeRegex: strp("^(?!US)")}, nil); err != nil {
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

func catalogFixture() GroupCatalog {
	disabled := false
	nodes := []string{"n1"}
	return GroupCatalog{
		Nodes: []model.Node{
			{ID: "n1", Name: "Node 1"},
			{ID: "n2", Name: "Node 2", Enabled: &disabled},
		},
		Subscriptions: []model.Subscription{
			{ID: "s1", Name: "Sub1"},
			{ID: "s2", Name: "Sub2"},
		},
		Aggregations: []model.Aggregation{
			{ID: "a1", Name: "Agg1", Subscriptions: []string{"s2"}},
		},
		Groups: []model.ProxyGroup{
			{ID: "g1", Name: "G1", Type: "select", ManualNodes: &nodes},
		},
	}
}

func parseGroup(t *testing.T, raw string) model.ProxyGroup {
	t.Helper()
	var g model.ProxyGroup
	_ = jsondecode.Unmarshal([]byte(raw), &g)
	return g
}

func TestValidateCandidatesRejectsEmptyAndUnknownSources(t *testing.T) {
	catalog := catalogFixture()
	cases := []struct {
		raw  string
		want string
	}{
		{`{"id":"x","type":"select"}`, "策略组必须选择实际消费的候选来源，不能隐式使用 DIRECT"},
		{`{"id":"x","type":"select","manual_nodes":["bogus"]}`, "候选来源必须引用有效启用资源"},
		{`{"id":"x","type":"select","manual_nodes":["n2"]}`, "候选来源必须引用有效启用资源"},
		{`{"id":"x","type":"select","manual_nodes":"n1"}`, "manual_nodes 必须是数组"},
		{`{"id":"x","type":"select","follow_group":5}`, "follow_group 必须是 ID 字符串"},
		{`{"id":"x","type":"select","follow_group":"missing"}`, "跟随来源必须是有效启用的普通来源组"},
		{`{"id":"x","type":"select","subscriptions":["s1"],"exclude_regex":"["}`, "exclude_regex 正则表达式无效"},
		{`{"id":"x","type":"select","regex":5}`, "regex 必须是字符串或 null"},
		{`{"id":"x","type":"select","manual_nodes":["n1"],"proxies_order":[{"type":"node","id":"n1"},{"type":"node","id":"n1"}]}`, "排序项不能重复"},
		{`{"id":"x","type":"select","manual_nodes":["n1"],"proxies_order":[{"type":"node"}]}`, "排序项必须包含且仅包含 type 和 id"},
		{`{"id":"x","type":"select","manual_nodes":["n1","DIRECT"],"proxies_order":[{"type":"node","id":"n1"}]}`, "排序必须包含所有手动节点和引用策略组"},
	}
	for _, c := range cases {
		err := CheckCandidateMembers(parseGroup(t, c.raw), requestMembers(t, json.RawMessage(c.raw)), catalog, nil, nil)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: err = %v, want %q", c.raw, err, c.want)
		}
	}
	for _, raw := range []string{
		`{"id":"x","type":"select","manual_nodes":["n1","DIRECT","REJECT"]}`,
		`{"id":"x","type":"select","subscriptions":["s1"],"aggregations":["a1"]}`,
		`{"id":"x","type":"chain"}`,
		`{"id":"x","type":"select","follow_group":"g1"}`,
		`{"id":"x","type":"select","source":"node","proxies":["n1"]}`,
	} {
		if err := CheckCandidates(parseGroup(t, raw), catalog, nil); err != nil {
			t.Errorf("%s rejected: %v", raw, err)
		}
	}
	// An already empty legacy record with unchanged sources stays editable.
	legacy := parseGroup(t, `{"id":"x","type":"select","name":"old"}`)
	if err := CheckCandidates(parseGroup(t, `{"id":"x","type":"select","name":"renamed"}`), catalog, &legacy); err != nil {
		t.Errorf("legacy empty group rejected: %v", err)
	}
}

func TestRenderMembersFollowsOrderAndDeduplicatesProviders(t *testing.T) {
	catalog := catalogFixture()
	group := parseGroup(t, `{"manual_nodes":["n1","DIRECT"],"include_groups":["g1"],"subscriptions":["s1","s2"],"aggregations":["a1"],"regex":"^US","aggregation_regex":"^HK","proxy_order":"strategies_first"}`)
	rendered := RenderGroup(group, catalog)
	if got := compact(t, rendered); got != `{"proxies":["G1","Node 1","DIRECT"],"use":["Sub1","Agg1"],"filter":"(^US)|(^HK)"}` {
		t.Errorf("rendered %s", got)
	}
	selection := SelectMembers(group)
	if len(selection.Members) != 3 || selection.Members[0].Type != "strategy" {
		t.Errorf("members %v", selection.Members)
	}
}

func TestMergeGroupUpdate(t *testing.T) {
	original := parseGroup(t, `{"id":"g","name":"G","url":"https://t.test","interval":300}`)
	var patch model.Fields
	_ = patch.UnmarshalJSON([]byte(`{"name":"H","delete_fields":["url"]}`))
	merged, err := MergeGroup(original, patch)
	if err != nil || !testjson.Equal(json.RawMessage(`{"id":"g","name":"H","interval":300}`), merged) {
		t.Fatalf("merged %+v %v", merged, err)
	}
	if original.URL == nil || *original.URL == "" {
		t.Fatal("original mutated")
	}
	for raw, want := range map[string]string{
		`{"delete_fields":["url","url"]}`:     "delete_fields 必须是无重复的健康检查字段数组",
		`{"delete_fields":["bogus"]}`:         "delete_fields 必须是无重复的健康检查字段数组",
		`{"delete_fields":"url"}`:             "delete_fields 必须是无重复的健康检查字段数组",
		`{"url":"x","delete_fields":["url"]}`: "同一字段不能同时赋值和删除",
		`{"regex":5}`:                         "regex 必须是字符串或 null",
	} {
		var p model.Fields
		_ = p.UnmarshalJSON([]byte(raw))
		if _, err := MergeGroup(original, p); err == nil || err.Error() != want {
			t.Errorf("patch %s: %v, want %q", raw, err, want)
		}
	}
}

func TestLowerGroupProviderFiltersScopesExclusionsToProviders(t *testing.T) {
	catalog := catalogFixture()
	subs := []string{"s1", "s2"}
	catalog.Groups[0].Subscriptions = &subs
	catalog.Groups[0].ExcludeRegex = strp("expired")
	mainNode, err := yamlx.FromJSON([]byte(`{
		"proxies":[{"name":"Node 1"}],
		"proxy-providers":{
			"Sub1":{"type":"http","url":"u1","path":"./p1.yaml"},
			"Sub2":{"type":"http","url":"u2","path":"./p2.yaml"}
		},
		"proxy-groups":[{"name":"G1","type":"select","proxies":["Node 1"],"use":["Sub1","Sub2"]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := LowerProviderFilters(catalog, mainNode); err != nil {
		t.Fatal(err)
	}
	groupNode := yamlx.Get(mainNode, "proxy-groups").Content[0]
	// One shared rule: folded into the group filter, providers untouched.
	if got := yamlx.Get(groupNode, "filter").Value; got != `^(?![\s\S]*?(?:expired))` {
		t.Errorf("filter = %q", got)
	}
	providers := yamlx.Get(mainNode, "proxy-providers")
	if len(providers.Content)/2 != 2 {
		t.Errorf("providers count = %d", len(providers.Content)/2)
	}
}

// requestMembers is a request group's members as JSON: requests are checked
// before the plain group record decodes them (O18 T1c).
func requestMembers(t *testing.T, value any) GroupMembers {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	members, _ := DecodeGroupMembers(data)
	return members
}

// originalMembers is a corpus record's stored group members, nil without one.
func originalMembers(t *testing.T, record map[string]any) *GroupMembers {
	t.Helper()
	if record["original"] == nil {
		return nil
	}
	return new(requestMembers(t, record["original"]))
}
