package rulesync

import (
	"context"
	"time"

	"configflow/internal/lifecycle"
)

// Loop returns the periodic agent-rule-sync loop owned by s.
func (s *Service) Loop() lifecycle.Periodic {
	return lifecycle.Periodic{
		Name:     "agent-rule-sync",
		Start:    lifecycle.AfterFirstRequest,
		Interval: s.interval,
		Wake:     s.waker.Signal(),
		Round:    s.Round,
	}
}

func (s *Service) interval(ctx context.Context) time.Duration {
	pause := s.waker.Interval(ctx)
	s.mu.Lock()
	retry := s.retryAt
	s.mu.Unlock()
	if !retry.IsZero() {
		// The worker dispatched by Round handles an already-due retry.
		pause = min(pause, max(time.Nanosecond, retry.Sub(s.now())))
	}
	return pause
}

// WakeLoop wakes s's rule-sync loop, for example after rule sources were
// refreshed and shared rule files may have changed.
func (s *Service) WakeLoop() {
	if s.waker != nil {
		s.waker.Wake()
	}
}

func (s *Service) advancePair(ctx context.Context, id string) error {
	if s.pair.Advance == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.pair.Advance(ctx, id)
	return ctx.Err()
}

func (s *Service) pairFailure(ctx context.Context, cause error) (string, bool) {
	if s.pair.FailureMessage == nil {
		return "", false
	}
	return s.pair.FailureMessage(ctx, cause)
}
