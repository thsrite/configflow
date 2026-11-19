package upgrades

import (
	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
)

type nativeReferenceRow struct {
	Group    string          `json:"group"`
	Key      string          `json:"key"`
	Original json.RawMessage `json:"original"`
	SHA      string          `json:"row_sha256"`
	Field    string          `json:"native_field"`
}

func nativeReference(t *testing.T, group, key string, original *jsonx.Object) *nativeReferenceRow {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var archive struct {
		SHA  string               `json:"archive_sha256"`
		Rows []nativeReferenceRow `json:"rows"`
	}
	if err := json.Unmarshal(raw, &archive); err != nil {
		t.Fatal(err)
	}
	originalArchive, err := os.ReadFile("testdata/a15_review.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(originalArchive)) != archive.SHA || archive.SHA != "c3f3dfe2ea56a8940328d95dd8d2e8058c00c3539a7b7309b6eb99e2b458fb4c" {
		t.Fatal("original archive changed", err)
	}
	for _, row := range archive.Rows {
		if row.Group != group || row.Key != key {
			continue
		}
		stored, err := jsonx.Parse(row.Original)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := jsonx.Compact(stored)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(encoded)) != row.SHA {
			t.Fatal("unbound stored row", group, key)
		}
		actual, err := jsonx.Compact(original)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(actual)) != row.SHA {
			return nil
		}
		return &row
	}
	return nil
}
func verifyNativeCorruptReference(t *testing.T, row *nativeReferenceRow, err error) {
	t.Helper()
	var corrupt *CorruptStateError
	if !errors.As(err, &corrupt) || corrupt.Field != row.Field {
		t.Fatalf("native refusal %v; want damaged %s", err, row.Field)
	}
}
func TestN810ReferenceBindingNegativeControl(t *testing.T) {
	row := reviewReference(t).GetList("poll")[0].(*jsonx.Object)
	if nativeReference(t, "poll", row.GetString("name"), row) == nil {
		t.Fatal("positive control did not bind")
	}
	altered := row.DeepCopy()
	altered.Set("record", jsonx.Obj("status", "succeeded"))
	if nativeReference(t, "poll", row.GetString("name"), altered) != nil {
		t.Fatal("changed input still selected native policy")
	}
}
func verifyNativeCredentialReference(t *testing.T, name string, token any, want *jsonx.Object, got string, err error) bool {
	t.Helper()
	row := nativeReference(t, "header", name, jsonx.Obj("name", name, "token", token, "python", want))
	if row == nil {
		return false
	}
	if got != "" || !errors.Is(err, agents.ErrAgentEndpoint) {
		t.Fatalf("wrong-type credential sent a request or was not refused: %q %v", got, err)
	}
	return true
}
