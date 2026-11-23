package subs

import (
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
// only when the subscription's group had them.
type DialerTarget struct {
	Type           string     `json:"type"`
	Members        []string   `json:"members"`
	URL            *string    `json:"url,omitzero"`
	Interval       *model.Int `json:"interval,omitzero"`
	Tolerance      *model.Int `json:"tolerance,omitzero"`
	Lazy           *bool      `json:"lazy,omitzero"`
	Timeout        *model.Int `json:"timeout,omitzero"`
	ExpectedStatus *string    `json:"expected-status,omitzero"`
	Strategy       *string    `json:"strategy,omitzero"`
	MaxFailedTimes *model.Int `json:"max-failed-times,omitzero"`
}

// DialersData is the bounded subscription dialer metadata stored beside nodes.
type DialersData struct {
	Targets    model.OrderedMap[DialerTarget] `json:"targets"`
	Unresolved []string                       `json:"unresolved"`
}
