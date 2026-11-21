package repo

import (
	"context"

	"configflow/internal/model"
	"configflow/internal/store"
)

// BorrowAgents returns the Agent registry visible to ctx without copying it.
//
// When shared is true the records are one published generation (the cached
// registry, or the generation pinned by WithReadSnapshot). It is immutable for
// its lifetime: commits publish a new generation and never touch it. The caller
// may keep the records, but must not modify anything reachable from them: no
// assignment through pointers, no element assignment or append on the slice or
// on nested slices. A shallow copy of a record (copy the struct value) may
// reassign its own top-level fields; Raw, Fields and Map copy on write, so
// SetJSON, Clear, Map.Set and Fields.Set on such a copy never reach the
// generation. Test binaries verify this with the view guard on every borrow.
//
// When shared is false ctx belongs to a write transaction and the records are
// its working copy, which the transaction may still change: the caller must
// deep-copy what it keeps beyond the current step and must not modify it either.
func (r *Repository) BorrowAgents(ctx context.Context) (records []model.Agent, shared bool, err error) {
	records, _, shared, err = r.BorrowAgentsAt(ctx)
	return records, shared, err
}

// BorrowAgentsAt is BorrowAgents that also returns the store head of the
// generation the records belong to, so a reader can key data derived from
// them. Inside a write transaction (shared false) head is the zero Head: the
// working records have no published generation and must not be cached.
func (r *Repository) BorrowAgentsAt(ctx context.Context) (records []model.Agent, head store.Head, shared bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, store.Head{}, false, err
	}
	if document, working, err := r.workingModel(ctx); working || err != nil {
		if err != nil {
			return nil, store.Head{}, false, err
		}
		return document.System.Agents, store.Head{}, false, nil
	}
	if snapshot := r.lockedSnapshot(ctx); snapshot != nil {
		records, head = snapshot.entry.Model.System.Agents, snapshot.entry.Head
	} else {
		records, head, err = r.agentRecords(ctx)
		if err != nil {
			return nil, store.Head{}, false, err
		}
		if err := ctx.Err(); err != nil {
			return nil, store.Head{}, false, err
		}
	}
	if len(records) > 0 {
		r.guardBorrow(&records[0], records)
	}
	return records, head, true, nil
}
