package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

type stubPairPublisher struct {
	deployCalls   int
	activateCalls int
	opCalls       int
}

func (s *stubPairPublisher) PairDeployment(_ context.Context, _, _ string) (*model.Deployment, error) {
	s.deployCalls++
	return &model.Deployment{DeploymentID: "d1", Status: "succeeded"}, nil
}

func (s *stubPairPublisher) ActivatePair(_ context.Context, _, _ string) (*model.Deployment, error) {
	s.activateCalls++
	return &model.Deployment{DeploymentID: "d1", Status: "succeeded"}, nil
}

func (s *stubPairPublisher) ServiceOperationJSON(_ context.Context, _ string, _ func() (json.RawMessage, error), _ bool) (json.RawMessage, error) {
	s.opCalls++
	return json.RawMessage(`{"success":true}`), nil
}

type stubMetricsSink struct {
	samples        int
	deletedMetrics int
	deletedTraffic int
}

func (s *stubMetricsSink) AddMetricsJSON(_ context.Context, _ *Manager, _ string, _ json.RawMessage) error {
	s.samples++
	return nil
}

func (s *stubMetricsSink) DeleteMetricsHistory(_ context.Context, _ *Manager, _ string) error {
	s.deletedMetrics++
	return nil
}

func (s *stubMetricsSink) DeleteTrafficHistory(_ context.Context, _ *Manager, _ string) error {
	s.deletedTraffic++
	return nil
}

