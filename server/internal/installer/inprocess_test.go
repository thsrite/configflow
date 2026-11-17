package installer

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConcurrentIssueKeepsIdentityAndLegacyLock(t *testing.T) {
	for _, kind := range []string{"file", "directory", "missing"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "docker-ha-installations")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			legacy := filepath.Join(root, ".issue.lock")
			switch kind {
			case "file":
				if err := os.WriteFile(legacy, []byte("python sentinel"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(legacy, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind != "missing" {
				stamp := time.Unix(1700000000, 0)
				if err := os.Chtimes(legacy, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.Stat(legacy)
			results := make(chan *Enrollment, 20)
			var wg sync.WaitGroup
			for range 20 {
				wg.Go(func() {
					request, cancel := context.WithCancel(context.Background())
					cancel()
					result, err := IssueEnrollment(request, dir, reviewBody(), "default")
					if err != nil {
						t.Error(err)
						return
					}
					results <- result
				})
			}
			wg.Wait()
			close(results)
			var first *Enrollment
			for result := range results {
				if first == nil {
					first = result
				}
				if *result != *first {
					t.Error("concurrent retry changed identity")
				}
			}
			if first == nil {
				t.Fatal("no enrollment")
			}
			after, err := os.Stat(legacy)
			if kind == "missing" {
				if !os.IsNotExist(err) {
					t.Fatal("created lock file", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("legacy lock metadata changed")
			}
			if kind == "file" {
				data, err := os.ReadFile(legacy)
				if err != nil || string(data) != "python sentinel" {
					t.Fatal("legacy lock contents changed", err)
				}
			}
		})
	}
}
