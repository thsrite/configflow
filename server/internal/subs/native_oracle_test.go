package subs

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"testing"
)

type nativeOracleExpected struct {
	HasError bool
	Error    string
	Message  string
	Value    json.RawMessage
}

type nativeOracleRow struct {
	Changed     bool              `json:"changed"`
	Classes     []string          `json:"classes"`
	ExpectedRaw json.RawMessage   `json:"expected"`
	OldLoads    []json.RawMessage `json:"old_loads"`
	NativeLoads []json.RawMessage `json:"native_loads"`
}

func nativeReaderCases(t *testing.T) map[string]nativeOracleRow {
	t.Helper()
	f, err := os.Open("testdata/n7_reader_expected.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	data, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Cases map[string]nativeOracleRow `json:"cases"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	return root.Cases
}

func changedNativeCase(t *testing.T, cases map[string]nativeOracleRow, id string) (*nativeOracleExpected, bool) {
	t.Helper()
	row, ok := cases[id]
	if !ok {
		t.Fatalf("missing reader oracle %s", id)
	}
	if !row.Changed {
		return nil, false
	}
	if len(row.Classes) == 0 {
		t.Fatalf("unclassified reader change %s", id)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(row.ExpectedRaw, &fields); err != nil {
		t.Fatalf("invalid expected %s: %v", id, err)
	}
	out := &nativeOracleExpected{Value: fields["value"]}
	if rawErr, hasErr := fields["error"]; hasErr {
		out.HasError = true
		_ = json.Unmarshal(rawErr, &out.Error)
	}
	if rawMsg, hasMsg := fields["message"]; hasMsg {
		_ = json.Unmarshal(rawMsg, &out.Message)
	}
	return out, true
}
