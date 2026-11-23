package substore

import (
	"context"
	"sync"

	"configflow/internal/convert"
	"configflow/internal/lifecycle"
)

// Service bundles the module-level singletons of sub_store_client,
// sub_store_runtime and dependencies (one environment, one runtime, one
// client, one dependency manager).
type Service struct {
	Env          *Env
	Runtime      *Runtime
	Client       *Client
	Dependencies *Dependencies
}

// NewService builds the parts on env with the default runtime root and
// idle time.
func NewService(env *Env) *Service {
	runtime := NewRuntime(env, "", IdleSeconds)
	client := NewClient(env, runtime)
	return &Service{Env: env, Runtime: runtime, Client: client, Dependencies: NewDependencies(env, runtime, client)}
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

// Wire registers the startup work of backend/app.py::initialize_app that
// belongs to this module: the Sub-Store idle monitor (only with the bundled
// Sub-Store) and the one-time removal of node caches of deleted
// subscriptions.
func Wire() {
	service := Default()
	if service.Env.IsBuiltinAvailable() {
		service.Runtime.warnTempDirectory()
		lifecycle.RegisterPeriodic(service.Runtime.idlePeriodic())
	}
}

// RegisterStartup registers cache pruning, allowing main to order it between
// configuration preparation and Agent initialization before wiring the app.
func RegisterStartup() {
	lifecycle.RegisterStartup(lifecycle.Startup{Name: "subscription-cache-prune", Run: func(ctx context.Context) error {
		// Caches of deleted subscriptions hold node credentials.
		_, err := PruneOrphanCaches(ctx)
		return err
	}})
}
