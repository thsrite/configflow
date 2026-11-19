package jsonresp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// EncodeDevice retains the Agent JSON wire format: ASCII string escapes and
// single spaces after object/list separators. Native JSON encoding determines
// values and member order; no Python value coercion or scalar parser is used.
func EncodeDevice(value any) ([]byte, error) {
	spaced, err := EncodeDeviceUTF8(value)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 0, len(spaced))
	for _, character := range string(spaced) {
		if character < 0x7f {
			result = append(result, byte(character))
			continue
		}
		if character <= 0xffff {
			result = fmt.Appendf(result, `\u%04x`, character)
			continue
		}
		high, low := utf16.EncodeRune(character)
		result = fmt.Appendf(result, `\u%04x\u%04x`, high, low)
	}
	return result, nil
}

// EncodeDeviceUTF8 retains separator spacing while sending literal UTF-8 text.
// The legacy config endpoint explicitly negotiates this form with its charset.
func EncodeDeviceUTF8(value any) ([]byte, error) {
	data, err := EncodeUTF8(value)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("device JSON must be UTF-8")
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, data, "", ""); err != nil {
		return nil, fmt.Errorf("format device JSON: %w", err)
	}
	spaced := bytes.ReplaceAll(formatted.Bytes(), []byte(",\n"), []byte(", "))
	return bytes.ReplaceAll(spaced, []byte("\n"), nil), nil
}
