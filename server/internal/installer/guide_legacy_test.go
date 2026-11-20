package installer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// Enrollment is the compatibility response for callers not yet using Capability.
type Enrollment struct {
	URL       string
	Token     string
	ExpiresAt any
}

func nativeCapability(e *Enrollment) *Capability {
	if e == nil {
		return nil
	}
	return &Capability{URL: e.URL, Token: e.Token}
}

func parseJSONAny(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func parseJSONMap(data []byte) (map[string]any, error) {
	v, err := parseJSONAny(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", v)
	}
	return m, nil
}

func appendJSONString(out []byte, text string, ensureASCII bool) []byte {
	const hexDigits = "0123456789abcdef"
	appendU := func(b []byte, r rune) []byte {
		return append(b, '\\', 'u', hexDigits[r>>12&0xf], hexDigits[r>>8&0xf], hexDigits[r>>4&0xf], hexDigits[r&0xf])
	}
	out = append(out, '"')
	start := 0
	for i := 0; i < len(text); {
		c := text[i]
		if c < utf8.RuneSelf {
			if c >= ' ' && c != '"' && c != '\\' && (c != 0x7f || !ensureASCII) {
				i++
				continue
			}
			out = append(out, text[start:i]...)
			switch c {
			case '"', '\\':
				out = append(out, '\\', c)
			case '\n':
				out = append(out, '\\', 'n')
			case '\r':
				out = append(out, '\\', 'r')
			case '\t':
				out = append(out, '\\', 't')
			case '\b':
				out = append(out, '\\', 'b')
			case '\f':
				out = append(out, '\\', 'f')
			default:
				out = appendU(out, rune(c))
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		invalid := r == utf8.RuneError && size == 1
		if !ensureASCII && !invalid {
			i += size
			continue
		}
		out = append(out, text[start:i]...)
		switch {
		case !ensureASCII:
			out = utf8.AppendRune(out, utf8.RuneError)
		case r >= 0x10000:
			r1, r2 := utf16.EncodeRune(r)
			out = appendU(appendU(out, r1), r2)
		default:
			out = appendU(out, r)
		}
		i += size
		start = i
	}
	out = append(out, text[start:]...)
	return append(out, '"')
}

func writeSortedCompactJSON(buf *bytes.Buffer, v any) error {
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
		buf.Write(appendJSONString(nil, x, false))
	case json.Number:
		buf.WriteString(string(x))
	case int:
		buf.WriteString(strconv.Itoa(x))
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
	case uint64:
		buf.WriteString(strconv.FormatUint(x, 10))
	case float64:
		buf.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeSortedCompactJSON(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(appendJSONString(nil, k, false))
			buf.WriteByte(':')
			if err := writeSortedCompactJSON(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		parsed, err := parseJSONAny(raw)
		if err != nil {
			return err
		}
		return writeSortedCompactJSON(buf, parsed)
	}
	return nil
}

func sortedCompactJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeSortedCompactJSON(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func escapeNonASCIIJSON(data []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(data))
	for _, r := range string(data) {
		if r < 0x7f {
			out.WriteByte(byte(r))
			continue
		}
		if r <= 0xFFFF {
			fmt.Fprintf(&out, "\\u%04x", r)
			continue
		}
		r1, r2 := utf16.EncodeRune(r)
		fmt.Fprintf(&out, "\\u%04x\\u%04x", r1, r2)
	}
	return out.Bytes()
}

func encodeJSONASCII(v any, indent bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	return escapeNonASCIIJSON(out), nil
}

type orderedField struct {
	key   string
	value any
}

type orderedObject struct {
	fields []orderedField
	index  map[string]int
}

func decodeOrderedJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return readOrderedValue(dec)
}

func readOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		o := &orderedObject{index: map[string]int{}}
		for dec.More() {
			kTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			k := kTok.(string)
			val, err := readOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			if idx, exists := o.index[k]; exists {
				o.fields[idx].value = val
			} else {
				o.index[k] = len(o.fields)
				o.fields = append(o.fields, orderedField{key: k, value: val})
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return o, nil
	case '[':
		var list []any
		for dec.More() {
			val, err := readOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			list = append(list, val)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return list, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %v", delim)
	}
}

func writeOrderedJSON(buf *bytes.Buffer, v any, indent, pyDefault, ensureASCII bool, level int) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		buf.WriteString(string(x))
	case string:
		buf.Write(appendJSONString(nil, x, ensureASCII))
	case []any:
		if len(x) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				if indent {
					buf.WriteByte(',')
				} else if pyDefault {
					buf.WriteString(", ")
				} else {
					buf.WriteByte(',')
				}
			}
			if indent {
				buf.WriteByte('\n')
				buf.WriteString(strings.Repeat("  ", level+1))
			}
			if err := writeOrderedJSON(buf, item, indent, pyDefault, ensureASCII, level+1); err != nil {
				return err
			}
		}
		if indent {
			buf.WriteByte('\n')
			buf.WriteString(strings.Repeat("  ", level))
		}
		buf.WriteByte(']')
	case *orderedObject:
		if len(x.fields) == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteByte('{')
		for i, f := range x.fields {
			if i > 0 {
				if indent {
					buf.WriteByte(',')
				} else if pyDefault {
					buf.WriteString(", ")
				} else {
					buf.WriteByte(',')
				}
			}
			if indent {
				buf.WriteByte('\n')
				buf.WriteString(strings.Repeat("  ", level+1))
			}
			if err := writeOrderedJSON(buf, f.key, indent, pyDefault, ensureASCII, level+1); err != nil {
				return err
			}
			if indent || pyDefault {
				buf.WriteString(": ")
			} else {
				buf.WriteByte(':')
			}
			if err := writeOrderedJSON(buf, f.value, indent, pyDefault, ensureASCII, level+1); err != nil {
				return err
			}
		}
		if indent {
			buf.WriteByte('\n')
			buf.WriteString(strings.Repeat("  ", level))
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("unsupported ordered JSON type %T", v)
	}
	return nil
}

func formatOrderedJSON(data []byte, indent, pyDefault, ensureASCII bool) ([]byte, error) {
	v, err := decodeOrderedJSON(data)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := writeOrderedJSON(&buf, v, indent, pyDefault, ensureASCII, 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pyASCIIStringRepr formats an ASCII string (such as ensure_ascii JSON) as a
// Python string literal matching jsonx.Repr.
func pyASCIIStringRepr(text string) string {
	quote := byte('\'')
	if strings.Contains(text, "'") && !strings.Contains(text, `"`) {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == quote || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte(quote)
	return b.String()
}

func cloneJSON(v any) any {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	out, err := parseJSONAny(raw)
	if err != nil {
		return v
	}
	return out
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out, _ := cloneJSON(m).(map[string]any)
	return out
}

func mapHas(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	_, ok := m[key]
	return ok
}

func mapGetOr(m map[string]any, key string, def any) any {
	if m == nil {
		return def
	}
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

func mapStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func mapBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

func asInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case uint32:
		return int64(x), true
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n, true
		}
		if f, err := x.Float64(); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) && math.Trunc(f) == f {
			return int64(f), true
		}
	case float64:
		if !math.IsNaN(x) && !math.IsInf(x, 0) && math.Trunc(x) == x {
			return int64(x), true
		}
	}
	return 0, false
}

func asFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

func mapInt(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	n, _ := asInt64(m[key])
	return n
}

func mapObj(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	o, _ := m[key].(map[string]any)
	return o
}

func mapList(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	l, _ := m[key].([]any)
	return l
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func jsonPath(v any, steps ...any) any {
	cur := v
	for _, step := range steps {
		switch k := step.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil
			}
			cur = m[k]
		case int:
			l, ok := cur.([]any)
			if !ok || k < 0 || k >= len(l) {
				return nil
			}
			cur = l[k]
		default:
			return nil
		}
	}
	return cur
}

