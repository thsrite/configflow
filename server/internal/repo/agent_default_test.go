package repo

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"configflow/internal/model"
	"configflow/internal/store"
)

func TestAgentDefaultProfileUsesScopedRowView(t *testing.T) {
	r := agentViewRepo(t)
	for _, id := range []string{"selected", "7"} {
		if _, err := r.CreateProfileRecord(t.Context(), ProfileCreation{ID: id, Name: id}, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{`{}`, DefaultProfileID}, {`{"default_profile_id":"selected"}`, "selected"},
		{`{"default_profile_id":null}`, DefaultProfileID}, {`{"default_profile_id":7}`, "7"},
		{`false`, DefaultProfileID},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			base, err := r.store.Load()
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(base.Document)
			if err != nil {
				t.Fatal(err)
			}
			var changed store.Document
			if err = json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			for i := range changed.System.Fields {
				if changed.System.Fields[i].Name == "system_config" {
					changed.System.Fields[i].Value = json.RawMessage(tc.raw)
				}
			}
			if _, committed, err := r.store.Commit(base, &changed, nil); err != nil || !committed {
				t.Fatalf("raw import fixture: committed=%v %v", committed, err)
			}
			r.dropCache()
			got, err := r.AgentDefaultProfileID(t.Context())
			if err != nil || got != tc.want {
				t.Fatalf("%q want %q: %v", got, tc.want, err)
			}
			if err := r.WithReadSnapshot(t.Context(), func(ctx context.Context) error {
				got, err := r.AgentDefaultProfileID(ctx)
				if err == nil && got != tc.want {
					t.Fatalf("pinned default %q", got)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	abort := errors.New("abort proposal")
	err := r.UpdateSystem(t.Context(), func(ctx context.Context, s *model.System) error {
		if err := s.SetJSON("system_config", []byte(`{"default_profile_id":"proposal"}`)); err != nil {
			return err
		}
		got, err := r.AgentDefaultProfileID(ctx)
		if err != nil || got != "proposal" {
			t.Fatalf("working default %q %v", got, err)
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if got, err := r.AgentDefaultProfileID(t.Context()); err != nil || got != DefaultProfileID {
		t.Fatalf("discarded proposal leaked: %q %v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.AgentDefaultProfileID(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestAgentDefaultProfileCoalescesColdGeneration(t *testing.T) {
	r := agentViewRepo(t)
	if _, err := r.CreateProfileRecord(t.Context(), ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{DefaultProfileID, "other"} {
		if want != "default" {
			if _, err := r.SetDefaultProfile(t.Context(), want); err != nil {
				t.Fatal(err)
			}
		}
		before := r.loads.Load()
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() {
				got, err := r.AgentDefaultProfileID(t.Context())
				if err == nil && got != want {
					err = errors.New("wrong default generation")
				}
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if loads := r.loads.Load() - before; loads != 1 {
			t.Fatalf("decoded cold generation %d times for eight Agents", loads)
		}
	}
}
