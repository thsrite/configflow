package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

// borrowedReads are the migrated read APIs, each returning what the caller
// received so it can be encoded and compared.
var borrowedReads = []struct {
	name string
	read func(context.Context, *Manager) (any, error)
}{
	{"ReadAgent", func(ctx context.Context, m *Manager) (any, error) { return m.ReadAgent(ctx, "p1", true) }},
	{"ReadAgentNoHistory", func(ctx context.Context, m *Manager) (any, error) { return m.ReadAgent(ctx, "bulk-0", false) }},
	{"ReadAgents", func(ctx context.Context, m *Manager) (any, error) { return m.ReadAgents(ctx, true, nil) }},
	{"ListAgents", func(ctx context.Context, m *Manager) (any, error) { return m.ListAgents(ctx, false) }},
	{"AgentSnapshots", func(ctx context.Context, m *Manager) (any, error) { return m.AgentSnapshots(ctx, true) }},
	{"ReadHAPair", func(ctx context.Context, m *Manager) (any, error) { return m.ReadHAPair(ctx, "p1") }},
	{"AgentPair", func(ctx context.Context, m *Manager) (any, error) { return m.AgentPair(ctx, "p2") }},
	{"ReadAgentByToken", func(ctx context.Context, m *Manager) (any, error) { return m.ReadAgentByToken(ctx, "synthetic-p1") }},
}

// commitRound changes stored members at every depth the borrowed records can
// reach: top-level scalars, nested pointer structs, and Map entries.
func commitRound(ctx context.Context, m *Manager, round int) error {
	return m.EditAgents(ctx, "", func(_ context.Context, records *[]model.Agent) error {
		p1 := registryAgent(*records, "p1")
		p1.Host = fmt.Sprintf("192.0.2.%d", round%200+1)
		if p1.OxiDNSHA != nil {
			p1.OxiDNSHA.Role = fmt.Sprintf("role-%d", round)
		}
		bulk := registryAgent(*records, "bulk-0")
		state, _ := bulk.Deployments.Get("dep-0000")
		if err := model.SetStoredMember(&state, "message", json.RawMessage(fmt.Sprintf(`"round %d"`, round))); err != nil {
			return err
		}
		bulk.Deployments.Set("dep-0000", state)
		return nil
	})
}

func encodeBorrowed(t testing.TB, value any) string {
	t.Helper()
	data, err := model.Marshal(value)
	if err != nil {
		t.Error(err)
	}
	return string(data)
}

