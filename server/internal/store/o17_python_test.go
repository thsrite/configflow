package store

import (
	"testing"
	"time"
)

// Storage version 2 (O17) only adds the agent_state table and two meta
// counters; the store tests never write state rows. The Python reference
// reads version 1 only, so a Go-created file is handed to it in the version-1
// layout, and Go's dumps leave the version-2 additions out when comparing.

// pythonView rewrites a Go-created version-2 file without state rows as
// version 1 (a no-op for a version-1 file).
func pythonView(t *testing.T, path string) {
	t.Helper()
	connection := must(connect(path, false, 2*time.Second))(t)
	defer connection.close()
	present := must(connection.hasStateTable())(t)
	if !present {
		return
	}
	rows := must(connection.strings1("SELECT count(*) FROM agent_state"))(t)
	if len(rows) != 1 || rows[0] != "0" {
		t.Fatalf("a store test file has agent_state rows: %v", rows)
	}
	for _, statement := range []string{
		"DROP TABLE agent_state",
		"DELETE FROM meta WHERE key IN ('state_generation', 'state_commit_id')",
		"UPDATE meta SET value = '1' WHERE key = 'storage_version'",
	} {
		if err := connection.exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

// versionOneDump removes the version-2 additions from a goDump result.
func versionOneDump(dump *testDoc) *testDoc {
	schema, ok := dump.Value("schema").([]any)
	if !ok {
		return dump
	}
	hasState := false
	kept := []any{}
	for _, row := range schema {
		if values, ok := row.([]any); ok && len(values) > 1 && values[1] == "agent_state" {
			hasState = true
			continue
		}
		kept = append(kept, row)
	}
	if !hasState {
		return dump
	}
	dump.Set("schema", kept)
	meta := []any{}
	for _, row := range dump.GetList("meta") {
		values := row.([]any)
		switch values[0] {
		case "state_generation", "state_commit_id":
			continue
		case "storage_version":
			row = []any{values[0], "1"}
		}
		meta = append(meta, row)
	}
	dump.Set("meta", meta)
	return dump
}
