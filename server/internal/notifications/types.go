package notifications

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"maps"
	"math/big"
	"slices"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
)

// Navigation is a frontend destination with typed query values. Python's
// target() always writes all three members.
type Navigation struct {
	Path  string                   `json:"path"`
	Query model.OrderedMap[string] `json:"query"`
	Label string                   `json:"label"`
}

// Destination constructs a complete destination, including an empty query object.
func Destination(path, label string, query map[string]string) *Navigation {
	n := &Navigation{Path: path, Label: label}
	for _, key := range slices.Sorted(maps.Keys(query)) {
		n.Query.Set(key, query[key])
	}
	return n
}

// OccurrenceCount is an arbitrary-precision incident count. Integer JSON
// literals never pass through int64 or float64; integral notation such as
// 2.0 is read through model.Int.
type OccurrenceCount big.Int

// UnmarshalJSON accepts integer literals and integral notation.
func (c *OccurrenceCount) UnmarshalJSON(data []byte) error {
	var count big.Int
	if err := count.UnmarshalJSON(data); err == nil {
		*c = OccurrenceCount(count)
		return nil
	}
	var integral model.Int
	if err := json.Unmarshal(data, &integral); err != nil {
		return err
	}
	count.SetInt64(int64(integral))
	*c = OccurrenceCount(count)
	return nil
}

// MarshalJSON emits the exact integer.
func (c OccurrenceCount) MarshalJSON() ([]byte, error) {
	value := big.Int(c)
	return value.MarshalJSON()
}

// Item is one incident or observation (notification_store.observe). Every
// member is optional: a dismissed receipt has only state, fingerprint and
// source, and an observation carries the members its producer sets. Members
// that a producer may write as null (a task without a configuration space:
// profile_id, profile_name; checked_at) or that are opaque (evidence,
// failure_count) are kept as JSON, so null stays present as Python wrote it.
// A stored text member that is null reads as absent.
type Item struct {
	State        string           `json:"state,omitzero"`
	Source       string           `json:"source,omitzero"`
	Title        *string          `json:"title,omitzero"`
	Message      *string          `json:"message,omitzero"`
	Impact       *string          `json:"impact,omitzero"`
	StatusLabel  *string          `json:"status_label,omitzero"`
	Severity     *string          `json:"severity,omitzero"`
	Scope        *string          `json:"scope,omitzero"`
	ProfileID    json.RawMessage  `json:"profile_id,omitzero"`
	ProfileName  json.RawMessage  `json:"profile_name,omitzero"`
	Target       *Navigation      `json:"target,omitzero"`
	CheckedAt    json.RawMessage  `json:"checked_at,omitzero"`
	FailureCount json.RawMessage  `json:"failure_count,omitzero"`
	Signature    *string          `json:"signature,omitzero"`
	Evidence     json.RawMessage  `json:"evidence,omitzero"`
	ID           string           `json:"id,omitzero"`
	Read         *bool            `json:"read,omitzero"`
	Version      *string          `json:"version,omitzero"`
	FirstSeenAt  *string          `json:"first_seen_at,omitzero"`
	LastSeenAt   *string          `json:"last_seen_at,omitzero"`
	Occurrences  *OccurrenceCount `json:"occurrences,omitzero"`
	Fingerprint  *string          `json:"fingerprint,omitzero"`
	Stale        *bool            `json:"stale,omitzero"`
}

// ProfileText is the text of profile_id ("" when absent, null or not text).
func (i *Item) ProfileText() string { return jsonText(i.ProfileID) }

// Items is the keyed incident store in stored order (equal-time display ties).
type Items = model.OrderedMap[*Item]

func newItems() *Items { return &Items{} }

// storeDocument is notifications.json: {"items": {...}}.
type storeDocument struct {
	Items *Items `json:"items"`
}

