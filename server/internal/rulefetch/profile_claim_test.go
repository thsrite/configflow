package rulefetch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/store"
)

func TestStoredProfileLocalCacheOwnership(t *testing.T) {
	for _, state := range []string{"stored", "deleted", "captured only", "proposed", "same name replacement", "unresolved library", "legacy rule_sets"} {
		t.Run(state, func(t *testing.T) {
			ctx, r, rule, old, native := migrationFixture(t)
			rule.ItemType = "ruleset"
			policy := "DIRECT"
			rule.Policy = &policy
			if err := r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error { shared.RuleLibrary = nil; return nil }); err != nil {
				t.Fatal(err)
			}
			err := editStoredProfile(ctx, r, func(_ context.Context, p *model.Profile) error {
				if state == "captured only" {
					return nil
				}
				current := rule
				if state == "unresolved library" {
					current.LibraryRuleID = "deleted-library"
				}
				if state == "same name replacement" {
					current.ID = "replacement"
				}
				if state == "proposed" {
					address := "https://rules.example.com/current.yaml"
					current.URL = &address
				}
				if state == "legacy rule_sets" {
					data, err := model.Marshal([]model.RuleConfig{current})
					if err != nil {
						return err
					}
					return p.SetJSON("rule_sets", data)
				}
				p.RuleConfigs = []model.RuleConfig{current}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if state == "deleted" {
				if err := editStoredProfile(ctx, r, func(_ context.Context, p *model.Profile) error { p.RuleConfigs = nil; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			config := &SourceConfig{Rules: []model.RuleConfig{rule}, RuleSets: []model.RuleConfig{rule}}
			before, err := r.ConfigRevision(ctx, "default")
			if err != nil {
				t.Fatal(err)
			}
			data, err := PeekCacheWithFormat(ctx, rule, config, "yaml")
			valid := state == "stored" || state == "legacy rule_sets"
			if err != nil || valid && string(data) != "payload: [example.com]\n" || !valid && data != nil {
				t.Fatalf("profile ownership %s: %q %v", state, data, err)
			}
			if _, err := os.Stat(native); !os.IsNotExist(err) {
				t.Fatalf("peek migrated %s: %v", state, err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(ctx)
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, disconnect := context.WithCancel(runtime.BindRequest(ctx))
			disconnect()
			_, err = ReadCacheWithFormat(request, rule, config, "yaml")
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(native)
			if valid && (err != nil || string(got) != "payload: [example.com]\n") || !valid && !os.IsNotExist(err) {
				t.Fatalf("migration authorization %s: %q %v", state, got, err)
			}
			after, err := r.ConfigRevision(ctx, "default")
			if err != nil || after != before {
				t.Fatal("cache migration changed profile revision", err)
			}
			if _, err := os.Stat(old); err != nil {
				t.Fatal("legacy rollback bytes removed", err)
			}
		})
	}
}

func TestProfileCacheMigrationRechecksStoredOwner(t *testing.T) {
	ctx, r, rule, old, native := migrationFixture(t)
	rule.ItemType = "ruleset"
	policy := "DIRECT"
	rule.Policy = &policy
	if err := r.UpdateShared(ctx, func(_ context.Context, s *model.Shared) error { s.RuleLibrary = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := editStoredProfile(ctx, r, func(_ context.Context, p *model.Profile) error { p.RuleConfigs = []model.RuleConfig{rule}; return nil }); err != nil {
		t.Fatal(err)
	}
	data, err := PeekCacheWithFormat(ctx, rule, nil, "yaml")
	if err != nil || len(data) == 0 {
		t.Fatal("current profile cannot claim old bytes", err)
	}
	if err := editStoredProfile(ctx, r, func(_ context.Context, p *model.Profile) error { p.RuleConfigs = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := migrateCache(ctx, rule, data, old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(native); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted owner migrated after pre-read", err)
	}
}

func TestProfileCacheClaimsKeepReceiptAndPathGuards(t *testing.T) {
	for _, kind := range []string{"receipt", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			ctx, r, rule, old, native := migrationFixture(t)
			rule.ItemType = "ruleset"
			policy := "DIRECT"
			rule.Policy = &policy
			if err := r.UpdateShared(ctx, func(_ context.Context, s *model.Shared) error { s.RuleLibrary = nil; return nil }); err != nil {
				t.Fatal(err)
			}
			if err := editStoredProfile(ctx, r, func(_ context.Context, p *model.Profile) error { p.RuleConfigs = []model.RuleConfig{rule}; return nil }); err != nil {
				t.Fatal(err)
			}
			if kind == "receipt" {
				if err := os.WriteFile(old+".invalid", []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				outside := filepath.Join(t.TempDir(), "outside.list")
				if err := os.Rename(old, outside); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, old); err != nil {
					t.Fatal(err)
				}
			}
			for _, read := range []func(context.Context, model.RuleConfig, *SourceConfig, string) ([]byte, error){PeekCacheWithFormat, ReadCacheWithFormat} {
				data, err := read(ctx, rule, nil, "yaml")
				if err != nil || data != nil {
					t.Fatalf("profile %s escaped guard: %q %v", kind, data, err)
				}
			}
			if _, err := os.Stat(native); !os.IsNotExist(err) {
				t.Fatal("invalid profile cache migrated", err)
			}
		})
	}
}

// Current write APIs reject newly created profile-local sources. Persist an old
// SQLite row directly through the native store API, just as an imported old DB
// would contain it; do not weaken the production write validator for the test.
func editStoredProfile(ctx context.Context, r *repo.Repository, edit func(context.Context, *model.Profile) error) error {
	target := store.New(filepath.Join(r.DataDir(), store.Filename), repo.LockTimeout)
	base, err := target.Load()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(base.Document)
	if err != nil {
		return err
	}
	var document store.Document
	if err := json.Unmarshal(encoded, &document); err != nil {
		return err
	}
	for index, rows := range document.Profiles {
		if rows.ID != "default" {
			continue
		}
		var fields model.Fields
		for _, row := range rows.Fields {
			if err := fields.Set(row.Name, row.Value); err != nil {
				return err
			}
		}
		encoded, err := fields.MarshalJSON()
		if err != nil {
			return err
		}
		var profile model.Profile
		if err := json.Unmarshal(encoded, &profile); err != nil {
			return err
		}
		if err := edit(ctx, &profile); err != nil {
			return err
		}
		encoded, err = model.Marshal(profile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(encoded, &fields); err != nil {
			return err
		}
		document.Profiles[index].Fields = nil
		for name, value := range fields.All() {
			document.Profiles[index].Fields = append(document.Profiles[index].Fields, store.Field{Name: name, Value: value})
		}
		_, _, err = target.CommitContext(ctx, base, &document, nil)
		return err
	}
	return errors.New("fixture profile missing")
}
