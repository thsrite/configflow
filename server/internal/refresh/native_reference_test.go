package refresh

import (
	"configflow/internal/jsonx"
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"testing"
)

func loadNativeReferences(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_expected.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	refs := parsed.(*jsonx.Object)
	archive, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != refs.GetString("corpus_sha256") {
		t.Fatal("original corpus changed; independently re-record projections")
	}
	return refs
}
func nativeReference(t *testing.T, refs *jsonx.Object, index int, row *jsonx.Object) *jsonx.Object {
	t.Helper()
	ref := refs.GetObject("rows").GetObject(strconv.Itoa(index))
	if ref == nil {
		return expected(row)
	}
	if !jsonx.Equal(ref.Value("input"), row.Value("input")) || !jsonx.Equal(ref.Value("original"), expected(row)) {
		t.Fatalf("reference %d not bound to original input/output", index)
	}
	return ref.GetObject("native")
}
func TestNativeReferenceEvidence(t *testing.T) {
	refs := loadNativeReferences(t)
	if refs.GetObject("rows").Len() != 32 {
		t.Fatal("independent projection manifest changed")
	}
	original := refs.GetList("edge_original")
	first := original[0].(*jsonx.Object).GetObject("output").GetObject("s").GetList("history")
	if len(first) != 3 || first[0] != "a" || first[1] != "b" {
		t.Fatal("lost original string-iteration evidence")
	}
	if original[3].(*jsonx.Object).GetString("error") != "TypeError" {
		t.Fatal("lost original non-iterable history evidence")
	}
	// Per-row binding rejects altered archive indices and either expected value.
	for i, row := range loadCorpus(t) {
		_ = nativeReference(t, refs, i, row)
	}
}
