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

// Writer rows are now checked by writer_reference_test.go through the native
// Marshal/Parse/Equal path and independent real Python semantic references.

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
