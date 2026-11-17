package rulelibrary

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"configflow/internal/lockx"
)

// Editing locks precede the repository lock, and never retain a leaf-cache lock
// while calling the repository. Entries disappear when their last waiter exits.
var edits = lockx.NewKeyed[string](lockx.LevelDevice, "rule-library-edit")

func editLease(ctx context.Context, item record) (context.Context, func(), error) {
	identity := record{}
	if err := put(&identity, "id", text(item, "id")); err != nil {
		return ctx, nil, err
	}
	if err := put(&identity, "source_type", "content"); err != nil {
		return ctx, nil, err
	}
	path, err := cachePath(ctx, identity)
	if err != nil {
		return ctx, nil, err
	}
	return edits.Lock(ctx, path, 5*time.Minute)
}

type stagedCache struct {
	path, temp, marker string
	wasInvalid         bool
	replace            func(string, string) error
}

func stageCache(ctx context.Context, item record, content []byte, replace func(string, string) error) (*stagedCache, error) {
	path, err := cachePath(ctx, item)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".rule-edit-*.tmp")
	if err != nil {
		return nil, err
	}
	stage := &stagedCache{path: path, temp: file.Name(), marker: path + ".invalid", replace: replace}
	if replace == nil {
		stage.replace = os.Rename
	}
	if _, err = os.Stat(stage.marker); err == nil {
		stage.wasInvalid = true
	} else if !errors.Is(err, os.ErrNotExist) {
		file.Close()
		_ = os.Remove(stage.temp)
		return nil, err
	}
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		stage.abort()
		return nil, fmt.Errorf("stage rule cache: %w", err)
	}
	// A durable invalidation receipt prevents the old stable or legacy cache from
	// being served after a committed edit, even when rename and removal fail.
	marker, err := os.OpenFile(stage.marker, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		stage.abort()
		return nil, err
	}
	err = marker.Sync()
	if closeErr := marker.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		stage.abort()
		return nil, err
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		stage.abort()
		return nil, err
	}
	return stage, nil
}
func (s *stagedCache) abort() {
	if s == nil {
		return
	}
	_ = os.Remove(s.temp)
	if !s.wasInvalid {
		_ = os.Remove(s.marker)
	}
}

// failClosed is used when the repository cannot establish a failed commit's
// outcome. Keep the durable receipt across restarts; readers use DB/inline.
func (s *stagedCache) failClosed() {
	if s == nil {
		return
	}
	_ = os.Remove(s.temp)
	_ = os.Remove(s.path)
}
func (s *stagedCache) commit() error {
	if s == nil {
		return nil
	}
	if err := s.replace(s.temp, s.path); err != nil {
		_ = os.Remove(s.path)
		_ = os.Remove(s.temp)
		return fmt.Errorf("replace rule cache: %w", err)
	}
	// Persist the replacement before clearing its durable invalidation receipt.
	if err := syncDirectory(filepath.Dir(s.path)); err != nil {
		return fmt.Errorf("persist rule cache replacement: %w", err)
	}
	if err := os.Remove(s.marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear rule cache invalidation: %w", err)
	}
	return syncDirectory(filepath.Dir(s.path))
}

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	err = file.Sync()
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}
