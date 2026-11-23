package rulesync

import (
	"context"

	"configflow/internal/lifecycle"
)

// Loop returns the periodic agent-rule-sync loop owned by s.
func (s *Service) Loop() lifecycle.Periodic {
	return lifecycle.Periodic{
		Name:     "agent-rule-sync",
		Start:    lifecycle.AfterFirstRequest,
		Interval: s.waker.Interval,
		Wake:     s.waker.Signal(),
		Round:    s.Round,
	}
}

// WakeLoop wakes s's rule-sync loop, for example after rule sources were
// refreshed and shared rule files may have changed.
func (s *Service) WakeLoop() {
	if s.waker != nil {
		s.waker.Wake()
	}
}

func (s *Service) advancePair(ctx context.Context, id string) error {
	if s.pair.Advance != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.pair.Advance(ctx, id)
		return ctx.Err()
	}
	return advancePair(ctx, s.m, id)
}

func (s *Service) pairFailure(ctx context.Context, cause error) (string, bool) {
	if s.pair.FailureMessage != nil {
		return s.pair.FailureMessage(ctx, cause)
	}
	return pairFailureMessage(ctx, s.m, cause)
}

// UseService installs an externally constructed Service as the active owner
// returned by Get(ctx) until W10 removes package-level lookups.
func UseService(s *Service) {
	activeService.Lock()
	activeService.service = s
	activeService.Unlock()
}
