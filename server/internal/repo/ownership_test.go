package repo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/model"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if os.Getenv("CONFIGFLOW_REPO_ORDER_HELPER") != "1" && lockx.Violations() != 0 {
		logx.Get("backend.lockx").Errorf("unexpected repository lock violations: %d", lockx.Violations())
		code = 1
	}
	os.Exit(code)
}

func repositoryChild(t *testing.T, name string) bool {
	t.Helper()
	if os.Getenv("CONFIGFLOW_REPO_ORDER_HELPER") == "1" {
		return false
	}
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+name+"$", "-test.count=1")
	cmd.Env = append(os.Environ(), "CONFIGFLOW_REPO_ORDER_HELPER=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("repository child: %v\n%s", err, out)
	}
	return true
}

func repoEventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !fn() {
		if time.Now().After(deadline) {
			t.Fatal("repository condition not reached")
		}
		runtime.Gosched()
	}
}

func repoReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("repository work did not finish")
		var zero T
		return zero
	}
}

func TestRepositoryRecordsL3Process(t *testing.T) {
	if repositoryChild(t, "TestRepositoryRecordsL3Process") {
		return
	}
	path := filepath.Join(t.TempDir(), ".config.lock")
	noError(t, withLock(bg, path, func(held context.Context) error {
		_, release, err := lockx.NewMutex(lockx.LevelDevice, "illegal-device").Lock(held, time.Second)
		if err != nil {
			return err
		}
		release()
		return nil
	}))
	if lockx.Violations() != 1 {
		t.Fatal("missing L3 record", lockx.Violations())
	}
}

func TestMissingJobLockFailsClosed(t *testing.T) {
	r, _ := rsRepository(t)
	old := SetHooks(Hooks{})
	defer SetHooks(old)
	head := must(r.Head(bg))(t)
	router := rsRouter(t, r)
	router.Endpoint = "http://192.0.2.5"
	raw := mustMarshalJSON(t, map[string]any{"routers": map[string]any{"items": []model.Router{router}}})
	_, _, err := r.SaveProfileFieldsJSON(bg, "live", raw, nil)
	if !errors.Is(err, ErrInUse) {
		t.Fatal("unwired job gate allowed change", err)
	}
	if must(r.Head(bg))(t) != head {
		t.Fatal("rejected change committed")
	}
}

func setOwnershipMarker(r *Repository, ctx context.Context, value string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.Update(ctx, func(_ context.Context, document *model.Document) error {
		return document.Shared.Fields.Set("marker", raw)
	})
}

func readOwnershipMarker(t *testing.T, r *Repository, ctx context.Context) string {
	t.Helper()
	raw, ok := must(r.Shared(ctx))(t).Fields.Get("marker")
	if !ok {
		return ""
	}
	var s string
	noError(t, json.Unmarshal(raw, &s))
	return s
}

func TestOwnerLifetimeAndLocalBatch(t *testing.T) {
	r := newRepo(t)
	request, disconnect := context.WithCancel(bg)
	disconnect()
	err := setOwnershipMarker(r, request, "disconnect")
	noError(t, err)
	job, shutdown := context.WithCancel(bg)
	shutdown()
	ctx := ctxx.WithParent(bg, job)
	err = setOwnershipMarker(r, ctx, "shutdown")
	if !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown ignored", err)
	}
	err = setOwnershipMarker(r, ctxx.Uninterruptible(ctx), "batch")
	noError(t, err)
	if readOwnershipMarker(t, r, bg) != "batch" {
		t.Fatal("batch interrupted after detach")
	}
}

func TestReadSnapshotSerializesWriterAndExpires(t *testing.T) {
	r := newRepo(t)
	err := setOwnershipMarker(r, bg, "old")
	noError(t, err)
	var past context.Context
	written := make(chan error, 1)
	noError(t, r.WithReadSnapshot(bg, func(held context.Context) error {
		past = held
		go func() { written <- setOwnershipMarker(r, bg, "new") }()
		key := resolvePath(r.lockFile)
		repoEventually(t, func() bool { configLocksHeld := configLocks.Held(key); return configLocksHeld })
		if readOwnershipMarker(t, r, held) != "old" {
			t.Fatal("incoherent snapshot")
		}
		select {
		case err := <-written:
			t.Fatal("writer escaped snapshot", err)
		default:
		}
		_, err := r.SaveSharedRecord(held, &model.Shared{}, nil)
		if !errors.Is(err, ErrReadOnlySnapshot) {
			t.Fatal("snapshot allowed write", err)
		}
		return nil
	}))
	noError(t, repoReceive(t, written))
	if readSnapshotActive(past) || holdsLock(past, r.lockFile) {
		t.Fatal("released context still holds snapshot/lock")
	}
	if readOwnershipMarker(t, r, past) != "new" {
		t.Fatal("expired snapshot reused")
	}
	_, release, err := lockx.NewMutex(lockx.LevelDevice, "after-snapshot").Lock(past, time.Second)
	noError(t, err)
	release()
}

