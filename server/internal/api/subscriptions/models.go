// Package subscriptions serves subscription management and authenticated provider exports.
package subscriptions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/httpx"
)

// object is an extensible persisted resource. Raw values survive read/modify/write.
type object map[string]json.RawMessage

func (o object) text(key string) string    { var v string; _ = json.Unmarshal(o[key], &v); return v }
func (o object) set(key string, value any) { o[key], _ = json.Marshal(value) }
func (o object) enabled() bool {
	return string(o["enabled"]) != "false" && string(o["enabled"]) != "null" && string(o["enabled"]) != "0" && string(o["enabled"]) != `""`
}
func (o object) stripDerived() {
	for _, key := range []string{"cached_node_count", "cached_total_count", "cached_updated_at", "traffic", "traffic_at", "last_fetch"} {
		delete(o, key)
	}
}
func response(status int, value any) *httpx.Response {
	data, err := json.Marshal(value)
	if err != nil {
		return httpx.Message(500, false, err.Error())
	}
	return httpx.RawJSON(status, data)
}
func readBody(c *httpx.Ctx) (object, error) {
	if _, err := c.JSON(); err != nil {
		return nil, err
	}
	data, err := c.Data()
	if err != nil {
		return nil, err
	}
	var value object
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, nil
	}
	return value, nil
}

// readOptionalBody accepts the empty legacy payloads used by optional-body
// endpoints, but never turns a populated non-object into a write request.
func readOptionalBody(c *httpx.Ctx) (object, error) {
	body, err := readBody(c)
	if err != nil || body != nil {
		return body, err
	}
	data, err := c.Data()
	if err != nil {
		return nil, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return nil, err
	}
	if nonempty(compact.Bytes()) {
		return nil, errors.New("request body must be a JSON object")
	}
	return object{}, nil
}

type cache struct {
	Nodes     []object        `json:"nodes"`
	Count     json.RawMessage `json:"count"`
	UpdatedAt json.RawMessage `json:"updated_at"`
	Metadata  object          `json:"metadata"`
}
type fetchReply struct {
	Success         bool            `json:"success"`
	Count           int             `json:"count"`
	Nodes           []object        `json:"nodes"`
	Preview         bool            `json:"preview,omitempty"`
	Added           *int            `json:"added,omitempty"`
	FromCache       bool            `json:"from_cache"`
	FetchError      *string         `json:"fetch_error"`
	CachedCount     int             `json:"cached_count"`
	TotalCount      json.RawMessage `json:"total_count"`
	CachedUpdatedAt json.RawMessage `json:"cached_updated_at"`
}
type healthEntry struct {
	History   []json.RawMessage `json:"history"`
	Traffic   json.RawMessage   `json:"traffic"`
	TrafficAt json.RawMessage   `json:"traffic_at"`
}

func validateFields(sub object) error {
	for _, key := range []string{"name", "url"} {
		if strings.TrimSpace(sub.text(key)) == "" {
			return fmt.Errorf(msgRequiredField, key)
		}
	}
	return validateOptions(sub)
}
