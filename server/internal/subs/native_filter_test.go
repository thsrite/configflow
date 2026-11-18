package subs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func nativeFilterExpectations(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_filter_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SourceSHA string `json:"source_sha256"`
		Cases     map[string]struct {
			InputSHA string `json:"input_sha256"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/filter.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != manifest.SourceSHA {
		t.Fatal("filter reference source drift")
	}
	z, err := gzip.NewReader(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	scanner := bufio.NewScanner(z)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for key, entry := range manifest.Cases {
		index, err := strconv.Atoi(key)
		if err != nil || index < 0 || index >= len(lines) {
			t.Fatal("filter fixture index", key)
		}
		sum := sha256.Sum256([]byte(lines[index]))
		if hex.EncodeToString(sum[:]) != entry.InputSHA {
			t.Fatalf("filter input drift %s", key)
		}
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object).GetObject("cases")
}

func TestN89NativeFilterRetainsRawItems(t *testing.T) {
	sub := model.Subscription{Name: "S", RegexFilter: new("^$")}
	nodes, err := yamlx.FromJSON([]byte(`[{"name":["bad"],"future":9007199254740993},{"name":5},{"name":""},false]`))
	if err != nil {
		t.Fatal(err)
	}
	kept := FilterProxies(sub, nodes.Content)
	if len(kept) != 2 || kept[0] != nodes.Content[0] || kept[1] != nodes.Content[2] {
		t.Fatal("R5 view selection or ownership changed")
	}
	data, err := yamlx.ToJSON(kept[0])
	if err != nil || string(data) != `{"name":["bad"],"future":9007199254740993}` {
		t.Fatalf("raw item changed %s %v", data, err)
	}
	typed := FilterNodes(model.Subscription{RegexFilter: new("^5$")}, []model.Node{{Name: "5"}, {Name: "other"}})
	if len(typed) != 1 || typed[0].Name != "5" {
		t.Fatal(typed)
	}
}
