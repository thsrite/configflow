package discovery

import (
	"configflow/internal/repo"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestApplyDomainsTransactionGuard(t *testing.T) {
	for _, reject := range []bool{true, false} {
		t.Run(map[bool]string{true: "reject", false: "accept"}[reject], func(t *testing.T) {
			svc, ctx := jobFixture(t)
			if _, err := svc.InitRulesets(ctx, "default", []string{"direct"}, ""); err != nil {
				t.Fatal(err)
			}
			r := svc.Repo
			before, err := r.ExportAllJSON(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := []byte(before)
			cacheBefore := ruleCacheSnapshot(t, r.DataDir())
			sentinel := errors.New("guard rejected")
			calls := 0
			_, err = svc.ApplyDomains(ctx, "default", []ApplyEntry{{Value: "example.com", RuleType: "DOMAIN", Target: "direct"}}, "manual", nil, ApplyOptions{BeforeWrite: func(tx context.Context, infos map[string]*RulesetInfo) error {
				calls++
				if infos["direct"] == nil {
					t.Fatal("missing captured selection")
				}
				// Reading through the transaction context must be reentrant and fresh.
				if _, err := r.Config(tx, "default", repo.ResourceRoots{}); err != nil {
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
			after, _ := r.ExportAllJSON(ctx, false)
			wire := []byte(after)
			if reject {
				if !errors.Is(err, sentinel) {
					t.Fatalf("error: %v", err)
				}
				if !reflect.DeepEqual(snapshot, wire) {
					t.Fatal("rejected guard changed repository")
				}
				if after := ruleCacheSnapshot(t, r.DataDir()); !reflect.DeepEqual(cacheBefore, after) {
					t.Fatal("rejected guard changed cache contents, paths or modification times")
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
	seedSvc, seed := jobFixture(t)
	if _, err := seedSvc.InitRulesets(seed, "default", []string{"direct"}, ""); err != nil {
		t.Fatal(err)
	}
	source := seedSvc.Repo
	document, err := source.ExportAllJSON(seed, false)
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
		svc := New(Options{Repo: r})
		ctx := context.Background()
		if _, err := r.ImportAllJSON(ctx, document); err != nil {
			t.Fatal(err)
		}
		var options []ApplyOptions
		if guarded {
			options = []ApplyOptions{{BeforeWrite: func(context.Context, map[string]*RulesetInfo) error { return nil }}}
		}
		out, err := svc.ApplyDomains(ctx, "default", []ApplyEntry{{Value: "example.com", RuleType: "DOMAIN", Target: "direct"}}, "manual", nil, options...)
		if err != nil {
			t.Fatal(err)
		}
		c, err := svc.loadConfig(ctx, "default")
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

// ruleCacheSnapshot detects even a redundant rewrite or a truncated empty cache.
func ruleCacheSnapshot(t *testing.T, dir string) map[string]struct {
	Body     string
	Modified int64
} {
	t.Helper()
	files := map[string]struct {
		Body     string
		Modified int64
	}{}
	root := filepath.Join(dir, "shared", "rules")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[relative] = struct {
			Body     string
			Modified int64
		}{string(data), info.ModTime().UnixNano()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
