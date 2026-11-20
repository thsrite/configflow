package substore

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func nativeReaderCases(t *testing.T) map[string]map[string]any {
	t.Helper()
	f, err := os.Open("../subs/testdata/n7_reader_expected.json.gz")
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
		Cases map[string]map[string]any `json:"cases"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		t.Fatal(err)
	}
	return root.Cases
}

func jsonTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		f, _ := x.Float64()
		return f != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func assertNativeReaderCase(t *testing.T, cases map[string]map[string]any, id string, got any, err error) {
	t.Helper()
	row := cases[id]
	if row == nil {
		t.Fatalf("missing reader oracle %s", id)
	}
	changed, _ := row["changed"].(bool)
	classes, _ := row["classes"].([]any)
	if changed && len(classes) == 0 {
		t.Fatalf("unclassified change %s", id)
	}
	want, _ := row["expected"].(map[string]any)
	if _, hasErr := want["error"]; hasErr {
		if err == nil {
			t.Errorf("%s expected rejection, got %#v", id, got)
			return
		}
		message, _ := want["message"].(string)
		native := errors.Is(err, yamlx.ErrSyntax) || errors.Is(err, yamlx.ErrPolicy) || errors.Is(err, yamlx.ErrAliasBudget) || errors.Is(err, yamlx.ErrAliasRecursion) || errors.Is(err, yamlx.ErrType)
		if !native && strings.ContainsAny(message, "订阅节点必须策略无法") {
			if err.Error() != message {
				t.Errorf("%s Chinese error %q want %q", id, err, message)
			}
		}
		return
	}
	if err != nil {
		t.Errorf("%s unexpected error %v", id, err)
		return
	}
	value := want["value"]
	// Empty non-list Python values are represented by an empty slice in Go.
	if list, ok := got.([]any); ok && len(list) == 0 && !jsonTruthy(value) {
		return
	}
	if !testjson.Equal(value, got) {
		t.Errorf("%s\n%s", id, testjson.Diff(value, got))
	}
}
