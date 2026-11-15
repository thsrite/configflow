package rules

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
)

type subErr struct {
	Class string `json:"class"`
	Text  string `json:"text"`
}

type subRow struct {
	P      string   `json:"p"`
	T      string   `json:"t"`
	Count  int      `json:"count"`
	Fn     *string  `json:"fn"`
	FnErr  *subErr  `json:"fn_err"`
	Search *[][]int `json:"search"`
	Tpl    *string  `json:"tpl"`
	Out    *string  `json:"out"`
	Err    *subErr  `json:"err"`
}

func classOf(err error) string {
	var idx *TemplateIndexError
	var re *reError
	switch {
	case errors.As(err, &idx):
		return "IndexError"
	case errors.As(err, &re):
		return "error"
	}
	return fmt.Sprintf("%T", err)
}

// Reference cases: testdata/gen_sub.py (CPython 3.11 re.sub / re.search).
func TestSubReference(t *testing.T) {
	f, err := os.Open("testdata/sub_reference.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	rows := 0
	for sc.Scan() {
		var row subRow
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		rows++
		re, err := CompilePyRegex(row.P, false)
		if err != nil {
			t.Fatalf("compile %q: %v", row.P, err)
		}
		switch {
		case row.Tpl != nil:
			got, err := re.SubTemplate(row.T, *row.Tpl, row.Count)
			if row.Err != nil {
				if err == nil || err.Error() != row.Err.Text || classOf(err) != row.Err.Class {
					t.Errorf("sub(%q, %q, %q): got %q, %v; want error %+v", row.P, *row.Tpl, row.T, got, err, *row.Err)
				}
			} else if err != nil || got != *row.Out {
				t.Errorf("sub(%q, %q, %q): got %q, %v; want %q", row.P, *row.Tpl, row.T, got, err, *row.Out)
			}
		case row.Fn != nil || row.FnErr != nil:
			got, err := re.Sub(row.T, func(m *Match) (string, error) {
				whole, _ := m.Group(0)
				last := "-"
				if one, ok := m.Group(1); ok {
					last = one
				}
				return "<" + whole + "|" + last + ">", nil
			}, row.Count)
			if row.FnErr != nil || err != nil || got != *row.Fn {
				t.Errorf("sub(%q, fn, %q, %d): got %q, %v; want %v %v", row.P, row.T, row.Count, got, err, row.Fn, row.FnErr)
			}
		default:
			m, err := re.Match(row.T)
			if err != nil {
				t.Fatal(err)
			}
			var got *[][]int
			if m != nil {
				spans := [][]int{}
				for i := 0; i <= m.Groups(); i++ {
					a, b := m.Span(i)
					spans = append(spans, []int{a, b})
				}
				got = &spans
			}
			if !reflect.DeepEqual(got, row.Search) {
				t.Errorf("search(%q, %q): got %v want %v", row.P, row.T, got, row.Search)
			}
		}
	}
	if rows < 1000 {
		t.Fatalf("only %d rows", rows)
	}
}
