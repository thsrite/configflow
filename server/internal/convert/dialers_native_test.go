package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func typedDialerConfig(t *testing.T, text string) *model.Config {
	t.Helper()
	config, err := DecodeDialerConfigJ(mustJSON(t, text))
	if err != nil {
		t.Fatal(err)
	}
	return config
}
func TestTypedDialerCore(t *testing.T) {
	config := typedDialerConfig(t, `{"nodes":[{"id":"entry","name":"Entry"},{"id":"exit","name":"Exit","type":"http","server":"example.com","port":80,"dialer_ref":{"type":"node","id":"entry"}}]}`)
	if err := ConfigDialers(config); err != nil {
		t.Fatal(err)
	}
	graph, err := BuildDialerGraph(config)
	if err != nil {
		t.Fatal(err)
	}
	children, _ := graph.Children(Ref{"node", "exit"})
	if len(children) != 1 || children[0] != (Ref{"node", "entry"}) {
		t.Fatalf("edges: %v", children)
	}
	children[0] = Ref{}
	original, _ := graph.Children(Ref{"node", "exit"})
	if original[0] != (Ref{"node", "entry"}) {
		t.Fatal("graph edges exposed mutable storage")
	}
	before, _ := model.Marshal(config.Nodes[1])
	proxy, err := NodeProxy(context.Background(), config.Nodes[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err = ApplyNodeDialer(config, config.Nodes[1], proxy)
	if err != nil || scalarText(yamlx.Get(proxy, "dialer-proxy")) != "Entry" {
		t.Fatalf("overlay: %v", err)
	}
	after, _ := model.Marshal(config.Nodes[1])
	if !bytes.Equal(before, after) {
		t.Fatal("dialer overlay mutated its input")
	}
	if _, err := ApplyNodeDialer(config, config.Nodes[1], nil); err == nil || err.Error() != msgDialerConversion {
		t.Fatalf("silent topology loss: %v", err)
	}
}
func TestTypedDialerMalformedValues(t *testing.T) {
	for _, tt := range []struct{ source, message string }{
		{`{"nodes":[{"id":"n","name":"N","params":[]}]}`, msgNodeParamsObject},
		{`{"nodes":[{"id":"n","name":"N","dialer_ref":{"type":"node"}}]}`, msgDialerRef},
		// A stored dialer_ref of another JSON kind reads as the empty
		// reference (fail closed), never as "no dialer" (O18 T1c).
		{`{"nodes":[{"id":"n","name":"N","dialer_ref":"relay"}]}`, msgDialerRef},
		{`{"nodes":[{"id":"n","name":"N","dialer_ref":[]}]}`, msgDialerRef},
		{`{"nodes":[{"id":"n","name":"N","params":{"dialer-proxy":5}}]}`, msgRawDialerName},
		{`{"nodes":[{"id":"n","name":"N","dialer_ref":{"type":"node","id":"missing"}}]}`, msgDialerRefMissing},
		{`{"proxy_groups":[{"id":"g","name":"G","manual_nodes":[false]}]}`, fmt.Sprintf(msgGroupStringList, "manual_nodes")},
	} {
		t.Run(tt.source, func(t *testing.T) {
			err := ConfigDialers(typedDialerConfig(t, tt.source))
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("want %s: %v", tt.message, err)
			}
		})
	}
	if _, err := ProviderName("abc", NameOverride{Rules: []NameRule{{Pattern: "b", Target: "$2"}}}); err == nil {
		t.Fatal("missing group accepted")
	}
	for _, tt := range []struct{ target, want string }{{"<$1>", "a<b>c"}, {"<$01>", "a<b>c"}, {"<$0>", "a<b>c"}, {"<$١>", "a<$١>c"}, {"<$１>", "a<$１>c"}} {
		name, err := ProviderName("abc", NameOverride{Rules: []NameRule{{Pattern: "(b)", Target: tt.target}}})
		if err != nil || name != tt.want {
			t.Fatalf("%s: %q %v", tt.target, name, err)
		}
	}
}

func TestTypedNumericResourceText(t *testing.T) {
	for _, source := range []string{
		`{"nodes":[{"id":5,"name":"N"}]}`,
		`{"nodes":[{"id":5,"name":"N"}],"proxy_groups":[{"id":"g","name":"G","manual_nodes":[5]}]}`,
		`{"nodes":[{"id":"n","name":"N","dialer_ref":{"type":"group","id":"g"}}],"proxy_groups":[{"id":"g","name":"G","use":["S"]}],"subscriptions":[{"id":5,"name":"S"}]}`,
	} {
		if err := ConfigDialers(typedDialerConfig(t, source)); err != nil {
			t.Fatalf("numeric string field rejected: %s / %v", source, err)
		}
	}
}

func TestTypedTopologyMalformedProvider(t *testing.T) {
	for _, definition := range []string{`1`, `{"override":1}`} {
		for _, forced := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/forced=%t", definition, forced), func(t *testing.T) {
				document, err := yamlx.FromJSON([]byte(fmt.Sprintf(`{"proxy-providers":{"F":%s}}`, definition)))
				if err != nil {
					t.Fatal(err)
				}
				for _, payloads := range []map[string][]*yamlx.Node{nil, {"F": {yamlx.Map(yamlx.Str("name"), yamlx.Str("P"))}}} {
					err := ValidateTopology(document, TopologyOptions{ForceChains: forced, ProviderProxies: payloads})
					if err == nil || err.Error() != msgTopologyShape {
						t.Fatalf("payload present=%t: want %q, got %v", payloads != nil, msgTopologyShape, err)
					}
				}
			})
		}
	}
	// Malformed unused providers must not hide an earlier business rejection.
	for _, definition := range []string{`1`, `{"override":1}`} {
		document, err := yamlx.FromJSON([]byte(fmt.Sprintf(`{"proxies":[{"name":"P","dialer-proxy":"Missing"}],"proxy-providers":{"F":%s}}`, definition)))
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateTopology(document, TopologyOptions{ForceChains: true}); err == nil || err.Error() != msgEmittedTargetMissing {
			t.Fatalf("business rejection changed: %v", err)
		}
	}
	valid, err := yamlx.FromJSON([]byte(`{"proxy-providers":{"F":{"override":{}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTopology(valid, TopologyOptions{ForceChains: true}); err != nil {
		t.Fatalf("valid unused provider rejected: %v", err)
	}
}

func TestTypedTopologyTargetDiagnostic(t *testing.T) {
	for _, raw := range []string{`["relay"]`, `{"relay":[1,false]}`, `9007199254740993`, `false`, `"missing"`} {
		t.Run(raw, func(t *testing.T) {
			document, err := yamlx.FromJSON([]byte(`{"proxy-providers":{"F":{}}}`))
			if err != nil {
				t.Fatal(err)
			}
			proxy, err := yamlx.FromJSON([]byte(`{"name":"Remote","type":"http","dialer-proxy":` + raw + `}`))
			if err != nil {
				t.Fatal(err)
			}
			diagnostic := raw
			if raw == `"missing"` {
				diagnostic = "missing"
			}
			err = ValidateTopology(document, TopologyOptions{ForceChains: true, ProviderProxies: map[string][]*yamlx.Node{"F": {proxy}}})
			if want := fmt.Sprintf(msgProviderTarget, "Remote", diagnostic); err == nil || err.Error() != want {
				t.Fatalf("want %q, got %v", want, err)
			}
		})
	}
}

func FuzzTypedDialerGraph(f *testing.F) {
	for _, source := range []string{`{}`, `{"nodes":[{"id":"a","name":"A","dialer_ref":{"type":"node","id":"b"}},{"id":"b","name":"B","dialer_ref":{"type":"node","id":"a"}}]}`, `{"nodes":[{"id":"a","name":"A"}],"proxy_groups":[{"id":"g","name":"G","manual_nodes":["a"]}]}`} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		var profile model.Profile
		var shared model.Shared
		if json.Unmarshal(source, &profile) != nil || json.Unmarshal(source, &shared) != nil {
			return
		}
		config := &model.Config{Profile: &profile, Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations}
		before, _ := model.Marshal(profile)
		_ = ConfigDialers(config)
		graph, err := BuildDialerGraph(config)
		after, _ := model.Marshal(profile)
		if !bytes.Equal(before, after) {
			t.Fatal("graph validation changed its input")
		}
		if err != nil {
			return
		}
		assertGraphDAG(t, graph)
		_, _ = RequiredNodes(config, []string{"a"}, true)
	})
}
func FuzzTypedTopology(f *testing.F) {
	for _, source := range []string{`{}`, `{"proxies":[{"name":"A","dialer-proxy":"DIRECT"}]}`, `{"proxies":[{"name":"A","dialer-proxy":"B"},{"name":"B","dialer-proxy":"A"}]}`, `{"proxies":[{"name":"A"}],"proxy-providers":{"F":{"override":{"proxy-name":[{"pattern":"(P)","target":"<$1>"}],"dialer-proxy":"A"}}}}`} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		node, err := yamlx.FromJSON(source)
		if err != nil {
			return
		}
		before, err := yamlx.ToJSON(node)
		if err != nil {
			t.Fatal(err)
		}
		payloads := map[string][]*yamlx.Node{}
		for name := range yamlx.Pairs(yamlx.Get(node, "proxy-providers")) {
			payloads[name] = []*yamlx.Node{yamlx.Map(yamlx.Str("name"), yamlx.Str("P"), yamlx.Str("type"), yamlx.Str("http"))}
		}
		_ = ValidateTopology(node, TopologyOptions{ForceChains: true, ProviderProxies: payloads})
		after, err := yamlx.ToJSON(node)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("topology validation modified output graph")
		}
	})
}

// assertGraphDAG uses Kahn's algorithm as an independent oracle for the core's
// stack-based DFS. Referenced builtins are terminal vertices.
func assertGraphDAG(t *testing.T, graph *Graph) {
	t.Helper()
	degrees := map[Ref]int{}
	for _, key := range graph.Keys() {
		if _, ok := degrees[key]; !ok {
			degrees[key] = 0
		}
		children, _ := graph.Children(key)
		for _, child := range children {
			if child.Kind != "builtin" && !graph.Has(child) {
				t.Fatalf("incomplete graph edge: %v -> %v", key, child)
			}
			degrees[child]++
		}
	}
	var ready []Ref
	for key, degree := range degrees {
		if degree == 0 {
			ready = append(ready, key)
		}
	}
	count := 0
	for len(ready) > 0 {
		key := ready[len(ready)-1]
		ready = ready[:len(ready)-1]
		count++
		children, _ := graph.Children(key)
		for _, child := range children {
			degrees[child]--
			if degrees[child] == 0 {
				ready = append(ready, child)
			}
		}
	}
	if count != len(degrees) {
		t.Fatal("successful graph contains a cycle")
	}
}

// These classifiers check stored-member shapes, not generated business results.
func dialerFieldType(obj *JObj, key, kind string, nullable bool) string {
	raw, present := obj.Get(key)
	if !present || raw == nil && nullable {
		return ""
	}
	valid := false
	switch kind {
	case "string":
		_, valid = raw.(string)
	case "bool":
		_, valid = raw.(bool)
	case "strings":
		items, ok := raw.([]any)
		valid = ok
		for _, item := range items {
			if _, ok := item.(string); !ok {
				valid = false
			}
		}
	}
	if !valid {
		return "dialer." + key + " " + kind
	}
	return ""
}
func dialerRefIssue(raw any) string {
	if raw == nil {
		return ""
	}
	ref, ok := raw.(*JObj)
	if !ok || ref == nil || ref.Len() != 2 {
		return "dialer.reference shape"
	}
	for _, key := range []string{"type", "id"} {
		if !ref.Has(key) {
			return "dialer.reference shape"
		}
		if issue := dialerFieldType(ref, key, "string", false); issue != "" {
			return issue
		}
	}
	return ""
}
func dialerResourceIssue(raw any, kind string) string {
	obj, ok := raw.(*JObj)
	if !ok || obj == nil {
		return "dialer.resource object"
	}
	for _, key := range []string{"id", "name", "type", "follow_group", "source", "subscription_id"} {
		if issue := dialerFieldType(obj, key, "string", key == "follow_group"); issue != "" {
			return issue
		}
	}
	if issue := dialerFieldType(obj, "enabled", "bool", false); issue != "" {
		return issue
	}
	if kind == "node" {
		if raw := obj.Value("dialer_ref"); raw != nil {
			if ref, ok := raw.(*JObj); ok {
				if typ := ref.GetString("type"); typ != "node" && typ != "group" {
					return "dialer.reference type"
				}
				if strings.TrimSpace(ref.GetString("id")) == "" {
					return "dialer.reference id"
				}
			}
		}
		return dialerRefIssue(obj.Value("dialer_ref"))
	}
	for _, key := range []string{"manual_nodes", "include_groups", "proxies", "subscriptions", "aggregations", "use", "nodes"} {
		if issue := dialerFieldType(obj, key, "strings", false); issue != "" {
			return issue
		}
	}
	if raw, present := obj.Get("proxies_order"); present {
		items, ok := raw.([]any)
		if !ok {
			return "dialer.proxies_order array"
		}
		for _, item := range items {
			if issue := dialerRefIssue(item); issue != "" {
				return issue
			}
		}
	}
	for _, key := range []string{"include_all", "include-all"} {
		if issue := dialerFieldType(obj, key, "bool", false); issue != "" {
			return issue
		}
	}
	if obj.GetString("type") == "chain" && obj.Value("chain") == nil {
		return "dialer.chain object"
	}
	if raw := obj.Value("chain"); raw != nil {
		chain, ok := raw.(*JObj)
		if !ok {
			return "dialer.chain object"
		}
		for _, key := range []string{"entry", "exit"} {
			if issue := dialerRefIssue(chain.Value(key)); issue != "" {
				return issue
			}
		}
	}
	return ""
}
func dialerProfileIssue(profile *JObj) string {
	if profile == nil {
		return "dialer.config object"
	}
	for _, source := range []struct{ key, kind string }{{"nodes", "node"}, {"proxy_groups", "group"}, {"subscriptions", "subscription"}, {"subscription_aggregations", "aggregation"}} {
		raw, present := profile.Get(source.key)
		if !present {
			continue
		}
		items, ok := raw.([]any)
		if !ok {
			return "dialer." + source.key + " array"
		}
		for _, item := range items {
			if source.kind == "subscription" || source.kind == "aggregation" {
				if obj, ok := item.(*JObj); ok && !obj.Has("id") {
					return "dialer.catalog id required"
				}
			}
			if issue := dialerResourceIssue(item, source.kind); issue != "" {
				return issue
			}
		}
	}
	return ""
}
func emittedShapeIssue(raw any) string {
	obj, ok := raw.(*JObj)
	if !ok || obj == nil {
		return "dialer.emitted object"
	}
	for _, key := range []string{"name", "type", "filter", "exclude-filter", "exclude-type"} {
		if issue := dialerFieldType(obj, key, "string", false); issue != "" {
			return issue
		}
	}
	if issue := dialerFieldType(obj, "dialer-proxy", "string", true); issue != "" {
		return issue
	}
	for _, key := range []string{"proxies", "use"} {
		if issue := dialerFieldType(obj, key, "strings", false); issue != "" {
			return issue
		}
	}
	return ""
}
func nameOverrideIssue(raw any) string {
	obj, ok := raw.(*JObj)
	if !ok || obj == nil {
		return "dialer.override object"
	}
	for _, key := range []string{"additional-prefix", "additional-suffix"} {
		if issue := dialerFieldType(obj, key, "string", false); issue != "" {
			return issue
		}
	}
	if raw, present := obj.Get("proxy-name"); present {
		items, ok := raw.([]any)
		if !ok {
			return "dialer.proxy-name array"
		}
		for _, item := range items {
			rule, ok := item.(*JObj)
			if !ok {
				return "dialer.name-rule object"
			}
			for _, key := range []string{"pattern", "target"} {
				if !rule.Has(key) {
					return "dialer.name-rule required field"
				}
				if issue := dialerFieldType(rule, key, "string", false); issue != "" {
					return issue
				}
			}
		}
	}
	return ""
}
func dialerRowIssue(row corpusRow) string {
	switch row.fn {
	case "validate_dialers", "validate_shapes", "dependency_graph", "incoming_dialers", "dependency_node_ids", "reject_surge_dialers", "dialer_target", "resolve_dialer", "overlay_dialer", "group_dependencies":
		if row.fn == "resolve_dialer" || row.fn == "overlay_dialer" {
			node := obj(row.args[1])
			if ref, ok := node.Value("dialer_ref").(*JObj); ok {
				key := "proxy_groups"
				if ref.GetString("type") == "node" {
					key = "nodes"
				}
				found := false
				for _, raw := range obj(row.args[0]).GetList(key) {
					if resource, ok := raw.(*JObj); ok && EqualJ(resource.Value("id"), ref.Value("id")) {
						found = true
						if !resource.Has("name") {
							return "dialer.target name required"
						}
						break
					}
				}
				if !found {
					return "dialer.dangling reference"
				}
			}
		}
		if issue := dialerProfileIssue(obj(row.args[0])); issue != "" {
			return issue
		}
		switch row.fn {
		case "dialer_target", "resolve_dialer", "overlay_dialer":
			return dialerResourceIssue(row.args[1], "node")
		case "group_dependencies":
			return dialerResourceIssue(row.args[1], "group")
		}
	case "raw_dialer":
		return dialerResourceIssue(row.args[0], "node")
	case "group_members":
		return dialerResourceIssue(row.args[0], "group")
	case "filter_provider_candidates":
		for _, item := range row.args[0].([]any) {
			if issue := emittedShapeIssue(item); issue != "" {
				return issue
			}
		}
		for _, arg := range row.args[1:] {
			if _, ok := arg.(string); !ok {
				return "dialer.filter string"
			}
		}
	case "provider_proxy_name":
		if issue := emittedShapeIssue(row.args[0]); issue != "" {
			return issue
		}
		return nameOverrideIssue(row.args[1])
	case "validate_emitted":
		config := obj(row.args[0])
		for _, key := range []string{"proxies", "proxy-groups"} {
			if raw, present := config.Get(key); present {
				items, ok := raw.([]any)
				if !ok {
					return "dialer.emitted array"
				}
				for _, item := range items {
					if issue := emittedShapeIssue(item); issue != "" {
						return issue
					}
				}
			}
		}
		if raw, present := config.Get("proxy-providers"); present {
			definitions, ok := raw.(*JObj)
			if !ok {
				return "dialer.providers object"
			}
			for _, raw := range definitions.Values() {
				if issue := emittedShapeIssue(raw); issue != "" {
					return issue
				}
				definition := raw.(*JObj)
				if override, present := definition.Get("override"); present {
					if issue := nameOverrideIssue(override); issue != "" {
						return issue
					}
					if issue := dialerFieldType(override.(*JObj), "dialer-proxy", "string", true); issue != "" {
						return issue
					}
				}
			}
		}
		if row.args[2] != nil {
			payloads, ok := row.args[2].(*JObj)
			if !ok {
				return "dialer.payloads object"
			}
			for _, raw := range payloads.Values() {
				items, ok := raw.([]any)
				if !ok {
					return "dialer.payload array"
				}
				for _, item := range items {
					if issue := emittedShapeIssue(item); issue != "" {
						return issue
					}
				}
			}
		}
	}
	return ""
}
func nativeProviderReplay(t *testing.T, row corpusRow, result any, err error) bool {
	t.Helper()
	if row.fn != "provider_proxy_name" {
		return false
	}
	rules := obj(row.args[1]).GetList("proxy-name")
	if len(rules) != 1 {
		return false
	}
	rule, ok := rules[0].(*JObj)
	if !ok || rule.GetString("pattern") != "(b)" || obj(row.args[0]).GetString("name") != "abc" {
		return false
	}
	target := rule.GetString("target")
	if target != "<$١>" && target != "<$１>" {
		return false
	}
	if err != nil || result != "a"+target+"c" {
		t.Fatalf("native group digits: %v %v", result, err)
	}
	return true
}

func nativeProviderBoundary(t *testing.T, row corpusRow, result any, err error) bool {
	t.Helper()
	if row.fn == "filter_provider_candidates" && row.args[1] == "a{4294967295}" {
		if err == nil || err.Error() != msgProviderFilter {
			t.Fatalf("native regex validation: %v", err)
		}
		return true
	}
	if row.fn != "provider_proxy_name" {
		return false
	}
	overrides := obj(row.args[1])
	rules := overrides.GetList("proxy-name")
	if len(rules) != 1 {
		return false
	}
	rule, ok := rules[0].(*JObj)
	if !ok {
		return false
	}
	if rule.GetString("pattern") == "a{99999999999}" {
		if err == nil || err.Error() != msgProviderNameRule {
			t.Fatalf("native name-rule validation: %v", err)
		}
		return true
	}
	if rule.GetString("pattern") != "(?P<n>H)" || rule.GetString("target") != "$1$0$١" {
		return false
	}
	key := obj(row.args[0]).GetString("name") + "\x00" + overrides.GetString("additional-prefix") + "\x00" + overrides.GetString("additional-suffix")
	wants := map[string]string{
		"排序_1_HK\x00\x00":   "排序_1_HH$١K",
		"H\nK\x00\x00pre-":  "HH$١\nKpre-",
		"HK 01\x00\x00":     "HH$١K 01",
		"HK 01\x00\x00pre-": "HH$١K 01pre-",
		"H\nK\x00-suf\x00":  "-sufHH$١\nK",
		"H\nK\x00\x00-suf":  "HH$١\nK-suf",
	}
	want, ok := wants[key]
	if !ok {
		return false
	}
	if err != nil || result != want {
		t.Fatalf("unclassified Unicode placeholder boundary %q: %v %v", key, result, err)
	}
	return true
}

// DialerProfileReplayIssue exposes only the owned-field schema classifier to
// external generator tests; it never computes a generated business result.
func DialerProfileReplayIssue(profile *JObj) string { return dialerProfileIssue(profile) }
