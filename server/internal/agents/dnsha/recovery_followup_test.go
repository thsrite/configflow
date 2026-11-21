package dnsha

import (
	"context"
	"errors"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
)

// rollbackFailedPair publishes a pair whose standby member reports
// rollback_failed and returns that member's child deployment id.
func rollbackFailedPair(t *testing.T, f *fixture) string {
	t.Helper()
	f.client.setAfter(failedRollbackReplies)
	f.publish(t, "primary", "recover-followup", true)
	last := f.drain(t, 6)
	if last == nil || last.Status != "rollback_failed" {
		t.Fatalf("setup: %+v", last)
	}
	a, e := f.s.get(f.ctx, "secondary")
	if e != nil {
		t.Fatal(e)
	}
	did, ok := journalChildID(a.OxiDNSHAPublication, "secondary")
	if !ok || did == "" {
		t.Fatal("standby child missing")
	}
	return did
}

// failedRollbackReplies makes the standby Agent report rollback_failed.
func failedRollbackReplies(_ context.Context, r agents.Request, reply *agents.Response) error {
	if r.Method == "GET" && strings.Contains(r.URL, secondaryHost) && strings.Contains(r.URL, "/api/deployments/") {
		o := parseObject(reply.Body)
		o["status"] = []byte(`"rollback_failed"`)
		o["success"] = []byte("false")
		reply.Body = rawObject(o)
	}
	return nil
}

func storedChild(t *testing.T, f *fixture, id, did string) model.Deployment {
	t.Helper()
	a, e := f.s.get(f.ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	d, _ := a.Deployments.Get(did)
	return d
}

// TestRecoveredMemberSettlesPairDeployment covers lab L3-D2: after 「重新恢复」
// the member task is rolling_back and the pair GET must ask the Agent.
func TestRecoveredMemberSettlesPairDeployment(t *testing.T) {
	tests := []struct {
		name string
		// during configures the fake while the pair GET runs after recovery.
		during      func(f *fixture)
		wantChild   string
		wantJournal string
	}{
		{"recovered", func(f *fixture) { f.client.setAfter(nil) }, "rolled_back", "failed"},
		{"still-failed", func(f *fixture) {}, "rollback_failed", "rollback_failed"},
		{"agent-offline", func(f *fixture) {
			f.client.setAfter(nil)
			f.client.setBefore(func(_ context.Context, r agents.Request) error {
				if strings.Contains(r.URL, secondaryHost) {
					return errors.New("synthetic agent offline")
				}
				return nil
			})
		}, "rolling_back", "rollback_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fixtureNew(t, Options{}, true, nil)
			did := rollbackFailedPair(t, f)
			recovered, e := f.m.RecoverDeploymentModel(f.ctx, "secondary", did)
			if e != nil {
				t.Fatal(e)
			}
			if recovered.Status != "rolling_back" {
				t.Fatalf("recover: %s", recovered.Status)
			}
			tt.during(f)
			for _, id := range []string{"secondary", "primary"} {
				// Both the page GET and the background loop must settle it.
				if id == "secondary" {
					if _, e = f.s.PairDeployment(f.ctx, "secondary", did); e != nil {
						t.Fatal(e)
					}
				} else if e = f.s.Reconcile(f.ctx); e != nil {
					t.Fatal(e)
				}
			}
			if got := storedChild(t, f, "secondary", did).Status; got != tt.wantChild {
				t.Fatalf("child status = %s, want %s", got, tt.wantChild)
			}
			a, e := f.s.get(f.ctx, "primary")
			if e != nil {
				t.Fatal(e)
			}
			if j := a.OxiDNSHAPublication; j.Status != tt.wantJournal {
				t.Fatalf("journal = %s %s, want %s", j.Status, j.Message, tt.wantJournal)
			}
			switch tt.name {
			case "recovered":
				if a.OxiDNSHAPublication.Message != msgRecovered {
					t.Fatalf("journal message = %s", a.OxiDNSHAPublication.Message)
				}
				f.publish(t, "primary", "after-recovery", true)
				if last := f.drain(t, 12); last == nil || last.Status != "succeeded" {
					t.Fatalf("publication after recovery: %+v", last)
				}
			case "still-failed":
				again, e := f.m.RecoverDeploymentModel(f.ctx, "secondary", did)
				if e != nil {
					t.Fatal(e)
				}
				if again.Status != "rolling_back" {
					t.Fatalf("second recovery refused: %s", again.Status)
				}
			case "agent-offline":
				f.client.setBefore(nil)
				if _, e = f.s.PairDeployment(f.ctx, "secondary", did); e != nil {
					t.Fatal(e)
				}
				if got := storedChild(t, f, "secondary", did).Status; got != "rolled_back" {
					t.Fatalf("child after reconnect = %s", got)
				}
			}
		})
	}
}

// TestSettledJournalPollsReopenedMember covers a member left open under an
// already settled journal: the pair GET records the Agent's answer.
func TestSettledJournalPollsReopenedMember(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	did := rollbackFailedPair(t, f)
	if _, e := f.m.RecoverDeploymentModel(f.ctx, "secondary", did); e != nil {
		t.Fatal(e)
	}
	if e := f.s.update(f.ctx, func(list []*agent) error {
		for _, a := range list {
			if a.OxiDNSHAPublication != nil {
				a.OxiDNSHAPublication.Status = "failed"
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	f.client.setAfter(nil)
	if _, e := f.s.PairDeployment(f.ctx, "secondary", did); e != nil {
		t.Fatal(e)
	}
	if got := storedChild(t, f, "secondary", did).Status; got != "rolled_back" {
		t.Fatalf("settled journal member = %s", got)
	}
}
