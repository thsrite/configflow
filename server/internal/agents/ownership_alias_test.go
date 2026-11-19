package agents

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestPreparedOwnershipOriginalMemberCannotBeRepairedByAlias(t *testing.T) {
	for _, tt := range []struct {
		member, value, alias string
		accepted             bool
	}{
		{"profile_id", `7`, `"7"`, false},
		{"submitted", `"true"`, ``, false},
		{"submitted", `"false"`, ``, false},
		{"submitted", `true`, ``, false},
		{"submitted", `false`, ``, true},
		{"submitted", `null`, ``, true},
	} {
		t.Run(tt.member+"_"+tt.value, func(t *testing.T) {
			e := newEnv(t)
			e.createProfile("7")
			if err := e.m.EditAgents(e.ctx, "7", func(_ context.Context, records *[]model.Agent) error {
				registryAgent(*records, e.id()).ProfileID = "7"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			id := "ownership-proof"
			e.begin(id)
			defer e.finish(id)
			if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error {
				a := registryAgent(*records, e.id())
				state, ok := a.Deployments.Get(id)
				if !ok {
					t.Fatal("missing task")
				}
				if err := state.SetJSON(tt.member, []byte(tt.value)); err != nil {
					return err
				}
				if tt.alias != "" {
					if err := state.Fields.Set("Profile_ID", []byte(tt.alias)); err != nil {
						return err
					}
				}
				a.Deployments.Set(id, state)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			head, _ := e.repo.Head(e.ctx)
			err := e.m.RememberPreparedContent(e.ctx, e.id(), id, []byte(`{"profile_id":"7","service_type":"mihomo"}`), nil)
			if (err == nil) != tt.accepted {
				t.Fatalf("member=%s raw=%s alias=%s accepted=%v err=%v", tt.member, tt.value, tt.alias, err == nil, err)
			}
			if !tt.accepted {
				if after, _ := e.repo.Head(e.ctx); after != head {
					t.Fatal("rejected ownership committed")
				}
			}
		})
	}
}

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
