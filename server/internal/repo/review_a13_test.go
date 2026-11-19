package repo

import (
	"context"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

// TestReviewAgentInspectionPrecedesLightCopy checks the view and no-history boundary.
func TestReviewAgentInspectionPrecedesLightCopy(t *testing.T) {
	r := newRepo(t)
	profile := must(r.DefaultProfileID(bg))(t)
	must(r.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{jsonx.Obj("id", "synthetic", "profile_id", profile, "deployments", jsonx.Obj("old", jsonx.Obj("api_key", "synthetic-key")), "journal", "old synthetic-key")})
		return system, nil
	}))(t)
	entry := must(r.readEntry(bg, false))(t)
	original := must(entry.legacyDocument())(t).GetObject("system").GetList("agents")[0].(*jsonx.Object)
	var key string
	light := must(r.GetAgentsInspect(bg, false, func(raw *jsonx.Object) {
		if raw != original {
			t.Error("inspection copied the original record/history")
		}
		key = raw.GetObject("deployments").GetObject("old").GetString("api_key")
	}))(t)
	if key != "synthetic-key" || light[0].(*jsonx.Object).Value("deployments") != AgentHistoryNotRead {
		t.Fatal("inspection did not precede the light-read projection")
	}
	light[0].(*jsonx.Object).Set("journal", "changed copy")
	if original.GetString("journal") != "old synthetic-key" {
		t.Fatal("light result mutated the inspected view")
	}
}

// TestReviewInspectionSnapshotSurvivesCommit rejects mixed-generation secret seeds.
func TestReviewInspectionSnapshotSurvivesCommit(t *testing.T) {
	r := newRepo(t)
	profile := must(r.DefaultProfileID(bg))(t)
	must(r.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{jsonx.Obj("id", "synthetic", "profile_id", profile, "password", "old-key", "journal", "old-key")})
		return system, nil
	}))(t)
	inspected, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(proceed) }) }
	defer release()
	type result struct {
		records []any
		key     string
		err     error
	}
	done := make(chan result, 1)
	go func() {
		var key string
		records, err := r.GetAgentsInspect(bg, false, func(raw *jsonx.Object) {
			key = raw.GetString("password")
			close(inspected)
			<-proceed
		})
		done <- result{records, key, err}
	}()
	select {
	case <-inspected:
	case <-time.After(5 * time.Second):
		t.Fatal("inspection did not start")
	}
	committed := make(chan error, 1)
	go func() {
		_, err := r.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
			agent := system.GetList("agents")[0].(*jsonx.Object)
			agent.Set("password", "new-key")
			agent.Set("journal", "new-key")
			return system, nil
		})
		committed <- err
	}()
	select {
	case err := <-committed:
		noError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("read-only inspection blocked a writer")
	}
	release()
	read := <-done
	if read.err != nil || read.key != "old-key" || read.records[0].(*jsonx.Object).GetString("journal") != read.key {
		t.Fatal("credential snapshot and light record came from different generations")
	}
	latest := must(r.GetAgents(bg, false))(t)
	if latest[0].(*jsonx.Object).GetString("journal") != "new-key" {
		t.Fatal("concurrent commit was lost")
	}
}
