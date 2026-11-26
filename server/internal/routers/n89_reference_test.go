package routers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

type r01NativeExpected struct {
	Line        int    `json:"line"`
	InputSHA256 string `json:"input_sha256"`
	Answer      struct {
		Out json.RawMessage `json:"out"`
		Err []string        `json:"err"`
	} `json:"answer"`
}

func loadR01NativeExpected(t *testing.T) map[int]r01NativeExpected {
	t.Helper()
	raw, err := os.ReadFile("testdata/n89_expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []r01NativeExpected
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	out := map[int]r01NativeExpected{}
	for _, r := range rows {
		if _, ok := out[r.Line]; ok {
			t.Fatal("duplicate expectation")
		}
		out[r.Line] = r
	}
	return out
}
func r01Hash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
