package substore

import (
	"context"
	"sync"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

type contentChangeSub struct {
	id uint64
	fn ContentSyncInvalidator
}

// Service bundles the Sub-Store environment, runtime, client, and dependency
// manager for one application instance.
type Service struct {
	Repo         *repo.Repository
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
	var r *repo.Repository
	if env != nil {
		r = env.Repo
	}
	return &Service{Repo: r, Env: env, Runtime: runtime, Client: client, Dependencies: NewDependencies(env, runtime, client)}
}

// SaveCache persists typed nodes and metadata and notifies subscribers on s
// when subscription hosts changed.
func (s *Service) SaveCache(ctx context.Context, r *repo.Repository, id string, nodes []model.Node, metadata *CacheMetadata) (*Cache, error) {
	if r == nil && s != nil {
		r = s.Repo
	}
	count := model.Int(len(nodes))
	var notify func(context.Context)
	if s != nil {
		notify = s.NotifyContentChange
	}
	return saveCache(ctx, r, id, &Cache{Count: &count, Nodes: nodes, Metadata: metadata}, notify)
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
	if s == nil || rt == nil {
		return
	}
	if s.Env.IsBuiltinAvailable() {
		s.Runtime.warnTempDirectory()
		rt.RegisterPeriodic(s.Runtime.idlePeriodic())
	}
}

// RegisterStartup installs the orphan subscription cache pruning step on rt.
func (s *Service) RegisterStartup(rt *lifecycle.Runtime) {
	if rt == nil {
		return
	}
	rt.RegisterStartup(lifecycle.Startup{Name: "subscription-cache-prune", Run: func(ctx context.Context) error {
		// Caches of deleted subscriptions hold node credentials.
		if s == nil || s.Repo == nil {
			return nil
		}
		_, err := PruneOrphanCaches(ctx, s.Repo)
		return err
	}})
}
