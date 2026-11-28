package trafficstore

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"io"
	"maps"
	"slices"
	"strings"
)

// records is the mutable, ordered storage index for traffic document members.
// Reports update many entries before one serialization. model.Fields remains
// appropriate for small extension-bearing records, but copying its whole state
// on every host update makes a 2,000-item report quadratic in retained hosts.
type records struct {
	order  []string
	values map[string]json.RawMessage
}

// recordReader reuses one decoder while indexing members of a document
// validated at the file boundary. Returned spans point into the source bytes.
type recordReader struct {
	buffer  bytes.Buffer
	decoder jsontext.Decoder
}

func (reader *recordReader) read(raw json.RawMessage) (records, error) {
	return reader.index(raw, jsontext.AllowDuplicateNames(true))
}

func (reader *recordReader) index(raw json.RawMessage, opts ...jsontext.Options) (records, error) {
	if len(raw) == 0 || raw[0] != '{' {
		return records{}, invalidData("invalid traffic collection")
	}
	reader.buffer = *bytes.NewBuffer(raw)
	d := &reader.decoder
	d.Reset(&reader.buffer, opts...)
	if _, err := d.ReadToken(); err != nil {
		return records{}, invalidData("invalid traffic collection")
	}
	var r records
	for d.PeekKind() != '}' {
		key, err := d.ReadToken()
		if err != nil {
			return records{}, invalidData("invalid traffic collection")
		}
		name := key.String()
		value, err := d.ReadValue()
		if err != nil {
			return records{}, invalidData("invalid traffic collection")
		}
		end := int(d.InputOffset())
		r.setEncoded(name, raw[end-len(value):end])
	}
	if _, err := d.ReadToken(); err != nil {
		return records{}, invalidData("invalid traffic collection")
	}
	if _, err := d.ReadToken(); err != io.EOF {
		return records{}, invalidData("invalid traffic collection")
	}
	return r, nil
}

// clone owns the mutable index and borrows immutable encoded values.
func (r records) clone() records                         { return records{slices.Clone(r.order), maps.Clone(r.values)} }
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

// setEncoded accepts an immutable encoded value from the document or the merge.
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
func (r records) MarshalJSON() ([]byte, error) { return r.encode(), nil }

func (r records) encode() []byte { return r.encodeObject("", nil) }

func (r records) size() int {
	size := 2
	for key, raw := range r.values {
		size += len(key) + len(raw) + 4
	}
	return size
}

// encodeObject inserts an indexed child directly into its parent buffer. Both
// the top-level days and the day's hosts can be large.
func (r records) encodeObject(name string, child *records) []byte {
	size := r.size()
	if child != nil {
		size += child.size() - len(r.values[name])
	}
	return r.appendObject(make([]byte, 0, size), name, child)
}
func (r records) appendObject(out []byte, name string, child *records) []byte {
	out = append(out, '{')
	first := true
	for _, key := range r.order {
		raw, ok := r.values[key]
		if !ok {
			continue
		}
		if !first {
			out = append(out, ',')
		}
		first = false
		if strings.ContainsAny(key, "<>&\u2028\u2029") {
			quoted, _ := json.Marshal(key)
			out = append(out, quoted...)
		} else {
			out, _ = jsontext.AppendQuote(out, key)
		}
		out = append(out, ':')
		if child != nil && key == name {
			out = child.appendObject(out, "", nil)
		} else {
			out = append(out, raw...)
		}
	}
	return append(out, '}')
}

func (r *records) compactOrder() {
	if len(r.order) != len(r.values) {
		r.order = slices.DeleteFunc(r.order, func(k string) bool { return !r.has(k) })
	}
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

func (reader *recordReader) openHost(hosts records, name string, stamp json.RawMessage) (*hostMerge, error) {
	raw, exists := hosts.get(name)
	if !exists {
		raw = []byte(`{}`)
	}
	entry, err := reader.read(raw)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"first_seen", "last_seen"} {
		if raw, ok := entry.get(key); ok && string(raw) != "null" && raw[0] != '"' {
			return nil, invalidData("invalid traffic host timestamp")
		}
	}
	if !entry.has("first_seen") {
		entry.setEncoded("first_seen", stamp)
	}
	entry.setEncoded("last_seen", stamp)
	portsRaw, ok := entry.get("ports")
	if !ok {
		portsRaw = []byte(`{}`)
	}
	ports, err := reader.read(portsRaw)
	if err != nil {
		return nil, err
	}
	routesRaw, ok := entry.get("by_route")
	if !ok {
		if exists {
			return nil, missingMember("by_route")
		}
		routesRaw = []byte(`{}`)
	}
	routes, err := reader.read(routesRaw)
	if err != nil {
		return nil, err
	}
	return &hostMerge{entry, ports, routes}, nil
}
