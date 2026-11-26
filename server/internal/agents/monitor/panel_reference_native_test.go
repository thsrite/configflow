package monitor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonresp"
	"configflow/internal/testjson"
)

func nativePanelExpectation(t *testing.T, c map[string]any, got any) bool {
	t.Helper()
	data, err := os.ReadFile("testdata/n810_panel_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ArchiveSHA256 string          `json:"archive_sha256"`
		Row           json.RawMessage `json:"row"`
		RowSHA256     string          `json:"row_sha256"`
		NativeText    string          `json:"native_text"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile("testdata/a07_reference.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != fixture.ArchiveSHA256 {
		t.Fatal("Python archive changed")
	}
	raw, err := jsonresp.Canonical(fixture.Row)
	if err != nil {
		t.Fatal(err)
	}
	bound := sha256.Sum256(raw)
	if hex.EncodeToString(bound[:]) != fixture.RowSHA256 {
		t.Fatal("native row binding changed")
	}
	raw, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = jsonresp.Canonical(raw)
	if err != nil {
		t.Fatal(err)
	}
	if sum = sha256.Sum256(raw); sum != bound {
		return false
	}
	expectedText := strings.ReplaceAll(fixture.NativeText, `:Infinity`, `:1e400`)
	expected, err := testjson.Parse([]byte(expectedText))
	if err != nil {
		t.Fatal(err)
	}
	if diff := testjson.Diff(expected, got); diff != "" {
		t.Fatalf("nonstandard JSON reply: %s", diff)
	}
	return true
}
