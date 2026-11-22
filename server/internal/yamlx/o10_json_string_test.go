package yamlx

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func encodingJSONString(s string) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// writeJSONString writes exactly what encoding/json writes (O10 R9 replaced
// a json.Encoder per string with a direct writer).
func TestWriteJSONString(t *testing.T) {
	inputs := []string{"", "plain", `quote " and \ backslash`, "<html> & 'x'", "  ", "日本語 🇭🇰", "\xff\xfe broken \xc3", "a\x00b", "\x7f", "�"}
	for c := 0; c < 0x100; c++ {
		inputs = append(inputs, string(rune(c)), string([]byte{byte(c)}))
	}
	for _, s := range inputs {
		var b bytes.Buffer
		writeJSONString(&b, s)
		if want := encodingJSONString(s); b.String() != want {
			t.Errorf("%q: got %s, want %s", s, b.String(), want)
		}
	}
}

func FuzzWriteJSONString(f *testing.F) {
	for _, seed := range []string{"", "a\"b\\c\n\t\r\x01", " x ", "\xffbad", "混合 text"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		var b bytes.Buffer
		writeJSONString(&b, s)
		if want := encodingJSONString(s); b.String() != want {
			t.Fatalf("%q: got %s, want %s", s, b.String(), want)
		}
		if !utf8.Valid(b.Bytes()) {
			t.Fatalf("invalid UTF-8 output for %q", s)
		}
	})
}
