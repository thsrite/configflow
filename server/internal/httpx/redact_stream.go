package httpx

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
	"slices"

	"configflow/internal/redact"
)

// responseInput keeps the original bytes so member ranges survive the next
// decoder call. A bytes.Buffer lets jsontext read those bytes without a copy.
type responseInput struct {
	text   []byte
	buffer bytes.Buffer
	d      jsontext.Decoder
}

func (in *responseInput) reset(raw []byte) {
	in.text = raw
	in.buffer = *bytes.NewBuffer(raw)
	in.d.Reset(&in.buffer, jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
}
func (in *responseInput) raw() ([]byte, error) {
	v, err := in.d.ReadValue()
	if err != nil {
		return nil, err
	}
	end := int(in.d.InputOffset())
	return in.text[end-len(v) : end], nil
}

type responseField struct {
	raw   []byte
	plain bool
}

// An object needs a sort before emission. Only the active objects' member
// ranges are retained; arrays and scalar tokens go straight to the encoder.
type responseObject struct {
	keys     []string
	original map[string][]byte
	output   map[string]responseField
	child    responseInput
}

type responseRedactor struct {
	encoder  *jsontext.Encoder
	secrets  redact.Secrets
	cache    map[string]string
	sortKeys bool
	objects  []*responseObject
	ctx      *Ctx
	status   int
}

func transformResponse(text []byte, secrets redact.Secrets, sortKeys bool, c *Ctx, status int) ([]byte, error) {
	if c != nil && bytes.Equal(bytes.TrimSpace(text), []byte("null")) {
		return text, nil
	}
	var in responseInput
	in.reset(text)
	var out bytes.Buffer
	out.Grow(len(text) + 1)
	s := responseRedactor{ctx: c, status: status, secrets: secrets, sortKeys: sortKeys, cache: map[string]string{},
		// The output is pre-sized. Use the streaming writer so jsontext does not
		// reserve additional space in the completed bytes.Buffer.
		encoder: jsontext.NewEncoder(struct{ io.Writer }{&out}, jsontext.EscapeForJS(true), jsontext.AllowDuplicateNames(true))}
	if err := s.value(&in, 0, c == nil, false); err != nil {
		return nil, err
	}
	if _, err := in.d.ReadToken(); err == nil {
		return nil, errors.New("trailing data")
	} else if err != io.EOF {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte{'\n'}), nil
}

func (s *responseRedactor) str(value string) string {
	if result, ok := s.cache[value]; ok {
		return result
	}
	result := value
	if redact.ContainsEncodedSecret(value, s.secrets) {
		result = redact.Redacted
	}
	if len(value) <= 1024 && len(s.cache) < 4096 {
		s.cache[value] = result
	}
	return result
}

func (s *responseRedactor) value(in *responseInput, depth int, plain, settings bool) error {
	kind := in.d.PeekKind()
	if !plain && depth >= responseMaxDepth && (kind == '{' || kind == '[') {
		if err := in.d.SkipValue(); err != nil {
			return err
		}
		return s.encoder.WriteToken(jsontext.String(redact.Redacted))
	}
	t, err := in.d.ReadToken()
	if err != nil {
		return err
	}
	switch kind {
	case '"':
		value := t.String()
		if !plain {
			value = s.str(value)
		}
		return s.encoder.WriteToken(jsontext.String(value))
	case '[':
		if err := s.encoder.WriteToken(jsontext.BeginArray); err != nil {
			return err
		}
		for in.d.PeekKind() != ']' {
			if err := s.value(in, depth+1, plain, false); err != nil {
				return err
			}
		}
		if _, err := in.d.ReadToken(); err != nil {
			return err
		}
		return s.encoder.WriteToken(jsontext.EndArray)
	case '{':
		return s.object(in, depth, plain, settings)
	default:
		return s.encoder.WriteToken(t)
	}
}

func (s *responseRedactor) object(in *responseInput, depth int, plain, settings bool) error {
	for len(s.objects) <= depth {
		s.objects = append(s.objects, nil)
	}
	f := s.objects[depth]
	if f == nil {
		f = &responseObject{original: map[string][]byte{}, output: map[string]responseField{}}
		s.objects[depth] = f
	}
	clear(f.original)
	clear(f.output)
	f.keys = f.keys[:0]
	for in.d.PeekKind() != '}' {
		key, err := in.d.ReadToken()
		if err != nil {
			return err
		}
		name := key.String()
		raw, err := in.raw()
		if err != nil {
			return err
		}
		if _, seen := f.original[name]; !seen {
			f.keys = append(f.keys, name)
		}
		f.original[name] = raw
	}
	if _, err := in.d.ReadToken(); err != nil {
		return err
	}
	if s.sortKeys {
		slices.Sort(f.keys)
	}
	for _, name := range f.keys {
		key := name
		if !plain {
			if redact.SensitiveSystemConfigFields[key] {
				continue
			}
			key = s.str(key)
		}
		f.output[key] = responseField{f.original[name], plain}
	}
	if !plain && (depth == 0 || settings) {
		s.restore(f, settings)
	}
	f.keys = f.keys[:0]
	for k := range f.output {
		f.keys = append(f.keys, k)
	}
	slices.Sort(f.keys)
	if err := s.encoder.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for _, key := range f.keys {
		if err := s.encoder.WriteToken(jsontext.String(key)); err != nil {
			return err
		}
		field := f.output[key]
		f.child.reset(field.raw)
		nestedSettings := depth == 0 && key == "settings" && !plain && s.status == 200 && oxidnsSettingsEndpoints[s.ctx.Endpoint] && s.ctx.Request.Method == "POST"
		if err := s.value(&f.child, depth+1, field.plain, nestedSettings); err != nil {
			return err
		}
	}
	return s.encoder.WriteToken(jsontext.EndObject)
}

// restore reinstates the same endpoint-specific original values after key
// collisions have been resolved. Plain values still receive canonical encoding.
func (s *responseRedactor) restore(f *responseObject, settings bool) {
	keep := func(key string, kind byte) {
		if raw := f.original[key]; len(raw) > 0 && (kind == 0 || raw[0] == kind) {
			f.output[key] = responseField{raw, true}
		}
	}
	endpoint := s.ctx.Endpoint
	if s.status == 200 {
		if settings || oxidnsSettingsEndpoints[endpoint] && s.ctx.Request.Method != "POST" {
			for _, key := range oxidnsNativeKeys {
				keep(key, 0)
			}
		}
		if settings {
			return
		}
		if renderDraftEndpoints[endpoint] {
			keep("config", '"')
		}
		if oxidnsValuesEndpoints[endpoint] {
			keep("values", '{')
		}
		if endpoint == "settings.handle_rule_fetch_proxy" {
			keep("rule_fetch_proxy", '"')
		}
	}
	if settings || !bytes.Equal(f.original["success"], []byte("true")) {
		return
	}
	if tokenEndpoints[endpoint] && s.status >= 200 && s.status < 300 {
		keep("token", '"')
	}
	if artifactEndpoints[endpoint] && s.status == 200 {
		keep("content", '"')
	}
}
