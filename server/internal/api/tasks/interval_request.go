package tasks

import (
	"encoding/json"

	"configflow/internal/httpx"
)

// intervalRequest keeps field presence and exact names independently of values.
// R226 permits exactly two members; encoding/json's default case-insensitive
// struct field matching and unknown-member ignoring would widen that contract.
type intervalRequest struct {
	Key      string
	Interval httpx.Number
	valid    bool
}

// UnmarshalJSON retains presence and field forms for the interval validator.
func (r *intervalRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = intervalRequest{}
	if len(fields) != 2 || fields["key"] == nil || fields["interval_seconds"] == nil {
		return nil
	}
	if err := json.Unmarshal(fields["key"], &r.Key); err != nil {
		return nil
	}
	if err := json.Unmarshal(fields["interval_seconds"], &r.Interval); err != nil {
		return err
	}
	r.valid = r.Key != ""
	return nil
}

// interval keeps lexical validation at the HTTP boundary.
func (r intervalRequest) interval() *int64 {
	if r.Interval.IsIntegerLiteral() {
		if value, err := r.Interval.Int64(); err == nil {
			return &value
		}
	}
	return nil
}
