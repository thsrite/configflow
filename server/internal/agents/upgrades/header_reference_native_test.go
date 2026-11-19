package upgrades

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

// Header error texts stay in their original archive. Only these four complete
// input/output rows use native refusal/classification rather than CPython bytes.
func verifyNativeHeaderReference(t *testing.T, value string, want any, encoded string, err error) {
	t.Helper()
	hashes := map[string]string{
		"Bearer 密钥":    "051f7f93c7b086757209629304a9bfdaac34bc76c86225cdde4dc2c80adfa4b6",
		"Bearer 密x":    "ecca3f5bf4f652b63d7250e4d97449b69417324fa48ffea35d0d9330ae3df57b",
		"Bearer tö😀":   "403fda24192a7c912ca8d52269d4eab90131224d735ba59c3126ab4ab5754806",
		"Bearer a密钥b密": "4a4f2a437ff7a09ec38f017627cd23564c893c0a964d93a3005bd465b47906c8",
	}
	archive, readErr := os.ReadFile("testdata/a15_review.json")
	if readErr != nil || fmt.Sprintf("%x", sha256.Sum256(archive)) != "c3f3dfe2ea56a8940328d95dd8d2e8058c00c3539a7b7309b6eb99e2b458fb4c" {
		t.Fatal("original upgrade archive changed", readErr)
	}
	row := jsonx.Obj("value", value, "python_class", "UnicodeEncodeError", "python_message", want)
	raw, encodeErr := jsonx.Compact(row)
	if encodeErr != nil || hashes[value] == "" || fmt.Sprintf("%x", sha256.Sum256(raw)) != hashes[value] {
		t.Fatal("unbound original header row", value)
	}
	if !errors.Is(err, agents.ErrHeaderEncoding) || ErrorClass(err) != "UnicodeEncodeError" || agents.IsTransportError(err) || encoded != "" {
		t.Fatalf("native header refusal=%v encoded=%q", err, encoded)
	}
}
