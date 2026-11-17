package agents

import (
	"context"
	"sync"

	core "configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/deploy/contentsync"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/substore"
)

type contentPool struct {
	mu         sync.Mutex
	checkers   map[*core.Manager]*contentsync.Checker
	dispatcher contentsync.Dispatcher
}

var contentChecks = contentPool{checkers: map[*core.Manager]*contentsync.Checker{}}
var contentPrepare struct {
	sync.RWMutex
	fn contentsync.PrepareFunc
}

// SetContentPrepare installs A14's preparation in the composition layer. The
// returned restore function is intended for isolated tests. Existing results
// are invalidated so jobs using an earlier implementation cannot publish them.
func SetContentPrepare(fn contentsync.PrepareFunc) func() {
	contentPrepare.Lock()
	old := contentPrepare.fn
	contentPrepare.fn = fn
	contentPrepare.Unlock()
	contentChecks.dispatcher.Invalidate()
	return func() {
		contentPrepare.Lock()
		contentPrepare.fn = old
		contentPrepare.Unlock()
		contentChecks.dispatcher.Invalidate()
	}
}
func prepareContent(ctx context.Context, t deploy.Target, s *deploy.Source, url, id string) (*deploy.Bundle, error) {
	contentPrepare.RLock()
	fn := contentPrepare.fn
	contentPrepare.RUnlock()
	if fn == nil {
		return nil, contentsync.ErrPreparationUnavailable
	}
	return fn(ctx, t, s, url, id)
}

func (p *contentPool) checker(m *core.Manager) *contentsync.Checker {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.checkers[m]; c != nil {
		return c
	}
	receipts := deploy.NewReceipts(m.Repository().DataDir())
	c := contentsync.New(contentsync.Options{Prepare: func(ctx context.Context, t deploy.Target, s *deploy.Source, url, id string) (*deploy.Bundle, error) {
		return prepareContent(repo.WithRepository(ctx, m.Repository()), t, s, url, id)
	}, LoadReceipt: func(ctx context.Context, t deploy.Target) (*deploy.Receipt, error) {
		return receipts.Load(ctx, t.Record, true)
	}, DefaultProfileID: m.Repository().DefaultProfileID})
	unregister := p.dispatcher.Register(c)
	if !m.OnClose(func() {
		p.mu.Lock()
		if p.checkers[m] == c {
			delete(p.checkers, m)
		}
		p.mu.Unlock()
		unregister()
		c.Close()
	}) {
		unregister()
		c.Close()
		return c
	}
	p.checkers[m] = c
	return c
}
func (p *contentPool) close() {
	p.mu.Lock()
	managers := make([]*core.Manager, 0, len(p.checkers))
	for m := range p.checkers {
		managers = append(managers, m)
	}
	p.mu.Unlock()
	for _, m := range managers {
		m.Close()
	}
}

// WireContentSync installs Sub-Store invalidation and Manager-owned cleanup.
// A14 injects preparation through SetContentPrepare, avoiding a deploy cycle.
func WireContentSync() {
	substore.SetContentSyncInvalidator(func(context.Context) error { contentChecks.dispatcher.Invalidate(); return nil })
	lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "agent-content-sync-close", Phase: lifecycle.PhaseClose, Run: func(context.Context) { contentChecks.close() }})
}
