package repo

import (
	"context"

	"configflow/internal/model"
)

// Borrowed reads (O10-R2, design o10-rootcause §2 S1).
//
// Config, ConfigWithOxiDNSAgents, ConfigFrom, Profile, Shared, SystemConfig
// and Backup no longer decode a private copy for every call. Outside write
// transactions and section updaters they return values that share the
// committed generation's immutable snapshot:
//
//   - the returned top-level value (the *model.Config, *model.Profile,
//     *model.Shared, … and the Config's resource slices) is the caller's own
//     shallow copy: assigning its fields, Set/SetJSON/Clear on it and
//     appending to its top-level slices are safe;
//   - everything reachable through it (slice elements of the stored records,
//     pointer targets, nested records) belongs to the snapshot and must never
//     be modified in place. Clone a slice (slices.Clone) or record (the
//     Clone* reads) before changing it;
//   - the value may be kept: a committed snapshot never changes.
//
// Inside a write transaction or a section updater the same reads return a
// caller-owned deep copy as before, because the document they read is the
// writer's working copy, which is modified after the read returns.
//
// The view guard of test binaries (view_guard.go) records every document a
// borrowing read lends and checks it again on later borrows.

// borrowsSnapshot reports whether a read with ctx sees an immutable committed
// snapshot (a cached or freshly decoded generation, or a locked read
// snapshot) rather than a writer's working copy.
func (r *Repository) borrowsSnapshot(ctx context.Context) bool {
	if r.transactionOf(ctx) != nil {
		return false
	}
	view, _ := ctx.Value(systemViewKey{}).(*systemView)
	return view == nil || view.repository != r || !view.active.Load()
}

// borrow runs fn on the document a reader with ctx sees and tells whether it
// is a shared immutable snapshot (true) or a writer's working copy (false),
// which a borrowing read must copy from.
func (r *Repository) borrow(ctx context.Context, fn func(document *model.Document, shared bool) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	shared := r.borrowsSnapshot(ctx)
	document, err := r.viewDocument(ctx)
	if err != nil {
		return err
	}
	if shared {
		r.guardBorrow(document, document)
	}
	return fn(document, shared)
}

// lend returns a shallow copy of a snapshot record for a borrowing read, or a
// deep copy when the record belongs to a writer's working copy.
func lend[T any](value *T, shared bool) (*T, error) {
	if !shared {
		return copyRecord(value)
	}
	result := *value
	return &result, nil
}
