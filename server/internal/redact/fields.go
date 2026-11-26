package redact

import (
	"bytes"
	"encoding/json"

	"configflow/internal/jsonresp"
)

// objectFields is the migration read boundary. Legacy objects participate through
// their accessor interface; the visitor itself has no jsonx dependency. Native
// ordered fields and maps use the same visitor, including cycles and depth.
type objectFields interface {
	Keys() []string
	Get(string) (any, bool)
}

type publicField struct {
	key   string
	value any
}
type orderedFields struct {
	members []publicField
	index   map[string]int
}

func (o *orderedFields) Keys() []string {
	keys := make([]string, len(o.members))
	for i, f := range o.members {
		keys[i] = f.key
	}
	return keys
}
func (o *orderedFields) Get(key string) (any, bool) {
	if i, ok := o.index[key]; ok {
		return o.members[i].value, true
	}
	return nil, false
}
func (o *orderedFields) Set(key string, value any) {
	if o.index == nil {
		o.index = make(map[string]int)
	}
	if i, ok := o.index[key]; ok {
		o.members[i].value = value
		return
	}
	o.index[key] = len(o.members)
	o.members = append(o.members, publicField{key, value})
}

// MarshalJSON retains the original traversal/collision order between redaction
// passes. The single HTTP exit sorts final object members as usual.
func (o *orderedFields) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	for i, f := range o.members {
		key, err := jsonresp.Encode(f.key)
		if err != nil {
			return nil, err
		}
		value, err := jsonresp.Encode(f.value)
		if err != nil {
			return nil, err
		}
		if i > 0 {
			out.WriteByte(',')
		}
		out.Write(key)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// decodePublic decodes a whole valid JSON value in one pass: objects become
// orderedFields (a repeated key keeps its first position and last value),
// arrays []any, numbers json.Number. Values nested deeper than the visitor's
// depth limit stay json.RawMessage; the visitor redacts them without decoding.
// Invalid JSON returns the standard decoding error.
func decodePublic(data []byte) (any, error) {
	data = bytes.TrimSpace(data)
	if !json.Valid(data) {
		var raw any
		return nil, json.Unmarshal(data, &raw)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return decodeTree(d, 0)
}

// eagerDepth bounds the eager decode; deeper values are decoded on visit.
const eagerDepth = maxDepth + 2

func decodeTree(d *json.Decoder, depth int) (any, error) {
	if depth > eagerDepth {
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		fields := &orderedFields{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			value, err := decodeTree(d, depth+1)
			if err != nil {
				return nil, err
			}
			fields.Set(key.(string), value)
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return fields, nil
	case json.Delim('['):
		out := []any{}
		for d.More() {
			value, err := decodeTree(d, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return out, nil
	}
	return token, nil
}

func encodedPublic(value any) json.RawMessage {
	data, err := jsonresp.Encode(value)
	if err != nil {
		return json.RawMessage(`"[REDACTED]"`)
	}
	return data
}

func fieldValue(value any, key string) any {
	switch v := value.(type) {
	case objectFields:
		item, _ := v.Get(key)
		if raw, ok := item.(json.RawMessage); ok {
			decoded, _ := decodePublic(raw)
			return decoded
		}
		return item
	case map[string]any:
		return v[key]
	case json.RawMessage:
		// One member is read: decode the top level only.
		data := bytes.TrimSpace(v)
		if len(data) == 0 || data[0] != '{' || !json.Valid(data) {
			return nil
		}
		parsed := &orderedFields{}
		d := json.NewDecoder(bytes.NewReader(data))
		_, _ = d.Token()
		for d.More() {
			name, err := d.Token()
			if err != nil {
				return nil
			}
			var raw json.RawMessage
			if err := d.Decode(&raw); err != nil {
				return nil
			}
			parsed.Set(name.(string), raw)
		}
		return fieldValue(parsed, key)
	}
	return nil
}
