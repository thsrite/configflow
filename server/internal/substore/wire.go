package substore

import (
	"context"
	"sync"

	"configflow/internal/convert"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
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

// Converter is the Sub-Store node converter for convert.ConvertNodeToMihomo
// and convert.RejectSurgeDialers (sub_store_client.convert_proxy_string).
func Converter() convert.ProxyStringConverter { return Default().Client }

var appLog = logx.Get("backend.app")

// Wire registers the startup work of backend/app.py::initialize_app that
// belongs to this module: the Sub-Store idle monitor (only with the bundled
// Sub-Store) and the one-time removal of node caches of deleted
// subscriptions.
func Wire() {
	lifecycle.Loop("sub-store-idle", func(ctx context.Context) {
		service := Default()
		if !service.Env.IsBuiltinAvailable() {
			return
		}
		service.Runtime.Monitor(ctx)
	})
	lifecycle.Loop("subscription-cache-prune", func(ctx context.Context) {
		// Caches of deleted subscriptions hold node credentials.
		if _, err := PruneOrphanCaches(ctx); err != nil {
			appLog.Warning("清理订阅缓存失败: " + err.Error())
		}
	})
}
