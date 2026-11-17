package rules

import (
	"encoding/json"
	"errors"
	"fmt"
)

// OrderIDs validates the two supported reorder payloads before a write starts.
// Missing IDs are resolved separately against the current stored collection.
func OrderIDs(body map[string]json.RawMessage, collection string) ([]string, error) {
	raw := body["ids"]
	var entries []json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		if value := body[collection]; len(value) == 0 || string(value) == "null" || json.Unmarshal(value, &entries) != nil {
			return nil, fmt.Errorf(msgOrderObjects, collection)
		}
		for i, entry := range entries {
			var object map[string]json.RawMessage
			if json.Unmarshal(entry, &object) != nil || object == nil {
				return nil, fmt.Errorf(msgOrderObjects, collection)
			}
			entries[i] = object["id"]
		}
	} else if json.Unmarshal(raw, &entries) != nil {
		return nil, errors.New(msgOrderIDs)
	}
	ids := make([]string, len(entries))
	seen := map[string]bool{}
	for i, entry := range entries {
		if len(entry) == 0 || string(entry) == "null" || json.Unmarshal(entry, &ids[i]) != nil || seen[ids[i]] {
			return nil, errors.New(msgOrderUnique)
		}
		seen[ids[i]] = true
	}
	return ids, nil
}
