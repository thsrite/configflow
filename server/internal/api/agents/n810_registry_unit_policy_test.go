package agents_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func n810UnitCanonical(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func nativeRegistryShape(t *testing.T, body any, old string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_registry_unit_policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Original map[string]any `json:"original"`
		SHA      string         `json:"sha256"`
		Native   string         `json:"native_message"`
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	original := map[string]any{"body": body, "python": map[string]any{"status": 500, "body": map[string]any{"success": false, "message": old}}}
	for _, row := range rows {
		if fmt.Sprintf("%x", sha256.Sum256(n810UnitCanonical(t, row.Original))) != row.SHA {
			t.Fatal("original registry unit row changed")
		}
		if bytes.Equal(n810UnitCanonical(t, original), n810UnitCanonical(t, row.Original)) {
			return map[string]any{"success": false, "message": row.Native}
		}
	}
	t.Fatal("unbound original registry unit input/500 result")
	return nil
}

func nativeLogVersionStatus(t *testing.T, request any, oldStatus int) int {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_log_version_policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Original map[string]any `json:"original"`
		SHA      string         `json:"sha256"`
		Status   int            `json:"native_status"`
	}
	if err = json.Unmarshal(raw, &row); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(n810UnitCanonical(t, row.Original))) != row.SHA || !bytes.Equal(n810UnitCanonical(t, row.Original), n810UnitCanonical(t, map[string]any{"request": request, "python_status": oldStatus})) {
		t.Fatal("complete original log/version request and status changed")
	}
	return row.Status
}