// decodeStore reads notifications.json. A file that is not {"items": {...}}
// or holds an item that is not an object is refused (Python raised
// ValueError), never rewritten. A member of the wrong type reads as absent
// and is reported; the receipts are display state the next refresh observes
// again.
func decodeStore(data []byte) (storeDocument, error) {
	var shape struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	if err := jsonresp.DecodeStored(data, &shape); err != nil {
		return storeDocument{}, errors.Join(ErrInvalidStore, err)
	}
	if shape.Items == nil {
		return storeDocument{}, ErrInvalidStore
	}
	for _, raw := range shape.Items {
		if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
			return storeDocument{}, errInvalidItem
		}
	}
	data, _ = jsonresp.NormalizeNonFinite(data)
	var document storeDocument
	issues, err := model.DecodeStored(data, &document, "")
	if err != nil {
		return storeDocument{}, errors.Join(ErrInvalidStore, err)
	}
	for _, issue := range issues {
		logger.Warningf(msgStoredFieldIssue, issue.Path, issue.Kind)
	}
	if document.Items == nil {
		document.Items = newItems()
	}
	return document, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func jsonString(value string) json.RawMessage      { data, _ := json.Marshal(value); return data }
func nullableString(value *string) json.RawMessage { data, _ := json.Marshal(value); return data }

func (i *Item) profile(id, name *string) {
	i.ProfileID = nullableString(id)
	i.ProfileName = nullableString(name)
}

// jsonText is the text of a JSON string value, "" for anything else.
func jsonText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return ""
	}
	return text
}

// RouterCheck is one driver report section consumed by notifications.
type RouterCheck struct {
	Status     string   `json:"status,omitzero"`
	Enabled    *bool    `json:"enabled,omitzero"`
	FixCommand string   `json:"fix_command,omitzero"`
	Warning    string   `json:"warning,omitzero"`
	Message    string   `json:"message,omitzero"`
	Blockers   []string `json:"blockers,omitzero"`
	Skip       []string `json:"skip,omitzero"`
	// members is the number of members the report section has; a section
	// without any is empty (Python: report.get(section) or {}), even when
	// it only has members notifications do not read.
	members int
}

// UnmarshalJSON decodes a report section leniently: a member of the wrong
// type reads as absent.
func (r *RouterCheck) UnmarshalJSON(data []byte) error {
	type wire RouterCheck
	var decoded wire
	if _, err := model.DecodeStored(data, &decoded, ""); err != nil {
		return err
	}
	var members map[string]jsontext.Value
	if json.Unmarshal(data, &members) != nil {
		members = nil
	}
	*r = RouterCheck(decoded)
	r.members = len(members)
	return nil
}

// empty reports a section without members.
func (r *RouterCheck) empty() bool {
	if r == nil {
		return true
	}
	if r.members > 0 {
		return false
	}
	encoded, err := json.Marshal(r)
	return err == nil && string(encoded) == "{}"
}

// RouteHints carries the IPv4 forwarding hint.
type RouteHints struct {
	ForwardHint *RouterCheck `json:"forward_hint,omitzero"`
}

// RouterReport is the subset of a device report that notifications consume.
// Source keeps the whole report as received: the publication summary reads
// members notifications do not declare.
type RouterReport struct {
	Routes      *RouteHints     `json:"routes,omitzero"`
	Desired     json.RawMessage `json:"desired,omitzero"`
	InSync      *bool           `json:"in_sync,omitzero"`
	IPv6FakeIP  *RouterCheck    `json:"ipv6_fakeip,omitzero"`
	IPv6Routes  *RouterCheck    `json:"ipv6_routes,omitzero"`
	IPv6DNS     *RouterCheck    `json:"ipv6_dns,omitzero"`
	IPv6PDGuard *RouterCheck    `json:"ipv6_pd_guard,omitzero"`
	HasRoutes   bool            `json:"-"`
	Source      jsontext.Value  `json:"-"`
}

// UnmarshalJSON decodes the report leniently and records the difference
// between absent and explicitly null routes.
func (r *RouterReport) UnmarshalJSON(data []byte) error {
	type wire RouterReport
	var decoded wire
	if _, err := model.DecodeStored(data, &decoded, ""); err != nil {
		return err
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil {
		return err
	}
	*r = RouterReport(decoded)
	_, r.HasRoutes = members["routes"]
	r.Source = bytes.Clone(data)
	return nil
}

// JSON returns the report as received, or its typed members for a report
// built in Go.
func (r *RouterReport) JSON() (json.RawMessage, error) {
	if len(r.Source) > 0 {
		return bytes.Clone(r.Source), nil
	}
	return json.Marshal(r)
}

// RouterSummary is the publication summary used to display a report incident.
type RouterSummary struct {
	State   string    `json:"state"`
	Pending model.Int `json:"pending"`
	Message string    `json:"message"`
}
