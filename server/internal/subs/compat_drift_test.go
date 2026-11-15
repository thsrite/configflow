package subs

// compat.go keeps a private copy of dialer_references (and of
// _parse_structured_proxy_string) because the real port, internal/convert,
// imports this package (validate_proxy_chains): subs cannot import convert.
// So the two cannot drift, this test runs the copy over convert's
// differential corpus (the Python results that internal/convert is checked
// against, see ../convert/testdata/gen.py): results and intentional
// DialerReferenceError messages must equal Python's. Inputs on which Python
// raises an unintended exception only require some error, and inputs the
// copy does not check like Python (listed in driftKnown) are counted.

import (
	"bufio"
	"compress/gzip"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

type driftRow struct {
	fn       string
	args     []any
	out      any
	errClass string
	errMsg   string
}

func loadDriftCorpus(t *testing.T) []driftRow {
	t.Helper()
	file, err := os.Open("../convert/testdata/corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	var rows []driftRow
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		entry := value.(*jsonx.Object)
		row := driftRow{fn: entry.GetString("fn"), args: entry.GetList("args"), out: entry.Value("out")}
		if e := entry.GetList("err"); e != nil {
			row.errClass, row.errMsg = e[0].(string), e[1].(string)
		}
		rows = append(rows, row)
	}
	return rows
}

// nonStringValue reports a dialer_target/resolve_dialer result that holds a
// non-string id or name.
func nonStringValue(fn string, out any) bool {
	switch fn {
	case "dialer_target":
		value, _ := out.([]any)
		return len(value) == 2 && !isString(value[1]) && value[1] != nil
	case "resolve_dialer":
		return out != nil && !isString(out)
	}
	return false
}

// driftConv mirrors conv() of the corpus generator for YAML-only values.
func driftConv(v any) any {
	switch value := v.(type) {
	case *jsonx.Object:
		if value == nil {
			return nil
		}
		out := jsonx.NewObject()
		for k, child := range value.All() {
			out.Set(k, driftConv(child))
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = driftConv(child)
		}
		return out
	case []byte:
		return jsonx.Obj("__bytes__", hex.EncodeToString(value))
	case yamlx.Timestamp:
		return jsonx.Obj("__date__", value.String())
	}
	return v
}

func driftRefs(refs []ref) []any {
	out := []any{}
	for _, r := range refs {
		out = append(out, []any{r.kind, r.id})
	}
	return out
}

// driftCall runs the compat copy; ok=false skips rows the copy has no
// counterpart for.
func driftCall(row driftRow) (result any, err error, ok bool) {
	obj := func(i int) *jsonx.Object { o, _ := row.args[i].(*jsonx.Object); return o }
	defer func() {
		// The copy indexes some malformed values Python only fails on later;
		// a panic counts as the error Python raised.
		if r := recover(); r != nil {
			result, err, ok = nil, pyErr("panic"), true
		}
	}()
	switch row.fn {
	case "validate_dialers":
		return nil, ValidateDialers(obj(0)), true
	case "validate_shapes":
		return nil, validateShapes(obj(0), row.args[1] == true), true
	case "raw_dialer":
		v, err := rawDialer(obj(0))
		return v, err, true
	case "parse_structured_proxy_string":
		if p := parseStructuredProxyString(row.args[0].(string)); p != nil {
			return p, nil, true
		}
		return nil, nil, true
	case "dependency_graph":
		g, err := dependencyGraph(obj(0))
		if err != nil {
			return nil, err, true
		}
		out := []any{}
		for _, k := range g.order {
			out = append(out, []any{[]any{k.kind, k.id}, driftRefs(g.m[k])})
		}
		return out, nil, true
	case "group_members":
		return driftRefs(groupMembers(obj(0))), nil, true
	case "dialer_target":
		r, err := dialerTarget(obj(0), obj(1))
		if r == nil {
			return nil, err, true
		}
		return []any{r.kind, r.id}, err, true
	case "resolve_dialer":
		if obj(1).Value("dialer_ref") == nil {
			return nil, nil, true
		}
		name, err := resolveDialer(obj(0), obj(1))
		return name, err, true
	case "filter_provider_candidates":
		var candidates []Candidate
		for _, p := range row.args[0].([]any) {
			o := p.(*jsonx.Object)
			candidates = append(candidates, Candidate{Name: o.GetString("name"), Type: o.GetString("type"), Raw: o})
		}
		got, err := filterProviderCandidates(candidates, row.args[1].(string), row.args[2].(string), row.args[3].(string))
		out := []any{}
		for _, c := range got {
			out = append(out, c.Raw)
		}
		return out, err, true
	}
	return nil, nil, false
}

func TestCompatDoesNotDriftFromConvertCorpus(t *testing.T) {
	rows := loadDriftCorpus(t)
	checked, lenient := map[string]int{}, map[string]int{}
	for i, row := range rows {
		got, err, ok := driftCall(row)
		if !ok {
			continue
		}
		checked[row.fn]++
		var dre *DialerReferenceError
		isDRE := errors.As(err, &dre)
		switch {
		case row.errClass == "DialerReferenceError":
			if !isDRE || dre.Message != row.errMsg {
				t.Errorf("row %d %s %s:\n want DialerReferenceError(%q)\n got  %v", i, row.fn, compact(t, row.args), row.errMsg, err)
			}
		case row.errClass != "":
			if err == nil || isDRE {
				// The copy skips some of Python's accidental TypeErrors
				// (unhashable ids, non-list fields): counted, not failed.
				lenient[row.fn+" "+row.errClass]++
			}
		case nonStringValue(row.fn, row.out):
			// The copy keeps ids and names as strings (jsonx.Str): Python's
			// 5 or ['N'] become "5" / "['N']". Only reachable without
			// validate_shapes, which every caller runs first.
			lenient[row.fn+" non-string id/name"]++
		default:
			if err != nil {
				t.Errorf("row %d %s %s: unexpected error %v", i, row.fn, compact(t, row.args), err)
				continue
			}
			if g, w := compact(t, driftConv(got)), compact(t, row.out); g != w {
				t.Errorf("row %d %s %s:\n got  %s\n want %s", i, row.fn, compact(t, row.args), g, w)
			}
		}
	}
	t.Logf("checked %v", checked)
	if len(lenient) > 0 {
		t.Logf("python raised an unintended exception, the copy did not: %v", lenient)
	}
}
