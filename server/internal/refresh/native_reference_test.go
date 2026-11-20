package refresh

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"testing"

	"configflow/internal/testjson"
)

func loadNativeReferences(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_expected.json")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := decodeMap(data)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != mapStr(refs, "corpus_sha256") {
		t.Fatal("original corpus changed; independently re-record projections")
	}
	return refs
}

func nativeReference(t *testing.T, refs map[string]any, index int, row map[string]any) map[string]any {
	t.Helper()
	ref := mapObj(mapObj(refs, "rows"), strconv.Itoa(index))
	if ref == nil {
		return expected(row)
	}
	if !testjson.Equal(ref["input"], row["input"]) || !testjson.Equal(ref["original"], expected(row)) {
		t.Fatalf("reference %d not bound to original input/output", index)
	}
	return mapObj(ref, "native")
}

func TestNativeReferenceEvidence(t *testing.T) {
	refs := loadNativeReferences(t)
	if len(mapObj(refs, "rows")) != 32 {
		t.Fatal("independent projection manifest changed")
	}
	original := mapList(refs, "edge_original")
	first := mapList(mapObj(mapObj(original[0].(map[string]any), "output"), "s"), "history")
	if len(first) != 3 || first[0] != "a" || first[1] != "b" {
		t.Fatal("lost original string-iteration evidence")
	}
	if mapStr(original[3].(map[string]any), "error") != "TypeError" {
		t.Fatal("lost original non-iterable history evidence")
	}
	// Per-row binding rejects altered archive indices and either expected value.
	for i, row := range loadCorpus(t) {
		_ = nativeReference(t, refs, i, row)
	}
}
