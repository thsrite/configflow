package agents

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

type agentFixtureFile struct {
	name string
	mode fs.FileMode
	data []byte
}

// Cache the completed, checkpointed initial fixture, never a live Repository or
// connection. Every test gets private files and still opens the real repository.
var initialAgentFiles = sync.OnceValues(func() ([]agentFixtureFile, error) {
	dir, err := os.MkdirTemp("", "agent-initial-fixture-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if _, err := repo.OpenDefault(dir); err != nil {
		return nil, err
	}
	var files []agentFixtureFile
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		name, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") {
			return fmt.Errorf("initial repository is not checkpointed: %s", name)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := agentFixtureFile{name: name, mode: info.Mode()}
		if !d.IsDir() {
			entry.data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		files = append(files, entry)
		return nil
	})
	return files, err
})

func seededAgentDirectory(t *testing.T) string {
	t.Helper()
	files, err := initialAgentFiles()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, file := range files {
		path := filepath.Join(dir, file.name)
		if file.mode.IsDir() {
			err = os.MkdirAll(path, file.mode.Perm())
		} else {
			err = os.WriteFile(path, file.data, file.mode.Perm())
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSeededAgentRepositoriesAreIndependent(t *testing.T) {
	t.Parallel()
	first := openRepo(t, seededAgentDirectory(t))
	second := openRepo(t, seededAgentDirectory(t))
	ctx := context.Background()
	before, err := second.System(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.SystemConfig.ServerDomain = "https://changed.example"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changed, err := first.System(ctx)
	if err != nil || changed.SystemConfig.ServerDomain != "https://changed.example" {
		t.Fatal("fixture mutation did not persist", err)
	}
	// Reopen to check the private on-disk copy rather than a retained read cache.
	second = openRepo(t, second.DataDir())
	after, err := second.System(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if diff := diffJSONValues(t, before, after); diff != "" {
		t.Fatalf("initial fixture state leaked between repositories: %s", diff)
	}
}
