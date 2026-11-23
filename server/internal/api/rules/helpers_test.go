package rules

import (
	"encoding/json"
	"fmt"
)

// record builds request JSON in tests.
type record map[string]json.RawMessage

func stringJSON(value string) json.RawMessage { raw, _ := json.Marshal(value); return raw }

func rawJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

// request decodes value's JSON into a request DTO, as the routes do.
func request[T any](value any) *T {
	out, err := decodeRequest[T](rawJSON(value))
	if err != nil {
		panic(fmt.Sprintf("decode request: %v", err))
	}
	return out
}

// sentMember is a member as decoded from raw.
func sentMember[T any](raw string) member[T] {
	var out member[T]
	_ = out.UnmarshalJSON([]byte(raw))
	return out
}