func TestProfileOperationsSerializePerProfileAndAllowParallelProfiles(t *testing.T) {
	r := newRepo(t)
	_, err := r.CreateProfileRecord(bg, ProfileCreation{ID: "other", Name: "Other"}, "")
	noError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	first := make(chan error, 1)
	go func() {
		first <- r.writeProfileFile(bg, "default", filepath.Join(r.profilesDir, "default", "first"), func() error { close(entered); <-release; return nil })
	}()
	repoReceive(t, entered)
	secondEntered := make(chan struct{})
	second := make(chan error, 1)
	go func() {
		second <- r.writeProfileFile(bg, "default", filepath.Join(r.profilesDir, "default", "second"), func() error { close(secondEntered); return nil })
	}()
	parallel := make(chan error, 1)
	go func() {
		parallel <- r.writeProfileFile(bg, "other", filepath.Join(r.profilesDir, "other", "file"), func() error { return nil })
	}()
	noError(t, repoReceive(t, parallel))
	select {
	case <-secondEntered:
		t.Fatal("same profile writes overlapped")
	default:
	}
	close(release)
	noError(t, repoReceive(t, first))
	noError(t, repoReceive(t, second))
}

func TestDeleteWaitsForProfileWrite(t *testing.T) {
	r := newRepo(t)
	_, err := r.CreateProfileRecord(bg, ProfileCreation{ID: "other", Name: "Other"}, "")
	noError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	path := filepath.Join(r.profilesDir, "default", "artifact")
	write := make(chan error, 1)
	go func() {
		write <- r.writeProfileFile(bg, "default", path, func() error {
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				return err
			}
			close(entered)
			<-release
			return os.WriteFile(path, []byte("complete"), 0600)
		})
	}()
	repoReceive(t, entered)
	deleted := make(chan error, 1)
	go func() { deleted <- r.DeleteProfile(bg, "default", "other") }()
	select {
	case err := <-deleted:
		t.Fatal("delete escaped writer", err)
	default:
	}
	close(release)
	noError(t, repoReceive(t, write))
	noError(t, repoReceive(t, deleted))
	paths, err := filepath.Glob(filepath.Join(r.migrationsDir, "deleted-default-*", "artifact"))
	noError(t, err)
	if len(paths) != 1 {
		t.Fatal("completed file not moved with deletion", paths)
	}
	data, err := os.ReadFile(paths[0])
	noError(t, err)
	if string(data) != "complete" {
		t.Fatal("partial write moved")
	}
}

func TestResourceLeafWaitAndLegacyFiles(t *testing.T) {
	r := newRepo(t)
	t.Setenv(LockTimeoutEnv, "0.1")
	path := filepath.Join(r.dataDir, "shared", "sample.json")
	operation, _ := r.profileOperationLockPath("default")
	for _, legacy := range []string{path + ".lock", operation} {
		noError(t, os.MkdirAll(filepath.Dir(legacy), 0700))
		noError(t, os.WriteFile(legacy, []byte("python sentinel"), 0600))
		stamp := time.Unix(1700000000, 0)
		noError(t, os.Chtimes(legacy, stamp, stamp))
		before := must(os.Stat(legacy))(t)
		_, err := r.WriteSharedJSON(bg, "sample.json", map[string]any{})
		noError(t, err)
		_, err = r.WriteProfileText(bg, "default", "artifact", []byte("complete"))
		noError(t, err)
		after := must(os.Stat(legacy))(t)
		if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || string(must(os.ReadFile(legacy))(t)) != "python sentinel" {
			t.Fatal("legacy resource lock changed")
		}
	}
	_, release, err := acquireFile(bg, path)
	noError(t, err)
	defer release()
	_, _, err = acquireFile(bg, path)
	if !errors.Is(err, lockx.ErrTimeout) {
		t.Fatal("leaf timeout lost", err)
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	_, _, err = acquireFile(ctx, path)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	newPath := filepath.Join(r.dataDir, "shared", "new.json")
	_, err = r.WriteSharedJSON(bg, "new.json", map[string]any{})
	noError(t, err)
	if _, err := os.Stat(newPath + ".lock"); !os.IsNotExist(err) {
		t.Fatal("created file lock", err)
	}
}
