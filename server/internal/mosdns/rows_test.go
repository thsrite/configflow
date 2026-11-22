package mosdns

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

// mosObj is an ordered JSON object helper for MosDNS test corpora and oracles.
type mosObj struct {
	keys   []string
	values map[string]any
}

func newMosObj() *mosObj { return &mosObj{values: map[string]any{}} }

func mosKV(kv ...any) *mosObj {
	o := newMosObj()
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), mosNormalizeVal(kv[i+1]))
	}
	return o
}

func mosInt(n int64) json.Number { return json.Number(strconv.FormatInt(n, 10)) }

func mosNormalizeVal(v any) any {
	switch x := v.(type) {
	case int:
		return mosInt(int64(x))
	case int64:
		return mosInt(x)
	default:
		return v
	}
}

func (o *mosObj) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

func (o *mosObj) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.values[key]
	return v, ok
}

func (o *mosObj) Value(key string) any {
	if o == nil {
		return nil
	}
	return o.values[key]
}

func (o *mosObj) Has(key string) bool {
	if o == nil {
		return false
	}
	_, ok := o.values[key]
	return ok
}

func (o *mosObj) GetObject(key string) *mosObj {
	v, _ := o.Value(key).(*mosObj)
	return v
}

func (o *mosObj) GetList(key string) []any {
	v, _ := o.Value(key).([]any)
	return v
}

func (o *mosObj) GetString(key string) string {
	v, _ := o.Value(key).(string)
	return v
}

func (o *mosObj) GetBool(key string) bool {
	v, _ := o.Value(key).(bool)
	return v
}

func (o *mosObj) Set(key string, value any) {
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = mosNormalizeVal(value)
}

func (o *mosObj) Delete(key string) {
	if _, exists := o.values[key]; !exists {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *mosObj) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		if o == nil {
			return
		}
		for _, k := range o.keys {
			if !yield(k, o.values[k]) {
				return
			}
		}
	}
}

func (o *mosObj) DeepCopy() *mosObj {
	if o == nil {
		return nil
	}
	cp := newMosObj()
	for _, k := range o.keys {
		cp.Set(k, mosDeepCopy(o.values[k]))
	}
	return cp
}

func mosDeepCopy(v any) any {
	switch x := v.(type) {
	case *mosObj:
		return x.DeepCopy()
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = mosDeepCopy(item)
		}
		return out
	default:
		return v
	}
}

func mosParse(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	val, err := readMosJSON(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON token")
	}
	return val, nil
}

func readMosJSON(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		o := newMosObj()
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := readMosJSON(dec)
			if err != nil {
				return nil, err
			}
			o.Set(k.(string), v)
		}
		_, err := dec.Token()
		return o, err
	case '[':
		list := []any{}
		for dec.More() {
			v, err := readMosJSON(dec)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		_, err := dec.Token()
		return list, err
	}
	return nil, errors.New("unexpected json delimiter")
}

