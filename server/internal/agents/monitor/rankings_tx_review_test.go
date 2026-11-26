package monitor

import (
	"context"
	"database/sql"
	"testing"
)

// HR review batch-40: a StoreSlot whose last statement fails leaves neither
// the slot nor its period behind, and the next writer is not blocked by a
// transaction left open.
func TestReviewBatch40StoreSlotFailureRollsBackEverything(t *testing.T) {
	ctx := context.Background()
	store := NewRankingStore(t.TempDir())
	if _, err := store.StoreSlot(ctx, "a1", "r", 0, &Slot{Records: 1, Rows: map[string]*Counts{}}); err != nil {
		t.Fatal(err)
	}
	c, err := store.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.conn.ExecContext(ctx, "CREATE TRIGGER fail_rows BEFORE INSERT ON ranking_rows"+
		" WHEN NEW.key = 'boom' BEGIN SELECT RAISE(ABORT, 'forced'); END"); err != nil {
		t.Fatal(err)
	}
	c.close()

	slot := &Slot{Records: 3, Rows: map[string]*Counts{"domains": {Keys: []string{"ok", "boom"}, Values: map[string]int64{"ok": 1, "boom": 2}}}}
	if added, err := store.StoreSlot(ctx, "a1", "r", 900, slot); err == nil || added {
		t.Fatalf("StoreSlot = %v, %v; want the forced error", added, err)
	}
	count := func(query string) int64 {
		t.Helper()
		c, err := store.connect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer c.close()
		var n sql.NullInt64
		if err := c.conn.QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n.Int64
	}
	if n := count("SELECT COUNT(*) FROM ranking_slots WHERE start = 900"); n != 0 {
		t.Fatalf("slot row kept after the failure: %d", n)
	}
	if n := count("SELECT records FROM ranking_periods WHERE start = 0"); n != 1 {
		t.Fatalf("period records = %d, want 1 (the failed slot's 3 rolled back)", n)
	}
	if n := count("SELECT COUNT(*) FROM ranking_rows"); n != 0 {
		t.Fatalf("ranking rows kept after the failure: %d", n)
	}
	slot.Rows["domains"] = &Counts{Keys: []string{"ok"}, Values: map[string]int64{"ok": 1}}
	if added, err := store.StoreSlot(ctx, "a1", "r", 900, slot); err != nil || !added {
		t.Fatalf("retry StoreSlot = %v, %v", added, err)
	}
	if n := count("SELECT records FROM ranking_periods WHERE start = 0"); n != 4 {
		t.Fatalf("period records after retry = %d, want 4", n)
	}
}
