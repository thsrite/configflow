package lifecycle_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

func TestDefaultRuntimeProcess(t *testing.T) {
	if os.Getenv("CONFIGFLOW_LIFECYCLE_HELPER") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDefaultRuntimeProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "CONFIGFLOW_LIFECYCLE_HELPER=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("default runtime helper: %v\n%s", err, out)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "routes")
	t.Setenv("PRODDIFF_ROUTES", path)
	// This value must never act as a production loop-disable switch.
	t.Setenv("CONFIGFLOW_LIFECYCLE_MODE", "manual")
	var startups, drains atomic.Int64
	ran := make(chan struct{}, 4)
	lifecycle.RegisterStartup(lifecycle.Startup{Name: "once", Run: func(context.Context) error { startups.Add(1); return nil }})
	lifecycle.Loop("legacy", func(ctx context.Context) { ran <- struct{}{}; <-ctx.Done() })
	if err := lifecycle.Configure(lifecycle.Options{Mode: lifecycle.ModeManual}); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 2; i++ {
		lifecycle.Start(context.Background())
		if manualDefaultBuild {
			select {
			case <-ran:
				t.Fatal("proddiff launched legacy loop")
			default:
			}
		} else {
			receive(t, ran)
		}
		if err := lifecycle.Configure(lifecycle.Options{}); !errors.Is(err, lifecycle.ErrRunning) {
			t.Fatalf("running reconfigure: %v", err)
		}
		lifecycle.Stop(time.Second)
		lifecycle.Stop(time.Second)
		if startups.Load() != i {
			t.Fatalf("startup once/restart: %d", startups.Load())
		}
	}
	if manualDefaultBuild {
		if _, err := os.Stat(path + ".ready"); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path + ".startups")
		if err != nil || strings.TrimSpace(string(data)) != "once" {
			t.Fatalf("startup metadata: %s %v", data, err)
		}
	} else if _, err := os.Stat(path + ".ready"); !os.IsNotExist(err) {
		t.Fatalf("production wrote test metadata: %v", err)
	}
	// Retain Wire's registry while replacing the configured instance. Global
	// Spawn and the zero-value adapter must track jobs on this replacement.
	if err := lifecycle.Configure(lifecycle.Options{Drain: func(context.Context) error { drains.Add(1); return nil }}); err != nil {
		t.Fatal(err)
	}
	lifecycle.Start(context.Background())
	if !manualDefaultBuild {
		receive(t, ran)
	}
	started := make(chan struct{})
	finished := make(chan struct{})
	if err := (lifecycle.DefaultSpawner{}).Spawn(context.Background(), "new-instance", func(ctx context.Context) { close(started); <-lifecycle.Stopping(ctx); close(finished) }); err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	select {
	case <-finished:
		t.Fatal("Spawn still bound to stopped instance")
	default:
	}
	lifecycle.Stop(time.Second)
	receive(t, finished)
	if drains.Load() != 1 || startups.Load() != 3 {
		t.Fatalf("Configure dropped registry or Drain: %d/%d", drains.Load(), startups.Load())
	}
	if err := lifecycle.Configure(lifecycle.Options{Grace: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	gate, finish := releaseGate(t)
	oldStarted := make(chan struct{})
	oldDone := make(chan struct{})
	if err := lifecycle.Spawn(context.Background(), "ignores-cancel", func(context.Context) { close(oldStarted); <-gate; close(oldDone) }); err != nil {
		t.Fatal(err)
	}
	receive(t, oldStarted)
	lifecycle.Stop(time.Second)
	if err := lifecycle.Configure(lifecycle.Options{}); !errors.Is(err, lifecycle.ErrRunning) {
		finish()
		t.Fatalf("Configure abandoned active old work: %v", err)
	}
	lifecycle.Start(context.Background())
	if !manualDefaultBuild {
		receive(t, ran)
	}
	lifecycle.Stop(time.Second)
	if err := lifecycle.Configure(lifecycle.Options{}); !errors.Is(err, lifecycle.ErrRunning) {
		finish()
		t.Fatalf("Configure abandoned earlier-generation work after restart: %v", err)
	}
	finish()
	receive(t, oldDone)
	until(t, func() bool { return lifecycle.Configure(lifecycle.Options{}) == nil })
}
