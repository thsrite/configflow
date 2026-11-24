package domaindiscovery_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"configflow/internal/api/domaindiscovery"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
)

func TestInitTargetsDeduplicateInFirstOccurrenceOrder(t *testing.T) {
	for _, tc := range []struct {
		name        string
		input, want []string
	}{
		{"direct", []string{"direct", "direct"}, []string{"direct"}},
		{"proxy-first", []string{"proxy", "direct", "proxy", "direct"}, []string{"proxy", "direct"}},
		{"direct-first", []string{"direct", "proxy", "direct", "proxy"}, []string{"direct", "proxy"}},
		{"default", []string{}, []string{"direct", "proxy"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httpxtest.New(t, httpxtest.Options{})
			domaindiscovery.NewHandler(s.Repo, nil).Register(s.App.Router)
			_, err := s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, p *model.Profile) error {
				if err := p.SetJSON("proxy_groups", json.RawMessage(`[{"id":"g","name":"PROXY","type":"select"}]`)); err != nil {
					return err
				}
				return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"fallback","itemType":"rule","rule_type":"MATCH","policy":"DIRECT"}]`))
			})
			if err != nil {
				t.Fatal(err)
			}
			var previous []string
			for round := 1; round <= 2; round++ {
				s.Post("/api/domain-discovery/rulesets/init", httpxtest.JSON(map[string]any{"targets": tc.input, "proxy_policy": "PROXY"})).WantStatus(200)
				shared, err := s.Repo.Shared(s.Context())
				if err != nil {
					t.Fatal(err)
				}
				libraries := shared.RuleLibrary
				profile, err := s.Repo.Profile(s.Context(), "default")
				if err != nil {
					t.Fatal(err)
				}
				refs := profile.RuleConfigs
				if len(libraries) != round*len(tc.want) || len(refs) != round*len(tc.want)+1 {
					t.Fatalf("redundant resources: round=%d libraries=%d references=%d", round, len(libraries), len(refs)-1)
				}
				if refs[len(refs)-1].ID != "fallback" {
					t.Fatal("new references moved after MATCH")
				}
				for i, target := range tc.want {
					index := (round-1)*len(tc.want) + i
					library := libraries[index]
					ref := refs[index]
					policy := "DIRECT"
					if target == "proxy" {
						policy = "PROXY"
					}
					if ref.LibraryRuleID != library.ID || ref.Policy == nil || *ref.Policy != policy {
						t.Fatal("first occurrence order or binding lost")
					}
					if slices.Contains(previous, library.ID) {
						t.Fatal("separate initialization reused a resource")
					}
					if round == 2 && library.Name != libraries[i].Name+"-2" {
						t.Fatal("separate initialization lost name suffix")
					}
				}
				previous = nil
				for _, item := range libraries {
					previous = append(previous, item.ID)
				}
				root, err := s.Repo.SharedRulesDir()
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if entry.IsDir() {
						return nil
					}
					if strings.HasSuffix(path, ".bak") {
						t.Error("duplicate targets generated a backup")
					}
					if strings.HasSuffix(path, ".list") {
						count++
						content, err := os.ReadFile(path)
						if err != nil {
							return err
						}
						if len(content) != 0 {
							t.Error("new discovery cache was not empty")
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if count != len(libraries) {
					t.Fatalf("cache count=%d libraries=%d", count, len(libraries))
				}
			}
			before, err := s.Repo.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			s.Post("/api/domain-discovery/rulesets/init", httpxtest.JSON(map[string]any{"targets": []string{"direct", "invalid", "direct"}, "proxy_policy": "PROXY"})).WantStatus(400)
			after, err := s.Repo.Store().Fingerprint()
			if err != nil || before != after {
				t.Fatal("invalid target mutated storage", err)
			}
		})
	}
}
