package refresh

import (
	"context"
	"errors"
	"sync"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/weburl"
)

// Options configures a scheduled-refresh Service instance.
type Options struct {
	Repo          *repo.Repository
	SubStore      *substore.Service
	Runtime       *lifecycle.Runtime
	Tasks         *tasks.Registry
	Notifications *notifications.Service
	Env           *RefreshEnv
}

// Service owns the scheduled-refresh loop pacer, collaborator environment, and
// instance-level rule-refresh event subscribers.
type Service struct {
	repo          *repo.Repository
	substore      *substore.Service
	runtime       *lifecycle.Runtime
	tasks         *tasks.Registry
	notifications *notifications.Service

	envMu sync.RWMutex
	env   *RefreshEnv

	healthFile repo.SharedFileView[*HealthSnapshot]
	loop       refreshPacer
	ruleRowsMu sync.Mutex
	ruleRows   *ruleRows

	hooksMu          sync.Mutex
	onRulesRefreshed []func()
}

// New constructs a refresh Service with explicit dependencies.
func New(opts Options) *Service {
	return &Service{
		repo:          opts.Repo,
		substore:      opts.SubStore,
		runtime:       opts.Runtime,
		tasks:         opts.Tasks,
		notifications: opts.Notifications,
		env:           opts.Env,
	}
}

// Repository returns the repository s was constructed with (nil when none).
func (s *Service) Repository() *repo.Repository {
	if s == nil {
		return nil
	}
	return s.repo
}

func (s *Service) repository() (*repo.Repository, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("refresh: repository is nil")
	}
	return s.repo, nil
}

// OnRulesRefreshed registers fn to run after a rule source refresh completes.
// fn must not block.
func (s *Service) OnRulesRefreshed(fn func()) {
	if s == nil || fn == nil {
		return
	}
	s.hooksMu.Lock()
	s.onRulesRefreshed = append(s.onRulesRefreshed, fn)
	s.hooksMu.Unlock()
}

// NotifyRulesRefreshed invokes all subscribers registered on s.
func (s *Service) NotifyRulesRefreshed() {
	if s == nil {
		return
	}
	s.hooksMu.Lock()
	hooks := append([]func(){}, s.onRulesRefreshed...)
	s.hooksMu.Unlock()
	for _, fn := range hooks {
		fn()
	}
}

// Wake notifies the scheduled-refresh pacer to recompute its next due pause.
func (s *Service) Wake() {
	if s == nil {
		return
	}
	s.loop.wake.Notify()
}

// SetEnv replaces the collaborator environment on s and returns a restore func.
func (s *Service) SetEnv(value *RefreshEnv) func() {
	s.envMu.Lock()
	old := s.env
	s.env = value
	s.envMu.Unlock()
	return func() {
		s.envMu.Lock()
		s.env = old
		s.envMu.Unlock()
	}
}

func (s *Service) currentEnv() *RefreshEnv {
	s.envMu.RLock()
	value := s.env
	s.envMu.RUnlock()
	if value != nil {
		return value
	}
	return DefaultRefreshEnvFor(s.repo, s.substore)
}

func (s *Service) pause(context.Context) time.Duration {
	if pause := time.Duration(s.loop.pause.Load()); pause > 0 {
		return pause
	}
	return TickSeconds * time.Second
}

func (s *Service) round(ctx context.Context) {
	scope := repo.WithScope(ctx)
	defer repo.ResetConfigContext(scope)
	s.loop.pause.Store(int64(TickSeconds * time.Second))
	defer func() {
		if recover() != nil {
			logger.Errorf(msgRoundPanic)
		}
	}()
	if _, err := s.RunRound(scope, nil); err != nil && !errors.Is(err, context.Canceled) {
		logger.Errorf(msgRoundFailed, weburl.SafeErrorDetails(err))
		return
	}
	s.pace(scope)
}

// Periodic returns the lifecycle.Periodic descriptor bound to s's pacer.
func (s *Service) Periodic() lifecycle.Periodic {
	return lifecycle.Periodic{
		Name:       "scheduled-refresh",
		Start:      lifecycle.AtBoot,
		FirstDelay: FirstDelaySeconds * time.Second,
		Interval:   s.pause,
		Wake:       &s.loop.wake,
		// A wake-up left from before this activation must not cut its first pauses short.
		OnStart: func(context.Context) { s.loop.wake.Clear(); s.loop.pause.Store(0) },
		Round:   func(ctx context.Context) error { s.round(ctx); return nil },
	}
}

// Register installs subscription/rule-source task builders and runners on tk
// and the scheduled-refresh periodic loop on rt.
func (s *Service) Register(rt *lifecycle.Runtime, tk *tasks.Registry) {
	if rt == nil {
		rt = s.runtime
	}
	if tk == nil {
		tk = s.tasks
	}
	if tk != nil {
		tk.SetRowBuilder(tasks.SlotSubscriptions, s.SubscriptionTasks)
		tk.SetRowBuilder(tasks.SlotRuleSources, s.RuleTasks)
		tk.SetRunner("subscription", s.runTask)
		tk.SetRunner("rule_source", s.runTask)
	}
	if rt != nil {
		rt.RegisterPeriodic(s.Periodic())
	}
}
