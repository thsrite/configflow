// Package ipv6 owns profile-bound Agent IPv6 readiness and post-publication
// verification. RouterOS inspection, publication and client storage stay with
// their existing owners; no host networking commands run on the panel.
package ipv6

import (
	"configflow/internal/routers"
	"encoding/json"
)

// Document is an immutable saved compatibility snapshot at the repository boundary.
type Document map[string]json.RawMessage

// Check is the existing RouterOS/Agent diagnostic representation.
type Check = routers.IPv6ReadinessCheck

// Configuration is the public projection of saved Mihomo IPv6 settings.
// Raw booleans retain invalid stored input for an actionable diagnostic.
type Configuration struct {
	IPv6            json.RawMessage `json:"ipv6"`
	DNSIPv6         json.RawMessage `json:"dns_ipv6"`
	IPv6Explicit    *bool           `json:"ipv6_explicit,omitempty"`
	DNSIPv6Explicit *bool           `json:"dns_ipv6_explicit,omitempty"`
	FakeIPv6Range   json.RawMessage `json:"fake_ipv6_range"`
	TUNEnabled      bool            `json:"tun_enabled"`
	FakeIPv6Route   bool            `json:"fake_ipv6_route"`
	FakeIPv6Enabled bool            `json:"fake_ipv6_enabled"`
	Errors          []string        `json:"errors"`
	Source          string          `json:"source"`
}

// ProbeTarget is a saved directly reachable TCP node candidate.
type ProbeTarget struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Literal bool   `json:"literal"`
}

// Connectivity deliberately distinguishes TCP reachability from a proved path.
type Connectivity struct {
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Scope   string   `json:"scope,omitempty"`
	Details []string `json:"details,omitempty"`
}

// Report contains an immutable readiness observation and its guarded prepare token.
type Report struct {
	Success          bool                     `json:"success"`
	AgentID          string                   `json:"agent_id"`
	ProfileID        string                   `json:"profile_id"`
	Configuration    Configuration            `json:"configuration"`
	Host             json.RawMessage          `json:"host"`
	Checks           []Check                  `json:"checks"`
	Routers          []*routers.IPv6Readiness `json:"routers"`
	CanPrepare       bool                     `json:"can_prepare"`
	PrepareInterface string                   `json:"prepare_interface"`
	CheckToken       string                   `json:"check_token"`
	Connectivity     Connectivity             `json:"connectivity"`
	ConfigRevision   *int64                   `json:"config_revision,omitempty"`
	CheckedAt        string                   `json:"checked_at,omitempty"`
	Cached           *bool                    `json:"cached,omitempty"`
	Publication      string                   `json:"publication,omitempty"`
	Stale            *bool                    `json:"stale,omitempty"`
	RefreshError     *RefreshError            `json:"refresh_error,omitempty"`
	Message          string                   `json:"message,omitempty"`
	ChangedRoutes    *int                     `json:"changed_routes,omitempty"`
}

// RefreshError explains why the previously persisted result is being shown.
type RefreshError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	At      string `json:"at"`
}

func raw(v any) json.RawMessage         { b, _ := json.Marshal(v); return b }
func object(b json.RawMessage) Document { var v Document; _ = json.Unmarshal(b, &v); return v }
func text(b json.RawMessage) string     { var s string; _ = json.Unmarshal(b, &s); return s }
func yes(b json.RawMessage) bool        { return string(b) == "true" }
func ptr[T any](v T) *T                 { return &v }
func check(id, label, status, message string, details ...string) Check {
	return Check{ID: id, Label: label, Status: status, Message: message, Details: details}
}
