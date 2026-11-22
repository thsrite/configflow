package convert

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// JObj is a test-only ordered JSON object that preserves key insertion order
// and exact number spellings for corpus replay and SHA256 verification.
type JObj struct {
	keys   []string
	values map[string]any
}

// NewJObj creates an empty ordered test JSON object.
func NewJObj() *JObj {
	return &JObj{values: map[string]any{}}
}

// JObjKV creates an ordered test JSON object from alternating key/value pairs.
func JObjKV(kv ...any) *JObj {
	o := NewJObj()
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

func (o *JObj) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.values[key]
	return v, ok
}

func (o *JObj) Value(key string) any {
	if o == nil {
		return nil
	}
	return o.values[key]
}

func (o *JObj) Has(key string) bool {
	if o == nil {
		return false
	}
	_, ok := o.values[key]
	return ok
}

func (o *JObj) Set(key string, value any) {
	if o.values == nil {
		o.values = map[string]any{}
	}
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *JObj) Delete(key string) {
	if o == nil {
		return
	}
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

func (o *JObj) Update(other *JObj) {
	if other == nil {
		return
	}
	for _, k := range other.keys {
		o.Set(k, other.values[k])
	}
}

func (o *JObj) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

func (o *JObj) Keys() []string {
	if o == nil {
		return nil
	}
	return append([]string(nil), o.keys...)
}

func (o *JObj) Values() []any {
	if o == nil {
		return nil
	}
	out := make([]any, len(o.keys))
	for i, k := range o.keys {
		out[i] = o.values[k]
	}
	return out
}

func (o *JObj) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		if o == nil {
			return
		}
		for _, k := range o.keys {
			if !yield(k, o.values[k]) {
				return
			}
		}
	}
}

func (o *JObj) GetString(key string) string {
	s, _ := o.Value(key).(string)
	return s
}

func (o *JObj) GetBool(key string) bool {
	b, _ := o.Value(key).(bool)
	return b
}

