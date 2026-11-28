//go:build linux

package trafficstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"
)

func TestIdleReportOnlyChangesPersistedStatus(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	day := naive(2026, 11, 20, 12)
	next := day.Add(time.Minute)
	seed := mustDecodeReport(t, map[string]any{
		"schema": 1, "status": "running", "mihomo_version": "v1",
		"items": []any{map[string]any{"host": "a.test", "rule": "MATCH", "policy": "P", "outlet": "direct", "conns": 1}},
	})
	if _, err := store.Add(t.Context(), "a", seed, day); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.Path("a"))
	if err != nil {
		t.Fatal(err)
	}
	readCalls := trackReads(store)
	store.LoadDocument("a", day) // discovery reading the history must not discard write eligibility
	idle := report(t, "running")
	status, err := store.Add(t.Context(), "a", idle, &next)
	if err != nil || status == nil || string(status.MihomoVersion) != `"v1"` || len(*readCalls) != 0 {
		t.Fatalf("empty report: %v, %v, history reads %v", status, err, *readCalls)
	}
	after, err := os.ReadFile(store.Path("a"))
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Replace(before, []byte(`"at":"2026-11-20T12:00:00"`), []byte(`"at":"2026-11-20T12:01:00"`), 1)
	if bytes.Equal(before, after) || !bytes.Equal(after, want) {
		t.Fatal("the persisted heartbeat must update precisely the timestamp and preserve every other byte")
	}
	ResetCache() // a restarted panel reads the same single-file format
	var disk Status
	if err := json.Unmarshal(store.StatusJSON("a"), &disk); err != nil || disk.At != status.At || !bytes.Equal(disk.MihomoVersion, status.MihomoVersion) {
		t.Fatalf("persisted status: %v, %v", disk, err)
	}
}

func TestIdleReportRechecksChangedFilesCountersAndDays(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	first := naive(2026, 11, 20, 12)
	idle := report(t, "running")
	if _, err := store.Add(t.Context(), "a", idle, first); err != nil {
		t.Fatal(err)
	}
	calls := trackReads(store)
	next := first.Add(time.Minute)
	counted := *idle
	counted.Dropped = big.NewInt(3)
	if _, err := store.Add(t.Context(), "a", &counted, &next); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 || string(store.LoadDocument("a", &next).Days()["2026-11-20"].Dropped) != "3" {
		t.Fatal("an empty item list with a nonzero counter must be merged")
	}
	later := first.AddDate(0, 0, RetentionDays)
	if _, err := store.Add(t.Context(), "a", idle, &later); err != nil {
		t.Fatal(err)
	}
	days := store.LoadDocument("a", &later).Days()
	if _, ok := days["2026-11-27"]; !ok || len(days) != 1 {
		t.Fatalf("retention after rollover: %v", sortedKeys(days))
	}
	// Keep size and mtime while replacing today's valid host object with an invalid value.
	old, err := os.ReadFile(store.Path("a"))
	if err != nil {
		t.Fatal(err)
	}
	bad := bytes.Replace(old, []byte(`"hosts":{}`), []byte(`"hosts":[]`), 1)
	if bytes.Equal(old, bad) {
		t.Fatal("missing host fixture")
	}
	rewriteBytes(t, store.Path("a"), bad, true)
	if _, err := store.Add(t.Context(), "a", idle, &later); !errors.Is(err, ErrInvalid) {
		t.Fatalf("damaged replacement accepted: %v", err)
	}
}

func TestIdleVerificationRejectsEditedTail(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "verify-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header := []byte(`{"status":{"at":"updated"}`)
	tail := []byte(`,"days":{}}`)
	contents := append(bytes.Clone(header), tail...)
	if _, err := f.Write(contents); err != nil {
		t.Fatal(err)
	}
	history := &idleHistory{digest: sha256.Sum256(tail)}
	if sig, err := verifyIdleFile(f, header, history); err != nil || sig == nil {
		t.Fatalf("original: signature %v, error %v", sig, err)
	}
	if _, err := f.WriteAt([]byte("X"), int64(len(header)+2)); err != nil {
		t.Fatal(err)
	}
	if sig, err := verifyIdleFile(f, header, history); err != nil || sig != nil {
		t.Fatalf("edited history: signature %v, error %v", sig, err)
	}
}

func TestIdleReportWithLegacyFieldOrder(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	at := naive(2026, 11, 20, 12)
	raw := []byte(`{"extension":{"keep":true},"days":{},"status":null}`)
	if err := os.WriteFile(store.Path("a"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	r := report(t, "running")
	if _, err := store.Add(t.Context(), "a", r, at); err != nil {
		t.Fatal(err)
	}
	calls := trackReads(store)
	next := at.Add(time.Minute)
	if _, err := store.Add(t.Context(), "a", r, &next); err != nil || len(*calls) != 0 {
		t.Fatalf("legacy field order: reads=%v err=%v", *calls, err)
	}
	persisted, err := os.ReadFile(store.Path("a"))
	if err != nil || !bytes.HasPrefix(persisted, []byte(`{"extension":{"keep":true},"days":`)) || !bytes.Contains(persisted, []byte(`"at":"2026-11-20T12:01:00"`)) {
		t.Fatalf("legacy fields or heartbeat lost: %v", err)
	}
	status := []byte(`{"value":"running"}`)
	if h := newIdleHistory([]byte(`{"extra":{"status":{"value":"running"}},"status":{"value":"running"}}`), status, "2026-11-20"); h != nil {
		t.Fatal("an ambiguous status offset must fall back to the full merge")
	}
}

func TestIdleReplacementCancellationLeavesOriginalFile(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	first := naive(2026, 11, 20, 12)
	status, err := store.Add(t.Context(), "a", report(t, "running"), first)
	if err != nil {
		t.Fatal(err)
	}
	path := store.absPath("a")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	history := newIdleHistory(raw, previous, "2026-11-20")
	status.At = "2026-11-20T12:05:00"
	next, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, committed, err := replaceIdleStatus(ctx, path, *statusSignature(path), next, history); !errors.Is(err, context.Canceled) || committed {
		t.Fatalf("canceled publication: committed=%v err=%v", committed, err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("canceled heartbeat changed file: %v", err)
	}
}
