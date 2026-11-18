//go:build configflow_difftest

package faultprobe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
