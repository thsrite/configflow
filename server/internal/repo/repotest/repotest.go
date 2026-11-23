// Package repotest writes stored rows directly for tests that need data the
// plain records refuse as input (values of the wrong type, undeclared
// members), as an older version or a hand edit could have left them. It does
// not decide what the repository accepts.
package repotest

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/repo/staterows"
	"configflow/internal/store"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"
)

// WriteRow stores value as the row scope/owner/field of the database at
// path, replacing the row or appending it after the owner's last row, as one
// commit: the generation advances and the commit id changes, so an open
// repository reads the row again instead of its cached document. A value for
// system/agents is the complete registry: the Agent state rows
// (agent_state) are removed, so the merged registry reads exactly value.
func WriteRow(t testing.TB, path, scope, owner, field string, value json.RawMessage) {
	t.Helper()
	// mode=rw: the repository must have created the database already.
	db, err := sql.Open("sqlite", "file:"+path+"?mode=rw")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var text string
	if err := tx.QueryRow("SELECT value FROM meta WHERE key='generation'").Scan(&text); err != nil {
		t.Fatal(err)
	}
	var generation int64
	if err := json.Unmarshal([]byte(text), &generation); err != nil {
		t.Fatalf("generation %q: %v", text, err)
	}
	generation++
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	commit, _ := json.Marshal(hex.EncodeToString(id))
	statements := []struct {
		query string
		args  []any
	}{
		{"UPDATE meta SET value=? WHERE key='generation'", []any{string(mustJSON(t, generation))}},
		{"INSERT INTO meta(key, value) VALUES('commit_id', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", []any{string(commit)}},
	}
	for _, s := range statements {
		if _, err := tx.Exec(s.query, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	if scope == "system" && owner == "" && field == "agents" {
		clearState(t, tx)
	}
	result, err := tx.Exec("UPDATE fields SET value=?, revision=? WHERE scope=? AND owner=? AND field=?", string(value), generation, scope, owner, field)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil {
		t.Fatal(err)
	} else if n == 0 {
		if _, err := tx.Exec("INSERT INTO fields(scope, owner, field, position, value, revision) VALUES(?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM fields WHERE scope=? AND owner=?), ?, ?)",
			scope, owner, field, scope, owner, string(value), generation); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// clearState removes every agent_state row inside tx (a version-1 file has
// no table) and advances the state counters when it removed any.
func clearState(t testing.TB, tx *sql.Tx) {
	t.Helper()
	var tables int
	if err := tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='agent_state'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables == 0 {
		return
	}
	result, err := tx.Exec("DELETE FROM agent_state")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return
	}
	var text string
	var state int64
	if err := tx.QueryRow("SELECT value FROM meta WHERE key='state_generation'").Scan(&text); err == nil {
		_ = json.Unmarshal([]byte(text), &state)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	commit, _ := json.Marshal(hex.EncodeToString(id))
	for key, value := range map[string]string{"state_generation": string(mustJSON(t, state+1)), "state_commit_id": string(commit)} {
		if _, err := tx.Exec("INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value); err != nil {
			t.Fatal(err)
		}
	}
}

// DowngradeToV1 rewrites the version-2 database at path as storage version 1,
// the layout Python and older Go read: the Agent state rows go back into the
// system.agents row (staterows.Merge) and the agent_state table and its
// counters are removed. Tests use it to build version-1 inputs for the
// upgrade and to hand a Go-written database to the Python reference.
func DowngradeToV1(t testing.TB, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=rw")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var tables int
	if err := tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='agent_state'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables > 0 {
		var rows []store.StateRow
		result, err := tx.Query("SELECT agent_id, member, value FROM agent_state ORDER BY agent_id, member")
		if err != nil {
			t.Fatal(err)
		}
		for result.Next() {
			var row store.StateRow
			var value string
			if err := result.Scan(&row.Owner, &row.Member, &value); err != nil {
				t.Fatal(err)
			}
			row.Value = json.RawMessage(value)
			rows = append(rows, row)
		}
		if err := result.Close(); err != nil {
			t.Fatal(err)
		}
		var agents string
		switch err := tx.QueryRow("SELECT value FROM fields WHERE scope='system' AND owner='' AND field='agents'").Scan(&agents); {
		case err == nil:
			if merged := staterows.Merge([]byte(agents), rows); string(merged) != agents {
				if _, err := tx.Exec("UPDATE fields SET value=? WHERE scope='system' AND owner='' AND field='agents'", string(merged)); err != nil {
					t.Fatal(err)
				}
			}
		case errors.Is(err, sql.ErrNoRows):
		default:
			t.Fatal(err)
		}
		if _, err := tx.Exec("DROP TABLE agent_state"); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		"DELETE FROM meta WHERE key IN ('state_generation', 'state_commit_id')",
		"UPDATE meta SET value='1' WHERE key='storage_version'",
	} {
		if _, err := tx.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
