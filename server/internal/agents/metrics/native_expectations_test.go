package metrics

import (
	"bytes"
	"configflow/internal/testjson"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

type metricsNativeExpectation struct {
	Group, Name, Source, SHA256 string
	Expected                    json.RawMessage
}

func nativeMetricExpected(t *testing.T, group, name string, item json.RawMessage) (any, bool) {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_metrics_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []metricsNativeExpectation
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile("testdata/a04_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Group != group || row.Name != name {
			continue
		}
		sum := sha256.Sum256([]byte(row.Source))
		if hex.EncodeToString(sum[:]) != row.SHA256 || !bytes.Contains(original, []byte(row.Source)) {
			t.Fatal("native expectation detached from source", row.Name)
		}
		source, err := testjson.Parse([]byte(row.Source))
		if err != nil || !testjson.Equal(source, item) {
			t.Fatal("native fixture identity changed", row.Name)
		}
		value, err := testjson.Parse(row.Expected)
		if err != nil {
			t.Fatal(err)
		}
		return value, true
	}
	return nil, false
}

func TestNativeMetricExpectationNegativeControl(t *testing.T) {
	raw, err := os.ReadFile("testdata/n810_metrics_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []metricsNativeExpectation
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 23 {
		t.Fatal("expectation cardinality", len(rows))
	}
	for _, row := range rows {
		changed := row.Source + " "
		sum := sha256.Sum256([]byte(changed))
		if hex.EncodeToString(sum[:]) == row.SHA256 {
			t.Fatal("changed fixture accepted")
		}
	}
}

func equalStoredTrafficJSON(a, b []byte) bool {
	left, e1 := testjson.Parse(a)
	right, e2 := testjson.Parse(b)
	return e1 == nil && e2 == nil && testjson.Equal(left, right)
}

func nativeStoreRefused(t *testing.T, name string, item json.RawMessage) bool {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_store_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []metricsNativeExpectation
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile("testdata/a05_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Name != name {
			continue
		}
		sum := sha256.Sum256([]byte(row.Source))
		if hex.EncodeToString(sum[:]) != row.SHA256 || !bytes.Contains(original, []byte(row.Source)) {
			t.Fatal("store native expectation detached", name)
		}
		source, err := testjson.Parse([]byte(row.Source))
		if err != nil || !testjson.Equal(source, item) {
			t.Fatal("store fixture changed", name)
		}
		return true
	}
	return false
}
