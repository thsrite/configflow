package routersplit

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"configflow/internal/testgolden"
	"configflow/internal/testjson"
)

type n89Expected struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	InputSHA256 string `json:"input_sha256"`
	Fn          string `json:"fn"`
	Answer      struct {
		Out   any     `json:"out"`
		Err   *recObj `json:"err"`
		Calls int     `json:"calls"`
	} `json:"answer"`
}

func n89Expectations(t *testing.T, path string) map[int]n89Expected {
	t.Helper()
	raw, err := testgolden.ReadFile(t, "testdata/n89_expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := parseRec(raw)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int]n89Expected{}
	for _, v := range value.([]any) {
		row := v.(*recObj)
		if row.str("file") != filepath.Base(path) {
			continue
		}
		a := row.obj("answer")
		e := n89Expected{File: row.str("file"), Line: row.intVal("line"), InputSHA256: row.str("input_sha256"), Fn: row.str("fn")}
		e.Answer.Out = decodeRecorded(a.value("out"))
		e.Answer.Err = a.obj("err")
		e.Answer.Calls = a.intVal("calls")
		if _, duplicate := out[e.Line]; duplicate {
			t.Fatal("duplicate independent row")
		}
		out[e.Line] = e
	}
	return out
}
func n89Check(t *testing.T, e n89Expected, row recordedRow, out any, err error, calls int) {
	t.Helper()
	if row.Fn != e.Fn || calls != e.Answer.Calls {
		t.Fatalf("independent row %d: function/collaborator count differs: %s/%s %d/%d", e.Line, row.Fn, e.Fn, calls, e.Answer.Calls)
	}
	if e.Answer.Err != nil {
		if p := compareError(e.Answer.Err, err); p != "" {
			t.Errorf("independent %s row %d: %s", e.Fn, e.Line, p)
		}
		return
	}
	if err != nil {
		t.Errorf("independent %s row %d unexpected error: %v", e.Fn, e.Line, err)
		return
	}
	if row.Fn == "full_tun_base_yaml" && sameYAML(out, e.Answer.Out) {
		return
	}
	if !testjson.Equal(e.Answer.Out, out, testjson.CompareOptions{DistinguishNumberLiterals: true}) {
		t.Errorf("independent %s row %d: got %s want %s", e.Fn, e.Line, encode(out), encode(e.Answer.Out))
	}
}

// Input hashes bind each changed answer to the unchanged literal corpus line.
func n89Hash(line []byte) string { sum := sha256.Sum256(line); return hex.EncodeToString(sum[:]) }
