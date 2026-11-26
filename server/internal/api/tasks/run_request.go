package tasks

import "encoding/json"

type runRequest struct{ Key string }

func (r *runRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = runRequest{}
	if raw := fields["key"]; raw != nil {
		return json.Unmarshal(raw, &r.Key)
	}
	return nil
}
