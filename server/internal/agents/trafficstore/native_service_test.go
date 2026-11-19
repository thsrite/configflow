package trafficstore

import (
	"configflow/internal/lifecycle"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

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
