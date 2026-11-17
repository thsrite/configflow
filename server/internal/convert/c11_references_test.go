package convert_test

import (
	"reflect"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
)

// TestLogicalGraphAndIncomingReferencesProtectChains ports the logical graph
// assertions of test_proxy_chains.py without involving C33's CRUD owners.
func TestLogicalGraphAndIncomingReferencesProtectChains(t *testing.T) {
	value, err := jsonx.ParseString(`{"profile_id":"default","nodes":[
 {"id":"a","name":"A","type":"http","server":"a.example.com","port":80},
 {"id":"b","name":"B","type":"http","server":"b.example.com","port":80}],
 "proxy_groups":[
 {"id":"chain","name":"Chain","type":"chain","chain":{"entry":{"type":"node","id":"a"},"exit":{"type":"node","id":"b"}}},
 {"id":"outer","name":"Outer","type":"chain","chain":{"entry":{"type":"group","id":"chain"},"exit":{"type":"node","id":"b"}}},
 {"id":"g","name":"Group","type":"select","enabled":false,"proxies_order":[{"type":"strategy","id":"chain"}]}],"rule_configs":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	profile := value.(*jsonx.Object)
	before := profile.DeepCopy()
	graph, err := convert.DependencyGraph(profile)
	if err != nil {
		t.Fatal(err)
	}
	children, ok := graph.Children(convert.Ref{Kind: "group", ID: "chain"})
	if !ok || !reflect.DeepEqual(children, []convert.Ref{{Kind: "node", ID: "a"}}) {
		t.Fatalf("chain graph children=%v", children)
	}
	for _, tc := range []struct {
		kind, id string
		want     map[convert.Ref]bool
	}{
		{"group", "chain", map[convert.Ref]bool{{Kind: "group", ID: "outer"}: true, {Kind: "group", ID: "g"}: true}},
		{"node", "a", nil}, {"node", "b", nil},
	} {
		incoming, err := convert.IncomingDialers(profile, tc.kind, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		set := map[convert.Ref]bool{}
		for _, ref := range incoming {
			set[ref] = true
		}
		if tc.want != nil {
			if !reflect.DeepEqual(set, tc.want) {
				t.Fatalf("incoming %s/%s=%v", tc.kind, tc.id, set)
			}
		} else if !set[convert.Ref{Kind: "group", ID: "chain"}] {
			t.Fatalf("chain is not an incoming reference of %s", tc.id)
		}
	}
	if !jsonx.Equal(profile, before) {
		t.Fatal("logical reference inspection mutated stored metadata")
	}
}
