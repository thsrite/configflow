package convert_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/convert"
)

type inputPatchCase struct {
	Row   int      `json:"row"`
	Paths []string `json:"paths"`
}

type inputPatchFixture struct {
	Source       string           `json:"source"`
	SourceSHA256 string           `json:"source_sha256"`
	ReaderSHA256 string           `json:"reader_sha256"`
	Cases        []inputPatchCase `json:"cases"`
}

func TestInputPatchesEvidence(t *testing.T) {
	patches := loadInputPatches(t)
	rows := loadMihomoCorpus(t)
	readers := loadMihomoReaderOracle(t)
	for index, paths := range patches {
		if index < 0 || index >= len(rows) {
			t.Fatal("invalid source row")
		}
		row := readers.row(t, "mihomo_corpus.jsonl.gz", index, rows[index])
		_ = keepOriginalInput(t, row, paths)
	}
}

func inputSourceDigest(t testing.TB, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func loadInputPatches(t testing.TB) map[int][]string {
	t.Helper()
	data, err := os.ReadFile("testdata/n88_input_patches.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture inputPatchFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Source != "mihomo_corpus.jsonl.gz" || fixture.SourceSHA256 != inputSourceDigest(t, "testdata/"+fixture.Source) || fixture.ReaderSHA256 != inputSourceDigest(t, "testdata/n73d_expected.json.gz") || len(fixture.Cases) != 31 {
		t.Fatal("input side-effect fixture provenance changed")
	}
	patches := map[int][]string{}
	for _, sample := range fixture.Cases {
		if _, ok := patches[sample.Row]; ok || len(sample.Paths) == 0 {
			t.Fatal("invalid input side-effect index")
		}
		patches[sample.Row] = sample.Paths
	}
	return patches
}

func keepOriginalInput(t testing.TB, row mihomoRow, paths []string) mihomoRow {
	t.Helper()
	if len(paths) == 0 {
		return row
	}
	if row.fn != "generate" {
		t.Fatal("side-effect patch applied to another function")
	}
	input := row.entry.GetObject("args").GetObject("profile")
	expected := row.entry.GetObject("mutated").DeepCopy()
	for _, path := range paths {
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 5 || parts[0] != "nodes" || parts[2] != "params" || parts[3] != "reality-opts" || parts[4] != "short-id" {
			t.Fatal("unknown side-effect path")
		}
		index, err := strconv.Atoi(parts[1])
		if err != nil || index < 0 || index >= len(input.GetList("nodes")) || index >= len(expected.GetList("nodes")) {
			t.Fatal("side-effect node index changed")
		}
		before := input.GetList("nodes")[index].(*convert.JObj).GetObject("params").GetObject("reality-opts")
		after := expected.GetList("nodes")[index].(*convert.JObj).GetObject("params").GetObject("reality-opts")
		if before == nil || after == nil || before.Has("short-id") || !after.Has("short-id") || after.Value("short-id") != "" {
			t.Fatal("side-effect evidence changed")
		}
		after.Delete("short-id")
		// All siblings remain exact: only the independently recorded path is
		// expected to retain the source, not Python's shallow-copy mutation.
		if encode(before) != encode(after) {
			t.Fatal("unclassified sibling mutation")
		}
	}
	entry := row.entry.DeepCopy()
	entry.Set("mutated", expected)
	return rowOf(entry)
}
