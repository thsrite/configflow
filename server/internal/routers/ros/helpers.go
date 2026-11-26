package ros

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/devhttp"
	"configflow/internal/netipx"
	"configflow/internal/routers/device"
)

const (
	// PathIPv6ND is the RouterOS IPv6 Neighbor Discovery path.
	PathIPv6ND = "/ipv6/nd"
	// PathIPv6AddressLists is the RouterOS IPv6 firewall address-list path.
	PathIPv6AddressLists = "/ipv6/firewall/address-list"
	// PathTables is the RouterOS routing table path.
	PathTables = "/routing/table"
)

// Resource identifies one previewed RouterOS object with its complete stable config.
type Resource struct {
	Path   string `json:"path"`
	ID     string `json:"id"`
	Config Row    `json:"config"`
}

// NewResource constructs a Resource snapshot from path and row.
func NewResource(path string, row Row) Resource {
	return Resource{Path: path, ID: row.ID(), Config: ConfigROSRow(row)}
}

// GuardRaw normalizes an empty manifest raw message to JSON "null", cloning non-empty bytes.
func GuardRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return slices.Clone(raw)
}

// JSONValuesEqual compares two JSON object payloads by decoding them as RouterOS Rows.
func JSONValuesEqual(a, b json.RawMessage) bool {
	var x, y Row
	return devhttp.DecodeJSON(a, &x) == nil && devhttp.DecodeJSON(b, &y) == nil && SameRow(x, y)
}

// IPv6DNSFlag parses a RouterOS boolean flag field for IPv6 DNS settings.
func IPv6DNSFlag(row Row, key string) (bool, error) {
	text := "false"
	if v, ok := row[key]; ok {
		text = v
	}
	switch strings.ToLower(text) {
	case "false", "no", "0":
		return false, nil
	case "true", "yes", "1":
		return true, nil
	default:
		return false, device.Error(fmt.Sprintf(MessageIPv6DNSFlag, key))
	}
}

// PolicyFlag reports whether value is a RouterOS truthy flag (including "1").
func PolicyFlag(value string) bool { return Flag(value) || value == "1" }

var ipv6Runtime = device.StringSet("active", "inactive", "immediate-gw", "belongs-to", "hw-offloaded", "debug.fwp-ptr", "actual-interface", "bytes", "packets", "uptime", "last-updated", "expires-after", "expires", "valid-lifetime", "preferred-lifetime", "valid", "preferred")

// StableIPv6Row keeps complete configuration, host bits and resolved LAN identity.
// Lease lifetimes are volatile; route activity is added separately by IPv6Evidence.
func StableIPv6Row(row Row) Row {
	out := Row{}
	for k, v := range row {
		if !strings.HasPrefix(k, ".") && !ipv6Runtime[k] {
			out[k] = Comparable(k, v)
		}
	}
	if value, ok := row["address"]; ok {
		address := value
		addrText, mask, hasMask := strings.Cut(address, "/")
		if a, err := netip.ParseAddr(addrText); err == nil {
			networkText := a.WithZone("").String()
			if hasMask {
				networkText += "/" + mask
			}
			if n, ok := netipx.ParseNetwork(networkText, false); ok {
				address = a.String() + "/" + strconv.Itoa(n.Bits())
			}
		}
		out["address"] = address
		if v, ok := row["actual-interface"]; ok {
			out["actual-interface"] = Comparable("actual-interface", v)
		}
	}
	return out
}

// Manifest distinguishes an untouched driver attribute from an explicit null.
type Manifest struct {
	Set   bool
	Value json.RawMessage
}

// Manifests carries feature-owned resource proofs to release persistence.
type Manifests struct {
	Policy, DNSGuard, IPv6FakeIP, IPv6Routes, IPv6PDGuard Manifest
}

// ManifestHolder exposes the per-release manifest updates (RouterOS only).
type ManifestHolder interface{ Manifests() Manifests }

// Manifests returns independent copies so callers cannot mutate driver proofs.
func (d *Client) Manifests() Manifests {
	return Manifests{
		IPv6PDGuard: Manifest{Set: d.ipv6PDGuardManifestSet, Value: slices.Clone(d.ipv6PDGuardManifest)},
		IPv6FakeIP:  Manifest{Set: d.ipv6FakeIPManifestSet, Value: slices.Clone(d.ipv6FakeIPManifest)},
		IPv6Routes:  Manifest{Set: d.ipv6RoutesManifestSet, Value: slices.Clone(d.ipv6RoutesManifest)},
		Policy:      Manifest{Set: d.policyManifestSet, Value: slices.Clone(d.policyManifest)},
		DNSGuard:    Manifest{Set: d.dnsGuardManifestSet, Value: slices.Clone(d.dnsGuardManifest)},
	}
}