func (o *JObj) GetInt(key string) int64 {
	switch v := o.Value(key).(type) {
	case json.Number:
		n, _ := v.Int64()
		return n
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

func (o *JObj) GetObject(key string) *JObj {
	child, _ := o.Value(key).(*JObj)
	return child
}

func (o *JObj) GetList(key string) []any {
	list, _ := o.Value(key).([]any)
	return list
}

func (o *JObj) Clone() *JObj {
	if o == nil {
		return nil
	}
	out := &JObj{keys: append([]string(nil), o.keys...), values: make(map[string]any, len(o.keys))}
	for _, k := range o.keys {
		out.values[k] = o.values[k]
	}
	return out
}

func (o *JObj) DeepCopy() *JObj {
	if o == nil {
		return nil
	}
	out := &JObj{keys: append([]string(nil), o.keys...), values: make(map[string]any, len(o.keys))}
	for _, k := range o.keys {
		out.values[k] = DeepCopyJ(o.values[k])
	}
	return out
}

// DeepCopyJ recursively clones a test JSON value tree.
func DeepCopyJ(v any) any {
	switch x := v.(type) {
	case *JObj:
		return x.DeepCopy()
	case []any:
		if x == nil {
			return nil
		}
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = DeepCopyJ(item)
		}
		return out
	default:
		return v
	}
}

func (o *JObj) MarshalJSON() ([]byte, error) {
	return CompactJ(o)
}

const (
	jTokNaN    = "\x00__CF_NAN__\x00"
	jTokInf    = "\x00__CF_INF__\x00"
	jTokNegInf = "\x00__CF_NEGINF__\x00"
)

// sanitizeNonFiniteTokens replaces bare Python NaN / Infinity / -Infinity
// tokens outside JSON strings with sentinel strings so encoding/json can parse
// frozen Python corpora.
func sanitizeNonFiniteTokens(src []byte) []byte {
	if !bytes.Contains(src, []byte("NaN")) && !bytes.Contains(src, []byte("Infinity")) {
		return src
	}
	var out bytes.Buffer
	out.Grow(len(src) + 32)
	inStr := false
	escaped := false
	for i := 0; i < len(src); {
		c := src[i]
		if inStr {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		if c == '"' {
			inStr = true
			out.WriteByte(c)
			i++
			continue
		}
		if bytes.HasPrefix(src[i:], []byte("-Infinity")) && (i+9 == len(src) || isJSONDelim(src[i+9])) {
			out.WriteString(`"\u0000__CF_NEGINF__\u0000"`)
			i += 9
			continue
		}
		if bytes.HasPrefix(src[i:], []byte("Infinity")) && (i+8 == len(src) || isJSONDelim(src[i+8])) {
			out.WriteString(`"\u0000__CF_INF__\u0000"`)
			i += 8
			continue
		}
		if bytes.HasPrefix(src[i:], []byte("NaN")) && (i+3 == len(src) || isJSONDelim(src[i+3])) {
			out.WriteString(`"\u0000__CF_NAN__\u0000"`)
			i += 3
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.Bytes()
}

func isJSONDelim(b byte) bool {
	return b == ',' || b == ']' || b == '}' || b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

// ParseJ parses JSON bytes into ordered *JObj / []any / json.Number / string / bool / nil.
func ParseJ(data []byte) (any, error) {
	cleaned := sanitizeNonFiniteTokens(data)
	d := json.NewDecoder(bytes.NewReader(cleaned))
	d.UseNumber()
	v, err := readJValue(d)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON value")
		}
		return nil, err
	}
	return v, nil
}

// ParseJString parses a JSON string into test JSON values.
func ParseJString(text string) (any, error) {
	return ParseJ([]byte(text))
}

func readJValue(d *json.Decoder) (any, error) {
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := NewJObj()
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("non-string key")
				}
				val, err := readJValue(d)
				if err != nil {
					return nil, err
				}
				o.Set(key, val)
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return o, nil
		case '[':
			list := []any{}
			for d.More() {
				val, err := readJValue(d)
				if err != nil {
					return nil, err
				}
				list = append(list, val)
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return list, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", t)
		}
	case string:
		switch t {
		case jTokNaN:
			return json.Number("NaN"), nil
		case jTokInf:
			return json.Number("Infinity"), nil
		case jTokNegInf:
			return json.Number("-Infinity"), nil
		}
		return t, nil
	default:
		return tok, nil
	}
}

// IsIntNumber reports whether a json.Number has an integer literal spelling.
func IsIntNumber(n json.Number) bool {
	s := string(n)
	if s == "" || s == "NaN" || s == "Infinity" || s == "-Infinity" {
		return false
	}
	return !strings.ContainsAny(s, ".eE")
}

// JInt returns a json.Number for an int64.
func JInt(v int64) json.Number {
	return json.Number(strconv.FormatInt(v, 10))
}

// JFloat formats a float64 like Python/JSON floats (appending ".0" when integral).
func JFloat(v float64) json.Number {
	switch {
	case math.IsNaN(v):
		return json.Number("NaN")
	case math.IsInf(v, 1):
		return json.Number("Infinity")
	case math.IsInf(v, -1):
		return json.Number("-Infinity")
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return json.Number(s)
}

// JStrings converts []any of strings to []string.
func JStrings(list []any) []string {
	out := make([]string, len(list))
	for i, item := range list {
		out[i], _ = item.(string)
	}
	return out
}

// JListOf converts a []T to []any.
func JListOf[T any](items []T) []any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}

// CompactJ encodes a test JSON value compactly in key-insertion order without HTML escaping.
func CompactJ(v any) ([]byte, error) {
	return EncodeJ(v, false, false)
}

// EncodeJ encodes a test JSON value with optional key sorting and ASCII escaping.
func EncodeJ(v any, sortKeys, ensureASCII bool) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJ(&buf, v, sortKeys, ensureASCII); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeJString(buf *bytes.Buffer, s string, ensureASCII bool) {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	raw := bytes.TrimSuffix(tmp.Bytes(), []byte("\n"))
	if !ensureASCII {
		buf.Write(raw)
		return
	}
	for _, r := range string(raw) {
		if r < 0x80 {
			buf.WriteRune(r)
		} else if r <= 0xFFFF {
			fmt.Fprintf(buf, "\\u%04x", r)
		} else {
			r1, r2 := encodeSurrogate(r)
			fmt.Fprintf(buf, "\\u%04x\\u%04x", r1, r2)
		}
	}
}

func encodeSurrogate(r rune) (rune, rune) {
	r -= 0x10000
	return 0xD800 + (r >> 10), 0xDC00 + (r & 0x3FF)
}

func writeJ(buf *bytes.Buffer, v any, sortKeys, ensureASCII bool) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		writeJString(buf, x, ensureASCII)
	case json.Number:
		buf.WriteString(string(x))
	case int:
		buf.WriteString(strconv.Itoa(x))
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
	case float64:
		buf.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case []any:
		if x == nil {
			buf.WriteString("null")
			return nil
		}
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJ(buf, item, sortKeys, ensureASCII); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case *JObj:
		if x == nil {
			buf.WriteString("null")
			return nil
		}
		keys := x.keys
		if sortKeys {
			keys = append([]string(nil), x.keys...)
			sort.Strings(keys)
		}
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJString(buf, k, ensureASCII)
			buf.WriteByte(':')
			if err := writeJ(buf, x.values[k], sortKeys, ensureASCII); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		var tmp bytes.Buffer
		enc := json.NewEncoder(&tmp)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return err
		}
		raw := bytes.TrimSuffix(tmp.Bytes(), []byte("\n"))
		parsed, err := ParseJ(raw)
		if err != nil {
			buf.Write(raw)
			return nil
		}
		return writeJ(buf, parsed, sortKeys, ensureASCII)
	}
	return nil
}

