package subs

import (
	"configflow/internal/model"
)

// HostSkip describes an ignored source entry without carrying node credentials.
type HostSkip struct {
	Domain string `json:"domain"`
	Reason string `json:"reason"`
	Fields model.Fields
	Raw    model.Raw
}

// UnmarshalJSON retains stored members with the shared model codec.
func (r *HostSkip) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, r, &r.Raw) }

// MarshalJSON preserves unknown and unchanged members.
func (r HostSkip) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }

// HostsData is persisted subscription hosts metadata. Host values are open DNS mappings.
type HostsData struct {
	Entries      model.Map[model.Dynamic] `json:"entries"`
	Total        model.Int                `json:"total"`
	SkippedCount model.Int                `json:"skipped_count"`
	Skipped      []HostSkip               `json:"skipped"`
	Fields       model.Fields
	Raw          model.Raw
}

// UnmarshalJSON retains stored members with the shared model codec.
func (r *HostsData) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, r, &r.Raw) }

// MarshalJSON preserves unknown and unchanged members.
func (r HostsData) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }

// DialerTarget is a supported subscription policy group and its flattened members.
type DialerTarget struct {
	Type           string     `json:"type"`
	Members        []string   `json:"members"`
	URL            *string    `json:"url"`
	Interval       *model.Int `json:"interval"`
	Tolerance      *model.Int `json:"tolerance"`
	Lazy           *bool      `json:"lazy"`
	Timeout        *model.Int `json:"timeout"`
	ExpectedStatus *string    `json:"expected-status"`
	Strategy       *string    `json:"strategy"`
	MaxFailedTimes *model.Int `json:"max-failed-times"`
	Fields         model.Fields
	Raw            model.Raw
}

// UnmarshalJSON retains stored members with the shared model codec.
func (r *DialerTarget) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, r, &r.Raw) }

// MarshalJSON preserves unknown and unchanged members.
func (r DialerTarget) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }

// DialersData is the bounded subscription dialer metadata stored beside nodes.
type DialersData struct {
	Targets    model.Map[DialerTarget] `json:"targets"`
	Unresolved []string                `json:"unresolved"`
	Fields     model.Fields
	Raw        model.Raw
}

// UnmarshalJSON retains stored members with the shared model codec.
func (r *DialersData) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, r, &r.Raw) }

// MarshalJSON preserves unknown and unchanged members.
func (r DialersData) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }
