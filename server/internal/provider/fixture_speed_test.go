package provider

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// corpusRepositoryFactory pays initial schema/config creation once, then copies
// the closed-on-return store files into a fresh directory for each row. Each row
// still opens a real repository and exercises all original durable writes and
// filesystem assertions; no writable database, cache or repository is shared.
func corpusRepositoryFactory(t *testing.T) func(*testing.T) *repo.Repository {
	t.Helper()
	seed := t.TempDir()
	if _, err := repo.Open(seed, repo.Options{}); err != nil {
		t.Fatal(err)
	}
	return func(t *testing.T) *repo.Repository {
		t.Helper()
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS(seed)); err != nil {
			t.Fatal(err)
		}
		r, err := repo.Open(root, repo.Options{})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
}

func TestCorpusRepositoryCopiesCannotLeakState(t *testing.T) {
	factory := corpusRepositoryFactory(t)
	clean := factory(t)
	ctx := repo.WithRepository(context.Background(), clean)
	before, err := clean.ExportAll(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	dirty := factory(t)
	dirtyCtx := repo.WithRepository(context.Background(), dirty)
	if _, err := dirty.WriteSharedJSON(dirtyCtx, "subscribes/leaked.json", jsonx.Obj("value", "dirty")); err != nil {
		t.Fatal(err)
	}
	if _, err := dirty.UpdateSharedTransaction(dirtyCtx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("rule_library", []any{jsonx.Obj("id", "leaked", "name", "Leaked", "content", "DOMAIN,leaked.example")})
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*repo.Repository{clean, factory(t)} {
		currentCtx := repo.WithRepository(context.Background(), r)
		after, err := r.ExportAll(currentCtx, false)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("database rows/revisions leaked from another corpus case")
		}
		if _, err := os.Stat(filepath.Join(r.DataDir(), "shared/subscribes/leaked.json")); !os.IsNotExist(err) {
			t.Fatal("cache file leaked from another corpus case")
		}
	}
}