// SetIPv6Manifest supplies an independent copy of one stored IPv6 receipt.
func (d *Client) SetIPv6Manifest(fake bool, raw json.RawMessage) {
	if fake {
		d.SetIPv6FakeIPManifest(raw)
	} else {
		d.SetIPv6RoutesManifest(raw)
	}
}

// SetIPv6FakeIPManifest supplies an independent copy of the stored IPv6 Fake-IP manifest.
func (d *Client) SetIPv6FakeIPManifest(raw json.RawMessage) {
	d.ipv6FakeIPManifest = GuardRaw(raw)
	d.ipv6FakeIPManifestSet = true
}

// SetIPv6RoutesManifest supplies an independent copy of the stored IPv6 routes manifest.
func (d *Client) SetIPv6RoutesManifest(raw json.RawMessage) {
	d.ipv6RoutesManifest = GuardRaw(raw)
	d.ipv6RoutesManifestSet = true
}

// SetIPv6PDGuardManifest supplies an independent copy of the stored PD receipt.
func (d *Client) SetIPv6PDGuardManifest(raw json.RawMessage) {
	d.ipv6PDGuardManifest = GuardRaw(raw)
	d.ipv6PDGuardManifestSet = true
}

// PolicyManifest returns the current RouterOS policy manifest bytes.
func (d *Client) PolicyManifest() json.RawMessage { return d.policyManifest }

// PolicyManifestSet reports whether the policy manifest was explicitly set.
func (d *Client) PolicyManifestSet() bool { return d.policyManifestSet }

// DNSGuardManifest returns the current DNS guard manifest bytes.
func (d *Client) DNSGuardManifest() json.RawMessage { return d.dnsGuardManifest }

// DNSGuardManifestSet reports whether the DNS guard manifest was explicitly set.
func (d *Client) DNSGuardManifestSet() bool { return d.dnsGuardManifestSet }

// IPv6FakeIPManifest returns the current IPv6 Fake-IP manifest bytes.
func (d *Client) IPv6FakeIPManifest() json.RawMessage { return d.ipv6FakeIPManifest }

// IPv6FakeIPManifestSet reports whether the IPv6 Fake-IP manifest was explicitly set.
func (d *Client) IPv6FakeIPManifestSet() bool { return d.ipv6FakeIPManifestSet }

// IPv6RoutesManifest returns the current IPv6 routes manifest bytes.
func (d *Client) IPv6RoutesManifest() json.RawMessage { return d.ipv6RoutesManifest }

// IPv6RoutesManifestSet reports whether the IPv6 routes manifest was explicitly set.
func (d *Client) IPv6RoutesManifestSet() bool { return d.ipv6RoutesManifestSet }

// IPv6PDGuardManifest returns the current IPv6 PD guard manifest bytes.
func (d *Client) IPv6PDGuardManifest() json.RawMessage { return d.ipv6PDGuardManifest }

// IPv6PDGuardManifestSet reports whether the IPv6 PD guard manifest was explicitly set.
func (d *Client) IPv6PDGuardManifestSet() bool { return d.ipv6PDGuardManifestSet }

// Endpoint returns the configured RouterOS base endpoint URL.
func (d *Client) Endpoint() string { return d.endpoint }

// HTTP returns the underlying device HTTP transport.
func (d *Client) HTTP() device.DeviceHTTP { return d.client }

// SetHTTP replaces the underlying device HTTP transport.
func (d *Client) SetHTTP(client device.DeviceHTTP) { d.client = client }

// Password returns the configured RouterOS password (for redaction only).
func (d *Client) Password() string { return d.password }

// BatchMin returns the minimum operation count for batched writes.
func (d *Client) BatchMin() int { return d.batchMin }

// SetBatchMin overrides the minimum operation count for batched writes.
func (d *Client) SetBatchMin(n int) { d.batchMin = n }

// RouteBatch returns the maximum batch size for route writes.
func (d *Client) RouteBatch() int { return d.routeBatch }

// SetRouteBatch overrides the maximum batch size for route writes.
func (d *Client) SetRouteBatch(n int) { d.routeBatch = n }

// ForwardFilterRows returns the cached /ip/firewall/filter rows from the last list read.
func (d *Client) ForwardFilterRows() []Row { return d.forwardFilterRows }

// ResetForwardFilterRows clears the cached forward filter rows.
func (d *Client) ResetForwardFilterRows() { d.forwardFilterRows = nil }

// NATListTouched reports whether NAT address lists were modified on this client.
func (d *Client) NATListTouched() bool { return d.natListTouched }

// MarkNATListTouched records that NAT address lists were modified on this client.
func (d *Client) MarkNATListTouched() { d.natListTouched = true }
