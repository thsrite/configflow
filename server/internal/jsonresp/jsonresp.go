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
