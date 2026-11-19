package monitor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
)

func nativePanelExpectation(t *testing.T, c *jsonx.Object, got any) bool {
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
	raw, err = jsonx.Compact(c)
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
	expected, err := jsonx.ParseString(fixture.NativeText)
	if err != nil {
		t.Fatal(err)
	}
	if sortedDump(t, got) != sortedDump(t, expected) {
		t.Fatalf("nonstandard JSON reply: got %s want %s", sortedDump(t, got), sortedDump(t, expected))
	}
	return true
}
