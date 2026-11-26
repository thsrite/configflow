package dnsha

import (
	"configflow/internal/model"
	"encoding/json"
)

// UnmarshalJSON retains fields from a newer publisher while reading the owned
// envelope strictly. Bad owned metadata must stop preparation before a core write.
func (p *payload) UnmarshalJSON(data []byte) error {
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var next payload
	for _, item := range []struct {
		name   string
		target any
	}{
		{"id", &next.ID}, {"source_snapshot", &next.Source}, {"resource_config", &next.Resource},
		{"config_revision", &next.Revision}, {"base_url", &next.BaseURL}, {"baseline_deployment_id", &next.BaselineID}, {"bundle", &next.Bundle},
	} {
		raw, present := fields.Get(item.name)
		if !present {
			continue
		}
		if err := json.Unmarshal(raw, item.target); err != nil {
			return err
		}
		fields.Delete(item.name)
	}
	next.extra = map[string]json.RawMessage{}
	for key, raw := range fields.All() {
		next.extra[key] = raw
	}
	*p = next
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

func (b *bundleMeta) UnmarshalJSON(data []byte) error {
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var next bundleMeta
	for _, item := range []struct {
		name   string
		target any
	}{
		{"manifest", &next.Manifest}, {"sha256", &next.SHA256}, {"config_version", &next.ConfigVersion}, {"comparison_config", &next.Comparison},
	} {
		if raw, present := fields.Get(item.name); present {
			if err := json.Unmarshal(raw, item.target); err != nil {
				return err
			}
		}
	}
	*b = next
	return nil
}
