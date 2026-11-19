package metrics

import (
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"
)

func TestNativeTrafficReportExactKeysAndBounds(t *testing.T) {
	at := float64(1700000000)
	valid := `{"schema":2,"usage":[{"hour":1699999200,"source":"192.0.2.1","chain":["G","DIRECT"],"up":4611686018427387903,"down":2,"conns":3}],"domains":[]}`
	tests := []struct {
		name, raw string
		valid     bool
	}{
		{"valid", valid, true}, {"wrong case", `{"Schema":2,"usage":[],"domains":[]}`, false}, {"duplicate last", `{"schema":2,"schema":9,"usage":[],"domains":[]}`, false},
		{"null", `null`, false}, {"not object", `[]`, false}, {"malformed", `{`, false}, {"counter overflow", `{"schema":2,"usage":[{"hour":1699999200,"source":"s","chain":[],"up":4611686018427387904,"down":0,"conns":0}],"domains":[]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecodeTrafficReport([]byte(tt.raw), &at)
			if (got != nil) != tt.valid {
				t.Fatal(got)
			}
		})
	}
}
func TestNativeServicesShutdownAndDisconnect(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(t.Context())
	defer runtime.Shutdown(context.Background())
	request, cancel := context.WithCancel(runtime.BindRequest(context.Background()))
	cancel()
	history := NewTrafficHistory(t.TempDir())
	report := &CleanedTraffic{Usage: []UsageRow{{Bucket: 1699999200, Source: "192.0.2.1", Up: 1}}}
	if err := history.AddTraffic(request, "a", report); err != nil {
		t.Fatal(err)
	}
	at := float64(1700000000)
	if _, err := history.FlushContext(request, 90, &at); err != nil {
		t.Fatal(err)
	}
	metrics := NewHistory(t.TempDir(), 4000)
	if _, err := metrics.AddSample(request, "a", []byte(`{"cpu":{"usage_percent":1}}`)); err != nil {
		t.Fatal(err)
	}
	bound := runtime.BindRequest(context.Background())
	runtime.Shutdown(context.Background())
	if err := history.AddTraffic(bound, "a", report); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := history.FlushContext(bound, 90, &at); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := metrics.FlushContext(bound); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if metrics.PendingCount() != 1 {
		t.Fatal("shutdown discarded pending metrics")
	}
	if _, err := os.Stat(metrics.agentFile("a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
func TestNativeSummaryIsolatesCorruptionAndThrottlesWarnings(t *testing.T) {
	h := NewHistory(t.TempDir(), 4000)
	sample := json.RawMessage(`{"cpu":{"usage_percent":12},"disk":[],"network":{"bytes_sent":"bad"}}`)
	for i := 0; i < 3; i++ {
		if _, err := h.AddSample(t.Context(), "a", sample); err != nil {
			t.Fatal(err)
		}
		summary, err := h.Summarize("a", big.NewInt(1))
		if err != nil || string(summary.CPU.Avg) != "12.0" {
			t.Fatal(summary, err)
		}
		stats, err := h.TrafficStatistics("a", "total")
		if err != nil || stats.DataPoints != nil {
			t.Fatal(stats, err)
		}
	}
	if len(h.warnings.last) != 2 {
		t.Fatal("warning keys", h.warnings.last)
	}
	if h.warnings.allow("a", "disk") {
		t.Fatal("same-field warning not throttled")
	}
	if !h.warnings.allow("b", "disk") {
		t.Fatal("different object suppressed")
	}
}
func TestNativeQuerySkipsDamagedSQLiteCounter(t *testing.T) {
	h := NewTrafficHistory(t.TempDir())
	at := float64(1700000000)
	c, err := h.connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	for _, args := range [][]any{{"a", int64(1699999200), int64(Hour), "good", "DIRECT", int64(4), int64(2), int64(1)}, {"a", int64(1699999200), int64(Hour), "broken", "DIRECT", "broken", int64(2), int64(1)}} {
		if _, err = c.exec("INSERT INTO traffic_usage VALUES(?,?,?,?,?,?,?,?)", args...); err != nil {
			t.Fatal(err)
		}
	}
	query, err := h.QueryTraffic(t.Context(), "a", "24h", "source", &at)
	if err != nil || string(query.Total.Up) != "4" || len(query.Rows) != 1 {
		t.Fatal(query, err)
	}
}
func FuzzNativeTrafficReport(f *testing.F) {
	f.Add([]byte(`{"schema":2,"usage":[],"domains":[]}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"Schema":2}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		at := float64(1700000000)
		r := DecodeTrafficReport(raw, &at)
		if r == nil {
			return
		}
		for _, row := range r.Usage {
			if row.Bucket%Hour != 0 || row.Up < 0 || row.Down < 0 || row.Conns < 0 {
				t.Fatal(row)
			}
		}
		if len(r.Usage)+len(r.Targets) > TrafficReportMaxItems {
			t.Fatal("report limit")
		}
		encoded, err := json.Marshal(r)
		if err != nil || !json.Valid(encoded) {
			t.Fatal(err)
		}
		var out CleanedTraffic
		if jsondecode.Unmarshal(encoded, &out) != nil {
			t.Fatal("DTO round trip")
		}
	})
}

