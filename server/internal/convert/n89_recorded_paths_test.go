package convert_test

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"testing"

	"configflow/internal/convert"
)

func recordedProviderReferences(t *testing.T) *convert.JObj {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_recorded_provider_paths.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := convert.ParseJ(data)
	if err != nil {
		t.Fatal(err)
	}
	refs := parsed.(*convert.JObj)
	archive, err := os.ReadFile("testdata/mihomo_recorded.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != refs.GetString("archive_sha256") {
		t.Fatal("recorded D2/R5 archive changed")
	}
	file, err := os.Open("testdata/mihomo_recorded.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	index, used := 0, 0
	for scanner.Scan() {
		if ref := refs.GetObject("rows").GetObject(strconv.Itoa(index)); ref != nil {
			if fmt.Sprintf("%x", sha256.Sum256(scanner.Bytes())) != ref.GetString("sha256") {
				t.Fatal("recorded reference lost exact input binding")
			}
			used++
		}
		index++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if used != 11 || refs.GetObject("rows").Len() != used {
		t.Fatal("expected nine D2 rows and two R5 hook inputs")
	}
	return refs.GetObject("rows")
}

func recordedProviderReference(t *testing.T, refs *convert.JObj, index int, entry *convert.JObj) *convert.JObj {
	t.Helper()
	ref := refs.GetObject(strconv.Itoa(index))
	if ref == nil {
		return entry
	}
	if ref.GetString("test") != entry.GetString("test") {
		t.Fatal("recorded provider test moved")
	}
	entry = entry.DeepCopy()
	if paths := ref.GetObject("paths"); paths.Len() > 0 {
		wrapper := convert.JObjKV(strconv.Itoa(index), convert.JObjKV("fn", "generate", "paths", paths))
		entry = providerPathReference(t, wrapper, index, mihomoRow{entry: entry, fn: "generate"}).entry
	}
	for _, raw := range ref.GetList("gateway_filters") {
		patch := raw.(*convert.JObj)
		calls := entry.GetObject("env").GetList("gateway")
		call := int(patch.GetInt("call"))
		group := int(patch.GetInt("group"))
		if call < 0 || call >= len(calls) {
			t.Fatal("R5 gateway call missing")
		}
		groups := calls[call].(*convert.JObj).GetObject("before").GetList("proxy-groups")
		if group < 0 || group >= len(groups) {
			t.Fatal("R5 gateway group missing")
		}
		item := groups[group].(*convert.JObj)
		if item.GetString("name") != patch.GetString("name") || item.Has("filter") || patch.GetString("filter") != "(0)|(0)" {
			t.Fatal("R5 hook patch does not bind original absent filter")
		}
		item.Set("filter", patch.GetString("filter"))
	}
	return entry
}
