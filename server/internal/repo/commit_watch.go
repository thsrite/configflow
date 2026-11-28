package repo

import (
	"sync"

	"configflow/internal/model"
)

// ChangeKind identifies a durable repository change that can wake a reader.
type ChangeKind uint8

const (
	// ChangeConfig is a commit that changed configuration rows.
	ChangeConfig ChangeKind = 1 << iota
	// ChangeState is a commit that changed Agent state rows.
	ChangeState
	// ChangeRuleCache is a published rule file, independent of database revisions.
	ChangeRuleCache
)

type commitWatcher struct {
	kinds ChangeKind
	fn    func()
}

type commitWatchers struct {
	mu   sync.Mutex
	next int
	list map[int]commitWatcher
}

// OnCommit registers fn for durable changes of the selected kinds. Database
// commits notify after releasing .config.lock. File publications may run within
// an edit. Callbacks must only signal a reader, never block or enter the repository.
// The returned function removes the registration.
func (r *Repository) OnCommit(kinds ChangeKind, fn func()) (cancel func()) {
	r.watchers.mu.Lock()
	defer r.watchers.mu.Unlock()
	if r.watchers.list == nil {
		r.watchers.list = map[int]commitWatcher{}
	}
	id := r.watchers.next
	r.watchers.next++
	r.watchers.list[id] = commitWatcher{kinds: kinds, fn: fn}
	return func() {
		r.watchers.mu.Lock()
		delete(r.watchers.list, id)
		r.watchers.mu.Unlock()
	}
}

func (r *Repository) notifyCommit(kinds ChangeKind) {
	r.watchers.mu.Lock()
	fns := make([]func(), 0, len(r.watchers.list))
	for _, watcher := range r.watchers.list {
		if watcher.kinds&kinds != 0 {
			fns = append(fns, watcher.fn)
		}
	}
	r.watchers.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// NotifyRuleCacheChanged wakes readers after rule cache bytes are published.
func (r *Repository) NotifyRuleCacheChanged() { r.notifyCommit(ChangeRuleCache) }

type commitObservers struct {
	mu   sync.Mutex
	next int
	list map[int]func(*Repository, *model.Document, *model.Document)
}

// OnConfigurationCommitted registers fn to observe committed configuration
// documents (when auto_backup is enabled). cancel removes the registration.
func (r *Repository) OnConfigurationCommitted(fn func(*Repository, *model.Document, *model.Document)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	r.observers.mu.Lock()
	defer r.observers.mu.Unlock()
	if r.observers.list == nil {
		r.observers.list = map[int]func(*Repository, *model.Document, *model.Document){}
	}
	id := r.observers.next
	r.observers.next++
	r.observers.list[id] = fn
	return func() {
		r.observers.mu.Lock()
		delete(r.observers.list, id)
		r.observers.mu.Unlock()
	}
}

func (r *Repository) snapshotObservers() []func(*Repository, *model.Document, *model.Document) {
	r.observers.mu.Lock()
	defer r.observers.mu.Unlock()
	if len(r.observers.list) == 0 {
		return nil
	}
	fns := make([]func(*Repository, *model.Document, *model.Document), 0, len(r.observers.list))
	for _, fn := range r.observers.list {
		fns = append(fns, fn)
	}
	return fns
}

// CommitWake keeps one OnCommit registration on the repository a loop last
// used: a loop that finds its repository per round calls Follow each round,
// and a replaced repository (tests, restarts) moves the registration along.
type CommitWake struct {
	mu         sync.Mutex
	repository *Repository
	cancel     func()
}

// Follow registers fn for commits of r changing kinds, unless r is already
// the followed repository.
func (w *CommitWake) Follow(r *Repository, kinds ChangeKind, fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if r == nil || r == w.repository {
		return
	}
	if w.cancel != nil {
		w.cancel()
	}
	w.repository, w.cancel = r, r.OnCommit(kinds, fn)
}
