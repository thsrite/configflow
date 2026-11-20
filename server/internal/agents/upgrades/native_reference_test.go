package upgrades

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"configflow/internal/agents"
)

type nativeReferenceRow struct {
	Group    string          `json:"group"`
	Key      string          `json:"key"`
	Original json.RawMessage `json:"original"`
	SHA      string          `json:"row_sha256"`
	Field    string          `json:"native_field"`
}

func compactRawJSON(raw json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func nativeReference(t *testing.T, group, key string, original json.RawMessage) *nativeReferenceRow {
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
		encoded, err := compactRawJSON(row.Original)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(encoded)) != row.SHA {
			t.Fatal("unbound stored row", group, key)
		}
		actual, err := compactRawJSON(original)
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
	ref := loadReviewReference(t)
	rowRaw := ref.Poll[0]
	var meta struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rowRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if nativeReference(t, "poll", meta.Name, rowRaw) == nil {
		t.Fatal("positive control did not bind")
	}
	var altered map[string]any
	if err := json.Unmarshal(rowRaw, &altered); err != nil {
		t.Fatal(err)
	}
	altered["record"] = map[string]any{"status": "succeeded"}
	alteredRaw, err := compactUnescapedJSON(altered)
	if err != nil {
		t.Fatal(err)
	}
	if nativeReference(t, "poll", meta.Name, alteredRaw) != nil {
		t.Fatal("changed input still selected native policy")
	}
}

func verifyNativeCredentialReference(t *testing.T, name string, token any, want json.RawMessage, got string, err error) bool {
	t.Helper()
	compactedWant, compactErr := compactRawJSON(want)
	if compactErr != nil {
		t.Fatal(compactErr)
	}
	raw, encodeErr := compactUnescapedJSON(struct {
		Name   string          `json:"name"`
		Token  any             `json:"token"`
		Python json.RawMessage `json:"python"`
	}{name, token, compactedWant})
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	row := nativeReference(t, "header", name, raw)
	if row == nil {
		return false
	}
	if got != "" || !errors.Is(err, agents.ErrAgentEndpoint) {
		t.Fatalf("wrong-type credential sent a request or was not refused: %q %v", got, err)
	}
	return true
}
