//go:build spike

// Command n7n8 is the Go half of the N7/N8 design spike (see ../../n7-n8.md).
// It reads the directory written by collect.py and a copy of
// configflow.sqlite3, and prints counts only (never values):
//
//   - yaml.v3 decoding vs PyYAML safe_load on every stored YAML text;
//   - yaml.v3 Node round trip (comments, values, CRLF, blank lines);
//   - encoding/json on every stored field (validity, ordered re-encode
//     byte stability).
//
// Run: go run -tags spike ./design/spikes/n7n8 <collect-out> <sqlite-copy>
package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

var stats = map[string]int{}

func bump(k string) { stats[k]++ }

func main() {
	out, db := os.Args[1], os.Args[2]
	yamlTexts(out)
	jsonFields(db)
	synthetic()
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-52s %d\n", k, stats[k])
	}
}

// ---- YAML ----------------------------------------------------------------

func norm(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case bool:
		return map[string]any{"b": x}
	case int:
		return map[string]any{"i": strconv.Itoa(x)}
	case int64:
		return map[string]any{"i": strconv.FormatInt(x, 10)}
	case uint64:
		return map[string]any{"i": strconv.FormatUint(x, 10)}
	case *big.Int:
		return map[string]any{"i": x.String()}
	case float64:
		switch {
		case math.IsNaN(x):
			return map[string]any{"f": "nan"}
		case math.IsInf(x, 1):
			return map[string]any{"f": "inf"}
		case math.IsInf(x, -1):
			return map[string]any{"f": "-inf"}
		}
		return map[string]any{"f": fmt.Sprintf("%.17g", x)}
	case string:
		return map[string]any{"s": x}
	case []byte:
		return map[string]any{"bin": base64.StdEncoding.EncodeToString(x)}
	case time.Time:
		return map[string]any{"t": x.String()}
	case []any:
		l := make([]any, len(x))
		for i, e := range x {
			l[i] = norm(e)
		}
		return l
	case map[string]any:
		m := map[any]any{}
		for k, e := range x {
			m[k] = e
		}
		return norm(m)
	case map[any]any:
		items := [][]any{}
		for k, e := range x {
			items = append(items, []any{norm(k), norm(e)})
		}
		sort.Slice(items, func(i, j int) bool { return key(items[i][0]) < key(items[j][0]) })
		l := make([]any, len(items))
		for i, it := range items {
			l[i] = it
		}
		return map[string]any{"map": l}
	}
	return map[string]any{"other": fmt.Sprintf("%T", v)}
}

func key(v any) string {
	b, _ := json.Marshal(v) // sorted map keys, like json.dumps(sort_keys=True)
	return string(b)
}

// canon re-reads a normalised value through JSON so both sides have the same Go types.
func canon(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

// diffClass names the first difference: "<py kind>→<go kind>".
func diffClass(py, gv any) string {
	kind := func(v any) string {
		switch x := v.(type) {
		case nil:
			return "null"
		case []any:
			return "list"
		case map[string]any:
			for k := range x {
				return k
			}
		}
		return "?"
	}
	if reflect.DeepEqual(py, gv) {
		return ""
	}
	kp, kg := kind(py), kind(gv)
	if kp != kg {
		return kp + "→" + kg
	}
	switch p := py.(type) {
	case []any:
		g := gv.([]any)
		if len(p) != len(g) {
			return "list-length"
		}
		for i := range p {
			if c := diffClass(p[i], g[i]); c != "" {
				return c
			}
		}
	case map[string]any:
		if kp == "map" {
			pm, gm := p["map"].([]any), gv.(map[string]any)["map"].([]any)
			if len(pm) != len(gm) {
				return "map-size"
			}
			for i := range pm {
				if c := diffClass(pm[i], gm[i]); c != "" {
					return c
				}
			}
			return ""
		}
		if kp == "f" {
			return "float-value"
		}
		return kp + "-value"
	}
	return "?"
}

type pyResult struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Value  any    `json:"value"`
	Error  string `json:"error"`
}

