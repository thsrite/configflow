package repo

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"configflow/internal/model"
)

// TestReviewAgentViewSnapshotSurvivesCommit rejects mixed-generation secret seeds
// and ensures read-only inspection never blocks a concurrent writer.
func TestReviewAgentViewSnapshotSurvivesCommit(t *testing.T) {
	r := newRepo(t)
	profile := must(r.DefaultProfileID(bg))(t)
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		return system.SetJSON("agents", []byte(`[{"id":"synthetic","profile_id":"`+profile+`","password":"old-key","journal":"old-key"}]`))
	}))
	inspected, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(proceed) }) }
	defer release()
	type result struct {
		key     string
		journal string
		err     error
	}
	done := make(chan result, 1)
	go func() {
		var key, journal string
		err := r.ViewAgents(bg, func(records []model.Agent) error {
			raw, err := model.Marshal(records[0])
			if err != nil {
				return err
			}
			var fields map[string]string
			if err := json.Unmarshal(raw, &fields); err != nil {
				return err
			}
			key = fields["password"]
			close(inspected)
			<-proceed
			rawAfter, err := model.Marshal(records[0])
			if err != nil {
				return err
			}
			var after map[string]string
			if err := json.Unmarshal(rawAfter, &after); err != nil {
				return err
			}
			journal = after["journal"]
			return nil
		})
		done <- result{key, journal, err}
	}()
	select {
	case <-inspected:
	case <-time.After(5 * time.Second):
		t.Fatal("inspection did not start")
	}
	committed := make(chan error, 1)
	go func() {
		committed <- r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
			return system.SetJSON("agents", []byte(`[{"id":"synthetic","profile_id":"`+profile+`","password":"new-key","journal":"new-key"}]`))
		})
	}()
	select {
	case err := <-committed:
		noError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("read-only inspection blocked a writer")
	}
	release()
	read := <-done
	if read.err != nil || read.key != "old-key" || read.journal != read.key {
		t.Fatalf("credential snapshot and record came from different generations: %+v", read)
	}
	var latest string
	noError(t, r.ViewAgents(bg, func(records []model.Agent) error {
		raw, err := model.Marshal(records[0])
		if err != nil {
			return err
		}
		var fields map[string]string
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		latest = fields["journal"]
		return nil
	}))
	if latest != "new-key" {
		t.Fatalf("concurrent commit was lost: %q", latest)
	}
}
