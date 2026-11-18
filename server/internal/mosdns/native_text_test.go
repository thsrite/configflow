package mosdns

import (
	"fmt"
	"testing"

	"configflow/internal/jsonx"
)

func TestNativeRuleTextAgainstPython(t *testing.T) {
	// These are domain labels, rule-type keywords and classical rule payloads,
	// not opaque passwords or IDs. Full Unicode case mapping retains the
	// existing sharp-s, dotted-I and contextual sigma behavior.
	oracle := newGoldenOracle(t, "server/internal/mosdns/testdata/n73_reference.py")
	tests := []struct {
		name, fn, input string
		native, python  any
	}{
		{"sharp-s keyword", "parse_clash", "straße,value", []any{"STRASSE", "value"}, []any{"STRASSE", "value"}},
		{"Turkish keyword", "parse_clash", "iıİ,example.com", []any{"IIİ", "example.com"}, []any{"IIİ", "example.com"}},
		{"sharp-s domain", "domain_pattern", "STRAßE.Example", "full:straße.example", "full:straße.example"},
		{"dotted-I domain", "domain_pattern", "İ.Example", "full:i\u0307.example", "full:i\u0307.example"},
		{"final sigma domain", "domain_pattern", "ΟΣ.123", "full:ος.123", "full:ος.123"},
		{"Unicode whitespace", "parse_clash", "\u00a0domain,example.com\u00a0", []any{"DOMAIN", "example.com"}, []any{"DOMAIN", "example.com"}},
		{"generated domain lines", "convert", "STRAßE.Example\nİ.Example", "full:straße.example\nfull:i\u0307.example", "full:straße.example\nfull:i\u0307.example"},
		{"unit separator output", "convert", "DOMAIN,example.com\x1f", "full:example.com\x1f", "full:example.com"},
	}
	for _, control := range []rune{0x1c, 0x1d, 0x1e, 0x1f} {
		text := string(control)
		tests = append(tests, struct {
			name, fn, input string
			native, python  any
		}{
			fmt.Sprintf("control U+%04X", control), "parse_clash", text + "DOMAIN,example.com" + text,
			[]any{text + "DOMAIN", "example.com" + text}, []any{"DOMAIN", "example.com"},
		})
		if control != 0x1f {
			tests = append(tests, struct {
				name, fn, input string
				native, python  any
			}{
				fmt.Sprintf("line separator U+%04X", control), "convert", "DOMAIN,example.com" + text,
				"full:example.com", "full:example.com",
			})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []any{tt.input}
			got, err, ok := callRow(tt.fn, args)
			if err != nil || !ok || !jsonx.Equal(got, tt.native) {
				t.Fatalf("native value=%q error=%v", got, err)
			}
			reference := oracle.Call(t, jsonx.Obj("fn", tt.fn, "args", args))
			if reference.Has("err") || !jsonx.Equal(reference.Value("out"), tt.python) {
				t.Fatalf("Python value=%v", reference)
			}
		})
	}
}
