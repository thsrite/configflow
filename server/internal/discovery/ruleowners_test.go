package discovery

import (
	"configflow/internal/model"
	"configflow/internal/rules"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStableMatcherPathNeverLoadsConfiguration(t *testing.T) {
	directory := t.TempDir()
	rule := model.RuleConfig{ID: "r"}
	filename := filepath.Join(directory, rules.CacheFilename(rule))
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("valid"), 0600); err != nil {
		t.Fatal(err)
	}
	read := func(context.Context, func(*model.Document) error) error {
		t.Fatal("stable read loaded entire config")
		return nil
	}
	if actual := matcherCachePath(context.Background(), rule, nil, directory, read); actual != filename {
		t.Fatal("stable path", actual)
	}
	if err := os.WriteFile(filename+".invalid", []byte("pending"), 0600); err != nil {
		t.Fatal(err)
	}
	blocked := ""
	if actual := matcherCachePath(context.Background(), rule, nil, directory, read); actual != blocked {
		t.Fatal("receipt was bypassed", actual)
	}
	if err := os.Remove(filename + ".invalid"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, "missing"), filename+".invalid"); err != nil {
		t.Fatal(err)
	}
	if actual := matcherCachePath(context.Background(), rule, nil, directory, read); actual != blocked {
		t.Fatal("dangling receipt was bypassed", actual)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if actual := matcherCachePath(ctx, rule, nil, directory, read); actual != blocked {
		t.Fatal("canceled path", actual)
	}
}
func TestMatcherLegacyPathUsesOnlyCurrentBorrowedDeclaration(t *testing.T) {
	directory := t.TempDir()
	rule := model.RuleConfig{ID: "missing"}
	failure := errors.New("synthetic read failure")
	calls := 0
	read := func(ctx context.Context, visit func(*model.Document) error) error { calls++; return failure }
	actual := matcherCachePath(context.Background(), rule, nil, directory, read)
	if calls != 1 || actual != "" {
		t.Fatal("legacy read failure did not fail closed", calls, actual)
	}
}