// EqualJ compares two test JSON values structurally (object key order ignored,
// numbers compared numerically with int/float equivalence, bool != number).
func EqualJ(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	switch x := a.(type) {
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case json.Number:
		return equalJNumber(x, b)
	case int:
		return equalJNumber(JInt(int64(x)), b)
	case int64:
		return equalJNumber(JInt(x), b)
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !EqualJ(x[i], y[i]) {
				return false
			}
		}
		return true
	case *JObj:
		y, ok := b.(*JObj)
		if !ok || x == nil || y == nil || x.Len() != y.Len() {
			return false
		}
		for _, k := range x.keys {
			yv, exists := y.values[k]
			if !exists || !EqualJ(x.values[k], yv) {
				return false
			}
		}
		return true
	default:
		ab, err1 := CompactJ(a)
		bb, err2 := CompactJ(b)
		if err1 != nil || err2 != nil {
			return false
		}
		ap, err1 := ParseJ(ab)
		bp, err2 := ParseJ(bb)
		if err1 != nil || err2 != nil {
			return false
		}
		return EqualJ(ap, bp)
	}
}

func equalJNumber(a json.Number, b any) bool {
	var bn json.Number
	switch y := b.(type) {
	case json.Number:
		bn = y
	case int:
		bn = JInt(int64(y))
	case int64:
		bn = JInt(y)
	default:
		return false
	}
	if a == bn {
		return true
	}
	ra, ok1 := new(big.Rat).SetString(string(a))
	rb, ok2 := new(big.Rat).SetString(string(bn))
	if !ok1 || !ok2 {
		return false
	}
	return ra.Cmp(rb) == 0
}

// DecodeJ encodes a test JSON value and unmarshals it into T using jsondecode.
func DecodeJ[T any](value any) (T, error) {
	var result T
	data, err := CompactJ(value)
	if err != nil {
		return result, err
	}
	if bytes.Contains(data, []byte("NaN")) || bytes.Contains(data, []byte("Infinity")) {
		if !json.Valid(data) {
			return result, fmt.Errorf("nonfinite JSON value")
		}
	}
	err = jsondecode.Unmarshal(data, &result)
	return result, err
}

func selectJMembers(value *JObj, names ...string) *JObj {
	selected := NewJObj()
	for _, name := range names {
		if member, present := value.Get(name); present {
			selected.Set(name, member)
		}
	}
	return selected
}

