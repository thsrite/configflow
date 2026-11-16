// Package routerdoc holds router helpers that packages below internal/routers
// (repo, agents) need: LAN client sightings read live from a main router and
// the check of stored client fixed-IP bindings. Leaf package.
package routerdoc

import (
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"

	"configflow/internal/netipx"
)

// Presence values, strongest first: online (seen just now), recent (known,
// unconfirmed), offline (evidence it is gone), unknown.
var Rank = map[string]int{"online": 3, "recent": 2, "offline": 1, "unknown": 0}

// SourceOrder is the order sources are reported in.
var SourceOrder = []string{"arp", "lease", "online"}

// Sighting is one router's view of a LAN address. Basis says which evidence
// decided the presence (arp, lease, probe, terminal, stale, absent, router,
// outside).
type Sighting struct {
	IP       string
	MAC      string
	Hostname string
	Sources  map[string]bool
	Presence string
	Basis    string
	LastSeen *int64 // seconds since the last DHCP contact
	RouterID string
	FixedIP  json.RawMessage // the client's fixed-IP binding, when it has one
}

// NewSighting is an address with no evidence yet.
func NewSighting(ip string) *Sighting {
	return &Sighting{IP: ip, Sources: map[string]bool{}, Presence: "unknown"}
}

// SightingView is a sighting as the UI sees it.
type SightingView struct {
	MAC      string          `json:"mac"`
	Hostname string          `json:"hostname"`
	Sources  []string        `json:"sources"`
	Presence string          `json:"presence"`
	Basis    string          `json:"basis"`
	LastSeen *int64          `json:"last_seen"`
	RouterID string          `json:"router_id"`
	FixedIP  json.RawMessage `json:"fixed_ip,omitempty"`
}

// View is the sighting as returned to the UI.
func (s *Sighting) View() SightingView {
	v := SightingView{MAC: s.MAC, Hostname: s.Hostname, Sources: []string{}, Presence: s.Presence, Basis: s.Basis,
		LastSeen: s.LastSeen, RouterID: s.RouterID, FixedIP: s.FixedIP}
	for _, source := range SourceOrder {
		if s.Sources[source] {
			v.Sources = append(v.Sources, source)
		}
	}
	return v
}

// Reading is one router's LAN client table.
type Reading struct {
	Sightings map[string]*Sighting // by IPv4
	Own       map[string]bool      // the router's own addresses
	LAN       []netip.Prefix       // subnets whose clients were scanned
	Complete  bool                 // false: a missing address proves nothing
	Notes     []string
}

// NewReading is an empty, complete reading.
func NewReading() *Reading {
	return &Reading{Sightings: map[string]*Sighting{}, Own: map[string]bool{}, Complete: true}
}

// Covers reports whether absence from this reading is evidence that ip
// is gone (false for an invalid address).
func (r *Reading) Covers(ip string) bool {
	address, ok := netipx.ParseAddr4(ip)
	if !ok || !r.Complete {
		return false
	}
	for _, network := range r.LAN {
		if network.Contains(address) {
			return true
		}
	}
	return false
}

// IPv4 is the canonical IPv4 host text, or "" for anything else.
func IPv4(value string) string {
	if a, ok := netipx.ParseAddr4(strings.TrimSpace(value)); ok {
		return a.String()
	}
	return ""
}

// NormalizeMAC returns AA:BB:CC:DD:EE:FF for a MAC written with ':' or '-'
// and one- or two-digit parts, or "".
func NormalizeMAC(value string) string {
	parts := strings.Split(strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(value)), "-", ":"), ":")
	if len(parts) != 6 {
		return ""
	}
	for i, part := range parts {
		n, err := strconv.ParseUint(strings.TrimSpace(part), 16, 8)
		if err != nil || len(part) < 1 || len(part) > 2 {
			return ""
		}
		parts[i] = strings.ToUpper(strconv.FormatUint(n|0x100, 16)[1:])
	}
	return strings.Join(parts, ":")
}

// Better merges two sightings of one address: the stronger presence wins
// and keeps what the weaker one adds.
func Better(current, candidate *Sighting) *Sighting {
	if current == nil {
		return candidate
	}
	strong, weak := current, candidate
	if Rank[candidate.Presence] > Rank[current.Presence] {
		strong, weak = candidate, current
	}
	for source := range weak.Sources {
		strong.Sources[source] = true
	}
	if strong.MAC == "" {
		strong.MAC = weak.MAC
	}
	if strong.Hostname == "" {
		strong.Hostname = weak.Hostname
	}
	if strong.LastSeen == nil {
		strong.LastSeen = weak.LastSeen
	}
	return strong
}
