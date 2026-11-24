package server

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/repo"
)

// The export sanitizer and router job lock live on repo.Options since W11 (no
// global hooks). server.New refuses a repository opened without them, so the
// production assembly cannot silently lose desensitized exports.
func TestNewRequiresServerRepositoryOptions(t *testing.T) {
	opts := RepoOptions()
	if opts.Export == nil || opts.RouterJobLock == nil || opts.DefaultServerDomain == nil {
		t.Fatal("RepoOptions leaves a server collaborator unset")
	}
	bare, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bare.Close)
	if _, err := New(context.Background(), Options{Repo: bare}); !errors.Is(err, repo.ErrServerCollaboratorsMissing) {
		t.Fatalf("New(bare repository) err = %v, want ErrServerCollaboratorsMissing", err)
	}
	app := newTestApp(t)
	if err := app.Repo.RequireServerCollaborators(); err != nil {
		t.Fatal(err)
	}
}
