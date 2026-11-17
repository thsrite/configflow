package instance

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"configflow/internal/version"
)

func TestLockHolderProcess(t *testing.T) {
	names := os.Getenv("INSTANCE_TEST_LOCKS")
	if names == "" {
		return
	}
	for _, name := range strings.Split(names, ",") {
		path := filepath.Join(os.Getenv("INSTANCE_TEST_DIR"), filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func holdInProcess(t *testing.T, dir string, names []string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockHolderProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "INSTANCE_TEST_DIR="+dir, "INSTANCE_TEST_LOCKS="+strings.Join(names, ","))
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = input.Close()
			if err := cmd.Wait(); err != nil {
				t.Errorf("lock holder: %v", err)
			}
			cancel()
		})
	}
	t.Cleanup(stop)
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("lock holder readiness: %q %v", line, err)
	}
	return stop
}

func TestCrossProcessExclusionAndPartialRollback(t *testing.T) {
	tests := []struct {
		name  string
		locks []string
	}{{"Python lifetime locks", lockNames[1:]}}
	for _, name := range lockNames {
		tests = append(tests, struct {
			name  string
			locks []string
		}{name, []string{name}})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			stop := holdInProcess(t, dir, tt.locks)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			_, err := Acquire(ctx, dir)
			if !errors.Is(err, ErrInUse) || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("busy lock: %v", err)
			}
			if err.Error() != fmt.Sprintf(msgDirectoryInUse, dir) {
				t.Fatalf("busy diagnostic: %q", err)
			}
			stop()
			// Any partial acquisition by the failed attempt must have been released.
			owner, err := Acquire(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			owner.Release()
		})
	}
}

func TestDiagnosticsAndPersistentLockFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range lockNames {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("old diagnostic\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := Acquire(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var releases sync.WaitGroup
	for range 10 {
		releases.Go(owner.Release)
	}
	releases.Wait()
	for n, name := range lockNames {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			if string(data) != "old diagnostic\n" {
				t.Fatalf("legacy lock modified: %s", name)
			}
			continue
		}
		parts := strings.Fields(string(data))
		if len(parts) != 3 || parts[0] != strconv.Itoa(os.Getpid()) || parts[1] != version.Version {
			t.Fatalf("server diagnostic: %q", data)
		}
		if _, err := time.Parse(time.RFC3339Nano, parts[2]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRetryAndCancelledAcquisition(t *testing.T) {
	dir := t.TempDir()
	owner, err := Acquire(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Release()
	timer := time.AfterFunc(30*time.Millisecond, owner.Release)
	defer timer.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	next, err := Acquire(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	next.Release()
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := Acquire(cancelled, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire: %v", err)
	}
}
