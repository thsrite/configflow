package substore

import (
	"compress/gzip"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func nativeReaderCases(t *testing.T) *jsonx.Object {
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
	v, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*jsonx.Object).GetObject("cases")
}

func assertNativeReaderCase(t *testing.T, cases *jsonx.Object, id string, got any, err error) {
	t.Helper()
	row := cases.GetObject(id)
	if row == nil {
		t.Fatalf("missing reader oracle %s", id)
	}
	if row.GetBool("changed") && len(row.GetList("classes")) == 0 {
		t.Fatalf("unclassified change %s", id)
	}
	want := row.GetObject("expected")
	if want.Has("error") {
		if err == nil {
			t.Errorf("%s expected rejection, got %s", id, jsonx.Repr(got))
			return
		}
		message := want.GetString("message")
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
	value := want.Value("value")
	// The existing Go API is typed []any: Python's empty non-list values have
	// always been represented by an empty list. This is not a new YAML exception.
	if list, ok := got.([]any); ok && len(list) == 0 && !jsonx.Truthy(value) {
		return
	}
	if !yamlxtest.EqualJSON(got, value) {
		t.Errorf("%s got %s want %s", id, jsonx.Repr(got), jsonx.Repr(value))
	}
}
