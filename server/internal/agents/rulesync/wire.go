package rulesync

import (
	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"context"
	"sync"
	"time"
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
	}
	return activeService.service, nil
}

// Wire supplies the real C07 row, C32 withheld projection and request-activated
// loop. It does not fill A17's independent row or task runner.
func Wire() {
	agents.SetRuleSyncRow(agentTaskRow)
	agents.RegisterHooks(agents.Hooks{PinnedWithheld: func(ctx context.Context, profileID string, ruleIDs []string) (bool, error) {
		r, e := repo.GetRepository(ctx)
		if e != nil {
			return false, e
		}
		return PinnedWithheld(ctx, r, deploy.NewReceipts(r.DataDir()), profileID, ruleIDs)
	}})
	lifecycle.RegisterPeriodic(lifecycle.Periodic{Name: "agent-rule-sync", Start: lifecycle.AfterFirstRequest, Interval: func(context.Context) time.Duration { return PollInterval }, Round: func(ctx context.Context) error {
		s, e := Get(ctx)
		if e != nil {
			return e
		}
		return s.Round(ctx)
	}})
}