func TestNativeTrafficShutdownWhileWaitingForOwnerLock(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(t.Context())
	defer runtime.Shutdown(context.Background())
	bound := runtime.BindRequest(context.Background())
	h := NewTrafficHistory(t.TempDir())
	h.mu.Lock()
	done := make(chan error, 1)
	entered := make(chan struct{})
	go func() {
		close(entered)
		done <- h.AddTraffic(bound, "a", &CleanedTraffic{Usage: []UsageRow{{Bucket: 1699999200, Up: 1}}})
	}()
	<-entered
	select {
	case err := <-done:
		h.mu.Unlock()
		t.Fatal("write ignored owner lock", err)
	case <-time.After(20 * time.Millisecond):
	}
	runtime.Shutdown(context.Background())
	h.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if h.PendingCount() != 0 {
		t.Fatal("shutdown lock waiter appended")
	}
}

func TestNativeSQLWritesRejectCorruptCountersWithoutCoercion(t *testing.T) {
	for _, scenario := range []string{"upsert", "compact_usage", "compact_targets"} {
		t.Run(scenario, func(t *testing.T) {
			h := NewTrafficHistory(t.TempDir())
			at := float64(1700000000)
			bucket := int64(1699999200)
			span := int64(Hour)
			table := "traffic_usage"
			if scenario != "upsert" {
				bucket -= 10 * Day
			}
			if scenario == "compact_targets" {
				table = "traffic_targets"
			}
			c, err := h.connect()
			if err != nil {
				t.Fatal(err)
			}
			defer c.close()
			statement := "INSERT INTO " + table + " VALUES(?,?,?,?,?,?,?,?)"
			args := []any{"a", bucket, span, "source", "DIRECT", "bad", int64(2), int64(1)}
			if table == "traffic_targets" {
				statement = "INSERT INTO " + table + " VALUES(?,?,?,?,?,?,?,?,?)"
				args = []any{"a", bucket, span, "source", "example.com", "DIRECT", "bad", int64(2), int64(1)}
			}
			if _, err = c.exec(statement, args...); err != nil {
				t.Fatal(err)
			}
			pendingBucket := bucket
			if scenario != "upsert" {
				pendingBucket = 1699999200
			}
			if err = h.AddTraffic(t.Context(), "a", &CleanedTraffic{Usage: []UsageRow{{Bucket: pendingBucket, Source: "source", Chain: "DIRECT", Up: 1}}}); err != nil {
				t.Fatal(err)
			}
			if _, err = h.FlushContext(t.Context(), 90, &at); err == nil {
				t.Fatal("corrupt counter was silently coerced")
			}
			if h.PendingCount() != 1 {
				t.Fatal("failed write lost pending report")
			}
			var value any
			if err = c.conn.QueryRowContext(t.Context(), "SELECT up FROM "+table+" WHERE agent_id='a'").Scan(&value); err != nil || value != "bad" {
				t.Fatal("corrupt row changed", value, err)
			}
		})
	}
}

func TestNativeSQLCompactionOverflowRollsBackAndClearRecovers(t *testing.T) {
	h := NewTrafficHistory(t.TempDir())
	at := float64(1700000000)
	oldDay := int64(at)/Day*Day - 10*Day
	c, err := h.connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	for _, bucket := range []int64{oldDay, oldDay + Hour} {
		if _, err = c.exec("INSERT INTO traffic_usage VALUES(?,?,?,?,?,?,?,?)", "broken", bucket, int64(Hour), "source", "DIRECT", 1e308, int64(0), int64(1)); err != nil {
			t.Fatal(err)
		}
	}
	if err = h.AddTraffic(t.Context(), "healthy", &CleanedTraffic{Usage: []UsageRow{{Bucket: 1699999200, Source: "source", Chain: "DIRECT", Up: 4}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.FlushContext(t.Context(), 90, &at); err == nil {
		t.Fatal("REAL SUM overflow silently committed")
	}
	var count int
	if err = c.conn.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM traffic_usage WHERE agent_id='broken' AND up=1e308").Scan(&count); err != nil || count != 2 || h.PendingCount() != 1 {
		t.Fatal("rollback changed counters", count, h.PendingCount(), err)
	}
	// The existing per-Agent traffic-history reset uses this deletion owner.
	if err = h.DeleteContext(t.Context(), "broken"); err != nil {
		t.Fatal(err)
	}
	if _, err = h.FlushContext(t.Context(), 90, &at); err != nil {
		t.Fatal("clear did not recover", err)
	}
	query, err := h.QueryTraffic(t.Context(), "healthy", "24h", "source", &at)
	if err != nil || string(query.Total.Up) != "4" {
		t.Fatal(query, err)
	}
}
