package notifications

import (
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"slices"

	"configflow/internal/model"
)

// Navigation is a frontend destination with typed query values.
type Navigation struct {
	Path  string            `json:"path"`
	Query model.Map[string] `json:"query"`
	Label string            `json:"label"`
	Extra model.Fields
	Raw   model.Raw
}

// UnmarshalJSON retains unknown destination fields.
func (n *Navigation) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, n, &n.Raw) }

// MarshalJSON writes the destination without losing stored members.
func (n Navigation) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&n, &n.Raw) }

// Destination constructs a complete destination, including an empty query object.
func Destination(path, label string, query map[string]string) *Navigation {
	n := &Navigation{Path: path, Label: label}
	_ = n.Query.UnmarshalJSON([]byte("{}"))
	for _, key := range slices.Sorted(maps.Keys(query)) {
		n.Query.Set(key, query[key])
	}
	return n
}

// OccurrenceCount is an arbitrary-precision incident count. Integer JSON
// literals never pass through int64 or float64; R8 integral notation keeps the
// shared model.Int validation, and malformed stored values still follow R5.
type OccurrenceCount big.Int

// UnmarshalJSON accepts integer literals and the existing R8 integral notation.
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

// MarshalJSON emits the exact integer, leaving model.Raw to retain unedited notation.
func (c OccurrenceCount) MarshalJSON() ([]byte, error) {
	value := big.Int(c)
	return value.MarshalJSON()
}

// Item is one incident or observation. Evidence remains opaque JSON; identifiers,
// state and presentation fields are typed. Raw retains unowned stored members.
type Item struct {
	State        string           `json:"state"`
	Source       string           `json:"source"`
	Title        *string          `json:"title"`
	Message      *string          `json:"message"`
	Impact       *string          `json:"impact"`
	StatusLabel  *string          `json:"status_label"`
	Severity     *string          `json:"severity"`
	Scope        *string          `json:"scope"`
	ProfileID    *string          `json:"profile_id"`
	ProfileName  *string          `json:"profile_name"`
	Target       *Navigation      `json:"target"`
	CheckedAt    json.RawMessage  `json:"checked_at"`
	FailureCount json.RawMessage  `json:"failure_count"`
	Signature    *string          `json:"signature"`
	Evidence     json.RawMessage  `json:"evidence"`
	ID           string           `json:"id"`
	Read         *bool            `json:"read"`
	Version      *string          `json:"version"`
	FirstSeenAt  *string          `json:"first_seen_at"`
	LastSeenAt   *string          `json:"last_seen_at"`
	Occurrences  *OccurrenceCount `json:"occurrences"`
	Fingerprint  *string          `json:"fingerprint"`
	Stale        *bool            `json:"stale"`
	Extra        model.Fields
	Raw          model.Raw
}

// UnmarshalJSON decodes an incident with R5 provenance.
func (i *Item) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, i, &i.Raw) }

// MarshalJSON preserves untouched members.
func (i Item) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&i, &i.Raw) }

// Items is the keyed incident store. Non-object entries are invalid, never dropped.
type Items struct{ model.Map[*Item] }

// UnmarshalJSON validates the store shape before exposing any entries.
func (i *Items) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return errors.Join(ErrInvalidStore, err)
	}
	if fields == nil {
		return ErrInvalidStore
	}
	for _, raw := range fields {
		var item Item
		if string(raw) == "null" {
			return errInvalidItem
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return errInvalidItem
		}
	}
	return i.Map.UnmarshalJSON(data)
}

// MarshalJSON retains the stable incident order used for equal-time display ties.
func (i Items) MarshalJSON() ([]byte, error) { return i.Map.MarshalJSON() }
func newItems() *Items                       { items := &Items{}; _ = items.UnmarshalJSON([]byte("{}")); return items }

type storeDocument struct {
	Items *Items `json:"items"`
	Extra model.Fields
	Raw   model.Raw
}

func (d *storeDocument) UnmarshalJSON(data []byte) error {
	// Reject wrong item shapes rather than letting record R5 discard the store.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return errors.Join(ErrInvalidStore, err)
	}
	if fields == nil || fields["items"] == nil {
		return ErrInvalidStore
	}
	var items Items
	if err := json.Unmarshal(fields["items"], &items); err != nil {
		return err
	}
	if err := model.DecodeRecord(data, d, &d.Raw); err != nil {
		return err
	}
	d.Items = &items
	return nil
}
func (d storeDocument) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&d, &d.Raw) }

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func jsonString(value string) json.RawMessage      { data, _ := json.Marshal(value); return data }
func nullableString(value *string) json.RawMessage { data, _ := json.Marshal(value); return data }

func (i *Item) profile(id, name *string) error {
	i.ProfileID = id
	i.ProfileName = name
	if id == nil {
		if err := model.SetMemberJSON(i, &i.Raw, "profile_id", json.RawMessage("null")); err != nil {
			return err
		}
	}
	if name == nil {
		return model.SetMemberJSON(i, &i.Raw, "profile_name", json.RawMessage("null"))
	}
	return nil
}

// RouterCheck is one driver report section consumed by notifications.
type RouterCheck struct {
	Status     string   `json:"status"`
	Enabled    *bool    `json:"enabled"`
	FixCommand string   `json:"fix_command"`
	Warning    string   `json:"warning"`
	Message    string   `json:"message"`
	Blockers   []string `json:"blockers"`
	Skip       []string `json:"skip"`
	Extra      model.Fields
	Raw        model.Raw
}

// UnmarshalJSON decodes a report section with field-local fallback.
func (r *RouterCheck) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, r, &r.Raw) }

// MarshalJSON retains the original report for a legacy summary hook.
func (r RouterCheck) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }

// RouteHints carries the IPv4 forwarding hint.
type RouteHints struct {
	ForwardHint *RouterCheck `json:"forward_hint"`
	Extra       model.Fields
	Raw         model.Raw
}

// UnmarshalJSON decodes route hints.
func (r *RouteHints) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, r, &r.Raw) }

// MarshalJSON retains route hint members.
func (r RouteHints) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }

// RouterReport is the subset of a device report that notifications consume.
type RouterReport struct {
	Routes      *RouteHints     `json:"routes"`
	Desired     json.RawMessage `json:"desired"`
	InSync      *bool           `json:"in_sync"`
	IPv6FakeIP  *RouterCheck    `json:"ipv6_fakeip"`
	IPv6Routes  *RouterCheck    `json:"ipv6_routes"`
	IPv6DNS     *RouterCheck    `json:"ipv6_dns"`
	IPv6PDGuard *RouterCheck    `json:"ipv6_pd_guard"`
	HasRoutes   bool            `json:"-"`
	Extra       model.Fields
	Raw         model.Raw
}

// UnmarshalJSON records the difference between absent and explicitly null routes.
func (r *RouterReport) UnmarshalJSON(data []byte) error {
	if err := model.DecodeRecord(data, r, &r.Raw); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, r.HasRoutes = fields["routes"]
	return nil
}

// MarshalJSON retains the full report for unconverted hooks.
func (r RouterReport) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }

// RouterSummary is the publication summary used to display a report incident.
type RouterSummary struct {
	State   string    `json:"state"`
	Pending model.Int `json:"pending"`
	Message string    `json:"message"`
	Extra   model.Fields
	Raw     model.Raw
}

// UnmarshalJSON decodes a summary with field-local fallback.
func (r *RouterSummary) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, r, &r.Raw) }

// MarshalJSON retains the summary fields.
func (r RouterSummary) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }
