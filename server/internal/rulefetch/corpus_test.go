package rulefetch

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/repo"
)

type orderedKV struct {
	k string
	v any
}

type orderedObj struct {
	items []orderedKV
	index map[string]int
}

func newOrderedObj(kv ...any) *orderedObj {
	o := &orderedObj{index: make(map[string]int, len(kv)/2)}
	for i := 0; i+1 < len(kv); i += 2 {
		o.set(kv[i].(string), kv[i+1])
	}
	return o
}

func (o *orderedObj) len() int {
	if o == nil {
		return 0
	}
	return len(o.items)
}

func (o *orderedObj) set(k string, v any) {
	if o.index == nil {
		o.index = make(map[string]int)
	}
	if idx, ok := o.index[k]; ok {
		o.items[idx].v = v
		return
	}
	o.index[k] = len(o.items)
	o.items = append(o.items, orderedKV{k: k, v: v})
}

func (o *orderedObj) del(k string) {
	if o == nil {
		return
	}
	idx, ok := o.index[k]
	if !ok {
		return
	}
	o.items = append(o.items[:idx], o.items[idx+1:]...)
	o.index = make(map[string]int, len(o.items))
	for i, kv := range o.items {
		o.index[kv.k] = i
	}
}

func (o *orderedObj) get(k string) (any, bool) {
	if o == nil {
		return nil, false
	}
	idx, ok := o.index[k]
	if !ok {
		return nil, false
	}
	return o.items[idx].v, true
}

func (o *orderedObj) has(k string) bool {
	_, ok := o.get(k)
	return ok
}

func (o *orderedObj) val(k string) any {
	v, _ := o.get(k)
	return v
}

func (o *orderedObj) str(k string) string {
	s, _ := o.val(k).(string)
	return s
}

func (o *orderedObj) list(k string) []any {
	l, _ := o.val(k).([]any)
	return l
}

func (o *orderedObj) obj(k string) *orderedObj {
	sub, _ := o.val(k).(*orderedObj)
	return sub
}

func (o *orderedObj) intVal(k string) int {
	v := o.val(k)
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func (o *orderedObj) clone() *orderedObj {
	if o == nil {
		return nil
	}
	out := &orderedObj{items: make([]orderedKV, len(o.items)), index: make(map[string]int, len(o.items))}
	copy(out.items, o.items)
	for k, idx := range o.index {
		out.index[k] = idx
	}
	return out
}

func parseOrderedJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := readOrderedValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing token")
	}
	return v, nil
}

func readOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch d := tok.(type) {
	case json.Delim:
		switch d {
		case '{':
			obj := newOrderedObj()
			for dec.More() {
				ktok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := ktok.(string)
				if !ok {
					return nil, fmt.Errorf("expected string key")
				}
				val, err := readOrderedValue(dec)
				if err != nil {
					return nil, err
				}
				obj.set(key, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := readOrderedValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
	}
	return tok, nil
}

func writeCompact(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case *orderedObj:
		if x == nil {
			buf.WriteString("null")
			return nil
		}
		buf.WriteByte('{')
		for i, kv := range x.items {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCompact(buf, kv.k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeCompact(buf, kv.v); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case []any:
		if x == nil {
			buf.WriteString("null")
			return nil
		}
		buf.WriteByte('[')
		for i, elem := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCompact(buf, elem); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case json.Number:
		buf.WriteString(string(x))
		return nil
	default:
		var tmp bytes.Buffer
		enc := json.NewEncoder(&tmp)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return err
		}
		b := bytes.TrimSuffix(tmp.Bytes(), []byte("\n"))
		buf.Write(b)
		return nil
	}
}

func encode(v any) string {
	var buf bytes.Buffer
	if err := writeCompact(&buf, v); err != nil {
		return "<" + err.Error() + ">"
	}
	return buf.String()
}

func orderedEqual(a, b any) bool {
	switch x := a.(type) {
	case *orderedObj:
		y, ok := b.(*orderedObj)
		if !ok || x.len() != y.len() {
			return false
		}
		for _, kv := range x.items {
			yv, exists := y.get(kv.k)
			if !exists || !orderedEqual(kv.v, yv) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !orderedEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return encode(a) == encode(b)
	}
}

func nonNilProblems(problems []Problem) []Problem {
	if problems == nil {
		return []Problem{}
	}
	return problems
}

func decodeOrdered(value any, target any) error {
	if value == nil {
		return nil
	}
	return jsondecode.Unmarshal([]byte(encode(value)), target)
}

func decodeRuleObj(o *orderedObj) model.RuleConfig {
	var r model.RuleConfig
	if o != nil {
		_ = decodeOrdered(o, &r)
	}
	return r
}

func decodeSourceConfig(o *orderedObj, withRules bool) *SourceConfig {
	if o == nil {
		return nil
	}
	cfg := &SourceConfig{}
	if sys := o.obj("system_config"); sys != nil {
		_ = decodeOrdered(sys, &cfg.Settings)
	}
	if lib := o.list("rule_library"); lib != nil {
		_ = decodeOrdered(lib, &cfg.Library)
	}
	if withRules {
		if rc := o.list("rule_configs"); rc != nil {
			_ = decodeOrdered(rc, &cfg.Rules)
		}
		if rs := o.list("rule_sets"); rs != nil {
			_ = decodeOrdered(rs, &cfg.RuleSets)
		}
	}
	return cfg
}

func argText(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return "\x00"
}

func argFormat(value any) string {
	if value == nil {
		return ""
	}
	return argText(value)
}

func runInspect(value, behavior, format any) (any, error) {
	content, ok := value.(string)
	if !ok {
		return nil, validationError(msgContentText)
	}
	return Inspect(content, argText(behavior), argFormat(format))
}

func runValidateRemote(value any, ruleObj, configObj *orderedObj, format string) error {
	rule := decodeRuleObj(ruleObj)
	var content Content
	switch v := value.(type) {
	case string:
		content = Content{Data: []byte(v), Text: true}
	case []byte:
		content = Content{Data: v}
	default:
		return NewRemoteError(rule, format, []Problem{remoteProblem(0, "", msgContentBytes)})
	}
	var library []model.RuleLibraryItem
	if configObj != nil {
		if lib := configObj.list("rule_library"); lib != nil {
			_ = decodeOrdered(lib, &library)
		}
	}
	return ValidateRemote(content, rule, library, format)
}

func runValidateDomain(ruleObj, prevObj *orderedObj) error {
	raw := json.RawMessage(`{}`)
	if ruleObj != nil {
		raw = json.RawMessage(encode(ruleObj))
	}
	var before json.RawMessage
	if prevObj != nil && prevObj.len() > 0 {
		before = json.RawMessage(encode(prevObj))
	}
	return ValidateContentJSON(raw, before)
}

func runPageIdentity(result *orderedObj, host string, checkedAt any) PageInfo {
	var probe PageProbe
	if result != nil {
		probe.Status = json.RawMessage(encode(result.val("status")))
		probe.Error, _ = result.val("error").(string)
		probe.FinalURL, _ = result.val("final_url").(string)
		if markers := result.obj("markers"); markers != nil {
			_ = decodeOrdered(markers, &probe.Markers)
		}
	}
	at, _ := checkedAt.(string)
	return IdentifyPage(probe, host, at)
}

func runResolveSource(url string, configObj *orderedObj, id string) (any, error) {
	cfg := decodeSourceConfig(configObj, true)
	if cfg == nil {
		cfg = &SourceConfig{}
	}
	src, err := ResolveSource(url, cfg, id)
	if src == nil {
		return nil, err
	}
	return src, err
}

func runNormalizeProxy(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", ErrProxyNotString
	}
	return NormalizeProxy(text)
}

// pyClass names the Python exception class an error stands for.
func pyClass(err error) string {
	var remote *RemoteRuleValidationError
	switch {
	case errors.As(err, &remote):
		return "RemoteRuleValidationError"
	case errors.Is(err, repo.ErrValidation):
		return "ProfileValidationError"
	}
	return "ValueError"
}

func errorValue(err error) any {
	var errs any
	var remote *RemoteRuleValidationError
	if errors.As(err, &remote) {
		errs = nonNilProblems(remote.Errors)
	}
	return newOrderedObj("class", pyClass(err), "text", err.Error(), "errors", errs)
}

func strs(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

func asIntArg(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func runCorpusRow(fn string, args []any) (any, error) {
	str := func(i int) string { s, _ := args[i].(string); return s }
	obj := func(i int) *orderedObj { o, _ := args[i].(*orderedObj); return o }
	switch fn {
	case "inspect":
		return runInspect(args[0], args[1], args[2])
	case "normalize_classical":
		content, warnings, err := NormalizeClassical(str(0))
		return []any{content, nonNilProblems(warnings)}, err
	case "normalize_yaml":
		content, warnings := NormalizeYAML(str(0))
		return []any{content, nonNilProblems(warnings)}, nil
	case "normalize_ruleset":
		content, warnings, err := NormalizeRuleset(str(0), argText(args[1]), argFormat(args[2]))
		return []any{content, nonNilProblems(warnings)}, err
	case "validate_remote":
		return nil, runValidateRemote(args[0], obj(1), nil, "")
	case "validate_remote_bytes":
		data, _ := base64.StdEncoding.DecodeString(str(0))
		return nil, runValidateRemote(data, obj(1), nil, "")
	case "read_payload":
		lines, _, err := ReadRulePayload(str(0))
		return strs(lines), err
	case "append":
		_, payload, err := ReadRulePayload(str(0))
		if err != nil {
			return nil, err
		}
		return AppendRuleLines(str(0), []string{"+.new.test", `x"y`}, payload)
	case "remove":
		content, removed, err := RemoveRulePayload(str(0), func(line string) bool {
			return containsRune(line, 'x') || line == "example.com"
		})
		return []any{content, strs(removed)}, err
	case "validate_domain":
		return nil, runValidateDomain(obj(0), nil)
	case "site_title":
		return SiteTitle(str(0)), nil
	case "clean_text":
		return CleanText(str(0), 200), nil
	case "known_service":
		if s := LookupService(str(0)); s != nil {
			return s, nil
		}
		return nil, nil
	case "page_identity":
		return runPageIdentity(obj(0), "example.com", "now"), nil
	case "resolve_source":
		return runResolveSource(str(0), obj(1), str(2))
	case "validate_rule_url":
		u, err := ValidateRuleURL(str(0))
		if err != nil {
			return nil, err
		}
		return ruleURLRow(u), nil
	case "normalize_proxy":
		return runNormalizeProxy(args[0])
	case "urljoin":
		joined, _, err := joinURL(str(0), str(1))
		return requestedForm(joined), err
	}
	panic("unknown fn " + fn)
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

// TestCorpus replays testdata/corpus.jsonl.gz (testdata/gen.py, real Python).
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
	rows, failures := 0, map[string]int{}
	classes := map[string]int{}
	for sc.Scan() {
		value, err := parseOrderedJSON(sc.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*orderedObj)
		rows++
		fn := row.str("fn")
		if droppedURLRow(fn) {
			continue
		}
		got, gotErr := runCorpusRow(fn, row.list("args"))
		var gotText, wantText string
		if want, isErr := row.get("err"); isErr {
			wantText = encode(classOnly(want))
			if gotErr != nil {
				gotText = encode(classOnly(errorValue(gotErr)))
			} else {
				gotText = "ok " + encode(got)
			}
		} else {
			want, _ := wantURLRow(fn, row.val("ok"))
			wantText = encode(want)
			if gotErr != nil {
				gotText = "error " + encode(errorValue(gotErr))
			} else {
				gotText = encode(got)
			}
		}
		if gotText != wantText {
			args := row.list("args")
			input, _ := args[0].(string)
			if urlAccepted(fn, input, args) {
				continue
			}
			if category := nativeReplayDifference(t, "corpus", fn, row.val("args"), gotText, wantText); category != "" {
				classes[category]++
				continue
			}
			failures[fn]++
			if failures[fn] <= 4 {
				t.Errorf("%s%s\n got %s\nwant %s", fn, encode(row.val("args")), gotText, wantText)
			}
		}
	}
	t.Logf("N7-3b classifications: %v", classes)
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(failures) > 0 {
		t.Errorf("failures by function: %v (of %d rows)", failures, rows)
	}
	if rows < 10000 {
		t.Fatalf("only %d rows", rows)
	}
}
