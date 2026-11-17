package taskstatus

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStoreInstancesShareMutexAndLeaveLegacyLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Filename+".lock")
	if err := os.WriteFile(path, []byte("python sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for worker := range 20 {
		wg.Go(func() {
			store := New(dir)
			for item := range 5 {
				if _, err := store.Record(fmt.Sprintf("%d:%d", worker, item), Result{OK: true}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	values, err := New(dir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if values.Len() != 100 {
		t.Fatal("updates lost", values.Len())
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || string(data) != "python sentinel" {
		t.Fatal("legacy lock changed")
	}
}

func TestNoNewTaskStatusLockFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := New(dir).Record("task", Result{OK: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, Filename+".lock")); !os.IsNotExist(err) {
		t.Fatal("created resource lock", err)
	}
}
