package ippresence

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
)

type generationReadState struct {
	Document          string
	Revision, Commits int64
	Files             map[string]string
}

func generationState(t *testing.T, ctx context.Context, r *repo.Repository) generationReadState {
	t.Helper()
	var data []byte
	if err := r.View(ctx, func(doc *model.Document) error {
		var err error
		data, err = model.Marshal(doc)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	revision, err := r.ConfigRevision(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	state := generationReadState{string(data), revision, r.CommitCount(), map[string]string{}}
	err = filepath.WalkDir(r.DataDir(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == ".config.lock" || strings.HasPrefix(entry.Name(), "configflow.sqlite3") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var content []byte
		if !entry.IsDir() {
			content, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		relative, err := filepath.Rel(r.DataDir(), path)
		if err != nil {
			return err
		}
		state.Files[relative] = fmt.Sprintf("%v:%d:%s", info.Mode(), info.ModTime().UnixNano(), content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestGeneratePeeksLegacyCacheAfterRuntimeShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("shutdown=%v", shutdown), func(t *testing.T) {
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx := repo.WithRepository(context.Background(), r)
			raw := []byte(`{"rule_configs":[{"id":"policy","name":"Synthetic","itemType":"ruleset","url":"https://rules.example.com/list.txt","source_type":"url","format":"text","library_rule_id":"r1","behavior":"classical"}],"rule_library":[{"id":"r1","name":"Synthetic","source_type":"url","url":"https://rules.example.com/list.txt","format":"text","behavior":"classical"}],"system_config":{"rule_proxy_token":"synthetic-capability"},"oxidns":{"proxy_rulesets":["policy"],"local_dns":"udp://192.0.2.53","remote_dns":"udp://192.0.2.54"}}`)
			var fixture struct {
				Library []model.RuleLibraryItem `json:"rule_library"`
			}
			if err := json.Unmarshal(raw, &fixture); err != nil {
				t.Fatal(err)
			}
			if err := r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error { shared.RuleLibrary = fixture.Library; return nil }); err != nil {
				t.Fatal(err)
			}
			root, err := r.SharedRulesDir()
			if err != nil {
				t.Fatal(err)
			}
			old := filepath.Join(root, "by-id", fmt.Sprintf("%x.list", sha256.Sum256([]byte("r1"))))
			if err := os.MkdirAll(filepath.Dir(old), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(old, []byte("DOMAIN-SUFFIX,example.com\n"), 0600); err != nil {
				t.Fatal(err)
			}
			stamp := time.Unix(1700000000, 123)
			if err := os.Chtimes(old, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			native := filepath.Join(root, rules.CacheFilename(rules.LibraryRule(fixture.Library[0])))
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(ctx)
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, disconnect := context.WithCancel(runtime.BindRequest(ctx))
			disconnect()
			if shutdown {
				runtime.Shutdown(context.Background())
				if lifecycle.Detach(request).Err() == nil {
					t.Fatal("not bound to stopped Runtime")
				}
			}
			// Use the production Wire hook, not a test-selected read implementation.
			restore := oxidns.SetNativeClassicalIPLines(nil)
			t.Cleanup(restore)
			repo.SetRepository(r)
			t.Cleanup(func() { repo.SetRepository(nil) })
			Wire()
			profile, err := oxidns.ProfileFromJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			ClearMemo()
			before := generationState(t, ctx, r)
			template, err := oxidns.Generate(profile, "http://panel.example.com")
			if err != nil {
				t.Fatal(err)
			}
			yamlx.Set(profile.Settings(), "config_mode", yamlx.Str("native"))
			yamlx.Set(profile.Settings(), "native_config", yamlx.Str(template))
			ClearMemo()
			text, err := oxidns.Generate(profile, "http://panel.example.com")
			if err != nil || text == "" {
				t.Fatalf("generation lost offline cache: %v", err)
			}
			if !reflect.DeepEqual(before, generationState(t, ctx, r)) {
				t.Fatal("pure generation changed DB/revision/receipt/files/mtime")
			}
			if _, err := os.Stat(native); !os.IsNotExist(err) {
				t.Fatalf("generation migrated a legacy cache: %v", err)
			}
		})
	}
}
