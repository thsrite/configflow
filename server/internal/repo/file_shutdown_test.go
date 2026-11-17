package repo_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
)

func TestO36cAtomicFileRuntimeBoundary(t *testing.T) {
	for _, boundary := range []string{"shutdown-after-stage", "disconnect-after-stage", "shutdown-after-primary"} {
		t.Run(boundary, func(t *testing.T) {
			r, runtime, base := commitRuntime(t)
			path := filepath.Join(r.DataDir(), "atomic.txt")
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".bak", []byte("older"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, disconnect := context.WithCancel(runtime.BindRequest(base))
			defer disconnect()
			stage := func(path string, data []byte) (string, error) {
				temp, err := repo.StageFileForTest(path, data)
				if err == nil {
					switch boundary {
					case "shutdown-after-stage":
						runtime.Shutdown(context.Background())
					case "disconnect-after-stage":
						disconnect()
					}
				}
				return temp, err
			}
			replace := func(from, to string) error {
				err := os.Rename(from, to)
				if err == nil && to == path && boundary == "shutdown-after-primary" {
					runtime.Shutdown(context.Background())
				}
				return err
			}
			err := repo.WriteAtomicForTest(lifecycle.Detach(ctx), path, []byte("new"), stage, replace)
			main, mainErr := os.ReadFile(path)
			backup, backupErr := os.ReadFile(path + ".bak")
			if mainErr != nil || backupErr != nil {
				t.Fatal(mainErr, backupErr)
			}
			if boundary == "shutdown-after-stage" {
				if !errors.Is(err, context.Canceled) || string(main) != "old" || string(backup) != "older" {
					t.Fatalf("cancelled staged file committed: err=%v main=%q backup=%q", err, main, backup)
				}
			} else if err != nil || string(main) != "new" || string(backup) != "old" {
				t.Fatalf("atomic batch stopped after commit: err=%v main=%q backup=%q", err, main, backup)
			}
			for _, pattern := range []string{".atomic.txt.*.tmp", ".atomic.txt.bak.*.tmp"} {
				matches, err := filepath.Glob(filepath.Join(r.DataDir(), pattern))
				if err != nil || len(matches) != 0 {
					t.Fatal("temporary files remain", matches, err)
				}
			}
		})
	}
}

func TestO36cRuleTouchLockRuntimeBoundary(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			r, runtime, base := commitRuntime(t)
			rule := jsonx.Obj("id", "o3-rule", "name", "O3", "source_type", "url",
				"url", "https://rules.example.com/o3.txt", "behavior", "domain", "format", "text", "enabled", true)
			content := []byte("example.com\n")
			for range 2 {
				if _, err := rulefetch.WriteValidatedRuleCache(base, rule, content, nil); err != nil {
					t.Fatal(err)
				}
			}
			path, err := r.SharedPath(filepath.ToSlash(filepath.Join("rules", rules.RuleCacheFilename(rule))))
			if err != nil {
				t.Fatal(err)
			}
			old := time.Unix(1700000000, 0)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			_, release, err := repo.AcquireFileForTest(base, path)
			if err != nil {
				t.Fatal(err)
			}
			held := true
			defer func() {
				if held {
					release()
				}
			}()
			ctx, disconnect := context.WithCancel(runtime.BindRequest(base))
			defer disconnect()
			done := make(chan error, 1)
			go func() {
				_, err := rulefetch.WriteValidatedRuleCache(ctx, rule, content, nil)
				done <- err
			}()
			escaped := false
			select {
			case err := <-done:
				escaped = true
				t.Errorf("rule touch escaped held leaf lock: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				disconnect()
			}
			release()
			held = false
			if escaped {
				return
			}
			err = commitResult(t, done)
			after, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if !os.SameFile(before, after) {
				t.Fatal("unchanged refresh replaced the file instead of touching it")
			}
			if shutdown {
				if !errors.Is(err, context.Canceled) || !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("rule touch landed after shutdown: %v", err)
				}
			} else if err != nil || !after.ModTime().After(old) {
				t.Fatalf("disconnected refresh did not advance freshness: %v", err)
			}
		})
	}
}

func TestO36cRuleTouchAfterShutdown(t *testing.T) {
	r, runtime, base := commitRuntime(t)
	rule := jsonx.Obj("id", "closed-rule", "name", "Closed", "source_type", "url",
		"url", "https://rules.example.com/closed.txt", "behavior", "domain", "format", "text")
	for range 2 {
		if _, err := rulefetch.WriteValidatedRuleCache(base, rule, "example.com\n", nil); err != nil {
			t.Fatal(err)
		}
	}
	path, err := r.SharedPath(filepath.Join("rules", rules.RuleCacheFilename(rule)))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	ctx := runtime.BindRequest(base)
	runtime.Shutdown(context.Background())
	_, err = rulefetch.WriteValidatedRuleCache(ctx, rule, "example.com\n", nil)
	info, statErr := os.Stat(path)
	if !errors.Is(err, context.Canceled) || statErr != nil || !info.ModTime().Equal(old) {
		t.Fatalf("closed Runtime touched rule freshness: %v, %v", err, statErr)
	}
}

func TestO36cFileOwnerLockRuntimeBoundary(t *testing.T) {
	for _, kind := range []string{"shared-text", "shared-json", "profile-text", "profile-json", "generated"} {
		for _, shutdown := range []bool{true, false} {
			name := kind + "/disconnect"
			if shutdown {
				name = kind + "/shutdown"
			}
			t.Run(name, func(t *testing.T) {
				r, runtime, base := commitRuntime(t)
				var path string
				var err error
				var write func(context.Context) error
				switch kind {
				case "shared-text", "shared-json":
					path, err = r.SharedPath("owner.json")
					write = func(ctx context.Context) error {
						if kind == "shared-json" {
							_, err := r.WriteSharedJSON(ctx, "owner.json", jsonx.Obj("value", "new"))
							return err
						}
						_, err := r.WriteSharedText(ctx, "owner.json", []byte("new"))
						return err
					}
				case "profile-text", "profile-json":
					path, err = r.ProfilePath("default", "owner.json")
					write = func(ctx context.Context) error {
						if kind == "profile-json" {
							_, err := r.WriteProfileJSON(ctx, "default", "owner.json", jsonx.Obj("value", "new"))
							return err
						}
						_, err := r.WriteProfileText(ctx, "default", "owner.json", []byte("new"))
						return err
					}
				case "generated":
					var directory string
					directory, err = r.GeneratedDir("default")
					path = filepath.Join(directory, "config.yaml")
					write = func(ctx context.Context) error {
						_, err := r.WriteGenerated(ctx, "default", "config.yaml", []byte("new"))
						return err
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
				_, release, err := repo.AcquireFileForTest(base, path)
				if err != nil {
					t.Fatal(err)
				}
				held := true
				defer func() {
					if held {
						release()
					}
				}()
				ctx, disconnect := context.WithCancel(runtime.BindRequest(base))
				defer disconnect()
				done := make(chan error, 1)
				go func() { done <- write(ctx) }()
				select {
				case err := <-done:
					t.Fatalf("file owner escaped lock: %v", err)
				case <-time.After(50 * time.Millisecond):
				}
				if shutdown {
					runtime.Shutdown(context.Background())
				} else {
					disconnect()
				}
				release()
				held = false
				err = commitResult(t, done)
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if shutdown {
					if !errors.Is(err, context.Canceled) || string(data) != "old" {
						t.Fatalf("file owner committed after shutdown: %v", err)
					}
				} else if err != nil || string(data) == "old" {
					t.Fatalf("disconnected file owner did not finish: %v", err)
				}
			})
		}
	}
}
