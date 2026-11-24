package rulefetch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

// The old filename is frozen from real Python (the same case appears in the
// untouched TestRuleCacheFilenameNamespaces), not computed by the migration.
const oldRemoteName = "by-source/4663082af6b370293ab4dcaac0c518c70514799bcc123b97e9874820611ca277.list"

func migrationFixture(t *testing.T) (context.Context, *repo.Repository, model.RuleConfig, string, string) {
	t.Helper()
	rule := model.RuleConfig{
		ID:         "r1",
		Name:       "Synthetic",
		SourceType: strp("url"),
		URL:        strp("https://x.test/a.yaml"),
		Behavior:   strp("domain"),
		Format:     strp("yaml"),
	}
	ctx, r := cacheRepo(t, &rule)
	dir, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, oldRemoteName)
	if err := os.MkdirAll(filepath.Dir(old), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("payload: [example.com]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return ctx, r, rule, old, cachePath(t, r, rule)
}

func TestNativeCacheMigrationPreservesOfflineFallback(t *testing.T) {
	ctx, r, rule, old, native := migrationFixture(t)
	stamp := time.Unix(1700000000, 123456789)
	if err := os.Chtimes(old, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := r.Shared(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := model.Marshal(before)
	offline := errors.New("synthetic upstream unavailable")
	content, refreshErr, err := RefreshRule(ctx, r, rule, nil, CacheOptions{
		AllowStale: true,
		Fetch:      func(context.Context, string) (Content, error) { return Content{}, offline },
	})
	if err != nil || !errors.Is(refreshErr, offline) || string(content) != "payload: [example.com]\n" {
		t.Fatalf("offline fallback: %s %v %v", content, refreshErr, err)
	}
	for _, path := range []string{old, native} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != string(content) {
			t.Fatalf("migration file: %v", err)
		}
	}
	after, err := r.Shared(ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterJSON, _ := model.Marshal(after)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatal("migration committed DB/revision changes")
	}
	info, err := os.Stat(native)
	if err != nil || !info.ModTime().Equal(stamp) {
		t.Fatal("migration extended source freshness", err)
	}
	// The native cache now works after the old file disappears.
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	cached, err := ReadCache(ctx, r, rule, nil)
	if err != nil || string(cached) != string(content) {
		t.Fatal("native read after migration", err)
	}
}

func TestNativeCacheMigrationRejectsBadBytesDeclarationsAndReceipts(t *testing.T) {
	for _, kind := range []string{"bad-content", "changed-url", "changed-behavior", "native-receipt", "old-receipt"} {
		t.Run(kind, func(t *testing.T) {
			ctx, r, rule, old, native := migrationFixture(t)
			switch kind {
			case "bad-content":
				if err := os.WriteFile(old, []byte("payload: ['DOMAIN,example.com']\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-url":
				rule.URL = strp("https://example.com/new.yaml")
			case "changed-behavior":
				rule.Behavior = strp("ipcidr")
			case "native-receipt":
				if err := os.MkdirAll(filepath.Dir(native), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(native+".invalid", []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			case "old-receipt":
				if err := os.WriteFile(old+".invalid", []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			content, err := ReadCache(ctx, r, rule, nil)
			if err != nil || content != nil {
				t.Fatalf("refusal %s: bytes=%d err=%v", kind, len(content), err)
			}
			if _, err := os.Stat(native); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid bytes migrated")
			}
			if _, err := r.Shared(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCacheWriteUsesOriginatingRuntimeAfterDisconnectAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			ctx, r, rule, _, native := migrationFixture(t)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, disconnect := context.WithCancel(runtime.BindRequest(ctx))
			disconnect()
			if shutdown {
				runtime.Shutdown(context.Background())
			}
			_, err := WriteCache(request, r, rule, Content{Data: []byte("payload: [changed.example]\n")}, nil)
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("shutdown error: %v", err)
				}
				if _, err := os.Stat(native); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("shutdown wrote cache")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(native)
				if err != nil || string(data) != "payload: [changed.example]\n" {
					t.Fatal("disconnect stopped admitted write", err)
				}
			}
			// A shutdown generation must not silently restart on the next write.
			_ = r
		})
	}
}

func TestDownloadRetryWaitKeepsRequestCancellation(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	request, cancel := context.WithCancel(runtime.BindRequest(context.Background()))
	cancel()
	if err := (&Client{}).wait(request, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("retry ignored cancellation: %v", err)
	}
}

func TestLegacyDigestIsReadOnlyMigrationMetadata(t *testing.T) {
	ctx, r, rule, old, native := migrationFixture(t)
	name, ok := rules.LegacyCacheFilename(rule)
	if !ok || filepath.Base(old) != filepath.Base(name) || filepath.Base(old) == filepath.Base(native) {
		t.Fatal("historical address not recognized")
	}
	if _, err := ReadCache(ctx, r, rule, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshCommitChecksRuntimeAfterDownload(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			ctx, r, rule, _, native := migrationFixture(t)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, disconnect := context.WithCancel(runtime.BindRequest(ctx))
			downloaded := make(chan struct{})
			release := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				_, _, err := RefreshRule(request, r, rule, nil, CacheOptions{Fetch: func(work context.Context, _ string) (Content, error) {
					if work.Err() != nil {
						t.Error("not admitted")
					}
					close(downloaded)
					<-release
					return Content{Data: []byte("payload: [fresh.example]\n")}, nil
				}})
				done <- err
			}()
			<-downloaded
			disconnect()
			if shutdown {
				runtime.Shutdown(context.Background())
			}
			close(release)
			select {
			case err := <-done:
				if shutdown {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("shutdown error %v", err)
					}
					if _, err := os.Stat(native); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("shutdown crossed cache commit")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					data, err := os.ReadFile(native)
					if err != nil || string(data) != "payload: [fresh.example]\n" {
						t.Fatal("disconnect cancelled owner", err)
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("owner stranded")
			}
		})
	}
}

func FuzzTypedRuleContent(f *testing.F) {
	for _, source := range []string{"payload: [example.com]", "example.com", "loop: &l [*l]", "payload: [false]", "payload: [\xff]"} {
		f.Add(source, "domain", "yaml")
	}
	f.Fuzz(func(t *testing.T, source, behavior, format string) {
		if len(source) > 65536 || len(behavior) > 128 || len(format) > 128 {
			return
		}
		_, _ = Inspect(source, behavior, format)
		rule := model.RuleConfig{Behavior: &behavior, Format: &format}
		_ = ValidateRemote(Content{Data: []byte(source)}, rule, nil, format)
	})
}

func TestLegacyCacheMigrationUsesHeldReadSnapshot(t *testing.T) {
	ctx, r, rule, _, native := migrationFixture(t)
	err := r.WithReadSnapshot(ctx, func(held context.Context) error {
		data, err := ReadCacheWithFormat(held, r, rule, nil, "yaml")
		if err != nil {
			return err
		}
		if string(data) != "payload: [example.com]\n" {
			t.Fatal("lost validated old bytes")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(native); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only snapshot wrote cache")
	}
	if _, err := ReadCacheWithFormat(ctx, r, rule, nil, "yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(native); err != nil {
		t.Fatal("writable read did not migrate cache", err)
	}
}

func TestKnownFormatNeverDecodesInferenceLibrary(t *testing.T) {
	rule := remoteSource()
	ctx, r := cacheRepo(t, &rule)
	if _, err := WriteCache(ctx, r, rule, Content{Data: []byte("example.com\n")}, nil); err != nil {
		t.Fatal(err)
	}
	// A caller-known format with an existing stable cache must never touch the
	// lazy library projection.
	config := &SourceConfig{cacheLibrary: func() []model.RuleLibraryItem {
		t.Fatal("known format visited library")
		return nil
	}}
	data, err := ReadCacheWithFormat(ctx, r, rule, config, "text")
	if err != nil || string(data) != "example.com\n" {
		t.Fatal("known format visited library", err)
	}
	_ = r
}
