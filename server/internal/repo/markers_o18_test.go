package repo

import (
	"encoding/json"
	"testing"

	"configflow/internal/store"
)

// markerSet and markerGet keep a test marker in a declared text member: plain
// records (O18 T1c) do not carry undeclared members.
func markerSet(target *string, raw json.RawMessage) error {
	*target = string(raw)
	return nil
}

func markerGet(raw json.RawMessage) (json.RawMessage, bool) {
	return raw, len(raw) > 0
}

// storeSharedRows writes shared rows as given, bypassing the strict input
// decode: plain records refuse wrong-typed input (O18 T1c), so stored dirty
// data for lenient-read tests is written to the store directly.
func storeSharedRows(t *testing.T, r *Repository, rows map[string]string) {
	t.Helper()
	base := must(r.store.Load())(t)
	for name, value := range rows {
		found := false
		for i := range base.Document.Shared.Fields {
			if base.Document.Shared.Fields[i].Name == name {
				base.Document.Shared.Fields[i].Value = json.RawMessage(value)
				found = true
			}
		}
		if !found {
			base.Document.Shared.Fields = append(base.Document.Shared.Fields, store.Field{Name: name, Value: json.RawMessage(value)})
		}
	}
	_, _, err := r.store.Commit(t.Context(), base, base.Document, nil)
	noError(t, err)
	r.dropCache()
}
