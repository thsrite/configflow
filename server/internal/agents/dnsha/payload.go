package dnsha

import "encoding/json"

var payloadFields = []string{"id", "source_snapshot", "resource_config", "config_revision", "base_url", "baseline_deployment_id", "bundle"}

// UnmarshalJSON retains fields from a newer publisher while reading the owned
// envelope strictly. Bad owned metadata must stop preparation before a core write.
func (p *payload) UnmarshalJSON(data []byte) error {
	type plain payload
	var next plain
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range payloadFields {
		delete(fields, key)
	}
	next.extra = fields
	*p = payload(next)
	return nil
}

// MarshalJSON keeps unknown envelope members when materialization adds the
// immutable bundle to a Python-written in-flight payload.
func (p payload) MarshalJSON() ([]byte, error) {
	type plain payload
	data, err := json.Marshal(plain(p))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range p.extra {
		if _, owned := fields[key]; !owned {
			fields[key] = value
		}
	}
	return json.Marshal(fields)
}
