package trafficstore

import (
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"
)

func decodeCleanedReport(raw json.RawMessage) (*Report, error) {
	var f model.Fields
	if err := jsondecode.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if !f.Has("schema") {
		if err := f.Set("schema", json.RawMessage("1")); err != nil {
			return nil, err
		}
	}
	dropped := member(f, "dropped")
	if len(dropped) == 0 {
		dropped = []byte("0")
	}
	n, ok := new(big.Int).SetString(string(dropped), 10)
	if !ok || n.Sign() < 0 {
		return nil, &TypeError{Message: "invalid cleaned dropped counter"}
	}
	if err := f.Set("dropped", json.RawMessage("0")); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	report, err := DecodeReport(raw)
	if report != nil {
		report.Dropped = n
	}
	return report, err
}

func nativeReport(t *testing.T) *Report {
	t.Helper()
	r, err := DecodeReport([]byte(`{"schema":1,"status":"running","items":[{"host":"a.example.com","policy":"P","outlet":"direct","rule":"MATCH","conns":2}]}`))
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	return r
}
func TestNativeStoreIsolatesBucketsAndRecoversThroughExistingClear(t *testing.T) {
	s := NewNative(t.TempDir())
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	bad := `{"status":null,"future":{"n":2.00},"days":{"2026-11-19":{"ip_only_conns":0,"dropped":0,"hosts":[]},"2026-11-18":{"ip_only_conns":0,"dropped":0,"hosts":{}}}}`
	if err := os.WriteFile(s.Path("a"), []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Add(t.Context(), "a", nativeReport(t), &at); err == nil {
			t.Fatal("bad bucket written")
		}
		raw, _ := os.ReadFile(s.Path("a"))
		if string(raw) != bad {
			t.Fatal("refusal modified original")
		}
	}
	yesterday := at.AddDate(0, 0, -1)
	if _, err := s.Add(t.Context(), "a", nativeReport(t), &yesterday); err != nil {
		t.Fatal("other date blocked", err)
	}
	if _, err := s.Add(t.Context(), "b", nativeReport(t), &at); err != nil {
		t.Fatal("other Agent blocked", err)
	}
	doc := s.LoadDocument("a", &at)
	if len(doc.Days()) != 1 {
		t.Fatal("bad date not isolated", doc.Days())
	}
	raw, _ := os.ReadFile(s.Path("a"))
	var fields map[string]json.RawMessage
	json.Unmarshal(raw, &fields)
	if string(fields["future"]) != `{"n":2.00}` {
		t.Fatal("unknown data lost")
	}
	// The existing domain-discovery clear route calls this exact removal owner.
	if err := s.DeleteContext(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(t.Context(), "a", nativeReport(t), &at); err != nil {
		t.Fatal("clear did not recover", err)
	}
	if len(s.LoadDocument("a", &at).Days()) != 1 {
		t.Fatal("fresh report missing")
	}
}
func TestNativeStoreRetentionDropsOldBrokenBucket(t *testing.T) {
	s := NewNative(t.TempDir())
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	if err := os.WriteFile(s.Path("a"), []byte(`{"status":null,"days":{"2026-10-01":{"hosts":[]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(t.Context(), "a", nativeReport(t), &at); err != nil {
		t.Fatal(err)
	}
	if len(s.LoadDocument("a", &at).Days()) != 1 {
		t.Fatal("expired bucket retained")
	}
}
func TestNativeStoreRuntimeLifetime(t *testing.T) {
	s := NewNative(t.TempDir())
	r := nativeReport(t)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(t.Context())
	defer runtime.Shutdown(context.Background())
	request, cancel := context.WithCancel(runtime.BindRequest(context.Background()))
	cancel()
	if _, err := s.Add(request, "a", r, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.Path("a"))
	bound := runtime.BindRequest(context.Background())
	runtime.Shutdown(context.Background())
	if _, err := s.Add(bound, "a", r, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(s.Path("a"))
	if string(before) != string(after) {
		t.Fatal("shutdown changed file")
	}
}
func TestNativeReportExactKeysAndClone(t *testing.T) {
	tests := []struct {
		raw  string
		good bool
	}{{`{"schema":1,"status":"running"}`, true}, {`{"Schema":1,"status":"running"}`, false}, {`{"schema":1,"Status":"running"}`, false}, {`{"schema":1,"status":"running","status":"unknown"}`, false}, {`{"schema":1,"status":"running","dropped":9007199254740993}`, false}, {`null`, false}, {`[]`, false}, {`{`, false}}
	for _, tt := range tests {
		r, err := DecodeReport([]byte(tt.raw))
		if (err == nil && r != nil) != tt.good {
			t.Fatal(tt.raw, r, err)
		}
	}
}
func FuzzNativeDiscoveryReport(f *testing.F) {
	f.Add([]byte(`{"schema":1,"status":"running","items":[]}`))
	f.Add([]byte(`{"schema":1,"status":[]}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		r, err := DecodeReport(raw)
		if err != nil || r == nil {
			return
		}
		encoded, err := json.Marshal(r)
		if err != nil || !json.Valid(encoded) {
			t.Fatal(err)
		}
		copy, err := decodeCleanedReport(encoded)
		if err != nil || copy == nil || copy.Dropped.Cmp(r.Dropped) != 0 || len(copy.Items) != len(r.Items) {
			t.Fatal("cleaned report round trip", err)
		}
	})
}

func TestNativeStoreShutdownAfterOwnerLockWait(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(t.Context())
	defer runtime.Shutdown(context.Background())
	bound := runtime.BindRequest(context.Background())
	s := NewNative(t.TempDir())
	report := nativeReport(t)
	mu.Lock()
	done := make(chan error, 1)
	entered := make(chan struct{})
	go func() { close(entered); _, err := s.Add(bound, "a", report, nil); done <- err }()
	<-entered
	select {
	case err := <-done:
		mu.Unlock()
		t.Fatal("write ignored owner lock", err)
	case <-time.After(20 * time.Millisecond):
	}
	runtime.Shutdown(context.Background())
	mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path("a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("shutdown lock waiter wrote", err)
	}
}

func TestNativeReportEscapedASCIIAndNegativeZero(t *testing.T) {
	r, err := DecodeReport([]byte(`{"schema":1,"status":"running","items":[{"host":"a.example.com","policy":"P","outlet":"direct","rule":"MATCH","network":"t\u0063p","port":-0,"conns":-0}]}`))
	if err != nil || r == nil || len(r.Items) != 1 || r.Items[0].Port != 0 || r.Items[0].Counts[0].Sign() != 0 {
		t.Fatal(r, err)
	}
}

func TestNativeReadKeepsPartialLegacyBuckets(t *testing.T) {
	s := NewNative(t.TempDir())
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	raw := `{"status":{"value":"running"},"days":{"2026-11-19":{"hosts":{"a.example.com":{"first_seen":"2026-11-19T12:00:00","last_seen":"2026-11-19T12:00:00","by_route":{"MATCH\t\tdirect\tP":{"conns":1}}}}}}}`
	if err := os.WriteFile(s.Path("a"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	day := s.LoadDocument("a", &at).Days()["2026-11-19"]
	if len(day.Hosts) != 1 || string(day.Hosts["a.example.com"].ByRoute["MATCH\t\tdirect\tP"].Conns) != "1" {
		t.Fatal("missing optional counters erased valid observations", day)
	}
}

func TestNativeReportRepeatedMembersKeepLastValue(t *testing.T) {
	raw := json.RawMessage(`{"schema":9,"schema":1,"status":"running","items":[{"host":"a.example.com","port":443,"port":80,"outlet":"direct","policy":"p","rule":"Match","fail_kinds":{"timeout":9,"timeout":2}}]}`)
	report, err := DecodeReport(raw)
	if err != nil || report == nil || len(report.Items) != 1 || report.Items[0].Port != 80 || report.Items[0].FailKinds["timeout"].Int64() != 2 {
		t.Fatalf("report: %+v, %v", report, err)
	}
	store := NewNative(t.TempDir())
	if _, err := store.Add(context.Background(), "a", report, nil); err != nil {
		t.Fatal(err)
	}
}

func TestNativeStoreInvalidFileMatchesCorruptDaysAndResetRecovers(t *testing.T) {
	for _, content := range []string{`{"days":[]}`, "{", "", " \n ", "[]", "null", `"old"`, "false", "42", `{"days":{},"broken":NaN}`} {
		t.Run(content, func(t *testing.T) {
			s := NewNative(t.TempDir())
			at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
			// Warm metadata first: a later corrupt replacement must evict it.
			if _, err := s.Add(t.Context(), "a", nativeReport(t), &at); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.Path("a"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			corruptionWarnings.Lock()
			before := len(corruptionWarnings.last)
			corruptionWarnings.Unlock()
			for i := 0; i < 2; i++ {
				_, err := s.Add(t.Context(), "a", nativeReport(t), &at)
				var shape *TypeError
				if !errors.As(err, &shape) || shape.Message != "invalid traffic days" {
					t.Fatalf("must match corrupt days error: %v", err)
				}
				raw, err := os.ReadFile(s.Path("a"))
				if err != nil || string(raw) != content {
					t.Fatalf("refusal changed original: %q %v", raw, err)
				}
			}
			corruptionWarnings.Lock()
			warnings := len(corruptionWarnings.last) - before
			corruptionWarnings.Unlock()
			if warnings != 1 {
				t.Fatalf("repeated refusal warning count=%d", warnings)
			}
			if s.StatusJSON("a") != nil || len(s.LoadDocument("a", &at).Days()) != 0 {
				t.Fatal("bad root differs from bad days read behavior")
			}
			raw, err := os.ReadFile(s.Path("a"))
			if err != nil || string(raw) != content {
				t.Fatal("read rewrote original", err)
			}
			// Negative control: the same store can write an actually missing file.
			if _, err := s.Add(t.Context(), "healthy", nativeReport(t), &at); err != nil {
				t.Fatal("missing file blocked", err)
			}
			if err := s.DeleteContext(t.Context(), "a"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Add(t.Context(), "a", nativeReport(t), &at); err != nil {
				t.Fatal("reset did not recover", err)
			}
			if len(s.LoadDocument("a", &at).Days()) != 1 {
				t.Fatal("post-reset report missing")
			}
		})
	}
}

func TestInspectStatusDetectsDamageCachesSignatureAndRecovers(t *testing.T) {
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	restoreNow := SetNow(func() time.Time { return at })
	defer restoreNow()

	tests := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"truncated", "{"},
		{"array_root", "[]"},
		{"null_root", "null"},
		{"bad_days", `{"status":{"value":"running"},"days":[]}`},
		{"bad_status_type", `{"status":[],"days":{}}`},
		{"bad_status_member", `{"status":{"value":"running","mihomo_version":123},"days":{}}`},
		{"nonfinite", `{"status":{"value":"running"},"days":{},"broken":NaN}`},
		{"bad_today_hosts", `{"status":{"value":"running","at":"2026-11-19T10:00:00"},"days":{"2026-11-19":{"ip_only_conns":0,"dropped":0,"hosts":[]}}}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolated(t)
			s := NewNative(t.TempDir())
			calls := trackReads(s)

			// Missing file is healthy and never triggers a read.
			snap := s.InspectStatus("a")
			if snap.State != StoreStateOK || snap.Status != nil || len(*calls) != 0 {
				t.Fatalf("missing file: snap=%+v reads=%v", snap, *calls)
			}

			// Healthy write warms statusCache; InspectStatus serves from cache with zero reads.
			if _, err := s.Add(t.Context(), "a", nativeReport(t), &at); err != nil {
				t.Fatal(err)
			}
			beforeReads := len(*calls)
			snap = s.InspectStatus("a")
			if snap.State != StoreStateOK || len(*calls) != beforeReads {
				t.Fatalf("cached healthy inspect: snap=%+v reads=%d->%d", snap, beforeReads, len(*calls))
			}

			// Invalid incoming report must not falsely mark a healthy store damaged.
			if _, err := s.Add(t.Context(), "a", &Report{}, &at); err == nil {
				t.Fatal("expected error for nil counters report")
			}
			if snap = s.InspectStatus("a"); snap.State != StoreStateOK {
				t.Fatalf("healthy store falsely marked damaged: %+v", snap)
			}

			// Corrupt file on disk: first inspect reads once, second reuses damagedCache with zero reads.
			if err := os.WriteFile(s.Path("a"), []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			*calls = nil
			snap = s.InspectStatus("a")
			if snap.State != StoreStateDamaged || len(*calls) != 1 || len(CachedPaths()) != 0 {
				t.Fatalf("first corrupt inspect: snap=%+v reads=%v cached=%v", snap, *calls, CachedPaths())
			}
			snap = s.InspectStatus("a")
			if snap.State != StoreStateDamaged || len(*calls) != 1 {
				t.Fatalf("second corrupt inspect should hit signature cache: snap=%+v reads=%v", snap, *calls)
			}
			if raw, err := os.ReadFile(s.Path("a")); err != nil || string(raw) != tc.content {
				t.Fatalf("read-only inspect modified file: %q %v", raw, err)
			}
			if _, err := s.Add(t.Context(), "a", nativeReport(t), &at); err == nil {
				t.Fatal("corrupt file unexpectedly accepted write")
			}

			// Delete recovers store state to ok and allows new writes.
			if err := s.DeleteContext(t.Context(), "a"); err != nil {
				t.Fatal(err)
			}
			if snap = s.InspectStatus("a"); snap.State != StoreStateOK || snap.Status != nil {
				t.Fatalf("post-clear inspect: %+v", snap)
			}
			if _, err := s.Add(t.Context(), "a", nativeReport(t), &at); err != nil {
				t.Fatalf("post-clear add failed: %v", err)
			}
			if snap = s.InspectStatus("a"); snap.State != StoreStateOK || len(snap.Status) == 0 {
				t.Fatalf("post-recovery inspect: %+v", snap)
			}
		})
	}
}

func TestInspectStatusDayBucketDamageClearsNextDay(t *testing.T) {
	isolated(t)
	current := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	restoreNow := SetNow(func() time.Time { return current })
	defer restoreNow()

	s := NewNative(t.TempDir())
	raw := `{"status":{"value":"running","at":"2026-11-19T10:00:00"},"days":{"2026-11-19":{"ip_only_conns":0,"dropped":0,"hosts":[]}}}`
	if err := os.WriteFile(s.Path("a"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if snap := s.InspectStatus("a"); snap.State != StoreStateDamaged {
		t.Fatalf("expected damaged on same day: %+v", snap)
	}
	current = current.AddDate(0, 0, 1)
	if snap := s.InspectStatus("a"); snap.State != StoreStateOK {
		t.Fatalf("expected ok after date rollover: %+v", snap)
	}
	if _, err := s.Add(t.Context(), "a", nativeReport(t), &current); err != nil {
		t.Fatalf("next-day add should succeed: %v", err)
	}
}
