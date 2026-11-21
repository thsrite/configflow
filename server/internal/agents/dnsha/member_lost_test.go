package dnsha

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
)

// confirmedHandoff advances a pair publication until the VIP handoff to the
// updated standby is confirmed, then cuts the standby off the network.
func confirmedHandoff(t *testing.T, f *fixture) {
	t.Helper()
	f.publish(t, "primary", "member-lost", true)
	for range 12 {
		if _, e := f.s.Advance(f.ctx, "primary"); e != nil {
			t.Fatal(e)
		}
		a, e := f.s.get(f.ctx, "primary")
		if e != nil {
			t.Fatal(e)
		}
		if h := a.OxiDNSHAPublication.Handoff; h != nil && yes(h.Confirmed) {
			f.client.setBefore(func(_ context.Context, r agents.Request) error {
				if strings.Contains(r.URL, secondaryHost) {
					return errors.New("synthetic member offline")
				}
				return nil
			})
			return
		}
	}
	t.Fatal("handoff never confirmed")
}

// TestLostMemberStopsPairAfterTimeout covers lab O-1: the new VIP holder
// goes offline after the confirmed handoff and never answers again.
func TestLostMemberStopsPairAfterTimeout(t *testing.T) {
	tests := []struct {
		name    string
		returns bool
	}{
		{"never-returns", false},
		{"returns-in-time", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			f := fixtureNew(t, Options{Now: func() time.Time { return now }}, true, nil)
			confirmedHandoff(t, f)
			last := f.drain(t, 3)
			if last.Status != "unknown" || last.Message != msgConnectionUnknown {
				t.Fatalf("offline member: %s %s", last.Status, last.Message)
			}
			now = now.Add(MemberLostTimeout - time.Minute)
			if last = f.drain(t, 2); last.Status != "unknown" {
				t.Fatalf("before timeout: %s %s", last.Status, last.Message)
			}
			if _, e := f.s.Disable(f.ctx, "primary", true); e == nil || !IsUserError(e) {
				t.Fatalf("disable during an unfinished pair publication: %v", e)
			}
			if tt.returns {
				f.client.setBefore(nil)
				if last = f.drain(t, 12); last.Status != "succeeded" {
					t.Fatalf("member returned: %s %s", last.Status, last.Message)
				}
				a, e := f.s.get(f.ctx, "primary")
				if e != nil {
					t.Fatal(e)
				}
				if a.OxiDNSHAPublication.UnreachableSince != nil {
					t.Fatal("lost-member clock kept after progress")
				}
				return
			}
			now = now.Add(time.Minute)
			last = f.drain(t, 2)
			// The failure releases the old holder's yield first and waits for
			// the election up to HandoffTimeout before it settles.
			now = now.Add(HandoffTimeout + time.Second)
			if last = f.drain(t, 4); last.Status != "failed" || last.Message != msgMemberLost {
				t.Fatalf("after timeout: %s %s", last.Status, last.Message)
			}
			if _, e := f.s.Disable(f.ctx, "primary", false); e == nil {
				t.Fatal("disable without force reached an offline member")
			} else {
				var refusal *Error
				if !errors.As(e, &refusal) || !refusal.CanForce {
					t.Fatalf("disable refusal: %v", e)
				}
			}
			out, e := f.s.Disable(f.ctx, "primary", true)
			if e != nil || !out.Success {
				t.Fatalf("forced disable: %+v %v", out, e)
			}
			a, e := f.s.get(f.ctx, "primary")
			if e != nil {
				t.Fatal(e)
			}
			if haEnabled(a) || a.OxiDNSHAPublication != nil {
				t.Fatal("forced disable kept HA records")
			}
		})
	}
}
