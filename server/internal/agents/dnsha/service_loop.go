package dnsha

import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/lifecycle"
)

// Loop returns the periodic oxidns-ha-publish reconciliation loop owned by s.
func (s *Service) Loop() lifecycle.Periodic {
	return lifecycle.Periodic{
		Name:     "oxidns-ha-publish",
		Start:    lifecycle.AtBoot,
		Interval: s.waker.Interval,
		Wake:     s.waker.Signal(),
		Round:    s.Reconcile,
	}
}

// Wake ends the current pause of s's reconciliation loop.
func (s *Service) Wake() { s.waker.Wake() }

// PairRulesJSON adapts s.PairRules to monitor.HARulesJSONFunc.
func (s *Service) PairRulesJSON(ctx context.Context, _ monitor.Registry, id, method string) (json.RawMessage, int, error) {
	r, code, err := s.PairRules(ctx, id, method)
	if IsUserError(err) {
		return nil, 0, &monitor.HABusyError{Message: s.FailureMessage(ctx, err)}
	}
	if err != nil {
		return nil, code, err
	}
	raw, err := json.Marshal(r)
	return raw, code, err
}

// PairFailureMessage translates an HA refusal into the sanitized user message
// shown by automatic rule sync.
func (s *Service) PairFailureMessage(ctx context.Context, cause error) (string, bool) {
	var refusal *Error
	if !errors.As(cause, &refusal) {
		return "", false
	}
	return s.FailureMessage(ctx, refusal), true
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
