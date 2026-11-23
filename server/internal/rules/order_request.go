package rules

import (
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/jsondecode"
)

// Order is a validated reorder request of a shared collection. Missing IDs
// are resolved separately against the current stored collection.
type Order struct {
	IDs      []string
	Position string
}

// orderWire is a reorder request as sent: ids, or (older clients) the
// collection's objects; any JSON value may stand in an identifier position.
type orderWire struct {
	IDs   jsondecode.Member[[]json.RawMessage]
	Items jsondecode.Member[[]*struct {
		ID json.RawMessage `json:"id"`
	}]
	Position jsondecode.Member[string]
}

// DecodeOrder validates the two supported reorder payloads (an object) before
// a write starts. collection names the member older clients send their
// objects in; an absent position is "top".
func DecodeOrder(body []byte, collection string) (Order, error) {
	members, err := jsondecode.Select(body, "ids", collection, "position")
	if err != nil {
		return Order{}, err
	}
	var sent orderWire
	for _, member := range []struct {
		name   string
		target json.Unmarshaler
	}{{"ids", &sent.IDs}, {collection, &sent.Items}, {"position", &sent.Position}} {
		if raw, present := members[member.name]; present {
			_ = member.target.UnmarshalJSON(raw)
		}
	}
	var entries []json.RawMessage
	if sent.IDs.Null() {
		if !sent.Items.OK {
			return Order{}, fmt.Errorf(msgOrderObjects, collection)
		}
		for _, item := range sent.Items.Value {
			if item == nil {
				return Order{}, fmt.Errorf(msgOrderObjects, collection)
			}
			entries = append(entries, item.ID)
		}
	} else if !sent.IDs.OK {
		return Order{}, errors.New(msgOrderIDs)
	} else {
		entries = sent.IDs.Value
	}
	ids := make([]string, len(entries))
	seen := map[string]bool{}
	for i, entry := range entries {
		if len(entry) == 0 || string(entry) == "null" || json.Unmarshal(entry, &ids[i]) != nil || seen[ids[i]] {
			return Order{}, errors.New(msgOrderUnique)
		}
		seen[ids[i]] = true
	}
	return Order{IDs: ids, Position: sent.Position.Or("top")}, nil
}

// OrderIDs is DecodeOrder's IDs for callers still holding a decoded object.
//
// Deprecated: transitional until every editor decodes its request DTO.
func OrderIDs(body map[string]json.RawMessage, collection string) ([]string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	order, err := DecodeOrder(data, collection)
	return order.IDs, err
}
