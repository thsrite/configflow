package dnsha

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
)

// A legacy pair whose identities were stored with the wrong JSON types no
// longer forms a pair for publication, but the UI's 停用 path must still
// remove the HA members from both records.
func TestReviewN810MalformedPairIdentityStaysRepairable(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(a *model.Agent)
	}{
		{"numeric group", func(a *model.Agent) {
			raw, _ := json.Marshal(a.OxiDNSHA)
			o := parseObject(raw)
			o["group_id"] = json.RawMessage(`7`)
			_ = a.SetJSON("oxidns_ha", rawObject(o))
		}},
		{"numeric peer", func(a *model.Agent) {
			raw, _ := json.Marshal(a.OxiDNSHA)
			o := parseObject(raw)
			o["peer_id"] = json.RawMessage(`7`)
			_ = a.SetJSON("oxidns_ha", rawObject(o))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fixtureNew(t, Options{}, true, nil)
			e := f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
				for i := range sys.Agents {
					tt.corrupt(&sys.Agents[i])
				}
				return nil
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
				a, e := f.m.ReadAgent(f.ctx, id, true)
				if e != nil || a == nil {
					t.Fatal(a, e)
				}
				if a.OxiDNSHA == nil && a.OxiDNSHAState == nil {
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
				a, e := f.m.ReadAgent(f.ctx, id, true)
				if e != nil || a == nil {
					t.Fatal(a, e)
				}
				if a.OxiDNSHA != nil || a.OxiDNSHAState != nil {
					t.Fatalf("%s kept malformed HA identity: %+v", id, a.OxiDNSHA)
				}
			}
		})
	}
}
