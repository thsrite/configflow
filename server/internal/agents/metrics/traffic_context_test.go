package metrics

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"configflow/internal/ctxx"
)

func TestTrafficWorkCancellationKeepsPendingAndRollsBack(t *testing.T) {
	h := NewTrafficHistory(t.TempDir())
	now := float64(1700000000)
	_ = h.AddTraffic(context.Background(), "machine", &CleanedTraffic{Usage: []UsageRow{{Bucket: 1699999200, Source: "192.0.2.5", Chain: "DIRECT", Up: 3}}})
	parent, cancel := context.WithCancel(context.Background())
	ctx := ctxx.WithParent(context.Background(), parent)
	cancel()
	if _, err := h.Flush(ctx, DefaultRetention, &now); !errors.Is(err, context.Canceled) {
		t.Fatalf("flush: %v", err)
	}
	if err := h.Delete(ctx, "machine"); !errors.Is(err, context.Canceled) {
		t.Fatalf("delete: %v", err)
	}
	if _, err := h.RetainAgents(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("retain: %v", err)
	}
	if h.PendingCount() != 1 {
		t.Fatal("cancelled work discarded pending report")
	}
	if _, err := os.Stat(h.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled work created database: %v", err)
	}
	locker, err := h.connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer locker.close()
	if err = locker.begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer locker.rollback(context.Background())
	entered := make(chan struct{})
	h.connectHook = func() (*trafficConn, error) {
		db, err := sql.Open("sqlite", h.Path)
		if err != nil {
			return nil, err
		}
		conn, err := db.Conn(context.Background())
		if err != nil {
			db.Close()
			return nil, err
		}
		c := &trafficConn{db: db, conn: conn}
		if _, err = c.exec(context.Background(), "PRAGMA busy_timeout=1000"); err != nil {
			c.close()
			return nil, err
		}
		close(entered)
		return c, nil
	}
	parent, cancel = context.WithCancel(context.Background())
	defer cancel()
	ctx = ctxx.WithParent(context.Background(), parent)
	done := make(chan error, 1)
	go func() { _, err := h.Flush(ctx, DefaultRetention, &now); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("flush did not open SQLite")
	}
	select {
	case err := <-done:
		t.Fatalf("write ignored lock: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("in-flight flush: %v", err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("cancelled SQLite write did not return")
	}
	h.connectHook = nil
	if h.PendingCount() != 1 {
		t.Fatal("failed flush lost pending increment")
	}
	var count int
	if err := locker.conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM traffic_usage").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial write: %d %v", count, err)
	}
	if err := locker.rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Flush(context.Background(), DefaultRetention, &now); err != nil {
		t.Fatal(err)
	}
	if h.PendingCount() != 0 {
		t.Fatal("retry did not consume pending increment")
	}
	var up int
	if err := locker.conn.QueryRowContext(context.Background(), "SELECT SUM(up) FROM traffic_usage").Scan(&up); err != nil || up != 3 {
		t.Fatalf("retry persisted %d: %v", up, err)
	}
}

func TestHistoryWorkCancellationPreservesFile(t *testing.T) {
	h := newTestHistory(t, t.TempDir(), 10)
	path := h.agentFile("machine")
	if err := os.WriteFile(path, []byte("history"), 0600); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.DeleteAgentHistory(ctxx.WithParent(context.Background(), parent), "machine"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "history" {
		t.Fatalf("cancelled deletion: %q %v", raw, err)
	}
}
