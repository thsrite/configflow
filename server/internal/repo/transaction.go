package repo

import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/ctxx"
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
	meta          map[string]json.RawMessage
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
// Writing is row-wise (O18 T1a, keepUnchangedRows): a row whose typed value
// the transaction did not change keeps its stored text (also a value read
// leniently), a changed value is written in Go's encoding, an omitzero member
// set to its zero value is deleted and undeclared rows are dropped.
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
// and validation up to the final commit, which Store.Commit checks
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
	var current *transaction
	err := withLock(ctx, r.lockFile, func(ctx context.Context) error {
		return r.transactLocked(ctx, options, func(ctx context.Context, tx *transaction) error {
			current = tx
			return fn(ctx, tx)
		})
	})
	if err != nil {
		return err
	}
	if current == nil || !current.changed {
		return nil
	}
	// After .config.lock is released (o3 3.2 rule 3): configuration
	// observers see configuration commits only, never Agent state commits.
	kinds := current.kinds()
	if kinds&ChangeConfig != 0 {
		r.configurationCommitted(current.base, current.document)
	}
	r.notifyCommit(kinds)
	return nil
}

// transactLocked is _write_locked: one write transaction; the caller holds
// .config.lock.
func (r *Repository) transactLocked(ctx context.Context, options writeOptions, fn func(ctx context.Context, tx *transaction) error) error {
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
		return inputError(err)
	}
	if options.batch != nil && *options.batch {
		ctx = ctxx.Uninterruptible(ctx)
	}
	maintain := true
	if settingsDamaged(base.Issues) {
		untouched, err := keepDamagedSettings(base.Model, tx.document)
		if err != nil {
			return err
		}
		maintain = !untouched
	}
	if maintain {
		if _, err := ensureSystemTokens(&tx.document.System); err != nil {
			return err
		}
	}
	if err := validateDocument(tx.document); err != nil {
		return err
	}
	oldRouters, err := routerRecordsIn(base.Model)
	if err != nil {
		return err
	}
	newRouters, err := routerRecordsIn(tx.document)
	if err != nil {
		return err
	}
	release, err := r.routerGuard(ctx, base.Model, oldRouters, newRouters, options.lockedRouters, options.policyRouter)
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
		tx.changed, tx.committed = true, committed.Head
	}
	return nil
}

// transaction is the state of one open write transaction.
type transaction struct {
	// base is the committed generation the transaction started from.
	base storedEntry
	// document is the typed working copy, owned by the transaction.
	document *model.Document
	// changed and committed: the transaction wrote, and the head it wrote.
	changed   bool
	committed store.Head
}

// kinds reports what a written transaction changed.
func (tx *transaction) kinds() ChangeKind {
	var kinds ChangeKind
	if !tx.committed.SameConfig(tx.base.Head) {
		kinds |= ChangeConfig
	}
	if tx.committed.State != tx.base.Head.State || tx.committed.StateCommitID != tx.base.Head.StateCommitID {
		kinds |= ChangeState
	}
	return kinds
}

// workingModel returns the typed working document of the write transaction
// ctx belongs to; ok is false outside a transaction.
func (r *Repository) workingModel(ctx context.Context) (*model.Document, bool, error) {
	tx := r.transactionOf(ctx)
	if tx == nil {
		return nil, false, nil
	}
	return r.systemInView(ctx, tx.document), true, nil
}

// configurationCommitted dispatches the post-commit observers on native,
// immutable snapshots.
func (r *Repository) configurationCommitted(base storedEntry, proposed *model.Document) {
	observers := r.snapshotObservers()
	if len(observers) == 0 || proposed == nil {
		return
	}
	backup, err := viewOf(&proposed.System.Backup)
	if err != nil {
		logger.Errorf(msgCommittedHookSkipped, err)
		return
	}
	if raw, _ := backup.get("auto_backup"); !jsonTruthy(raw) {
		return
	}
	for _, committed := range observers {
		committed(r, base.Model, proposed)
	}
}
