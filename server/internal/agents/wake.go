package agents

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

// Change-driven background loops (design/o17-runtime-state.md 2.6). A loop
// that depends on stored configuration, Agent state and Agent presence is
// woken when one of them changes, instead of re-reading everything every few
// seconds: Waker ties a lifecycle.Periodic to the repository's commits
// (repo.OnCommit) and to Agents coming online (Manager.OnPresence). While a
// round reports work in flight (a device to poll, a retry to wait for) the
// loop keeps its short interval; otherwise it rests until woken, with a long
// fallback for anything the notifications cannot see.

// IdleFallback is the pause of an idle change-driven loop: what the loop
// reads outside the configuration database is still looked at this often.
const IdleFallback = 10 * time.Minute

// Waker is the wake signal and pacing of one change-driven loop.
type Waker struct {
	signal lifecycle.Signal
	busy   atomic.Bool
	epoch  atomic.Uint64
	active time.Duration
	idle   time.Duration
}

// NewWaker returns a Waker pausing active while a round reported work in
// flight and idle otherwise.
func NewWaker(active, idle time.Duration) *Waker {
	return &Waker{active: active, idle: idle}
}

// Signal is the loop's lifecycle.Periodic.Wake.
func (w *Waker) Signal() *lifecycle.Signal { return &w.signal }

// Interval is the loop's lifecycle.Periodic.Interval.
func (w *Waker) Interval(context.Context) time.Duration {
	if w.busy.Load() {
		return w.active
	}
	return w.idle
}

// Done records whether the finished round left work in flight.
func (w *Waker) Done(busy bool) { w.busy.Store(busy) }

// Busy reports what the last round recorded.
func (w *Waker) Busy() bool { return w.busy.Load() }

// Wake ends the loop's current pause.
func (w *Waker) Wake() {
	w.epoch.Add(1)
	w.signal.Notify()
}

// Epoch identifies the latest input change, including coalesced wakes.
func (w *Waker) Epoch() uint64 { return w.epoch.Load() }

// Watch wakes the loop on commits of m's repository that change kinds and,
// with presence, when an Agent of m comes online. It ends when m closes.
func (w *Waker) Watch(m *Manager, kinds repo.ChangeKind, presence bool) {
	cancels := []func(){m.repository.OnCommit(kinds, w.Wake)}
	if presence {
		cancels = append(cancels, m.OnPresence(w.Wake))
	}
	stop := func() {
		for _, cancel := range cancels {
			cancel()
		}
	}
	if !m.OnClose(stop) {
		stop()
	}
}

type presenceWatchers struct {
	mu   sync.Mutex
	next int
	list map[int]func()
}

// OnPresence registers fn to run when an Agent of m comes online (its first
// heartbeat, or the first after it was offline). fn runs on the heartbeat's
// goroutine after the runtime state is updated and must not block.
func (m *Manager) OnPresence(fn func()) (cancel func()) {
	m.presence.mu.Lock()
	defer m.presence.mu.Unlock()
	if m.presence.list == nil {
		m.presence.list = map[int]func(){}
	}
	id := m.presence.next
	m.presence.next++
	m.presence.list[id] = fn
	return func() {
		m.presence.mu.Lock()
		delete(m.presence.list, id)
		m.presence.mu.Unlock()
	}
}

func (m *Manager) notifyPresence() {
	m.presence.mu.Lock()
	fns := make([]func(), 0, len(m.presence.list))
	for _, fn := range m.presence.list {
		fns = append(fns, fn)
	}
	m.presence.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// fresh reports an Agent whose last heartbeat is recent enough to be online.
func (s *RuntimeState) fresh(id string) bool {
	s.mu.Lock()
	entry, ok := s.entries[id]
	s.mu.Unlock()
	return ok && nowTimestamp()-entry.HeartbeatAt <= HeartbeatFreshSeconds
}
