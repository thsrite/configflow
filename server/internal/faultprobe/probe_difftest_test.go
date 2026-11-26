//go:build configflow_difftest

package faultprobe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func control(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte(strings.Repeat("a", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ControlEnv, dir)
	return dir
}
func TestPointCancelledAndUnregistered(t *testing.T) {
	dir := control(t)
	if MaxWait != 30*time.Second {
		t.Fatal("production barrier maximum changed")
	}
	if err := os.WriteFile(filepath.Join(dir, "pause."+string(StoreBeforeCommit)), nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	Point(ctx, StoreBeforeCommit)
	data, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil || !strings.Contains(string(data), "cancelled") {
		t.Fatalf("%s %v", data, err)
	}
	Point(t.Context(), Phase("../../unregistered"))
	if len(Active()) != 0 {
		t.Fatal("phase lease leaked")
	}
}
func TestTokenRequiresPrivateRegularFile(t *testing.T) {
	dir := control(t)
	if _, err := Token(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "token"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Token(); err == nil {
		t.Fatal("public token accepted")
	}
}

func TestPointConcurrentObserversReleaseTogether(t *testing.T) {
	dir := control(t)
	pause := filepath.Join(dir, "pause."+string(ProviderRender))
	if err := os.WriteFile(pause, nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	for range 32 {
		workers.Go(func() { Point(ctx, ProviderRender) })
	}
	// The event transcript proves all callers reached the barrier before release.
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		data, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
		if err == nil && strings.Count(string(data), `"event":"entered"`) == 32 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("concurrent observers did not reach the barrier")
		case <-tick.C:
		}
	}
	if active := Active(); len(active) != 1 || active[0] != ProviderRender {
		t.Fatalf("active phases: %v", active)
	}
	if err := os.Remove(pause); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	if active := Active(); len(active) != 0 {
		t.Fatalf("released observers still active: %v", active)
	}
}
