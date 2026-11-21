package repo

import (
	"bytes"
	"fmt"
	"testing"

	"configflow/internal/model"
)

// ErrSnapshotMutated reports that a borrowed, immutable snapshot changed
// after it was published. It is a programming error in a reader, raised only
// by the view guard that test binaries enable.
var ErrSnapshotMutated = fmt.Errorf("repository snapshot mutated by a reader")

// guardMaxGenerations bounds the borrowed generations the guard remembers.
const guardMaxGenerations = 8

// guardedGeneration is the encoding of one published, borrowed generation.
type guardedGeneration struct {
	value   any
	encoded []byte
}

// defaultViewGuard is the guard of every repository opened by a test binary:
// a reader that modified a shared snapshot fails the test loudly, whichever
// package or goroutine it runs in. Production repositories have no guard.
func defaultViewGuard() {
	logger.Errorf("%v", ErrSnapshotMutated)
	panic(ErrSnapshotMutated)
}

// testViewGuard returns the guard a repository opened by this binary starts
// with: defaultViewGuard in test binaries, none otherwise.
func testViewGuard() func() {
	if testing.Testing() {
		return defaultViewGuard
	}
	return nil
}

// SetViewGuard replaces the guard called when a reader modified a shared
// snapshot; nil disables checking. It is a test seam: test binaries start with
// a guard that panics, and allocation or timing tests may switch it off.
func (r *Repository) SetViewGuard(guard func()) {
	r.guardMu.Lock()
	r.viewGuard = guard
	r.guarded, r.guardOrder = nil, nil
	r.guardMu.Unlock()
}

// guardBorrow checks a published generation that is handed to a borrower.
// The first borrow records its encoding; every later borrow of the same
// generation must see identical bytes. Recording a new generation verifies and
// forgets the oldest remembered ones, so a reader that modified a generation
// it retained is caught at the latest when that generation is retired.
// key identifies the generation (the address of its first element or of the
// document); value is what is encoded.
func (r *Repository) guardBorrow(key, value any) {
	if r.viewGuard == nil || key == nil {
		return
	}
	encoded, err := model.Marshal(value)
	r.guardMu.Lock()
	defer r.guardMu.Unlock()
	guard := r.viewGuard
	if guard == nil {
		return
	}
	if seen, ok := r.guarded[key]; ok {
		if err != nil || !bytes.Equal(seen.encoded, encoded) {
			guard()
		}
		return
	}
	if err != nil {
		guard()
		return
	}
	if r.guarded == nil {
		r.guarded = make(map[any]guardedGeneration)
	}
	r.guardOrder = append(r.guardOrder, key)
	r.guarded[key] = guardedGeneration{value: value, encoded: encoded}
	for len(r.guardOrder) > guardMaxGenerations {
		oldest := r.guardOrder[0]
		r.guardOrder = r.guardOrder[1:]
		retired, ok := r.guarded[oldest]
		delete(r.guarded, oldest)
		if !ok {
			continue
		}
		if now, err := model.Marshal(retired.value); err != nil || !bytes.Equal(now, retired.encoded) {
			guard()
		}
	}
}

// CheckBorrowed verifies every borrowed generation the guard remembers. Tests
// call it after a run that retained borrowed records; it returns
// ErrSnapshotMutated (without calling the guard) when one changed.
func (r *Repository) CheckBorrowed() error {
	r.guardMu.Lock()
	defer r.guardMu.Unlock()
	for _, key := range r.guardOrder {
		seen, ok := r.guarded[key]
		if !ok {
			continue
		}
		if now, err := model.Marshal(seen.value); err != nil || !bytes.Equal(now, seen.encoded) {
			return ErrSnapshotMutated
		}
	}
	return nil
}
