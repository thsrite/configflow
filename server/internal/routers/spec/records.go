package spec

import (
	"encoding/json"
	"slices"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/routers/device"
)

// DesiredDNSGuard binds the validated settings to the previous publication.
type DesiredDNSGuard struct {
	Settings DNSGuard        `json:"settings"`
	Previous json.RawMessage `json:"previous"`
}

// ROSPolicyDesired separates selected destinations from ordinary static routes.
type ROSPolicyDesired struct {
	Selected       []string        `json:"selected"`
	Sources        []string        `json:"sources"`
	CIDRs          []ProxyCIDR     `json:"cidrs"`
	Error          string          `json:"error"`
	Owner          string          `json:"owner"`
	Previous       json.RawMessage `json:"previous"`
	Warnings       []string        `json:"warnings,omitempty"`
	SourceRevision string          `json:"source_revision,omitempty"`
}

// MarshalJSON keeps the selected-policy empty warnings array; a withdrawn
// policy has no source_revision or warnings member, as in the inspection API.
func (d ROSPolicyDesired) MarshalJSON() ([]byte, error) {
	type plain ROSPolicyDesired
	if d.SourceRevision != "" {
		return json.Marshal(struct {
			plain
			Warnings []string `json:"warnings"`
		}{plain(d), append([]string{}, d.Warnings...)})
	}
	return json.Marshal(plain(d))
}

// RoutersOf decodes stored router records from one encoded profile config.
// Malformed records are skipped so one bad item only affects its own row.
func RoutersOf(config json.RawMessage) []*Router {
	var cfg struct {
		Routers json.RawMessage `json:"routers"`
	}
	if jsondecode.Unmarshal(config, &cfg) != nil {
		return nil
	}
	return RouterRecords(cfg.Routers)
}

// StoredRouters is RoutersOf for a profile's typed routers member. Only the
// member is encoded, to the bytes it has inside the encoded configuration.
func StoredRouters(routers *model.Routers) ([]*Router, error) {
	data, err := model.Marshal(routers)
	if err != nil {
		return nil, err
	}
	return RouterRecords(data), nil
}

// RouterRecords decodes the items of an encoded routers member like RoutersOf.
func RouterRecords(container json.RawMessage) []*Router {
	var routers struct {
		Items json.RawMessage `json:"items"`
	}
	var items []json.RawMessage
	if jsondecode.Unmarshal(container, &routers) != nil || jsondecode.Unmarshal(routers.Items, &items) != nil {
		return nil
	}
	out := make([]*Router, 0, len(items))
	for _, item := range items {
		var fields map[string]json.RawMessage
		if json.Unmarshal(item, &fields) != nil || fields == nil {
			continue
		}
		r := &Router{}
		if json.Unmarshal(item, r) != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ReleaseRouterRecord preserves missing stored connection fields for the repo
// CAS. Native defaults are runtime values, not an edit to the stored record.
func ReleaseRouterRecord(r *Router) (json.RawMessage, error) {
	if len(r.source) > 0 {
		var original Router
		if err := json.Unmarshal(r.source, &original); err != nil {
			return nil, err
		}
		if device.JSONEqual(r, &original) {
			return slices.Clone(r.source), nil
		}
	}
	return json.Marshal(r)
}

// SameReleaseRouter reports whether two router records produce equal release CAS bytes.
func SameReleaseRouter(a, b *Router) bool {
	left, e := ReleaseRouterRecord(a)
	if e != nil {
		return false
	}
	right, e := ReleaseRouterRecord(b)
	return e == nil && device.JSONEqual(left, right)
}

// StoredRecord returns retained stored fields (including malformed legacy
// members) when settings are unchanged; otherwise it encodes the edited record.
func (r *Router) StoredRecord() (json.RawMessage, error) { return ReleaseRouterRecord(r) }

// ClearSource clears the retained original JSON bytes so the next marshal reflects edits.
func (r *Router) ClearSource() { r.source = nil }

// SetSource records raw stored JSON bytes for CAS preservation.
func (r *Router) SetSource(raw json.RawMessage) { r.source = slices.Clone(raw) }

// FindSyncRouter locates a router record by id in one encoded profile config.
func FindSyncRouter(config json.RawMessage, id string) *Router {
	for _, r := range RoutersOf(config) {
		if r.ID == id {
			return r
		}
	}
	return nil
}