// TestBorrowedReadsSurviveConcurrentCommits: N readers borrow and traverse
// records while one writer commits in a loop and observes heartbeats. Every
// borrowed result must encode identically before and after later commits.
func TestBorrowedReadsSurviveConcurrentCommits(t *testing.T) {
	const readers, rounds = 4, 25
	for _, api := range borrowedReads {
		t.Run(api.name, func(t *testing.T) {
			m := haPairManager(t, 3)
			ctx := context.Background()
			var stop atomic.Bool
			var wg sync.WaitGroup
			for range readers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for !stop.Load() {
						got, err := api.read(ctx, m)
						if err != nil {
							t.Error(err)
							return
						}
						first := encodeBorrowed(t, got)
						if again := encodeBorrowed(t, got); again != first {
							t.Error("borrowed result changed while reading")
							return
						}
					}
				}()
			}
			for round := range rounds {
				before, err := api.read(ctx, m)
				if err != nil {
					t.Fatal(err)
				}
				encoded := encodeBorrowed(t, before)
				if err := commitRound(ctx, m, round); err != nil {
					t.Fatal(err)
				}
				if _, err := m.ObserveHeartbeat(ctx, "p1", &model.Agent{Version: fmt.Sprintf("1.0.%d", round)}); err != nil {
					t.Fatal(err)
				}
				if after := encodeBorrowed(t, before); after != encoded {
					t.Fatalf("round %d: commit changed a borrowed result", round)
				}
			}
			stop.Store(true)
			wg.Wait()
			if err := m.Repository().CheckBorrowed(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestBorrowedReadsAreCallerShallowCopies: top-level reassignment and the
// model's copy-on-write setters on a returned record never reach the
// generation, so later reads see the stored values.
func TestBorrowedReadsAreCallerShallowCopies(t *testing.T) {
	m := haPairManager(t, 1)
	ctx := context.Background()
	a, err := m.ReadAgent(ctx, "p1", true)
	if err != nil || a == nil {
		t.Fatal(a, err)
	}
	a.Status, a.Name = "offline", "changed"
	if err := model.SetStoredMember(&a, "host", json.RawMessage(`"198.51.100.7"`)); err != nil {
		t.Fatal(err)
	}
	list, err := m.ListAgents(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	list[0] = model.Agent{ID: "replaced"}
	again, err := m.ReadAgent(ctx, "p1", true)
	if err != nil || again == nil {
		t.Fatal(again, err)
	}
	if again.Name != "primary" || again.Host != "" || again.Status == "offline" {
		t.Fatalf("caller change reached the generation: %+v", again)
	}
	if first, err := m.ReadAgents(ctx, true, nil); err != nil || first[0].ID != "p1" {
		t.Fatal("element assignment reached the generation", err)
	}
	if err := m.Repository().CheckBorrowed(); err != nil {
		t.Fatal(err)
	}
}

// TestViewGuardCatchesBorrowedMutation is the negative control of the guard:
// modifying a nested value of a borrowed record must be caught on the next
// borrow, and the default test guard must fail loudly.
func TestViewGuardCatchesBorrowedMutation(t *testing.T) {
	mutations := []struct {
		name, id string
		mutate   func(*model.Agent)
	}{
		{"nested pointer", "p1", func(a *model.Agent) { a.OxiDNSHA.Role = "tampered" }},
		{"pointer scalar", "p1", func(a *model.Agent) { *a.OxiDNSHA.Enabled = false }},
		{"map value pointer", "bulk-0", func(a *model.Agent) {
			state, _ := a.Deployments.Get("dep-0000")
			*state.Message = "tampered"
		}},
		{"nested slice element", "bulk-0", func(a *model.Agent) {
			state, _ := a.Deployments.Get("dep-0000")
			state.Events[0].Status = "tampered"
		}},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			m := haPairManager(t, 1)
			ctx := context.Background()
			r := m.Repository()
			caught := 0
			r.SetViewGuard(func() { caught++ })
			id := tt.id
			a, err := m.ReadAgent(ctx, id, true)
			if err != nil || a == nil {
				t.Fatal(a, err)
			}
			if err := r.CheckBorrowed(); err != nil {
				t.Fatal("untouched generation reported", err)
			}
			tt.mutate(a)
			if err := r.CheckBorrowed(); !errors.Is(err, repo.ErrSnapshotMutated) {
				t.Fatalf("CheckBorrowed = %v, want ErrSnapshotMutated", err)
			}
			if _, err := m.ReadAgent(ctx, id, true); err != nil {
				t.Fatal(err)
			}
			if caught == 0 {
				t.Fatal("guard missed a mutation of a borrowed record")
			}
		})
	}
	t.Run("default guard panics", func(t *testing.T) {
		// haPairManager opens its repository like every test: with the
		// default guard of test binaries.
		m := haPairManager(t, 1)
		ctx := context.Background()
		a, err := m.ReadAgent(ctx, "p1", true)
		if err != nil {
			t.Fatal(err)
		}
		a.OxiDNSHA.Role = "tampered"
		defer m.Repository().SetViewGuard(nil)
		defer func() {
			if recovered := recover(); recovered != repo.ErrSnapshotMutated {
				t.Fatalf("recovered %v, want ErrSnapshotMutated", recovered)
			}
		}()
		_, _ = m.ReadAgent(ctx, "p1", true)
		t.Fatal("default guard did not stop the read")
	})
}

// TestViewGuardReplacedWhileBorrowing: allocation tests switch the guard off
// after Open, while background loops may already borrow. Under -race the guard
// field must not be a plain read/write.
func TestViewGuardReplacedWhileBorrowing(t *testing.T) {
	m := haPairManager(t, 1)
	ctx := context.Background()
	r := m.Repository()
	var stop atomic.Bool
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				if _, err := m.ReadAgents(ctx, false, nil); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for round := range 50 {
		if round%2 == 0 {
			r.SetViewGuard(nil)
		} else {
			r.SetViewGuard(func() { t.Error("untouched generation reported") })
		}
	}
	stop.Store(true)
	wg.Wait()
}
