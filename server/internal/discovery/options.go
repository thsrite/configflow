package discovery

import (
	"bytes"
	"encoding/json"
)

// JSONOptionEnabled decodes the legacy JSON option contract: empty containers,
// null, false, an empty string and numeric zero disable an option. It reads only
// the leading token, so checking a large container does not allocate its contents.
// It does not interpret arbitrary Go values or emulate language runtime types.
func JSONOptionEnabled(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decoder.Token()
	if err != nil {
		return false
	}
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case json.Number:
		number, _ := v.Float64()
		return number != 0
	case json.Delim:
		return (v == '[' || v == '{') && decoder.More()
	default:
		return false
	}
}
