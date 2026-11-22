package monitor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"configflow/internal/jsonresp"
)

// nativeHARefusal binds complete, unchanged Python rows. Only the six reviewed
// malformed storage cases use a native refusal; all other rows keep their oracle.
func nativeHARefusal(t *testing.T, row json.RawMessage, got error) bool {
	t.Helper()
	data, err := os.ReadFile("testdata/n810_ha_refusals.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ArchiveSHA256 string `json:"archive_sha256"`
		Rows          []struct {
			Row         json.RawMessage `json:"row"`
			SHA256      string          `json:"sha256"`
			NativeError string          `json:"native_error"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile("testdata/a07_review.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != fixture.ArchiveSHA256 {
		t.Fatal("Python archive changed")
	}
	canonical, err := jsonresp.Canonical(row)
	if err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(canonical)
	for _, item := range fixture.Rows {
		expected, err := jsonresp.Canonical(item.Row)
		if err != nil {
			t.Fatal(err)
		}
		bound := sha256.Sum256(expected)
		if hex.EncodeToString(bound[:]) != item.SHA256 {
			t.Fatal("native refusal row binding changed")
		}
		if bound != sum {
			continue
		}
		if got != nil && got.Error() != item.NativeError {
			t.Fatalf("native HA refusal: %v, want %s", got, item.NativeError)
		}
		return true
	}
	return false
}
