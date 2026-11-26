package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// HR O18 T1a: lists that encoding/json/v2 decodes into plain records (input
// through SetJSON/DecodeDocument) have no spare capacity (borrowed reads).
// repo's TestO10R2SnapshotSlicesHaveNoSpareCapacity only sees stored lists,
// which decodeRecordList allocates exactly, so it passes without clipSlices.
func TestReviewO18T1aInputListsHaveNoSpareCapacity(t *testing.T) {
	for _, n := range []int{1, 3, 5, 9} {
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprintf(`{"id":"g%d","name":"g%d","type":"select","proxies":[]}`, i, i)
		}
		list := json.RawMessage("[" + strings.Join(items, ",") + "]")
		var profile Profile
		if err := profile.SetJSON("proxy_groups", list); err != nil {
			t.Fatal(err)
		}
		if cap(profile.ProxyGroups) != len(profile.ProxyGroups) || len(profile.ProxyGroups) != n {
			t.Errorf("SetJSON %d groups: len %d cap %d", n, len(profile.ProxyGroups), cap(profile.ProxyGroups))
		}
		var shared Shared
		if err := DecodeInput("shared", []byte(`{"nodes":[`+strings.Repeat(`{"name":"n","type":"direct"},`, n-1)+`{"name":"n","type":"direct"}]}`), &shared); err != nil {
			t.Fatal(err)
		}
		if cap(shared.Nodes) != len(shared.Nodes) || len(shared.Nodes) != n {
			t.Errorf("DecodeInput %d nodes: len %d cap %d", n, len(shared.Nodes), cap(shared.Nodes))
		}
		document, err := DecodeDocument([]byte(`{"schema_version":5,"system":{},"shared":{},"profiles":{"p":{"id":"p","proxy_groups":` + string(list) + `}}}`))
		if err != nil {
			t.Fatal(err)
		}
		if groups := document.Profiles[0].ProxyGroups; cap(groups) != len(groups) || len(groups) != n {
			t.Errorf("DecodeDocument %d groups: len %d cap %d", n, len(groups), cap(groups))
		}
	}
}