func asStrings(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func isTruthyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case json.Number:
		if f, err := strconv.ParseFloat(string(x), 64); err == nil {
			return f != 0
		}
		return string(x) != "" && string(x) != "0"
	case []any:
		return len(x) != 0
	case map[string]any:
		return len(x) != 0
	default:
		return true
	}
}

func legacyResponse(value any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return parseJSONMap(raw)
}

// GenerateGuide is the test facade for GenerateGuideJSON.
func GenerateGuide(value any, enrollment *Enrollment) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	request, err := legacyGuideRequest(raw)
	if err != nil {
		return nil, err
	}
	result, err := renderGuide(request, nativeCapability(enrollment))
	if err != nil {
		return nil, err
	}
	return legacyResponse(result)
}

// legacyGuideRequest keeps artificial numeric IPv4 renderer cases out of the
// protocol decoder.
func legacyGuideRequest(raw json.RawMessage) (GuideRequest, error) {
	var fields model.Fields
	if err := jsondecode.Unmarshal(raw, &fields); err != nil {
		return GuideRequest{}, valueError(msgGuideObject)
	}
	original, hasIP := fields.Get("agent_ip")
	_, textIP := fieldText(fields, "agent_ip")
	address, node, display, _ := decodeIPv4Value(original)
	if hasIP && !textIP {
		if err := setSessionField(&fields, "agent_ip", address); err != nil {
			return GuideRequest{}, err
		}
		normalized, err := json.Marshal(fields)
		if err != nil {
			return GuideRequest{}, err
		}
		request, err := DecodeGuideRequest(normalized)
		if err != nil {
			return request, err
		}
		if err := request.Options.identity.Set("agent_ip", original); err != nil {
			return request, err
		}
		request.Options.addressNode = node
		request.Options.addressDisplay = display
		return request, nil
	}
	return DecodeGuideRequest(raw)
}

// IdentityCommand renders the bounded host-identity creation step.
func IdentityCommand(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", valueError(msgIdentityPathCanonical)
	}
	return identityCommand(text)
}

