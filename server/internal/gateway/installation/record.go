// Package installation owns durable gateway installation capabilities and ordered
// two-core publication. Scripts, configuration generation and device I/O remain
// with installer, onboarding, deploy and host respectively.
package installation

import (
	"configflow/internal/gateway/host"
	"encoding/json"
)

// Error is an installation refusal. Cause remains available to cancellation
// and shared-owner classifiers; Code is an optional frontend remedy code.
type Error struct {
	Message string
	Status  int
	Code    string
	Cause   error
}

func (e *Error) Error() string { return e.Message }

// Unwrap retains the owning error.
func (e *Error) Unwrap() error                 { return e.Cause }
func failure(message string, status int) error { return &Error{Message: message, Status: status} }

// Record is the private Python-compatible durable installation session.
// The raw model preserves unknown members during a read/modify/write.
type Record struct {
	ID             string                     `json:"id"`
	ProfileID      string                     `json:"profile_id"`
	ConfigRevision int64                      `json:"config_revision"`
	Name           string                     `json:"name"`
	ServerURL      string                     `json:"server_url"`
	Network        host.Spec                  `json:"network"`
	Ports          map[string]int             `json:"ports"`
	Token          string                     `json:"token"`
	CreatedAt      string                     `json:"created_at"`
	Expires        float64                    `json:"expires"`
	Phase          string                     `json:"phase"`
	AgentIDs       map[string]string          `json:"agent_ids"`
	Registered     map[string]string          `json:"registered"`
	Deployments    map[string]json.RawMessage `json:"deployments"`
	Message        string                     `json:"message"`
	AutoNetwork    bool                       `json:"auto_network,omitempty"`
	RequestedIP    string                     `json:"requested_ip,omitempty"`
	Takeover       string                     `json:"takeover,omitempty"`
	Script         string                     `json:"script"`
	DownloadPath   string                     `json:"download_path,omitempty"`
	Remedy         json.RawMessage            `json:"remedy,omitempty"`
	extra          map[string]json.RawMessage
}

// UnmarshalJSON validates native stored fields and retains unknown members.
func (r *Record) UnmarshalJSON(data []byte) error {
	type plain Record
	var next plain
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	fields := object(data)
	if fields == nil {
		return failure(msgCorrupt, 409)
	}
	if string(fields["config_revision"]) == "null" {
		return failure(msgCorrupt, 409)
	}
	known, err := json.Marshal(next)
	if err != nil {
		return err
	}
	for key := range object(known) {
		delete(fields, key)
	}
	// Optional absent/zero fields are still declared, never opaque extras.
	for _, key := range []string{"auto_network", "requested_ip", "takeover", "download_path", "remedy"} {
		delete(fields, key)
	}
	next.extra = fields
	*r = Record(next)
	return nil
}

// MarshalJSON merges opaque members into the native record representation.
func (r Record) MarshalJSON() ([]byte, error) {
	type plain Record
	b, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	fields := object(b)
	for key, value := range r.extra {
		if _, known := fields[key]; !known {
			fields[key] = value
		}
	}
	return json.Marshal(fields)
}

type task struct {
	DeploymentID string `json:"deployment_id"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	Error        string `json:"error"`
	HTTPStatus   int    `json:"http_status"`
	Pending      bool   `json:"pending"`
	Success      bool   `json:"success"`
	Existing     bool   `json:"existing"`
}

func taskOf(raw json.RawMessage) task { var v task; _ = json.Unmarshal(raw, &v); return v }
func terminal(status string) bool {
	return status == "failed" || status == "rolled_back" || status == "rollback_failed"
}

var services = [...]string{"mihomo", "oxidns"}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func object(b json.RawMessage) map[string]json.RawMessage {
	var v map[string]json.RawMessage
	_ = json.Unmarshal(b, &v)
	return v
}
func text(b json.RawMessage) string { var s string; _ = json.Unmarshal(b, &s); return s }
func flag(b json.RawMessage) bool   { var v bool; _ = json.Unmarshal(b, &v); return v }
