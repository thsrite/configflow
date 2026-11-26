package repo_test

import (
	"os"
	"testing"
	"time"
)

func TestSharedTextIfChangedFreshness(t *testing.T) {
	tests := []struct {
		name, main, backup, wantBackup string
		haveMain, haveBackup, touch    bool
	}{
		{name: "both-match", main: "same", backup: "same", wantBackup: "same", haveMain: true, haveBackup: true, touch: true},
		{name: "main-differs", main: "old", backup: "same", wantBackup: "old", haveMain: true, haveBackup: true},
		{name: "backup-differs", main: "same", backup: "older", wantBackup: "same", haveMain: true, haveBackup: true},
		{name: "backup-missing", main: "same", wantBackup: "same", haveMain: true},
		{name: "new-file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, _, ctx := commitRuntime(t)
			path, err := r.SharedPath("freshness.txt")
			if err != nil {
				t.Fatal(err)
			}
			old := time.Unix(1700000000, 0)
			for _, file := range []struct {
				name, value string
				exists      bool
			}{{path, test.main, test.haveMain}, {path + ".bak", test.backup, test.haveBackup}} {
				if !file.exists {
					continue
				}
				if err := os.WriteFile(file.name, []byte(file.value), 0640); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(file.name, old, old); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.Stat(path)
			written, err := r.WriteSharedTextIfChanged(ctx, "freshness.txt", []byte("same"))
			if err != nil || written != path {
				t.Fatal(written, err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "same" || !after.ModTime().After(old) {
				t.Fatal("content or freshness", string(data), err)
			}
			if test.touch {
				if !os.SameFile(before, after) || before.Mode() != after.Mode() {
					t.Fatal("touch replaced the primary or changed its mode")
				}
			} else if (before != nil && os.SameFile(before, after)) || after.Mode().Perm() != 0600 {
				t.Fatal("replacement did not use a new private file")
			}
			backup, err := os.ReadFile(path + ".bak")
			if !test.haveMain {
				if !os.IsNotExist(err) {
					t.Fatal("new file unexpectedly gained a backup", err)
				}
			} else if err != nil || string(backup) != test.wantBackup {
				t.Fatal("previous-content backup", string(backup), err)
			}
			if test.touch {
				info, err := os.Stat(path + ".bak")
				if err != nil || !info.ModTime().Equal(old) {
					t.Fatal("touch modified backup freshness", err)
				}
			}
		})
	}
}