func yamlTexts(out string) {
	f, err := os.Open(filepath.Join(out, "py.jsonl"))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<20), 64<<20)
	for sc.Scan() {
		var r pyResult
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			panic(err)
		}
		raw, err := os.ReadFile(filepath.Join(out, "texts", r.Name))
		if err != nil {
			panic(err)
		}
		text := string(raw)
		origin := r.Origin
		if strings.HasPrefix(origin, "file:") {
			origin = "file"
		}
		p := "yaml." + origin + "."
		bump(p + "texts")
		if strings.Contains(text, "\r\n") {
			bump(p + "crlf")
		}
		docs := countDocs(text)
		if docs > 1 {
			bump(p + "multi_document")
		}
		var gv any
		gerr := yaml.Unmarshal(raw, &gv)
		switch {
		case r.Error != "" && gerr != nil:
			bump(p + "load.both_error")
		case r.Error != "":
			bump(p + "load.py_error_only." + r.Error)
		case gerr != nil:
			c := "other"
			if strings.Contains(gerr.Error(), "already defined") {
				c = "duplicate_key"
			}
			bump(p + "load.go_error_only." + c)
		default:
			if c := diffClass(canon(r.Value), canon(norm(gv))); c != "" {
				bump(p + "load.value_differs." + c)
			} else {
				bump(p + "load.equal")
			}
		}
		roundTrip(p, raw)
	}
	if err := sc.Err(); err != nil {
		panic(err)
	}
}

func countDocs(text string) int {
	dec := yaml.NewDecoder(strings.NewReader(text))
	n := 0
	for {
		var node yaml.Node
		if err := dec.Decode(&node); err != nil {
			if !errors.Is(err, io.EOF) {
				n++
			}
			return n
		}
		n++
	}
}

func comments(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	c := 0
	for _, s := range []string{n.HeadComment, n.LineComment, n.FootComment} {
		if s != "" {
			c += strings.Count(s, "\n") + 1
		}
	}
	for _, k := range n.Content {
		c += comments(k)
	}
	return c
}

func commentLines(text string) int {
	c := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			c++
		}
	}
	return c
}

func blankLines(text string) int {
	c := 0
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) == "" {
			c++
		}
	}
	return c
}

func roundTrip(p string, raw []byte) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		bump(p + "node.parse_error")
		return
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		bump(p + "node.encode_error")
		return
	}
	enc.Close()
	var back yaml.Node
	if err := yaml.Unmarshal(buf.Bytes(), &back); err != nil {
		bump(p + "node.reparse_error")
		return
	}
	var a, b any
	errA := doc.Decode(&a)
	errB := back.Decode(&b)
	if (errA == nil) != (errB == nil) || errA == nil && !reflect.DeepEqual(canon(norm(a)), canon(norm(b))) {
		bump(p + "node.value_changed")
	} else {
		bump(p + "node.value_kept")
	}
	if before := comments(&doc); before > 0 {
		bump(p + "node.with_comments")
		if comments(&back) < before {
			bump(p + "node.comments_lost")
		}
		if commentLines(buf.String()) < commentLines(string(raw)) {
			bump(p + "node.comment_lines_fewer")
		}
	}
	if bytes.Equal(buf.Bytes(), raw) {
		bump(p + "node.bytes_identical")
	}
	if blankLines(string(raw)) > blankLines(buf.String()) {
		bump(p + "node.blank_lines_dropped")
	}
}

// ---- JSON ----------------------------------------------------------------

// reencode writes a JSON value again in the stored form (compact, members in
// order, numbers as written, strings by encoding/json without HTML escaping).
func reencode(dec *json.Decoder, w *bytes.Buffer) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	switch x := t.(type) {
	case json.Delim:
		closing := map[json.Delim]byte{'{': '}', '[': ']'}[x]
		w.WriteByte(byte(x))
		first := true
		for dec.More() {
			if !first {
				w.WriteByte(',')
			}
			first = false
			if x == '{' {
				k, err := dec.Token()
				if err != nil {
					return err
				}
				writeString(w, k.(string))
				w.WriteByte(':')
			}
			if err := reencode(dec, w); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
		w.WriteByte(closing)
	case string:
		writeString(w, x)
	case json.Number:
		w.WriteString(x.String())
	case bool:
		w.WriteString(strconv.FormatBool(x))
	case nil:
		w.WriteString("null")
	}
	return nil
}

func writeString(w *bytes.Buffer, s string) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	w.Write(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
}

