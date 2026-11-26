package dnsha

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/model"
)

// confirmedHandoff advances a pair publication until the VIP handoff to the
// updated standby is confirmed, then cuts the standby off the network.
func confirmedHandoff(t *testing.T, f *fixture) {
	t.Helper()
	f.publish(t, "primary", "member-lost", true)
	var last *model.HAPublicationSummary
	for range 12 {
		out, e := f.s.Advance(f.ctx, "primary")
		if e != nil {
			t.Fatal(e)
		}
		last = out
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
	// Name the state the pair stopped in: a storage failure (e.g. a full
	// test disk) fails the publication and would otherwise look like a
	// handoff that never progressed.
	if last != nil {
		t.Fatalf("handoff never confirmed: %s/%s %s", last.Status, last.Phase, last.Message)
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

// TestLostMembersBothOfflineKeepPairUnknown covers the O-1 corner where the
// old holder is offline too when the timeout fires: its submitted child cannot
// be proven and its yield cannot be revoked, so the pair stays unknown under
// the lost-member message (logged once) until it answers, then settles as the
// lost-member failure.
func TestLostMembersBothOfflineKeepPairUnknown(t *testing.T) {
	now := time.Now()
	f := fixtureNew(t, Options{Now: func() time.Time { return now }}, true, nil)
	confirmedHandoff(t, f)
	if last := f.drain(t, 3); last.Status != "unknown" {
		t.Fatalf("offline member: %s %s", last.Status, last.Message)
	}
	f.client.setBefore(func(context.Context, agents.Request) error { return errors.New("synthetic pair offline") })
	now = now.Add(MemberLostTimeout + time.Second)
	for range 3 {
		// Transport errors are the expected outcome of these rounds.
		_, _ = f.s.Advance(f.ctx, "primary")
	}
	a, e := f.s.get(f.ctx, "primary")
	if e != nil {
		t.Fatal(e)
	}
	j := a.OxiDNSHAPublication
	if j.Status != "unknown" || !strings.HasPrefix(j.Message, msgMemberLost) || j.UnreachableSince == nil {
		t.Fatalf("both offline after timeout: %s %s", j.Status, j.Message)
	}
	if _, e = f.s.Disable(f.ctx, "primary", true); e == nil || !IsUserError(e) {
		t.Fatalf("disable while the yield is unrevoked: %v", e)
	}
	f.client.setBefore(func(_ context.Context, r agents.Request) error {
		if strings.Contains(r.URL, secondaryHost) {
			return errors.New("synthetic member offline")
		}
		return nil
	})
	f.drain(t, 2)
	now = now.Add(HandoffTimeout + time.Second)
	if last := f.drain(t, 4); last.Status != "failed" || last.Message != msgMemberLost {
		t.Fatalf("old holder back: %s %s", last.Status, last.Message)
	}
}
