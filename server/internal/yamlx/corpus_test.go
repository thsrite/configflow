package yamlx

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// The corpus is produced by testdata/gen.py with PyYAML 6 on CPython 3.11.

type corpusLine struct {
	Kind    string          `json:"kind"`
	Input   string          `json:"input"`
	Out     json.RawMessage `json:"out"`
	Options string          `json:"options"`
	Spec    json.RawMessage `json:"spec"`
	Text    string          `json:"text"`
	Safe    loadOutcome     `json:"safe"`
	Native  loadOutcome     `json:"native"`
}

type loadOutcome struct {
	Value   *string `json:"value"`
	Error   string  `json:"error"`
	Message string  `json:"message"`
	Special string  `json:"special"`
}

func readCorpus(t *testing.T) []corpusLine {
	t.Helper()
	file, err := os.Open("testdata/corpus.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var lines []corpusLine
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	for scanner.Scan() {
		var line corpusLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

// optionSets maps the names in gen.py's OPTION_SETS to DumpOptions.
var optionSets = map[string]DumpOptions{
	"safe":          {SortKeys: true},
	"safe_u":        {AllowUnicode: true},
	"safe_u_sorted": {AllowUnicode: true, SortKeys: true},
	"indent":        {AllowUnicode: true, Indent: 2, IndentSequences: true},
	"indent_sorted": {SortKeys: true, IndentSequences: true},
	"ruleset":       {AllowUnicode: true, Width: 1<<31 - 1},
	"compose":       {AllowUnicode: true, Width: 1000},
	"flow_true":     {AllowUnicode: true, FlowStyle: FlowFlow},
	"flow_auto":     {AllowUnicode: true, SortKeys: true, FlowStyle: FlowAuto},
	"indent4_w30":   {AllowUnicode: true, Indent: 4, Width: 30},
	"explicit":      {AllowUnicode: true, SortKeys: true, ExplicitStart: true, ExplicitEnd: true},
}

func expectedText(t *testing.T, raw json.RawMessage) (text string, isError bool) {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, false
	}
	return "", true
}

func TestDumpMatchesPyYAML(t *testing.T) {
	counts := map[string]int{}
	csafeDiffers := 0
	cases := 0
	for index, line := range readCorpus(t) {
		if line.Kind != "dump" {
			continue
		}
		cases++
		var out map[string]json.RawMessage
		if err := json.Unmarshal(line.Out, &out); err != nil {
			t.Fatal(err)
		}
		value, err := jsonx.ParseString(line.Input)
		if err != nil {
			t.Fatalf("case %d: %v", index, err)
		}
		for name, options := range optionSets {
			want, isError := expectedText(t, out[name])
			got, err := Dump(value, options)
			if isError {
				if err == nil {
					t.Errorf("case %d %s input %s: Python raised, Go wrote %q", index, name, line.Input, got)
				}
				continue
			}
			if err != nil {
				t.Errorf("case %d %s input %s: %v", index, name, line.Input, err)
				continue
			}
			if got != want {
				t.Errorf("case %d %s input %s:\n got %q\nwant %q", index, name, line.Input, got, want)
				continue
			}
			counts[name]++
		}
		if csafe, isError := expectedText(t, out["csafe"]); !isError {
			if safe, _ := expectedText(t, out["safe_u"]); csafe != safe {
				csafeDiffers++
			}
		}
	}
	for name := range optionSets {
		t.Logf("%-14s %d cases byte-identical", name, counts[name])
	}
	t.Logf("dump cases: %d; CSafeDumper text differs from SafeDumper text (information only): %d", cases, csafeDiffers)
	if cases < 1000 {
		t.Fatalf("corpus has only %d dump cases", cases)
	}
}

type specEnv map[string]any

func buildSpec(spec any, env specEnv) any {
	switch v := spec.(type) {
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, buildSpec(item, env))
		}
		return out
	case *jsonx.Object:
		if name, ok := v.Get("$ref"); ok {
			return env[name.(string)]
		}
		if name, ok := v.Get("$define"); ok {
			value := buildSpec(v.Value("value"), env)
			env[name.(string)] = value
			return value
		}
		if m, ok := v.Get("$flowmap"); ok {
			return FlowMap{buildSpec(m, env).(*jsonx.Object)}
		}
		if l, ok := v.Get("$flowlist"); ok {
			return FlowList(buildSpec(l, env).([]any))
		}
		if image, ok := v.Get("$image"); ok {
			return &SharedString{Value: image.(string), Anchor: "agent_image"}
		}
		out := jsonx.NewObject()
		for k, item := range v.All() {
			out.Set(k, buildSpec(item, env))
		}
		return out
	}
	return spec
}

// Specs the Go representation cannot express: PyYAML anchors a shared empty
// list, but an empty Go slice has no identity.
var specKnownDifferent = map[int]string{3: "shared empty list"}

