package installer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"configflow/internal/model"
)

// identityJSON preserves the published installation identity format. Sorted
// ASCII JSON, numeric normalization and separators are part of these identities;
// this codec must not be used for requests, responses or ordinary storage.
func identityJSON(raw json.RawMessage, compact bool) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("installation identity: trailing JSON")
	}
	var out bytes.Buffer
	var write func(any) error
	write = func(v any) error {
		comma, colon := ",", ":"
		if !compact {
			comma, colon = ", ", ": "
		}
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for key := range x {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			out.WriteByte('{')
			for i, key := range keys {
				if i > 0 {
					out.WriteString(comma)
				}
				if err := write(key); err != nil {
					return err
				}
				out.WriteString(colon)
				if err := write(x[key]); err != nil {
					return err
				}
			}
			out.WriteByte('}')
		case []any:
			out.WriteByte('[')
			for i, item := range x {
				if i > 0 {
					out.WriteString(comma)
				}
				if err := write(item); err != nil {
					return err
				}
			}
			out.WriteByte(']')
		case string:
			var encoded bytes.Buffer
			enc := json.NewEncoder(&encoded)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(x); err != nil {
				return err
			}
			text := bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'})
			for _, r := range string(text) {
				if r < 127 {
					out.WriteRune(r)
					continue
				}
				if r <= 0xffff {
					fmt.Fprintf(&out, "\\u%04x", r)
				} else {
					a, b := utf16.EncodeRune(r)
					fmt.Fprintf(&out, "\\u%04x\\u%04x", a, b)
				}
			}
		case json.Number:
			text := x.String()
			if strings.ContainsAny(text, ".eE") {
				number, err := x.Float64()
				if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
					return fmt.Errorf("installation identity: invalid number")
				}
				format := byte('f')
				if number != 0 && (math.Abs(number) < 1e-4 || math.Abs(number) >= 1e16) {
					format = 'e'
				}
				text = strconv.FormatFloat(number, format, -1, 64)
				if !strings.ContainsAny(text, ".eE") {
					text += ".0"
				}
			}
			if text == "-0" {
				text = "0"
			}
			out.WriteString(text)
		default:
			data, err := json.Marshal(x)
			if err != nil {
				return err
			}
			out.Write(data)
		}
		return nil
	}
	if err := write(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func installationSpecDigest(fields model.Fields) (string, error) {
	raw, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	data, err := identityJSON(raw, false)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func installationSessionJSON(fields model.Fields) ([]byte, error) { return json.Marshal(fields) }
