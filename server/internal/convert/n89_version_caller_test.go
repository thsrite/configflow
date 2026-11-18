package convert

import (
	"configflow/internal/jsonx"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// Full per-row/source hashes and historical Python results are additionally
// verified by TestN89CallerReferenceProvenance in the public-boundary package.
func nativeSubsVersionCases(t *testing.T, archive string) *jsonx.Object {
	t.Helper()
	fixture, err := os.ReadFile("testdata/n89_subs_callers_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsonx.Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	root := parsed.(*jsonx.Object)
	source, err := os.ReadFile("testdata/" + archive + ".jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != root.GetObject("sources").GetString(archive) {
		t.Fatal("Agent reference source drift")
	}
	return root.GetObject("cases")
}
func applyNativeSubsVersion(row *corpusRow, expected *jsonx.Object) {
	if expected == nil {
		return
	}
	failure := expected.GetList("native_error")
	row.errClass, row.errMsg = failure[0].(string), failure[1].(string)
	row.mutated, row.hasMut = expected.Value("native_mutated"), true
}
