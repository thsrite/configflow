// Package subscriptions serves subscription management and authenticated provider exports.
package subscriptions

import (
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

const msgFilterType = "节点筛选正则必须是字符串"

// proxy is one proxy definition as JSON members: its protocol members are an
// open set owned by the client formats, so it stays a JSON object.
type proxy map[string]json.RawMessage

func (o proxy) text(key string) string { var v string; _ = json.Unmarshal(o[key], &v); return v }

// subscriptionFields is a subscription as sent: its members in order, as
// JSON, after the derived statistics are dropped. The stored record decodes it.
type subscriptionFields struct{ model.Fields }

func (f subscriptionFields) text(key string) string {
	raw, _ := f.Get(key)
	var v string
	_ = json.Unmarshal(raw, &v)
	return v
}
func (f *subscriptionFields) set(key string, value any) {
	raw, _ := json.Marshal(value)
	_ = f.Set(key, raw)
}
func (f *subscriptionFields) stripDerived() {
	for _, key := range []string{"cached_node_count", "cached_total_count", "cached_updated_at", "traffic", "traffic_at", "last_fetch"} {
		f.Delete(key)
	}
}

// record is the stored subscription the fields decode to.
func (f subscriptionFields) record() (model.Subscription, error) {
	var out model.Subscription
	data, err := f.MarshalJSON()
	if err == nil {
		err = json.Unmarshal(data, &out)
	}
	return out, err
}

func response(status int, value any) *httpx.Response {
	data, err := json.Marshal(value)
	if err != nil {
		return httpx.Message(500, false, err.Error())
	}
	return httpx.RawJSON(status, data)
}
func readBody(c *httpx.Ctx) (*subscriptionFields, error) {
	var raw json.RawMessage
	if err := c.Bind(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, nil
	}
	out := &subscriptionFields{}
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, nil
	}
	return out, nil
}

// readRequestObject rejects every non-object, including null and empty arrays,
// before a reorder or fetch can acquire a write lock or contact a source.
func readRequestObject(c *httpx.Ctx) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.Bind(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, httpx.BadRequest(msgBodyObject)
	}
	return raw, nil
}

type cache struct {
	Nodes     []proxy         `json:"nodes"`
	Count     json.RawMessage `json:"count"`
	UpdatedAt json.RawMessage `json:"updated_at"`
	Metadata  cacheMetadata   `json:"metadata"`
}

// cacheMetadata is the source a cache was fetched from.
type cacheMetadata struct {
	URL jsondecode.Member[string] `json:"url"`
}

type fetchReply struct {
	Success         bool            `json:"success"`
	Count           int             `json:"count"`
	Nodes           []proxy         `json:"nodes"`
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

func validateFields(sub *subscriptionFields) error {
	for _, key := range []string{"name", "url"} {
		if strings.TrimSpace(sub.text(key)) == "" {
			return fmt.Errorf(msgRequiredField, key)
		}
	}
	return validateOptions(sub)
}
