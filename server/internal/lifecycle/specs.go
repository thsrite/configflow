package lifecycle

import (
	"configflow/internal/clock"
	"context"
	"sort"
	"sync"
	"time"
)

// Timer is the shared stoppable one-shot timer supplied by a Clock.
type Timer = clock.Timer

// Clock is the shared time source; tests use lifecycletest.Clock.
type Clock = clock.Clock

type realClock = clock.Real

// Signal coalesces notifications that interrupt a loop's current or next pause.
// Its zero value is usable; notifying during a round never starts another round.
type Signal struct {
	once sync.Once
	ch   chan struct{}
}

func (s *Signal) channel() <-chan struct{} {
	if s == nil {
		return nil
	}
	s.once.Do(func() { s.ch = make(chan struct{}, 1) })
	return s.ch
}

// Notify wakes a waiting loop, retaining at most one pending notification.
func (s *Signal) Notify() {
	if s == nil {
		return
	}
	s.channel()
	select {
	case s.ch <- struct{}{}:
	default:
	}
}

// Clear discards at most one pending notification without waiting. It is used
// when initializing owner state, before a loop's first wait. Later Notify calls
// retain their usual coalescing semantics.
func (s *Signal) Clear() {
	select {
	case <-s.channel():
	default:
	}
}

// StartMode selects when a periodic loop becomes eligible to start.
type StartMode int

const (
	// AtBoot starts a loop when its Runtime starts.
	AtBoot StartMode = iota
	// AfterFirstRequest waits for the first admitted request in each runtime generation.
	AfterFirstRequest
)

// Periodic describes a loop whose intervals are measured from round completion.
type Periodic struct {
	// OnStart initializes owner memory once per activation, before the first wait.
	// It runs synchronously, must not block or call lifecycle registration methods.
	// Manual mode never invokes it. Use Startup for one-shot I/O.
	OnStart    func(context.Context)
	Name       string
	Start      StartMode
	FirstDelay time.Duration
	Interval   func(context.Context) time.Duration
	Round      func(context.Context) error
	Wake       *Signal
}

// Startup describes synchronous once-per-start work, in registration order.
type Startup struct {
	Name string
	Run  func(context.Context) error
}

// Phase orders shutdown hooks around work drainage.
type Phase int

const (
	// PhaseStopSignal runs immediately when shutdown starts.
	PhaseStopSignal Phase = iota
	// PhaseAfterDrain runs after work has drained or its cancellation deadline passed.
	PhaseAfterDrain
	// PhaseClose releases final resources after after-drain hooks.
	PhaseClose
)

// ShutdownHook runs within its phase, in registration order.
type ShutdownHook struct {
	Name  string
	Phase Phase
	Run   func(context.Context)
}

// Registry owns task specifications. Register before starting an isolated Runtime;
// global periodic registration also supports wiring newly named specifications while running.
// Re-registering a name replaces it in place, allowing repeated application wiring.
type Registry struct {
	mu        sync.Mutex
	periodics []Periodic
	startups  []Startup
	hooks     []ShutdownHook
}

// NewRegistry creates an independent registry for a Runtime.
func NewRegistry() *Registry { return new(Registry) }

// RegisterPeriodic adds or replaces a named periodic specification.
func (r *Registry) RegisterPeriodic(p Periodic) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.periodics {
		if r.periodics[i].Name == p.Name {
			r.periodics[i] = p
			return
		}
	}
	r.periodics = append(r.periodics, p)
}

// RegisterStartup adds or replaces a named startup step without changing its order.
func (r *Registry) RegisterStartup(s Startup) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.startups {
		if r.startups[i].Name == s.Name {
			r.startups[i] = s
			return
		}
	}
	r.startups = append(r.startups, s)
}

// RegisterShutdown adds or replaces a hook with the same name and phase.
func (r *Registry) RegisterShutdown(h ShutdownHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.hooks {
		if r.hooks[i].Name == h.Name && r.hooks[i].Phase == h.Phase {
			r.hooks[i] = h
			return
		}
	}
	r.hooks = append(r.hooks, h)
}

type registrySnapshot struct {
	periodics []Periodic
	startups  []Startup
	hooks     []ShutdownHook
}

func (r *Registry) snapshot() registrySnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return registrySnapshot{append([]Periodic(nil), r.periodics...), append([]Startup(nil), r.startups...), append([]ShutdownHook(nil), r.hooks...)}
}

func (r *Registry) names() []string {
	s := r.snapshot()
	seen := map[string]bool{}
	for _, p := range s.periodics {
		seen[p.Name] = true
	}
	for _, p := range s.startups {
		seen[p.Name] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Mode selects automatic timer driving or explicit test-driven rounds.
type Mode int

const (
	// ModeRun drives registered loops with their Clock timers.
	ModeRun Mode = iota
	// ModeManual runs startup work but requires explicit RunRound calls for loops.
	ModeManual
)

// Options configures an independent Runtime. Zero Clock, Grace and Registry
// select real time, eight seconds and an independent empty registry respectively.
type Options struct {
	Clock    Clock
	Mode     Mode
	Grace    time.Duration
	Registry *Registry
	// Drain waits for admitted HTTP handlers during shutdown (O3-3 wiring).
	// It runs concurrently with job drainage and must honour ctx.
	Drain func(context.Context) error
}
