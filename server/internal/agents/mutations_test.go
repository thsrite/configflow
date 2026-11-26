package agents

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/lifecycle"
)

func TestTypedAgentMutationsDetachButRespectShutdown(t *testing.T) {
	e, id := setupRuntime(t)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(e.ctx)
	defer rt.Shutdown(context.Background())
	request, disconnect := context.WithCancel(rt.BindRequest(e.ctx))
	disconnect()
	edits, err := DecodeAgentEdits([]byte(`{"name":"disconnected"}`))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := e.m.EditAgent(request, id, edits); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if a, err := e.m.SetDomainDiscoveryModel(request, id, true); err != nil || a == nil || a.DomainDiscoveryEnabled == nil || !*a.DomainDiscoveryEnabled {
		t.Fatal(a, err)
	}
	if a, err := e.m.SetTrafficHistoryModel(request, id, true); err != nil || a == nil || a.TrafficHistorySince == nil {
		t.Fatal(a, err)
	}
	bound := rt.BindRequest(e.ctx)
	rt.Shutdown(context.Background())
	head, _ := e.repo.Head(e.ctx)
	if ok, err := e.m.EditAgent(bound, id, edits); ok || !errors.Is(err, context.Canceled) {
		t.Fatal(ok, err)
	}
	if a, err := e.m.SetDomainDiscoveryModel(bound, id, false); a != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(a, err)
	}
	if a, err := e.m.SetTrafficHistoryModel(bound, id, false); a != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(a, err)
	}
	if ok, err := e.m.DeleteAgent(bound, id); ok || !errors.Is(err, context.Canceled) {
		t.Fatal(ok, err)
	}
	if after, _ := e.repo.Head(e.ctx); after != head {
		t.Fatal("shutdown wrote")
	}
}
