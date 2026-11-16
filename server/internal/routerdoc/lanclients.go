package routerdoc

import (
	"fmt"
	"net/netip"
	"strings"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/netipx"
	"configflow/internal/pystr"
)

// Port of backend/routers/lan_clients.py: vendor-neutral LAN client
// sightings read live from a main router (see the Python module doc for the
// meaning of presence and basis).

// Rank is RANK.
var Rank = map[string]int{"online": 3, "recent": 2, "offline": 1, "unknown": 0}

// SourceOrder is SOURCE_ORDER.
var SourceOrder = []string{"arp", "lease", "online"}

// Sighting is the Sighting dataclass. LastSeen nil is None.
type Sighting struct {
	IP       string
	MAC      string
	Hostname string
	Sources  map[string]bool // 'arp' | 'lease' | 'online'
	Presence string          // default "unknown"
	Basis    string
	LastSeen *int64
	RouterID string
	FixedIP  *jsonx.Object // nil is None
}

// NewSighting is Sighting(ip) with the dataclass defaults.
func NewSighting(ip string) *Sighting {
	return &Sighting{IP: ip, Sources: map[string]bool{}, Presence: "unknown"}
}

// Public is Sighting.public().
func (s *Sighting) Public() *jsonx.Object {
	sources := []any{}
	for _, item := range SourceOrder {
		if s.Sources[item] {
			sources = append(sources, item)
		}
	}
	var lastSeen any
	if s.LastSeen != nil {
		lastSeen = *s.LastSeen
	}
	out := jsonx.Obj("mac", s.MAC, "hostname", s.Hostname, "sources", sources, "presence", s.Presence,
		"basis", s.Basis, "last_seen", lastSeen, "router_id", s.RouterID)
	if s.FixedIP != nil {
		out.Set("fixed_ip", s.FixedIP)
	}
	return out
}

// Reading is the Reading dataclass (Sightings keyed by IPv4 text, in the
// order SightingOrder lists them).
type Reading struct {
	Sightings     map[string]*Sighting
	SightingOrder []string
	Own           map[string]bool
	LAN           []netip.Prefix // a set of IPv4Network: unique values
	Complete      bool
	Notes         []string
}

// NewReading is Reading() with the dataclass defaults.
func NewReading() *Reading {
	return &Reading{Sightings: map[string]*Sighting{}, Own: map[string]bool{}, Complete: true}
}

// Covers is Reading.covers(ip): whether absence from this reading is
// evidence that ip is gone. The error is IPv4Address's ValueError.
func (r *Reading) Covers(ip string) (bool, error) {
	address, ok := netipx.ParseAddr4(ip)
	if !ok {
		return false, &ValueError{Message: "invalid IPv4 address: " + jsonx.Repr(ip)}
	}
	if !r.Complete {
		return false, nil
	}
	for _, network := range r.LAN {
		if network.Contains(address) {
			return true, nil
		}
	}
	return false, nil
}

// ValueError is a Python ValueError raised by a helper here (its text is
// not Python's).
type ValueError struct{ Message string }

func (e *ValueError) Error() string { return e.Message }

// PythonClass is "AddressValueError" (a ValueError subclass).
func (e *ValueError) PythonClass() string { return "AddressValueError" }

func pyStr(value any) string {
	if !jsonx.Truthy(value) {
		return ""
	}
	return jsonx.Str(value)
}

// IPv4 is ipv4(value): canonical IPv4 host text, or "" for anything else.
func IPv4(value any) string {
	address, ok := netipx.ParseAddr4(pystr.Strip(pyStr(value)))
	if !ok {
		return ""
	}
	return address.String()
}

// NormalizeMAC is normalize_mac(value): AA:BB:CC:DD:EE:FF, or "". Like
// Python it upper-cases with the full Unicode mapping (U+FB00 "ﬀ" becomes
// "FF") and parses each part with int(part, 16) (non-ASCII decimal digits,
// a sign, surrounding white space).
func NormalizeMAC(value any) string {
	text := strings.ReplaceAll(pystr.Upper(pystr.Strip(pyStr(value))), "-", ":")
	parts := strings.Split(text, ":")
	if len(parts) != 6 {
		return ""
	}
	for _, part := range parts {
		if n := utf8.RuneCountInString(part); n != 1 && n != 2 {
			return ""
		}
	}
	out := make([]string, 6)
	for i, part := range parts {
		number, err := pystr.Int(part, 16)
		if err != nil {
			return ""
		}
		out[i] = fmt.Sprintf("%02X", number)
	}
	return strings.Join(out, ":")
}

// Better is better(current, candidate): the merged sighting of one address;
// the stronger presence decides. The error is the KeyError Python raises for
// a presence outside Rank.
func Better(current, candidate *Sighting) (*Sighting, error) {
	if current == nil {
		return candidate, nil
	}
	candidateRank, ok := Rank[candidate.Presence]
	if !ok {
		return nil, &KeyError{Key: candidate.Presence}
	}
	currentRank, ok := Rank[current.Presence]
	if !ok {
		return nil, &KeyError{Key: current.Presence}
	}
	strong, weak := current, candidate
	if candidateRank > currentRank {
		strong, weak = candidate, current
	}
	if strong.Sources == nil {
		strong.Sources = map[string]bool{}
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
	return strong, nil
}

// KeyError is Python's KeyError for a missing str key.
type KeyError struct{ Key string }

func (e *KeyError) Error() string { return jsonx.Repr(e.Key) }

// PythonClass is "KeyError".
func (e *KeyError) PythonClass() string { return "KeyError" }
