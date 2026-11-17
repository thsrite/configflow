package rulelibrary

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/jsonx"
	"configflow/internal/lockx"
	"configflow/internal/repo"
)

// Editing locks precede the repository lock, and never retain a leaf-cache lock
// while calling the repository. Entries disappear when their last waiter exits.
var edits = lockx.NewKeyed[string](lockx.LevelDevice, "rule-library-edit")

func editLease(ctx context.Context, item record) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
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
	held, release, err := edits.Lock(ctx, path, 5*time.Minute)
	if err != nil {
		return ctx, nil, err
	}
	if err = held.Err(); err != nil {
		release()
		return ctx, nil, err
	}
	return held, release, nil
}

type stagedCache struct {
	path, temp, marker string
	item               record
	baseline           record
	wasInvalid         bool
	replace            func(context.Context, string, string) error
}

func stageCache(ctx context.Context, item record, content []byte, replace func(context.Context, string, string) error) (*stagedCache, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	baseline, err := source(ctx, text(item, "id"))
	if err != nil {
		return nil, err
	}
	path, err := cachePath(ctx, item)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// A previous stopped owner leaves its durable receipt and private temp in
	// place. The next admitted edit of this identity owns its cleanup lease.
	prefix := ".rule-edit-" + filepath.Base(path) + "-"
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), prefix+"*.tmp"))
	if err != nil {
		return nil, err
	}
	for _, leftover := range leftovers {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if err = os.Remove(leftover); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), prefix+"*.tmp")
	if err != nil {
		return nil, err
	}
	stage := &stagedCache{path: path, temp: file.Name(), marker: path + ".invalid", item: item, baseline: baseline, replace: replace}
	if replace == nil {
		stage.replace = replaceFile
	}
	if _, err = os.Stat(stage.marker); err == nil {
		stage.wasInvalid = true
	} else if !errors.Is(err, os.ErrNotExist) {
		file.Close()
		if ctx.Err() == nil {
			_ = os.Remove(stage.temp)
		}
		return nil, err
	}
	if err = ctx.Err(); err == nil {
		_, err = file.Write(content)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		stage.abort(ctx)
		return nil, fmt.Errorf("stage rule cache: %w", err)
	}
	// A durable invalidation receipt prevents the old stable or legacy cache from
	// being served after a committed edit, even when rename and removal fail.
	if err = ctx.Err(); err != nil {
		stage.abort(ctx)
		return nil, err
	}
	marker, err := os.OpenFile(stage.marker, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		stage.abort(ctx)
		return nil, err
	}
	err = ctx.Err()
	if err == nil {
		err = marker.Sync()
	}
	if closeErr := marker.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		stage.abort(ctx)
		return nil, err
	}
	if err = syncDirectory(ctx, filepath.Dir(path)); err != nil {
		stage.abort(ctx)
		return nil, err
	}
	return stage, nil
}
func (s *stagedCache) abort(ctx context.Context) {
	if s == nil || ctx.Err() != nil {
		return
	}
	_ = os.Remove(s.temp)
	if s.wasInvalid || ctx.Err() != nil {
		return
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return
	}
	clear := func(tx context.Context, catalog *jsonx.Object) {
		for _, value := range catalog.GetList("rule_library") {
			current, ok := value.(*jsonx.Object)
			if !ok || current.GetString("id") != text(s.item, "id") {
				continue
			}
			item, err := native(current)
			if err == nil && !different(item, s.baseline, declarationFields) && tx.Err() == nil {
				_ = os.Remove(s.marker)
			}
			return
		}
	}
	if r.InTransaction(ctx) {
		// A staging failure is still inside the original L3 transaction.
		// Re-entering it would deadlock; its working declaration is the baseline.
		if catalog, err := r.GetShared(ctx); err == nil {
			clear(ctx, catalog)
		}
		return
	}
	_, _ = r.UpdateSharedTransaction(ctx, func(tx context.Context, catalog *jsonx.Object) (*jsonx.Object, error) {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		clear(tx, catalog)
		return nil, errCachePublished
	})
}

// failClosed is used when the repository cannot establish a failed commit's
// outcome. Keep the durable receipt across restarts; readers use DB/inline.
func (s *stagedCache) failClosed(ctx context.Context) {
	if s == nil || ctx.Err() != nil {
		return
	}
	_ = os.Remove(s.temp)
	if ctx.Err() == nil {
		_ = os.Remove(s.path)
	}
}
func (s *stagedCache) commit(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	_, err = r.UpdateSharedTransaction(ctx, func(tx context.Context, catalog *jsonx.Object) (*jsonx.Object, error) {
		// Keep the transaction values and lock tracker, but never let its
		// uninterruptible SQL view admit file publication after owner shutdown.
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return nil, err
		}
		for _, value := range catalog.GetList("rule_library") {
			current, ok := value.(*jsonx.Object)
			if !ok || current.GetString("id") != text(s.item, "id") {
				continue
			}
			item, err := native(current)
			if err != nil {
				return nil, err
			}
			if different(item, s.item, declarationFields) {
				return nil, errStaleCacheStage
			}
			if err = s.publish(tx); err != nil {
				return nil, err
			}
			// Serialize publication with every repository editor, but never
			// commit this unchanged candidate or advance its revision/hooks.
			return nil, errCachePublished
		}
		return nil, errStaleCacheStage
	})
	if errors.Is(err, errCachePublished) {
		return nil
	}
	return err
}

var (
	errStaleCacheStage = errors.New("rule declaration changed before cache publication")
	errCachePublished  = errors.New("rule cache published without document commit")
)

func (s *stagedCache) publish(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.replace(ctx, s.temp, s.path); err != nil {
		if ctx.Err() == nil {
			_ = os.Remove(s.path)
		}
		if ctx.Err() == nil {
			_ = os.Remove(s.temp)
		}
		return fmt.Errorf("replace rule cache: %w", err)
	}
	// Persist the replacement before clearing its durable invalidation receipt.
	if err := syncDirectory(ctx, filepath.Dir(s.path)); err != nil {
		return fmt.Errorf("persist rule cache replacement: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(s.marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear rule cache invalidation: %w", err)
	}
	return syncDirectory(ctx, filepath.Dir(s.path))
}

func syncDirectory(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	err = ctx.Err()
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func replaceFile(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(from, to)
}
