package rulefetch

import (
	"bytes"
	"configflow/internal/fsx"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

// CacheEditLocks serializes rule-library publication and cleanup before the
// repository lock. All DB/cache writers share these identity leases.
var CacheEditLocks = lockx.NewKeyed[string](lockx.LevelDevice, "rule-library-edit")

// LockCacheEdits leases sorted, distinct library identities before a DB edit.
func LockCacheEdits(ctx context.Context, r *repo.Repository, ids ...string) (context.Context, func(), error) {
	if r == nil {
		return ctx, nil, errNilRepository
	}
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	paths := []string{}
	for i, id := range ids {
		if i > 0 && id == ids[i-1] {
			continue
		}
		kind := "content"
		path, err := r.SharedPath("rules/" + rules.CacheFilename(model.RuleConfig{ID: id, SourceType: &kind}))
		if err != nil {
			return ctx, nil, err
		}
		paths = append(paths, path)
	}
	if len(paths) == 1 {
		held, release, err := CacheEditLocks.Lock(ctx, paths[0], 5*time.Minute)
		if err != nil {
			return ctx, nil, err
		}
		if err := held.Err(); err != nil {
			release()
			return ctx, nil, err
		}
		return held, release, nil
	}
	waiting, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		if err := waiting.Err(); err != nil {
			return ctx, nil, err
		}
		held := ctx
		releases := []func(){}
		release := func() {
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
		}
		blocked := ""
		for _, path := range paths {
			next, unlock, ok := CacheEditLocks.TryLock(held, path)
			if !ok {
				blocked = path
				break
			}
			held = next
			releases = append(releases, unlock)
		}
		if blocked == "" {
			if err := ctx.Err(); err != nil {
				release()
				return ctx, nil, err
			}
			return held, release, nil
		}
		// Never block holding another L2 lease. Retry the set after the busy
		// identity becomes available; TryLock acquisitions cannot deadlock.
		release()
		_, unlock, err := CacheEditLocks.Lock(waiting, blocked, 5*time.Minute)
		if err != nil {
			return ctx, nil, err
		}
		unlock()
	}
}

// CachePublication stages bytes and a durable invalidation receipt before a DB
// commit. Keep its identity lease until Commit or failure cleanup completes.
type CachePublication struct {
	repo               *repo.Repository
	path, temp, marker string
	item               model.RuleLibraryItem
	baseline           model.RuleLibraryItem
	wasInvalid         bool
	replace            func(context.Context, string, string) error
}

// StageCache persists prepared bytes and invalidation before the caller commits
// the declaration. Call it inside the DB transaction with the owner lifetime.
func StageCache(ctx context.Context, r *repo.Repository, item model.RuleLibraryItem, content []byte, replace func(context.Context, string, string) error) (*CachePublication, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errNilRepository
	}
	catalog, err := r.Shared(ctx)
	if err != nil {
		return nil, err
	}
	var baseline model.RuleLibraryItem
	for _, current := range catalog.RuleLibrary {
		if current.ID == item.ID {
			baseline = current
			break
		}
	}
	path, err := r.SharedPath("rules/" + rules.CacheFilename(rules.LibraryRule(item)))
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
	stage := &CachePublication{repo: r, path: path, temp: file.Name(), marker: path + ".invalid", item: item, baseline: baseline, replace: replace}
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
		stage.Abort(ctx)
		return nil, fmt.Errorf("stage rule cache: %w", err)
	}
	// A durable invalidation receipt prevents the old stable or legacy cache from
	// being served after a committed edit, even when rename and removal fail.
	if err = ctx.Err(); err != nil {
		stage.Abort(ctx)
		return nil, err
	}
	marker, err := os.OpenFile(stage.marker, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		stage.Abort(ctx)
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
		stage.Abort(ctx)
		return nil, err
	}
	if err = syncDirectory(ctx, filepath.Dir(path)); err != nil {
		stage.Abort(ctx)
		return nil, err
	}
	return stage, nil
}

// Abort clears a rejected stage only if the original declaration still owns it.
func (s *CachePublication) Abort(ctx context.Context) {
	if s == nil || ctx.Err() != nil {
		return
	}
	_ = os.Remove(s.temp)
	if s.wasInvalid || ctx.Err() != nil {
		return
	}
	r := s.repo
	if r == nil {
		return
	}
	clear := func(tx context.Context, catalog *model.Shared) {
		for _, item := range catalog.RuleLibrary {
			if item.ID == s.item.ID {
				if sameCacheDeclaration(item, s.baseline) && tx.Err() == nil {
					_ = os.Remove(s.marker)
				}
				return
			}
		}
	}
	if r.InTransaction(ctx) {
		if catalog, err := r.Shared(ctx); err == nil {
			clear(ctx, catalog)
		}
		return
	}
	_ = r.UpdateShared(ctx, func(tx context.Context, catalog *model.Shared) error {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		clear(tx, catalog)
		return errCachePublished
	})
}

