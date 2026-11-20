package mosdns

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

// Rows of testdata/corpus.jsonl.gz (gen.py) and testdata/recorded.jsonl.gz
// (c21_record.py): {"fn", "args": [...], "out" | "err": [class, message]}.

func loadRows(t *testing.T, path string) []*jsonx.Object {
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
	var rows []*jsonx.Object
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, value.(*jsonx.Object))
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
		out = append(out, jsonx.Obj("part", f.Part, "url", u, "local_path", f.LocalPath))
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
	return jsonx.Parse(data)
}

func rowRuleList(rules []model.RuleConfig) ([]any, error) {
	result := []any{}
	for _, rule := range rules {
		raw, err := model.Marshal(rule)
		if err != nil {
			return nil, err
		}
		original, err := jsonx.Parse(raw)
		if err != nil {
			return nil, err
		}
		object := original.(*jsonx.Object)
		type ruleDTO model.RuleConfig
		value := ruleDTO(rule)
		value.Fields = model.Fields{}
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		known, err := jsonx.Parse(data)
		if err != nil {
			return nil, err
		}
		for key, value := range known.(*jsonx.Object).All() {
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

func asConfig(v any) (*jsonx.Object, bool) {
	o, ok := v.(*jsonx.Object)
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
		if fn == "generate" {
			out, err = GenerateMosDNSConfig(config, base)
		} else {
			out, err = GetMosDNSRulesetDownloads(config, base)
		}
	case "custom_files":
		config, ok1 := asConfig(arg(0))
		if !ok1 {
			return nil, nil, false
		}
		out, err = GetMosDNSCustomFiles(config)
	case "split":
		config, ok1 := asConfig(arg(0))
		if !ok1 {
			return nil, nil, false
		}
		native, lerr := legacyConfig(config)
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
				out = jsonx.Obj("dump", string(data))
			}
		}
	case "proxy_url":
		config, ok1 := asConfig(arg(0))
		base, ok2 := arg(1).(string)
		url, ok3 := arg(2).(string)
		if !ok1 || !ok2 || !ok3 {
			return nil, nil, false
		}
		native, lerr := legacyConfig(config)
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
		native, lerr := legacyConfig(config)
		if lerr != nil {
			return nil, lerr, true
		}
		data, cerr := jsonx.Compact(arg(2))
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
		data, cerr := jsonx.Compact(arg(0))
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
func compareRow(t *testing.T, oracle *goldenOracle, entry *jsonx.Object) (msg string, accepted, supported bool) {
	fn := entry.GetString("fn")
	args := entry.GetList("args")
	out, err, ok := callRow(fn, args)
	if !ok {
		return "", false, false
	}
	if !jsonx.Equal(entry.Value("out"), out) || err != nil || entry.Has("err") {
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
	if got != want2 {
		if fn == "generate" {
			a, aok := out.(string)
			b, bok := want.(string)
			if aok && bok && yamlxtest.SemanticEqual(a, b) {
				return "", false, true
			}
		}
		if fn == "upstreams" {
			a, aok := out.(*jsonx.Object)
			b, bok := want.(*jsonx.Object)
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
	failures, accepted, unsupported := 0, 0, 0
	counts := map[string]int{}
	for i, entry := range rows {
		counts[entry.GetString("fn")]++
		msg, acc, ok := compareRow(t, oracle, entry)
		switch {
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
	t.Logf("%d corpus rows %v; %d native/structural comparisons, %d not expressible, %d differ", len(rows), counts,
		accepted, unsupported, failures)
	if failures > 0 {
		t.Errorf("%d of %d rows differ", failures, len(rows))
	}
}

func compact(v any) string {
	b, err := jsonx.Compact(v)
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	return string(b)
}
