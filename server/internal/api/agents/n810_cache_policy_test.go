package agents_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func nativeCacheTagStatus(t *testing.T, input string, oldStatus int) int {
	t.Helper()
	var shape map[string]json.RawMessage
	var tags []map[string]json.RawMessage
	if json.Unmarshal([]byte(input), &shape) != nil || len(shape) != 1 || json.Unmarshal(shape["tags"], &tags) != nil || len(tags) != 1 || tags[0] == nil || len(tags[0]) != 0 {
		return oldStatus
	}
	raw, err := os.ReadFile("difftest/testdata/n810_api_python.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var row struct {
			Original map[string]any `json:"original"`
			SHA      string         `json:"sha256"`
			Kind     string         `json:"kind"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		request := row.Original["request"].(map[string]any)
		if request["name"] != "cache flush dict tag" {
			continue
		}
		encoded, err := json.Marshal(row.Original)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(encoded)) != row.SHA || row.Kind != "cache-tags" || row.Original["python"].(map[string]any)["status"] != float64(oldStatus) {
			t.Fatal("complete original cache HTTP row changed")
		}
		return 400
	}
	t.Fatal("bound cache HTTP row missing")
	return 0
}
func TestN810CachePolicyRejectsChangedShape(t *testing.T) {
	if nativeCacheTagStatus(t, `{"tags":[[]]}`, 500) != 500 || nativeCacheTagStatus(t, `{"Tags":[{}]}`, 500) != 500 {
		t.Fatal("cache policy accepted a different original input")
	}
}
