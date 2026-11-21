package repo

import (
	"context"

	"configflow/internal/model"
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
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if document, working, err := r.workingModel(ctx); working || err != nil {
		if err != nil {
			return nil, false, err
		}
		return document.System.Agents, false, nil
	}
	if snapshot := r.lockedSnapshot(ctx); snapshot != nil {
		records = snapshot.entry.Model.System.Agents
	} else {
		records, err = r.agentRecords(ctx)
		if err != nil {
			return nil, false, err
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
	}
	if len(records) > 0 {
		r.guardBorrow(&records[0], records)
	}
	return records, true, nil
}
