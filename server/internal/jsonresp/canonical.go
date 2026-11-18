package jsonresp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Canonical re-encodes a complete JSON value with native sorted object keys.
// Numbers stay as json.Number, retaining their exact original number literal.
// It is for identities of JSON values, never for repairing request input.
func Canonical(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("multiple JSON values")
	}
	return Encode(value)
}
