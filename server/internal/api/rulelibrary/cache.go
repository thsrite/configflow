package rulelibrary

// C33 and other inline editors share the same durable publication owner.
import (
	"context"
	"os"

	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

var edits = rulefetch.CacheEditLocks
var errStaleCacheStage = rulefetch.ErrStaleCacheStage

func editLease(ctx context.Context, id string) (context.Context, func(), error) {
	return rulefetch.LockCacheEdits(ctx, id)
}

type stagedCache struct {
	publication        *rulefetch.CachePublication
	path, temp, marker string
}

func stageCache(ctx context.Context, item model.RuleLibraryItem, content []byte, replace func(context.Context, string, string) error) (*stagedCache, error) {
	publication, err := rulefetch.StageCache(ctx, item, content, replace)
	if err != nil {
		return nil, err
	}
	return &stagedCache{publication, publication.Path(), publication.TempPath(), publication.Path() + ".invalid"}, nil
}
func (s *stagedCache) abort(ctx context.Context) {
	if s != nil {
		s.publication.Abort(ctx)
	}
}
func (s *stagedCache) failClosed(ctx context.Context) {
	if s != nil {
		s.publication.FailClosed(ctx)
	}
}
func (s *stagedCache) commit(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.publication.Commit(ctx)
}
func replaceFile(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(from, to)
}
