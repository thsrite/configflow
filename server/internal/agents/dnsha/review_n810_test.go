package dnsha

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

// A legacy pair whose identities were stored with the wrong JSON types no
// longer forms a pair for publication, but the UI's 停用 path must still
// remove the HA members from both records.
func TestReviewN810MalformedPairIdentityStaysRepairable(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(a *jsonx.Object)
	}{
		{"numeric group", func(a *jsonx.Object) { a.GetObject("oxidns_ha").Set("group_id", jsonx.Int(7)) }},
		{"numeric peer", func(a *jsonx.Object) { a.GetObject("oxidns_ha").Set("peer_id", jsonx.Int(7)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fixtureNew(t, Options{}, true, nil)
			_, e := f.m.UpdateAgents(f.ctx, "", func(_ context.Context, rows *[]any) (any, error) {
				for _, v := range *rows {
					tt.corrupt(v.(*jsonx.Object))
				}
				return nil, nil
			})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.s.pair(f.ctx, "primary"); e == nil || !IsUserError(e) {
				t.Fatalf("coerced identity formed a pair: %v", e)
			}
			if _, e = f.m.ReadHAPair(f.ctx, "primary"); !errors.Is(e, agents.ErrPairIncomplete) {
				t.Fatalf("registry pair: %v", e)
			}
			// Each member is released through the same UI action. When the peer
			// identity is unusable, the other member is released by its own 停用.
			for _, id := range []string{"primary", "secondary"} {
				a, e := f.m.GetAgentByID(f.ctx, id, true)
				if e != nil || a == nil {
					t.Fatal(a, e)
				}
				if !a.Has("oxidns_ha") && !a.Has("oxidns_ha_state") {
					continue
				}
				if _, e = f.s.Disable(f.ctx, id, false); e != nil {
					var he *Error
					if !errors.As(e, &he) || !he.CanForce {
						t.Fatal(id, e)
					}
					if _, e = f.s.Disable(f.ctx, id, true); e != nil {
						t.Fatal(id, e)
					}
				}
			}
			for _, id := range []string{"primary", "secondary"} {
				a, e := f.m.GetAgentByID(f.ctx, id, true)
				if e != nil || a == nil {
					t.Fatal(a, e)
				}
				if a.Has("oxidns_ha") || a.Has("oxidns_ha_state") {
					t.Fatalf("%s kept malformed HA identity: %v", id, a.Value("oxidns_ha"))
				}
			}
		})
	}
}
