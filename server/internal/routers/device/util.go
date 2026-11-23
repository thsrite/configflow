package device

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/devhttp"
	"configflow/internal/netipx"
)

// Unique returns values with duplicates removed, preserving first-seen order.
func Unique(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

// FirstNonEmpty returns the first non-empty string in values, or "".
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// NonEmpty returns a slice containing only the non-empty strings from values.
func NonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// StringSet constructs a membership map for values.
func StringSet(values ...string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

// OrText returns s if non-empty, otherwise fallback.
func OrText(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// JSON marshals v to json.RawMessage, ignoring marshal errors for known-safe structs.
func JSON[T any](v T) json.RawMessage { data, _ := json.Marshal(v); return data }

// JSONEqual compares two JSON-shaped values structurally (decoded numbers as
// written; arrays and objects compare deeply, never panicking).
func JSONEqual(a, b any) bool {
	normalize := func(v any) (any, bool) {
		data, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var out any
		return out, devhttp.DecodeJSON(data, &out) == nil
	}
	left, ok1 := normalize(a)
	right, ok2 := normalize(b)
	return ok1 && ok2 && reflect.DeepEqual(left, right)
}

// HasStoredState keeps opaque nonempty ownership evidence fail-closed, including
// malformed records. Legacy empty/null/false/zero receipts carry no ownership.
func HasStoredState(raw json.RawMessage) bool {
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return false
	}
	switch s := compact.String(); s {
	case "", "null", "false", `""`, "[]", "{}":
		return false
	default:
		f, err := strconv.ParseFloat(s, 64)
		return err != nil || f != 0
	}
}

// RequestString is strict validation of a user identity, never a coercion.
func RequestString(raw json.RawMessage) (string, bool) {
	var text *string
	if json.Unmarshal(raw, &text) != nil || text == nil {
		return "", false
	}
	return *text, true
}

// Networks parses CIDRs, single IPs and iKuai a-b ranges into IPv4
// networks, skipping anything else.
func Networks(values []string) []netip.Prefix {
	var out []netip.Prefix
	for _, value := range values {
		text := strings.TrimSpace(value)
		if first, last, isRange := strings.Cut(text, "-"); isRange {
			a, ok1 := netipx.ParseAddr4(strings.TrimSpace(first))
			b, ok2 := netipx.ParseAddr4(strings.TrimSpace(last))
			if ok1 && ok2 && !b.Less(a) {
				out = append(out, netipx.Summarize(a, b)...)
			}
		} else if n, ok := netipx.ParseNetwork4(text, false); ok {
			out = append(out, n)
		}
	}
	return out
}

// SupernetIndex answers "is target inside one of these networks" by hashing
// target's supernets (at most 33) instead of scanning every network. Each
// network carries owner positions, e.g. the rule it came from.
type SupernetIndex struct {
	owners  map[netip.Prefix][]int
	lengths []int // sorted prefix lengths present
}

// NewSupernetIndex indexes networks under owner 0.
func NewSupernetIndex(networks []netip.Prefix) *SupernetIndex {
	ix := &SupernetIndex{owners: map[netip.Prefix][]int{}}
	ix.Add(networks, 0)
	return ix
}

// Add registers networks under owner.
func (ix *SupernetIndex) Add(networks []netip.Prefix, owner int) {
	for _, n := range networks {
		n = n.Masked()
		if !slices.Contains(ix.owners[n], owner) {
			ix.owners[n] = append(ix.owners[n], owner)
		}
		if !slices.Contains(ix.lengths, n.Bits()) {
			ix.lengths = append(ix.lengths, n.Bits())
			slices.Sort(ix.lengths)
		}
	}
}

func (ix *SupernetIndex) supernets(target netip.Prefix, yield func(netip.Prefix) bool) {
	for _, bits := range ix.lengths {
		if bits > target.Bits() {
			return
		}
		if !yield(netip.PrefixFrom(target.Addr(), bits).Masked()) {
			return
		}
	}
}

// Owners returns the sorted owner indices of every indexed network containing target.
func (ix *SupernetIndex) Owners(target netip.Prefix) []int {
	var found []int
	ix.supernets(target, func(p netip.Prefix) bool { found = append(found, ix.owners[p]...); return true })
	slices.Sort(found)
	return slices.Compact(found)
}

// Covers reports whether any indexed network contains target.
func (ix *SupernetIndex) Covers(target netip.Prefix) bool {
	covered := false
	ix.supernets(target, func(p netip.Prefix) bool { covered = len(ix.owners[p]) > 0; return !covered })
	return covered
}
