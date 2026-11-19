package mcp

import (
	"bytes"
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// object is an open JSON document used for resource extensions, never decoded
// into dynamic Python-shaped values. Values remain native JSON at API boundaries.
type object map[string]json.RawMessage

func packed(o object) json.RawMessage {
	if o == nil {
		o = object{}
	}
	b, _ := json.Marshal(o)
	return b
}                                   // Members originate in a JSON decoder or the scalar encoders below.
func text(s string) json.RawMessage { b, _ := json.Marshal(s); return b } // encoding/json cannot fail for a string.
func flag(b bool) json.RawMessage {
	if b {
		return json.RawMessage(`true`)
	}
	return json.RawMessage(`false`)
}
func number(n int) json.RawMessage { return json.RawMessage(strconv.Itoa(n)) }

// unset is JSON presence, not language truthiness. Other shapes are validated
// by their typed DTO rather than interpreted as a Python false value.
func unset(r json.RawMessage) bool {
	r = bytes.TrimSpace(r)
	return len(r) == 0 || bytes.Equal(r, []byte("null"))
}
func blankText(r json.RawMessage) bool {
	if unset(r) {
		return true
	}
	var s string
	return json.Unmarshal(r, &s) == nil && s == ""
}

func asObject(r json.RawMessage) object {
	var o object
	_ = json.Unmarshal(r, &o)
	if o == nil {
		o = object{}
	}
	return o
}
func copyObject(o object) object {
	next := object{}
	for k, v := range o {
		next[k] = v
	}
	return next
}
func rawString(r json.RawMessage) string {
	var s model.String
	_ = json.Unmarshal(r, &s)
	return string(s)
}
func unwrap(result, fallback json.RawMessage) json.RawMessage {
	if v, ok := asObject(result)["data"]; ok {
		return v
	}
	return fallback
}

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return e.message }
func bad(message string, args ...string) error {
	if len(args) > 0 {
		return &apiError{400, fmt.Sprintf(message, args[0])}
	}
	return &apiError{400, message}
}

type arguments struct {
	ProfileID       *string           `json:"profile_id"`
	ID              string            `json:"id"`
	Action          string            `json:"action"`
	SourceProfileID string            `json:"source_profile_id"`
	Data            object            `json:"data"`
	Params          object            `json:"params"`
	Collection      string            `json:"collection"`
	IDs             []string          `json:"ids"`
	Position        *string           `json:"position"`
	Preview         bool              `json:"preview"`
	IncludeStatus   bool              `json:"include_status"`
	CountOnly       bool              `json:"count_only"`
	Mode            *string           `json:"mode"`
	Scope           *string           `json:"scope"`
	Target          string            `json:"target"`
	BaseURL         string            `json:"base_url"`
	URL             string            `json:"url"`
	LogPath         *string           `json:"log_path"`
	Enabled         *bool             `json:"enabled"`
	Lines           *int              `json:"lines"`
	Hours           *int              `json:"hours"`
	Method          string            `json:"method"`
	Section         string            `json:"section"`
	Search          *string           `json:"search"`
	Level           *string           `json:"level"`
	Content         *string           `json:"content"`
	Regex           string            `json:"regex"`
	Source          json.RawMessage   `json:"source"`
	Rule            object            `json:"rule"`
	Routes          map[string]string `json:"routes"`
	SkipUnsupported *bool             `json:"skip_unsupported"`
	DNS             *string           `json:"dns"`
	DNSIP           *string           `json:"dns_ip"`
	RuleType        string            `json:"rule_type"`
	Domains         []string          `json:"domains"`
	Policy          string            `json:"policy"`
	Query           string            `json:"query"`
	Subscriptions   []string          `json:"subscriptions"`
	Aggregations    []string          `json:"aggregations"`
	Desensitize     *bool             `json:"desensitize"`
}
type invocation struct {
	app      *httpx.App
	args     arguments
	original object
	profile  string
}

