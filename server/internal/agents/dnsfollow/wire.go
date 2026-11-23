package dnsfollow

import (
	"context"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
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
	loop.Watch(m, repo.ChangeConfig|repo.ChangeState, true)
	return s
}

// Wire initializes the DNS-follow owner when the registry manager is replaced.
func Wire(m *agents.Manager) { ForManager(m) }

// Register installs the boot loop and C07 row/runner on the default runtime.
func Register() { RegisterOn(lifecycle.Current()) }

// RegisterOn installs the boot loop and C07 row/runner on rt.
func RegisterOn(rt *lifecycle.Runtime) {
	if rt == nil {
		rt = lifecycle.Current()
	}
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
	rt.RegisterPeriodic(lifecycle.Periodic{Name: "dns-follow", Start: lifecycle.AtBoot, Interval: loop.Interval, Wake: loop.Signal(), Round: func(ctx context.Context) error {
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
