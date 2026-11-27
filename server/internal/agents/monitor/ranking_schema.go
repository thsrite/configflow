package monitor

import (
	"strings"
)

const rankingRowColumns = " (" +
	" agent_id TEXT NOT NULL, recorder TEXT NOT NULL, start INTEGER NOT NULL, seconds INTEGER NOT NULL," +
	" kind TEXT NOT NULL, key TEXT NOT NULL, count INTEGER NOT NULL," +
	" PRIMARY KEY (agent_id, recorder, start, seconds, kind, key))"

// The private ranking database had user_version 0 in all rowid releases.
// 1 marks a committed rebuild whose free pages still need to be returned to disk.
const rankingVacuumPending = 1

func compactRankingRows(c *rankConn) error {
	var version, withoutRowID int
	if err := c.queryRow("SELECT user_version, wr FROM pragma_user_version, pragma_table_list"+
		" WHERE schema='main' AND name='ranking_rows'").Scan(&version, &withoutRowID); err != nil {
		return err
	}
	if withoutRowID == 0 {
		if version != 0 {
			return nil
		}
		var err error
		version, err = rebuildRankingRows(c)
		if err != nil {
			return err
		}
	}
	if version != rankingVacuumPending {
		return nil
	}
	// VACUUM manages its own transaction. Shutdown or a full disk leaves a
	// usable compact table and a durable indication to retry next maintenance.
	if _, err := c.conn.ExecContext(c.ctx, "VACUUM"); err != nil {
		return err
	}
	_, err := c.conn.ExecContext(c.ctx, "PRAGMA user_version=2")
	return err
}

func rebuildRankingRows(c *rankConn) (int, error) {
	tx, err := c.conn.BeginTx(c.ctx, nil) // BEGIN IMMEDIATE also serializes two panel processes.
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var ddl string
	var version, extras int
	if err := tx.QueryRowContext(c.ctx, "SELECT sql, user_version,"+
		" (SELECT COUNT(*) FROM sqlite_schema WHERE tbl_name='ranking_rows' AND type IN ('index','trigger') AND sql IS NOT NULL)"+
		" FROM sqlite_schema, pragma_user_version WHERE type='table' AND name='ranking_rows'").Scan(&ddl, &version, &extras); err != nil {
		return 0, err
	}
	// Only rebuild the released schema. Preserve extensions, custom indexes and triggers.
	normalize := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }
	if version != 0 || extras != 0 || normalize(ddl) != normalize("CREATE TABLE ranking_rows"+rankingRowColumns) {
		return version, nil
	}
	for _, stmt := range []string{
		"CREATE TABLE ranking_rows_compact" + rankingRowColumns + " WITHOUT ROWID",
		"INSERT INTO ranking_rows_compact SELECT agent_id, recorder, start, seconds, kind, key, count FROM ranking_rows" +
			" ORDER BY agent_id, recorder, start, seconds, kind, key",
		"DROP TABLE ranking_rows",
		"ALTER TABLE ranking_rows_compact RENAME TO ranking_rows",
		"PRAGMA user_version=1",
	} {
		if _, err := tx.ExecContext(c.ctx, stmt); err != nil {
			return 0, err
		}
	}
	return rankingVacuumPending, tx.Commit()
}
