package convert_test

import (
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"testing"
)

func providerPathReferences(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_provider_paths.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	refs := parsed.(*jsonx.Object)
	archive, err := os.ReadFile("testdata/mihomo_corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != refs.GetString("archive_sha256") {
		t.Fatal("original provider corpus changed")
	}
	if refs.GetObject("rows").Len() != 49 {
		t.Fatal("independent provider path references incomplete")
	}
	return refs.GetObject("rows")
}
func providerPathReference(t *testing.T, refs *jsonx.Object, index int, row mihomoRow) mihomoRow {
	t.Helper()
	ref := refs.GetObject(strconv.Itoa(index))
	if ref == nil {
		return row
	}
	if row.fn != ref.GetString("fn") {
		t.Fatal("D2 reference row moved")
	}
	// N7/R5 reader references may deliberately replace an archived success
	// with an explicit error. Keep that whole error comparison unchanged.
	if row.errClass != "" {
		return row
	}
	paths := ref.GetObject("paths")
	replacements := 0
	rewrite := func(old string) string {
		if value, ok := paths.Value(old).(string); ok {
			replacements++
			return value
		}
		return old
	}
	entry := row.entry.DeepCopy()
	if row.fn == "generate" {
		document, err := yamlx.Parse([]byte(entry.GetString("out")))
		if err != nil {
			t.Fatal(err)
		}
		for _, provider := range yamlx.Pairs(yamlx.Get(document, "proxy-providers")) {
			old := yamlx.Get(provider, "path")
			if old != nil && old.Tag == "!!str" {
				old.Value = rewrite(old.Value)
			}
		}
		rendered, err := yamlx.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		entry.Set("out", string(rendered))
	} else {
		for _, item := range entry.GetList("out") {
			provider := item.(*jsonx.Object)
			provider.Set("local_path", rewrite(provider.GetString("local_path")))
		}
	}
	if replacements != paths.Len() {
		t.Fatalf("provider path reference %d did not bind every exact old path", index)
	}
	row.entry = entry
	return row
}
func TestD2ProviderPathReferencesBoundToOriginalRows(t *testing.T) {
	refs := providerPathReferences(t)
	for index, row := range loadMihomoCorpus(t) {
		_ = providerPathReference(t, refs, index, row)
	}
}
