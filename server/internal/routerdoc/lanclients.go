// Package routerdoc holds router helpers that packages below internal/routers
// (repo, agents) need: LAN client sightings read live from a main router and
// the check of stored client fixed-IP bindings. Leaf package.
package routerdoc

import (
	"encoding/json"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/netipx"
)

// Rank orders presence values: online (seen just now), recent (known,
// unconfirmed), offline (evidence it is gone), unknown.
var Rank = map[string]int{"online": 3, "recent": 2, "offline": 1, "unknown": 0}

// SourceOrder is the order sources are reported in.
var SourceOrder = []string{"arp", "lease", "online"}

// Sighting is one router's view of a LAN address; its JSON is what the UI
// sees. Basis says which evidence decided the presence (arp, lease, probe,
// terminal, stale, absent, router, outside).
type Sighting struct {
	IP       string          `json:"-"`
	MAC      string          `json:"mac"`
	Hostname string          `json:"hostname"`
	Sources  []string        `json:"sources"` // in SourceOrder (AddSource)
	Presence string          `json:"presence"`
	Basis    string          `json:"basis"`
	LastSeen *int64          `json:"last_seen"` // seconds since the last DHCP contact
	RouterID string          `json:"router_id"`
	FixedIP  json.RawMessage `json:"fixed_ip,omitempty"` // the client's fixed-IP binding
}

// NewSighting is an address with no evidence yet.
func NewSighting(ip string) *Sighting {
	return &Sighting{IP: ip, Sources: []string{}, Presence: "unknown"}
}

// AddSource records a source (arp, lease, online), keeping SourceOrder.
func (s *Sighting) AddSource(source string) {
	if !slices.Contains(s.Sources, source) {
		s.Sources = append(s.Sources, source)
		slices.SortFunc(s.Sources, func(a, b string) int {
			return slices.Index(SourceOrder, a) - slices.Index(SourceOrder, b)
		})
	}
}

// Reading is one router's LAN client table: sightings by IPv4, the router's
// own addresses, the scanned subnets; incomplete: absence proves nothing.
type Reading struct {
	Sightings map[string]*Sighting
	Own       map[string]bool
	LAN       []netip.Prefix
	Complete  bool
	Notes     []string
}

// NewReading is an empty, complete reading.
func NewReading() *Reading {
	return &Reading{Sightings: map[string]*Sighting{}, Own: map[string]bool{}, Complete: true}
}

// Covers reports whether absence from this reading is evidence that ip is gone.
func (r *Reading) Covers(ip string) bool {
	address, ok := netipx.ParseAddr4(ip)
	return ok && r.Complete && slices.ContainsFunc(r.LAN, func(n netip.Prefix) bool { return n.Contains(address) })
}

// IPv4 is the canonical IPv4 host text, or "" for anything else.
func IPv4(value string) string {
	if a, ok := netipx.ParseAddr4(strings.TrimSpace(value)); ok {
		return a.String()
	}
	return ""
}

// NormalizeMAC returns AA:BB:CC:DD:EE:FF for a MAC written with ':' or '-' and one- or two-digit parts, or "".
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

// Better merges two sightings of one address: the stronger presence wins and keeps what the weaker one adds.
func Better(current, candidate *Sighting) *Sighting {
	if current == nil {
		return candidate
	}
	strong, weak := current, candidate
	if Rank[candidate.Presence] > Rank[current.Presence] {
		strong, weak = candidate, current
	}
	for _, source := range weak.Sources {
		strong.AddSource(source)
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
