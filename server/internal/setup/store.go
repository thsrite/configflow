package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/repo"
)

const maxRecordBytes = 16 << 20 // Same limit as the existing setupflow reader.

var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// Session operations span repository transactions and installation reads, so
// they are L2 operation locks, before L3. All Store instances share these keys.
var sessionLocks = lockx.NewKeyed[string](lockx.LevelDevice, "setup-session")

type recordStore struct{ root string }

func storeFor(r *repo.Repository) recordStore {
	return recordStore{filepath.Join(r.DataDir(), "setup-sessions")}
}

func (s recordStore) path(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", failure(msgSessionID, 404)
	}
	return filepath.Join(s.root, id+".json"), nil
}

func (s recordStore) locked(ctx context.Context, id string, run func(context.Context) error) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	held, release, err := sessionLocks.Lock(ctx, path, repo.LockTimeout())
	if err != nil {
		return err
	}
	defer release()
	if err := held.Err(); err != nil {
		return err
	}
	return run(held)
}

func (s recordStore) read(ctx context.Context, id string) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.path(id)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, failure(msgMissingSession, 404)
	}
	if err != nil {
		return nil, fmt.Errorf("read setup session: %w", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxRecordBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read setup session: %w", err)
	}
	var record Record
	if len(raw) > maxRecordBytes || json.Unmarshal(raw, &record) != nil || record.ID != id || record.ProfileID == "" {
		return nil, ErrCorruptRecord
	}
	return &record, ctx.Err()
}

// write is called with the session operation lock held. Shutdown cancellation
// is checked after lock acquisition and again immediately before replacement.
func (s recordStore) write(ctx context.Context, record *Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(record.ID)
	if err != nil {
		return err
	}
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode setup session: %w", err)
	}
	if len(raw) > maxRecordBytes {
		return ErrCorruptRecord
	}
	if err := os.MkdirAll(s.root, 0700); err != nil {
		return fmt.Errorf("create setup session directory: %w", err)
	}
	file, err := os.CreateTemp(s.root, ".setup-")
	if err != nil {
		return fmt.Errorf("create setup session temporary file: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("write setup session: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close setup session: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace setup session: %w", err)
	}
	return nil
}

func (s recordStore) remove(ctx context.Context, id string) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s recordStore) ids(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list setup sessions: %w", err)
	}
	ids := []string{}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") && validID.MatchString(id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// AdoptOwnedWrite records a committed embedded onboarding save as the wizard's
// own edit. A21 calls this after releasing L3 and checking revision+1; this owner
// independently retains shutdown cancellation and serializes session updates.
func AdoptOwnedWrite(ctx context.Context, r *repo.Repository, profileID string, before, after json.RawMessage) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	previous, err := OwnedDigest(before, false)
	if err != nil {
		return err
	}
	next, err := OwnedDigest(after, false)
	if err != nil {
		return err
	}
	withoutBase, err := OwnedDigest(after, true)
	if err != nil {
		return err
	}
	s := storeFor(r)
	ids, err := s.ids(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		err := s.locked(ctx, id, func(held context.Context) error {
			record, err := s.read(held, id)
			var missing *Error
			if errors.As(err, &missing) && missing.Status == 404 {
				return nil
			}
			if err != nil {
				return err
			}
			if record.ProfileID != profileID || enabled(record.Completed) || text(record.OwnedDigest) != previous {
				return nil
			}
			record.OwnedDigest, record.OwnedDigestWithoutBase = &next, &withoutBase
			return s.write(held, record)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
