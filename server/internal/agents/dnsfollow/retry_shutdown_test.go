package dnsfollow

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"configflow/internal/agents/agentfake"
	"configflow/internal/model"
)

func TestShutdownBetweenRetryCommitsRetainsCompleteWaitingState(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.editAgent(t, "target", func(a *model.Agent) {
		_ = model.SetStoredMember(&a, "dns_follow_state", []byte(`{"status":"failed"}`))
		_ = model.SetStoredMember(&a, "dns_follow_job", []byte(`{"binding_id":"old-private-job"}`))
	})
	rt := startRuntime(t, f.ctx)
	ctx := rt.BindRequest(f.ctx)
	before := durable(t, f)
	commits := f.r.CommitCount()
	calls := 0
	// The clock locates the second real UpdateAgents callback. Stop the actual
	// Runtime there, after reset committed but before the queued event can commit.
	f.s.now = func() time.Time {
		calls++
		if calls == 2 {
			rt.Stop(20 * time.Millisecond)
		}
		return time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	}
	_, err := f.s.Retry(ctx, "target")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err, calls)
	}
	a := f.agent(t)
	st := state(a)
	if st.Status != "waiting" || !value(st.RetryRequested) || value(st.Attempt) != 1 || a.DNSFollowJob != nil || len(st.Events) != 0 || f.r.CommitCount() != commits+1 {
		t.Fatal("partial retry reset", st.Status, st.Message, st.Attempt)
	}
	after := durable(t, f)
	delete(before, "@DB")
	delete(after, "@DB")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("late taskstatus/file write")
	}
}
