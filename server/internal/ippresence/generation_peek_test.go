package ippresence_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/ippresence"
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
			ctx := context.Background()
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
			profile, err := oxidns.ProfileFromJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			ippresence.ClearMemo()
			before := generationState(t, ctx, r)
			template, err := oxidns.Generate(profile, "http://panel.example.com")
			if err != nil {
				t.Fatal(err)
			}
			yamlx.Set(profile.Settings(), "config_mode", yamlx.Str("native"))
			yamlx.Set(profile.Settings(), "native_config", yamlx.Str(template))
			ippresence.ClearMemo()
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

func TestClassicalCacheDoesNotChangeNativeRouting(t *testing.T) {
	const base = "https://panel.example.com"
	ctx := t.Context()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	raw := []byte(`{
		"rule_configs":[{"id":"classical","itemType":"ruleset","name":"Synthetic","url":"https://rules.example.com/list.txt","source_type":"url","format":"text","behavior":"classical","library_rule_id":"source"}],
		"rule_library":[{"id":"source","name":"Synthetic","source_type":"url","url":"https://rules.example.com/list.txt","format":"text","behavior":"classical"}],
		"system_config":{"rule_proxy_token":"synthetic-capability"},
		"oxidns":{"direct_rulesets":["classical"],"default_forward":"forward_local","local_dns":"udp://192.0.2.53","remote_dns":"udp://192.0.2.54"}
	}`)
	cfg, err := model.DecodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		shared.RuleLibrary = cfg.RuleLibrary
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	root, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "by-id", fmt.Sprintf("%x.list", sha256.Sum256([]byte("source"))))
	if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("DOMAIN-SUFFIX,example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	known, err := ippresence.PeekTypedClassicalIPLines(ctx, r, cfg.Profile.RuleConfigs[0], cfg.RuleLibrary)
	if err != nil || known == nil || *known {
		t.Fatalf("fixture must prove absence of IP lines: %v / %v", known, err)
	}
	before := generationState(t, ctx, r)

	for _, shape := range []string{"domain", "domain+ip", "ip"} {
		t.Run(shape, func(t *testing.T) {
			profile, err := oxidns.ProfileFromJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			template, err := oxidns.Generate(profile, base)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := yamlx.Parse([]byte(template))
			if err != nil {
				t.Fatal(err)
			}
			const ipMatch = "resp_ip $rules_ip_1"
			if shape != "domain" {
				plugins := yamlx.Get(doc, "plugins")
				ipPath := oxidns.RulePath(oxidns.M("id", "classical"), "ip")
				plugins.Content = append(plugins.Content,
					oxidns.M("tag", "rules_ip_1", "type", "ip_set", "args", oxidns.M("files", oxidns.L(ipPath))))
				for _, plugin := range plugins.Content {
					if yamlx.Get(plugin, "tag").Value != "sequence_main" {
						continue
					}
					args := yamlx.Get(plugin, "args")
					entries := args.Content
					args.Content = nil
					for _, entry := range entries[:len(entries)-1] {
						match := yamlx.Get(entry, "matches")
						if shape == "ip" && match != nil && len(match.Content) == 1 &&
							strings.HasPrefix(match.Content[0].Value, "qname $rules_domain_") {
							continue
						}
						args.Content = append(args.Content, entry)
					}
					args.Content = append(args.Content, oxidns.M("matches", oxidns.L(ipMatch), "exec", "goto direct_ip"), entries[len(entries)-1])
					break
				}
			}
			native, err := yamlx.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			yamlx.Set(profile.Settings(), "config_mode", yamlx.Str("native"))
			yamlx.Set(profile.Settings(), "native_config", yamlx.Str(string(native)))
			render := func(repository *repo.Repository) (string, string, error) {
				profile.Repo = repository
				text, err := oxidns.Generate(profile, base)
				if err != nil {
					t.Fatal(err)
				}
				result, err := oxidns.ApplyRuleDNS(profile, profile, []oxidns.DNSAssignment{{RuleID: "classical", Choice: "proxy"}}, oxidns.RuleDNSApplyOptions{BaseURL: base})
				if err != nil {
					return text, "", err
				}
				if !result.Changed {
					t.Fatal("DNS assignment did not change settings")
				}
				return text, string(oxidns.ToJSON(result.Settings)), nil
			}
			uncachedYAML, uncachedEdit, uncachedErr := render(nil)
			ippresence.ClearMemo()
			cachedYAML, cachedEdit, cachedErr := render(r)
			if cachedYAML != uncachedYAML || cachedEdit != uncachedEdit {
				t.Fatal("cache presence changed generated YAML or replacement DNS settings bytes")
			}
			if shape == "ip" {
				if !errors.Is(cachedErr, oxidns.ErrRoutingLocked) || !errors.Is(uncachedErr, oxidns.ErrRoutingLocked) ||
					cachedErr.Error() != uncachedErr.Error() || !strings.Contains(cachedYAML, ipMatch) {
					t.Fatalf("IP-only draft must stay locked and keep its IP match: %v / %v", cachedErr, uncachedErr)
				}
			} else if cachedErr != nil || uncachedErr != nil || strings.Contains(cachedYAML, ipMatch) {
				t.Fatalf("managed draft must remain editable and deliver domain-only routing: %v / %v", cachedErr, uncachedErr)
			}
		})
	}
	if !reflect.DeepEqual(before, generationState(t, ctx, r)) {
		t.Fatal("routing and generation changed DB/revision/receipt/files/mtime")
	}
}
