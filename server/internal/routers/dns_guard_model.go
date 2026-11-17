package routers

import (
	"encoding/json"
	"slices"
)

// DNSGuardSnapshot contains the four DNS guard tables; nil Netwatch means unreadable.
type DNSGuardSnapshot map[string][]Row

// DesiredDNSGuard binds the validated settings to the previous publication.
type DesiredDNSGuard struct {
	Settings DNSGuard        `json:"settings"`
	Previous json.RawMessage `json:"previous"`
	// PolicyCIDRs supplies R08's destinations without coupling its model to R09.
	PolicyCIDRs []ProxyCIDR `json:"-"`
}

// DNSGuardSpec is the IPv4 LAN path and exemptions actually written to RouterOS.
type DNSGuardSpec struct {
	Agent     string    `json:"agent"`
	Interface string    `json:"interface"`
	Network   string    `json:"network"`
	Bypass    []string  `json:"bypass"`
	BlockDoT  bool      `json:"block_dot"`
	Failover  bool      `json:"failover"`
	Exempt    *[]string `json:"exempt,omitempty"`
}

// DNSGuardResource is an object proven by its ID and complete stable configuration.
type DNSGuardResource = rosResourceManifest

// DNSGuardManifest records the guard's creation order and ownership proof.
type DNSGuardManifest struct {
	Version   int                `json:"version"`
	Spec      *DNSGuardSpec      `json:"spec"`
	Resources []DNSGuardResource `json:"resources"`
}

// DNSGuardCreate describes one new object; Top requests the start of its table.
type DNSGuardCreate struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	Values Row    `json:"values"`
	Top    bool   `json:"top,omitempty"`
}

// DNSGuardReplace is a previewed incremental update. Nil Keep/Delete/Roles omit
// the delta keys on first enable, preserving the existing operation contract.
type DNSGuardReplace struct {
	OpHead
	Spec     *DNSGuardSpec       `json:"spec"`
	Previous json.RawMessage     `json:"previous"`
	Owned    []DNSGuardResource  `json:"owned"`
	Keep     *[]string           `json:"keep,omitempty"`
	Delete   *[]DNSGuardResource `json:"delete,omitempty"`
	Create   []DNSGuardCreate    `json:"create"`
	Roles    *[]string           `json:"roles,omitempty"`
}

// DNSGuardUndo is the reverse journal and the previous persisted publication.
type DNSGuardUndo struct {
	Kind     string          `json:"kind"`
	Changes  []ROSChange     `json:"changes"`
	Previous json.RawMessage `json:"previous"`
}

// DNSGuardItem explains what publishing does to one object.
type DNSGuardItem struct {
	Role   string `json:"role"`
	Object string `json:"object"`
	Detail string `json:"detail"`
	Status string `json:"status"`
	Paused bool   `json:"paused"`
}

// DNSGuardReport is the user-visible, read-only part of a guard plan.
type DNSGuardReport struct {
	Enabled  bool           `json:"enabled"`
	Status   string         `json:"status"`
	Message  string         `json:"message"`
	Items    []DNSGuardItem `json:"items"`
	Blockers []string       `json:"blockers"`
	Warnings []string       `json:"warnings"`
}

// DNSGuardPlan adds executable operations and verification roles to the report.
type DNSGuardPlan struct {
	DNSGuardReport
	Spec  json.RawMessage `json:"spec"`
	Ops   []Op            `json:"ops"`
	Roles []string        `json:"roles"`
}

// DNSGuardCheck is the exact ordered set of roles expected after publishing.
type DNSGuardCheck struct {
	Roles []string `json:"roles"`
}

const (
	// DNSGuardBypassList is the reserved exemption list.
	DNSGuardBypassList = "cf-dnsguard-bypass"
	dnsGuardPrefix     = "cf-dnsguard:"
	dnsGuardWatch      = "cf-dnsguard"
	dnsGuardHijack     = dnsGuardPrefix + "hijack"
)

var dnsGuardPaths = []string{rosAddresses, rosNAT, rosFilter, rosNetwatch}

func guardPath(path string) bool { return slices.Contains(dnsGuardPaths, path) }
func guardRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return slices.Clone(raw)
}

func parseDNSGuardManifest(raw json.RawMessage) (*DNSGuardManifest, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var m *DNSGuardManifest
	if json.Unmarshal(raw, &m) != nil {
		return nil, routerError(messageGuardManifest)
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
		if !guardPath(e.Path) || !rosIDPattern.MatchString(e.ID) || e.Role == "" || e.Config == nil || seen[key] || roles[e.Role] {
			return nil, routerError(messageGuardManifest)
		}
		seen[key] = true
		roles[e.Role] = true
	}
	return m, nil
}

// DNSGuardRelevant reports whether inspection must include the guard stage.
func DNSGuardRelevant(r *Router) bool {
	return r != nil && r.Type == "routeros" && (r.DNSGuard != nil && r.DNSGuard.Enabled || truthy(r.Extra["dns_guard_state"]))
}
