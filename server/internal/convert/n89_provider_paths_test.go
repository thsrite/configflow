package convert_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/yamlx"
)

func providerPathReferences(t *testing.T) *convert.JObj {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_provider_paths.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := convert.ParseJ(data)
	if err != nil {
		t.Fatal(err)
	}
	refs := parsed.(*convert.JObj)
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

func providerPathReference(t *testing.T, refs *convert.JObj, index int, row mihomoRow) mihomoRow {
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
			provider := item.(*convert.JObj)
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

// Only reverse the independently recorded path scalars for the original byte
// manifest. Edit keeps every other byte observable.
func providerByteReference(t *testing.T, refs *convert.JObj, index int, output string) string {
	t.Helper()
	reference := refs.GetObject(strconv.Itoa(index))
	if reference == nil {
		return output
	}
	if reference.GetString("fn") != "generate" {
		t.Fatal("byte reference is not a generated document")
	}
	paths := reference.GetObject("paths")
	reversed := 0
	data, err := yamlx.Edit([]byte(output), func(document *yamlx.Node) error {
		for _, provider := range yamlx.Pairs(yamlx.Get(document, "proxy-providers")) {
			field := yamlx.Get(provider, "path")
			if field == nil || field.ShortTag() != "!!str" {
				continue
			}
			for old, native := range paths.All() {
				if field.Value == native {
					field.Value = old
					reversed++
				}
			}
		}
		return nil
	})
	if err != nil || reversed != paths.Len() {
		t.Fatalf("exact provider byte paths %d: %d/%d %v", index, reversed, paths.Len(), err)
	}
	return string(data)
}
