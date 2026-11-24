//go:build configflow_difftest

package rulefetch_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"configflow/internal/discovery"
	"configflow/internal/faultprobe"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
)

// Stop the first real writer after DB commit, with its private stage and receipt
// still live. A second editor must not delete that stage or clear its receipt.
func TestReviewInlinePublicationInterleaving(t *testing.T) {
	for _, test := range []struct {
		name, first, second, want string
	}{
		{"adopt-adopt", "first.example", "second.example", "+.old.example\n+.remove.example\n+.first.example\n+.second.example\n"},
		{"adopt-undo", "first.example", "", "+.old.example\n+.first.example\n"},
		{"undo-adopt", "", "second.example", "+.old.example\n+.second.example\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := httpxtest.New(t, httpxtest.Options{})
			seedInlinePublication(t, s)
			control := t.TempDir()
			if err := os.Chmod(control, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(control, "token"), bytes.Repeat([]byte("x"), 32), 0600); err != nil {
				t.Fatal(err)
			}
			pause := filepath.Join(control, "pause."+string(faultprobe.RuleDBCommitted))
			if err := os.WriteFile(pause, nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(faultprobe.ControlEnv, control)
			var writers sync.WaitGroup
			t.Cleanup(func() {
				_ = os.Remove(pause)
				writers.Wait()
			})
			disc := discovery.New(discovery.Options{Repo: s.Repo})
			edit := func(domain string) error {
				if domain == "" {
					_, err := disc.UndoDomain(s.Context(), "default", "remove.example", "direct")
					return err
				}
				_, err := disc.ApplyDomains(s.Context(), "default", []discovery.ApplyEntry{{Value: domain, RuleType: "DOMAIN-SUFFIX", Target: "direct"}}, "manual", nil)
				return err
			}
			first := make(chan error, 1)
			writers.Add(1)
			go func() {
				defer writers.Done()
				first <- edit(test.first)
			}()
			deadline := time.Now().Add(10 * time.Second)
			for {
				events, _ := os.ReadFile(filepath.Join(control, "events.jsonl"))
				if bytes.Contains(events, []byte(`"phase":"rule-db-committed","event":"entered"`)) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("first editor did not reach the post-commit barrier")
				}
				time.Sleep(10 * time.Millisecond)
			}
			catalog, err := s.Repo.Shared(s.Context())
			if err != nil {
				t.Fatal(err)
			}
			rule := rules.LibraryRule(catalog.RuleLibrary[0])
			root, err := rulefetch.GetRulesDir(s.Repo)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, rules.CacheFilename(rule))
			stages, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".rule-edit-*.tmp"))
			if err != nil || len(stages) != 1 || !rulefetch.CacheEditLocks.Held(path) {
				t.Fatalf("first editor lost stage/identity lease: %v %v", stages, err)
			}
			second := make(chan error, 1)
			writers.Add(1)
			go func() {
				defer writers.Done()
				second <- edit(test.second)
			}()
			select {
			case err := <-second:
				t.Fatalf("second editor passed the paused owner: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			after, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".rule-edit-*.tmp"))
			if err != nil || !reflect.DeepEqual(after, stages) {
				t.Fatalf("second editor replaced the first stage: %v %v", after, err)
			}
			if _, err := os.Stat(path + ".invalid"); err != nil {
				t.Fatal("second editor cleared the live receipt", err)
			}
			if err := os.Remove(pause); err != nil {
				t.Fatal(err)
			}
			for _, done := range []<-chan error{first, second} {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("editors did not finish after releasing the barrier")
				}
			}
			catalog, err = s.Repo.Shared(s.Context())
			if err != nil {
				t.Fatal(err)
			}
			rule = rules.LibraryRule(catalog.RuleLibrary[0])
			cache, err := rulefetch.ReadCache(s.Context(), s.Repo, rule, nil)
			if err != nil || string(cache) != test.want || *rule.Content != test.want {
				t.Fatalf("interleaved DB/cache: %q want %q: %v", cache, test.want, err)
			}
			if _, err := os.Stat(path + ".invalid"); !os.IsNotExist(err) {
				t.Fatal("completed editors retained invalidation", err)
			}
		})
	}
}
