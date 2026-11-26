package notifications

import "encoding/json"

type readRequest struct{ Versions map[string]string }

// UnmarshalJSON preserves exact key spelling and rejects null version values.
func (r *readRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = readRequest{}
	var versions map[string]json.RawMessage
	if err := json.Unmarshal(fields["versions"], &versions); err != nil {
		return err
	}
	if versions == nil {
		return nil
	}
	r.Versions = make(map[string]string, len(versions))
	for key, raw := range versions {
		var value *string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if value == nil {
			r.Versions = nil
			return nil
		}
		r.Versions[key] = *value
	}
	return nil
}

func (r readRequest) valid() bool {
	if r.Versions == nil || len(r.Versions) > 2000 {
		return false
	}
	for key, version := range r.Versions {
		if len([]rune(key)) > 1024 || len([]rune(version)) > 100 {
			return false
		}
	}
	return true
}
