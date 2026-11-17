package mosdns

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

// Rows of testdata/corpus.jsonl.gz (gen.py) and testdata/recorded.jsonl.gz
// (c21_record.py): {"fn", "args": [...], "out" | "err": [class, message]}.

func loadRows(t *testing.T, path string) []*jsonx.Object {
	t.Helper()
	file, err := os.Open(path)
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
	var u *UnsupportedYAMLError
	if errors.As(err, &u) {
		return "Unsupported"
	}
	if class := convert.PyErrorClass(err); class != "" {
		return class
	}
	return fmt.Sprintf("%T", err)
}

func filesValue(files []RulesetFile) []any {
	out := []any{}
	for _, f := range files {
		out = append(out, jsonx.Obj("part", f.Part, "url", f.URL, "local_path", f.LocalPath))
	}
	return out
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
		var rules, sets any
		rules, sets, err = SplitRulesAndRulesets(config)
		out = []any{rules, sets}
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
		var list []any
		list, err = ParseDNSUpstreams(arg(0))
		if err == nil {
			node, derr := yamlbridge.Encode(list)
			if derr != nil {
				return nil, derr, true
			}
			data, derr := yamlx.Marshal(node)
			if derr != nil {
				return nil, derr, true
			}
			text := string(data)
			out = jsonx.Obj("dump", text)
		}
	case "proxy_url":
		config, ok1 := asConfig(arg(0))
		base, ok2 := arg(1).(string)
		url, ok3 := arg(2).(string)
		if !ok1 || !ok2 || !ok3 {
			return nil, nil, false
		}
		out, err = BuildRuleProxyURL(config, base, url, arg(3), arg(4))
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
		var files []RulesetFile
		files, err = RulesetFiles(config, base, arg(2), stem)
		out = filesValue(files)
	case "custom_matches":
		out, err = ParseCustomMatches(arg(0))
	case "ruleset_parts":
		parts := []any{}
		for _, p := range RulesetParts(arg(0)) {
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
func compareRow(t *testing.T, oracle *yamlxtest.Oracle, entry *jsonx.Object) (msg string, accepted, supported bool) {
	fn := entry.GetString("fn")
	args := entry.GetList("args")
	out, err, ok := callRow(fn, args)
	if !ok {
		return "", false, false
	}
	if !jsonx.Equal(entry.Value("out"), out) || err != nil || entry.Has("err") {
		if nativeMosDNSOutcome(t, oracle, entry, out, err) {
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
	oracle := yamlxtest.NewOracle(t, "server/internal/mosdns/testdata/n73_reference.py")
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