// DecodeConfigJ decodes a resolved test config *JObj into *model.Config
// matching the 3-section ownership of Profile / Shared / System.
func DecodeConfigJ(value *JObj) (*model.Config, error) {
	if value == nil {
		value = NewJObj()
	}
	profileMembers := value.Clone()
	for _, key := range []string{"profile_id", "system_config", "backup", "agents", "nodes", "subscriptions", "subscription_aggregations", "rule_library"} {
		profileMembers.Delete(key)
	}
	profile, err := DecodeJ[model.Profile](profileMembers)
	if err != nil {
		return nil, err
	}
	shared, err := DecodeJ[model.Shared](selectJMembers(value, "nodes", "subscriptions", "subscription_aggregations", "rule_library"))
	if err != nil {
		return nil, err
	}
	wire, err := DecodeJ[model.System](selectJMembers(value, "system_config", "backup", "agents"))
	if err != nil {
		return nil, err
	}
	return &model.Config{
		Profile:       &profile,
		ProfileID:     value.GetString("profile_id"),
		SystemConfig:  wire.SystemConfig,
		Backup:        wire.Backup,
		Agents:        wire.Agents,
		HasAgents:     value.Has("agents"),
		Nodes:         shared.Nodes,
		Subscriptions: shared.Subscriptions,
		Aggregations:  shared.Aggregations,
		RuleLibrary:   shared.RuleLibrary,
	}, nil
}

// DecodeRulesConfigJ decodes only rule_configs from a test config object.
func DecodeRulesConfigJ(value *JObj) (*model.Config, error) {
	profile, err := DecodeJ[model.Profile](selectJMembers(value, "rule_configs"))
	return &model.Config{Profile: &profile}, err
}

// DecodeRuleLibraryConfigJ decodes only rule_library from a test config object.
func DecodeRuleLibraryConfigJ(value *JObj) (*model.Config, error) {
	shared, err := DecodeJ[model.Shared](selectJMembers(value, "rule_library"))
	return &model.Config{RuleLibrary: shared.RuleLibrary}, err
}

// DecodeDialerConfigJ decodes the dialer-owned fields from a test config object.
func DecodeDialerConfigJ(value *JObj) (*model.Config, error) {
	profile, err := DecodeJ[model.Profile](selectJMembers(value, "proxy_groups"))
	if err != nil {
		return nil, err
	}
	shared, err := DecodeJ[model.Shared](selectJMembers(value, "nodes", "subscriptions", "subscription_aggregations"))
	if err != nil {
		return nil, err
	}
	return &model.Config{
		Profile:       &profile,
		Nodes:         shared.Nodes,
		Subscriptions: shared.Subscriptions,
		Aggregations:  shared.Aggregations,
	}, nil
}

// ValidateResourceArraysJ enforces the strict array/object shape expected by
// dialer and client corpus rows for nodes and proxy_groups.
func ValidateResourceArraysJ(value *JObj) error {
	if value == nil {
		return nil
	}
	for _, key := range []string{"nodes", "proxy_groups"} {
		if raw, present := value.Get(key); present {
			items, err := DecodeJ[[]json.RawMessage](raw)
			if err != nil || items == nil {
				return &DialerReferenceError{Message: fmt.Sprintf(msgResourceArray, key), cause: err}
			}
			for _, item := range items {
				var object map[string]json.RawMessage
				if err := json.Unmarshal(item, &object); err != nil || object == nil {
					return &DialerReferenceError{Message: msgResourceObject, cause: err}
				}
			}
		}
	}
	return nil
}

// YAMLNodeFromJ converts a test JSON value tree into an owned *yamlx.Node.
func YAMLNodeFromJ(v any) (*yamlx.Node, error) {
	switch x := v.(type) {
	case nil:
		return yamlx.Null(), nil
	case bool:
		return yamlx.Bool(x), nil
	case string:
		return yamlx.Str(x), nil
	case int:
		return yamlx.Int(int64(x)), nil
	case int64:
		return yamlx.Int(x), nil
	case json.Number:
		s := string(x)
		switch s {
		case "NaN":
			return &yamlx.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".nan"}, nil
		case "Infinity":
			return &yamlx.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".inf"}, nil
		case "-Infinity":
			return &yamlx.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: "-.inf"}, nil
		}
		if IsIntNumber(x) {
			return &yamlx.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: s}, nil
		}
		return &yamlx.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: s}, nil
	case []any:
		if x == nil {
			return yamlx.Null(), nil
		}
		seq := yamlx.Seq()
		for _, item := range x {
			child, err := YAMLNodeFromJ(item)
			if err != nil {
				return nil, err
			}
			seq.Content = append(seq.Content, child)
		}
		return seq, nil
	case *JObj:
		if x == nil {
			return yamlx.Null(), nil
		}
		m := yamlx.Map()
		for _, k := range x.keys {
			child, err := YAMLNodeFromJ(x.values[k])
			if err != nil {
				return nil, err
			}
			m.Content = append(m.Content, yamlx.Str(k), child)
		}
		return m, nil
	default:
		raw, err := CompactJ(v)
		if err != nil {
			return nil, err
		}
		return yamlx.FromJSON(raw)
	}
}

