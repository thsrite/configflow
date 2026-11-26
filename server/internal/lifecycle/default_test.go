package lifecycle_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

func TestProcessRuntimeBehavior(t *testing.T) {
	if os.Getenv("CONFIGFLOW_LIFECYCLE_HELPER") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessRuntimeBehavior$", "-test.count=1")
		cmd.Env = append(os.Environ(), "CONFIGFLOW_LIFECYCLE_HELPER=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("process runtime helper: %v\n%s", err, out)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "routes")
	t.Setenv("PRODDIFF_ROUTES", path)
	// This value must never act as a production loop-disable switch.
	t.Setenv("CONFIGFLOW_LIFECYCLE_MODE", "manual")
	var startups, drains atomic.Int64
	ran := make(chan struct{}, 4)
	rt := lifecycle.NewProcessRuntime(lifecycle.Options{
		Mode:  lifecycle.ModeManual,
		Drain: func(context.Context) error { drains.Add(1); return nil },
	})
	rt.RegisterStartup(lifecycle.Startup{Name: "once", Run: func(context.Context) error { startups.Add(1); return nil }})
	rt.RegisterPeriodic(lifecycle.Periodic{Name: "periodic", Interval: func(context.Context) time.Duration { return time.Hour }, Round: func(context.Context) error { ran <- struct{}{}; return nil }})
	for i := int64(1); i <= 2; i++ {
		rt.Start(context.Background())
		rt.ReportReady()
		if manualDefaultBuild {
			select {
			case <-ran:
				t.Fatal("proddiff launched periodic loop")
			default:
			}
		} else {
			receive(t, ran)
		}
		rt.Stop(time.Second)
		rt.Stop(time.Second)
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
	rt.Start(context.Background())
	if !manualDefaultBuild {
		receive(t, ran)
	}
	started := make(chan struct{})
	finished := make(chan struct{})
	bound := rt.BindRequest(context.Background())
	if err := (lifecycle.ContextSpawner{}).Spawn(bound, "bound-instance", func(ctx context.Context) { close(started); <-lifecycle.Stopping(ctx); close(finished) }); err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	select {
	case <-finished:
		t.Fatal("Spawn exited before runtime stop")
	default:
	}
	rt.Stop(time.Second)
	receive(t, finished)
	if drains.Load() != 3 || startups.Load() != 3 {
		t.Fatalf("runtime drain/startup counts: %d/%d", drains.Load(), startups.Load())
	}
}

func TestStoppingOutsideRuntimeIsNil(t *testing.T) {
	if ch := lifecycle.Stopping(context.Background()); ch != nil {
		t.Fatalf("Stopping outside runtime = %v, want nil", ch)
	}
}
