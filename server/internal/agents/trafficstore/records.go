package trafficstore

import (
	"bytes"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"encoding/json"
)

// records is the mutable, ordered storage index for hosts, routes and ports.
// Reports update many entries before one serialization. model.Fields remains
// appropriate for small extension-bearing records, but copying its whole state
// on every host update makes a 2,000-item report quadratic in retained hosts.
type records struct {
	order  []string
	values map[string]json.RawMessage
}

func readRecords(raw json.RawMessage) (records, error) {
	var fields model.Fields
	if len(raw) == 0 || raw[0] != '{' || jsondecode.Unmarshal(raw, &fields) != nil {
		return records{}, &TypeError{Message: "invalid traffic collection"}
	}
	r := records{values: make(map[string]json.RawMessage, fields.Len()), order: fields.Keys()}
	for key, raw := range fields.All() {
		r.values[key] = raw
	}
	return r, nil
}
func (r records) get(key string) (json.RawMessage, bool) { raw, ok := r.values[key]; return raw, ok }
func (r records) has(key string) bool                    { _, ok := r.values[key]; return ok }
func (r records) len() int                               { return len(r.values) }
func (r records) keys() []string {
	keys := make([]string, 0, len(r.values))
	for _, key := range r.order {
		if _, ok := r.values[key]; ok {
			keys = append(keys, key)
		}
	}
	return keys
}

// setEncoded accepts an owned value produced by json.Marshal or storedCounter.raw.
// It is deliberately confined to this storage codec, after validation or encoding.
func (r *records) setEncoded(key string, raw json.RawMessage) {
	if r.values == nil {
		r.values = map[string]json.RawMessage{}
	}
	if _, ok := r.values[key]; !ok {
		r.order = append(r.order, key)
	}
	r.values[key] = raw
}
func (r *records) delete(key string) { delete(r.values, key) }

// MarshalJSON writes storage members in their original or insertion order.
func (r records) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	first := true
	for _, key := range r.order {
		raw, exists := r.values[key]
		if !exists {
			continue
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(raw)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
func recordMember(r records, key string) json.RawMessage { raw, _ := r.get(key); return raw }
func recordPut(r *records, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	r.setEncoded(key, raw)
	return nil
}

type hostMerge struct {
	entry         records
	ports, routes records
}

func openHost(hosts records, name, stamp string) (*hostMerge, error) {
	raw, exists := hosts.get(name)
	if !exists {
		raw = []byte(`{}`)
	}
	entry, err := readRecords(raw)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"first_seen", "last_seen"} {
		if raw, ok := entry.get(key); ok && string(raw) != "null" {
			var value string
			if jsondecode.Unmarshal(raw, &value) != nil {
				return nil, &TypeError{Message: "invalid traffic host timestamp"}
			}
		}
	}
	if !entry.has("first_seen") {
		if err = recordPut(&entry, "first_seen", stamp); err != nil {
			return nil, err
		}
	}
	if err = recordPut(&entry, "last_seen", stamp); err != nil {
		return nil, err
	}
	portsRaw, ok := entry.get("ports")
	if !ok {
		portsRaw = []byte(`{}`)
	}
	ports, err := readRecords(portsRaw)
	if err != nil {
		return nil, err
	}
	routesRaw, ok := entry.get("by_route")
	if !ok {
		if exists {
			return nil, &KeyError{Key: "by_route"}
		}
		routesRaw = []byte(`{}`)
	}
	routes, err := readRecords(routesRaw)
	if err != nil {
		return nil, err
	}
	return &hostMerge{entry, ports, routes}, nil
}
