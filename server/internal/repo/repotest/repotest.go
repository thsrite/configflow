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
	"testing"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"
)

// WriteRow stores value as the row scope/owner/field of the database at
// path, replacing the row or appending it after the owner's last row, as one
// commit: the generation advances and the commit id changes, so an open
// repository reads the row again instead of its cached document.
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
