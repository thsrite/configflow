// Package subscriptions serves subscription management and authenticated provider exports.
package subscriptions

import (
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/health/check"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

const msgFilterType = "节点筛选正则必须是字符串"

// proxy is one proxy definition as JSON members: its protocol members are an
// open set owned by the client formats, so it stays a JSON object.
type proxy map[string]json.RawMessage

func (o proxy) text(key string) string { var v string; _ = json.Unmarshal(o[key], &v); return v }

// subscriptionRequest is a subscription create/replace request body decoded
// with exact member matching. Derived display statistics and undeclared
// members are ignored.
type subscriptionRequest struct {
	ID             jsondecode.Member[json.RawMessage] `json:"id"`
	Name           jsondecode.Member[string]          `json:"name"`
	URL            jsondecode.Member[string]          `json:"url"`
	Type           jsondecode.Member[json.RawMessage] `json:"type"`
	Enabled        jsondecode.Member[json.RawMessage] `json:"enabled"`
	Interval       jsondecode.Member[json.RawMessage] `json:"interval"`
	RegexFilter    jsondecode.Member[string]          `json:"regex_filter"`
	Description    jsondecode.Member[json.RawMessage] `json:"description"`
	HealthCheck    jsondecode.Member[json.RawMessage] `json:"health_check"`
	HealthCheckURL jsondecode.Member[json.RawMessage] `json:"health_check_url"`
}

// decodeSubscription is the stored subscription a create/replace request
// becomes. The members decode with the stored-data rules, as before the typed
// request: a member of another type is read leniently (a number as text, a
// non-object health_check as absent) instead of refusing the request. overrideID
// replaces the sent id (replace); a creation without health_check gets the
// provider defaults.
func decodeSubscription(req *subscriptionRequest, overrideID string, creating bool) (model.Subscription, error) {
	var sub model.Subscription
	for _, field := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"id", req.ID.Raw},
		{"name", req.Name.Raw},
		{"url", req.URL.Raw},
		{"type", req.Type.Raw},
		{"enabled", req.Enabled.Raw},
		{"interval", req.Interval.Raw},
		{"regex_filter", req.RegexFilter.Raw},
		{"description", req.Description.Raw},
		{"health_check", req.HealthCheck.Raw},
		{"health_check_url", req.HealthCheckURL.Raw},
	} {
		if field.raw == nil {
			continue
		}
		if err := model.SetStoredMember(&sub, field.name, field.raw); err != nil {
			return model.Subscription{}, err
		}
	}
	if overrideID != "" {
		sub.ID = overrideID
	}
	if creating && !req.HealthCheck.Present() {
		sub.HealthCheck = check.ProviderHealthDefaults(req.HealthCheckURL.Raw)
	}
	return sub, nil
}

func response(status int, value any) *httpx.Response {
	data, err := json.Marshal(value)
	if err != nil {
		return httpx.Message(500, false, err.Error())
	}
	return httpx.RawJSON(status, data)
}
func decodeSubscriptionBody(c *httpx.Ctx) (*subscriptionRequest, bool, error) {
	req, ok, err := httpx.BindObject[subscriptionRequest](c, false)
	if err != nil || !ok {
		return nil, ok, err
	}
	return &req, true, nil
}

// readRequestObject rejects every non-object, including null and empty arrays,
// before a reorder or fetch can acquire a write lock or contact a source.
func readRequestObject(c *httpx.Ctx) (json.RawMessage, error) {
	raw, ok, err := httpx.BindObject[json.RawMessage](c, false)
	if err != nil {
		return nil, err
	}
	if !ok {
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

func validateFields(sub *subscriptionRequest) error {
	if strings.TrimSpace(sub.Name.Value) == "" {
		return fmt.Errorf(msgRequiredField, "name")
	}
	if strings.TrimSpace(sub.URL.Value) == "" {
		return fmt.Errorf(msgRequiredField, "url")
	}
	return validateOptions(sub)
}
