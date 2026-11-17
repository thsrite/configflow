package yamlx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FromJSON reads exactly one JSON value using encoding/json. Object order and
// number text are retained; duplicate names keep the last value at the first
// name's position. JSON escapes, including surrogate pairs, follow encoding/json.
func FromJSON(data []byte) (*Node, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	n, err := readJSONNode(d)
	if err != nil {
		return nil, fmt.Errorf("read JSON document: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("yamlx: trailing JSON data")
		}
		return nil, fmt.Errorf("read JSON document end: %w", err)
	}
	return n, nil
}

func readJSONNode(d *json.Decoder) (*Node, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch v := token.(type) {
	case json.Delim:
		switch v {
		case '{':
			n := Map()
			index := map[string]int{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("yamlx: JSON object key is not a string")
				}
				value, err := readJSONNode(d)
				if err != nil {
					return nil, err
				}
				if i, exists := index[name]; exists {
					n.Content[i] = value
				} else {
					index[name] = len(n.Content) + 1
					n.Content = append(n.Content, Str(name), value)
				}
			}
			_, err := d.Token()
			return n, err
		case '[':
			n := Seq()
			for d.More() {
				value, err := readJSONNode(d)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, value)
			}
			_, err := d.Token()
			return n, err
		}
	case string:
		return Str(v), nil
	case bool:
		return Bool(v), nil
	case nil:
		return Null(), nil
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(v.String(), ".eE") {
			tag = "!!float"
		}
		return &Node{Kind: yaml.ScalarNode, Tag: tag, Value: v.String()}, nil
	}
	return nil, fmt.Errorf("yamlx: unexpected JSON token %v", token)
}

// ToJSON writes compact JSON in effective mapping order without HTML escaping.
// Timestamp, binary and other non-JSON scalar tags become their scalar text.
// Non-finite YAML numbers likewise become their original scalar text. Non-string
// object keys return errors rather than silently losing keys. Aliases and merges
// are followed.
func ToJSON(n *Node) ([]byte, error) {
	if n == nil {
		n = Null()
	}
	if err := validateNode(n); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := writeJSONNode(&b, n); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeJSONNode(b *bytes.Buffer, n *Node) error {
	n = Resolve(n)
	if n.Kind == yaml.DocumentNode {
		return writeJSONNode(b, n.Content[0])
	}
	switch n.Kind {
	case 0:
		b.WriteString("null")
	case yaml.MappingNode:
		b.WriteByte('{')
		for i, p := range effectivePairs(n, map[*Node]bool{}) {
			key, ok := stringKey(p.key)
			if !ok {
				return invalidNode(p.key, "JSON object key must be a string")
			}
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, key)
			b.WriteByte(':')
			if err := writeJSONNode(b, p.value); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case yaml.SequenceNode:
		b.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeJSONNode(b, c); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!null":
			b.WriteString("null")
		case "!!bool":
			var v bool
			if err := n.Decode(&v); err != nil {
				return syntaxError(err)
			}
			b.WriteString(strconv.FormatBool(v))
		case "!!int":
			if v, ok := yamlInteger(n.Value); ok {
				// JSON-origin number spellings, notably -0, are not normalised.
				if json.Valid([]byte(n.Value)) {
					b.WriteString(n.Value)
				} else {
					b.WriteString(v.String())
				}
			} else {
				return invalidNode(n, "invalid integer scalar")
			}
		case "!!float":
			if len(n.Value) > 0 && (n.Value[0] == '-' || n.Value[0] >= '0' && n.Value[0] <= '9') && json.Valid([]byte(n.Value)) {
				b.WriteString(n.Value)
				break
			}
			var v float64
			if err := n.Decode(&v); err != nil {
				return syntaxError(err)
			}
			if math.IsNaN(v) || math.IsInf(v, 0) {
				writeJSONString(b, n.Value)
				break
			}
			b.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
		default:
			writeJSONString(b, n.Value)
		}
	}
	return nil
}

func writeJSONString(b *bytes.Buffer, s string) {
	e := json.NewEncoder(b)
	e.SetEscapeHTML(false)
	// encoding/json cannot fail when writing a string to bytes.Buffer.
	_ = e.Encode(s)
	b.Truncate(b.Len() - 1)
}
