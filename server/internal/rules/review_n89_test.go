package rules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

func TestReviewN89CachePathConfinesSymlinkTargets(t *testing.T) {
	for _, namespace := range []bool{false, true} {
		for _, outside := range []bool{false, true} {
			t.Run(fmt.Sprintf("namespace=%t/outside=%t", namespace, outside), func(t *testing.T) {
				root := t.TempDir()
				rule := model.RuleConfig{ID: "../../synthetic"}
				current := filepath.Join(root, CacheFilename(rule))
				targetDir := filepath.Join(root, "contained")
				if outside {
					targetDir = t.TempDir()
				}
				if err := os.MkdirAll(targetDir, 0700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(targetDir, filepath.Base(current))
				if err := os.WriteFile(target, []byte("valid"), 0600); err != nil {
					t.Fatal(err)
				}
				if namespace {
					if err := os.Symlink(targetDir, filepath.Dir(current)); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.MkdirAll(filepath.Dir(current), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, current); err != nil {
						t.Fatal(err)
					}
				}
				got := CacheReadPath(rule, nil, nil, root)
				if outside && got != "" {
					t.Fatalf("escaping cache path accepted: %q", got)
				}
				if !outside && got != current {
					t.Fatalf("contained cache path refused: %q", got)
				}
			})
		}
	}
}

func TestReviewN89ProviderCopyEnforcesAliasBudget(t *testing.T) {
	for _, depth := range []int{4, 20} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			catalog, document, _ := aliasProviderFixture(t)
			target := yamlx.Seq(yamlx.Str("entry"))
			target.Anchor = "budget0"
			for i := 1; i <= depth; i++ {
				next := yamlx.Seq(
					&yamlx.Node{Kind: yaml.AliasNode, Value: target.Anchor, Alias: target},
					&yamlx.Node{Kind: yaml.AliasNode, Value: target.Anchor, Alias: target})
				next.Anchor = fmt.Sprintf("budget%d", i)
				target = next
			}
			provider := yamlx.Get(yamlx.Get(document, "proxy-providers"), "Synthetic")
			yamlx.Set(provider, "extension", target)
			err := LowerProviderFilters(catalog, document)
			if depth == 20 {
				if !errors.Is(err, yamlx.ErrAliasBudget) {
					t.Fatalf("expected actual alias budget refusal, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal("small shared graph refused", err)
			}
			// Count physical nodes, following both edges exactly once. A copy
			// must preserve sharing instead of expanding the logical tree.
			seen := map[*yamlx.Node]bool{}
			var visit func(*yamlx.Node)
			visit = func(n *yamlx.Node) {
				if n == nil || seen[n] {
					return
				}
				seen[n] = true
				visit(n.Alias)
				for _, child := range n.Content {
					visit(child)
				}
			}
			group := yamlx.Get(document, "proxy-groups").Content[0]
			name := yamlx.Get(group, "use").Content[0].Value
			visit(yamlx.Get(yamlx.Get(document, "proxy-providers"), name))
			if len(seen) > 60 {
				t.Fatalf("provider alias copy expanded: %d physical nodes", len(seen))
			}
			if _, err := yamlx.Marshal(document); err != nil {
				t.Fatal("small graph lost output closure", err)
			}
		})
	}
}
