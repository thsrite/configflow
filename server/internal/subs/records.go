package subs

import (
	"encoding/json"

	"configflow/internal/model"
)

// HostSkip describes an ignored source entry without carrying node credentials.
type HostSkip struct {
	Domain string `json:"domain"`
	Reason string `json:"reason"`
}

// HostsData is persisted subscription hosts metadata
// (subscription_hosts.extract_subscription_hosts); every member is always
// written. Host values are open DNS mappings.
type HostsData struct {
	Entries      model.OrderedMap[model.Dynamic] `json:"entries"`
	Total        model.Int                       `json:"total"`
	SkippedCount model.Int                       `json:"skipped_count"`
	Skipped      []HostSkip                      `json:"skipped"`
}

// DialerTarget is a supported subscription policy group and its flattened
// members. type and members are always written; the health-check settings
// only when the subscription's group had them, as the scalar the
// subscription wrote (Python stores them as given). Generation reads them
// typed and leaves out a value of another type (dialerSettings).
type DialerTarget struct {
	Type           string          `json:"type"`
	Members        []string        `json:"members"`
	URL            json.RawMessage `json:"url,omitzero"`
	Interval       json.RawMessage `json:"interval,omitzero"`
	Tolerance      json.RawMessage `json:"tolerance,omitzero"`
	Lazy           json.RawMessage `json:"lazy,omitzero"`
	Timeout        json.RawMessage `json:"timeout,omitzero"`
	ExpectedStatus json.RawMessage `json:"expected-status,omitzero"`
	Strategy       json.RawMessage `json:"strategy,omitzero"`
	MaxFailedTimes json.RawMessage `json:"max-failed-times,omitzero"`
}

// DialersData is the bounded subscription dialer metadata stored beside nodes.
type DialersData struct {
	Targets    model.OrderedMap[DialerTarget] `json:"targets"`
	Unresolved []string                       `json:"unresolved"`
}
