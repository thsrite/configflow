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
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	db, err := sql.Open("sqlite", fileURI(path, "rw"))
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
		routerRows, err := tx.Query("SELECT owner, value FROM fields WHERE scope='profile' AND field='routers'")
		if err != nil {
			t.Fatal(err)
		}
		type profileRouters struct{ id, value string }
		var profiles []profileRouters
		for routerRows.Next() {
			var item profileRouters
			if err := routerRows.Scan(&item.id, &item.value); err != nil {
				routerRows.Close()
				t.Fatal(err)
			}
			profiles = append(profiles, item)
		}
		if err := routerRows.Close(); err != nil {
			t.Fatal(err)
		}
		for _, item := range profiles {
			if merged := staterows.MergeRouters(item.id, []byte(item.value), rows); string(merged) != item.value {
				if _, err := tx.Exec("UPDATE fields SET value=? WHERE scope='profile' AND owner=? AND field='routers'", string(merged), item.id); err != nil {
					t.Fatal(err)
				}
			}
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

// PythonLayout prepares a Go data directory for the Python reference, which
// reads storage version 1 only: a version-2 configflow.sqlite3 is rewritten
// with DowngradeToV1. A missing or version-1 database is left alone. Go
// upgrades the file again when it next opens the directory.
func PythonLayout(t testing.TB, dataDir string) {
	t.Helper()
	path := filepath.Join(dataDir, "configflow.sqlite3")
	if _, err := os.Stat(path); err != nil {
		return
	}
	db, err := sql.Open("sqlite", fileURI(path, "ro"))
	if err != nil {
		t.Fatal(err)
	}
	var version string
	err = db.QueryRow("SELECT value FROM meta WHERE key='storage_version'").Scan(&version)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if version != "1" {
		DowngradeToV1(t, path)
	}
}

// MergedLoad loads the database at path like store.Load and shows it as the
// version-1 rows Python keeps: the Agent state rows merged into
// system.agents and Document.State empty. Comparisons of whole stored
// documents with the Python reference use it.
func MergedLoad(path string) (store.Entry, error) {
	s := store.New(path, func() time.Duration { return 10 * time.Second })
	defer s.Close()
	entry, err := s.Load()
	if err != nil || len(entry.Document.State) == 0 {
		if err == nil {
			entry.Document.State = nil
		}
		return entry, err
	}
	document := *entry.Document
	if document.System != nil {
		fields := append([]store.Field(nil), document.System.Fields...)
		for i := range fields {
			if fields[i].Name == "agents" {
				fields[i].Value = staterows.Merge(fields[i].Value, document.State)
			}
		}
		document.System = &store.Section{Fields: fields}
	}
	if len(document.Profiles) > 0 {
		profiles := make([]store.ProfileRows, len(document.Profiles))
		for i, profile := range document.Profiles {
			fields := append([]store.Field(nil), profile.Fields...)
			for j := range fields {
				if fields[j].Name == "routers" {
					fields[j].Value = staterows.MergeRouters(profile.ID, fields[j].Value, document.State)
				}
			}
			profiles[i] = store.ProfileRows{ID: profile.ID, Fields: fields}
		}
		document.Profiles = profiles
	}
	document.State = nil
	entry.Document = &document
	return entry, nil
}

// fileURI is the SQLite URI of path: test directories may hold "#", "%" or
// "?" (subtest names), which a plain "file:"+path would misread.
func fileURI(path, mode string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String() + "?mode=" + mode
}

// MergedAgentsRow returns the system.agents row text of the database at path
// with its Agent state rows merged in (staterows.Merge): the row as Python
// stores it. value is the stored configuration row text.
func MergedAgentsRow(t testing.TB, path string, value string) string {
	t.Helper()
	db, err := sql.Open("sqlite", fileURI(path, "ro"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='agent_state'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables == 0 {
		return value
	}
	result, err := db.Query("SELECT agent_id, member, value FROM agent_state ORDER BY agent_id, member")
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	var rows []store.StateRow
	for result.Next() {
		var row store.StateRow
		var text string
		if err := result.Scan(&row.Owner, &row.Member, &text); err != nil {
			t.Fatal(err)
		}
		row.Value = json.RawMessage(text)
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	return string(staterows.Merge([]byte(value), rows))
}
