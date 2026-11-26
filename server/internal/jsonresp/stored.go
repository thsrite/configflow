package jsonresp

import (
	"bytes"
	"configflow/internal/jsondecode"
	"fmt"
	"unicode/utf8"
)

// DecodeStored reads a persisted legacy JSON value. Only the three non-finite
// numeric literals are replaced with null, leaving every other field intact.
// This compatibility function must never be used for request binding.
func DecodeStored(data []byte, dst any) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("decode stored JSON: invalid UTF-8")
	}
	data, _ = NormalizeNonFinite(data)
	if err := jsondecode.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode stored JSON: %w", err)
	}
	return nil
}

// NormalizeNonFinite repairs only the three legacy non-JSON numeric literals, outside
// quoted strings. Native validation still rejects any other malformed syntax.
func NormalizeNonFinite(data []byte) ([]byte, bool) {
	if !bytes.Contains(data, []byte("NaN")) && !bytes.Contains(data, []byte("Infinity")) {
		return data, false
	}
	var out bytes.Buffer
	quoted, escaped, changed := false, false, false
	for i := 0; i < len(data); {
		b := data[i]
		if quoted {
			out.WriteByte(b)
			i++
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				quoted = false
			}
			continue
		}
		if b == '"' {
			quoted = true
			out.WriteByte(b)
			i++
			continue
		}
		matched := false
		for _, literal := range []string{"-Infinity", "Infinity", "NaN"} {
			if !bytes.HasPrefix(data[i:], []byte(literal)) {
				continue
			}
			end := i + len(literal)
			before := i == 0 || bytes.IndexByte([]byte("[,: \t\r\n"), data[i-1]) >= 0
			after := end == len(data) || bytes.IndexByte([]byte("],} \t\r\n"), data[end]) >= 0
			if before && after {
				out.WriteString("null")
				i = end
				changed = true
				matched = true
				break
			}
		}
		if !matched {
			out.WriteByte(b)
			i++
		}
	}
	if !changed {
		return data, false
	}
	return out.Bytes(), true
}