// InspectionCommand renders read-only Docker inspection.
func InspectionCommand(value any) (string, error) {
	if !isTruthyValue(value) {
		return inspectionCommand("")
	}
	text, ok := value.(string)
	if !ok {
		return "", valueError(msgOriginalContainerName)
	}
	return inspectionCommand(text)
}

func legacyGuideInputs(params, request map[string]any, method string) (GuideOptions, GuideRequest, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return GuideOptions{}, GuideRequest{}, err
	}
	var fields model.Fields
	if err := jsondecode.Unmarshal(raw, &fields); err != nil {
		return GuideOptions{}, GuideRequest{}, err
	}
	p := GuideOptions{identity: fields}
	for _, field := range []struct {
		key    string
		target *string
	}{
		{"server_url", &p.ServerURL}, {"agent_name", &p.AgentName}, {"agent_ip", &p.AgentIP}, {"data_dir", &p.DataDir}, {"network_mode", &p.NetworkMode}, {"ha_parent", &p.HAParent}, {"ha_subnet", &p.HASubnet}, {"ha_gateway", &p.HAGateway}, {"ha_host_id_path", &p.HAHostIDPath},
	} {
		*field.target, _ = fieldText(fields, field.key)
	}
	p.OxidnsPort, _ = fieldNumber(fields, "oxidns_port")
	addressRaw, _ := fields.Get("agent_ip")
	if address, node, display, ok := decodeIPv4Value(addressRaw); ok {
		p.AgentIP = address
		p.addressNode = node
		p.addressDisplay = display
	}
	gatewayRaw, _ := fields.Get("ha_gateway")
	if emptyInspectionName(gatewayRaw) {
		p.HAGateway = ""
	} else if gateway, _, _, ok := decodeIPv4Value(gatewayRaw); ok {
		p.HAGateway = gateway
	} else {
		p.HAGateway = "invalid-gateway"
	}
	r := GuideRequest{Options: p, InstallMethod: method, Image: DockerHAImage}
	if request != nil {
		r.CreateHostIdentity = isTruthyValue(request["create_host_identity"])
		if image, present := request["image"]; present {
			r.Image, _ = image.(string)
			r.imageRaw, err = json.Marshal(image)
			if err != nil {
				return GuideOptions{}, GuideRequest{}, err
			}
			p.imageRaw = r.imageRaw
		}
	}
	r.Options = p
	return p, r, nil
}

// GenerateCompact adapts test arguments to a typed plan.
func GenerateCompact(params, request map[string]any, enrollment *Enrollment) (string, map[string]any, error) {
	p, r, err := legacyGuideInputs(params, request, "compose_compact")
	if err != nil {
		return "", nil, err
	}
	text, info, err := generateCompactNative(p, r, nativeCapability(enrollment))
	if err != nil {
		return "", nil, err
	}
	result, err := legacyResponse(info)
	return text, result, err
}

// GenerateDirect adapts test arguments to a typed plan.
func GenerateDirect(params, request map[string]any, enrollment *Enrollment) (string, map[string]any, error) {
	p, r, err := legacyGuideInputs(params, request, "compose_direct")
	if err != nil {
		return "", nil, err
	}
	text, info, err := generateDirectNative(p, r, nativeCapability(enrollment))
	if err != nil {
		return "", nil, err
	}
	result, err := legacyResponse(info)
	return text, result, err
}

// GenerateBundle adapts test arguments to a typed plan.
func GenerateBundle(params, request map[string]any, enrollment *Enrollment) (string, map[string]any, error) {
	p, r, err := legacyGuideInputs(params, request, "compose")
	if err != nil {
		return "", nil, err
	}
	text, info, err := generateBundleNative(p, r, nativeCapability(enrollment))
	if err != nil {
		return "", nil, err
	}
	result, err := legacyResponse(info)
	return text, result, err
}

// BootstrapCommand adapts test guide inputs to the image bootstrap command.
func BootstrapCommand(enrollment *Enrollment, request map[string]any) (string, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	var fields model.Fields
	if err := jsondecode.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	image, _ := textOr(fields, "image", DockerHAImage)
	p := GuideOptions{}
	p.DataDir, _ = fieldText(fields, "data_dir")
	p.AgentName, _ = textOr(fields, "agent_name", "agent")
	p.AgentIP, _ = fieldText(fields, "agent_ip")
	p.NetworkMode, _ = fieldText(fields, "network_mode")
	return bootstrapNative(nativeCapability(enrollment), GuideRequest{Options: p, Image: image})
}

func annotateCompose(text string, compose map[string]any) (string, error) {
	raw, err := json.Marshal(compose)
	if err != nil {
		return "", err
	}
	node, err := yamlx.FromJSON(raw)
	if err != nil {
		return "", err
	}
	return annotateComposeNative(text, node)
}

// ErrComposeVariantNotPorted is retained for source compatibility; all variants
// are implemented by the typed renderer.
var ErrComposeVariantNotPorted = errors.New("Docker HA Compose variant is unavailable")