func TestManagerPairPublisherFailClosedAndTwoPhase(t *testing.T) {

	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	ctx := context.Background()

	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		return s.SetJSON("agents", json.RawMessage(`[{
			"id":"dns-a","name":"DNS A","service_type":"oxidns","profile_id":"default",
			"host":"192.0.2.10","port":8080,"token":"synthetic-token",
			"oxidns_ha_publication":{"id":"pub-1","status":"running","children":{"dns-a":"pub-1"}}
		}]`))
	}); err != nil {
		t.Fatal(err)
	}

	// Negative control: nil PairPublisher and empty global hooks MUST fail closed.
	m, err := New(ctx, r, Options{RuntimeState: NewRuntimeState()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	if _, err := m.GetDeploymentModel(ctx, "dns-a", "pub-1"); !errors.Is(err, ErrHAOwnerMissing) {
		t.Fatalf("GetDeploymentModel without PairPublisher = %v, want ErrHAOwnerMissing", err)
	}
	if _, err := m.ActivateDeploymentModel(ctx, "dns-a", "pub-1"); !errors.Is(err, ErrHAOwnerMissing) {
		t.Fatalf("ActivateDeploymentModel without PairPublisher = %v, want ErrHAOwnerMissing", err)
	}
	raw, err := m.RunServiceOperation(ctx, "dns-a", RestartService, nil, nil)
	if err != nil {
		t.Fatalf("RunServiceOperation without PairPublisher unexpected error: %v", err)
	}
	if !strings.Contains(string(raw), `"http_status":409`) {
		t.Fatalf("RunServiceOperation without PairPublisher should fail closed with 409, got %s", raw)
	}

	// D1 two-phase setter: first SetPairPublisher succeeds, second fails.
	stub := &stubPairPublisher{}
	if err := m.SetPairPublisher(stub); err != nil {
		t.Fatalf("first SetPairPublisher: %v", err)
	}
	if err := m.SetPairPublisher(stub); !errors.Is(err, ErrPairPublisherAlreadySet) {
		t.Fatalf("second SetPairPublisher = %v, want ErrPairPublisherAlreadySet", err)
	}
	if _, err := m.GetDeploymentModel(ctx, "dns-a", "pub-1"); err != nil || stub.deployCalls != 1 {
		t.Fatalf("GetDeploymentModel with PairPublisher: err=%v calls=%d", err, stub.deployCalls)
	}
	if _, err := m.ActivateDeploymentModel(ctx, "dns-a", "pub-1"); err != nil || stub.activateCalls != 1 {
		t.Fatalf("ActivateDeploymentModel with PairPublisher: err=%v calls=%d", err, stub.activateCalls)
	}
	if _, err := m.RunServiceOperation(ctx, "dns-a", RestartService, nil, nil); err != nil || stub.opCalls != 1 {
		t.Fatalf("RunServiceOperation with PairPublisher: err=%v calls=%d", err, stub.opCalls)
	}

	// SetPairPublisher after MarkStarted fails.
	m2, err := New(ctx, r, Options{RuntimeState: NewRuntimeState()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m2.Close)
	m2.MarkStarted()
	if err := m2.SetPairPublisher(stub); !errors.Is(err, ErrManagerStarted) {
		t.Fatalf("SetPairPublisher after MarkStarted = %v, want ErrManagerStarted", err)
	}
}

func TestManagerReceiptMatchesFailClosedWhenNil(t *testing.T) {

	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	m, err := New(context.Background(), r, Options{RuntimeState: NewRuntimeState()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	ok, err := m.candidateReceiptMatches(&model.Agent{ID: "a"}, &model.Deployment{DeploymentID: "d"})
	if err != nil || ok {
		t.Fatalf("candidateReceiptMatches with nil ReceiptMatches = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestManagerOptionsMetricsAndDeletedStores(t *testing.T) {

	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	ctx := context.Background()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		return s.SetJSON("agents", json.RawMessage(`[{"id":"a1","name":"A1","service_type":"mihomo","profile_id":"default","host":"192.0.2.10","port":8080,"token":"synthetic-token"}]`))
	}); err != nil {
		t.Fatal(err)
	}

	sink := &stubMetricsSink{}
	stores := []NamedDeleter{
		{Name: "traffic_store", Delete: func(context.Context, string) error { return nil }},
		{Name: "domain_probe", Delete: func(context.Context, string) error { return nil }},
	}
	m, err := New(ctx, r, Options{Metrics: sink, DeletedStores: stores, RuntimeState: NewRuntimeState()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	if err := m.addReportedMetrics(ctx, "a1", json.RawMessage(`{"cpu":{"usage_percent":5}}`)); err != nil || sink.samples != 1 {
		t.Fatalf("addReportedMetrics: err=%v samples=%d", err, sink.samples)
	}
	if _, err := m.DeleteAgent(ctx, "a1"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	if sink.deletedMetrics != 1 || sink.deletedTraffic != 1 {
		t.Fatalf("DeleteAgent metrics=%d traffic=%d, want 1/1", sink.deletedMetrics, sink.deletedTraffic)
	}
	gotStores := m.DeletedStores()
	if len(gotStores) != 2 || gotStores[0].Name != "traffic_store" || gotStores[1].Name != "domain_probe" {
		t.Fatalf("DeletedStores = %+v", gotStores)
	}
}

// TestSetPairPublisherConcurrentOnce races SetPairPublisher callers with
// MarkStarted and pair readers (run under -race): at most one setter wins,
// later ones get ErrPairPublisherAlreadySet or ErrManagerStarted.
func TestSetPairPublisherConcurrentOnce(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	m, err := New(context.Background(), r, Options{RuntimeState: NewRuntimeState()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	const setters = 8
	results := make(chan error, setters)
	var wg sync.WaitGroup
	for range setters {
		wg.Add(2)
		go func() {
			defer wg.Done()
			results <- m.SetPairPublisher(&stubPairPublisher{})
		}()
		go func() {
			defer wg.Done()
			_ = m.pairPublisher()
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); m.MarkStarted() }()
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrPairPublisherAlreadySet), errors.Is(err, ErrManagerStarted):
		default:
			t.Fatalf("SetPairPublisher = %v", err)
		}
	}
	if wins > 1 {
		t.Fatalf("%d SetPairPublisher calls succeeded, want at most 1", wins)
	}
	if err := m.SetPairPublisher(&stubPairPublisher{}); !errors.Is(err, ErrManagerStarted) {
		t.Fatalf("SetPairPublisher after MarkStarted = %v", err)
	}
}
