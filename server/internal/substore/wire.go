package substore

import (
	"context"
	"sync"

	"configflow/internal/convert"
	"configflow/internal/lifecycle"
)

type contentChangeSub struct {
	id uint64
	fn ContentSyncInvalidator
}

// Service bundles the module-level singletons of sub_store_client,
// sub_store_runtime and dependencies (one environment, one runtime, one
// client, one dependency manager).
type Service struct {
	Env          *Env
	Runtime      *Runtime
	Client       *Client
	Dependencies *Dependencies

	changeMu   sync.Mutex
	nextSubID  uint64
	changeSubs []contentChangeSub
}

// NewService builds the parts on env with the default runtime root and
// idle time.
func NewService(env *Env) *Service {
	runtime := NewRuntime(env, "", IdleSeconds)
	client := NewClient(env, runtime)
	return &Service{Env: env, Runtime: runtime, Client: client, Dependencies: NewDependencies(env, runtime, client)}
}

// OnContentChange subscribes fn to subscription cache host-change events on
// this Service instance and returns an unsubscribe function.
func (s *Service) OnContentChange(fn ContentSyncInvalidator) func() {
	if s == nil || fn == nil {
		return func() {}
	}
	s.changeMu.Lock()
	s.nextSubID++
	id := s.nextSubID
	s.changeSubs = append(s.changeSubs, contentChangeSub{id: id, fn: fn})
	s.changeMu.Unlock()
	return func() {
		s.changeMu.Lock()
		for i, sub := range s.changeSubs {
			if sub.id == id {
				s.changeSubs = append(s.changeSubs[:i], s.changeSubs[i+1:]...)
				break
			}
		}
		s.changeMu.Unlock()
	}
}

// NotifyContentChange invokes all subscribed content-change invalidators on s.
func (s *Service) NotifyContentChange(ctx context.Context) {
	if s == nil {
		return
	}
	s.changeMu.Lock()
	subs := append([]contentChangeSub(nil), s.changeSubs...)
	s.changeMu.Unlock()
	for _, sub := range subs {
		invokeContentInvalidator(ctx, sub.fn)
	}
}

// Register installs the Sub-Store idle monitor on rt when the built-in runtime
// is available.
func (s *Service) Register(rt *lifecycle.Runtime) {
	if s == nil {
		return
	}
	if rt == nil {
		rt = lifecycle.Current()
	}
	if s.Env.IsBuiltinAvailable() {
		s.Runtime.warnTempDirectory()
		rt.RegisterPeriodic(s.Runtime.idlePeriodic())
	}
}

// RegisterStartup installs the orphan subscription cache pruning step on rt.
func (s *Service) RegisterStartup(rt *lifecycle.Runtime) {
	if rt == nil {
		rt = lifecycle.Current()
	}
	rt.RegisterStartup(lifecycle.Startup{Name: "subscription-cache-prune", Run: func(ctx context.Context) error {
		// Caches of deleted subscriptions hold node credentials.
		_, err := PruneOrphanCaches(ctx)
		return err
	}})
}

var (
	defaultMu      sync.Mutex
	defaultService *Service
)

// Default is the process-wide service (created on first use).
func Default() *Service {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultService == nil {
		defaultService = NewService(NewEnv())
	}
	return defaultService
}

// SetDefault replaces the process-wide service (tests) and returns a
// function restoring the previous one.
func SetDefault(s *Service) (restore func()) {
	defaultMu.Lock()
	previous := defaultService
	defaultService = s
	defaultMu.Unlock()
	return func() {
		defaultMu.Lock()
		defaultService = previous
		defaultMu.Unlock()
	}
}

// NativeConverter supplies the shared typed node-conversion collaborator.
func NativeConverter() convert.NodeConverter { return Default().Client }

// Wire registers the Sub-Store idle monitor on the default runtime.
func Wire() { WireOn(lifecycle.Current()) }

// WireOn registers the Sub-Store idle monitor on rt.
func WireOn(rt *lifecycle.Runtime) { Default().Register(rt) }

// RegisterStartup registers cache pruning on the default runtime.
func RegisterStartup() { RegisterStartupOn(lifecycle.Current()) }

// RegisterStartupOn registers cache pruning on rt.
func RegisterStartupOn(rt *lifecycle.Runtime) { Default().RegisterStartup(rt) }