func jsonFields(path string) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT value FROM fields")
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			panic(err)
		}
		bump("json.fields")
		if !json.Valid([]byte(value)) {
			bump("json.invalid_for_encoding_json")
			continue
		}
		dec := json.NewDecoder(strings.NewReader(value))
		dec.UseNumber()
		var w bytes.Buffer
		if err := reencode(dec, &w); err != nil {
			bump("json.reencode_error")
			continue
		}
		if w.String() == value {
			bump("json.ordered_reencode_identical")
		} else {
			bump("json.ordered_reencode_differs")
		}
	}
}

// ---- synthetic checks ----------------------------------------------------

func synthetic() {
	check := func(name string, ok bool) {
		if ok {
			bump("synthetic." + name + ".yes")
		} else {
			bump("synthetic." + name + ".no")
		}
	}
	var n yaml.Node
	errNode := yaml.Unmarshal([]byte("a: 1\na: 2\n"), &n)
	check("duplicate_key_accepted_by_node_parse", errNode == nil)
	var v any
	check("duplicate_key_accepted_by_decode", yaml.Unmarshal([]byte("a: 1\na: 2\n"), &v) == nil)
	check("duplicate_key_accepted_by_node_decode", errNode == nil && n.Decode(&v) == nil)
	crlf := "# head\r\na: 1 # line\r\nlisteners:\r\n  - name: x\r\n    proxy: old # keep\r\n"
	var c yaml.Node
	check("crlf_with_comments_parses", yaml.Unmarshal([]byte(crlf), &c) == nil && comments(&c) == 3)
	var anchor yaml.Node
	src := "base: &b {x: 1}\nuse:\n  <<: *b\n  y: 2\n"
	check("anchor_merge_parses", yaml.Unmarshal([]byte(src), &anchor) == nil)
	var out bytes.Buffer
	e := yaml.NewEncoder(&out)
	e.SetIndent(2)
	_ = e.Encode(&anchor)
	check("anchor_merge_kept_on_encode", strings.Contains(out.String(), "&b") && strings.Contains(out.String(), "<<: *b"))
	var y11 any
	_ = yaml.Unmarshal([]byte("a: yes\nb: on\nc: 0777\nd: 1:30\n"), &y11)
	m := y11.(map[string]any)
	check("yaml11_yes_is_string", m["a"] == "yes")
	check("yaml11_0777_is_int_777", m["c"] == 777)
	check("yaml11_0777_is_octal_511", m["c"] == 511)
	check("yaml11_sexagesimal_is_string", m["d"] == "1:30")
	enc, _ := yaml.Marshal(map[string]any{"a": "yes", "b": "on", "c": "0777", "d": "1:30", "e": "1_000", "f": "2001-12-14", "g": strings.Repeat("word ", 40)})
	check("encoder_quotes_yes", strings.Contains(string(enc), `a: "yes"`))
	check("encoder_quotes_on", strings.Contains(string(enc), `b: "on"`))
	check("encoder_quotes_0777", strings.Contains(string(enc), `c: "0777"`))
	check("encoder_quotes_1:30", strings.Contains(string(enc), `d: "1:30"`))
	check("encoder_quotes_1_000", strings.Contains(string(enc), `e: "1_000"`))
	check("encoder_quotes_date", strings.Contains(string(enc), `f: "2001-12-14"`))
	check("encoder_no_line_wrap", strings.Count(string(enc), "\n") == 7)
	var ts any
	_ = yaml.Unmarshal([]byte("t: 2001-12-14\n"), &ts)
	_, isString := ts.(map[string]any)["t"].(string)
	check("timestamp_into_any_is_string", isString)
	var big any
	_ = yaml.Unmarshal([]byte("n: 123456789012345678901234567890\n"), &big)
	_, isFloat := big.(map[string]any)["n"].(float64)
	check("bigint_into_any_is_float64", isFloat)
	var pair yaml.Node
	check("json_surrogate_pair_via_yaml_v3", yaml.Unmarshal([]byte(`{"s": "😀"}`), &pair) == nil &&
		len(pair.Content) == 1 && pair.Content[0].Content[1].Value == "😀")
	var multi yaml.Node
	dec := yaml.NewDecoder(strings.NewReader("a: 1\n---\nb: 2\n"))
	check("multi_document_first_decodes", dec.Decode(&multi) == nil)
}
