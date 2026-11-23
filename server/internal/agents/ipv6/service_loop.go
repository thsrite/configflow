package ipv6

import (
	"context"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/deploy/contentsync"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

// NewService constructs an IPv6 verification Service with its own waker
// watching configuration and Agent state commits on m.
func NewService(m *agents.Manager, checker func(*agents.Manager) *contentsync.Checker) *Service {
	w := agents.NewWaker(5*time.Second, agents.IdleFallback)
	if m != nil {
		w.Watch(m, repo.ChangeConfig|repo.ChangeState, false)
	}
	return &Service{
		Manager: m,
		Checker: checker,
		waker:   w,
	}
}

// Loop returns the periodic ipv6-verification-scheduler loop owned by s.
func (s *Service) Loop() lifecycle.Periodic {
	w := s.waker
	if w == nil {
		w = schedulerLoop
	}
	return lifecycle.Periodic{
		Name:       "ipv6-verification-scheduler",
		FirstDelay: 0,
		Interval:   w.Interval,
		Wake:       w.Signal(),
		Round: func(ctx context.Context) error {
			idle, err := s.Tick(ctx)
			w.Done(err != nil || !idle)
			return err
		},
	}
}

// EnrollSource adapts s.Enroll to deploy.SetIPv6Enroll.
func (s *Service) EnrollSource(ctx context.Context, _ *agents.Manager, id, did string, src *deploy.Source) error {
	b, err := src.Raw()
	if err != nil {
		return err
	}
	return s.Enroll(ctx, id, did, object(b))
}
