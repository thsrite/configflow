package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

// readHAPairReference is the pre-O10 ReadHAPair: copy every Agent, then pick.
func readHAPairReference(ctx context.Context, m *Manager, id string) ([]model.Agent, error) {
	records, err := m.ReadAgents(ctx, false, nil)
	if err != nil {
		return nil, err
	}
	record := registryAgent(records, id)
	if !HAEnabledModel(record) {
		return nil, nil
	}
	group, peerID, valid := haPairIdentity(record)
	peer := registryAgent(records, peerID)
	otherGroup, otherID, otherValid := haPairIdentity(peer)
	if !valid || !otherValid || !HAEnabledModel(peer) || group != otherGroup || otherID != id || !haProfilesMatch(record, peer) {
		return nil, ErrPairIncomplete
	}
	return []model.Agent{*record, *peer}, nil
}

func haPairManager(t *testing.T, others int) *Manager {
	t.Helper()
	ResetRuntime()
	t.Cleanup(ResetRuntime)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	decode := func(raw string) model.Agent {
		var a model.Agent
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	list := []model.Agent{
		decode(`{"id":"p1","name":"primary","service_type":"oxidns","profile_id":"default","token":"synthetic-p1","oxidns_ha":{"enabled":true,"group_id":"g","peer_id":"p2","role":"primary"}}`),
		decode(`{"id":"p2","name":"backup","service_type":"oxidns","profile_id":"default","token":"synthetic-p2","oxidns_ha":{"enabled":true,"group_id":"g","peer_id":"p1"}}`),
		decode(`{"id":"half","service_type":"oxidns","profile_id":"default","oxidns_ha":{"enabled":true,"group_id":"g","peer_id":"p1"}}`),
		decode(`{"id":"plain","service_type":"oxidns","profile_id":"default"}`),
	}
	for i := range others {
		unrelated := *buildHistoryAgent(t, 2)
		unrelated.ID = fmt.Sprintf("bulk-%d", i)
		if err := model.SetStoredMember(&unrelated, "id", json.RawMessage(fmt.Sprintf(`"bulk-%d"`, i))); err != nil {
			t.Fatal(err)
		}
		list = append(list, unrelated)
	}
	if err := r.UpdateSystem(context.Background(), func(_ context.Context, s *model.System) error {
		s.Agents = list
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return newManager(t, r)
}

func TestReadHAPairMatchesFullListing(t *testing.T) {
	m := haPairManager(t, 3)
	ctx := context.Background()
	for _, id := range []string{"p1", "p2", "half", "plain", "missing"} {
		t.Run(id, func(t *testing.T) {
			got, gotErr := m.ReadHAPair(ctx, id)
			want, wantErr := readHAPairReference(ctx, m, id)
			if !errors.Is(gotErr, wantErr) || (gotErr == nil) != (wantErr == nil) {
				t.Fatalf("error = %v, want %v", gotErr, wantErr)
			}
			gotJSON, err := model.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			wantJSON, err := model.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotJSON) != string(wantJSON) || (got == nil) != (want == nil) {
				t.Fatalf("got %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

// TestReadHAPairDoesNotCopyOtherAgents guards the O10 idle fix: rule-sync
// workers resolve their HA leader every run; copying the whole registry made
// that cost grow with the number of unrelated Agents.
func TestReadHAPairDoesNotCopyOtherAgents(t *testing.T) {
	small, large := haPairManager(t, 3), haPairManager(t, 60)
	// Allocation measurements exclude the test-only view guard's encodings.
	small.Repository().SetViewGuard(nil)
	large.Repository().SetViewGuard(nil)
	ctx := context.Background()
	measure := func(fn func() error) int64 {
		if err := fn(); err != nil {
			t.Fatal(err)
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for range 5 {
			if err := fn(); err != nil {
				t.Fatal(err)
			}
		}
		runtime.ReadMemStats(&after)
		return int64(after.TotalAlloc-before.TotalAlloc) / 5
	}
	pair := func(m *Manager) func() error {
		return func() error { _, err := m.ReadHAPair(ctx, "p1"); return err }
	}
	if s, l := measure(pair(small)), measure(pair(large)); l > s+32*1024 {
		t.Fatalf("ReadHAPair allocation grew with unrelated Agents: %d B -> %d B", s, l)
	}
	// Negative control: deep-copying the full listing grows with the same Agents.
	full := func(m *Manager) func() error {
		return func() error {
			records, err := m.ReadAgents(ctx, false, nil)
			for i := range records {
				if err == nil {
					_, err = CloneAgent(&records[i])
				}
			}
			return err
		}
	}
	if s, l := measure(full(small)), measure(full(large)); l < s+256*1024 {
		t.Fatalf("control: full listing did not grow with Agents: %d B -> %d B", s, l)
	}
}
