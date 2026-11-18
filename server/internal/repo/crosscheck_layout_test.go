package repo

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

// O12 permits these retired lock families, not whole directories or *.lock.
// .config.lock, cache content/backup/markers and migration copies stay compared.
var xcRetiredLockPattern = regexp.MustCompile(`^(profiles/\.[A-Za-z0-9][A-Za-z0-9_-]{0,63}\.operation\.lock|shared/rules/(by-id|by-source)/[0-9a-f]{64}\.list\.lock)$`)

// These additional leaf locks are the exact resources accessed by the C04
// corpus. Keep siblings and unrelated .lock paths in the comparison.
var xcRetiredCorpusLeafLocks = map[string]bool{
	"profiles/work/a/c.txt.lock":               true,
	"profiles/work/data/x.json.lock":           true,
	"profiles/work/generated/config.yaml.lock": true,
	"shared/cache/a.json.lock":                 true,
	"shared/cache/missing.json.lock":           true,
	"shared/rules/x.list.lock":                 true,
}

func xcRetiredLock(relative string) bool {
	return xcRetiredLockPattern.MatchString(relative) || xcRetiredCorpusLeafLocks[relative]
}

type xcLockFile struct {
	info os.FileInfo
	data []byte
}

func xcRetiredLockSnapshot(t *testing.T, directory string) map[string]xcLockFile {
	t.Helper()
	files := map[string]xcLockFile{}
	for _, value := range xcLayout(t, directory) {
		relative := value.(string)
		if xcRetiredLock(relative) {
			path := filepath.Join(directory, filepath.FromSlash(relative))
			files[relative] = xcLockFile{must(os.Stat(path))(t), must(os.ReadFile(path))(t)}
		}
	}
	return files
}

func xcRetiredLockChanges(before, after map[string]xcLockFile) []string {
	changed := []string{}
	for path, current := range after {
		old, existed := before[path]
		if !existed || !os.SameFile(old.info, current.info) || !old.info.ModTime().Equal(current.info.ModTime()) || old.info.Mode() != current.info.Mode() || !bytes.Equal(old.data, current.data) {
			changed = append(changed, path)
		}
	}
	for path := range before {
		if _, exists := after[path]; !exists {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

func TestCrossCheckRetiredLockFilterIsNarrow(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, tt := range []struct {
		path    string
		retired bool
	}{
		{"profiles/.work.operation.lock", true},
		{"profiles/.copy-2.operation.lock", true},
		{"shared/rules/by-source/" + digest + ".list.lock", true},
		{"shared/rules/by-id/" + digest + ".list.lock", true},
		{"profiles/work/a/c.txt.lock", true},
		{"profiles/work/data/x.json.lock", true},
		{"profiles/work/generated/config.yaml.lock", true},
		{"shared/cache/a.json.lock", true},
		{"shared/cache/missing.json.lock", true},
		{"shared/rules/x.list.lock", true},
		{"profiles/work/a/other.txt.lock", false},
		{"shared/cache/other.json.lock", false},
		{"shared/rules/x.list", false},
		{"shared/rules/x.list.lock/", false},
		{"shared/rules/x.list.lock.bak", false},
		{".config.lock", false},
		{"profiles/", false},
		{"profiles/work/config.json", false},
		{"profiles/work.operation.lock", false},
		{"profiles/..operation.lock", false},
		{"profiles/.work.operation.lock/", false},
		{"profiles/.work.operation.lock.bak", false},
		{"migrations/backup/profiles/.work.operation.lock", false},
		{"shared/rules/by-source/" + digest + ".list", false},
		{"shared/rules/by-source/" + digest + ".list.bak", false},
		{"shared/rules/by-source/" + digest + ".list.invalid", false},
		{"shared/rules/by-source/" + digest + ".list.lock/", false},
		{"shared/rules/by-source/short.list.lock", false},
		{"shared/rules/by-name/" + digest + ".list.lock", false},
		{"shared/other.json.lock", false},
	} {
		t.Run(tt.path, func(t *testing.T) {
			if got := xcRetiredLock(tt.path); got != tt.retired {
				t.Fatalf("retired=%t want=%t", got, tt.retired)
			}
			got := xcMaskedLayout(t, jsonx.Obj("result", []any{tt.path}))
			if tt.retired {
				if len(got) != 0 {
					t.Fatal("retired lock stayed in comparison", got)
				}
			} else if len(got) != 1 || got[0] != xcOrdinal.ReplaceAllString(xcMask(tt.path), "<random>") {
				t.Fatal("unrelated layout entry was relaxed", got)
			}
		})
	}
}

func TestCrossCheckRetiredLockGuardDetectsChanges(t *testing.T) {
	for _, action := range []string{"unchanged", "create", "remove", "content", "mtime", "inode"} {
		t.Run(action, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "profiles", ".legacy.operation.lock")
			noError(t, os.MkdirAll(filepath.Dir(path), 0700))
			noError(t, os.WriteFile(path, []byte("synthetic legacy lock"), 0600))
			stamp := time.Unix(1700000000, 0)
			noError(t, os.Chtimes(path, stamp, stamp))
			before := xcRetiredLockSnapshot(t, dir)
			switch action {
			case "create":
				noError(t, os.WriteFile(filepath.Join(filepath.Dir(path), ".new.operation.lock"), nil, 0600))
			case "remove":
				noError(t, os.Remove(path))
			case "content":
				noError(t, os.WriteFile(path, []byte("changed"), 0600))
				noError(t, os.Chtimes(path, stamp, stamp))
			case "mtime":
				noError(t, os.Chtimes(path, stamp.Add(time.Second), stamp.Add(time.Second)))
			case "inode":
				replacement := path + ".new"
				noError(t, os.WriteFile(replacement, []byte("synthetic legacy lock"), 0600))
				noError(t, os.Chtimes(replacement, stamp, stamp))
				noError(t, os.Rename(replacement, path))
			}
			changed := xcRetiredLockChanges(before, xcRetiredLockSnapshot(t, dir))
			if action == "unchanged" && len(changed) != 0 || action != "unchanged" && len(changed) != 1 {
				t.Fatalf("guard did not detect %s: %v", action, changed)
			}
		})
	}
}
