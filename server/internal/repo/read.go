package repo

import (
	"configflow/internal/jsondecode"
	"context"
	"fmt"

	"configflow/internal/model"
)

// Typed reads (design n7-n8 2.5).
//
// System and the Clone* reads return values the caller owns: each call
// decodes a fresh record from the generation it reads, so the caller may
// modify it freely; nothing it does reaches the repository unless it is
// written through a write transaction. Profile, Shared, SystemConfig, Backup
// and Config borrow the snapshot instead (config_borrow.go).
//
// View hands fn the decoded document instead, without copying. Cache hits share
// the snapshot identified by its store head, so View is the read for hot paths.
// Oversized documents are not cached, and concurrent cold loads may decode the
// same head independently; snapshot pointer identity is not an API guarantee.
// The document is a read-only snapshot:
//
//   - fn must not modify it, nor keep it or anything reachable from it after
//     returning. Model containers (Map, Fields, Dynamic) copy on write, but
//     slices and records do not: a modification would be seen by every reader
//     of the generation and could be written back by none.
//   - A committed snapshot is immutable for its lifetime: a commit by this or another process
//     publishes a new generation and never touches the snapshot, so fn sees
//     one consistent generation without holding any lock.
//   - View holds no lock across fn itself (failed loads may retry under L3).
//     With the context of a write transaction it sees that
//     transaction's working copy (uncommitted changes included); with the
//     context of WithReadSnapshot it sees that locked generation. Neither may
//     be modified through View either.
//   - model.Issues reports decoding diagnostics, not validation of later field
//     assignments. Clear removes the cleared member's diagnostic.
//   - It honours cancellation: a cancelled ctx returns ctx.Err() before reading.
//
// Values from View are never handed to a writer; writers read inside their
// transaction (Update*, whose working copy is their own).

// View runs fn on a read-only snapshot of the configuration document; see the
// rules above.
func (r *Repository) View(ctx context.Context, fn func(document *model.Document) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	document, err := r.viewDocument(ctx)
	if err != nil {
		return err
	}
	guard := r.currentViewGuard()
	if guard == nil {
		return fn(document)
	}
	before, err := model.Marshal(document)
	if err != nil {
		return err
	}
	result := fn(document)
	after, err := model.Marshal(document)
	if err != nil || string(after) != string(before) {
		guard()
	}
	return result
}

// read is View for the legacy readers, which keep their behaviour until their
// callers migrate: like Python they do not stop for a cancelled context.
func (r *Repository) read(ctx context.Context, fn func(document *model.Document) error) error {
	document, err := r.viewDocument(ctx)
	if err != nil {
		return err
	}
	return fn(document)
}

// viewDocument is the document a reader with ctx sees.
func (r *Repository) viewDocument(ctx context.Context) (*model.Document, error) {
	if document, ok, err := r.workingModel(ctx); ok || err != nil {
		return document, err
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, err
	}
	return entry.Model, nil
}

// copyRecord decodes a fresh copy of a model record: its unchanged members
// re-encode to the same bytes, so the copy keeps R1/R2 provenance.
func copyRecord[T any](value *T) (*T, error) {
	data, err := model.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := new(T)
	if err := jsondecode.Unmarshal(data, result); err != nil {
		return nil, fmt.Errorf("copy model record: %w", err)
	}
	return result, nil
}

// Profile returns a stored profile (its own fields only; shared resources are
// not resolved), borrowed from the snapshot: see "Borrowed reads" in
// config_borrow.go. ErrValidation for a malformed id, ErrNotFound for an unknown
// one. CloneProfile returns a deep copy for callers that edit it.
func (r *Repository) Profile(ctx context.Context, profileID string) (*model.Profile, error) {
	return r.readProfile(ctx, profileID, false)
}

// CloneProfile is Profile as a caller-owned deep copy (one encode and decode
// of the profile), for editors that modify nested members.
func (r *Repository) CloneProfile(ctx context.Context, profileID string) (*model.Profile, error) {
	return r.readProfile(ctx, profileID, true)
}

func (r *Repository) readProfile(ctx context.Context, profileID string, owned bool) (*model.Profile, error) {
	if err := ValidateProfileID(profileID); err != nil {
		return nil, err
	}
	var result *model.Profile
	err := r.borrow(ctx, func(document *model.Document, shared bool) error {
		profile := profileByKey(document, profileID)
		if profile == nil {
			return notFound(profileID)
		}
		var err error
		result, err = lend(profile, shared && !owned)
		return err
	})
	return result, err
}

// Shared returns the shared section borrowed from the snapshot; see
// "Borrowed reads" in config_borrow.go. CloneShared returns a deep copy.
func (r *Repository) Shared(ctx context.Context) (*model.Shared, error) {
	return readSection(r, ctx, false, func(document *model.Document) *model.Shared { return &document.Shared })
}

// CloneShared is Shared as a caller-owned deep copy (one encode and decode of
// every shared resource), for editors that modify catalog records.
func (r *Repository) CloneShared(ctx context.Context) (*model.Shared, error) {
	return readSection(r, ctx, true, func(document *model.Document) *model.Shared { return &document.Shared })
}

// System returns a caller-owned copy of the system section (settings, backup
// and Agents).
func (r *Repository) System(ctx context.Context) (*model.System, error) {
	var result *model.System
	err := r.View(ctx, func(document *model.Document) error {
		var err error
		result, err = copyRecord(&document.System)
		return err
	})
	return result, err
}

// SystemConfig returns the system settings record only, borrowed from the
// snapshot; see "Borrowed reads" in config_borrow.go. Callers that read settings
// use it instead of System, which also copies every Agent with its deployment
// and verification history. CloneSystemConfig returns a deep copy.
func (r *Repository) SystemConfig(ctx context.Context) (*model.SystemConfig, error) {
	return readSection(r, ctx, false, func(document *model.Document) *model.SystemConfig { return &document.System.SystemConfig })
}

// CloneSystemConfig is SystemConfig as a caller-owned deep copy, for settings
// editors.
func (r *Repository) CloneSystemConfig(ctx context.Context) (*model.SystemConfig, error) {
	return readSection(r, ctx, true, func(document *model.Document) *model.SystemConfig { return &document.System.SystemConfig })
}

// Backup returns the backup settings record only, borrowed from the snapshot;
// see "Borrowed reads" in config_borrow.go. CloneBackup returns a deep copy.
func (r *Repository) Backup(ctx context.Context) (*model.Backup, error) {
	return readSection(r, ctx, false, func(document *model.Document) *model.Backup { return &document.System.Backup })
}

// CloneBackup is Backup as a caller-owned deep copy, for settings editors.
func (r *Repository) CloneBackup(ctx context.Context) (*model.Backup, error) {
	return readSection(r, ctx, true, func(document *model.Document) *model.Backup { return &document.System.Backup })
}

// readSection lends (or, owned or inside a writer, deep-copies) one record of
// the document a reader with ctx sees.
func readSection[T any](r *Repository, ctx context.Context, owned bool, section func(*model.Document) *T) (*T, error) {
	var result *T
	err := r.borrow(ctx, func(document *model.Document, shared bool) error {
		var err error
		result, err = lend(section(document), shared && !owned)
		return err
	})
	return result, err
}

// profileByKey finds a profile by its section key, or nil.
func profileByKey(document *model.Document, profileID string) *model.Profile {
	for _, profile := range document.Profiles {
		if profile.Key() == profileID {
			return profile
		}
	}
	return nil
}
