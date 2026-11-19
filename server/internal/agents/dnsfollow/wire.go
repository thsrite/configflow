package dnsfollow

import (
	"context"
	"sync"
	"time"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/tasks"
)

var services = struct {
	sync.Mutex
	byManager map[*agents.Manager]*Service
}{byManager: map[*agents.Manager]*Service{}}

// ForManager returns the shared owner for this manager's request and loop paths.
func ForManager(m *agents.Manager) *Service {
	services.Lock()
	defer services.Unlock()
	if s := services.byManager[m]; s != nil {
		return s
	}
	s := New(m, Options{})
	if !m.OnClose(func() { services.Lock(); delete(services.byManager, m); services.Unlock() }) {
		return s
	}
	services.byManager[m] = s
	return s
}

// Wire initializes the DNS-follow owner when the registry manager is replaced.
func Wire(m *agents.Manager) { ForManager(m) }

// Register installs the boot loop and C07 row/runner without synthesizing the
// missing rule-sync half of the Agent task slot.
func Register() {
	agents.OnManagerInit(func(_ context.Context, m *agents.Manager) { Wire(m) })
	agents.SetDNSFollowRow(agentTaskRow)
	tasks.SetRunner("dns_follow", func(ctx context.Context, _, id string) error {
		m, e := agents.Get(ctx)
		if e != nil {
			return e
		}
		s := ForManager(m)
		_, e = s.Retry(ctx, id)
		if IsUserError(e) {
			return &tasks.Invalid{Message: s.FailureMessage(ctx, e)}
		}
		return e
	})
	lifecycle.RegisterPeriodic(lifecycle.Periodic{Name: "dns-follow", Start: lifecycle.AtBoot, Interval: func(context.Context) time.Duration { return PollInterval }, Round: func(ctx context.Context) error {
		m, e := agents.Get(ctx)
		if e == nil {
			e = ForManager(m).Tick(ctx)
		}
		if e != nil && ctx.Err() == nil {
			logger.Error(msgLoopFailed)
		}
		return e
	}})
}
