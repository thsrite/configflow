package rulesync

import (
	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"context"
	"sync"
)

var activeService struct {
	sync.Mutex
	service *Service
}

// Get returns the reconciliation owner of the current process manager.
func Get(ctx context.Context) (*Service, error) {
	m, e := agents.Get(ctx)
	if e != nil {
		return nil, e
	}
	activeService.Lock()
	defer activeService.Unlock()
	if activeService.service == nil || activeService.service.m != m {
		activeService.service = New(m, nil, Options{})
		loop.Watch(m, repo.ChangeConfig|repo.ChangeState, true)
	}
	return activeService.service, nil
}

// Wire supplies the real C07 row, C32 withheld projection and request-activated
// loop. It does not fill A17's independent row or task runner.
// loop wakes the rule-sync loop on configuration and Agent state commits, an
// Agent coming online and finished rule-source refreshes (WakeLoop); while a
// worker runs or an Agent waits (pending debounce, running, retry) it polls
// every PollInterval (design/o17-runtime-state.md 2.6). Rule contents an
// Agent downloads itself are not notified: the idle fallback is short.
var loop = agents.NewWaker(PollInterval, IdleFallback)

// WakeLoop wakes the rule-sync loop, for example after rule sources were
// refreshed and the shared rule files may have changed.
func WakeLoop() { loop.Wake() }

var wakeOnce sync.Once

// Wire registers rule sync on the default lifecycle runtime.
func Wire() { WireOn(lifecycle.Current()) }

// WireOn registers rule sync on rt.
func WireOn(rt *lifecycle.Runtime) {
	if rt == nil {
		rt = lifecycle.Current()
	}
	wakeOnce.Do(func() { refresh.OnRulesRefreshed(WakeLoop) })
	rt.RegisterPeriodic(lifecycle.Periodic{Name: "agent-rule-sync", Start: lifecycle.AfterFirstRequest, Interval: loop.Interval, Wake: loop.Signal(), Round: func(ctx context.Context) error {
		s, e := Get(ctx)
		if e != nil {
			return e
		}
		return s.Round(ctx)
	}})
}
