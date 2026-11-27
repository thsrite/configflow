package monitor

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"modernc.org/sqlite"
)

func legacyRankingStore(t *testing.T) (*RankingStore, *rankConn) {
	t.Helper()
	s := NewRankingStore(t.TempDir())
	c, err := s.connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.close)
	execRankingSQL(t, c, "DROP TABLE ranking_rows", "CREATE TABLE ranking_rows"+rankingRowColumns)
	return s, c
}

func execRankingSQL(t *testing.T, c *rankConn, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := c.conn.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

func checkRankingLayout(t *testing.T, c *rankConn, wr, version int) {
	t.Helper()
	var gotWR, gotVersion int
	if err := c.queryRow("SELECT wr, user_version FROM pragma_table_list, pragma_user_version WHERE name='ranking_rows'").Scan(&gotWR, &gotVersion); err != nil {
		t.Fatal(err)
	}
	if gotWR != wr || gotVersion != version {
		t.Fatalf("WITHOUT ROWID=%d version=%d, want %d/%d", gotWR, gotVersion, wr, version)
	}
}

func TestRankingUpgradePreservesResultsAndExistingWriters(t *testing.T) {
	s, c := legacyRankingStore(t)
	const start int64 = 1700002800
	slot := &Slot{Records: 7, Rows: map[string]*Counts{"domains": {Keys: []string{"a.test", "b.test"}, Values: map[string]int64{"a.test": 4, "b.test": 3}}}}
	if added, err := s.StoreSlot(t.Context(), "agent", "dns", start, slot); err != nil || !added {
		t.Fatal(added, err)
	}
	read := func() *HistoryAnswer {
		t.Helper()
		reopened := NewRankingStore(filepath.Dir(filepath.Dir(s.Path)))
		answer, err := reopened.Query(t.Context(), "agent", "dns", start, start+HistoryHour)
		if err != nil {
			t.Fatal(err)
		}
		return answer
	}
	before := read()
	if before.Records != 7 || len(before.Rows["domains"]) != 2 {
		t.Fatalf("bad legacy fixture: %+v", before)
	}
	checkRankingLayout(t, c, 0, 0) // user reads do not trigger migration
	if err := s.Maintain(t.Context(), []string{"agent"}, i64(start+HistoryHour)); err != nil {
		t.Fatal(err)
	}
	checkRankingLayout(t, c, 1, 2)
	if after := read(); !reflect.DeepEqual(before, after) {
		t.Fatalf("ranking changed: %+v -> %+v", before, after)
	}
	if added, err := s.StoreSlot(t.Context(), "agent", "dns", start, slot); err != nil || added {
		t.Fatalf("duplicate slot after upgrade: %v, %v", added, err)
	}
	if added, err := s.StoreSlot(t.Context(), "agent", "dns", start+900, slot); err != nil || !added {
		t.Fatalf("new slot after upgrade: %v, %v", added, err)
	}
	if answer := read(); answer.Records != 14 || answer.Rows["domains"][0].Count != 8 {
		t.Fatalf("upsert after upgrade: %+v", answer)
	}
	var schemaBefore, schemaAfter int
	if err := c.queryRow("PRAGMA schema_version").Scan(&schemaBefore); err != nil {
		t.Fatal(err)
	}
	if err := compactRankingRows(c); err != nil {
		t.Fatal(err)
	}
	if err := c.queryRow("PRAGMA schema_version").Scan(&schemaAfter); err != nil || schemaBefore != schemaAfter {
		t.Fatalf("repeat maintenance changed schema from %d to %d: %v", schemaBefore, schemaAfter, err)
	}
}

func TestRankingUpgradeRollbackAndVacuumResume(t *testing.T) {
	s, c := legacyRankingStore(t)
	execRankingSQL(t, c, "INSERT INTO ranking_rows VALUES ('a','r',0,3600,'domains','kept',9)",
		"CREATE TABLE ranking_rows_compact(sentinel TEXT)")
	if err := compactRankingRows(c); err == nil {
		t.Fatal("staging table collision should abort the rebuild")
	}
	checkRankingLayout(t, c, 0, 0)
	var count int
	if err := c.queryRow("SELECT count FROM ranking_rows WHERE key='kept'").Scan(&count); err != nil || count != 9 {
		t.Fatal("original row lost", err)
	}
	execRankingSQL(t, c, "DROP TABLE ranking_rows_compact")
	if pending, err := rebuildRankingRows(c); err != nil || pending != rankingVacuumPending {
		t.Fatalf("retry: %d %v", pending, err)
	}
	checkRankingLayout(t, c, 1, 1)
	// A process can stop after COMMIT and before VACUUM; a new connection resumes it.
	newConn, err := s.connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer newConn.close()
	if err := compactRankingRows(newConn); err != nil {
		t.Fatal(err)
	}
	checkRankingLayout(t, newConn, 1, 2)
}

func TestRankingUpgradeKeepsCustomSchema(t *testing.T) {
	_, c := legacyRankingStore(t)
	execRankingSQL(t, c, "ALTER TABLE ranking_rows ADD COLUMN custom TEXT", "CREATE INDEX custom_keys ON ranking_rows(custom)")
	if err := compactRankingRows(c); err != nil {
		t.Fatal(err)
	}
	checkRankingLayout(t, c, 0, 0)
	execRankingSQL(t, c, "INSERT INTO ranking_rows VALUES ('a','r',0,3600,'domains','example',9,'kept')")
}

func TestRankingUpgradeFullDiskDuringCopy(t *testing.T) {
	_, c := legacyRankingStore(t)
	execRankingSQL(t, c, "WITH RECURSIVE numbers(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM numbers WHERE n<3000)"+
		" INSERT INTO ranking_rows SELECT 'agent','dns',0,3600,'domains',printf('domain-%0100d.test',n),n FROM numbers")
	var pages int
	if err := c.queryRow("PRAGMA page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	execRankingSQL(t, c, fmt.Sprintf("PRAGMA max_page_count=%d", pages+1))
	err := compactRankingRows(c)
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code() != 13 { // SQLITE_FULL
		t.Fatalf("expected SQLITE_FULL from the copy, got %v", err)
	}
	checkRankingLayout(t, c, 0, 0)
	var n, staging int
	if err := c.queryRow("SELECT count(*), (SELECT count(*) FROM sqlite_schema WHERE name='ranking_rows_compact') FROM ranking_rows").Scan(&n, &staging); err != nil || n != 3000 || staging != 0 {
		t.Fatalf("after failed copy: rows=%d staging tables=%d err=%v", n, staging, err)
	}
	execRankingSQL(t, c, "PRAGMA max_page_count=1000000")
	if err := compactRankingRows(c); err != nil {
		t.Fatal(err)
	}
	checkRankingLayout(t, c, 1, 2)
}
