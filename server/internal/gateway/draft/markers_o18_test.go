package draft

import "encoding/json"

// markerSet and markerGet keep a test marker in a declared text member: plain
// records (O18 T1c) do not carry undeclared members.
func markerSet(target *string, raw json.RawMessage) error {
	*target = string(raw)
	return nil
}

func markerGet(raw json.RawMessage) (json.RawMessage, bool) {
	return raw, len(raw) > 0
}
