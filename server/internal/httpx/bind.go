package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"configflow/internal/jsondecode"
)

// Bind decodes one JSON request into dst with exact struct member names. It shares JSON's
// media-type, byte-encoding and body-cache handling. Invalid JSON or an
// incompatible DTO returns 400; a non-JSON Content-Type returns 415. Unknown
// fields are ignored unless the DTO explicitly validates them. Duplicate object
// members replace the entire earlier value, including nested objects.
func (c *Ctx) Bind(dst any) error {
	if !c.IsJSON() {
		return &HTTPError{Code: 415, Description: errNotJSONDescription}
	}
	if err := c.bind(dst); err != nil {
		var invalid *json.InvalidUnmarshalError
		if errors.As(err, &invalid) {
			return err
		}
		return BadRequest("")
	}
	return nil
}

// BindSilent decodes a JSON request like get_json(silent=True). It returns false
// and clears dst when the media type, JSON or DTO is invalid. Valid null binds
// the destination's zero value and returns true. dst must be a non-nil pointer.
func (c *Ctx) BindSilent(dst any) bool {
	if !c.IsJSON() {
		clearBinding(dst)
		return false
	}
	return c.bind(dst) == nil
}

func clearBinding(dst any) {
	v := reflect.ValueOf(dst)
	if v.IsValid() && v.Kind() == reflect.Pointer && !v.IsNil() && v.Elem().CanSet() {
		v.Elem().SetZero()
	}
}

func (c *Ctx) bind(dst any) error {
	target := reflect.ValueOf(dst)
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		return fmt.Errorf("bind JSON: %w", &json.InvalidUnmarshalError{Type: reflect.TypeOf(dst)})
	}
	clearBinding(dst)
	data, err := c.Data()
	if err != nil {
		return err
	}
	text, err := decodeJSONBytes(data)
	if err != nil {
		return err
	}
	if !json.Valid(text) {
		return errors.New("bind JSON: malformed document")
	}
	d := json.NewDecoder(bytes.NewReader(text))
	d.UseNumber()
	value, err := bindingValue(d, text)
	if err != nil {
		return err
	}
	if _, err = d.Token(); err != io.EOF {
		if err == nil {
			return errors.New("bind JSON: trailing data")
		}
		return err
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err = e.Encode(value); err != nil {
		return err
	}
	// Decode into a fresh destination: encoding/json may partially update a DTO
	// before returning a type error, which must not leak through silent binding.
	next := reflect.New(target.Elem().Type())
	if err = jsondecode.Decode(b.Bytes(), next.Interface()); err != nil {
		return err
	}
	target.Elem().Set(next.Elem())
	return nil
}

// bindingValue uses only the native token parser for syntax. Containers are
// collected once before one encoding pass, so nested duplicates replace values
// without encoding/json's struct/map merge behavior or quadratic re-encoding.
// Scalar token spans remain RawMessage: integer/fraction/exponent spellings and
// string escapes survive into custom DTO unmarshalers unchanged.
func bindingValue(d *json.Decoder, source []byte) (any, error) {
	start := d.InputOffset()
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		out := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			value, err := bindingValue(d, source)
			if err != nil {
				return nil, err
			}
			out[key.(string)] = value
		}
		_, err = d.Token()
		return out, err
	case json.Delim('['):
		out := []any{}
		for d.More() {
			value, err := bindingValue(d, source)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		_, err = d.Token()
		return out, err
	default:
		raw := bytes.TrimSpace(source[start:d.InputOffset()])
		// InputOffset starts after the previous token, before the native decoder
		// consumes the array comma or object colon. Remove that one separator only.
		if len(raw) > 0 && (raw[0] == ',' || raw[0] == ':') {
			raw = bytes.TrimSpace(raw[1:])
		}
		return json.RawMessage(raw), nil
	}
}
