package repo

import (
	"context"
	"errors"

	"configflow/internal/ctxx"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/store"
)

// Write transactions (design n7-n8 2.5 and section 3).
//
// One writer at a time: a transaction holds the profile-independent
// .config.lock (L3, cross-process flock plus the in-process lock of O3-6). It
// reads the committed generation, decodes its own working copy of the typed
// document, runs the caller's function on it and, when that returns nil,
// validates the result and commits only the rows whose bytes changed. An
// error from the function discards the working copy. The ConfigurationCommitted
// hook runs after the commit, once the lock is released.
//
// The context handed to the function belongs to the transaction: repository
// reads made with it (View, Profile, the legacy readers) see the working copy;
// a write made with it fails with ErrInUse (nestedWriteMessage), like a nested
// write on one Python thread. Write entry points detach request cancellation
// first (PORTING 4b, ctxx.Detach), so a disconnected client cannot abort a
// commit half-way; shutdown cancellation is kept.

// transaction is the state of one open write transaction.
type transaction struct {
	// base is the committed generation the transaction started from.
	base storedEntry
	// document is the typed working copy, owned by the transaction.
	document *model.Document
	// legacy is the *jsonx.Object working copy while a legacy updater runs
	// (legacy.go); it is authoritative until converted back into document.
	legacy *jsonx.Object
	// proposed, when set, is a legacy view equal to document (the legacy
	// updater's result); validation uses it instead of converting again.
	proposed *jsonx.Object
}

type txKey struct{ repository *Repository }

func (r *Repository) transactionOf(ctx context.Context) *transaction {
	tx, _ := ctx.Value(txKey{r}).(*transaction)
	return tx
}

// InTransaction reports whether ctx belongs to an open write transaction of
// this repository (Python: repository._local.document is not None).
func (r *Repository) InTransaction(ctx context.Context) bool { return r.transactionOf(ctx) != nil }

const nestedWriteMessage = "不能在配置事务中再次开启写事务"

type writeOptions struct {
	lockedRouters map[routerKey]bool
	policyRouter  *routerKey
	meta          *jsonx.Object
	batch         *bool // marked immediately before an approved filesystem + DB batch
}

// beginCommitBatch is restricted to the three paths listed in Clarified in
// O3-6c. Once filesystem effects start, their final database commit must finish.
func beginCommitBatch(ctx context.Context, started *bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	*started = true
	return nil
}

// Update runs fn as one write transaction on the whole typed document. fn
// owns d only while it runs. Update maintains no section revisions or
// timestamps: callers that change a profile, the shared resources or the
// system section bump that section's Revision themselves. A nil error from fn
// commits after validation; any error discards the working copy.
//
// Writing follows the codec rules of model: untouched members keep their
// stored bytes (also null and wrong-typed values), assigned values replace
// them, and only Clear (Python's pop) removes a stored member.
func (r *Repository) Update(ctx context.Context, fn func(ctx context.Context, d *model.Document) error) error {
	ctx = ctxx.Detach(ctx)
	return r.transact(ctx, writeOptions{}, func(ctx context.Context, tx *transaction) error {
		return fn(ctx, tx.document)
	})
}

// transact is `with self._document(write=True, ...)`: one write transaction
// under .config.lock; see the package notes above.
//
// Shutdown (O3-6c): the work context stays cancellable through the callback
// and validation up to the final commit, which Store.CommitContext checks
// immediately before COMMIT; a cancelled transaction writes nothing.
// Callbacks otherwise edit only the candidate. A guard-only callback may commit
// a checked file action and abort its unchanged candidate (C33's replayable
// marker protocol). Filesystem + DB batches use beginCommitBatch only on the
// three approved paths; once marked, the final commit completes.
func (r *Repository) transact(ctx context.Context, options writeOptions, fn func(ctx context.Context, tx *transaction) error) error {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if readSnapshotActive(ctx) {
		return ErrReadOnlySnapshot
	}
	if r.InTransaction(ctx) {
		return inUse(nestedWriteMessage)
	}
	var previous, current *jsonx.Object
	err := withLock(ctx, r.lockFile, func(ctx context.Context) error {
		return r.transactLocked(ctx, options, func(ctx context.Context, tx *transaction) error {
			if err := fn(ctx, tx); err != nil {
				return err
			}
			previous = tx.base.Document
			return nil
		}, &current)
	})
	if err != nil {
		return err
	}
	if jsonx.Truthy(current.GetObject("system").GetObject("backup").Value("auto_backup")) {
		if committed := currentHooks().ConfigurationCommitted; committed != nil {
			committed(r, previous, current)
		}
	}
	return nil
}

// transactLocked is _write_locked: one write transaction; the caller holds
// .config.lock. When proposed is not nil it receives the legacy view of the
// document that was validated (and committed, unless nothing changed).
func (r *Repository) transactLocked(ctx context.Context, options writeOptions, fn func(ctx context.Context, tx *transaction) error,
	proposed ...**jsonx.Object) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.InTransaction(ctx) {
		return inUse(nestedWriteMessage)
	}
	base, err := r.readEntry(ctx, true)
	if err != nil {
		return err
	}
	// The working copy is decoded from the immutable rows, never shared with
	// the cached snapshot that View hands out.
	document, _, err := decodeModelRows(base.Entry.Document)
	if err != nil {
		return err
	}
	tx := &transaction{base: base, document: document}
	if err := fn(context.WithValue(ctx, txKey{r}, tx), tx); err != nil {
		return err
	}
	if options.batch != nil && *options.batch {
		ctx = ctxx.Uninterruptible(ctx)
	}
	// Business validation and the router guard still run on the legacy view
	// until their owners are migrated (N8-5b/c).
	legacy := tx.proposed
	if legacy == nil {
		if legacy, err = legacyModelDocument(tx.document); err != nil {
			return err
		}
	}
	if err := ensureLegacyRuleProxyToken(tx, legacy); err != nil {
		return err
	}
	if err := validateDocument(tx.document); err != nil {
		return err
	}
	release, err := r.routerCommitGuard(ctx, base.Document, legacy, options.lockedRouters, options.policyRouter)
	if err != nil {
		return err
	}
	defer release()
	committed, changed, err := r.commitStore(ctx, base, tx.document, options.meta)
	if err != nil {
		r.dropCache()
		if errors.Is(err, store.ErrConflict) {
			return &Error{Kind: ErrRepository, Message: conflictMessage, Cause: err}
		}
		return err
	}
	if changed {
		r.setCache(committed)
		r.commitCount.Add(1)
	}
	for _, target := range proposed {
		*target = legacy
	}
	return nil
}
