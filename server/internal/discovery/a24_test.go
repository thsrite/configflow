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

func TestApplyDomainsGuardPassMatchesDefault(t *testing.T) {
	seed := jobFixture(t)
	if _, err := InitRulesets(seed, "default", []string{"direct"}, ""); err != nil {
		t.Fatal(err)
	}
	source, _ := repo.GetRepository(seed)
	document, err := source.ExportAll(seed, false)
	if err != nil {
		t.Fatal(err)
	}
	var outputs []ApplyResult
	var histories [][]HistoryEntry
	var libraries [][]libraryItem
	for _, guarded := range []bool{false, true} {
		r, err := repo.Open(t.TempDir(), repo.Options{})
		if err != nil {
			t.Fatal(err)
		}
		ctx := repo.WithRepository(context.Background(), r)
		if _, err := r.ImportAll(ctx, document); err != nil {
			t.Fatal(err)
		}
		var options []ApplyOptions
		if guarded {
			options = []ApplyOptions{{BeforeWrite: func(context.Context, map[string]*RulesetInfo) error { return nil }}}
		}
		out, err := ApplyDomains(ctx, "default", []ApplyEntry{{Value: "example.com", RuleType: "DOMAIN", Target: "direct"}}, "manual", nil, options...)
		if err != nil {
			t.Fatal(err)
		}
		c, err := loadConfig(ctx, "default")
		if err != nil {
			t.Fatal(err)
		}
		for i := range c.Settings.History {
			c.Settings.History[i].AppliedAt = "<time>"
		}
		outputs = append(outputs, out)
		histories = append(histories, c.Settings.History)
		libraries = append(libraries, c.Library)
	}
	if !reflect.DeepEqual(outputs[0], outputs[1]) {
		t.Fatal("guard changed response")
	}
	for _, pair := range [][2]any{{histories[0], histories[1]}, {libraries[0], libraries[1]}} {
		a, _ := json.Marshal(pair[0])
		b, _ := json.Marshal(pair[1])
		if string(a) != string(b) {
			t.Fatalf("guard changed persisted content: %s != %s", a, b)
		}
	}
}

func TestQueryRulesetIgnoresUnrelatedSettings(t *testing.T) {
	out, err := QueryRuleset(json.RawMessage(`{"domain_discovery":{"direct_ruleset":42},"rule_library":[{"id":"ip","name":"IPs","source_type":"content","behavior":"ipcidr"}],"rule_configs":[{"id":"ref","itemType":"ruleset","library_rule_id":"ip","policy":"DIRECT"}]}`), "ip")
	if err != nil || out == nil || out.ID != "ip" || len(out.References) != 1 || !out.Active {
		t.Fatalf("query: %+v %v", out, err)
	}
}
