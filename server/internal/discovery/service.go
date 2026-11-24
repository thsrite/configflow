package discovery

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/health"
	"configflow/internal/health/check"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/tasks"
)

// Options configures a domain-discovery Service.
type Options struct {
	Repo          *repo.Repository
	Agents        *agents.Manager
	Traffic       *trafficstore.Service
	Notifications *notifications.Service
	Runtime       *lifecycle.Runtime
	Tasks         *tasks.Registry
	Client        AgentClient
}

// Service owns domain-discovery dependencies, matcher cache, job state, and
// background registration.
type Service struct {
	Repo          *repo.Repository
	Agents        *agents.Manager
	Traffic       *trafficstore.Service
	Notifications *notifications.Service
	Runtime       *lifecycle.Runtime
	Tasks         *tasks.Registry

	client AgentClient

	matcherMu    sync.Mutex
	matcherCache *rules.Cache

	jobMu   sync.Mutex
	jobs    []*Job
	jobLock *lockx.Mutex
}

// New constructs a Service with explicit collaborators.
func New(opts Options) *Service {
	client := opts.Client
	if client == nil {
		client = managerClient{agents: opts.Agents}
	}
	return &Service{
		Repo:          opts.Repo,
		Agents:        opts.Agents,
		Traffic:       opts.Traffic,
		Notifications: opts.Notifications,
		Runtime:       opts.Runtime,
		Tasks:         opts.Tasks,
		client:        client,
		jobLock:       lockx.NewMutex(lockx.LevelJob, "domain-discovery"),
	}
}

func (s *Service) repo() (*repo.Repository, error) {
	if s == nil || s.Repo == nil {
		return nil, errors.New("discovery: repository is nil")
	}
	return s.Repo, nil
}

func (s *Service) agents() (*agents.Manager, error) {
	if s == nil || s.Agents == nil {
		return nil, errors.New("discovery: agent manager is nil")
	}
	return s.Agents, nil
}

func (s *Service) trafficStore() (*trafficstore.NativeStore, error) {
	if s != nil && s.Traffic != nil {
		return s.Traffic.Store(), nil
	}
	r, err := s.repo()
	if err != nil {
		return nil, err
	}
	return trafficstore.NewNative(filepath.Join(r.DataDir(), "traffic")), nil
}

func (s *Service) probeStore() (*check.ProbeStore, error) {
	r, err := s.repo()
	if err != nil {
		return nil, err
	}
	return health.ProbeStore(r.DataDir()), nil
}

func (s *Service) regionStore() (*check.RegionStore, error) {
	r, err := s.repo()
	if err != nil {
		return nil, err
	}
	return health.RegionStore(r.DataDir()), nil
}

func (s *Service) agentClient() AgentClient {
	if s != nil && s.client != nil {
		return s.client
	}
	var m *agents.Manager
	if s != nil {
		m = s.Agents
	}
	return managerClient{agents: m}
}

func (s *Service) mutex() *lockx.Mutex {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	if s.jobLock == nil {
		s.jobLock = lockx.NewMutex(lockx.LevelJob, "domain-discovery")
	}
	return s.jobLock
}

func (s *Service) spawner(ctx context.Context) lockx.Spawner {
	if sp := lifecycle.SpawnerFrom(ctx); sp != nil {
		return sp
	}
	if s != nil && s.Runtime != nil {
		return s.Runtime
	}
	return directSpawner{}
}

type directSpawner struct{}

func (directSpawner) Spawn(ctx context.Context, _ string, fn func(context.Context)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	go fn(ctx)
	return nil
}

// Register installs domain-discovery task providers on tk and the automatic
// discovery periodic and cache shutdown hook on rt.
func (s *Service) Register(rt *lifecycle.Runtime, tk *tasks.Registry) {
	if tk != nil {
		s.registerTasks(tk)
	}
	if rt != nil {
		rt.RegisterPeriodic(s.autoPeriodic())
		rt.RegisterShutdown(matcherShutdown(s.clearMatcherCache))
	}
}