func newInvocation(ctx context.Context, app *httpx.App, raw json.RawMessage) (*invocation, error) {
	i := &invocation{app: app}
	var original object
	if json.Unmarshal(raw, &original) != nil {
		return nil, bad(msgType)
	}
	if profile, ok := original["profile_id"]; ok && !unset(profile) {
		var id string
		if json.Unmarshal(profile, &id) != nil {
			return nil, bad(msgProfileType)
		}
	}
	if jsondecode.Unmarshal(raw, &i.args) != nil || jsondecode.Unmarshal(raw, &i.original) != nil {
		return nil, bad(msgType)
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	i.profile, err = r.DefaultProfileID(ctx)
	if err != nil {
		return nil, err
	}
	if i.args.ProfileID != nil && *i.args.ProfileID != "" {
		i.profile = *i.args.ProfileID
	}
	return i, nil
}
func (i *invocation) require(key string) error {
	v, ok := i.original[key]
	if !ok || bytes.Equal(bytes.TrimSpace(v), []byte(`null`)) || bytes.Equal(bytes.TrimSpace(v), []byte(`""`)) {
		return bad(msgMissing, key)
	}
	return nil
}
func (i *invocation) call(ctx context.Context, method, path string, body json.RawMessage, query [][2]string) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Paths are fixed by the tool registry. No client-controlled method, header,
	// origin or internal capability can escape into a network request.
	request := httpx.InvokeRequest{Method: method, Path: path, Query: query, Header: http.Header{"X-ConfigFlow-Profile": []string{i.profile}}}
	if body != nil {
		request.JSON = body
	}
	response, err := i.app.Invoke(ctx, request)
	if err != nil {
		return nil, err
	}
	var payload json.RawMessage
	switch {
	case !utf8.Valid(response.Body):
		payload = packed(object{"binary": flag(true), "content_type": text(strings.Split(response.Header.Get("Content-Type"), ";")[0]), "size": number(len(response.Body))})
	case len(response.Body) == 0:
		payload = json.RawMessage(`null`)
	case json.Valid(response.Body):
		payload = bytes.Clone(response.Body)
	default:
		payload = text(string(response.Body))
	}
	if response.Status >= 400 {
		message := ""
		o := asObject(payload)
		for _, key := range []string{"message", "error", "msg"} {
			if !blankText(o[key]) {
				message = rawString(o[key])
				if message == "" {
					message = string(o[key])
				}
				break
			}
		}
		if message == "" {
			var s string
			if json.Unmarshal(payload, &s) == nil {
				message = strings.TrimSpace(s)
			}
		}
		if message == "" {
			message = fmt.Sprintf("HTTP %d", response.Status)
		}
		return nil, &apiError{response.Status, message}
	}
	return payload, nil
}
func (i *invocation) get(ctx context.Context, path string) (json.RawMessage, error) {
	return i.call(ctx, http.MethodGet, path, nil, nil)
}
func (i *invocation) post(ctx context.Context, path string, body object) (json.RawMessage, error) {
	return i.call(ctx, http.MethodPost, path, packed(body), nil)
}
func (i *invocation) put(ctx context.Context, path string, body object) (json.RawMessage, error) {
	return i.call(ctx, http.MethodPut, path, packed(body), nil)
}
func (i *invocation) delete(ctx context.Context, path string) (json.RawMessage, error) {
	return i.call(ctx, http.MethodDelete, path, nil, nil)
}
func pathPart(id string) (string, error) {
	if id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return "", bad(msgPathID)
	}
	return id, nil
}
func value(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}
func intValue(p *int, fallback int) int {
	if p == nil {
		return fallback
	}
	return *p
}
func addQuery(q *[][2]string, key string, p *string) {
	if p != nil {
		*q = append(*q, [2]string{key, *p})
	}
}
func resultAction(action, id string, item json.RawMessage) json.RawMessage {
	o := object{"action": text(action)}
	if action == "delete" {
		o["id"] = text(id)
	} else {
		o["item"] = item
	}
	return packed(o)
}