// mosCompact encodes values identically to Python json.dumps(v, ensure_ascii=False, separators=(',', ':')).
func mosCompact(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := mosWriteJSON(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func mosWriteString(buf *bytes.Buffer, s string) {
	const hex = "0123456789abcdef"
	buf.WriteByte('"')
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch b {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if b < 0x20 {
				buf.WriteString(`\u00`)
				buf.WriteByte(hex[b>>4])
				buf.WriteByte(hex[b&0x0f])
			} else {
				buf.WriteByte(b)
			}
		}
	}
	buf.WriteByte('"')
}

func mosWriteJSON(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		mosWriteString(buf, x)
	case json.Number:
		buf.WriteString(string(x))
	case int:
		buf.WriteString(strconv.Itoa(x))
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
	case float64:
		buf.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := mosWriteJSON(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case *mosObj:
		if x == nil {
			buf.WriteString("null")
			return nil
		}
		buf.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			mosWriteString(buf, k)
			buf.WriteByte(':')
			if err := mosWriteJSON(buf, x.values[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("Object of type %T is not JSON serializable", v)
	}
	return nil
}

func mosConfigFromObj(config *mosObj) (*model.Config, error) {
	projection := newMosObj()
	if config != nil {
		for _, key := range []string{"mosdns", "rule_configs", "rules", "rule_sets", "system_config", "profile_id", "rule_library"} {
			if value, ok := config.Get(key); ok {
				projection.Set(key, value)
			}
		}
	}
	data, err := mosCompact(projection)
	if err != nil {
		return nil, err
	}
	return ConfigFromJSON(data)
}

func mosSliceValue(value any) ([]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	out, err := mosParse(data)
	if err != nil {
		return nil, err
	}
	list, ok := out.([]any)
	if !ok {
		return nil, fmt.Errorf("mosdns: expected array")
	}
	return list, nil
}

// Rows of testdata/corpus.jsonl.gz (gen.py) and testdata/recorded.jsonl.gz
// (c21_record.py): {"fn", "args": [...], "out" | "err": [class, message]}.

func loadRows(t *testing.T, path string) []*mosObj {
	t.Helper()
	file, err := testgolden.Open(t, path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<27)
	var rows []*mosObj
	for scanner.Scan() {
		value, err := mosParse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, value.(*mosObj))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func errClass(err error) string {
	if errors.Is(err, ErrRuleProxyToken) {
		return "ValueError"
	}
	var invalidURL *url.Error
	if errors.As(err, &invalidURL) {
		return "ValueError"
	}
	var u *UnsupportedYAMLError
	if errors.As(err, &u) {
		return "Unsupported"
	}
	return fmt.Sprintf("%T", err)
}

func filesValue(files []SourceFile) []any {
	out := []any{}
	for _, f := range files {
		var u any
		if f.URL != nil {
			u = *f.URL
		}
		out = append(out, mosKV("part", f.Part, "url", u, "local_path", f.LocalPath))
	}
	return out
}

func rowNode(node *yamlx.Node) (any, error) {
	data, err := yamlx.ToJSON(node)
	if err != nil {
		if hasNonStringKey(node) {
			return node, nil
		}
		return nil, err
	}
	return mosParse(data)
}

func rowRuleList(rules []model.RuleConfig) ([]any, error) {
	result := []any{}
	for _, rule := range rules {
		raw, err := model.Marshal(rule)
		if err != nil {
			return nil, err
		}
		original, err := mosParse(raw)
		if err != nil {
			return nil, err
		}
		object := original.(*mosObj)
		type ruleDTO model.RuleConfig
		value := ruleDTO(rule)
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		known, err := mosParse(data)
		if err != nil {
			return nil, err
		}
		for key, value := range known.(*mosObj).All() {
			if key == "Fields" || !object.Has(key) {
				continue
			}
			if value == nil {
				if isUnifiedSwitch(key) {
					continue
				}
				object.Delete(key)
			} else {
				object.Set(key, value)
			}
		}
		result = append(result, object)
	}
	return result, nil
}

func strOrNone(v any) string {
	s, _ := v.(string)
	return s
}

func asConfig(v any) (*mosObj, bool) {
	o, ok := v.(*mosObj)
	return o, ok
}

// callRow runs the Go counterpart of one row; ok=false means the arguments
// cannot be expressed in the Go signature.
func callRow(fn string, args []any) (out any, err error, ok bool) {
	arg := func(i int) any {
		if i < len(args) {
			return args[i]
		}
		return nil
	}
	switch fn {
	case "generate", "downloads":
		config, ok1 := asConfig(arg(0))
		base, ok2 := arg(1).(string)
		if len(args) < 2 {
			base, ok2 = "", true
		}
		if !ok1 || !ok2 {
			return nil, nil, false
		}
		native, lerr := mosConfigFromObj(config)
		if lerr != nil {
			return nil, lerr, true
		}
		if fn == "generate" {
			out, err = Generate(native, base)
		} else {
			var dl []Download
			dl, err = Downloads(native, base)
			if err == nil {
				out, err = mosSliceValue(dl)
			}
		}
	case "custom_files":
		config, ok1 := asConfig(arg(0))
		if !ok1 {
			return nil, nil, false
		}
		native, lerr := mosConfigFromObj(config)
		if lerr != nil {
			return nil, lerr, true
		}
		var files []File
		files, err = CustomFiles(native)
		if err == nil {
			out, err = mosSliceValue(files)
		}
	case "split":
		config, ok1 := asConfig(arg(0))
		if !ok1 {
			return nil, nil, false
		}
		native, lerr := mosConfigFromObj(config)
		if lerr != nil {
			return nil, lerr, true
		}
		rules, sets, serr := SplitRules(native)
		if serr != nil {
			return nil, serr, true
		}
		r, rerr := rowRuleList(rules)
		if rerr != nil {
			return nil, rerr, true
		}
		s, err2 := rowRuleList(sets)
		out, err = []any{r, s}, err2
	case "convert":
		content, ok1 := arg(0).(string)
		if !ok1 {
			return nil, nil, false
		}
		if len(args) > 1 && args[1] != nil {
			part, ok2 := args[1].(string)
			if !ok2 {
				return nil, nil, false
			}
			out, err = ConvertRuleContentForMosDNS(content, part)
		} else {
			out, err = ConvertRuleContentForMosDNS(content)
		}
	case "domain_pattern":
		pattern, ok1 := arg(0).(string)
		if !ok1 {
			return nil, nil, false
		}
		if s, found := DomainPatternForMosDNS(pattern); found {
			out = s
		}
	case "parse_clash":
		line, ok1 := arg(0).(string)
		if !ok1 {
			return nil, nil, false
		}
		kind, value := ParseClashRuleValue(line)
		out = []any{kind, value}
	case "upstreams":
		raw := arg(0)
		text, isStr := raw.(string)
		if raw == nil {
			text, isStr = "", true
		}
		if !isStr {
			err = ErrUpstreamType
		} else {
			var nodes []*yamlx.Node
			nodes, err = Upstreams(text)
			if err == nil {
				data, derr := yamlx.Marshal(yamlx.Seq(nodes...))
				if derr != nil {
					return nil, derr, true
				}
				out = mosKV("dump", string(data))
			}
		}
	case "proxy_url":
		config, ok1 := asConfig(arg(0))
		base, ok2 := arg(1).(string)
		url, ok3 := arg(2).(string)
		if !ok1 || !ok2 || !ok3 {
			return nil, nil, false
		}
		native, lerr := mosConfigFromObj(config)
		if lerr != nil {
			return nil, lerr, true
		}
		p, _ := arg(3).(string)
		id, _ := arg(4).(string)
		out, err = RuleProxyURL(native, base, url, p, id)
	case "ruleset_files":
		config, ok1 := asConfig(arg(0))
		base, ok2 := arg(1).(string)
		stem, ok3 := arg(3).(string)
		if arg(3) == nil {
			ok3 = true
		}
		if !ok1 || !ok2 || !ok3 {
			return nil, nil, false
		}
		native, lerr := mosConfigFromObj(config)
		if lerr != nil {
			return nil, lerr, true
		}
		data, cerr := mosCompact(arg(2))
		if cerr != nil {
			return nil, cerr, true
		}
		var item model.RuleConfig
		if uerr := json.Unmarshal(data, &item); uerr != nil {
			return nil, uerr, true
		}
		var files []SourceFile
		files, err = RuleFiles(native, base, item, stem)
		out = filesValue(files)
	case "custom_matches":
		data, cerr := mosCompact(arg(0))
		if cerr != nil {
			return nil, cerr, true
		}
		var fields model.Fields
		if ferr := fields.Set("custom_matches", data); ferr != nil {
			return nil, ferr, true
		}
		raw, merr := fields.MarshalJSON()
		if merr != nil {
			return nil, merr, true
		}
		var settings model.MosDNSSettings
		if uerr := json.Unmarshal(raw, &settings); uerr != nil {
			return nil, uerr, true
		}
		list := []any{}
		for _, node := range MatchEntries(settings.CustomMatches) {
			v, nerr := rowNode(node)
			if nerr != nil {
				return nil, nerr, true
			}
			list = append(list, v)
		}
		out = list
	case "ruleset_parts":
		behavior, _ := arg(0).(string)
		parts := []any{}
		for _, p := range Parts(behavior) {
			parts = append(parts, p)
		}
		out = parts
	default:
		return nil, nil, false
	}
	if list, isFiles := out.([]any); isFiles && list == nil {
		out = []any{}
	}
	return out, err, true
}

// compareRow returns "" when Go matches the row, else a description.
// accepted reports an independently checked native schema or output comparison.
// nonTextRuleProxyToken reports whether a row's configuration stores a
// rule_proxy_token that is present, not null and not text.
func nonTextRuleProxyToken(entry *mosObj) bool {
	args := entry.GetList("args")
	if len(args) == 0 {
		return false
	}
	config, ok := args[0].(*mosObj)
	if !ok {
		return false
	}
	system, ok := config.Value("system_config").(*mosObj)
	if !ok {
		return false
	}
	token, present := system.Get("rule_proxy_token")
	if !present || token == nil {
		return false
	}
	_, text := token.(string)
	return !text
}

// splitStoredForm is a split result (two lists of rules) as plain
// model.RuleConfig records write it.
func splitStoredForm(value any) (any, bool) {
	data, err := mosCompact(value)
	if err != nil {
		return nil, false
	}
	var lists [][]model.RuleConfig
	if json.Unmarshal(data, &lists) != nil {
		return nil, false
	}
	out := make([]any, len(lists))
	for i, list := range lists {
		rules, err := rowRuleList(list)
		if err != nil {
			return nil, false
		}
		out[i] = rules
	}
	return out, true
}

func compareRow(t *testing.T, oracle *goldenOracle, entry *mosObj) (msg string, accepted, supported bool) {
	fn := entry.GetString("fn")
	args := entry.GetList("args")
	out, err, ok := callRow(fn, args)
	if !ok {
		return "", false, false
	}
	if fn == "split" && err == nil && !entry.Has("err") && !yamlxtest.EqualJSON(entry.Value("out"), out) {
		// Accepted O18 T1c difference: Python returned the stored rule dicts
		// as they were ({"id": null}, "id": 3, "value": ""); the split rules
		// are plain records holding the read values. Python's lists are
		// compared in the records' stored form, so clean values stay exact.
		if want, ok := splitStoredForm(entry.Value("out")); ok {
			if gotForm, ok := splitStoredForm(out); ok && yamlxtest.EqualJSON(want, gotForm) {
				return "", true, true
			}
		}
	}
	if !yamlxtest.EqualJSON(entry.Value("out"), out) || err != nil || entry.Has("err") {
		if nativeR5Outcome(t, oracle, entry, out, err) || nativeMosDNSOutcome(t, oracle, entry, out, err) {
			return "", true, true
		}
	}
	if want := entry.GetList("err"); want != nil {
		if err == nil {
			return fmt.Sprintf("want %v, got output %s", want, compact(out)), false, true
		}
		if fn == "convert" && want[0] == "TypeError" && err.Error() == PayloadCollectionMessage {
			return "", true, true
		}
		class := errClass(err)
		// Exception texts are compared only when Chinese (user-facing); URL
		// parsing moved to net/url in N2 and its error texts differ.
		chinese := strings.ContainsFunc(fmt.Sprint(want[1]), func(r rune) bool { return r >= 0x4e00 && r <= 0x9fff })
		if class != want[0] || (chinese && err.Error() != want[1]) {
			return fmt.Sprintf("want %v, got [%s %q]", want, class, err.Error()), false, true
		}
		return "", false, true
	}
	if err != nil {
		return fmt.Sprintf("want output, got [%s %q]", errClass(err), err.Error()), false, true
	}
	want, _ := entry.Get("out")
	got, want2 := compact(out), compact(want)
	if got != want2 && fn == "split" && yamlxtest.EqualJSON(want, out) {
		// Split rules are plain records (O18 T1c): members are written in
		// declaration order, not in the stored order Python kept.
		return "", false, true
	}
	if got != want2 {
		if fn == "generate" {
			a, aok := out.(string)
			b, bok := want.(string)
			if aok && bok && yamlxtest.SemanticEqual(a, b) {
				return "", false, true
			}
		}
		if fn == "upstreams" {
			a, aok := out.(*mosObj)
			b, bok := want.(*mosObj)
			if aok && bok && a.Len() == 1 && b.Len() == 1 && yamlxtest.SemanticEqual(a.GetString("dump"), b.GetString("dump")) {
				return "", false, true
			}
		}
		return fmt.Sprintf("output differs:\n got %s\nwant %s", got, want2), false, true
	}
	return "", false, true
}

func TestCorpus(t *testing.T) {
	t.Parallel()
	oracle := newGoldenOracle(t, "server/internal/mosdns/testdata/n73_reference.py")
	rows := loadRows(t, "testdata/corpus.jsonl.gz")
	failures, accepted, unsupported, pendingToken := 0, 0, 0, 0
	counts := map[string]int{}
	for i, entry := range rows {
		counts[entry.GetString("fn")]++
		msg, acc, ok := compareRow(t, oracle, entry)
		switch {
		case ok && !acc && msg != "" && nonTextRuleProxyToken(entry):
			// Suspected product regression, handed back to the T1c author:
			// a rule_proxy_token stored as a number (or another non-text
			// value) reads as its text and is used as the token, where
			// Python ignored it ("MosDNS rule proxy requires a rule proxy
			// token"). Counted so that a fix is noticed.
			pendingToken++
		case !ok:
			unsupported++
		case acc:
			accepted++
			if os.Getenv("C21_SHOW_ACCEPTED") != "" && accepted <= 400 {
				t.Logf("accepted row %d %s %s", i, entry.GetString("fn"), compact(entry.GetList("args")))
			}
		case msg != "":
			failures++
			if failures <= 25 {
				t.Errorf("row %d %s %s: %s", i, entry.GetString("fn"), compact(entry.GetList("args")), msg)
			}
		}
	}
	t.Logf("%d corpus rows %v; %d native/structural comparisons, %d not expressible, %d differ, %d pending (non-text rule_proxy_token)", len(rows), counts,
		accepted, unsupported, failures, pendingToken)
	// The token is read strictly (stored:"secret"): no row is pending.
	if pendingToken != 0 {
		t.Errorf("pending non-text rule_proxy_token rows = %d, want 0", pendingToken)
	}
	if failures > 0 {
		t.Errorf("%d of %d rows differ", failures, len(rows))
	}
}

func compact(v any) string {
	b, err := mosCompact(v)
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	return string(b)
}
