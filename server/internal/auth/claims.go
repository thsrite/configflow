package auth

import (
	"encoding/json"
)

// Claims is an authenticated JWT payload. Registered fields retain their JSON
// form for claim validation; unknown application claims remain opaque JSON.
type Claims struct {
	Username  json.RawMessage
	Expires   json.RawMessage
	IssuedAt  json.RawMessage
	NotBefore json.RawMessage
	Audience  json.RawMessage
	Error     json.RawMessage
	Detail    json.RawMessage
	Extra     map[string]json.RawMessage
}

// UnmarshalJSON decodes exact-case registered names and keeps extensions.
func (c *Claims) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*c = Claims{Extra: fields}
	for key, dst := range map[string]*json.RawMessage{"username": &c.Username, "exp": &c.Expires, "iat": &c.IssuedAt, "nbf": &c.NotBefore, "aud": &c.Audience, "error": &c.Error, "detail": &c.Detail} {
		*dst = fields[key]
		delete(fields, key)
	}
	return nil
}

// MarshalJSON encodes registered claims and unknown fields without changing types.
func (c Claims) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(c.Extra)+7)
	for key, value := range c.Extra {
		fields[key] = value
	}
	for key, value := range map[string]json.RawMessage{"username": c.Username, "exp": c.Expires, "iat": c.IssuedAt, "nbf": c.NotBefore, "aud": c.Audience, "error": c.Error, "detail": c.Detail} {
		if value != nil {
			fields[key] = value
		}
	}
	return json.Marshal(fields)
}

func (c *Claims) empty() bool {
	return c == nil || len(c.Extra) == 0 && c.Username == nil && c.Expires == nil && c.IssuedAt == nil && c.NotBefore == nil && c.Audience == nil && c.Error == nil && c.Detail == nil
}

func claimString(raw json.RawMessage) string {
	var text string
	_ = json.Unmarshal(raw, &text)
	return text
}
