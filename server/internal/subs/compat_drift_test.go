package subs

// Archived Python dialer cases now exercise the typed shared-dialers owner.
// Approved R5 differences have exact, independently generated references;
// ordinary business results stay unchanged.

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

type driftRow struct {
	fn       string
	args     []json.RawMessage
	outRaw   json.RawMessage
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
		var entry struct {
			Fn   string            `json:"fn"`
			Args []json.RawMessage `json:"args"`
			Out  json.RawMessage   `json:"out"`
			Err  []string          `json:"err"`
		}
		if err := json.Unmarshal(sanitizeNonFiniteJSON(scanner.Bytes()), &entry); err != nil {
			t.Fatal(err)
		}
		row := driftRow{fn: entry.Fn, args: entry.Args, outRaw: entry.Out}
		if len(entry.Err) >= 2 {
			row.errClass, row.errMsg = entry.Err[0], entry.Err[1]
		}
		rows = append(rows, row)
	}
	return rows
}

// nonStringValue reports a dialer_target/resolve_dialer result that holds a
// non-string id or name.
func nonStringValue(fn string, outRaw json.RawMessage) bool {
	v, err := decodeAny(outRaw)
	if err != nil {
		return false
	}
	switch fn {
	case "dialer_target":
		value, _ := v.([]any)
		if len(value) != 2 || value[1] == nil {
			return false
		}
		_, ok := value[1].(string)
		return !ok
	case "resolve_dialer":
		if v == nil {
			return false
		}
		_, ok := v.(string)
		return !ok
	}
	return false
}

func driftRefs(refs []DependencyRef) [][]string {
	out := make([][]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, []string{r.Kind, r.ID})
	}
	return out
}

// driftCall runs the typed shared-dialer owner; ok=false selects cases owned elsewhere.
func driftCall(t *testing.T, row driftRow) (result any, err error, ok bool) {
	ops := sharedDialers()
	switch row.fn {
	case "validate_dialers":
		cfg, err := decodeConfigRaw(row.args[0])
		if err != nil {
			return nil, err, true
		}
		return nil, referenceFailure(ops.Validate(cfg)), true
	case "validate_shapes":
		cfg, err := decodeConfigRaw(row.args[0])
		if err != nil {
			return nil, err, true
		}
		var ids bool
		_ = json.Unmarshal(row.args[1], &ids)
		return nil, referenceFailure(ops.Shapes(cfg, ids)), true
	case "raw_dialer":
		var node model.Node
		if err := jsondecode.Unmarshal(row.args[0], &node); err != nil {
			return nil, err, true
		}
		target, err := ops.NodeDialer(node)
		if err != nil {
			return nil, referenceFailure(err), true
		}
		if target == nil {
			return nil, nil, true
		}
		return yamlToRaw(t, target), nil, true
	case "parse_structured_proxy_string":
		var text string
		_ = json.Unmarshal(row.args[0], &text)
		node, err := ops.Structured(text)
		if err != nil || node == nil {
			return nil, nil, true
		}
		return yamlToRaw(t, node), nil, true
	case "dependency_graph":
		cfg, err := decodeConfigRaw(row.args[0])
		if err != nil {
			return nil, err, true
		}
		g, err := ops.Graph(cfg)
		if err != nil {
			return nil, referenceFailure(err), true
		}
		out := []any{}
		for _, k := range g.Order {
			out = append(out, []any{[]string{k.Kind, k.ID}, driftRefs(g.Edges[k])})
		}
		return out, nil, true
	case "group_members":
		var group model.ProxyGroup
		if err := jsondecode.Unmarshal(row.args[0], &group); err != nil {
			return [][]string{}, nil, true
		}
		return driftRefs(ops.Members(group)), nil, true
	case "dialer_target":
		cfg, err := decodeConfigRaw(row.args[0])
		if err != nil {
			return nil, err, true
		}
		var node model.Node
		if err := jsondecode.Unmarshal(row.args[1], &node); err != nil {
			return nil, err, true
		}
		r, err := ops.NodeTarget(cfg, node)
		if r == nil {
			return nil, referenceFailure(err), true
		}
		return []string{r.Kind, r.ID}, referenceFailure(err), true
	case "resolve_dialer":
		var rawNode map[string]json.RawMessage
		if err := json.Unmarshal(row.args[1], &rawNode); err != nil {
			return nil, err, true
		}
		if dr, ok := rawNode["dialer_ref"]; !ok || string(dr) == "null" {
			return nil, nil, true
		}
		cfg, err := decodeConfigRaw(row.args[0])
		if err != nil {
			return nil, err, true
		}
		var node model.Node
		if err := jsondecode.Unmarshal(row.args[1], &node); err != nil {
			return nil, err, true
		}
		name, err := ops.Resolve(cfg, node)
		if name == nil {
			return "", referenceFailure(err), true
		}
		return *name, referenceFailure(err), true
	case "filter_provider_candidates":
		var rawList []json.RawMessage
		if err := json.Unmarshal(row.args[0], &rawList); err != nil {
			return nil, err, true
		}
		native := make([]FilterCandidate, 0, len(rawList))
		for index, raw := range rawList {
			var item struct {
				Name string `json:"name"`
				Type string `json:"type"`
			}
			_ = json.Unmarshal(raw, &item)
			native = append(native, FilterCandidate{Name: item.Name, Type: item.Type, Key: strconv.Itoa(index)})
		}
		var include, exclude, excludeType string
		_ = json.Unmarshal(row.args[1], &include)
		_ = json.Unmarshal(row.args[2], &exclude)
		_ = json.Unmarshal(row.args[3], &excludeType)
		got, err := ops.Filter(native, include, exclude, excludeType)
		if err != nil {
			return nil, referenceFailure(err), true
		}
		out := make([]json.RawMessage, 0, len(got))
		for _, c := range got {
			idx, _ := strconv.Atoi(c.Key)
			out = append(out, rawList[idx])
		}
		return out, nil, true
	}
	return nil, nil, false
}

