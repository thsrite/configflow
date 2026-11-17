package subs

import (
	"compress/gzip"
	"io"
	"os"
	"testing"

	"configflow/internal/jsonx"
)

func nativeReaderCases(t *testing.T) *jsonx.Object {
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
	v, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*jsonx.Object).GetObject("cases")
}

func changedNativeCase(t *testing.T, cases *jsonx.Object, id string) (*jsonx.Object, bool) {
	t.Helper()
	row := cases.GetObject(id)
	if row == nil {
		t.Fatalf("missing reader oracle %s", id)
	}
	if !row.GetBool("changed") {
		return nil, false
	}
	if len(row.GetList("classes")) == 0 {
		t.Fatalf("unclassified reader change %s", id)
	}
	return row.GetObject("expected"), true
}