func TestSpecsMatchPyYAML(t *testing.T) {
	index, matched := -1, 0
	for _, line := range readCorpus(t) {
		if line.Kind != "spec" {
			continue
		}
		index++
		spec, err := jsonx.Parse(line.Spec)
		if err != nil {
			t.Fatal(err)
		}
		var want string
		if err := json.Unmarshal(line.Out, &want); err != nil {
			t.Fatal(err)
		}
		got, err := Dump(buildSpec(spec, specEnv{}), optionSets[line.Options])
		if err != nil {
			t.Errorf("spec %d: %v", index, err)
			continue
		}
		if reason, known := specKnownDifferent[index]; known {
			if got == want {
				t.Errorf("spec %d (%s) now matches; drop it from specKnownDifferent", index, reason)
			}
			continue
		}
		if got != want {
			t.Errorf("spec %d %s %s:\n got %q\nwant %q", index, line.Options, line.Spec, got, want)
			continue
		}
		matched++
	}
	t.Logf("%d spec cases byte-identical (anchors/aliases, FlowMap/FlowList, shared strings)", matched)
}

// normalize turns the YAML-only types into JSON-encodable values, like enc() in gen.py.
func normalize(v any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		out := jsonx.NewObject()
		for k, item := range x.All() {
			out.Set(k, normalize(item))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalize(item)
		}
		return out
	case Timestamp:
		return jsonx.Obj("$ts", x.String())
	case []byte:
		return jsonx.Obj("$bytes", hex.EncodeToString(x))
	}
	return v
}

// nativeKeyCheck is NativeLoader.construct_mapping's key check
// (OxidnsSettingsError messages).
func nativeKeyCheck(key any, duplicate bool) error {
	name, ok := key.(string)
	if !ok || name == "__proto__" || name == "prototype" || name == "constructor" {
		return &Error{Class: "OxidnsSettingsError", Msg: "OxiDNS YAML 字段名必须为安全文本"}
	}
	if duplicate {
		return &Error{Class: "OxidnsSettingsError", Msg: "OxiDNS YAML 包含重复字段"}
	}
	return nil
}

// checkLoad compares one load with Python: the value, or the exception class
// and message exactly, or (for values JSON cannot encode) the matching
// representability error.
func checkLoad(t *testing.T, name, text string, want loadOutcome, options LoadOptions, load func(string, LoadOptions) (any, error)) bool {
	t.Helper()
	got, err := load(text, options)
	switch {
	case want.Value != nil:
		if err != nil {
			t.Errorf("%s %q: Python loaded %s, Go failed: %v", name, text, *want.Value, err)
			return false
		}
		expected, perr := jsonx.ParseString(*want.Value)
		if perr != nil {
			t.Fatal(perr)
		}
		wantBytes, _ := jsonx.Compact(expected)
		gotBytes, cerr := jsonx.Compact(normalize(got))
		if cerr != nil {
			t.Errorf("%s %q: %v", name, text, cerr)
			return false
		}
		if string(gotBytes) != string(wantBytes) {
			t.Errorf("%s %q:\n got %s\nwant %s", name, text, gotBytes, wantBytes)
			return false
		}
	case want.Special != "":
		var nonString *NonStringKeyError
		var unrepresentable *UnrepresentableError
		switch {
		case want.Special == "nonstring" && errors.As(err, &nonString):
		case want.Special == "unsupported" && errors.As(err, &unrepresentable) && unrepresentable.Kind != "recursive":
		case strings.Contains(want.Special, "recursion") && errors.As(err, &unrepresentable) && unrepresentable.Kind == "recursive":
		default:
			t.Errorf("%s %q: Python gave a value jsonx cannot hold (%s), Go: %T, %v", name, text, want.Special, got, err)
			return false
		}
	default:
		var pyErr *Error
		if !errors.As(err, &pyErr) {
			t.Errorf("%s %q: Python raised %s: %s\nGo: %T, %v", name, text, want.Error, want.Message, got, err)
			return false
		}
		if pyErr.Class != want.Error || pyErr.Msg != want.Message {
			t.Errorf("%s %q:\n got %s: %s\nwant %s: %s", name, text, pyErr.Class, pyErr.Msg, want.Error, want.Message)
			return false
		}
	}
	return true
}

func TestLoadMatchesPyYAML(t *testing.T) {
	safe, native, total := 0, 0, 0
	for _, line := range readCorpus(t) {
		if line.Kind != "load" {
			continue
		}
		total++
		if checkLoad(t, "safe_load", line.Text, line.Safe, LoadOptions{}, LoadWith) {
			safe++
		}
		if checkLoad(t, "native", line.Text, line.Native, LoadOptions{Schema: SchemaNative, Key: nativeKeyCheck}, LoadWith) {
			native++
		}
	}
	t.Logf("%d texts; equal to PyYAML (value, or exception class and message): safe_load %d, NativeLoader %d", total, safe, native)
}
