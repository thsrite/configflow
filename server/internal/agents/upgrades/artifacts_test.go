package upgrades

// Port of backend/test_agent_artifacts.py: Agent artifact digests stay
// bounded, fresh and safe for concurrent callers. Python's Path.open /
// readinto interception is the openHook / readHook seam here.
// test_windows_streams_without_reusing_or_storing_digest_cache is not
// ported: the Go server does not run on Windows (os.name == 'nt' branch).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/testjson"
)

func artifactDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AGENTS_STATIC_DIR", dir)
	ResetArtifactCache()
	t.Cleanup(func() {
		ResetArtifactCache()
		openHook, readHook = nil, nil
		maxArtifactBytes, artifactReadBytes, artifactCacheLimit = 128*1024*1024, 256*1024, 12
	})
	return dir
}

func put(t *testing.T, dir string, content []byte, arch string) string {
	t.Helper()
	path := filepath.Join(dir, "configflow-agent-"+arch)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func artifactInfo(content []byte) map[string]any {
	sum := sha256.Sum256(content)
	return map[string]any{"sha256": hex.EncodeToString(sum[:]), "size": len(content)}
}

func mustArtifacts(t *testing.T) model.OrderedMap[model.UpgradeArtifact] {
	t.Helper()
	result, err := ArtifactsModel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func firstArch(t *testing.T) model.UpgradeArtifact {
	v, _ := mustArtifacts(t).Get(Arches[0])
	return v
}

// interceptReads counts the opens of path and calls callback after every
// read of it (Python's ReadProxy).
func interceptReads(path string, callback func(calls, n int) error) *int {
	opened := 0
	openHook = func(p string) {
		if p == path {
			opened++
		}
	}
	readHook = func(p string, calls, n int) error {
		if p != path || callback == nil {
			return nil
		}
		return callback(calls, n)
	}
	return &opened
}

func statNS(t *testing.T, path string) (atime, mtime, ctime int64) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	return statNanos(st)
}

func utime(t *testing.T, path string, atime, mtime int64) {
	t.Helper()
	if err := os.Chtimes(path, time.Unix(0, atime), time.Unix(0, mtime)); err != nil {
		t.Fatal(err)
	}
}

func TestStreamingMatchesAllArchesAndPreservesOrder(t *testing.T) {
	dir := artifactDir(t)
	want := map[string]any{}
	for _, arch := range Arches {
		content := append([]byte("\x7fELF"), []byte(strings.Repeat(arch, 5000))...)
		put(t, dir, content, arch)
		want[arch] = artifactInfo(content)
	}
	artifactReadBytes = 3
	got := mustArtifacts(t)
	if !testjson.Equal(want, got) || strings.Join(mustArtifacts(t).Keys(), ",") != strings.Join(Arches, ",") {
		t.Fatalf("got %+v", got)
	}
}

func TestElfAndInclusiveSizeLimit(t *testing.T) {
	cases := []struct {
		content  string
		accepted bool
	}{
		{"", false}, {"\x7fEL", false}, {"ELF!", false},
		{"\x7fELF", true}, {"\x7fELF" + strings.Repeat("x", 12), true},
		{"\x7fELF" + strings.Repeat("x", 13), false},
	}
	for _, c := range cases {
		dir := artifactDir(t)
		maxArtifactBytes = 16
		put(t, dir, []byte(c.content), Arches[0])
		want := map[string]any{}
		if c.accepted {
			want[Arches[0]] = artifactInfo([]byte(c.content))
		}
		if got := mustArtifacts(t); !testjson.Equal(want, got) {
			t.Fatalf("%q: got %+v", c.content, got)
		}
	}
}

func TestMissingAndNonregularArtifactsAreSkipped(t *testing.T) {
	dir := artifactDir(t)
	os.Mkdir(filepath.Join(dir, "configflow-agent-"+Arches[1]), 0o755)
	if got := mustArtifacts(t); got.Len() != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestCacheAvoidsRereadAndReturnsIndependentDicts(t *testing.T) {
	dir := artifactDir(t)
	content := []byte("\x7fELFcached")
	path := put(t, dir, content, Arches[0])
	opened := interceptReads(path, nil)
	first := mustArtifacts(t)
	item, _ := first.Get(Arches[0])
	item.SHA256 = "corrupt"
	first.Set(Arches[0], item)
	first.Set("new", model.UpgradeArtifact{})
	second := mustArtifacts(t)
	if !testjson.Equal(map[string]any{Arches[0]: artifactInfo(content)}, second) || *opened != 1 {
		t.Fatalf("second %+v opened %d", second, *opened)
	}
	if second.Len() != 1 {
		t.Fatal("shared objects")
	}
}

func TestInPlaceEditWithSameSizeAndMtimeInvalidatesCache(t *testing.T) {
	dir := artifactDir(t)
	path := put(t, dir, []byte("\x7fELFold"), Arches[0])
	atime, mtime, ctime := statNS(t, path)
	opened := interceptReads(path, nil)
	if !testjson.Equal(artifactInfo([]byte("\x7fELFold")), firstArch(t)) {
		t.Fatal("old")
	}
	os.WriteFile(path, []byte("\x7fELFnew"), 0o644)
	utime(t, path, atime, mtime)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, _, c := statNS(t, path); c != ctime || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
		utime(t, path, atime, mtime)
	}
	if _, m, c := statNS(t, path); c == ctime || m != mtime {
		t.Fatal("ctime did not change")
	}
	if !testjson.Equal(artifactInfo([]byte("\x7fELFnew")), firstArch(t)) || *opened != 2 {
		t.Fatalf("opened %d", *opened)
	}
}

func TestReplacementWithSameSizeAndMtimeInvalidatesCache(t *testing.T) {
	dir := artifactDir(t)
	path := put(t, dir, []byte("\x7fELFold"), Arches[0])
	atime, mtime, _ := statNS(t, path)
	opened := interceptReads(path, nil)
	mustArtifacts(t)
	replacement := filepath.Join(dir, "replacement")
	os.WriteFile(replacement, []byte("\x7fELFnew"), 0o644)
	utime(t, replacement, atime, mtime)
	os.Rename(replacement, path)
	if !testjson.Equal(artifactInfo([]byte("\x7fELFnew")), firstArch(t)) || *opened != 2 {
		t.Fatalf("opened %d", *opened)
	}
}

func TestDeletionDiscardsCachedSummaryAndRecreationIsRead(t *testing.T) {
	dir := artifactDir(t)
	path := put(t, dir, []byte("\x7fELFold"), Arches[0])
	opened := interceptReads(path, nil)
	mustArtifacts(t)
	os.Remove(path)
	if got := mustArtifacts(t); got.Len() != 0 || cacheLen() != 0 {
		t.Fatalf("got %+v cache %d", got, cacheLen())
	}
	os.WriteFile(path, []byte("\x7fELFnew"), 0o644)
	if !testjson.Equal(artifactInfo([]byte("\x7fELFnew")), firstArch(t)) || *opened != 2 {
		t.Fatalf("opened %d", *opened)
	}
}

func TestDirectorySwitchDoesNotReuseSameName(t *testing.T) {
	dir := artifactDir(t)
	put(t, dir, []byte("\x7fELFold"), Arches[0])
	if !testjson.Equal(artifactInfo([]byte("\x7fELFold")), firstArch(t)) {
		t.Fatal("old")
	}
	other := filepath.Join(dir, "other")
	os.Mkdir(other, 0o755)
	put(t, other, []byte("\x7fELFnew"), Arches[0])
	t.Setenv("AGENTS_STATIC_DIR", other)
	if !testjson.Equal(artifactInfo([]byte("\x7fELFnew")), firstArch(t)) || cacheLen() != 2 {
		t.Fatalf("cache %d", cacheLen())
	}
	t.Setenv("AGENTS_STATIC_DIR", dir)
	if !testjson.Equal(artifactInfo([]byte("\x7fELFold")), firstArch(t)) {
		t.Fatal("old again")
	}
}

func TestCacheIsBoundedAcrossDirectoryChanges(t *testing.T) {
	dir := artifactDir(t)
	artifactCacheLimit = 2
	for number := 0; number < 6; number++ {
		directory := filepath.Join(dir, strconv.Itoa(number))
		os.Mkdir(directory, 0o755)
		put(t, directory, []byte{0x7f, 'E', 'L', 'F', byte(number)}, Arches[0])
		t.Setenv("AGENTS_STATIC_DIR", directory)
		if mustArtifacts(t).Len() == 0 || cacheLen() > 2 {
			t.Fatalf("round %d cache %d", number, cacheLen())
		}
	}
}

func TestReplacementWhileReadingRetriesBeforePublishing(t *testing.T) {
	dir := artifactDir(t)
	path := put(t, dir, []byte("\x7fELFold-version"), Arches[0])
	replacement := filepath.Join(dir, "replacement")
	newContent := []byte("\x7fELFnew-version")
	os.WriteFile(replacement, newContent, 0o644)
	changed := false
	opened := interceptReads(path, func(_, n int) error {
		if n > 0 && !changed {
			os.Rename(replacement, path)
			changed = true
		}
		return nil
	})
	if !testjson.Equal(artifactInfo(newContent), firstArch(t)) || *opened != 2 {
		t.Fatalf("opened %d", *opened)
	}
	if !testjson.Equal(artifactInfo(newContent), firstArch(t)) || *opened != 2 {
		t.Fatalf("opened %d", *opened)
	}
}

func TestReplacementBetweenStatAndOpenRetries(t *testing.T) {
	dir := artifactDir(t)
	path := put(t, dir, []byte("\x7fELFold"), Arches[0])
	replacement := filepath.Join(dir, "replacement")
	os.WriteFile(replacement, []byte("\x7fELFnew"), 0o644)
	opened := 0
	openHook = func(p string) {
		if p == path {
			if opened == 0 {
				os.Rename(replacement, path)
			}
			opened++
		}
	}
	if !testjson.Equal(artifactInfo([]byte("\x7fELFnew")), firstArch(t)) || opened != 2 {
		t.Fatalf("opened %d", opened)
	}
}

func TestDeletionWhileReadingIsNotPublishedOrCached(t *testing.T) {
	dir := artifactDir(t)
	path := put(t, dir, []byte("\x7fELFremoved"), Arches[0])
	removed := false
	interceptReads(path, func(_, n int) error {
		if n > 0 && !removed {
			os.Remove(path)
			removed = true
		}
		return nil
	})
	if got := mustArtifacts(t); got.Len() != 0 || cacheLen() != 0 {
		t.Fatalf("got %+v cache %d", got, cacheLen())
	}
}

func TestRepeatedFileChangesFailWithoutCaching(t *testing.T) {
	dir := artifactDir(t)
	path := put(t, dir, []byte("\x7fELFchanging"), Arches[0])
	opened := interceptReads(path, func(_, n int) error {
		if n == 0 {
			atime, mtime, _ := statNS(t, path)
			utime(t, path, atime, mtime+1)
		}
		return nil
	})
	_, err := ArtifactsModel(context.Background())
	if !errors.Is(err, ErrArtifactChanged) || !strings.Contains(err.Error(), "changed while being read") || *opened != 2 || cacheLen() != 0 {
		t.Fatalf("err %v opened %d cache %d", err, *opened, cacheLen())
	}
}

func TestReadErrorsPropagateAndAreNotCached(t *testing.T) {
	for _, failure := range []error{os.ErrPermission, errors.New("read failure")} {
		dir := artifactDir(t)
		path := put(t, dir, []byte("\x7fELFbroken"), Arches[0])
		interceptReads(path, func(int, int) error { return failure })
		if _, err := ArtifactsModel(context.Background()); !errors.Is(err, failure) || cacheLen() != 0 {
			t.Fatalf("err %v cache %d", err, cacheLen())
		}
	}
}

func TestInvalidOversizeFileDoesNotHideLateReadErrors(t *testing.T) {
	dir := artifactDir(t)
	path := put(t, dir, []byte(strings.Repeat("not-an-elf-file", 10)), Arches[0])
	maxArtifactBytes = 8
	artifactReadBytes = 8
	late := errors.New("late read failure")
	interceptReads(path, func(calls, _ int) error {
		if calls == 3 {
			return late
		}
		return nil
	})
	if _, err := ArtifactsModel(context.Background()); !errors.Is(err, late) {
		t.Fatalf("err %v", err)
	}
}

func TestConcurrentRequestsShareOneCompleteDigest(t *testing.T) {
	dir := artifactDir(t)
	content := []byte("\x7fELFconcurrent")
	path := put(t, dir, content, Arches[0])
	var mu sync.Mutex
	reads := 0
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	openHook = func(p string) {
		if p != path {
			return
		}
		mu.Lock()
		reads++
		mu.Unlock()
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-time.After(5 * time.Second):
			t.Error("not released")
		}
	}
	results := make([]model.OrderedMap[model.UpgradeArtifact], 8)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = ArtifactsModel(context.Background())
		}(i)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no read started")
	}
	close(release)
	wg.Wait()
	if reads != 1 {
		t.Fatalf("reads %d", reads)
	}
	for _, result := range results {
		if !testjson.Equal(map[string]any{Arches[0]: artifactInfo(content)}, result) {
			t.Fatalf("result %+v", result)
		}
	}
	item, _ := results[0].Get(Arches[0])
	item.Size = -1
	results[0].Set(Arches[0], item)
	secondItem, _ := results[1].Get(Arches[0])
	if !testjson.Equal(len(content), secondItem.Size) {
		t.Fatal("shared result")
	}
}
