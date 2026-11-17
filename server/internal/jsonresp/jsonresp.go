// Package jsonresp encodes Go values with encoding/json and UTF-8 strings.
// It does not apply HTTP policy; all JSON responses are finalized by httpx.
package jsonresp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Encode writes JSON without HTML escaping or a trailing newline. Legacy
// jsonx objects participate through their encoding/json bridge.
func Encode(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}

// EncodeUTF8 writes JSON data with literal Unicode line/paragraph separators.
// Unlike JavaScript source, JSON permits U+2028 and U+2029 in strings. Keeping
// them as UTF-8 makes byte-budgeted JSON records independent of JS escaping.
// Literal backslash-u text remains escaped and is never changed.
func EncodeUTF8(value any) ([]byte, error) {
	data, err := Encode(value)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] == '\\' && i+1 < len(data) {
			if i+5 < len(data) && data[i+1] == 'u' && data[i+2] == '2' && data[i+3] == '0' && data[i+4] == '2' && (data[i+5] == '8' || data[i+5] == '9') {
				out = append(out, 0xe2, 0x80, 0xa8+(data[i+5]-'8'))
				i += 5
				continue
			}
			out = append(out, data[i], data[i+1])
			i++
			continue
		}
		out = append(out, data[i])
	}
	return out, nil
}
