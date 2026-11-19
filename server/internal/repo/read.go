package repo

import (
	"configflow/internal/jsondecode"
	"context"
	"fmt"

	"configflow/internal/model"
)

// Typed reads (design n7-n8 2.5).
//
// Profile, Shared and System return values the caller owns: each call decodes
// a fresh record from the generation it reads, so the caller may modify it
// freely; nothing it does reaches the repository unless it is written through
// a write transaction.
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
	if r.viewGuard == nil {
		return fn(document)
	}
	before, err := model.Marshal(document)
	if err != nil {
		return err
	}
	result := fn(document)
	after, err := model.Marshal(document)
	if err != nil || string(after) != string(before) {
		r.viewGuard()
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

// Profile returns a caller-owned copy of a stored profile (its own fields
// only; shared resources are not resolved). ErrValidation for a malformed id,
// ErrNotFound for an unknown one.
func (r *Repository) Profile(ctx context.Context, profileID string) (*model.Profile, error) {
	if err := ValidateProfileID(profileID); err != nil {
		return nil, err
	}
	var result *model.Profile
	err := r.View(ctx, func(document *model.Document) error {
		profile := profileByKey(document, profileID)
		if profile == nil {
			return notFound(profileID)
		}
		var err error
		result, err = copyRecord(profile)
		return err
	})
	return result, err
}

// Shared returns a caller-owned copy of the shared section.
func (r *Repository) Shared(ctx context.Context) (*model.Shared, error) {
	var result *model.Shared
	err := r.View(ctx, func(document *model.Document) error {
		var err error
		result, err = copyRecord(&document.Shared)
		return err
	})
	return result, err
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

// profileByKey finds a profile by its section key, or nil.
func profileByKey(document *model.Document, profileID string) *model.Profile {
	for _, profile := range document.Profiles {
		if profile.Key() == profileID {
			return profile
		}
	}
	return nil
}
