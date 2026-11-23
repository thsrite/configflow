package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

// ErrAgentsAlreadySet rejects a second SetAgents call on a Service.
var ErrAgentsAlreadySet = errors.New("notifications: agents source is already set")

// ErrServiceStarted rejects two-phase setter calls after startup wiring completes.
var ErrServiceStarted = errors.New("notifications: service already started")

// Options configures a notifications Service instance.
type Options struct {
	Repo         *repo.Repository
	Tasks        *tasks.Registry
	Agents       func(context.Context) ([]model.Agent, error)
	Summarize    func(*RouterReport, json.RawMessage) (*RouterSummary, error)
	Rows         func(context.Context) ([]*tasks.Task, error)
	MissingTasks func() []string
	Clock        func() time.Time
}

// Service owns notification source wiring and incident snapshot projection for
// one server instance.
type Service struct {
	mu        sync.Mutex
	repo      *repo.Repository
	tasks     *tasks.Registry
	inputs    Inputs
	agentsSet bool
	started   bool
	clock     func() time.Time
}

// New constructs a notifications Service with explicit dependencies.
func New(opts Options) *Service {
	s := &Service{
		repo:  opts.Repo,
		tasks: opts.Tasks,
		inputs: Inputs{
			Rows:         opts.Rows,
			MissingTasks: opts.MissingTasks,
			Agents:       opts.Agents,
			Summarize:    opts.Summarize,
		},
		agentsSet: opts.Agents != nil,
		clock:     opts.Clock,
	}
	return s
}

var (
	defaultSvcMu   sync.RWMutex
	defaultService = New(Options{})
)

// Default returns the process default notifications Service (transitional helper
// until W9/W11 remove package-level wiring).
func Default() *Service {
	defaultSvcMu.RLock()
	defer defaultSvcMu.RUnlock()
	return defaultService
}

// UseService installs svc as the process default notifications Service so
// packages not yet migrated to explicit injection (internal/routers until W9)
// register onto svc.
func UseService(svc *Service) {
	if svc == nil {
		return
	}
	defaultSvcMu.Lock()
	defaultService = svc
	defaultSvcMu.Unlock()
}

// SetAgents installs the Agent snapshot source in the two-phase construction
// step (O17 D6). It may be called at most once and only before MarkStarted.
func (s *Service) SetAgents(fn func(context.Context) ([]model.Agent, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		logger.Errorf("notifications.SetAgents: %v", ErrServiceStarted)
		return ErrServiceStarted
	}
	if s.agentsSet {
		logger.Errorf("notifications.SetAgents: %v", ErrAgentsAlreadySet)
		return ErrAgentsAlreadySet
	}
	s.inputs.Agents = fn
	s.agentsSet = fn != nil
	return nil
}

// MarkStarted seals two-phase setters on s.
func (s *Service) MarkStarted() {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
}

// SetInputs installs non-nil sources and returns an isolated-test restoration.
func (s *Service) SetInputs(in Inputs) func() {
	s.mu.Lock()
	previous := s.inputs
	prevSet := s.agentsSet
	if in.Rows != nil {
		s.inputs.Rows = in.Rows
	}
	if in.MissingTasks != nil {
		s.inputs.MissingTasks = in.MissingTasks
	}
	if in.Agents != nil {
		s.inputs.Agents = in.Agents
		s.agentsSet = true
	}
	if in.Summarize != nil {
		s.inputs.Summarize = in.Summarize
	}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.inputs = previous
		s.agentsSet = prevSet
		s.mu.Unlock()
	}
}

// ResetInputs clears source wiring.
func (s *Service) ResetInputs() {
	s.mu.Lock()
	s.inputs = Inputs{}
	s.agentsSet = false
	s.mu.Unlock()
}

func (s *Service) currentInputs() Inputs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inputs
}

func (s *Service) taskRegistry() *tasks.Registry {
	if s.tasks != nil {
		return s.tasks
	}
	return tasks.Default()
}

func (s *Service) repository(ctx context.Context) (*repo.Repository, error) {
	if s.repo != nil {
		return s.repo, nil
	}
	return repo.GetRepository(ctx)
}

// Complete reports whether all required notification sources are wired.
func (s *Service) Complete() bool { return s.currentInputs().Agents != nil }

// MissingSources lists required unwired sources.
func (s *Service) MissingSources() []string {
	var missing []string
	if s.currentInputs().Agents == nil {
		missing = append(missing, "agents")
	}
	return missing
}

// Validate checks that all required notification sources and task slots are wired.
func (s *Service) Validate() error {
	if !s.Complete() {
		return ErrSourcesMissing
	}
	return s.taskRegistry().Validate()
}

// SnapshotItems refreshes receipts and returns pending incidents in display order.
func (s *Service) SnapshotItems(ctx context.Context, completedTask string) ([]*Item, error) {
	return s.snapshot(lifecycle.Detach(ctx), completedTask, true, false)
}

// PreviewItems returns the projection without creating or rewriting receipts.
func (s *Service) PreviewItems(ctx context.Context) ([]*Item, error) {
	return s.snapshot(ctx, "", false, false)
}

func (s *Service) snapshot(ctx context.Context, completedTask string, persist, partial bool) ([]*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	in := s.currentInputs()
	if in.Agents == nil {
		return nil, ErrSourcesMissing
	}
	if in.Rows == nil {
		reg := s.taskRegistry()
		in.Rows = reg.Available
		if in.MissingTasks == nil {
			in.MissingTasks = reg.Missing
		}
	}
	retainUnseen := false
	if in.MissingTasks != nil {
		if missing := in.MissingTasks(); len(missing) > 0 {
			if !partial {
				return nil, &tasks.Unavailable{Slots: missing}
			}
			retainUnseen = true
		}
	}
	repository, err := s.repository(ctx)
	if err != nil {
		return nil, err
	}
	var ticket uint64
	if persist {
		ticket = observationSequence.Add(1)
	}
	collected, err := collect(ctx, repository, completedTask, in)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := []*Item{}
	err = transaction(ctx, repository.DataDir(), ticket, persist, func(items *Items, apply bool) error {
		if apply {
			if err := collected.apply(items, retainUnseen); err != nil {
				return err
			}
		}
		// Map iteration must not affect stable ties in display order.
		for _, key := range items.Keys() {
			item, _ := items.Get(key)
			if item.State != "pending" {
				continue
			}
			copy := *item
			copy.Signature = nil
			copy.Evidence = nil
			result = append(result, &copy)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rank := func(item *Item) int {
		switch stringValue(item.Severity) {
		case "error":
			return 0
		case "warning":
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return stringValue(result[i].LastSeenAt) > stringValue(result[j].LastSeenAt) })
	sort.SliceStable(result, func(i, j int) bool { return rank(result[i]) < rank(result[j]) })
	return result, nil
}

// RefreshTasks observes known sources without retiring unobserved old incidents.
func (s *Service) RefreshTasks(ctx context.Context, completedTask string) error {
	if !s.Complete() {
		return nil
	}
	_, err := s.snapshot(lifecycle.Detach(ctx), completedTask, true, true)
	return err
}

// TaskStatus wires status commits to notification refresh after the leaf unlock.
func (s *Service) TaskStatus(ctx context.Context, dataDir string) *taskstatus.Store {
	st := taskstatus.New(dataDir)
	st.OnRecord = func(key string) { Safely(func() error { return s.RefreshTasks(ctx, key) }) }
	return st
}
