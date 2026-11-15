package pyyaml

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

type refError struct {
	Class   string  `json:"class"`
	Text    string  `json:"text"`
	Problem *string `json:"problem"`
	Line    *int    `json:"line"`
}

type refRow struct {
	Text         string           `json:"text"`
	Tokens       []map[string]any `json:"tokens"`
	ScanError    *refError        `json:"scan_error"`
	Node         json.RawMessage  `json:"node"`
	ComposeError *refError        `json:"compose_error"`
}

func markJSON(m Mark) []any { return []any{float64(m.Index), float64(m.Line), float64(m.Column)} }

func styleJSON(style rune) any {
	if style == 0 {
		return nil
	}
	return string(style)
}

func tokenJSON(t Token) map[string]any {
	out := map[string]any{"id": t.Kind.ID(), "s": markJSON(t.Start), "e": markJSON(t.End)}
	switch t.Kind {
	case TokenAlias, TokenAnchor:
		out["value"] = t.Value
	case TokenScalar:
		out["value"], out["plain"], out["style"] = t.Value, t.Plain, styleJSON(t.Style)
	case TokenTag:
		var handle any
		if t.HasHandle {
			handle = t.Handle
		}
		out["value"] = []any{handle, t.Suffix}
	case TokenDirective:
		out["name"] = t.Value
		switch t.Value {
		case "YAML":
			out["value"] = []any{float64(t.Major), float64(t.Minor)}
		case "TAG":
			out["value"] = []any{t.TagHandle, t.Tag}
		default:
			out["value"] = nil
		}
	}
	return out
}

func nodeJSON(n *Node, seen map[*Node]int) map[string]any {
	if id, ok := seen[n]; ok {
		return map[string]any{"ref": float64(id)}
	}
	seen[n] = len(seen)
	kinds := []string{"ScalarNode", "SequenceNode", "MappingNode"}
	out := map[string]any{"kind": kinds[n.Kind], "tag": n.Tag, "s": markJSON(n.Start), "e": markJSON(n.End)}
	switch n.Kind {
	case ScalarNode:
		out["value"], out["style"] = n.Value, styleJSON(n.Style)
	case SequenceNode:
		out["flow"] = n.FlowStyle
		items := []any{}
		for _, item := range n.Items {
			items = append(items, nodeJSON(item, seen))
		}
		out["items"] = items
	case MappingNode:
		out["flow"] = n.FlowStyle
		pairs := []any{}
		for _, pair := range n.Pairs {
			pairs = append(pairs, []any{nodeJSON(pair[0], seen), nodeJSON(pair[1], seen)})
		}
		out["pairs"] = pairs
	}
	return out
}

func errorJSON(err error) refError {
	out := refError{Text: err.Error()}
	switch e := err.(type) {
	case *Error:
		out.Class = e.Class
		if e.Marked() {
			if e.Problem != "" {
				p := e.Problem
				out.Problem = &p
			}
			if e.ProblemMark != nil {
				line := e.ProblemMark.Line
				out.Line = &line
			}
		}
	case *ValueError:
		out.Class = "ValueError"
	default:
		out.Class = fmt.Sprintf("%T", err)
	}
	return out
}

func roundTrip(v any) any {
	data, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

func TestCorpus(t *testing.T) {
	f, err := os.Open("testdata/corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	rows, failures := 0, 0
	for sc.Scan() {
		var row refRow
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		rows++
		tokens, err := Scan(row.Text)
		if row.ScanError != nil {
			if err == nil {
				t.Errorf("scan %q: want error %q", row.Text, row.ScanError.Text)
				failures++
			} else if got := errorJSON(err); !reflect.DeepEqual(got, *row.ScanError) {
				t.Errorf("scan %q:\n got %+v\nwant %+v", row.Text, got, *row.ScanError)
				failures++
			}
		} else if err != nil {
			t.Errorf("scan %q: unexpected error %v", row.Text, err)
			failures++
		} else {
			got := []any{}
			for _, tok := range tokens {
				got = append(got, tokenJSON(tok))
			}
			want := roundTrip(row.Tokens)
			if want == nil {
				want = []any{}
			}
			if !reflect.DeepEqual(roundTrip(got), want) {
				t.Errorf("scan %q:\n got %v\nwant %v", row.Text, roundTrip(got), want)
				failures++
			}
		}
		root, err := Compose(row.Text)
		if row.ComposeError != nil {
			if err == nil {
				t.Errorf("compose %q: want error %q", row.Text, row.ComposeError.Text)
				failures++
			} else if got := errorJSON(err); !reflect.DeepEqual(got, *row.ComposeError) {
				t.Errorf("compose %q:\n got %+v\nwant %+v", row.Text, got, *row.ComposeError)
				failures++
			}
		} else if err != nil {
			t.Errorf("compose %q: unexpected error %v", row.Text, err)
			failures++
		} else {
			var got any
			if root != nil {
				got = roundTrip(nodeJSON(root, map[*Node]int{}))
			}
			var want any
			_ = json.Unmarshal(row.Node, &want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("compose %q:\n got %v\nwant %v", row.Text, got, want)
				failures++
			}
		}
		if failures > 30 {
			t.Fatal("too many failures")
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if rows < 5000 {
		t.Fatalf("only %d rows", rows)
	}
}