// FailClosed is used when the repository cannot establish a failed commit's
// outcome. Keep the durable receipt across restarts; readers use DB/inline.
func (s *CachePublication) FailClosed(ctx context.Context) {
	if s == nil || ctx.Err() != nil {
		return
	}
	_ = os.Remove(s.temp)
	if ctx.Err() == nil {
		_ = os.Remove(s.path)
	}
}

// Commit publishes only while the committed declaration still matches the stage.
func (s *CachePublication) Commit(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r := s.repo
	if r == nil {
		return errNilRepository
	}
	err := r.UpdateShared(ctx, func(tx context.Context, catalog *model.Shared) error {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if err := tx.Err(); err != nil {
			return err
		}
		for _, item := range catalog.RuleLibrary {
			if item.ID != s.item.ID {
				continue
			}
			if !sameCacheDeclaration(item, s.item) {
				return ErrStaleCacheStage
			}
			if err := s.publish(tx); err != nil {
				return err
			}
			// Abort the unchanged repository candidate; do not advance revision/hooks.
			return errCachePublished
		}
		return ErrStaleCacheStage
	})
	if errors.Is(err, errCachePublished) {
		r.NotifyRuleCacheChanged()
		return nil
	}
	return err
}

var (
	// ErrStaleCacheStage rejects a stage superseded by a different declaration.
	ErrStaleCacheStage = errors.New("rule declaration changed before cache publication")
	errCachePublished  = errors.New("rule cache published without document commit")
)

func (s *CachePublication) publish(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Capture only the current primary, under the declaration guard. Reading
	// failure must not block publication or replace a previously valid backup.
	previous, previousErr := os.ReadFile(s.path)
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
	// Rename is the publication point. Finish its durability, best-effort
	// backup and receipt removal even if shutdown starts after this point.
	ctx = ctxx.Uninterruptible(ctx)
	// Persist the replacement before clearing its durable invalidation receipt.
	if err := syncDirectory(ctx, filepath.Dir(s.path)); err != nil {
		return fmt.Errorf("persist rule cache replacement: %w", err)
	}
	if previousErr == nil {
		s.refreshBackup(ctx, previous)
	}
	if err := os.Remove(s.marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear rule cache invalidation: %w", err)
	}
	return syncDirectory(ctx, filepath.Dir(s.path))
}

// refreshBackup retains the previous primary after a successful publication.
// A failed refresh leaves the old backup intact and cannot fail the main write.
func (s *CachePublication) refreshBackup(ctx context.Context, previous []byte) {
	pattern := ".rule-edit-" + filepath.Base(s.path) + "-backup-*.tmp"
	_ = fsx.WriteFile(ctx, s.path+".bak", previous, fsx.Options{Pattern: pattern})
}

func syncDirectory(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fsx.SyncDir(path)
}

func replaceFile(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(from, to)
}

// Path returns the stable cache path of this publication.
func (s *CachePublication) Path() string { return s.path }

// TempPath returns the private staged file path.
func (s *CachePublication) TempPath() string { return s.temp }

func sameCacheDeclaration(a, b model.RuleLibraryItem) bool {
	left, e1 := json.Marshal(a)
	right, e2 := json.Marshal(b)
	if e1 != nil || e2 != nil {
		return false
	}
	var x, y map[string]json.RawMessage
	if json.Unmarshal(left, &x) != nil || json.Unmarshal(right, &y) != nil {
		return false
	}
	for _, key := range []string{"url", "source_type", "behavior", "format", "content"} {
		v, ok := x[key]
		if !ok {
			v = json.RawMessage("null")
		}
		w, ok := y[key]
		if !ok {
			w = json.RawMessage("null")
		}
		var cv, cw bytes.Buffer
		if json.Compact(&cv, v) != nil || json.Compact(&cw, w) != nil || !bytes.Equal(cv.Bytes(), cw.Bytes()) {
			return false
		}
	}
	return true
}

// Discard removes private staged bytes without clearing invalidation. Use it
// after an uncertain DB outcome: the next publication repairs the cache.
func (s *CachePublication) Discard(ctx context.Context) {
	if s != nil && ctx.Err() == nil {
		_ = os.Remove(s.temp)
	}
}
