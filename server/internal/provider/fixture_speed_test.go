package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
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
	ctx := context.Background()
	before, err := clean.ExportAllJSON(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	dirty := factory(t)
	dirtyCtx := context.Background()
	if _, err := dirty.WriteSharedJSON(dirtyCtx, "subscribes/leaked.json", map[string]any{"value": "dirty"}); err != nil {
		t.Fatal(err)
	}
	if err := dirty.UpdateShared(dirtyCtx, func(_ context.Context, shared *model.Shared) error {
		shared.RuleLibrary = []model.RuleLibraryItem{testjson.Decode[model.RuleLibraryItem](t, map[string]any{
			"id": "leaked", "name": "Leaked", "content": "DOMAIN,leaked.example",
		})}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*repo.Repository{clean, factory(t)} {
		currentCtx := context.Background()
		after, err := r.ExportAllJSON(currentCtx, false)
		if err != nil {
			t.Fatal(err)
		}
		if !testjson.Equal(before, after) {
			t.Fatal("database rows/revisions leaked from another corpus case")
		}
		if _, err := os.Stat(filepath.Join(r.DataDir(), "shared/subscribes/leaked.json")); !os.IsNotExist(err) {
			t.Fatal("cache file leaked from another corpus case")
		}
	}
}
