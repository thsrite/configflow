package agents

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestHAPairIdentityUsesExactStoredKeys(t *testing.T) {
	for _, tt := range []struct {
		raw, group, peer string
		valid            bool
	}{
		{`{"oxidns_ha":{"Group_ID":"group","Peer_ID":"peer"}}`, "", "", false},
		{`{"oxidns_ha":{"group_id":"","peer_id":"","Group_ID":"group","Peer_ID":"peer"}}`, "", "", false},
		{`{"oxidns_ha":{"group_id":"group","peer_id":"peer","Group_ID":"forged","Peer_ID":"forged"}}`, "group", "peer", true},
	} {
		var a model.Agent
		if err := json.Unmarshal([]byte(tt.raw), &a); err != nil {
			t.Fatal(err)
		}
		group, peer, valid := haPairIdentity(&a)
		if valid != tt.valid || valid && (group != tt.group || peer != tt.peer) {
			t.Fatalf("input=%s group=%q peer=%q valid=%v", tt.raw, group, peer, valid)
		}
	}
}
