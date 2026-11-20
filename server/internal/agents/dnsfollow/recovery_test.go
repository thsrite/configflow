package dnsfollow

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/model"
)

func TestLostAcceptedReplyRestartAndRollbackFailedNeverUploadTwice(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.publishSource(t, "source-next")
	f.target.mu.Lock()
	f.target.loseUploadResponse = true
	f.target.pollOffline = true
	f.target.mu.Unlock()
	if err := f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	st := state(f.agent(t))
	id := value(st.TargetDeploymentID)
	if st.Status != "publishing" || id == "" || f.agent(t).DNSFollowJob == nil || f.target.uploads() != 2 {
		t.Fatal(st)
	}
	// Real Manager replacement recovers the persisted job and the Agent's journal.
	f.m.Close()
	m, err := agents.New(f.ctx, f.r)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	f.m = m
	f.s = New(m, Options{})
	f.target.mu.Lock()
	f.target.pollOffline = false
	f.target.mu.Unlock()
	remote := f.target.fake.Journal()[id]
	remote.Status = "rollback_failed"
	f.target.fake.SetState(id, remote)
	for i := 0; i < 2; i++ {
		if err = f.s.Tick(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	st = state(f.agent(t))
	if st.Status != "publishing" || st.Message != msgRollbackFailed {
		t.Fatal(st)
	}
	before := f.r.CommitCount()
	if err = f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	if before != f.r.CommitCount() {
		t.Fatal("pending state rewritten")
	}
	_, err = f.s.Retry(f.ctx, "target")
	wantRefusal(t, err, msgRetry)
	remote.Status = "succeeded"
	f.target.fake.SetState(id, remote)
	if err = f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	st = state(f.agent(t))
	if st.Status != "succeeded" || f.target.uploads() != 2 {
		t.Fatal(st)
	}
	if !strings.Contains(f.profile(t), "127.0.0.1:2053") {
		t.Fatal("restart did not finish draft")
	}
}

func TestFailedChildRetryCreatesOneDistinctAttempt(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.publishSource(t, "source-next")
	f.target.mu.Lock()
	f.target.pollOffline = true
	f.target.mu.Unlock()
	if err := f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	id := value(state(f.agent(t)).TargetDeploymentID)
	remote := f.target.fake.Journal()[id]
	remote.Status = "failed"
	f.target.fake.SetState(id, remote)
	f.target.mu.Lock()
	f.target.pollOffline = false
	f.target.activeID = "target-baseline"
	f.target.mu.Unlock()
	if err := f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	if state(f.agent(t)).Status != "failed" {
		t.Fatal(state(f.agent(t)))
	}
	if _, err := f.s.Retry(f.ctx, "target"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	st := state(f.agent(t))
	if st.Status != "succeeded" || value(st.TargetDeploymentID) == id || value(st.Attempt) != 1 || f.target.uploads() != 3 {
		t.Fatal(st)
	}
	for i := 0; i < 3; i++ {
		if err := f.s.Tick(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if f.target.uploads() != 3 {
		t.Fatal("retry uploaded twice")
	}
}

func TestInterruptedPreparationWithoutJobFailsSafely(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	err := f.s.update(f.ctx, func(_ context.Context, rows []*agent) error {
		a := selected(rows, "target")
		a.DNSFollowState.Status = "preparing"
		a.DNSFollowState.TargetDeploymentID = ptr("orphan-child")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	if state(f.agent(t)).Status != "failed" || !strings.Contains(state(f.agent(t)).Message, msgInterrupted) || f.target.uploads() != 1 {
		t.Fatal(state(f.agent(t)))
	}
}

func TestBindingChangedAfterUploadCannotFinalizeDraft(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.publishSource(t, "source-next")
	before := f.profile(t)
	f.target.mu.Lock()
	f.target.hook = func(r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/deployments" {
			f.editAgent(t, "target", func(a *model.Agent) { a.DNSFollow.ID = "replacement-binding" })
		}
	}
	f.target.mu.Unlock()
	if err := f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	st := state(f.agent(t))
	if st.Status != "failed" || !strings.Contains(st.Message, msgFinalOwnership) || f.profile(t) != before || f.target.uploads() != 2 {
		t.Fatal(st.Status, st.Message)
	}
}
