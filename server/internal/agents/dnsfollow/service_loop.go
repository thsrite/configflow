package dnsfollow

import (
	"context"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/tasks"
)

// Loop returns the periodic dns-follow reconciliation loop owned by s.
func (s *Service) Loop() lifecycle.Periodic {
	return lifecycle.Periodic{
		Name:     "dns-follow",
		Start:    lifecycle.AtBoot,
		Interval: s.waker.Interval,
		Wake:     s.waker.Signal(),
		Round: func(ctx context.Context) error {
			err := s.Tick(ctx)
			if err != nil && ctx.Err() == nil {
				logger.Error(msgLoopFailed)
			}
			return err
		},
	}
}

// Wake ends the current pause of s's reconciliation loop.
func (s *Service) Wake() { s.waker.Wake() }

// Runner returns the tasks.RunFunc that retries a failed follow binding via s.
func (s *Service) Runner() tasks.RunFunc {
	return func(ctx context.Context, _, id string) error {
		_, err := s.Retry(ctx, id)
		if IsUserError(err) {
			return &tasks.Invalid{Message: s.FailureMessage(ctx, err)}
		}
		return err
	}
}

// RegisterInstance binds an externally constructed Service to m so transitional
// ForManager(m) lookups share the same instance until W10 removes ForManager.
func RegisterInstance(m *agents.Manager, s *Service) {
	if m == nil || s == nil {
		return
	}
	services.Lock()
	defer services.Unlock()
	if m.OnClose(func() {
		services.Lock()
		if services.byManager[m] == s {
			delete(services.byManager, m)
		}
		services.Unlock()
	}) {
		services.byManager[m] = s
	}
}
