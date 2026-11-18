package health

import (
	"bytes"
	"encoding/json"
	"errors"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
)

type rawObject struct {
	Fields model.Fields
	raw    model.Raw
}

func (o *rawObject) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, o, &o.raw)
}

func (o rawObject) MarshalJSON() ([]byte, error) {
	return model.EncodeRecord(&o, &o.raw)
}

type strictString string

func (s *strictString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return errors.New(msgStrictStringRequired)
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return err
	}
	*s = strictString(value)
	return nil
}

func (s strictString) MarshalJSON() ([]byte, error) {
	return jsonresp.Encode(string(s))
}

func decodeNonNullText(data []byte) (string, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return "", errors.New(msgTextScalarRequired)
	}
	var text model.String
	if err := json.Unmarshal(data, &text); err != nil {
		return "", err
	}
	return string(text), nil
}

func parseNonNegativeJSONInt(data []byte) (json.Number, bool) {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("-0")) || bytes.Equal(trimmed, []byte("0")) {
		return json.Number("0"), true
	}
	if len(trimmed) == 0 || trimmed[0] < '1' || trimmed[0] > '9' {
		return "", false
	}
	for _, b := range trimmed[1:] {
		if b < '0' || b > '9' {
			return "", false
		}
	}
	return json.Number(string(trimmed)), true
}

func parsePositiveJSONInt(data []byte) (json.Number, bool) {
	n, ok := parseNonNegativeJSONInt(data)
	if !ok || n == "0" {
		return "", false
	}
	return n, true
}

func compactRaw(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(bytes.TrimSpace(raw))
	}
	return buf.String()
}

func isStringToken(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '"'
}
