package dnsguard

import (
	"encoding/json"
	"slices"

	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
)

// Snapshot contains the four DNS guard tables; nil Netwatch means unreadable.
type Snapshot map[string][]ros.Row

// Spec is the IPv4 LAN path and exemptions actually written to RouterOS.
type Spec struct {
	Agent     string    `json:"agent"`
	Interface string    `json:"interface"`
	Network   string    `json:"network"`
	Bypass    []string  `json:"bypass"`
	BlockDoT  bool      `json:"block_dot"`
	Failover  bool      `json:"failover"`
	Exempt    *[]string `json:"exempt,omitempty"`
}

// Resource is an object proven by its ID and complete stable configuration.
type Resource = ros.ResourceManifest

// Manifest records the guard's creation order and ownership proof.
type Manifest struct {
	Version   int        `json:"version"`
	Spec      *Spec      `json:"spec"`
	Resources []Resource `json:"resources"`
}

// Create describes one new object; Top requests the start of its table.
type Create struct {
	Role   string  `json:"role"`
	Path   string  `json:"path"`
	Values ros.Row `json:"values"`
	Top    bool    `json:"top,omitempty"`
}

// Replace is a previewed incremental update. Nil Keep/Delete/Roles omit
// the delta keys on first enable, preserving the existing operation contract.
type Replace struct {
	device.OpHead
	Spec     *Spec           `json:"spec"`
	Previous json.RawMessage `json:"previous"`
	Owned    []Resource      `json:"owned"`
	Keep     *[]string       `json:"keep,omitempty"`
	Delete   *[]Resource     `json:"delete,omitempty"`
	Create   []Create        `json:"create"`
	Roles    *[]string       `json:"roles,omitempty"`
}

// Undo is the reverse journal and the previous persisted publication.
type Undo struct {
	Kind     string          `json:"kind"`
	Changes  []ros.ROSChange `json:"changes"`
	Previous json.RawMessage `json:"previous"`
}

// Item explains what publishing does to one object.
type Item struct {
	Role   string `json:"role"`
	Object string `json:"object"`
	Detail string `json:"detail"`
	Status string `json:"status"`
	Paused bool   `json:"paused"`
}

// Report is the user-visible, read-only part of a guard plan.
type Report struct {
	Enabled  bool     `json:"enabled"`
	Status   string   `json:"status"`
	Message  string   `json:"message"`
	Items    []Item   `json:"items"`
	Blockers []string `json:"blockers"`
	Warnings []string `json:"warnings"`
}

// Plan adds executable operations and verification roles to the report.
type Plan struct {
	Report
	Spec  json.RawMessage `json:"spec"`
	Ops   []device.Op     `json:"ops"`
	Roles []string        `json:"roles"`
}

// Check is the exact ordered set of roles expected after publishing.
type Check struct {
	Roles []string `json:"roles"`
}

const (
	// BypassList is the reserved exemption list.
	BypassList = "cf-dnsguard-bypass"
	// Prefix is the comment prefix shared by managed DNS guard rules.
	Prefix = "cf-dnsguard:"
	// WatchName is the reserved Netwatch entry name.
	WatchName = "cf-dnsguard"
	// HijackComment is the comment shared by managed DNS hijack NAT rules.
	HijackComment = Prefix + "hijack"
)

var paths = []string{ros.PathAddresses, ros.PathNAT, ros.PathFilter, ros.PathNetwatch}

// Paths returns a copy of the RouterOS paths inspected by the DNS guard.
func Paths() []string { return slices.Clone(paths) }

func guardPath(path string) bool { return slices.Contains(paths, path) }

// ParseManifest decodes and validates a stored DNS guard ownership manifest.
func ParseManifest(raw json.RawMessage) (*Manifest, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var m *Manifest
	if jsondecode.Unmarshal(raw, &m) != nil {
		return nil, device.Error(messageGuardManifest)
	}
	if m == nil {
		return nil, nil
	}
	var object map[string]json.RawMessage
	_ = json.Unmarshal(raw, &object)
	if len(object) == 0 {
		return nil, nil
	}
	seen := map[[2]string]bool{}
	roles := map[string]bool{}
	for _, e := range m.Resources {
		key := [2]string{e.Path, e.ID}
		if !guardPath(e.Path) || !ros.IDPattern.MatchString(e.ID) || e.Role == "" || e.Config == nil || seen[key] || roles[e.Role] {
			return nil, device.Error(messageGuardManifest)
		}
		seen[key] = true
		roles[e.Role] = true
	}
	return m, nil
}

// Relevant reports whether inspection must include the guard stage.
func Relevant(r *spec.Router) bool {
	return r != nil && r.Type == "routeros" && (r.DNSGuard != nil && r.DNSGuard.Enabled || device.HasStoredState(r.Extra["dns_guard_state"]))
}
