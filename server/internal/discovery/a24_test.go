package discovery

import (
	"configflow/internal/repo"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestApplyDomainsTransactionGuard(t *testing.T) {
	for _, reject := range []bool{true, false} {
		t.Run(map[bool]string{true: "reject", false: "accept"}[reject], func(t *testing.T) {
			ctx := jobFixture(t)
			if _, err := InitRulesets(ctx, "default", []string{"direct"}, ""); err != nil {
				t.Fatal(err)
			}
			r, _ := repo.GetRepository(ctx)
			before, err := r.ExportAll(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, _ := json.Marshal(before)
			sentinel := errors.New("guard rejected")
			calls := 0
			_, err = ApplyDomains(ctx, "default", []ApplyEntry{{Value: "example.com", RuleType: "DOMAIN", Target: "direct"}}, "manual", nil, ApplyOptions{BeforeWrite: func(tx context.Context, infos map[string]*RulesetInfo) error {
				calls++
				if infos["direct"] == nil {
					t.Fatal("missing captured selection")
				}
				// Reading through the transaction context must be reentrant and fresh.
				if _, err := r.GetCompatConfig(tx, "default", repo.ResourceRoots{}); err != nil {
					t.Fatal(err)
				}
				if reject {
					return sentinel
				}
				return nil
			}})
			if calls != 1 {
				t.Fatalf("guard calls %d", calls)
			}
			after, _ := r.ExportAll(ctx, false)
			wire, _ := json.Marshal(after)
			if reject {
				if !errors.Is(err, sentinel) {
					t.Fatalf("error: %v", err)
				}
				if !reflect.DeepEqual(snapshot, wire) {
					t.Fatal("rejected guard changed repository")
				}
				files, _ := filepath.Glob(filepath.Join(r.DataDir(), "shared/rules/*"))
				for _, file := range files {
					raw, _ := os.ReadFile(file)
					if strings.Contains(string(raw), "example.com") {
						t.Fatal("cache changed")
					}
				}
			} else if err != nil {
				t.Fatal(err)
			} else if reflect.DeepEqual(snapshot, wire) {
				t.Fatal("accepted guard made no change")
			}
		})
	}
}