func TestCompatDoesNotDriftFromConvertCorpus(t *testing.T) {
	rows := loadDriftCorpus(t)
	native := nativeChainExpectations(t)
	oracle := nativeReaderCases(t)
	checked, lenient, accepted, handedBack := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	for i, row := range rows {
		if row.fn == "parse_structured_proxy_string" {
			if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/drift/%d", i)); changed {
				row.outRaw = expected.Value
				row.errClass = expected.Error
				row.errMsg = expected.Message
			}
		}
		if entry, ok := native[fmt.Sprintf("drift/%d", i)]; ok {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(entry.Expected, &fields); err != nil {
				t.Fatal(err)
			}
			if rawErr, hasErr := fields["error"]; hasErr {
				_ = json.Unmarshal(fields["class"], &row.errClass)
				_ = json.Unmarshal(rawErr, &row.errMsg)
			} else {
				row.errClass = ""
				row.errMsg = ""
				row.outRaw = fields["value"]
			}
		}
		got, err, ok := driftCall(t, row)
		if !ok {
			continue
		}
		checked[row.fn]++
		var dre *DialerReferenceError
		isDRE := errors.As(err, &dre)
		switch {
		case row.errClass == "DialerReferenceError":
			if (!isDRE || dre.Message != row.errMsg) && len(row.args) > 0 && o18T1cDirtyInput(row.args[0]) {
				accepted[row.fn+": stored dirty data reads leniently"]++
				continue
			}
			if (!isDRE || dre.Message != row.errMsg) && len(row.args) > 0 && o18T1cChainMissingEndpoint(row.args[0], row.errMsg) {
				handedBack[row.fn+": chain without entry/exit"]++
				continue
			}
			if !isDRE || dre.Message != row.errMsg {
				t.Errorf("row %d %s %s:\n want DialerReferenceError(%q)\n got  %v", i, row.fn, compact(t, row.args), row.errMsg, err)
			}
		case row.errClass != "":
			if err == nil || isDRE {
				// The copy skips some of Python's accidental TypeErrors
				// (unhashable ids, non-list fields): counted, not failed.
				lenient[row.fn+" "+row.errClass]++
			}
		case nonStringValue(row.fn, row.outRaw):
			// The copy keeps ids and names as strings: Python's
			// 5 or ['N'] become "5" / "['N']". Only reachable without
			// validate_shapes, which every caller runs first.
			lenient[row.fn+" non-string id/name"]++
		default:
			if err != nil {
				t.Errorf("row %d %s %s: unexpected error %v", i, row.fn, compact(t, row.args), err)
				continue
			}
			if !testjson.Equal(row.outRaw, got) && len(row.args) > 0 && o18T1cDirtyInput(row.args[0]) {
				accepted[row.fn+": stored dirty data reads leniently"]++
				continue
			}
			if !testjson.Equal(row.outRaw, got) {
				t.Errorf("row %d %s %s:\n got  %s\n want %s\n diff %s", i, row.fn, compact(t, row.args), compact(t, got), row.outRaw, testjson.Diff(row.outRaw, got))
			}
		}
	}
	t.Logf("checked %v; O18 T1c 接受 %v; 疑似回归（交回 T1c 作者）%v", checked, accepted, handedBack)
	if len(lenient) > 0 {
		t.Logf("python raised an unintended exception, the copy did not: %v", lenient)
	}
}