// JFromYAMLNode converts a *yamlx.Node into test JSON values (*JObj / []any / json.Number / etc.).
func JFromYAMLNode(node *yamlx.Node) (any, error) {
	if node == nil {
		return nil, nil
	}
	node = yamlx.Resolve(node)
	if node == nil {
		return nil, nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return JFromYAMLNode(node.Content[0])
	case yaml.MappingNode:
		out := NewJObj()
		for i := 0; i+1 < len(node.Content); i += 2 {
			kNode := yamlx.Resolve(node.Content[i])
			key := ""
			if kNode != nil {
				key = kNode.Value
			}
			val, err := JFromYAMLNode(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			out.Set(key, val)
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, len(node.Content))
		for i, item := range node.Content {
			val, err := JFromYAMLNode(item)
			if err != nil {
				return nil, err
			}
			out[i] = val
		}
		return out, nil
	case yaml.ScalarNode:
		switch node.ShortTag() {
		case "!!null":
			return nil, nil
		case "!!bool":
			return node.Value == "true" || node.Value == "True" || node.Value == "TRUE", nil
		case "!!str":
			return node.Value, nil
		case "!!int":
			if bi, ok := new(big.Int).SetString(strings.ReplaceAll(node.Value, "_", ""), 0); ok {
				return json.Number(bi.String()), nil
			}
			raw, err := yamlx.ToJSON(node)
			if err == nil {
				return json.Number(string(raw)), nil
			}
			return json.Number(node.Value), nil
		case "!!float":
			lower := strings.ToLower(strings.ReplaceAll(node.Value, "_", ""))
			switch lower {
			case ".nan":
				return json.Number("NaN"), nil
			case ".inf", "+.inf":
				return json.Number("Infinity"), nil
			case "-.inf":
				return json.Number("-Infinity"), nil
			}
			f, err := strconv.ParseFloat(lower, 64)
			if err == nil {
				return JFloat(f), nil
			}
			if errors.Is(err, strconv.ErrRange) {
				if math.IsInf(f, 1) {
					return json.Number("Infinity"), nil
				}
				if math.IsInf(f, -1) {
					return json.Number("-Infinity"), nil
				}
			}
			raw, err := yamlx.ToJSON(node)
			if err == nil {
				return json.Number(string(raw)), nil
			}
			return json.Number(node.Value), nil
		default:
			return node.Value, nil
		}
	default:
		return nil, nil
	}
}

// Test-only exports of unexported typed helpers for package convert_test corpus replays.

// SyncMihomoHostsForTest exposes syncMihomoHosts to package convert_test.
func SyncMihomoHostsForTest(document *yamlx.Node, config *model.Config) error {
	return syncMihomoHosts(document, config)
}

// AddRegionProbeForTest exposes addRegionProbe to package convert_test.
func AddRegionProbeForTest(document *yamlx.Node, port int64) (bool, error) {
	return addRegionProbe(document, port)
}

// ProviderHealthNodeForTest exposes providerHealthNode to package convert_test.
func ProviderHealthNodeForTest(check *model.ProviderHealthCheck, address *string, env MihomoEnvironment) (*yamlx.Node, error) {
	return providerHealthNode(check, address, env)
}

// ClientSanitizeNameForTest exposes clientSanitizeName to package convert_test.
func ClientSanitizeNameForTest(text string) string {
	return clientSanitizeName(text)
}

// BuildProviderHealthForTest builds a provider health check node from a model.Subscription.
func BuildProviderHealthForTest(record model.Subscription) (*yamlx.Node, error) {
	env := CurrentMihomoEnvironment()
	if env.ProviderHealth == nil {
		env = DefaultMihomoEnvironment()
	}
	return providerHealthNode(record.HealthCheck, record.HealthCheckURL, env)
}
